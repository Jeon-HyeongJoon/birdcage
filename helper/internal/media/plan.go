package media

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Profile is the target compatibility level, chosen from the host Premiere version.
type Profile string

const (
	// ProfileCompat targets Premiere 2021–2025: H.264 8-bit 4:2:0 + AAC in MP4.
	ProfileCompat Profile = "compat"
	// ProfileModern targets Premiere 26+: additionally accepts 8-bit HEVC as-is.
	ProfileModern Profile = "modern"
)

// ProfileForHost picks a profile from a Premiere version string like "15.2.0".
func ProfileForHost(version string) Profile {
	major, _ := strconv.Atoi(strings.SplitN(version, ".", 2)[0])
	if major >= 26 {
		return ProfileModern
	}
	return ProfileCompat
}

// Action for one stream.
type Action string

const (
	Copy      Action = "copy"
	Transcode Action = "transcode"
	Drop      Action = "none" // stream absent
)

// Plan is the normalization decision for one file.
type Plan struct {
	Profile   Profile  `json:"profile"`
	Video     Action   `json:"video"`
	Audio     Action   `json:"audio"`
	Encoder   string   `json:"encoder,omitempty"`
	TargetFPS Rational `json:"-"`
	FPS       string   `json:"fps,omitempty"`
	// Passthrough means the input is already Premiere-ready MP4: just move it.
	Passthrough bool     `json:"passthrough"`
	Reasons     []string `json:"reasons"`
	Warnings    []string `json:"warnings,omitempty"`
	BitrateBps  int64    `json:"bitrateBps,omitempty"`
}

// standardRates are snapped to so Premiere sees the usual sequence rates.
var standardRates = []Rational{
	{24000, 1001}, {24, 1}, {25, 1}, {30000, 1001}, {30, 1}, {48, 1}, {50, 1}, {60000, 1001}, {60, 1},
	{120, 1},
}

// SnapFPS returns the closest standard rate within 2%, else the value rounded to 1/1000.
func SnapFPS(fps float64) Rational {
	if fps <= 0 {
		return Rational{30, 1}
	}
	best, bestErr := Rational{}, math.Inf(1)
	for _, r := range standardRates {
		if e := math.Abs(r.Float()-fps) / fps; e < bestErr {
			best, bestErr = r, e
		}
	}
	if bestErr <= 0.02 {
		return best
	}
	return Rational{int64(math.Round(fps * 1000)), 1000}
}

// MakePlan decides copy vs transcode per stream.
func MakePlan(info *Info, vfr VFRReport, profile Profile, encoder string) Plan {
	p := Plan{Profile: profile, Video: Drop, Audio: Drop}
	if v := info.Video; v != nil {
		p.Video = Copy
		switch {
		case !videoCodecOK(v, profile):
			p.Video = Transcode
			p.Reasons = append(p.Reasons, fmt.Sprintf("video codec %s/%s not in %s profile", v.Codec, v.PixFmt, profile))
		case vfr.Variable:
			p.Video = Transcode
			p.Reasons = append(p.Reasons, fmt.Sprintf("variable frame rate (irregular %.1f%%, r/avg mismatch %v)", vfr.IrregularPct, vfr.RAvgMismatch))
		}
		if v.HDR() {
			// V0: 톤매핑은 하지 않는다. 포맷 선택에서 SDR을 우선하므로 드물다.
			p.Warnings = append(p.Warnings, "HDR 원본입니다. Premiere에서 색이 다르게 보일 수 있습니다(톤매핑은 다음 버전).")
		}
		if p.Video == Transcode {
			fps := v.FPS
			if vfr.Variable && vfr.MedianDelta > 0 {
				fps = 1 / vfr.MedianDelta
			}
			p.TargetFPS = SnapFPS(fps)
			p.FPS = p.TargetFPS.String()
			p.Encoder = encoder
			p.BitrateBps = TargetBitrate(v.Width, v.Height, p.TargetFPS.Float())
		}
	}
	if a := info.Audio; a != nil {
		p.Audio = Copy
		if !audioOK(a) {
			p.Audio = Transcode
			p.Reasons = append(p.Reasons, fmt.Sprintf("audio %s %dHz not AAC 44.1/48k", a.Codec, a.SampleRate))
		}
	}
	p.Passthrough = info.IsMP4() && p.Video != Transcode && p.Audio != Transcode
	if p.Passthrough {
		p.Reasons = append(p.Reasons, "already Premiere-ready")
	} else if p.Video != Transcode && p.Audio != Transcode {
		p.Reasons = append(p.Reasons, "remux to mp4")
	}
	return p
}

func videoCodecOK(v *Video, profile Profile) bool {
	if v.BitDepth() != 8 || !(strings.HasPrefix(v.PixFmt, "yuv420p") || strings.HasPrefix(v.PixFmt, "yuvj420p")) {
		return false
	}
	switch v.Codec {
	case "h264":
		return !strings.Contains(v.Profile, "4:4:4") && !strings.Contains(v.Profile, "4:2:2")
	case "hevc":
		return profile == ProfileModern
	}
	return false
}

// audioOK keeps AAC at 44.1/48 kHz as-is: AAC→AAC 재인코딩은 화질(음질) 손실만 있고
// Premiere는 44.1k도 임포트 시 자동 변환(conform)하므로 이득이 작다.
func audioOK(a *Audio) bool {
	return a.Codec == "aac" && (a.SampleRate == 48000 || a.SampleRate == 44100) && a.Channels > 0 && a.Channels <= 8
}

// TargetBitrate estimates an editing-friendly H.264 bitrate: 0.14 bits per pixel per frame,
// clamped to 4–80 Mbps (1080p30 ≈ 8.7 Mbps).
func TargetBitrate(w, h int, fps float64) int64 {
	if fps <= 0 {
		fps = 30
	}
	br := 0.14 * float64(w*h) * fps
	return int64(math.Max(4e6, math.Min(80e6, br)))
}

// Args builds the ffmpeg arguments for the plan. out must end in .mp4 semantics; the
// muxer is forced with -f mp4 so a temp name is fine.
func (p Plan) Args(in, out string, info *Info) []string {
	args := []string{"-hide_banner", "-nostdin", "-y", "-loglevel", "error", "-progress", "pipe:1", "-nostats",
		"-i", in, "-map_metadata", "0", "-dn", "-sn"}
	if p.Video != Drop {
		args = append(args, "-map", "0:v:0")
	}
	if p.Audio != Drop {
		args = append(args, "-map", "0:a:0")
	}
	switch p.Video {
	case Copy:
		args = append(args, "-c:v", "copy")
		if info.Video != nil && info.Video.Codec == "h264" {
			args = append(args, "-tag:v", "avc1")
		} else if info.Video != nil && info.Video.Codec == "hevc" {
			args = append(args, "-tag:v", "hvc1")
		}
	case Transcode:
		args = append(args, encoderArgs(p.Encoder, p.BitrateBps)...)
		gop := int(math.Round(p.TargetFPS.Float() * 2))
		args = append(args, "-fps_mode", "cfr", "-r", p.FPS, "-g", strconv.Itoa(max(gop, 1)),
			"-pix_fmt", "yuv420p", "-tag:v", "avc1")
	}
	switch p.Audio {
	case Copy:
		args = append(args, "-c:a", "copy")
	case Transcode:
		args = append(args, "-c:a", "aac", "-b:a", "256k", "-ar", "48000")
	}
	return append(args, "-movflags", "+faststart", "-f", "mp4", out)
}

func encoderArgs(enc string, br int64) []string {
	b := strconv.FormatInt(br, 10)
	maxr := strconv.FormatInt(br*3/2, 10)
	buf := strconv.FormatInt(br*2, 10)
	switch enc {
	case "h264_videotoolbox":
		return []string{"-c:v", enc, "-b:v", b, "-maxrate", maxr, "-bufsize", buf, "-profile:v", "high", "-allow_sw", "1"}
	case "h264_nvenc":
		return []string{"-c:v", enc, "-preset", "p5", "-rc", "vbr", "-cq", "19", "-b:v", b, "-maxrate", maxr, "-bufsize", buf, "-profile:v", "high"}
	case "h264_qsv":
		return []string{"-c:v", enc, "-b:v", b, "-maxrate", maxr, "-bufsize", buf, "-profile:v", "high"}
	case "h264_amf":
		return []string{"-c:v", enc, "-rc", "vbr_peak", "-b:v", b, "-maxrate", maxr, "-bufsize", buf, "-profile:v", "high"}
	default: // libx264
		return []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-maxrate", maxr, "-bufsize", buf, "-profile:v", "high"}
	}
}
