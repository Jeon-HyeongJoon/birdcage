// Package media inspects downloaded files with ffprobe and normalizes them for
// Premiere with ffmpeg.
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/proc"
)

// Rational is a frame rate like 30000/1001.
type Rational struct{ Num, Den int64 }

// ParseRational parses "30000/1001" or "30". Invalid input yields 0/0.
func ParseRational(s string) Rational {
	n, d, found := strings.Cut(strings.TrimSpace(s), "/")
	num, err := strconv.ParseInt(n, 10, 64)
	if err != nil {
		return Rational{}
	}
	den := int64(1)
	if found {
		if den, err = strconv.ParseInt(d, 10, 64); err != nil {
			return Rational{}
		}
	}
	return Rational{num, den}
}

// Float returns the value, or 0 for an invalid rational.
func (r Rational) Float() float64 {
	if r.Den == 0 {
		return 0
	}
	return float64(r.Num) / float64(r.Den)
}

func (r Rational) String() string { return fmt.Sprintf("%d/%d", r.Num, r.Den) }

// Video describes the primary video stream.
type Video struct {
	Index          int      `json:"index"`
	Codec          string   `json:"codec"`
	Profile        string   `json:"profile,omitempty"`
	PixFmt         string   `json:"pixFmt"`
	Width          int      `json:"width"`
	Height         int      `json:"height"`
	RFrameRate     Rational `json:"-"`
	AvgFrameRate   Rational `json:"-"`
	FPS            float64  `json:"fps"`
	ColorTransfer  string   `json:"colorTransfer,omitempty"`
	ColorPrimaries string   `json:"colorPrimaries,omitempty"`
	BitRate        int64    `json:"bitRate,omitempty"`
	// StartSec is the stream start time. A section download keeps the source timeline (-copyts),
	// so it is the position in the original video where this file's first frame sits.
	StartSec float64 `json:"startSec,omitempty"`
}

// Audio describes the primary audio stream.
type Audio struct {
	Index      int     `json:"index"`
	Codec      string  `json:"codec"`
	Profile    string  `json:"profile,omitempty"`
	SampleRate int     `json:"sampleRate"`
	Channels   int     `json:"channels"`
	StartSec   float64 `json:"startSec,omitempty"`
}

// Info is the ffprobe summary of a file.
type Info struct {
	Container   string  `json:"container"` // ffprobe format_name, e.g. "mov,mp4,m4a,3gp,3g2,mj2"
	DurationSec float64 `json:"durationSec"`
	SizeBytes   int64   `json:"sizeBytes"`
	Video       *Video  `json:"video,omitempty"`
	Audio       *Audio  `json:"audio,omitempty"`
}

// IsMP4 reports whether the container is the ISO BMFF family Premiere reads natively.
func (i *Info) IsMP4() bool {
	return strings.Contains(i.Container, "mp4") || strings.Contains(i.Container, "mov")
}

// HDR reports PQ/HLG transfer characteristics.
func (v *Video) HDR() bool {
	return v.ColorTransfer == "smpte2084" || v.ColorTransfer == "arib-std-b67"
}

// BitDepth infers bit depth from the pixel format.
func (v *Video) BitDepth() int {
	switch {
	case strings.Contains(v.PixFmt, "p10"), strings.Contains(v.PixFmt, "p010"):
		return 10
	case strings.Contains(v.PixFmt, "p12"):
		return 12
	}
	return 8
}

type ffStream struct {
	Index          int    `json:"index"`
	CodecType      string `json:"codec_type"`
	CodecName      string `json:"codec_name"`
	Profile        string `json:"profile"`
	PixFmt         string `json:"pix_fmt"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	RFrameRate     string `json:"r_frame_rate"`
	AvgFrameRate   string `json:"avg_frame_rate"`
	ColorTransfer  string `json:"color_transfer"`
	ColorPrimaries string `json:"color_primaries"`
	BitRate        string `json:"bit_rate"`
	SampleRate     string `json:"sample_rate"`
	Channels       int    `json:"channels"`
	StartTime      string `json:"start_time"`
	Disposition    struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}

type ffProbe struct {
	Streams []ffStream `json:"streams"`
	Format  struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
	} `json:"format"`
}

// ParseProbe parses `ffprobe -show_streams -show_format -of json` output.
func ParseProbe(b []byte) (*Info, error) {
	var p ffProbe
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("ffprobe json: %w", err)
	}
	info := &Info{Container: p.Format.FormatName}
	info.DurationSec, _ = strconv.ParseFloat(p.Format.Duration, 64)
	info.SizeBytes, _ = strconv.ParseInt(p.Format.Size, 10, 64)
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			// 앨범아트(attached_pic)는 영상 스트림이 아니다.
			if info.Video != nil || s.Disposition.AttachedPic == 1 {
				continue
			}
			v := &Video{
				Index: s.Index, Codec: s.CodecName, Profile: s.Profile, PixFmt: s.PixFmt,
				Width: s.Width, Height: s.Height,
				RFrameRate: ParseRational(s.RFrameRate), AvgFrameRate: ParseRational(s.AvgFrameRate),
				ColorTransfer: s.ColorTransfer, ColorPrimaries: s.ColorPrimaries,
			}
			v.BitRate, _ = strconv.ParseInt(s.BitRate, 10, 64)
			v.StartSec, _ = strconv.ParseFloat(s.StartTime, 64)
			v.FPS = v.AvgFrameRate.Float()
			if v.FPS == 0 {
				v.FPS = v.RFrameRate.Float()
			}
			info.Video = v
		case "audio":
			if info.Audio != nil {
				continue
			}
			a := &Audio{Index: s.Index, Codec: s.CodecName, Profile: s.Profile, Channels: s.Channels}
			a.SampleRate, _ = strconv.Atoi(s.SampleRate)
			a.StartSec, _ = strconv.ParseFloat(s.StartTime, 64)
			info.Audio = a
		}
	}
	return info, nil
}

// Prober runs ffprobe.
type Prober struct{ FFprobe string }

// Probe returns stream/format info for path.
func (p *Prober) Probe(ctx context.Context, path string) (*Info, perf.ProcStats, error) {
	out, res, err := proc.Output(ctx, proc.Spec{
		Path:       p.FFprobe,
		Args:       []string{"-v", "error", "-show_streams", "-show_format", "-of", "json", path},
		StderrTail: 10,
	})
	if err != nil {
		return nil, res.Stats, err
	}
	info, err := ParseProbe(out)
	return info, res.Stats, err
}

// VFRReport summarizes frame timing regularity.
type VFRReport struct {
	Samples      int     `json:"samples"`
	MedianDelta  float64 `json:"medianDeltaSec"`
	IrregularPct float64 `json:"irregularPct"` // share of frame intervals deviating >10% from the median
	RAvgMismatch bool    `json:"rAvgMismatch"` // r_frame_rate and avg_frame_rate differ >1%
	Variable     bool    `json:"variable"`
}

// AnalyzeVFR reads packet timestamps of the first window seconds (demux only, cheap)
// and decides whether the stream is variable frame rate.
//
// 휴대폰 촬영본(Instagram/TikTok)은 VFR이 흔하고 Premiere에서 오디오 싱크가 밀리는 원인이 된다.
// r_frame_rate/avg_frame_rate 비교만으로는 놓치는 경우가 있어 실제 프레임 간격 분포를 본다.
func (p *Prober) AnalyzeVFR(ctx context.Context, path string, v *Video, windowSec int) (VFRReport, perf.ProcStats, error) {
	if windowSec <= 0 {
		windowSec = 30
	}
	out, res, err := proc.Output(ctx, proc.Spec{
		Path: p.FFprobe,
		Args: []string{"-v", "error", "-select_streams", "v:0", "-read_intervals", "%+" + strconv.Itoa(windowSec),
			"-show_entries", "packet=pts_time", "-of", "csv=p=0", path},
		StderrTail: 10,
	})
	if err != nil {
		return VFRReport{}, res.Stats, err
	}
	return VFRFromTimestamps(parseTimes(string(out)), v), res.Stats, nil
}

func parseTimes(s string) []float64 {
	var ts []float64
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l), ","))
		if l == "" || l == "N/A" {
			continue
		}
		if f, err := strconv.ParseFloat(l, 64); err == nil {
			ts = append(ts, f)
		}
	}
	return ts
}

// VFRFromTimestamps classifies frame timing from presentation timestamps.
func VFRFromTimestamps(ts []float64, v *Video) VFRReport {
	r := VFRReport{}
	if v != nil {
		rf, af := v.RFrameRate.Float(), v.AvgFrameRate.Float()
		if rf > 0 && af > 0 && math.Abs(rf-af)/rf > 0.01 {
			r.RAvgMismatch = true
		}
	}
	// B-프레임 때문에 패킷 순서 ≠ 표시 순서이므로 정렬 후 간격을 본다.
	sort.Float64s(ts)
	deltas := make([]float64, 0, len(ts))
	for i := 1; i < len(ts); i++ {
		if d := ts[i] - ts[i-1]; d > 0 {
			deltas = append(deltas, d)
		}
	}
	r.Samples = len(deltas)
	if len(deltas) < 10 {
		r.Variable = r.RAvgMismatch
		return r
	}
	sorted := append([]float64(nil), deltas...)
	sort.Float64s(sorted)
	r.MedianDelta = sorted[len(sorted)/2]
	irregular := 0
	for _, d := range deltas {
		if math.Abs(d-r.MedianDelta) > 0.1*r.MedianDelta {
			irregular++
		}
	}
	r.IrregularPct = math.Round(float64(irregular)/float64(len(deltas))*1000) / 10
	r.Variable = r.IrregularPct > 2 || r.RAvgMismatch
	return r
}
