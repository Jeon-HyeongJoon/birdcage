// Package engine defines the downloader abstraction and failure taxonomy.
//
// 실패 유형(Kind)이 다음 행동을 결정한다(설계 L0):
// 복구 불가(private/removed/drm/...)는 즉시 종료, rate_limited는 엔진 전환 없이 지연 재시도,
// extractor_broken/degraded만 다음 폴백 단계로 넘긴다.
package engine

import (
	"context"
	"fmt"
	"strings"

	"birdcage/helper/internal/perf"
)

// Kind, Policy, the Kind*/Policy* constants, PolicyFor and MessageFor are generated
// from specs/failures.json (failures_gen.go). 실패 분류표를 바꾸려면 명세를 고치고 `make gen`.

// Failure is a classified error.
type Failure struct {
	Kind    Kind   `json:"kind"`
	Message string `json:"message"`          // 사용자에게 보여줄 한국어 설명
	Detail  string `json:"detail,omitempty"` // 원본 오류(마지막 stderr 줄 등)
	Policy  Policy `json:"policy"`
}

func (f *Failure) Error() string {
	if f.Detail != "" {
		return fmt.Sprintf("%s: %s (%s)", f.Kind, f.Message, f.Detail)
	}
	return fmt.Sprintf("%s: %s", f.Kind, f.Message)
}

// Fail builds a Failure with the default policy and message for its kind.
func Fail(k Kind, detail string) *Failure {
	return &Failure{Kind: k, Message: MessageFor(k), Detail: detail, Policy: PolicyFor(k)}
}

// Info is the metadata of a media item, from a probe.
type Info struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Uploader    string  `json:"uploader,omitempty"`
	Extractor   string  `json:"extractor"` // e.g. Youtube, Instagram, TikTok
	WebpageURL  string  `json:"webpageUrl"`
	DurationSec float64 `json:"durationSec,omitempty"`
	IsLive      bool    `json:"isLive,omitempty"`
	// Selected lists the formats the engine will download (video+audio or a single muxed one).
	Selected []Format `json:"selected,omitempty"`
	// Thumbnail is a small (≥192px wide) preview image URL the panel loads directly.
	Thumbnail string `json:"thumbnail,omitempty"`
	// Variants are the downloadable qualities, best first (one per resolution).
	Variants []Variant `json:"variants,omitempty"`
	// BestAudio is the best audio-only format (audio-only downloads, size display).
	BestAudio *Format `json:"bestAudio,omitempty"`
	// Subtitles lists available subtitle tracks (manual first; automatic only in the original language).
	Subtitles []Subtitle `json:"subtitles,omitempty"`
	// Storyboards are the platform's thumbnail sprite sheets (YouTube), smallest tiles first.
	Storyboards []Storyboard `json:"storyboards,omitempty"`
	// InfoPath is the engine's cached info file reused by Download (yt-dlp --load-info-json).
	InfoPath string `json:"-"`
}

// Variant is one selectable quality: a video format plus (for DASH) the audio it merges with.
//
// Res는 yt-dlp의 res와 같이 짧은 변(세로 영상 1080×1920 → 1080)이라 "1080p"가 방향과 무관하다.
type Variant struct {
	FormatID  string  `json:"formatId"` // yt-dlp -f value, e.g. "137+140" or "18"
	Res       int     `json:"res"`
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	FPS       float64 `json:"fps,omitempty"`
	VCodec    string  `json:"vcodec"`
	ACodec    string  `json:"acodec,omitempty"`
	HDR       bool    `json:"hdr,omitempty"`
	SizeBytes int64   `json:"sizeBytes,omitempty"` // video+audio, exact or approximate (0 = unknown)
	// Transcode reports that the video codec is not Premiere-ready for the session profile
	// (판단은 코덱만으로 한다. VFR은 받은 뒤 ffprobe로만 알 수 있다).
	Transcode bool `json:"transcode"`
	// Parts are the formats downloaded for this variant (progress aggregation).
	Parts []Format `json:"-"`
}

// Storyboard is a set of sprite sheets: each sprite holds Cols×Rows tiles, one every IntervalSec.
// 타임라인 필름스트립과 스크럽 미리보기에 쓴다(영상을 받지 않고 0.6s, docs/decisions/0011).
type Storyboard struct {
	Width       int      `json:"width"`  // tile size in pixels
	Height      int      `json:"height"` //
	Cols        int      `json:"cols"`
	Rows        int      `json:"rows"`
	IntervalSec float64  `json:"intervalSec"`
	Sprites     []Sprite `json:"sprites"`
}

// Sprite is one sheet image (a URL, or a local file path for generated storyboards).
type Sprite struct {
	URL         string  `json:"url"`
	StartSec    float64 `json:"startSec"`
	DurationSec float64 `json:"durationSec"`
}

// Subtitle is one available subtitle track.
type Subtitle struct {
	Lang string `json:"lang"`
	Name string `json:"name,omitempty"`
	Auto bool   `json:"auto,omitempty"` // automatic captions (speech recognition)
}

// Variant returns the variant with the given format id.
func (i *Info) Variant(formatID string) (Variant, bool) {
	for _, v := range i.Variants {
		if v.FormatID == formatID {
			return v, true
		}
	}
	return Variant{}, false
}

// Format summarizes one selected stream/format.
type Format struct {
	ID        string  `json:"id"`
	Ext       string  `json:"ext,omitempty"`
	VCodec    string  `json:"vcodec,omitempty"`
	ACodec    string  `json:"acodec,omitempty"`
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	FPS       float64 `json:"fps,omitempty"`
	SizeBytes int64   `json:"sizeBytes,omitempty"` // exact or approximate
	Protocol  string  `json:"protocol,omitempty"`  // https | m3u8_native | …
}

// Segmented reports HLS-style formats whose timestamps do not start at the source origin
// (MPEG-TS PTS) — 구간 다운로드의 시작 시각으로 In/Out을 계산할 수 없다.
func (f Format) Segmented() bool {
	return strings.HasPrefix(f.Protocol, "m3u8") || strings.HasPrefix(f.Protocol, "f4m")
}

// ExpectedBytes sums the known sizes of the selected formats (0 if unknown).
func (i *Info) ExpectedBytes() int64 {
	var n int64
	for _, f := range i.Selected {
		if f.SizeBytes <= 0 {
			return 0
		}
		n += f.SizeBytes
	}
	return n
}

// Progress is a download progress update.
type Progress struct {
	Status          string  `json:"status"` // downloading | finished | merging
	DownloadedBytes int64   `json:"downloadedBytes"`
	TotalBytes      int64   `json:"totalBytes,omitempty"`
	TotalEstimated  bool    `json:"totalEstimated,omitempty"`
	SpeedBps        float64 `json:"speedBps,omitempty"`
	ETASec          float64 `json:"etaSec,omitempty"`
	FragIndex       int     `json:"fragIndex,omitempty"`
	FragCount       int     `json:"fragCount,omitempty"`
	FormatID        string  `json:"formatId,omitempty"`
	// Fraction is overall 0..1 progress when computable, else -1.
	Fraction float64 `json:"fraction"`
}

// Section is a span of the source timeline in seconds.
type Section struct {
	Start float64 `json:"startSec"`
	End   float64 `json:"endSec"`
}

// DownloadRequest asks the engine to fetch a probed item.
type DownloadRequest struct {
	Info      *Info
	MaxHeight int    // 0 = no cap
	FormatID  string // explicit variant (Variant.FormatID); "" = select by MaxHeight
	WorkDir   string // engine writes its output here
	// Section limits the download to part of the source. The output keeps the source timeline
	// (stream start times = original positions) so the caller can align streams and place In/Out.
	Section *Section
	// AudioOnly downloads audio (FormatID, else the best audio, else the best muxed format).
	AudioOnly bool
}

// DownloadResult is the engine's output file.
type DownloadResult struct {
	Path  string
	Bytes int64
	Proc  perf.ProcStats
}

// SubtitleFetcher is implemented by engines that fetch one subtitle track as SRT in a separate
// call, so a subtitle failure (자주 HTTP 429) never fails the media download.
type SubtitleFetcher interface {
	Subtitles(ctx context.Context, info *Info, lang string, auto bool, workDir string) (path string, err error)
}

// Engine downloads media for URLs it supports.
type Engine interface {
	Name() string
	Probe(ctx context.Context, url, workDir string, maxHeight int) (*Info, perf.ProcStats, error)
	Download(ctx context.Context, req DownloadRequest, onProgress func(Progress)) (*DownloadResult, error)
}
