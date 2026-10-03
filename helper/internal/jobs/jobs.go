// Package jobs runs download → normalize pipelines with a bounded queue.
//
// 동시성: 전체 동시 작업 수(기본 2)와 사이트(호스트)별 1개 제한 — 같은 사이트에 동시 요청을 몰면
// 차단(rate limit)이 빨라지기 때문이다. 진행률 이벤트는 작업당 200ms로 묶어 보낸다.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/library"
	"birdcage/helper/internal/media"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/subs"
)

// State of a job.
type State string

const (
	Queued      State = "queued"
	Probing     State = "probing"
	Downloading State = "downloading"
	Normalizing State = "normalizing"
	Done        State = "done"
	Failed      State = "failed"
	Cancelled   State = "cancelled"
)

// Terminal reports whether the state is final.
func (s State) Terminal() bool { return s == Done || s == Failed || s == Cancelled }

// Options for a job.
type Options struct {
	MaxHeight int           `json:"maxHeight"`
	Profile   media.Profile `json:"profile"`
	// ProbeID reuses a Probe result: the download starts from its info.json without re-extraction.
	ProbeID string `json:"probeId,omitempty"`
	// FormatID is the chosen variant (engine.Variant.FormatID); "" selects by MaxHeight.
	FormatID string `json:"formatId,omitempty"`
	// Range downloads only part of the source (plus handles) and reports In/Out in Snapshot.Clip.
	Range *engine.Section `json:"range,omitempty"`
	// AudioOnly downloads the best audio and writes WAV (PCM 16-bit 48 kHz).
	AudioOnly bool `json:"audioOnly,omitempty"`
	// Subtitles fetches one subtitle track as an SRT sidecar on the file's timeline.
	Subtitles *SubtitleRequest `json:"subtitles,omitempty"`
}

// SubtitleRequest names one subtitle track from media.info subtitles.
type SubtitleRequest struct {
	Lang string `json:"lang"`
	Auto bool   `json:"auto,omitempty"`
}

// RangeHandleSec is extra media kept before/after a requested range for trims and transitions.
const RangeHandleSec = 1.0

// Clip is the requested range inside the output file (In/Out to set on the Premiere item).
type Clip struct {
	InSec  float64 `json:"inSec"`
	OutSec float64 `json:"outSec"`
}

// StageTiming is the wall time of one pipeline stage.
type StageTiming struct {
	Stage string  `json:"stage"`
	MS    float64 `json:"ms"`
}

// Snapshot is the externally visible job state (sent to panels as-is).
type Snapshot struct {
	JobID    string           `json:"jobId"`
	URL      string           `json:"url"`
	State    State            `json:"state"`
	Options  Options          `json:"options"`
	Progress float64          `json:"progress"` // stage progress 0..1, -1 unknown
	Download *engine.Progress `json:"download,omitempty"`
	Info     *engine.Info     `json:"info,omitempty"`
	Media    *media.Info      `json:"media,omitempty"` // final file
	Plan     *media.Plan      `json:"plan,omitempty"`
	File     string           `json:"file,omitempty"`
	Clip     *Clip            `json:"clip,omitempty"`
	// SubtitleFile is the SRT sidecar next to File (aligned to File's timeline).
	SubtitleFile string          `json:"subtitleFile,omitempty"`
	Failure      *engine.Failure `json:"failure,omitempty"`
	Warnings     []string        `json:"warnings,omitempty"`
	Timings      []StageTiming   `json:"timings,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// Event is emitted on every visible change.
type Event struct {
	Type string // update | done | failed
	Job  Snapshot
}

// Toolset is what one pipeline run uses. It is resolved per job so tools installed
// after startup (tools.ensure) are picked up without restarting the helper.
type Toolset struct {
	Engine     engine.Engine
	Prober     *media.Prober
	Normalizer *media.Normalizer
	Warnings   []string // 예: JS 런타임 없음 → YouTube 화질 저하 가능
}

// Deps are the pipeline's collaborators.
type Deps struct {
	Tools      func(ctx context.Context) (Toolset, error)
	LibraryDir string
	WorkDir    string // per-job scratch dirs are created inside
	Rec        *perf.Recorder
	Log        *slog.Logger
}

// Config tunes the manager.
type Config struct {
	MaxConcurrent  int
	PerHostLimit   int
	ProgressEvery  time.Duration
	NetworkRetries int
	KeepWorkDir    bool // 디버깅용: 작업 폴더를 지우지 않음
	// ProbeTTL bounds how long a probe can be reused. 플랫폼의 미디어 URL은 서명·만료가 있어
	// (YouTube 약 6시간, Instagram·TikTok은 더 짧을 수 있음) 넉넉히 짧게 잡는다.
	ProbeTTL  time.Duration
	MaxProbes int // cached probes kept at once (oldest evicted)
}

func (c *Config) defaults() {
	if c.ProbeTTL <= 0 {
		c.ProbeTTL = 20 * time.Minute
	}
	if c.MaxProbes <= 0 {
		c.MaxProbes = 16
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 2
	}
	if c.PerHostLimit <= 0 {
		c.PerHostLimit = 1
	}
	if c.ProgressEvery <= 0 {
		c.ProgressEvery = 200 * time.Millisecond
	}
	if c.NetworkRetries < 0 {
		c.NetworkRetries = 0
	}
}

type job struct {
	snap     Snapshot
	host     string
	cancel   context.CancelFunc
	lastEmit time.Time
}

// Manager owns all jobs.
type Manager struct {
	cfg  Config
	deps Deps

	mu       sync.Mutex
	jobs     map[string]*job
	order    []string // submission order
	running  map[string]int
	active   int
	subs     map[int]*subscriber
	nextSub  int
	probes   map[string]*probe
	probeSem chan struct{}
	closed   bool
	wg       sync.WaitGroup
	ctx      context.Context
	stop     context.CancelFunc
}

// New creates a manager.
func New(cfg Config, deps Deps) *Manager {
	cfg.defaults()
	if deps.Log == nil {
		deps.Log = slog.New(slog.DiscardHandler)
	}
	ctx, stop := context.WithCancel(context.Background())
	// 이전 실행이 남긴 조회 폴더(비정상 종료)를 치운다.
	if old, _ := filepath.Glob(filepath.Join(deps.WorkDir, "probe-*")); len(old) > 0 {
		for _, d := range old {
			_ = os.RemoveAll(d)
		}
	}
	return &Manager{cfg: cfg, deps: deps, jobs: map[string]*job{}, running: map[string]int{},
		subs: map[int]*subscriber{}, probes: map[string]*probe{}, probeSem: make(chan struct{}, 2), ctx: ctx, stop: stop}
}

// probe is a cached link probe.
type probe struct {
	id, url string
	info    *engine.Info
	dir     string
	at      time.Time
}

// Probe extracts a link's metadata (formats, thumbnail, subtitles) and caches it so a job created
// with Options.ProbeID downloads without re-extraction (yt-dlp --load-info-json).
// 실측: 다운로드 2.23s → 0.86s(docs/decisions/0010). 변형의 변환 필요 여부는 세션 프로필로 표시한다.
func (m *Manager) Probe(ctx context.Context, rawURL string, maxHeight int, profile media.Profile) (string, *engine.Info, error) {
	u, err := parseURL(rawURL)
	if err != nil {
		return "", nil, engine.Fail(engine.KindUnsupportedURL, err.Error())
	}
	select {
	case m.probeSem <- struct{}{}:
	case <-ctx.Done():
		return "", nil, engine.Fail(engine.KindCancelled, "")
	}
	defer func() { <-m.probeSem }()
	ts, err := m.deps.Tools(ctx)
	if err != nil {
		return "", nil, asFailure(err, engine.KindToolMissing)
	}
	id := "p" + newID()[1:]
	dir := filepath.Join(m.deps.WorkDir, "probe-"+id)
	span := m.deps.Rec.Start("probe", "host", hostKey(u.Hostname()), "engine", ts.Engine.Name(), "max_height", maxHeight)
	var info *engine.Info
	err = m.withRetry(ctx, func() error {
		var ps perf.ProcStats
		var err error
		info, ps, err = ts.Engine.Probe(ctx, u.String(), dir, maxHeight)
		span.SetProc(ps)
		return err
	})
	if err != nil {
		span.End(err)
		_ = os.RemoveAll(dir)
		if ctx.Err() != nil {
			return "", nil, engine.Fail(engine.KindCancelled, "")
		}
		return "", nil, asFailure(err, engine.KindInternal)
	}
	for i := range info.Variants {
		info.Variants[i].Transcode = info.Variants[i].NeedsTranscode(profile == media.ProfileModern)
	}
	span.Set("variants", len(info.Variants), "subtitles", len(info.Subtitles))
	span.End(nil)
	m.mu.Lock()
	m.probes[id] = &probe{id: id, url: u.String(), info: info, dir: dir, at: time.Now()}
	stale := m.evictProbesLocked(time.Now())
	m.mu.Unlock()
	for _, d := range stale {
		_ = os.RemoveAll(d)
	}
	return id, info, nil
}

// ProbeInfo returns a copy of a cached probe's info (nil when unknown or expired).
func (m *Manager) ProbeInfo(id string) *engine.Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.probes[id]
	if p == nil || time.Since(p.at) > m.cfg.ProbeTTL {
		return nil
	}
	info := *p.info
	return &info
}

// Toolset resolves the tools like a job does (미리보기도 같은 엔진·ffmpeg를 쓴다).
func (m *Manager) Toolset(ctx context.Context) (Toolset, error) { return m.deps.Tools(ctx) }

// evictProbesLocked drops expired probes and the oldest beyond MaxProbes; it returns their dirs.
func (m *Manager) evictProbesLocked(now time.Time) []string {
	var dirs []string
	for id, p := range m.probes {
		if now.Sub(p.at) > m.cfg.ProbeTTL {
			dirs = append(dirs, p.dir)
			delete(m.probes, id)
		}
	}
	for len(m.probes) > m.cfg.MaxProbes {
		var oldest *probe
		for _, p := range m.probes {
			if oldest == nil || p.at.Before(oldest.at) {
				oldest = p
			}
		}
		dirs = append(dirs, oldest.dir)
		delete(m.probes, oldest.id)
	}
	return dirs
}

// reuseProbe copies a cached probe's info.json into the job's work dir. It returns nil when the
// probe is unknown, expired or for another URL (the job then probes again).
func (m *Manager) reuseProbe(id, url, work string) *engine.Info {
	if id == "" {
		return nil
	}
	m.mu.Lock()
	p := m.probes[id]
	if p != nil && (p.url != url || time.Since(p.at) > m.cfg.ProbeTTL) {
		p = nil
	}
	m.mu.Unlock()
	if p == nil {
		return nil
	}
	b, err := os.ReadFile(p.info.InfoPath)
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil
	}
	dst := filepath.Join(work, "info.json")
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return nil
	}
	info := *p.info
	info.InfoPath = dst
	return &info
}

func asFailure(err error, fallback engine.Kind) *engine.Failure {
	var f *engine.Failure
	if errors.As(err, &f) {
		return f
	}
	return engine.Fail(fallback, err.Error())
}

func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid url %q", raw)
	}
	return u, nil
}

// ErrClosed is returned after Close.
var ErrClosed = errors.New("job manager closed")

// Submit validates and enqueues a URL.
func (m *Manager) Submit(rawURL string, opts Options) (string, error) {
	u, err := parseURL(rawURL)
	if err != nil {
		return "", err
	}
	id := newID()
	now := time.Now().UTC()
	j := &job{host: hostKey(u.Hostname()), snap: Snapshot{JobID: id, URL: u.String(), State: Queued, Options: opts,
		Progress: -1, CreatedAt: now, UpdatedAt: now}}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", ErrClosed
	}
	m.jobs[id] = j
	m.order = append(m.order, id)
	m.emitLocked(j, "update", true)
	m.scheduleLocked()
	m.mu.Unlock()
	return id, nil
}

// Cancel stops a queued or running job.
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok || j.snap.State.Terminal() {
		return false
	}
	if j.cancel != nil {
		j.cancel()
		return true
	}
	m.finishLocked(j, Cancelled, engine.Fail(engine.KindCancelled, ""))
	return true
}

// List returns snapshots in submission order.
func (m *Manager) List() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Snapshot, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.jobs[id].snap)
	}
	return out
}

// Stats is a cheap summary for monitoring.
func (m *Manager) Stats() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := map[State]int{}
	for _, j := range m.jobs {
		counts[j.snap.State]++
	}
	return map[string]any{"active": m.active, "queued": counts[Queued], "done": counts[Done],
		"failed": counts[Failed], "total": len(m.jobs), "subscribers": len(m.subs)}
}

// Subscribe returns a channel of events and an unsubscribe func.
//
// 매니저는 구독자 때문에 막히지 않는다: 이벤트는 구독자별 대기열에 쌓이고, 느린 구독자에게는
// 같은 작업의 진행률(update)을 최신 것으로 덮어써 전달한다. 스냅샷이 전체 상태라 정보 손실이 없고
// done/failed는 덮어쓰지 않는다.
func (m *Manager) Subscribe() (<-chan Event, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextSub
	m.nextSub++
	s := newSubscriber()
	m.subs[id] = s
	return s.out, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if s, ok := m.subs[id]; ok {
			delete(m.subs, id)
			s.close()
		}
	}
}

type subscriber struct {
	mu      sync.Mutex
	pending []Event
	notify  chan struct{}
	out     chan Event
	done    chan struct{}
	once    sync.Once
}

func newSubscriber() *subscriber {
	s := &subscriber{notify: make(chan struct{}, 1), out: make(chan Event), done: make(chan struct{})}
	go s.loop()
	return s
}

func (s *subscriber) push(ev Event) {
	s.mu.Lock()
	replaced := false
	if ev.Type == "update" {
		for i := len(s.pending) - 1; i >= 0; i-- {
			if s.pending[i].Job.JobID == ev.Job.JobID {
				if s.pending[i].Type == "update" {
					s.pending[i] = ev
					replaced = true
				}
				break
			}
		}
	}
	if !replaced {
		s.pending = append(s.pending, ev)
	}
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *subscriber) loop() {
	defer close(s.out)
	for {
		select {
		case <-s.done:
			return
		case <-s.notify:
		}
		for {
			s.mu.Lock()
			if len(s.pending) == 0 {
				s.mu.Unlock()
				break
			}
			ev := s.pending[0]
			s.pending = s.pending[1:]
			s.mu.Unlock()
			select {
			case s.out <- ev:
			case <-s.done:
				return
			}
		}
	}
}

func (s *subscriber) close() { s.once.Do(func() { close(s.done) }) }

// Close cancels everything and waits for workers.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, j := range m.jobs {
		if j.cancel != nil {
			j.cancel()
		} else if !j.snap.State.Terminal() {
			m.finishLocked(j, Cancelled, engine.Fail(engine.KindCancelled, "shutdown"))
		}
	}
	var probeDirs []string
	for id, p := range m.probes {
		probeDirs = append(probeDirs, p.dir)
		delete(m.probes, id)
	}
	m.mu.Unlock()
	for _, d := range probeDirs {
		_ = os.RemoveAll(d)
	}
	m.stop()
	m.wg.Wait()
	m.mu.Lock()
	for id, s := range m.subs {
		delete(m.subs, id)
		s.close()
	}
	m.mu.Unlock()
}

func (m *Manager) scheduleLocked() {
	for _, id := range m.order {
		if m.active >= m.cfg.MaxConcurrent || m.closed {
			return
		}
		j := m.jobs[id]
		if j.snap.State != Queued || j.cancel != nil || m.running[j.host] >= m.cfg.PerHostLimit {
			continue
		}
		ctx, cancel := context.WithCancel(m.ctx)
		j.cancel = cancel
		m.active++
		m.running[j.host]++
		m.wg.Add(1)
		go m.run(ctx, j)
	}
}

func (m *Manager) emitLocked(j *job, typ string, force bool) {
	now := time.Now()
	if !force && now.Sub(j.lastEmit) < m.cfg.ProgressEvery {
		return
	}
	j.lastEmit = now
	j.snap.UpdatedAt = now.UTC()
	ev := Event{Type: typ, Job: cloneSnap(j.snap)}
	for _, s := range m.subs {
		s.push(ev)
	}
}

func cloneSnap(s Snapshot) Snapshot {
	if s.Download != nil {
		d := *s.Download
		s.Download = &d
	}
	s.Warnings = append([]string(nil), s.Warnings...)
	s.Timings = append([]StageTiming(nil), s.Timings...)
	return s
}

func (m *Manager) finishLocked(j *job, st State, f *engine.Failure) {
	j.snap.State = st
	j.snap.Failure = f
	if st == Done {
		j.snap.Progress = 1
	}
	typ := "done"
	if st != Done {
		typ = "failed"
	}
	m.emitLocked(j, typ, true)
}

// update mutates a job under the lock and emits (throttled unless force).
func (m *Manager) update(j *job, force bool, fn func(s *Snapshot)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&j.snap)
	m.emitLocked(j, "update", force)
}

func (m *Manager) run(ctx context.Context, j *job) {
	defer m.wg.Done()
	span := m.deps.Rec.StartJob(j.snap.JobID, "job", "host", j.host, "max_height", j.snap.Options.MaxHeight)
	path, err := m.pipeline(ctx, j)

	m.mu.Lock()
	j.cancel = nil
	m.active--
	m.running[j.host]--
	var f *engine.Failure
	switch {
	case err == nil:
		j.snap.File = path
		m.finishLocked(j, Done, nil)
	case ctx.Err() != nil:
		f = engine.Fail(engine.KindCancelled, "")
		m.finishLocked(j, Cancelled, f)
	default:
		if !errors.As(err, &f) {
			f = engine.Fail(engine.KindInternal, err.Error())
		}
		m.finishLocked(j, Failed, f)
	}
	timings := j.snap.Timings
	m.scheduleLocked()
	m.mu.Unlock()

	for _, t := range timings {
		span.Set("stage_"+t.Stage+"_ms", t.MS)
	}
	if f != nil {
		span.Set("failure", string(f.Kind))
		span.End(f)
		return
	}
	span.End(nil)
}

func (m *Manager) stage(j *job, st State) {
	m.update(j, true, func(s *Snapshot) {
		s.State = st
		s.Progress = -1
	})
}

func (m *Manager) timing(j *job, stage string, d time.Duration) {
	m.mu.Lock()
	j.snap.Timings = append(j.snap.Timings, StageTiming{Stage: stage, MS: perf.MS(d)})
	m.mu.Unlock()
}

func (m *Manager) pipeline(ctx context.Context, j *job) (string, error) {
	id := j.snap.JobID
	work := filepath.Join(m.deps.WorkDir, id)
	if !m.cfg.KeepWorkDir {
		defer os.RemoveAll(work)
	}
	opts := j.snap.Options
	ts, err := m.deps.Tools(ctx)
	if err != nil {
		return "", asFailure(err, engine.KindToolMissing)
	}
	if len(ts.Warnings) > 0 {
		m.update(j, true, func(s *Snapshot) { s.Warnings = append(s.Warnings, ts.Warnings...) })
	}

	// 1) probe (또는 패널이 먼저 조회한 결과 재사용)
	m.stage(j, Probing)
	sp := m.deps.Rec.StartJob(id, "stage.probe", "engine", ts.Engine.Name(), "reused", opts.ProbeID != "")
	info := m.reuseProbe(opts.ProbeID, j.snap.URL, work)
	if info != nil {
		m.timing(j, "probe", sp.End(nil))
	} else {
		sp.Set("reused", false)
		err = m.withRetry(ctx, func() error {
			var ps perf.ProcStats
			var err error
			info, ps, err = ts.Engine.Probe(ctx, j.snap.URL, work, opts.MaxHeight)
			sp.SetProc(ps)
			return err
		})
		m.timing(j, "probe", sp.End(err))
		if err != nil {
			return "", err
		}
	}
	formatID := opts.FormatID
	if v, ok := info.Variant(opts.FormatID); ok && !opts.AudioOnly {
		info.Selected = v.Parts // 진행률 합산과 예상 크기가 실제로 받을 포맷을 따른다
	}
	if opts.AudioOnly {
		formatID = ""
		if info.BestAudio != nil {
			formatID = info.BestAudio.ID
			info.Selected = []engine.Format{*info.BestAudio}
		}
	}
	slim := *info // 작업 스냅샷은 진행률마다 전송되므로 선택지 목록은 빼고 보낸다
	slim.Variants, slim.Subtitles, slim.BestAudio, slim.Storyboards = nil, nil, nil, nil
	m.update(j, true, func(s *Snapshot) { s.Info = &slim })

	// 구간: 핸들을 붙여 받는다. HLS(조각 TS)는 시작 시각이 원본 기준이 아니라 전체를 받고 In/Out만 건다.
	rng, err := clampRange(opts.Range, info.DurationSec)
	if err != nil {
		return "", err
	}
	var section *engine.Section
	trim := false // 오디오만: 전체를 받아 WAV로 바꿀 때 자른다
	if rng != nil {
		switch {
		case opts.AudioOnly && !(info.BestAudio != nil && mp4Family(info.BestAudio.Ext)):
			// webm/opus 구간은 원래 시작 시각을 잃는다(실측 0.021s, 길이 N/A). 오디오는 작아 전체를 받는다.
			trim = true
		case segmented(info.Selected):
			m.update(j, true, func(s *Snapshot) {
				s.Warnings = append(s.Warnings, "이 형식(HLS)은 구간만 받을 수 없어 전체를 받고 In/Out으로 구간을 표시합니다.")
			})
		default:
			section = &engine.Section{Start: max(0, rng.Start-RangeHandleSec), End: rng.End + RangeHandleSec}
			if info.DurationSec > 0 {
				section.End = min(section.End, info.DurationSec)
			}
		}
	}

	// 2) download
	m.stage(j, Downloading)
	var dl *engine.DownloadResult
	err = m.withRetry(ctx, func() error {
		sp := m.deps.Rec.StartJob(id, "stage.download", "engine", ts.Engine.Name(), "expected_bytes", info.ExpectedBytes())
		var err error
		dl, err = ts.Engine.Download(ctx, engine.DownloadRequest{Info: info, MaxHeight: opts.MaxHeight, FormatID: formatID, WorkDir: work,
			Section: section, AudioOnly: opts.AudioOnly},
			func(p engine.Progress) {
				m.update(j, false, func(s *Snapshot) {
					pp := p
					s.Download = &pp
					s.Progress = p.Fraction
				})
			})
		if dl != nil {
			sp.SetProc(dl.Proc)
			sp.Set("bytes", dl.Bytes)
			if wall := dl.Proc.WallMS; wall > 0 {
				sp.Set("throughput_bps", float64(dl.Bytes)/(wall/1000))
			}
		}
		m.timing(j, "download", sp.End(err))
		return err
	})
	if err != nil {
		return "", err
	}

	// 3) analyze
	m.stage(j, Normalizing)
	sp = m.deps.Rec.StartJob(id, "stage.analyze")
	in, ps, err := ts.Prober.Probe(ctx, dl.Path)
	if err != nil {
		sp.SetProc(ps)
		sp.End(err)
		return "", engine.Fail(engine.KindMedia, "ffprobe: "+err.Error())
	}
	var vfr media.VFRReport
	if in.Video != nil && !opts.AudioOnly {
		var vps perf.ProcStats
		vfr, vps, err = ts.Prober.AnalyzeVFR(ctx, dl.Path, in.Video, 30)
		if err != nil {
			sp.End(err)
			return "", engine.Fail(engine.KindMedia, "ffprobe vfr: "+err.Error())
		}
		sp.Set("vfr_ms", vps.WallMS, "vfr_samples", vfr.Samples, "vfr", vfr.Variable)
	}
	m.timing(j, "analyze", sp.End(nil))

	// 4) plan + normalize
	encoder := ""
	plan := media.MakePlan(in, vfr, opts.Profile, "")
	if opts.AudioOnly {
		if in.Audio == nil {
			return "", engine.Fail(engine.KindMedia, "오디오 스트림이 없습니다")
		}
		plan = media.AudioPlan(in, opts.Profile)
	}
	if plan.Video == media.Transcode {
		encoder, err = ts.Normalizer.Encoder(ctx)
		if err != nil {
			return "", engine.Fail(engine.KindMedia, err.Error())
		}
		plan = media.MakePlan(in, vfr, opts.Profile, encoder)
	}
	var clip *Clip
	fileStart := 0.0 // 파일 0초가 원본의 몇 초인지(자막 시프트)
	if rng != nil {
		start := 0.0 // 전체 다운로드: 파일 타임라인 = 원본 타임라인
		if trim {
			start = max(0, rng.Start-RangeHandleSec)
			end := rng.End + RangeHandleSec
			if in.DurationSec > 0 {
				end = min(end, in.DurationSec)
			}
			plan.Trim(start, end-start)
		}
		if section != nil {
			start = streamStart(in)
			plan.AlignTo(start)
			// 구간 파일의 시작은 요청 시작(핸들 포함) 직전 키프레임이어야 한다. 아니면 타임라인이 원본 기준이 아니다.
			if start > section.Start+0.5 || start < section.Start-30 {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("구간 시작 위치를 확인하지 못했습니다(파일 시작 %.2fs, 요청 %.2fs): In/Out을 설정하지 않습니다.", start, section.Start))
				start = -1
			}
		}
		if start >= 0 {
			clip = &Clip{InSec: round3(rng.Start - start), OutSec: round3(rng.End - start)}
			fileStart = start
		}
	}
	ext := ".mp4"
	if opts.AudioOnly {
		ext = ".wav"
	}
	dest, release, err := library.ReserveTagged(m.deps.LibraryDir, info.Extractor, info.Title, info.ID, rangeTag(rng), ext)
	if err != nil {
		return "", engine.Fail(engine.KindInternal, err.Error())
	}
	defer release()
	m.update(j, true, func(s *Snapshot) {
		pl := plan
		s.Plan = &pl
		s.Warnings = append(s.Warnings, plan.Warnings...)
	})

	sp = m.deps.Rec.StartJob(id, "stage.normalize", "video", string(plan.Video), "audio", string(plan.Audio),
		"encoder", plan.Encoder, "passthrough", plan.Passthrough, "duration_sec", in.DurationSec,
		"width", width(in), "height", height(in), "in_bytes", in.SizeBytes)
	if plan.Passthrough {
		err = moveFile(dl.Path, dest)
	} else {
		var nr media.Result
		nr, err = ts.Normalizer.Run(ctx, plan, dl.Path, dest, in, func(f float64) {
			m.update(j, false, func(s *Snapshot) { s.Progress = f })
		})
		sp.SetProc(nr.Proc)
		sp.Set("speed_x", nr.Speed)
		if in.DurationSec > 0 && nr.Proc.WallMS > 0 {
			sp.Set("realtime_x", in.DurationSec/(nr.Proc.WallMS/1000))
		}
	}
	m.timing(j, "normalize", sp.End(err))
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", engine.Fail(engine.KindMedia, err.Error())
	}

	// 5) verify the result
	out, _, err := ts.Prober.Probe(ctx, dest)
	if err != nil {
		return "", engine.Fail(engine.KindMedia, "verify: "+err.Error())
	}
	if clip != nil && out.DurationSec > 0 {
		clip.OutSec = min(clip.OutSec, round3(out.DurationSec))
		clip.InSec = max(0, min(clip.InSec, clip.OutSec))
	}
	m.update(j, true, func(s *Snapshot) {
		s.Media = out
		s.Clip = clip
	})

	// 6) 자막: 별도 호출이라 실패해도 작업은 성공(경고만). 시작 위치를 모르면(clip 없음·구간) 옮길 수 없어 건너뛴다.
	if req := opts.Subtitles; req != nil && req.Lang != "" {
		var warn string
		if rng != nil && clip == nil {
			warn = "구간 위치를 몰라 자막을 맞출 수 없어 받지 않았습니다."
		} else if path, err := m.subtitles(ctx, j, ts, info, req, work, dest, fileStart, out.DurationSec); err != nil {
			warn = "자막을 받지 못했습니다: " + err.Error()
		} else {
			m.update(j, true, func(s *Snapshot) { s.SubtitleFile = path })
		}
		if warn != "" {
			m.update(j, true, func(s *Snapshot) { s.Warnings = append(s.Warnings, warn) })
		}
	}
	return dest, nil
}

// subtitles fetches one track, shifts it onto the file's timeline and writes "<file>.<lang>.srt".
func (m *Manager) subtitles(ctx context.Context, j *job, ts Toolset, info *engine.Info, req *SubtitleRequest, work, dest string, fileStart, length float64) (string, error) {
	sf, ok := ts.Engine.(engine.SubtitleFetcher)
	if !ok {
		return "", fmt.Errorf("엔진 %s는 자막을 지원하지 않습니다", ts.Engine.Name())
	}
	sp := m.deps.Rec.StartJob(j.snap.JobID, "stage.subtitles", "lang", req.Lang, "auto", req.Auto)
	src, err := sf.Subtitles(ctx, info, req.Lang, req.Auto, filepath.Join(work, "subs"))
	if err != nil {
		m.timing(j, "subtitles", sp.End(err))
		var f *engine.Failure
		if errors.As(err, &f) {
			return "", errors.New(f.Message)
		}
		return "", err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		m.timing(j, "subtitles", sp.End(err))
		return "", err
	}
	cues := subs.Shift(subs.Parse(string(b)), fileStart, length)
	sp.Set("cues", len(cues))
	if len(cues) == 0 {
		err := errors.New("이 구간에 자막이 없습니다")
		m.timing(j, "subtitles", sp.End(err))
		return "", err
	}
	path := strings.TrimSuffix(dest, filepath.Ext(dest)) + "." + library.Sanitize(req.Lang) + ".srt"
	err = os.WriteFile(path, []byte(subs.Format(cues)), 0o644)
	m.timing(j, "subtitles", sp.End(err))
	return path, err
}

// clampRange validates a requested range against the source duration (0 = unknown).
func clampRange(r *engine.Section, dur float64) (*engine.Section, error) {
	if r == nil {
		return nil, nil
	}
	c := *r
	c.Start = max(0, c.Start)
	if dur > 0 {
		c.End = min(c.End, dur)
	}
	if !(c.End > c.Start) || math.IsNaN(c.Start) || math.IsNaN(c.End) {
		return nil, engine.Fail(engine.KindInternal, fmt.Sprintf("잘못된 구간 %.3f–%.3f (길이 %.3f)", r.Start, r.End, dur))
	}
	if c.Start == 0 && dur > 0 && c.End >= dur {
		return nil, nil // 전체와 같다
	}
	return &c, nil
}

// mp4Family: containers whose section downloads keep the source start time (-copyts).
func mp4Family(ext string) bool { return ext == "m4a" || ext == "mp4" || ext == "mov" }

func segmented(fs []engine.Format) bool {
	for _, f := range fs {
		if f.Segmented() {
			return true
		}
	}
	return false
}

// streamStart is where the file starts in the source timeline: the video start, else the audio start.
func streamStart(in *media.Info) float64 {
	if in.Video != nil {
		return in.Video.StartSec
	}
	if in.Audio != nil {
		return in.Audio.StartSec
	}
	return 0
}

// rangeTag names a range in the file name: "0m05.3s-0m08.1s" (같은 영상의 여러 구간이 구분된다).
func rangeTag(r *engine.Section) string {
	if r == nil {
		return ""
	}
	f := func(x float64) string {
		m := int(x) / 60
		return fmt.Sprintf("%dm%04.1fs", m, x-float64(m*60))
	}
	return f(r.Start) + "-" + f(r.End)
}

func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// withRetry retries network failures (설계 L0: network → 즉시 재시도). Rate limits are not
// retried inside the job in V0; the failure is reported so the user sees it.
func (m *Manager) withRetry(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt <= m.cfg.NetworkRetries; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		var f *engine.Failure
		if !errors.As(err, &f) || f.Policy != engine.PolicyRetry || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return err
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// 작업 폴더와 라이브러리가 다른 볼륨이면 rename이 실패한다 → 복사 후 삭제.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".birdcage-tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func width(i *media.Info) int {
	if i.Video == nil {
		return 0
	}
	return i.Video.Width
}

func height(i *media.Info) int {
	if i.Video == nil {
		return 0
	}
	return i.Video.Height
}

// hostKey groups subdomains (www.youtube.com, m.youtube.com, youtu.be → youtube).
func hostKey(h string) string {
	h = strings.ToLower(strings.TrimPrefix(h, "www."))
	switch {
	case h == "youtu.be" || strings.HasSuffix(h, "youtube.com"):
		return "youtube"
	case strings.HasSuffix(h, "instagram.com"):
		return "instagram"
	case strings.HasSuffix(h, "tiktok.com"):
		return "tiktok"
	}
	return h
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "j" + hex.EncodeToString(b[:])
}
