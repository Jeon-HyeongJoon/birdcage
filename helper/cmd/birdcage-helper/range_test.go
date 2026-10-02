package main

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/media/mediatest"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/proc"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/tools"
)

// TestEndToEndLocalRange: 실제 yt-dlp가 로컬 HTTP(Range 지원) 파일에서 구간만 받고(--download-sections),
// Helper가 정렬 정규화 후 clip In/Out을 돌려준다. In 위치 프레임·오디오가 원본과 같아야 한다. 인터넷 불필요.
func TestEndToEndLocalRange(t *testing.T) {
	perftest.Track(t)
	tp := toolPaths(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	// GOP 2s(30fps) H.264 + 노이즈 AAC, 20s. 구간 9.3–12.1s(279번째 프레임부터).
	if _, err := proc.Run(context.Background(), proc.Spec{Path: tp[tools.FFmpeg], StderrTail: 5, Args: []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=s=320x180:r=30:d=20", "-f", "lavfi", "-i", "anoisesrc=d=20:r=48000:a=0.3:seed=3",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "60", "-keyint_min", "60", "-sc_threshold", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart", src}}); err != nil {
		t.Fatal(err)
	}
	var rangeRequests atomic.Int32
	files := http.FileServer(http.Dir(dir)) // ServeContent: Range 요청 지원
	fs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			rangeRequests.Add(1)
		}
		files.ServeHTTP(w, r)
	}))
	defer fs.Close()

	args := []string{"--yt-dlp", tp[tools.YtDlp], "--ffmpeg", tp[tools.FFmpeg], "--ffprobe", tp[tools.FFprobe]}
	h := startInProcess(t, args...)
	c := connect(t, h.ready)
	c.send(protocol.TypeHello, "h", protocol.Hello{Client: "e2e", Host: protocol.Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}})
	c.until(30*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeHelloOK })

	const a, b = 9.3, 12.1
	c.send(protocol.TypeJobCreate, "c", protocol.JobCreate{URL: fs.URL + "/clip.mp4", MaxHeight: 720, Range: &engine.Section{Start: a, End: b}})
	acc := c.until(10*time.Second, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypeJobAccepted || e.Type == protocol.TypeError
	})
	if acc.Type == protocol.TypeError {
		t.Fatalf("job.create: %s", acc.Data)
	}
	env := c.until(2*time.Minute, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypeJobDone || e.Type == protocol.TypeJobFailed
	})
	var ev protocol.JobEvent
	_ = protocol.DecodeData(env, &ev)
	j := ev.Job
	if env.Type == protocol.TypeJobFailed {
		t.Fatalf("job failed: %+v", j.Failure)
	}
	if j.Clip == nil || j.Plan == nil || !j.Plan.Aligned {
		t.Fatalf("clip %+v plan %+v warnings %v", j.Clip, j.Plan, j.Warnings)
	}
	// 핸들 1s → 구간 다운로드 8.3s부터 → 직전 키프레임 8.0s. In = 9.3 − 8.0.
	if math.Abs(j.Clip.InSec-1.3) > 0.001 || math.Abs(j.Clip.OutSec-4.1) > 0.001 {
		t.Fatalf("clip %+v, want In 1.3 Out 4.1", j.Clip)
	}
	if j.Media.DurationSec > 6 || j.Media.DurationSec < 5 { // 8.0–13.1s
		t.Fatalf("downloaded %.2fs; want only the section with handles (~5.1s)", j.Media.DurationSec)
	}
	if !strings.Contains(filepath.Base(j.File), "0m09.3s-0m12.1s") {
		t.Fatalf("file name %q lacks the range tag", filepath.Base(j.File))
	}
	if rangeRequests.Load() == 0 {
		t.Fatal("no HTTP Range request: the whole file was fetched")
	}
	ctx := context.Background()
	want, err := mediatest.FrameMD5(ctx, tp[tools.FFmpeg], src, 279)
	if err != nil {
		t.Fatal(err)
	}
	got, err := mediatest.FrameMD5(ctx, tp[tools.FFmpeg], j.File, int(math.Round(j.Clip.InSec*30)))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("frame at In differs from the source frame at the requested start")
	}
	ref, _ := mediatest.PCM(ctx, tp[tools.FFmpeg], src, a+0.5, 0.3)
	cmp, _ := mediatest.PCM(ctx, tp[tools.FFmpeg], j.File, j.Clip.InSec+0.5, 0.3)
	if lag := mediatest.AudioLag(ref, cmp, 2400); lag < -48 || lag > 48 {
		t.Fatalf("audio off by %d samples (%.2f ms)", lag, float64(lag)/48)
	}
	t.Logf("clip %+v, file %.2fs, timings %+v, range requests %d", j.Clip, j.Media.DurationSec, j.Timings, rangeRequests.Load())
}
