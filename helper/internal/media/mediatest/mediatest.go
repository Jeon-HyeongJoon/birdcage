// Package mediatest decodes frames and audio with ffmpeg for exactness tests (frame identity,
// audio offset). 테스트 전용 도구지만 여러 패키지(media, cmd)의 통합 테스트가 같이 쓰도록 일반 패키지로 둔다.
package mediatest

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strconv"
)

func run(ctx context.Context, ffmpeg string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, ffmpeg, append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg %v: %v: %s", args, err, errb.String())
	}
	return out.Bytes(), nil
}

// FrameMD5 decodes frame n (0-based, presentation order) as raw yuv420p and returns its md5.
// 복사 경로(재인코딩 없음)라면 원본의 같은 프레임과 비트 단위로 같다.
func FrameMD5(ctx context.Context, ffmpeg, file string, n int) (string, error) {
	b, err := run(ctx, ffmpeg, "-i", file, "-vf", "select=eq(n\\,"+strconv.Itoa(n)+")", "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "yuv420p", "-")
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", fmt.Errorf("%s: no frame %d", file, n)
	}
	sum := md5.Sum(b)
	return hex.EncodeToString(sum[:]), nil
}

// FrameGray decodes frame n scaled to w×h gray8 (for comparing re-encoded frames).
func FrameGray(ctx context.Context, ffmpeg, file string, n, w, h int) ([]byte, error) {
	b, err := run(ctx, ffmpeg, "-i", file, "-vf", fmt.Sprintf("select=eq(n\\,%d),scale=%d:%d", n, w, h), "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray", "-")
	if err != nil {
		return nil, err
	}
	if len(b) != w*h {
		return nil, fmt.Errorf("%s: frame %d: got %d bytes", file, n, len(b))
	}
	return b, nil
}

// SAD is the sum of absolute differences of two equal-length byte slices.
func SAD(a, b []byte) int {
	s := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		s += d
	}
	return s
}

// PCM decodes dur seconds from ss as mono s16 at 48 kHz.
func PCM(ctx context.Context, ffmpeg, file string, ss, dur float64) ([]int16, error) {
	b, err := run(ctx, ffmpeg, "-ss", strconv.FormatFloat(ss, 'f', 6, 64), "-i", file, "-t", strconv.FormatFloat(dur, 'f', 6, 64),
		"-vn", "-ac", "1", "-ar", "48000", "-f", "s16le", "-")
	if err != nil {
		return nil, err
	}
	out := make([]int16, len(b)/2)
	_ = binary.Read(bytes.NewReader(b), binary.LittleEndian, out)
	return out, nil
}

// AudioLag returns the lag (in samples, + = b is late) that best aligns b to a, searching ±maxLag.
// 노이즈 신호에서만 뜻이 분명하다(사인파는 주기마다 최소가 생긴다).
func AudioLag(a, b []int16, maxLag int) int {
	lo, hi := maxLag, len(a)-maxLag
	if hi <= lo {
		return 0
	}
	best, bestErr := 0, int64(-1)
	for k := -maxLag; k <= maxLag; k++ {
		var e int64
		for i := lo; i < hi; i++ {
			j := i + k
			if j < 0 || j >= len(b) {
				e += 1 << 15
				continue
			}
			d := int64(a[i]) - int64(b[j])
			if d < 0 {
				d = -d
			}
			e += d
		}
		if bestErr < 0 || e < bestErr {
			best, bestErr = k, e
		}
	}
	return best
}
