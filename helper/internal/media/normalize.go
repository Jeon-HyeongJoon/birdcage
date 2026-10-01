package media

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/proc"
)

// Normalizer runs ffmpeg according to a Plan.
type Normalizer struct {
	FFmpeg string

	encOnce sync.Once
	encoder string
	encErr  error
	encMS   float64
}

// candidates lists hardware H.264 encoders to try per OS, best first; libx264 is the fallback.
func candidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"h264_videotoolbox", "libx264"}
	case "windows":
		return []string{"h264_nvenc", "h264_qsv", "h264_amf", "libx264"}
	default:
		return []string{"h264_nvenc", "h264_qsv", "h264_vaapi", "libx264"}
	}
}

// Encoder returns the first H.264 encoder that actually works on this machine.
// 컴파일된 인코더 목록만으로는 하드웨어 존재를 알 수 없어 0.2초짜리 실제 인코딩으로 확인하고 결과를 캐시한다.
func (n *Normalizer) Encoder(context.Context) (string, error) {
	n.encOnce.Do(func() {
		// 결과를 캐시하므로 호출자 컨텍스트 취소에 영향받지 않게 별도 타임아웃을 쓴다.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		t0 := time.Now()
		for _, enc := range candidates() {
			if enc == "h264_vaapi" {
				continue // 장치 경로 설정이 필요해 V0에서는 건너뛴다
			}
			_, err := proc.Run(ctx, proc.Spec{Path: n.FFmpeg, Args: []string{"-hide_banner", "-nostdin", "-loglevel", "error",
				"-f", "lavfi", "-i", "color=c=black:s=320x240:r=30:d=0.2", "-frames:v", "5", "-c:v", enc,
				"-pix_fmt", "yuv420p", "-f", "null", "-"}, StderrTail: 5})
			if err == nil {
				n.encoder = enc
				break
			}
		}
		n.encMS = perf.MS(time.Since(t0))
		if n.encoder == "" {
			n.encErr = fmt.Errorf("no working H.264 encoder (tried %s)", strings.Join(candidates(), ", "))
		}
	})
	return n.encoder, n.encErr
}

// EncoderProbeMS reports how long encoder detection took (0 before the first call).
func (n *Normalizer) EncoderProbeMS() float64 { return n.encMS }

// Result of a normalization run.
type Result struct {
	Proc perf.ProcStats
	// Speed is ffmpeg's final processing speed relative to realtime (e.g. 4.2x).
	Speed float64
}

// Run executes the plan, writing to out via a temp file renamed on success.
// onProgress receives 0..1 based on out_time vs duration.
func (n *Normalizer) Run(ctx context.Context, plan Plan, in, out string, info *Info, onProgress func(float64)) (Result, error) {
	tmp := out + ".birdcage-tmp"
	defer os.Remove(tmp)
	var (
		mu    sync.Mutex
		speed float64
	)
	dur := info.DurationSec
	lw := proc.NewLineWriter(func(line string) {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return
		}
		switch k {
		case "out_time_us", "out_time_ms": // ffmpeg은 두 키 모두 마이크로초로 출력한다
			us, err := strconv.ParseInt(v, 10, 64)
			if err == nil && dur > 0 && onProgress != nil {
				f := float64(us) / 1e6 / dur
				onProgress(min(max(f, 0), 1))
			}
		case "speed":
			if s, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "x"), 64); err == nil {
				mu.Lock()
				speed = s
				mu.Unlock()
			}
		case "progress":
			if v == "end" && onProgress != nil {
				onProgress(1)
			}
		}
	})
	res, err := proc.Run(ctx, proc.Spec{Path: n.FFmpeg, Args: plan.Args(in, tmp, info), Stdout: lw, StderrTail: 20})
	lw.Flush()
	if err != nil {
		return Result{Proc: res.Stats}, err
	}
	if err := os.Rename(tmp, out); err != nil {
		return Result{Proc: res.Stats}, err
	}
	mu.Lock()
	defer mu.Unlock()
	return Result{Proc: res.Stats, Speed: speed}, nil
}
