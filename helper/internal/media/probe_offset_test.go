package media

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"birdcage/helper/internal/proc"
)

// 구간 결합 파일처럼 비디오가 오디오보다 6초 늦게 시작해도 비디오 형식(pix_fmt)을 알아내야 한다.
// 기본 분석 시간(5초)이면 "unknown"이 되어 H.264를 변환 대상으로 오판했다.
func TestProbeLateVideoStart(t *testing.T) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg/ffprobe not found")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		if _, err := proc.Run(context.Background(), proc.Spec{Path: ffmpeg, StderrTail: 5, Args: append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)}); err != nil {
			t.Fatal(err)
		}
	}
	v, a, out := filepath.Join(dir, "v.mp4"), filepath.Join(dir, "a.m4a"), filepath.Join(dir, "late.mp4")
	run("-f", "lavfi", "-i", "testsrc2=s=320x240:r=15:d=8", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", v)
	run("-f", "lavfi", "-i", "anoisesrc=d=16:r=44100:a=0.2", "-c:a", "aac", a)
	run("-copyts", "-itsoffset", "6", "-i", v, "-i", a, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy", "-f", "mp4", out)
	in, _, err := (&Prober{FFprobe: ffprobe}).Probe(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	if in.Video == nil || in.Video.PixFmt != "yuv420p" || in.Video.StartSec < 5.9 {
		t.Fatalf("video %+v", in.Video)
	}
	if p := MakePlan(in, VFRReport{}, ProfileCompat, ""); p.Video != Copy {
		t.Fatalf("H.264 yuv420p must be copied, plan %+v", p)
	}
}
