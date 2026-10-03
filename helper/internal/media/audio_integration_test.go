package media

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"birdcage/helper/internal/media/mediatest"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/proc"
)

// 오디오만(WAV): 전체를 받아 자르는 경로와 m4a 구간(-copyts) 정렬 경로 모두 원본과 1ms 이내.
func TestAudioOnlyWAVExact(t *testing.T) {
	perftest.Track(t)
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg/ffprobe not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	ff := func(args ...string) {
		t.Helper()
		if _, err := proc.Run(ctx, proc.Spec{Path: ffmpeg, StderrTail: 10, Args: append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)}); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(dir, "a.m4a") // 44.1kHz 스테레오 노이즈 AAC(YouTube 140과 같은 형태)
	ff("-f", "lavfi", "-i", "anoisesrc=d=12:r=44100:a=0.3:seed=11", "-ac", "2", "-c:a", "aac", "-b:a", "128k", src)
	const a, b, handle = 5.3, 8.1, 1.0
	p := &Prober{FFprobe: ffprobe}
	n := &Normalizer{FFmpeg: ffmpeg}

	check := func(t *testing.T, out string, inSec float64) {
		t.Helper()
		got, _, err := p.Probe(ctx, out)
		if err != nil {
			t.Fatal(err)
		}
		if got.Audio == nil || got.Audio.Codec != "pcm_s16le" || got.Audio.SampleRate != 48000 || got.Audio.Channels != 2 || got.Video != nil {
			t.Fatalf("output %+v / %+v", got.Audio, got.Video)
		}
		for _, off := range []float64{0.3, 2.0, -0.7} {
			ref, err := mediatest.PCM(ctx, ffmpeg, src, a+off, 0.3)
			if err != nil {
				t.Fatal(err)
			}
			cmp, err := mediatest.PCM(ctx, ffmpeg, out, inSec+off, 0.3)
			if err != nil {
				t.Fatal(err)
			}
			lag := mediatest.AudioLag(ref, cmp, 2400)
			t.Logf("In%+.1fs: lag %d samples (%.2f ms)", off, lag, float64(lag)/48)
			if lag < -48 || lag > 48 {
				t.Fatalf("In%+.1fs off by %.2f ms", off, float64(lag)/48)
			}
		}
	}

	t.Run("full download, trimmed while converting", func(t *testing.T) {
		in, _, err := p.Probe(ctx, src)
		if err != nil {
			t.Fatal(err)
		}
		plan := AudioPlan(in, ProfileCompat)
		start := a - handle
		plan.Trim(start, b+handle-start)
		out := filepath.Join(dir, "trim.wav")
		if _, err := n.Run(ctx, plan, src, out, in, nil); err != nil {
			t.Fatal(err)
		}
		check(t, out, a-start)
	})

	t.Run("m4a section aligned to its source start", func(t *testing.T) {
		sec := filepath.Join(dir, "sec.m4a")
		start := a - handle
		ff("-ss", f3(start), "-t", f3(b+handle-start), "-i", src, "-c", "copy", "-copyts", "-f", "mp4", sec)
		in, _, err := p.Probe(ctx, sec)
		if err != nil {
			t.Fatal(err)
		}
		if in.Audio.StartSec < start-0.1 || in.Audio.StartSec > start+0.05 {
			t.Fatalf("section audio start %.3f, want ≈%.3f (copyts kept)", in.Audio.StartSec, start)
		}
		plan := AudioPlan(in, ProfileCompat)
		plan.AlignTo(in.Audio.StartSec)
		out := filepath.Join(dir, "sec.wav")
		if _, err := n.Run(ctx, plan, sec, out, in, nil); err != nil {
			t.Fatal(err)
		}
		check(t, out, a-in.Audio.StartSec)
	})
}
