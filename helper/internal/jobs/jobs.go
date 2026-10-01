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
}

// StageTiming is the wall time of one pipeline stage.
type StageTiming struct {
	Stage string  `json:"stage"`
	MS    float64 `json:"ms"`
}

// Snapshot is the externally visible job state (sent to panels as-is).
type Snapshot struct {
	JobID     string           `json:"jobId"`
	URL       string           `json:"url"`
	State     State            `json:"state"`
	Options   Options          `json:"options"`
	Progress  float64          `json:"progress"` // stage progress 0..1, -1 unknown
	Download  *engine.Progress `json:"download,omitempty"`
	Info      *engine.Info     `json:"info,omitempty"`
	Media     *media.Info      `json:"media,omitempty"` // final file
	Plan      *media.Plan      `json:"plan,omitempty"`
	File      string           `json:"file,omitempty"`
	Failure   *engine.Failure  `json:"failure,omitempty"`
	Warnings  []string         `json:"warnings,omitempty"`
	Timings   []StageTiming    `json:"timings,omitempty"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
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
}

func (c *Config) defaults() {
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

	mu      sync.Mutex
	jobs    map[string]*job
	order   []string // submission order
	running map[string]int
	active  int
	subs    map[int]*subscriber
	nextSub int
	closed  bool
	wg      sync.WaitGroup
	ctx     context.Context
	stop    context.CancelFunc
}

// New creates a manager.
func New(cfg Config, deps Deps) *Manager {
	cfg.defaults()
	if deps.Log == nil {
		deps.Log = slog.New(slog.DiscardHandler)
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Manager{cfg: cfg, deps: deps, jobs: map[string]*job{}, running: map[string]int{},
		subs: map[int]*subscriber{}, ctx: ctx, stop: stop}
}

// ErrClosed is returned after Close.
var ErrClosed = errors.New("job manager closed")

// Submit validates and enqueues a URL.
func (m *Manager) Submit(rawURL string, opts Options) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid url %q", rawURL)
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
	m.mu.Unlock()
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
		var f *engine.Failure
		if errors.As(err, &f) {
			return "", f
		}
		return "", engine.Fail(engine.KindToolMissing, err.Error())
	}
	if len(ts.Warnings) > 0 {
		m.update(j, true, func(s *Snapshot) { s.Warnings = append(s.Warnings, ts.Warnings...) })
	}

	// 1) probe
	m.stage(j, Probing)
	var info *engine.Info
	err = m.withRetry(ctx, func() error {
		sp := m.deps.Rec.StartJob(id, "stage.probe", "engine", ts.Engine.Name())
		var ps perf.ProcStats
		var err error
		info, ps, err = ts.Engine.Probe(ctx, j.snap.URL, work, opts.MaxHeight)
		sp.SetProc(ps)
		m.timing(j, "probe", sp.End(err))
		return err
	})
	if err != nil {
		return "", err
	}
	m.update(j, true, func(s *Snapshot) { s.Info = info })

	// 2) download
	m.stage(j, Downloading)
	var dl *engine.DownloadResult
	err = m.withRetry(ctx, func() error {
		sp := m.deps.Rec.StartJob(id, "stage.download", "engine", ts.Engine.Name(), "expected_bytes", info.ExpectedBytes())
		var err error
		dl, err = ts.Engine.Download(ctx, engine.DownloadRequest{Info: info, MaxHeight: opts.MaxHeight, WorkDir: work},
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
	sp := m.deps.Rec.StartJob(id, "stage.analyze")
	in, ps, err := ts.Prober.Probe(ctx, dl.Path)
	if err != nil {
		sp.SetProc(ps)
		sp.End(err)
		return "", engine.Fail(engine.KindMedia, "ffprobe: "+err.Error())
	}
	var vfr media.VFRReport
	if in.Video != nil {
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
	if plan.Video == media.Transcode {
		encoder, err = ts.Normalizer.Encoder(ctx)
		if err != nil {
			return "", engine.Fail(engine.KindMedia, err.Error())
		}
		plan = media.MakePlan(in, vfr, opts.Profile, encoder)
	}
	dest, release, err := library.Reserve(m.deps.LibraryDir, info.Extractor, info.Title, info.ID, ".mp4")
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
	m.update(j, true, func(s *Snapshot) { s.Media = out })
	return dest, nil
}

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
