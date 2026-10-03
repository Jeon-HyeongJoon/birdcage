package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
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
	"birdcage/helper/internal/rangefetch"
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

// TestEndToEndLocalAudioOnlyRange: 직접 링크(오디오 전용 포맷 없음)에서 오디오만 + 구간 → 전체를 받아
// WAV로 바꿀 때 자른다. In 위치 오디오가 원본과 같아야 한다.
func TestEndToEndLocalAudioOnlyRange(t *testing.T) {
	perftest.Track(t)
	tp := toolPaths(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	if _, err := proc.Run(context.Background(), proc.Spec{Path: tp[tools.FFmpeg], StderrTail: 5, Args: []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=s=160x90:r=30:d=12", "-f", "lavfi", "-i", "anoisesrc=d=12:r=48000:a=0.3:seed=5",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart", src}}); err != nil {
		t.Fatal(err)
	}
	fs := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer fs.Close()
	h := startInProcess(t, "--yt-dlp", tp[tools.YtDlp], "--ffmpeg", tp[tools.FFmpeg], "--ffprobe", tp[tools.FFprobe])
	c := connect(t, h.ready)
	c.send(protocol.TypeHello, "h", protocol.Hello{Client: "e2e", Host: protocol.Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}})
	c.until(30*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeHelloOK })

	const a, b = 4.4, 7.0
	c.send(protocol.TypeJobCreate, "c", protocol.JobCreate{URL: fs.URL + "/clip.mp4", AudioOnly: true, Range: &engine.Section{Start: a, End: b}})
	c.until(10*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeJobAccepted })
	env := c.until(2*time.Minute, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypeJobDone || e.Type == protocol.TypeJobFailed
	})
	var ev protocol.JobEvent
	_ = protocol.DecodeData(env, &ev)
	j := ev.Job
	if env.Type == protocol.TypeJobFailed {
		t.Fatalf("job failed: %+v", j.Failure)
	}
	if filepath.Ext(j.File) != ".wav" || j.Media.Video != nil || j.Media.Audio.Codec != "pcm_s16le" || j.Media.Audio.SampleRate != 48000 {
		t.Fatalf("file %s media %+v / %+v", j.File, j.Media.Video, j.Media.Audio)
	}
	if j.Clip == nil || math.Abs(j.Clip.InSec-1.0) > 0.001 || math.Abs(j.Clip.OutSec-3.6) > 0.001 { // 3.4s부터 잘림
		t.Fatalf("clip %+v, want In 1.0 Out 3.6", j.Clip)
	}
	if math.Abs(j.Media.DurationSec-4.6) > 0.05 {
		t.Fatalf("wav %.3fs, want 4.6s (range + handles)", j.Media.DurationSec)
	}
	ctx := context.Background()
	for _, off := range []float64{0.2, 2.0} {
		ref, _ := mediatest.PCM(ctx, tp[tools.FFmpeg], src, a+off, 0.3)
		cmp, _ := mediatest.PCM(ctx, tp[tools.FFmpeg], j.File, j.Clip.InSec+off, 0.3)
		if lag := mediatest.AudioLag(ref, cmp, 2400); lag < -48 || lag > 48 {
			t.Fatalf("audio at In%+.1fs off by %.2f ms", off, float64(lag)/48)
		}
	}
	t.Logf("wav %.2fs clip %+v plan %+v", j.Media.DurationSec, j.Clip, j.Plan.Reasons)
}

// TestEndToEndLocalRangeFetch: YouTube DASH와 같은 형태(스트림별 조각 mp4, 앞에 sidx, DASH 목록)를 로컬에서 서빙하고
// 구간을 받는다. ffmpeg 구간 받기 대신 필요한 조각만 범위 요청으로 받아야 하고(서버가 보낸 바이트가 구간 분량뿐),
// In 위치 프레임·오디오가 원본과 같아야 한다.
func TestEndToEndLocalRangeFetch(t *testing.T) {
	perftest.Track(t)
	tp := toolPaths(t)
	dir := t.TempDir()
	ff := func(args ...string) {
		if _, err := proc.Run(context.Background(), proc.Spec{Path: tp[tools.FFmpeg], StderrTail: 5, Args: append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)}); err != nil {
			t.Fatal(err)
		}
	}
	frag := []string{"-movflags", "+frag_keyframe+empty_moov+global_sidx+default_base_moof"}
	src := filepath.Join(dir, "src.mp4") // 비교 기준(일반 mp4)
	ff("-f", "lavfi", "-i", "testsrc2=s=320x180:r=30:d=40", "-f", "lavfi", "-i", "anoisesrc=d=40:r=48000:a=0.3:seed=9",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "60", "-keyint_min", "60", "-sc_threshold", "0", "-pix_fmt", "yuv420p", "-c:a", "aac", src)
	ff(append([]string{"-i", src, "-map", "0:v", "-c", "copy"}, append(frag, filepath.Join(dir, "v.mp4"))...)...)
	ff(append([]string{"-i", src, "-map", "0:a", "-c", "copy", "-frag_duration", "2000000"}, append(frag, filepath.Join(dir, "a.m4a"))...)...)
	mpd := `<?xml version="1.0"?><MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT40S" minBufferTime="PT2S" profiles="urn:mpeg:dash:profile:isoff-on-demand:2011"><Period>` +
		`<AdaptationSet mimeType="video/mp4" contentType="video"><Representation id="v" codecs="avc1.42c00d" width="320" height="180" frameRate="30" bandwidth="300000"><BaseURL>v.mp4</BaseURL>` + segmentBase(t, filepath.Join(dir, "v.mp4")) + `</Representation></AdaptationSet>` +
		`<AdaptationSet mimeType="audio/mp4" contentType="audio"><Representation id="a" codecs="mp4a.40.2" audioSamplingRate="48000" bandwidth="128000"><BaseURL>a.m4a</BaseURL>` + segmentBase(t, filepath.Join(dir, "a.m4a")) + `</Representation></AdaptationSet></Period></MPD>`
	if err := os.WriteFile(filepath.Join(dir, "m.mpd"), []byte(mpd), 0o644); err != nil {
		t.Fatal(err)
	}
	vs, _ := os.Stat(filepath.Join(dir, "v.mp4"))
	as, _ := os.Stat(filepath.Join(dir, "a.m4a"))
	fullBytes := vs.Size() + as.Size()
	var served atomic.Int64
	files := http.FileServer(http.Dir(dir))
	fs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		files.ServeHTTP(&countingWriter{ResponseWriter: w, n: &served}, r)
	}))
	defer fs.Close()
	h := startInProcess(t, "--yt-dlp", tp[tools.YtDlp], "--ffmpeg", tp[tools.FFmpeg], "--ffprobe", tp[tools.FFprobe])
	c := connect(t, h.ready)
	c.send(protocol.TypeHello, "h", protocol.Hello{Client: "e2e", Host: protocol.Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}})
	c.until(30*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeHelloOK })
	c.send(protocol.TypeMediaProbe, "p", protocol.MediaProbe{URL: fs.URL + "/m.mpd"})
	var mi protocol.MediaInfo
	_ = protocol.DecodeData(c.until(time.Minute, func(e protocol.Envelope) bool { return e.Type == protocol.TypeMediaInfo }), &mi)
	if mi.Info == nil || len(mi.Info.Variants) == 0 {
		t.Fatalf("probe %+v", mi)
	}
	before := served.Load()
	const a, b = 21.3, 24.1
	c.send(protocol.TypeJobCreate, "c", protocol.JobCreate{URL: fs.URL + "/m.mpd", ProbeID: mi.ProbeID, FormatID: mi.Info.Variants[0].FormatID, Range: &engine.Section{Start: a, End: b}})
	c.until(10*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeJobAccepted })
	sawProgress := false
	env := c.until(2*time.Minute, func(e protocol.Envelope) bool {
		if e.Type == protocol.TypeJobUpdate {
			var ev protocol.JobEvent
			_ = protocol.DecodeData(e, &ev)
			if d := ev.Job.Download; d != nil && d.Fraction > 0 && d.TotalBytes > 0 {
				sawProgress = true
			}
		}
		return e.Type == protocol.TypeJobDone || e.Type == protocol.TypeJobFailed
	})
	var ev protocol.JobEvent
	_ = protocol.DecodeData(env, &ev)
	j := ev.Job
	if env.Type == protocol.TypeJobFailed || j.Clip == nil {
		t.Fatalf("job %+v clip %+v warnings %v", j.Failure, j.Clip, j.Warnings)
	}
	got := served.Load() - before
	if got > fullBytes/4 {
		t.Fatalf("server sent %d of %d bytes: should be the section's fragments only", got, fullBytes)
	}
	if !sawProgress {
		t.Fatal("no byte progress reported")
	}
	if math.Abs(j.Clip.InSec-1.3) > 0.001 { // 핸들 1s → 20.3s → 조각 시작 20.0s
		t.Fatalf("clip %+v, want In 1.3", j.Clip)
	}
	ctx := context.Background()
	want, _ := mediatest.FrameMD5(ctx, tp[tools.FFmpeg], src, int(math.Round(a*30)))
	frame, _ := mediatest.FrameMD5(ctx, tp[tools.FFmpeg], j.File, int(math.Round(j.Clip.InSec*30)))
	if frame != want || want == "" {
		t.Fatalf("frame at In differs (clip %+v)", j.Clip)
	}
	// 오디오 기준은 실제로 서빙한 a.m4a를 처음부터 읽은 것이다(복사하면서 원본의 프라이밍 편집 목록을 잃어
	// src.mp4보다 21ms 늦다 — YouTube 실측에서는 −0.04ms).
	for _, off := range []float64{0.5, 2.0, -0.6} {
		ref, _ := mediatest.PCM(ctx, tp[tools.FFmpeg], filepath.Join(dir, "a.m4a"), a+off, 0.3)
		cmp, _ := mediatest.PCM(ctx, tp[tools.FFmpeg], j.File, j.Clip.InSec+off, 0.3)
		if lag := mediatest.AudioLag(ref, cmp, 2400); lag < -48 || lag > 48 {
			t.Fatalf("audio at In%+.1fs off by %.2f ms", off, float64(lag)/48)
		}
	}
	t.Logf("served %d of %d bytes, clip %+v, timings %+v", got, fullBytes, j.Clip, j.Timings)
}

// segmentBase builds the DASH SegmentBase element (init + sidx byte ranges) for a fragmented file.
func segmentBase(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := rangefetch.ParseIndex(b[:min(len(b), 64<<10)])
	if err != nil {
		t.Fatal(err)
	}
	sidxEnd := ix.InitEnd + int64(binary.BigEndian.Uint32(b[ix.InitEnd:]))
	return fmt.Sprintf(`<SegmentBase indexRange="%d-%d"><Initialization range="0-%d"/></SegmentBase>`, ix.InitEnd, sidxEnd-1, ix.InitEnd-1)
}

type countingWriter struct {
	http.ResponseWriter
	n *atomic.Int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	k, err := c.ResponseWriter.Write(p)
	c.n.Add(int64(k))
	return k, err
}
