package preview

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/media"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/proc"
)

func TestMain(m *testing.M) { perftest.Main(m) }

func TestPeaksFromPCM(t *testing.T) {
	// 1초 무음 + 1초 최대 진폭 사각파(8kHz).
	pcm := make([]byte, 2*16000)
	for i := 8000; i < 16000; i++ {
		v := int16(32767)
		if i%2 == 0 {
			v = -32767
		}
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(v))
	}
	p := peaksFromPCM(pcm, 8000)
	if p.BucketSec != 0.01 || p.Count != 200 || p.DurationSec != 2 {
		t.Fatalf("buckets %+v", p)
	}
	vals, _ := base64.StdEncoding.DecodeString(p.Data)
	if vals[0] != 0 || vals[99] != 0 || vals[100] != 255 || vals[199] != 255 {
		t.Fatalf("values %v … %v", vals[:3], vals[198:])
	}
	// 긴 오디오는 버킷 수가 상한(20000)을 넘지 않는다.
	if long := peaksFromPCM(make([]byte, 2*8000*3600), 8000); long.Count > maxBuckets || long.Count < maxBuckets-1 {
		t.Fatalf("1h buckets %d", long.Count)
	}
}

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := &Cache{Root: t.TempDir(), CapBytes: 2500}
	mk := func(key string, size int, age time.Duration) {
		d, err := c.Dir(key)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f"), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := time.Now().Add(-age)
		_ = os.Chtimes(d, ts, ts)
	}
	mk("old", 1000, 3*time.Hour)
	mk("mid", 1000, 2*time.Hour)
	mk("new", 1000, time.Hour)
	removed := c.Evict("old") // 지금 쓰는 폴더(old)는 지우지 않는다
	if len(removed) != 1 || filepath.Base(removed[0]) != "mid" {
		t.Fatalf("removed %v", removed)
	}
	for _, k := range []string{"old", "new"} {
		if _, err := os.Stat(filepath.Join(c.Root, k)); err != nil {
			t.Fatalf("%s removed: %v", k, err)
		}
	}
}

// fakeEngine serves fixture files: audio-only requests get the m4a, others the video.
type fakeEngine struct {
	audio, video string
	mu           sync.Mutex
	calls        []engine.DownloadRequest
}

func (f *fakeEngine) Name() string { return "fake" }
func (f *fakeEngine) Probe(context.Context, string, string, int) (*engine.Info, perf.ProcStats, error) {
	return nil, perf.ProcStats{}, nil
}
func (f *fakeEngine) Download(ctx context.Context, req engine.DownloadRequest, onProgress func(engine.Progress)) (*engine.DownloadResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	src := f.video
	if req.AudioOnly {
		src = f.audio
	}
	_ = os.MkdirAll(req.WorkDir, 0o755)
	dst := filepath.Join(req.WorkDir, "media"+filepath.Ext(src))
	b, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	onProgress(engine.Progress{Status: "finished", Fraction: 1})
	return &engine.DownloadResult{Path: dst, Bytes: int64(len(b))}, os.WriteFile(dst, b, 0o644)
}

func fixtures(t *testing.T) (ffmpeg, ffprobe, audio, video, muxed string) {
	t.Helper()
	ff, err1 := exec.LookPath("ffmpeg")
	fp, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg/ffprobe not found")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		if _, err := proc.Run(context.Background(), proc.Spec{Path: ff, StderrTail: 5, Args: append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)}); err != nil {
			t.Fatal(err)
		}
	}
	audio = filepath.Join(dir, "a.m4a")
	video = filepath.Join(dir, "v.mp4")
	muxed = filepath.Join(dir, "m.mp4")
	run("-f", "lavfi", "-i", "anoisesrc=d=6:r=48000:a=0.3", "-c:a", "aac", audio)
	run("-f", "lavfi", "-i", "testsrc2=s=640x360:r=30:d=6", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", video)
	run("-f", "lavfi", "-i", "testsrc2=s=320x568:r=30:d=6", "-f", "lavfi", "-i", "sine=d=6", "-shortest", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", muxed)
	return ff, fp, audio, video, muxed
}

func TestRunStagesThenCache(t *testing.T) {
	perftest.Track(t)
	ff, fp, audio, video, _ := fixtures(t)
	eng := &fakeEngine{audio: audio, video: video}
	s := &Service{Cache: Cache{Root: t.TempDir(), CapBytes: 1 << 30}, Rec: perftest.Recorder()}
	infoPath := filepath.Join(t.TempDir(), "info.json")
	_ = os.WriteFile(infoPath, []byte(`{}`), 0o644)
	info := &engine.Info{ID: "abc", Extractor: "Youtube", DurationSec: 6, InfoPath: infoPath, BestAudio: &engine.Format{ID: "140", Ext: "m4a"}}
	tools := Tools{Engine: eng, FFmpeg: ff, Prober: &media.Prober{FFprobe: fp}}

	var states []State
	s.Run(context.Background(), "p1", info, tools, func(st State) { states = append(states, st) })
	var stages []Stage
	for _, st := range states {
		if len(stages) == 0 || stages[len(stages)-1] != st.Stage {
			stages = append(stages, st.Stage)
		}
	}
	if len(stages) != 3 || stages[0] != StageAudio || stages[1] != StageVideo || stages[2] != StageReady {
		t.Fatalf("stages %v (last %+v)", stages, states[len(states)-1])
	}
	last := states[len(states)-1]
	// AAC 디코드는 인코더 패딩만큼 길다(6s → 6.016s): 10ms 버킷 600~605개.
	if last.Peaks == nil || last.Peaks.Count < 600 || last.Peaks.Count > 605 || last.Video == nil || last.Video.Width != 640 || last.Video.Height != 360 {
		t.Fatalf("ready state %+v / %+v", last.Peaks, last.Video)
	}
	// 파형 단계에서 이미 피크가 있다(비디오보다 먼저).
	for _, st := range states {
		if st.Stage == StageVideo && st.Peaks == nil {
			t.Fatal("peaks must arrive before the video")
		}
	}
	got, _, err := (&media.Prober{FFprobe: fp}).Probe(context.Background(), last.Video.Path)
	if err != nil || got.Video.Codec != "h264" || got.Audio == nil || got.Audio.Codec != "aac" {
		t.Fatalf("preview file %+v / %+v: %v", got.Video, got.Audio, err)
	}
	if last.Storyboard == nil || len(last.Storyboard.Sprites) != 1 || last.Storyboard.IntervalSec != 0.5 {
		t.Fatalf("generated storyboard %+v", last.Storyboard)
	}
	if _, err := os.Stat(last.Storyboard.Sprites[0].URL); err != nil {
		t.Fatalf("sprite file: %v", err)
	}
	if len(eng.calls) != 2 || !eng.calls[0].AudioOnly || eng.calls[1].AudioOnly || eng.calls[1].MaxHeight != MaxHeight {
		t.Fatalf("downloads %+v", eng.calls)
	}

	// 두 번째: 캐시에서 즉시, 받지 않는다.
	states = nil
	s.Run(context.Background(), "p2", info, tools, func(st State) { states = append(states, st) })
	if len(states) != 1 || states[0].Stage != StageReady || !states[0].Cached || len(eng.calls) != 2 || states[0].ProbeID != "p2" {
		t.Fatalf("cached run %+v calls %d", states, len(eng.calls))
	}
}

func TestRunSingleFileSource(t *testing.T) {
	ff, fp, audio, _, muxed := fixtures(t)
	eng := &fakeEngine{audio: audio, video: muxed}
	s := &Service{Cache: Cache{Root: t.TempDir(), CapBytes: 1 << 30}, Rec: perftest.Recorder()}
	info := &engine.Info{ID: "x", Extractor: "Generic", DurationSec: 6, InfoPath: filepath.Join(t.TempDir(), "none.json"),
		Storyboards: []engine.Storyboard{{Width: 80}}} // 플랫폼 스토리보드가 있으면 만들지 않는다
	var last State
	s.Run(context.Background(), "p", info, Tools{Engine: eng, FFmpeg: ff, Prober: &media.Prober{FFprobe: fp}}, func(st State) { last = st })
	if last.Stage != StageReady || last.Peaks == nil || last.Video == nil || last.Video.Height != 568 || last.Storyboard != nil {
		t.Fatalf("single-file preview %+v", last)
	}
	if len(eng.calls) != 1 || eng.calls[0].AudioOnly {
		t.Fatalf("one download expected for a single-file source: %+v", eng.calls)
	}
}

func TestRunCancelledIsSilent(t *testing.T) {
	ff, fp, audio, video, _ := fixtures(t)
	s := &Service{Cache: Cache{Root: t.TempDir()}, Rec: perftest.Recorder()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var states []State
	s.Run(ctx, "p", &engine.Info{ID: "c", Extractor: "Youtube", BestAudio: &engine.Format{ID: "140"}},
		Tools{Engine: &cancelEngine{fakeEngine{audio: audio, video: video}}, FFmpeg: ff, Prober: &media.Prober{FFprobe: fp}},
		func(st State) { states = append(states, st) })
	for _, st := range states {
		if st.Stage == StageFailed {
			t.Fatalf("cancellation must not be reported as a failure: %+v", st)
		}
	}
}

type cancelEngine struct{ fakeEngine }

func (c *cancelEngine) Download(ctx context.Context, req engine.DownloadRequest, onProgress func(engine.Progress)) (*engine.DownloadResult, error) {
	return nil, engine.Fail(engine.KindCancelled, "")
}
