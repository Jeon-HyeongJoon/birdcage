package ytdlp

import (
	"math"
	"sort"
	"strings"

	"birdcage/helper/internal/engine"
)

// 화질 목록(Variants)은 -J의 formats 전체에서 해상도마다 하나를 고른다.
// 고르는 순서는 다운로드 선택기(FormatSelector: hdr:sdr, res, vcodec:h264, acodec:aac)와 같다:
// SDR > H.264 > fps > https(조각 HLS보다 DASH) > tbr. 그래서 같은 해상도면 실제 다운로드와 같은 포맷이 된다.

type fullFormat struct {
	rawFormat
	TBR          float64 `json:"tbr"`
	ABR          float64 `json:"abr"`
	DynamicRange string  `json:"dynamic_range"`
	LangPref     float64 `json:"language_preference"`
}

func (f fullFormat) hasVideo() bool { return f.VCodec != "" && f.VCodec != "none" }
func (f fullFormat) hasAudio() bool { return f.ACodec != "" && f.ACodec != "none" }
func (f fullFormat) drc() bool      { return strings.HasSuffix(f.FormatID, "-drc") }
func (f fullFormat) hdr() bool      { return f.DynamicRange != "" && f.DynamicRange != "SDR" }

// res is the short side (yt-dlp "res"): a vertical 1080×1920 video is 1080p.
func (f fullFormat) res() int {
	if f.Width > 0 && f.Height > 0 {
		return min(f.Width, f.Height)
	}
	return f.Height
}

func (f fullFormat) size() int64 {
	if f.Filesize > 0 {
		return int64(f.Filesize)
	}
	return int64(f.FilesizeApprox)
}

func (f fullFormat) format() engine.Format {
	return engine.Format{ID: f.FormatID, Ext: f.Ext, VCodec: noneToEmpty(f.VCodec), ACodec: noneToEmpty(f.ACodec),
		Width: f.Width, Height: f.Height, FPS: f.FPS, SizeBytes: f.size(), Protocol: f.Protocol}
}

func videoRank(f fullFormat) []float64 {
	b := func(ok bool) float64 {
		if ok {
			return 1
		}
		return 0
	}
	return []float64{b(!f.hdr()), b(engine.CodecFamily(f.VCodec) == "h264"), b(!f.hasAudio()), f.FPS,
		b(f.Protocol == "https" || f.Protocol == "http"), f.TBR, float64(f.size())}
}

func audioRank(f fullFormat, preferAAC bool) []float64 {
	b := func(ok bool) float64 {
		if ok {
			return 1
		}
		return 0
	}
	aac := 0.0
	if preferAAC {
		aac = b(engine.CodecFamily(f.ACodec) == "aac")
	}
	return []float64{f.LangPref, b(!f.drc()), aac, b(f.Protocol == "https" || f.Protocol == "http"), f.ABR, f.TBR}
}

func greater(a, b []float64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// buildVariants picks one variant per resolution (best first) and the best audio-only format.
// mergeAudio is the audio a DASH video is merged with (AAC preferred, like the download selector);
// bestAudio is the best-quality audio for audio-only downloads (any codec: it is decoded to WAV).
func buildVariants(formats []fullFormat) (variants []engine.Variant, bestAudio *engine.Format) {
	var merge, best *fullFormat
	byRes := map[int]*fullFormat{}
	for i := range formats {
		f := &formats[i]
		switch {
		case f.hasVideo():
			r := f.res()
			if r <= 0 {
				continue
			}
			if cur := byRes[r]; cur == nil || greater(videoRank(*f), videoRank(*cur)) {
				byRes[r] = f
			}
		case f.hasAudio():
			if merge == nil || greater(audioRank(*f, true), audioRank(*merge, true)) {
				merge = f
			}
			if best == nil || greater(audioRank(*f, false), audioRank(*best, false)) {
				best = f
			}
		}
	}
	for r, v := range byRes {
		vr := engine.Variant{FormatID: v.FormatID, Res: r, Width: v.Width, Height: v.Height, FPS: v.FPS,
			VCodec: v.VCodec, ACodec: noneToEmpty(v.ACodec), HDR: v.hdr(), SizeBytes: v.size(), Parts: []engine.Format{v.format()}}
		if !v.hasAudio() && merge != nil {
			vr.FormatID = v.FormatID + "+" + merge.FormatID
			vr.ACodec = merge.ACodec
			vr.Parts = append(vr.Parts, merge.format())
			if vr.SizeBytes > 0 && merge.size() > 0 {
				vr.SizeBytes += merge.size()
			} else {
				vr.SizeBytes = 0 // 하나라도 모르면 합계도 모른다
			}
		}
		variants = append(variants, vr)
	}
	sort.Slice(variants, func(i, j int) bool { return variants[i].Res > variants[j].Res })
	if best != nil {
		f := best.format()
		bestAudio = &f
	}
	return variants, bestAudio
}

type rawThumb struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// pickThumbnail returns the smallest 16:9 thumbnail at least minWidth wide (2× the 96px card),
// falling back to the extractor's main thumbnail.
func pickThumbnail(thumbs []rawThumb, fallback string, minWidth int) string {
	best := -1
	for i, t := range thumbs {
		if t.URL == "" || t.Width < minWidth || t.Height <= 0 {
			continue
		}
		if math.Abs(float64(t.Width)/float64(t.Height)-16.0/9.0) > 0.1 {
			continue
		}
		if best < 0 || t.Width < thumbs[best].Width {
			best = i
		}
	}
	if best >= 0 {
		return thumbs[best].URL
	}
	return fallback
}

type rawSub struct {
	Ext  string `json:"ext"`
	Name string `json:"name"`
}

// subtitleList lists manual subtitles, then automatic captions only for the original language:
// YouTube은 자동 자막을 기계 번역 언어 150여 개로 함께 내보내 목록이 쓸모없어진다.
func subtitleList(manual, auto map[string][]rawSub, language string) []engine.Subtitle {
	name := func(subs []rawSub) string {
		for _, s := range subs {
			if s.Name != "" {
				return s.Name
			}
		}
		return ""
	}
	var out []engine.Subtitle
	for _, lang := range sortedKeys(manual) {
		if lang == "live_chat" {
			continue
		}
		out = append(out, engine.Subtitle{Lang: lang, Name: name(manual[lang])})
	}
	var autos []string
	for _, lang := range sortedKeys(auto) {
		if strings.HasSuffix(lang, "-orig") || (language != "" && lang == language) {
			autos = append(autos, lang)
		}
	}
	if len(autos) == 0 && len(auto) <= 5 { // 번역 목록이 없으면(짧은 목록) 그대로 보여 준다
		autos = sortedKeys(auto)
	}
	for _, lang := range autos {
		out = append(out, engine.Subtitle{Lang: lang, Name: name(auto[lang]), Auto: true})
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
