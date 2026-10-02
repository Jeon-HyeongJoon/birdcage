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
