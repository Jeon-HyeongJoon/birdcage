package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/proc"
)

// 실제 ffmpeg/ffprobe로 픽스처를 만들어 분석→계획→정규화→검증까지 돌린다.
// ffmpeg이 없으면 건너뛴다(BIRDCAGE_FFMPEG/BIRDCAGE_FFPROBE로 경로 지정 가능).

func ffTools(t *testing.T) (ffmpeg, ffprobe string) {
	t.Helper()
	find := func(env, name string) string {
		if p := os.Getenv(env); p != "" {
			return p
		}
		p, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s not found", name)
		}
		return p
	}
	return find("BIRDCAGE_FFMPEG", "ffmpeg"), find("BIRDCAGE_FFPROBE", "ffprobe")
}

func gen(t *testing.T, ffmpeg, out string, args ...string) {
	t.Helper()
	full := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)
	full = append(full, out)
	if _, err := proc.Run(context.Background(), proc.Spec{Path: ffmpeg, Args: full, StderrTail: 10}); err != nil {
		t.Fatalf("fixture %s: %v", filepath.Base(out), err)
	}
}

func TestNormalizeFixtures(t *testing.T) {
	ffmpeg, ffprobe := ffTools(t)
	dir := t.TempDir()
	src := []string{"-f", "lavfi", "-i", "testsrc2=s=640x360:r=30:d=3", "-f", "lavfi", "-i", "sine=f=440:r=44100:d=3"}

	cfr := filepath.Join(dir, "cfr.mp4")
	gen(t, ffmpeg, cfr, append(src, "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")...)
	vp9 := filepath.Join(dir, "vp9.webm")
	gen(t, ffmpeg, vp9, append(src, "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-b:v", "400k", "-c:a", "libopus", "-shortest")...)
	vfr := filepath.Join(dir, "vfr.mp4")
	// 앞 45프레임은 30fps, 이후는 20fps 간격 → 휴대폰 촬영본 같은 가변 프레임레이트.
	gen(t, ffmpeg, vfr, "-f", "lavfi", "-i", "testsrc2=s=640x360:r=30:d=3",
		"-vf", "setpts='if(lt(N,45),N/30,1.5+(N-45)/20)/TB'", "-fps_mode", "passthrough",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-video_track_timescale", "90000")

	p := &Prober{FFprobe: ffprobe}
	n := &Normalizer{FFmpeg: ffmpeg}
	rec := perftest.Recorder()

	encSpan := rec.Start("test.encoder_detect")
	enc, err := n.Encoder(context.Background())
	encSpan.Set("encoder", enc)
	encSpan.End(err)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("encoder=%s detect=%.0fms", enc, n.EncoderProbeMS())

	cases := []struct {
		name        string
		in          string
		passthrough bool
		video       Action
		vfr         bool
	}{
		{"cfr h264/aac", cfr, true, Copy, false},
		{"vp9/opus webm", vp9, false, Transcode, false},
		{"vfr h264", vfr, false, Transcode, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			perftest.Track(t)
			ctx := context.Background()
			info, _, err := p.Probe(ctx, c.in)
			if err != nil {
				t.Fatal(err)
			}
			rep, _, err := p.AnalyzeVFR(ctx, c.in, info.Video, 30)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Variable != c.vfr {
				t.Fatalf("vfr = %+v, want variable=%v", rep, c.vfr)
			}
			plan := MakePlan(info, rep, ProfileCompat, enc)
			if plan.Passthrough != c.passthrough || plan.Video != c.video {
				t.Fatalf("plan %+v", plan)
			}
			if plan.Passthrough {
				return
			}
			out := filepath.Join(dir, filepath.Base(c.in)+".norm.mp4")
			sp := rec.Start("test.normalize", "case", c.name, "encoder", enc, "video", string(plan.Video), "audio", string(plan.Audio))
			var last float64
			res, err := n.Run(ctx, plan, c.in, out, info, func(f float64) { last = f })
			sp.SetProc(res.Proc)
			sp.Set("speed_x", res.Speed)
			sp.End(err)
			if err != nil {
				t.Fatal(err)
			}
			if last != 1 {
				t.Errorf("final progress = %v", last)
			}
			got, _, err := p.Probe(ctx, out)
			if err != nil {
				t.Fatal(err)
			}
			if got.Video == nil || got.Video.Codec != "h264" || got.Video.PixFmt != "yuv420p" || !got.IsMP4() {
				t.Fatalf("output video %+v container %s", got.Video, got.Container)
			}
			if info.Audio != nil && (got.Audio == nil || got.Audio.Codec != "aac") {
				t.Fatalf("output audio %+v", got.Audio)
			}
			if c.name == "vp9/opus webm" && got.Audio.SampleRate != 48000 {
				t.Errorf("transcoded audio rate %d", got.Audio.SampleRate)
			}
			after, _, err := p.AnalyzeVFR(ctx, out, got.Video, 30)
			if err != nil {
				t.Fatal(err)
			}
			if after.Variable {
				t.Fatalf("output still VFR: %+v", after)
			}
			if _, err := os.Stat(out + ".birdcage-tmp"); !os.IsNotExist(err) {
				t.Error("temp file left behind")
			}
		})
	}
}
