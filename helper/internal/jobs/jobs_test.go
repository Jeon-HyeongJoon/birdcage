package jobs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/media"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/proc"
)

func TestMain(m *testing.M) { perftest.Main(m) }

// fakeEngine simulates yt-dlp: Probe returns fixed info, Download copies a fixture.
type fakeEngine struct {
	fixture   string
	probeErr  func(call int) error
	block     chan struct{} // if set, Download waits on it (or ctx)
	probes    atomic.Int32
	inFlight  atomic.Int32
	maxFlight atomic.Int32
}

func (f *fakeEngine) Name() string { return "fake" }

func (f *fakeEngine) Probe(ctx context.Context, url, workDir string, maxHeight int) (*engine.Info, perf.ProcStats, error) {
	n := int(f.probes.Add(1))
	if f.probeErr != nil {
		if err := f.probeErr(n); err != nil {
			return nil, perf.ProcStats{WallMS: 1}, err
		}
	}
	_ = os.MkdirAll(workDir, 0o755)
	return &engine.Info{ID: "vid1", Title: "Test: clip/1", Extractor: "Youtube", WebpageURL: url, DurationSec: 2,
		InfoPath: filepath.Join(workDir, "info.json"), Selected: []engine.Format{{ID: "18", SizeBytes: 1000}}}, perf.ProcStats{WallMS: 1}, nil
}

func (f *fakeEngine) Download(ctx context.Context, req engine.DownloadRequest, onProgress func(engine.Progress)) (*engine.DownloadResult, error) {
	cur := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		m := f.maxFlight.Load()
		if cur <= m || f.maxFlight.CompareAndSwap(m, cur) {
			break
		}
	}
	for i := 1; i <= 5; i++ {
		onProgress(engine.Progress{Status: "downloading", DownloadedBytes: int64(i * 200), TotalBytes: 1000, Fraction: float64(i) / 5, FormatID: "18"})
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, engine.Fail(engine.KindCancelled, "")
		}
	}
	dst := filepath.Join(req.WorkDir, "media.mp4")
	b, err := os.ReadFile(f.fixture)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return nil, err
	}
	return &engine.DownloadResult{Path: dst, Bytes: int64(len(b)), Proc: perf.ProcStats{WallMS: 5}}, nil
}

var (
	fixtureOnce sync.Once
	fixturePath string
	fixtureErr  error
)

// fixture creates a 2s VP9/Opus webm (needs transcoding) once per test binary.
func fixture(t *testing.T) (string, string, string) {
	t.Helper()
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg/ffprobe not found")
	}
	fixtureOnce.Do(func() {
		dir, err := os.MkdirTemp("", "birdcage-jobs-fixture")
		if err != nil {
			fixtureErr = err
			return
		}
		fixturePath = filepath.Join(dir, "src.webm")
		_, fixtureErr = proc.Run(context.Background(), proc.Spec{Path: ffmpeg, StderrTail: 5, Args: []string{"-hide_banner", "-loglevel", "error", "-y",
			"-f", "lavfi", "-i", "testsrc2=s=320x240:r=30:d=2", "-f", "lavfi", "-i", "sine=r=48000:d=2",
			"-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-b:v", "200k", "-c:a", "libopus", "-shortest", fixturePath}})
	})
	if fixtureErr != nil {
		t.Fatal(fixtureErr)
	}
	return fixturePath, ffmpeg, ffprobe
}

func newManager(t *testing.T, eng engine.Engine, ffmpeg, ffprobe string, cfg Config) (*Manager, string) {
	t.Helper()
	lib := t.TempDir()
	ts := Toolset{Engine: eng, Prober: &media.Prober{FFprobe: ffprobe}, Normalizer: &media.Normalizer{FFmpeg: ffmpeg}}
	m := New(cfg, Deps{
		Tools:      func(context.Context) (Toolset, error) { return ts, nil },
		LibraryDir: lib, WorkDir: t.TempDir(), Rec: perftest.Recorder(),
	})
	t.Cleanup(m.Close)
	return m, lib
}

func waitTerminal(t *testing.T, ch <-chan Event, id string) (Snapshot, []State) {
	t.Helper()
	var states []State
	timeout := time.After(30 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Job.JobID != id {
				continue
			}
			if len(states) == 0 || states[len(states)-1] != ev.Job.State {
				states = append(states, ev.Job.State)
			}
			if ev.Job.State.Terminal() {
				return ev.Job, states
			}
		case <-timeout:
			t.Fatalf("job %s did not finish; states %v", id, states)
		}
	}
}

// waitAll collects events until every id is terminal (events for other ids are kept, not dropped).
func waitAll(t *testing.T, ch <-chan Event, ids ...string) map[string]Snapshot {
	t.Helper()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := map[string]Snapshot{}
	timeout := time.After(30 * time.Second)
	for len(out) < len(want) {
		select {
		case ev := <-ch:
			if want[ev.Job.JobID] && ev.Job.State.Terminal() {
				out[ev.Job.JobID] = ev.Job
			}
		case <-timeout:
			t.Fatalf("jobs did not finish: got %d of %d", len(out), len(want))
		}
	}
	return out
}

func TestPipelineHappyPath(t *testing.T) {
	perftest.Track(t)
	src, ffmpeg, ffprobe := fixture(t)
	m, lib := newManager(t, &fakeEngine{fixture: src}, ffmpeg, ffprobe, Config{})
	ch, unsub := m.Subscribe()
	defer unsub()

	id, err := m.Submit("https://www.youtube.com/watch?v=vid1", Options{MaxHeight: 720, Profile: media.ProfileCompat})
	if err != nil {
		t.Fatal(err)
	}
	snap, states := waitTerminal(t, ch, id)
	if snap.State != Done {
		t.Fatalf("state %s failure %+v", snap.State, snap.Failure)
	}
	// 느린 구독자에게는 중간 update가 최신 스냅샷으로 합쳐질 수 있다(설계). 그래서 모든 상태가
	// 보인다고 가정하지 않고, 보인 상태들이 파이프라인 순서를 지키며 done으로 끝나는지만 본다.
	order := map[State]int{Queued: 0, Probing: 1, Downloading: 2, Normalizing: 3, Done: 4}
	for i := 1; i < len(states); i++ {
		if order[states[i]] <= order[states[i-1]] {
			t.Fatalf("states out of order: %v", states)
		}
	}
	if states[len(states)-1] != Done {
		t.Fatalf("states %v must end with done", states)
	}
	if filepath.Dir(snap.File) != filepath.Join(lib, "Youtube") || filepath.Base(snap.File) != "Test_ clip_1 [vid1].mp4" {
		t.Fatalf("file %s", snap.File)
	}
	if snap.Media == nil || snap.Media.Video.Codec != "h264" || snap.Media.Audio.Codec != "aac" || snap.Plan.Video != media.Transcode {
		t.Fatalf("media %+v plan %+v", snap.Media, snap.Plan)
	}
	if len(snap.Timings) != 4 {
		t.Fatalf("timings %+v", snap.Timings)
	}
}

func TestPerHostLimitAndConcurrency(t *testing.T) {
	perftest.Track(t)
	src, ffmpeg, ffprobe := fixture(t)
	eng := &fakeEngine{fixture: src, block: make(chan struct{})}
	m, _ := newManager(t, eng, ffmpeg, ffprobe, Config{MaxConcurrent: 3})
	ch, unsub := m.Subscribe()
	defer unsub()

	a, _ := m.Submit("https://youtu.be/a", Options{Profile: media.ProfileCompat})
	b, _ := m.Submit("https://www.youtube.com/watch?v=b", Options{Profile: media.ProfileCompat}) // 같은 사이트 → 대기
	c, _ := m.Submit("https://www.tiktok.com/@x/video/1", Options{Profile: media.ProfileCompat}) // 다른 사이트 → 동시 실행

	deadline := time.Now().Add(5 * time.Second)
	for eng.inFlight.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := eng.inFlight.Load(); got != 2 {
		t.Fatalf("in flight = %d, want 2 (youtube 1 + tiktok 1)", got)
	}
	for _, s := range m.List() {
		if s.JobID == b && s.State != Queued {
			t.Fatalf("second youtube job should wait, state %s", s.State)
		}
	}
	close(eng.block)
	for id, s := range waitAll(t, ch, a, b, c) {
		if s.State != Done {
			t.Fatalf("%s: %s %+v", id, s.State, s.Failure)
		}
	}
	if eng.maxFlight.Load() > 2 {
		t.Fatalf("max in flight %d", eng.maxFlight.Load())
	}
}

func TestCancelRunningJob(t *testing.T) {
	eng := &fakeEngine{block: make(chan struct{})}
	m, _ := newManager(t, eng, "", "", Config{})
	ch, unsub := m.Subscribe()
	defer unsub()
	id, _ := m.Submit("https://youtu.be/a", Options{})
	for eng.inFlight.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if !m.Cancel(id) {
		t.Fatal("cancel returned false")
	}
	s, _ := waitTerminal(t, ch, id)
	if s.State != Cancelled || s.Failure.Kind != engine.KindCancelled {
		t.Fatalf("state %s %+v", s.State, s.Failure)
	}
	if m.Cancel(id) {
		t.Fatal("cancelling a finished job must return false")
	}
}

func TestFailureKindsAndNetworkRetry(t *testing.T) {
	src, ffmpeg, ffprobe := fixture(t)

	priv := &fakeEngine{fixture: src, probeErr: func(int) error { return engine.Fail(engine.KindPrivate, "ERROR: Private video") }}
	m, _ := newManager(t, priv, ffmpeg, ffprobe, Config{NetworkRetries: 1})
	ch, unsub := m.Subscribe()
	id, _ := m.Submit("https://youtu.be/p", Options{Profile: media.ProfileCompat})
	s, _ := waitTerminal(t, ch, id)
	unsub()
	if s.State != Failed || s.Failure.Kind != engine.KindPrivate || priv.probes.Load() != 1 {
		t.Fatalf("private: %s %+v probes=%d", s.State, s.Failure, priv.probes.Load())
	}

	flaky := &fakeEngine{fixture: src, probeErr: func(n int) error {
		if n == 1 {
			return engine.Fail(engine.KindNetwork, "timed out")
		}
		return nil
	}}
	m2, _ := newManager(t, flaky, ffmpeg, ffprobe, Config{NetworkRetries: 1})
	ch2, unsub2 := m2.Subscribe()
	defer unsub2()
	id2, _ := m2.Submit("https://youtu.be/n", Options{Profile: media.ProfileCompat})
	s2, _ := waitTerminal(t, ch2, id2)
	if s2.State != Done || flaky.probes.Load() != 2 {
		t.Fatalf("network retry: %s %+v probes=%d", s2.State, s2.Failure, flaky.probes.Load())
	}
}

func TestToolMissing(t *testing.T) {
	m := New(Config{}, Deps{
		Tools: func(context.Context) (Toolset, error) {
			return Toolset{}, engine.Fail(engine.KindToolMissing, "yt-dlp")
		},
		WorkDir: t.TempDir(), LibraryDir: t.TempDir(),
	})
	defer m.Close()
	ch, unsub := m.Subscribe()
	defer unsub()
	id, _ := m.Submit("https://youtu.be/x", Options{})
	s, _ := waitTerminal(t, ch, id)
	if s.Failure == nil || s.Failure.Kind != engine.KindToolMissing || s.Failure.Policy != engine.PolicyAskUser {
		t.Fatalf("%+v", s.Failure)
	}
}

// 이벤트를 읽지 않는 구독자가 있어도 작업이 끝까지 진행돼야 한다(매니저가 막히면 안 됨).
func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	perftest.Track(t)
	src, ffmpeg, ffprobe := fixture(t)
	m, _ := newManager(t, &fakeEngine{fixture: src}, ffmpeg, ffprobe, Config{ProgressEvery: time.Nanosecond})
	_, unsubIdle := m.Subscribe() // 읽지 않음
	defer unsubIdle()
	id, _ := m.Submit("https://youtu.be/s", Options{Profile: media.ProfileCompat})
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range m.List() {
			if s.JobID == id && s.State.Terminal() {
				if s.State != Done {
					t.Fatalf("state %s %+v", s.State, s.Failure)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job blocked by an idle subscriber")
}

func TestSubmitRejectsBadURL(t *testing.T) {
	m := New(Config{}, Deps{Tools: func(context.Context) (Toolset, error) { return Toolset{}, errors.New("unused") }})
	defer m.Close()
	for _, u := range []string{"", "ftp://x/y", "javascript:alert(1)", "notaurl"} {
		if _, err := m.Submit(u, Options{}); err == nil {
			t.Errorf("accepted %q", u)
		}
	}
}

func TestHostKey(t *testing.T) {
	for in, want := range map[string]string{"www.youtube.com": "youtube", "m.youtube.com": "youtube", "youtu.be": "youtube",
		"www.instagram.com": "instagram", "vm.tiktok.com": "tiktok", "vimeo.com": "vimeo.com"} {
		if got := hostKey(in); got != want {
			t.Errorf("hostKey(%s)=%s", in, got)
		}
	}
}
