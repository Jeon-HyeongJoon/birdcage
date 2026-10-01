// Package engine defines the downloader abstraction and failure taxonomy.
//
// 실패 유형(Kind)이 다음 행동을 결정한다(설계 L0):
// 복구 불가(private/removed/drm/...)는 즉시 종료, rate_limited는 엔진 전환 없이 지연 재시도,
// extractor_broken/degraded만 다음 폴백 단계로 넘긴다.
package engine

import (
	"context"
	"fmt"

	"birdcage/helper/internal/perf"
)

// Kind classifies a failure.
type Kind string

const (
	KindPrivate         Kind = "private"
	KindRemoved         Kind = "removed"
	KindDRM             Kind = "drm"
	KindLoginRequired   Kind = "login_required"
	KindAgeRestricted   Kind = "age_restricted"
	KindRateLimited     Kind = "rate_limited"
	KindGeoBlocked      Kind = "geo_blocked"
	KindUnsupportedURL  Kind = "unsupported_url"
	KindLive            Kind = "live"
	KindFormat          Kind = "format_unavailable"
	KindExtractorBroken Kind = "extractor_broken"
	KindNetwork         Kind = "network"
	KindToolMissing     Kind = "tool_missing"
	KindCancelled       Kind = "cancelled"
	KindMedia           Kind = "media" // ffmpeg/ffprobe 처리 실패
	KindInternal        Kind = "internal"
)

// Policy is what the job runner should do after a failure.
type Policy string

const (
	PolicyStop      Policy = "stop"       // 재시도해도 소용없음
	PolicyRetryLate Policy = "retry_late" // 같은 엔진으로 지연 재시도(엔진 전환 금지)
	PolicyRetry     Policy = "retry"      // 즉시 재시도 가능(네트워크)
	PolicyFallback  Policy = "fallback"   // 다음 폴백 단계로
	PolicyAskUser   Policy = "ask_user"   // 사용자 조치 필요(쿠키 등)
)

// PolicyFor maps a failure kind to the runner policy.
func PolicyFor(k Kind) Policy {
	switch k {
	case KindRateLimited:
		return PolicyRetryLate
	case KindNetwork:
		return PolicyRetry
	case KindExtractorBroken, KindFormat:
		return PolicyFallback
	case KindLoginRequired, KindAgeRestricted, KindToolMissing:
		return PolicyAskUser
	default:
		return PolicyStop
	}
}

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
	return &Failure{Kind: k, Message: messages[k], Detail: detail, Policy: PolicyFor(k)}
}

var messages = map[Kind]string{
	KindPrivate:         "비공개 영상입니다.",
	KindRemoved:         "삭제되었거나 존재하지 않는 영상입니다.",
	KindDRM:             "DRM으로 보호된 콘텐츠라 받을 수 없습니다.",
	KindLoginRequired:   "로그인이 필요한 콘텐츠입니다. 설정에서 쿠키를 지정하세요.",
	KindAgeRestricted:   "연령 제한 콘텐츠입니다. 로그인 쿠키가 필요합니다.",
	KindRateLimited:     "플랫폼이 요청을 일시적으로 제한했습니다. 잠시 후 다시 시도합니다.",
	KindGeoBlocked:      "현재 지역에서 볼 수 없는 콘텐츠입니다.",
	KindUnsupportedURL:  "지원하지 않는 링크입니다.",
	KindLive:            "진행 중인 라이브 방송은 아직 지원하지 않습니다.",
	KindFormat:          "요청한 화질/형식을 찾을 수 없습니다.",
	KindExtractorBroken: "다운로드 엔진이 이 사이트에서 동작하지 않습니다(엔진 업데이트 필요 가능성).",
	KindNetwork:         "네트워크 오류가 발생했습니다.",
	KindToolMissing:     "필요한 도구(yt-dlp/ffmpeg)가 설치되지 않았습니다.",
	KindCancelled:       "취소되었습니다.",
	KindMedia:           "영상 변환에 실패했습니다.",
	KindInternal:        "내부 오류가 발생했습니다.",
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
	// InfoPath is the engine's cached info file reused by Download (yt-dlp --load-info-json).
	InfoPath string `json:"-"`
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

// DownloadRequest asks the engine to fetch a probed item.
type DownloadRequest struct {
	Info      *Info
	MaxHeight int    // 0 = no cap
	WorkDir   string // engine writes its output here
}

// DownloadResult is the engine's output file.
type DownloadResult struct {
	Path  string
	Bytes int64
	Proc  perf.ProcStats
}

// Engine downloads media for URLs it supports.
type Engine interface {
	Name() string
	Probe(ctx context.Context, url, workDir string, maxHeight int) (*Info, perf.ProcStats, error)
	Download(ctx context.Context, req DownloadRequest, onProgress func(Progress)) (*DownloadResult, error)
}
