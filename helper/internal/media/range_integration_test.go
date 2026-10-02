package media

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"birdcage/helper/internal/media/mediatest"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/proc"
)

// 구간 정확도: yt-dlp 구간 다운로드와 같은 형태(비디오·오디오를 따로 탐색, -copyts로 원래 타임라인 유지)의
// 파일을 만들고 AlignTo 정규화 뒤 In 위치의 프레임·오디오가 원본과 일치하는지 본다(docs/decisions/0010).
func TestSectionAlignmentExact(t *testing.T) {
	perftest.Track(t)
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg/ffprobe not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir := t.TempDir()
	ff := func(args ...string) {
		t.Helper()
		if _, err := proc.Run(ctx, proc.Spec{Path: ffmpeg, StderrTail: 10, Args: append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)}); err != nil {
			t.Fatal(err)
		}
	}
	// 원본(DASH처럼 분리): H.264 GOP 2s(30fps) 비디오 전용 + AAC 노이즈 오디오 전용, 그리고 VP9 비디오 전용.
	src := filepath.Join(dir, "src.mp4")
	ff("-f", "lavfi", "-i", "testsrc2=s=320x180:r=30:d=12", "-f", "lavfi", "-i", "anoisesrc=d=12:r=48000:a=0.3:seed=7",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "60", "-keyint_min", "60", "-sc_threshold", "0", "-bf", "2", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "192k", src)
	v := filepath.Join(dir, "v.mp4")
	a := filepath.Join(dir, "a.m4a")
	vp9 := filepath.Join(dir, "v.webm")
	ff("-i", src, "-map", "0:v", "-c", "copy", v)
	ff("-i", src, "-map", "0:a", "-c", "copy", a)
	ff("-i", src, "-map", "0:v", "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-g", "60", "-b:v", "300k", vp9)

	const reqStart, reqEnd, handle = 5.3, 8.1, 1.0 // 요청 구간(원본 초). 5.3s = 159번째 프레임
	secStart := reqStart - handle
	cases := []struct {
		name       string
		video      string
		audioSS    float64 // 오디오 입력 탐색 위치: S(요청 지점) 또는 더 앞(webm/opus처럼 처음부터)
		transcode  bool
		wantVStart float64
	}{
		{"h264 copy, audio later than video", v, secStart, false, 4.0},
		{"h264 copy, audio earlier than video", v, secStart - 1.5, false, 4.0},
		{"vp9 transcode, audio later", vp9, secStart, true, 4.0},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sec := filepath.Join(dir, "sec"+strconv.Itoa(i)+".mp4")
			dur := reqEnd + handle - secStart
			// yt-dlp FFmpegFD와 같은 모양: 입력별 -ss/-t, 스트림 복사, 출력 -copyts(--downloader-args ffmpeg_o:-copyts).
			ff("-ss", f3(secStart), "-t", f3(dur), "-i", c.video, "-ss", f3(c.audioSS), "-t", f3(dur+secStart-c.audioSS), "-i", a,
				"-map", "0:v:0", "-map", "1:a:0", "-c", "copy", "-copyts", "-f", "mp4", sec)
			p := &Prober{FFprobe: ffprobe}
			in, _, err := p.Probe(ctx, sec)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(in.Video.StartSec-c.wantVStart) > 0.001 {
				t.Fatalf("section video start %.3f, want keyframe %.3f (copyts lost?)", in.Video.StartSec, c.wantVStart)
			}
			enc := ""
			if c.transcode {
				enc = "libx264"
			}
			plan := MakePlan(in, VFRReport{}, ProfileCompat, enc)
			if (plan.Video == Transcode) != c.transcode {
				t.Fatalf("plan %+v", plan)
			}
			plan.AlignTo(in.Video.StartSec)
			out := filepath.Join(dir, "out"+strconv.Itoa(i)+".mp4")
			if _, err := (&Normalizer{FFmpeg: ffmpeg}).Run(ctx, plan, sec, out, in, nil); err != nil {
				t.Fatal(err)
			}
			got, _, err := p.Probe(ctx, out)
			if err != nil {
				t.Fatal(err)
			}
			if got.Video.StartSec > 0.001 || got.Audio.StartSec > 0.001 {
				t.Fatalf("normalized starts video %.4f audio %.4f, want 0", got.Video.StartSec, got.Audio.StartSec)
			}
			inPoint := reqStart - in.Video.StartSec // 1.3s → 39번째 프레임
			n := int(math.Round(inPoint * 30))
			if c.transcode {
				// 재인코딩: 원본의 이웃 프레임 중 가장 가까운 것이 같은 번호여야 한다.
				gotF, err := mediatest.FrameGray(ctx, ffmpeg, out, n, 64, 36)
				if err != nil {
					t.Fatal(err)
				}
				best, bestSAD := -1, -1
				for k := 158; k <= 160; k++ {
					ref, err := mediatest.FrameGray(ctx, ffmpeg, src, k, 64, 36)
					if err != nil {
						t.Fatal(err)
					}
					if d := mediatest.SAD(gotF, ref); bestSAD < 0 || d < bestSAD {
						best, bestSAD = k, d
					}
				}
				if best != 159 {
					t.Fatalf("frame at In matches source frame %d, want 159", best)
				}
			} else {
				want, err := mediatest.FrameMD5(ctx, ffmpeg, src, 159)
				if err != nil {
					t.Fatal(err)
				}
				gotMD5, err := mediatest.FrameMD5(ctx, ffmpeg, out, n)
				if err != nil {
					t.Fatal(err)
				}
				if gotMD5 != want {
					t.Fatalf("frame %d at In (%.3fs) differs from source frame 159", n, inPoint)
				}
			}
			// 오디오: In 이후와 핸들 구간(In 이전) 모두 원본과 1ms 이내.
			for _, off := range []float64{0.4, 1.5, -0.6} {
				ref, err := mediatest.PCM(ctx, ffmpeg, src, reqStart+off, 0.3)
				if err != nil {
					t.Fatal(err)
				}
				cmp, err := mediatest.PCM(ctx, ffmpeg, out, inPoint+off, 0.3)
				if err != nil {
					t.Fatal(err)
				}
				lag := mediatest.AudioLag(ref, cmp, 2400)
				t.Logf("audio at In%+.1fs: lag %d samples (%.2f ms)", off, lag, float64(lag)/48)
				if lag < -48 || lag > 48 {
					t.Fatalf("audio at In%+.1fs off by %d samples (%.2f ms), want ≤1ms", off, lag, float64(lag)/48)
				}
			}
		})
	}
}

func f3(x float64) string { return strconv.FormatFloat(x, 'f', 3, 64) }
