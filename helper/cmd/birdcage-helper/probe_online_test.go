package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/jobs"
	"birdcage/helper/internal/media/mediatest"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/tools"
)

// TestEndToEndOnlineProbe: 실제 YouTube 링크를 조회(media.probe)하고, 고른 변형을 probeId로 받아
// 다운로드가 재추출 없이 시작되는지 본다(G-e2e: -run TestEndToEndOnline). BIRDCAGE_E2E=1일 때만.
func TestEndToEndOnlineProbe(t *testing.T) {
	if os.Getenv("BIRDCAGE_E2E") != "1" {
		t.Skip("set BIRDCAGE_E2E=1 to probe real platforms")
	}
	perftest.Track(t)
	tp := toolPaths(t)
	args := []string{"--yt-dlp", tp[tools.YtDlp], "--ffmpeg", tp[tools.FFmpeg], "--ffprobe", tp[tools.FFprobe]}
	if d := tp[tools.Deno]; d != "" {
		args = append(args, "--deno", d)
	}
	h := startInProcess(t, args...)
	c := connect(t, h.ready)
	c.send(protocol.TypeHello, "h", protocol.Hello{Client: "e2e", Host: protocol.Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}})
	c.until(30*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeHelloOK })

	const url = "https://www.youtube.com/watch?v=jNQXAC9IVRw"
	t0 := time.Now()
	c.send(protocol.TypeMediaProbe, "p", protocol.MediaProbe{URL: url, MaxHeight: 1080})
	env := c.until(2*time.Minute, func(e protocol.Envelope) bool { return e.Type == protocol.TypeMediaInfo && e.ID == "p" })
	probeMS := float64(time.Since(t0).Microseconds()) / 1000
	var mi protocol.MediaInfo
	if err := protocol.DecodeData(env, &mi); err != nil {
		t.Fatal(err)
	}
	if mi.Failure != nil || mi.Info == nil || mi.ProbeID == "" {
		t.Fatalf("probe failed: %+v", mi.Failure)
	}
	if len(mi.Info.Variants) == 0 || mi.Info.Thumbnail == "" || mi.Info.DurationSec <= 0 {
		t.Fatalf("probe info incomplete: variants=%d thumb=%q dur=%v", len(mi.Info.Variants), mi.Info.Thumbnail, mi.Info.DurationSec)
	}
	// 가장 작은 H.264 변형을 고른다(다운로드 시간 최소화).
	pick := mi.Info.Variants[len(mi.Info.Variants)-1]
	for _, v := range mi.Info.Variants {
		if !v.Transcode && v.Res <= pick.Res {
			pick = v
		}
	}
	t.Logf("probe %0.fms: %d variants, picked %s (%dp %s, %d bytes), subtitles %d", probeMS, len(mi.Info.Variants), pick.FormatID, pick.Res, pick.VCodec, pick.SizeBytes, len(mi.Info.Subtitles))

	c.send(protocol.TypeJobCreate, "c", protocol.JobCreate{URL: url, MaxHeight: pick.Res, ProbeID: mi.ProbeID, FormatID: pick.FormatID})
	acc := c.until(10*time.Second, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypeJobAccepted || e.Type == protocol.TypeError
	})
	if acc.Type == protocol.TypeError {
		t.Fatalf("job.create error: %s", acc.Data)
	}
	var a protocol.JobAccepted
	_ = protocol.DecodeData(acc, &a)
	done := c.until(5*time.Minute, func(e protocol.Envelope) bool {
		if e.Type != protocol.TypeJobDone && e.Type != protocol.TypeJobFailed {
			return false
		}
		var ev protocol.JobEvent
		_ = protocol.DecodeData(e, &ev)
		return ev.Job.JobID == a.JobID
	})
	var ev protocol.JobEvent
	_ = protocol.DecodeData(done, &ev)
	if done.Type == protocol.TypeJobFailed {
		t.Fatalf("job failed: %+v", ev.Job.Failure)
	}
	for _, st := range ev.Job.Timings {
		if st.Stage == "probe" && st.MS > 100 {
			t.Fatalf("job re-extracted (probe stage %.0fms); the probe must be reused", st.MS)
		}
	}
	var ids []string
	for _, f := range ev.Job.Info.Selected {
		ids = append(ids, f.ID)
	}
	if strings.Join(ids, "+") != pick.FormatID {
		t.Fatalf("selected %v, want variant %s", ids, pick.FormatID)
	}
	t.Logf("job timings %+v file=%s", ev.Job.Timings, ev.Job.File)
}

// TestEndToEndOnlineRange: 실제 YouTube DASH(비디오·오디오 분리)에서 구간만 받아 전체 다운로드와 비교한다.
// In 프레임이 전체 파일의 같은 시점 프레임과 같고, 오디오가 1ms 이내여야 한다.
func TestEndToEndOnlineRange(t *testing.T) {
	if os.Getenv("BIRDCAGE_E2E") != "1" {
		t.Skip("set BIRDCAGE_E2E=1 to download from real platforms")
	}
	perftest.Track(t)
	tp := toolPaths(t)
	args := []string{"--yt-dlp", tp[tools.YtDlp], "--ffmpeg", tp[tools.FFmpeg], "--ffprobe", tp[tools.FFprobe]}
	if d := tp[tools.Deno]; d != "" {
		args = append(args, "--deno", d)
	}
	h := startInProcess(t, args...)
	c := connect(t, h.ready)
	c.send(protocol.TypeHello, "h", protocol.Hello{Client: "e2e", Host: protocol.Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}})
	c.until(30*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeHelloOK })

	const url = "https://www.youtube.com/watch?v=jNQXAC9IVRw" // 15fps, 19s
	full := jobResult(t, c, url, 360, 5*time.Minute).Job
	const a, b = 7.25, 12.5
	c.send(protocol.TypeJobCreate, "r", protocol.JobCreate{URL: url, MaxHeight: 360, Range: &engine.Section{Start: a, End: b}})
	c.until(10*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeJobAccepted })
	env := c.until(5*time.Minute, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypeJobDone || e.Type == protocol.TypeJobFailed
	})
	var ev protocol.JobEvent
	_ = protocol.DecodeData(env, &ev)
	j := ev.Job
	if env.Type == protocol.TypeJobFailed || j.Clip == nil {
		t.Fatalf("range job: %+v clip %+v warnings %v", j.Failure, j.Clip, j.Warnings)
	}
	fps := full.Media.Video.FPS
	ctx := context.Background()
	want, err := mediatest.FrameMD5(ctx, tp[tools.FFmpeg], full.File, int(math.Ceil(a*fps-1e-6)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := mediatest.FrameMD5(ctx, tp[tools.FFmpeg], j.File, int(math.Ceil(j.Clip.InSec*fps-1e-6)))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("frame at In (%.3fs) differs from the full download at %.2fs", j.Clip.InSec, a)
	}
	for _, off := range []float64{0.5, 3.0} {
		ref, _ := mediatest.PCM(ctx, tp[tools.FFmpeg], full.File, a+off, 0.3)
		cmp, _ := mediatest.PCM(ctx, tp[tools.FFmpeg], j.File, j.Clip.InSec+off, 0.3)
		lag := mediatest.AudioLag(ref, cmp, 2400)
		t.Logf("audio at In%+.1fs: lag %d samples (%.2f ms)", off, lag, float64(lag)/48)
		if lag < -48 || lag > 48 {
			t.Fatalf("audio off by %.2f ms", float64(lag)/48)
		}
	}
	t.Logf("clip %+v (file %.2fs vs full %.2fs), timings %+v", j.Clip, j.Media.DurationSec, full.Media.DurationSec, j.Timings)
}

// TestEndToEndOnlineAudioAndSubtitles: YouTube 오디오만(m4a 구간 경로) + 수동 영어 자막 사이드카.
func TestEndToEndOnlineAudioAndSubtitles(t *testing.T) {
	if os.Getenv("BIRDCAGE_E2E") != "1" {
		t.Skip("set BIRDCAGE_E2E=1 to download from real platforms")
	}
	perftest.Track(t)
	tp := toolPaths(t)
	args := []string{"--yt-dlp", tp[tools.YtDlp], "--ffmpeg", tp[tools.FFmpeg], "--ffprobe", tp[tools.FFprobe]}
	if d := tp[tools.Deno]; d != "" {
		args = append(args, "--deno", d)
	}
	h := startInProcess(t, args...)
	c := connect(t, h.ready)
	c.send(protocol.TypeHello, "h", protocol.Hello{Client: "e2e", Host: protocol.Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}})
	c.until(30*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeHelloOK })

	const url = "https://www.youtube.com/watch?v=jNQXAC9IVRw"
	c.send(protocol.TypeJobCreate, "a", protocol.JobCreate{URL: url, AudioOnly: true, Range: &engine.Section{Start: 3, End: 12},
		Subtitles: &jobs.SubtitleRequest{Lang: "en"}})
	c.until(10*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeJobAccepted })
	env := c.until(5*time.Minute, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypeJobDone || e.Type == protocol.TypeJobFailed
	})
	var ev protocol.JobEvent
	_ = protocol.DecodeData(env, &ev)
	j := ev.Job
	if env.Type == protocol.TypeJobFailed {
		t.Fatalf("job failed: %+v", j.Failure)
	}
	if filepath.Ext(j.File) != ".wav" || j.Clip == nil || j.Plan == nil || !j.Plan.Aligned {
		t.Fatalf("file %s clip %+v plan %+v", j.File, j.Clip, j.Plan)
	}
	if j.SubtitleFile == "" {
		t.Fatalf("no subtitle sidecar, warnings %v", j.Warnings)
	}
	b, err := os.ReadFile(j.SubtitleFile)
	if err != nil || !strings.Contains(string(b), " --> ") {
		t.Fatalf("subtitle file %q: %v", b, err)
	}
	t.Logf("wav %.2fs clip %+v subtitle %s (%d bytes) timings %+v", j.Media.DurationSec, j.Clip, filepath.Base(j.SubtitleFile), len(b), j.Timings)
}
