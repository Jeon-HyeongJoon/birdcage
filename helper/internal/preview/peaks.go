package preview

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"math"

	"birdcage/helper/internal/proc"
)

// Peaks is the waveform: one linear peak per bucket, 0–255 (255 = full scale), base64 bytes.
// 표시(선그래프, dB 변환)는 패널이 정한다(docs/decisions/0011).
type Peaks struct {
	BucketSec   float64 `json:"bucketSec"`
	Count       int     `json:"count"`
	DurationSec float64 `json:"durationSec"`
	Data        string  `json:"data"`
}

const (
	peakRate    = 8000  // 디코드 샘플레이트: 말소리 대역의 윤곽을 잃지 않으면서 작게
	maxBuckets  = 20000 // 10분이면 약 30ms 간격: 확대해도 선이 매끄럽다
	minBucketMS = 10
)

// ComputePeaks decodes the first audio stream to mono s16 at 8 kHz and takes the peak of each bucket.
func ComputePeaks(ctx context.Context, ffmpeg, path string) (Peaks, error) {
	out, _, err := proc.Output(ctx, proc.Spec{Path: ffmpeg, StderrTail: 10, Args: []string{"-hide_banner", "-nostdin", "-loglevel", "error",
		"-i", path, "-vn", "-ac", "1", "-ar", "8000", "-f", "s16le", "-"}})
	if err != nil {
		return Peaks{}, err
	}
	return peaksFromPCM(out, peakRate), nil
}

func peaksFromPCM(pcm []byte, rate int) Peaks {
	n := len(pcm) / 2
	dur := float64(n) / float64(rate)
	if n == 0 {
		return Peaks{}
	}
	per := max(rate*minBucketMS/1000, int(math.Ceil(float64(n)/maxBuckets)))
	buckets := (n + per - 1) / per
	vals := make([]byte, buckets)
	for b := 0; b < buckets; b++ {
		peak := 0
		for i := b * per; i < min(n, (b+1)*per); i++ {
			v := int(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		vals[b] = byte(min(255, (peak*255+16383)/32767))
	}
	return Peaks{BucketSec: float64(per) / float64(rate), Count: buckets, DurationSec: dur, Data: base64.StdEncoding.EncodeToString(vals)}
}
