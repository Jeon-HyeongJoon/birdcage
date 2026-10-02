package jobs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/media"
	"birdcage/helper/internal/perf"
)

// variantEngine is fakeEngine plus a real info.json on disk and selectable variants.
type variantEngine struct {
	fakeEngine
	mu   sync.Mutex
	reqs []engine.DownloadRequest
}

func (f *variantEngine) Probe(ctx context.Context, url, workDir string, maxHeight int) (*engine.Info, perf.ProcStats, error) {
	info, ps, err := f.fakeEngine.Probe(ctx, url, workDir, maxHeight)
	if err != nil {
		return nil, ps, err
	}
	if err := os.WriteFile(info.InfoPath, []byte(`{"id":"vid1"}`), 0o644); err != nil {
		return nil, ps, err
	}
	info.Variants = []engine.Variant{
		{FormatID: "313+140", Res: 2160, VCodec: "vp09.00.51.08", SizeBytes: 9000,
			Parts: []engine.Format{{ID: "313", SizeBytes: 8000}, {ID: "140", SizeBytes: 1000}}},
		{FormatID: "137+140", Res: 1080, VCodec: "avc1.640028", SizeBytes: 3000,
			Parts: []engine.Format{{ID: "137", SizeBytes: 2000}, {ID: "140", SizeBytes: 1000}}},
	}
	info.Subtitles = []engine.Subtitle{{Lang: "en"}}
	return info, ps, nil
}

func (f *variantEngine) Download(ctx context.Context, req engine.DownloadRequest, onProgress func(engine.Progress)) (*engine.DownloadResult, error) {
	f.mu.Lock()
	r := req
	info := *req.Info
	r.Info = &info
	f.reqs = append(f.reqs, r)
	f.mu.Unlock()
	if _, err := os.Stat(req.Info.InfoPath); err != nil {
		return nil, engine.Fail(engine.KindInternal, "info.json missing in job dir: "+err.Error())
	}
	return f.fakeEngine.Download(ctx, req, onProgress)
}

func TestProbeIsReusedByJob(t *testing.T) {
	src, ffmpeg, ffprobe := fixture(t)
	eng := &variantEngine{fakeEngine: fakeEngine{fixture: src}}
	m, _ := newManager(t, eng, ffmpeg, ffprobe, Config{})
	events, unsub := m.Subscribe()
	defer unsub()

	const url = "https://www.youtube.com/watch?v=vid1"
	pid, info, err := m.Probe(context.Background(), url, 1080, media.ProfileCompat)
	if err != nil {
		t.Fatal(err)
	}
	// 변환 필요 여부는 세션 프로필 기준(compat: VP9 변환, H.264 그대로).
	if !info.Variants[0].Transcode || info.Variants[1].Transcode {
		t.Fatalf("transcode flags %+v", info.Variants)
	}
	id, err := m.Submit(url, Options{MaxHeight: 1080, Profile: media.ProfileCompat, ProbeID: pid, FormatID: "137+140"})
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := waitTerminal(t, events, id)
	if snap.State != Done {
		t.Fatalf("state %s failure %+v", snap.State, snap.Failure)
	}
	if n := eng.probes.Load(); n != 1 {
		t.Fatalf("engine probed %d times; the job must reuse the probe", n)
	}
	if len(eng.reqs) != 1 || eng.reqs[0].FormatID != "137+140" || len(eng.reqs[0].Info.Selected) != 2 || eng.reqs[0].Info.Selected[0].ID != "137" {
		t.Fatalf("download request %+v", eng.reqs)
	}
	// 작업 스냅샷에는 선택지 목록을 싣지 않는다(진행률마다 전송).
	if snap.Info == nil || snap.Info.Variants != nil || snap.Info.Subtitles != nil {
		t.Fatalf("job snapshot info %+v", snap.Info)
	}
	// 같은 조회로 두 번째 작업(예: 다른 구간)도 재추출 없이.
	id2, _ := m.Submit(url, Options{MaxHeight: 1080, Profile: media.ProfileCompat, ProbeID: pid})
	if snap, _ := waitTerminal(t, events, id2); snap.State != Done || eng.probes.Load() != 1 {
		t.Fatalf("second job state %s probes %d", snap.State, eng.probes.Load())
	}
}

func TestProbeNotReusedForOtherURLOrExpired(t *testing.T) {
	src, ffmpeg, ffprobe := fixture(t)
	eng := &variantEngine{fakeEngine: fakeEngine{fixture: src}}
	m, _ := newManager(t, eng, ffmpeg, ffprobe, Config{ProbeTTL: time.Hour})
	events, unsub := m.Subscribe()
	defer unsub()
	pid, _, err := m.Probe(context.Background(), "https://www.youtube.com/watch?v=vid1", 1080, media.ProfileCompat)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := m.Submit("https://www.youtube.com/watch?v=other", Options{MaxHeight: 1080, ProbeID: pid})
	if snap, _ := waitTerminal(t, events, id); snap.State != Done || eng.probes.Load() != 2 {
		t.Fatalf("other URL must probe again: state %s probes %d", snap.State, eng.probes.Load())
	}
	// 만료: 시간을 되돌려 TTL을 넘긴다.
	m.mu.Lock()
	m.probes[pid].at = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()
	id, _ = m.Submit("https://www.youtube.com/watch?v=vid1", Options{MaxHeight: 1080, ProbeID: pid})
	if snap, _ := waitTerminal(t, events, id); snap.State != Done || eng.probes.Load() != 3 {
		t.Fatalf("expired probe must not be reused: state %s probes %d", snap.State, eng.probes.Load())
	}
}

func TestProbeCacheEvictsAndCleansUp(t *testing.T) {
	src, ffmpeg, ffprobe := fixture(t)
	eng := &variantEngine{fakeEngine: fakeEngine{fixture: src}}
	m, _ := newManager(t, eng, ffmpeg, ffprobe, Config{MaxProbes: 2})
	var dirs []string
	for i := 0; i < 3; i++ {
		pid, _, err := m.Probe(context.Background(), "https://youtu.be/x", 720, media.ProfileCompat)
		if err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, filepath.Join(m.deps.WorkDir, "probe-"+pid))
		time.Sleep(2 * time.Millisecond) // 순서가 분명하도록
	}
	if _, err := os.Stat(dirs[0]); !os.IsNotExist(err) {
		t.Fatalf("oldest probe dir should be removed: %v", err)
	}
	m.Close()
	for _, d := range dirs[1:] {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("Close should remove probe dir %s: %v", d, err)
		}
	}
	if _, _, err := m.Probe(context.Background(), "ftp://x", 0, media.ProfileCompat); err == nil {
		t.Fatal("invalid URL accepted")
	}
}
