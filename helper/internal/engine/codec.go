package engine

import "strings"

// CodecFamily maps a yt-dlp codec tag (avc1.640028, vp09.00.40.08, hev1…, bytevc1) to a family name.
func CodecFamily(tag string) string {
	t := strings.ToLower(tag)
	switch {
	case strings.HasPrefix(t, "avc"), strings.HasPrefix(t, "h264"):
		return "h264"
	case strings.HasPrefix(t, "hvc1"), strings.HasPrefix(t, "hev1"), strings.HasPrefix(t, "hevc"), strings.HasPrefix(t, "h265"), t == "bytevc1":
		return "hevc"
	case strings.HasPrefix(t, "vp09"), strings.HasPrefix(t, "vp9"):
		return "vp9"
	case strings.HasPrefix(t, "av01"), t == "av1":
		return "av1"
	case strings.HasPrefix(t, "mp4a"), t == "aac":
		return "aac"
	case t == "opus":
		return "opus"
	}
	return t
}

// TenBit reports a 10/12-bit codec tag (vp09.02.*, av01.*.10, hev1.2.*; Main10).
func TenBit(tag string) bool {
	parts := strings.Split(strings.ToLower(tag), ".")
	switch CodecFamily(tag) {
	case "vp9":
		return len(parts) >= 4 && parts[3] != "08"
	case "av1":
		return len(parts) >= 4 && parts[3] != "08"
	case "hevc":
		return len(parts) >= 2 && parts[1] == "2"
	}
	return false
}

// NeedsTranscode estimates from the codec tag alone whether normalization will re-encode the video
// (media.MakePlan과 같은 기준: 8-bit H.264, modern 프로필은 8-bit HEVC까지 그대로 쓴다).
func (v Variant) NeedsTranscode(acceptHEVC bool) bool {
	if TenBit(v.VCodec) {
		return true
	}
	switch CodecFamily(v.VCodec) {
	case "h264":
		return false
	case "hevc":
		return !acceptHEVC
	}
	return true
}
