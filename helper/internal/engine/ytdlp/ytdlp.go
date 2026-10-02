// Package ytdlp implements engine.Engine with the yt-dlp executable.
//
// 흐름: Probe(-J, 포맷 선택까지 포함) → info.json 저장 → Download(--load-info-json으로 재추출 없이 다운로드).
// 같은 -f/-S를 두 단계에 모두 넘겨 Probe 결과의 requested_formats가 실제 다운로드 대상과 일치하게 한다.
package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/proc"
)

// Engine runs yt-dlp.
type Engine struct {
	YtDlp     string // yt-dlp executable
	FFmpeg    string // ffmpeg executable passed via --ffmpeg-location (optional)
	JSRuntime string // --js-runtimes value, e.g. "deno:/path/deno" (optional)
	// ConcurrentFragments speeds up DASH/HLS downloads (-N). 0 uses yt-dlp's default.
	ConcurrentFragments int
	Log                 *slog.Logger
}

// Name implements engine.Engine.
func (e *Engine) Name() string { return "yt-dlp" }

// FormatSelector returns -f/-S values tuned for Premiere.
//
// -S 우선순위: SDR > 해상도(≤maxHeight 중 최대) > H.264 > AAC.
// 해상도를 코덱보다 앞에 둬서 같은 해상도 안에서 H.264를 고르게 한다(YouTube는 1080p까지 avc1 제공).
func FormatSelector(maxHeight int) (format, sort string) {
	format = "bv*+ba/b"
	parts := []string{"hdr:sdr"}
	if maxHeight > 0 {
		parts = append(parts, "res:"+strconv.Itoa(maxHeight))
	}
	parts = append(parts, "vcodec:h264", "acodec:aac")
	return format, strings.Join(parts, ",")
}

func (e *Engine) commonArgs() []string {
	args := []string{"--ignore-config", "--no-playlist", "--no-color"}
	if e.FFmpeg != "" {
		args = append(args, "--ffmpeg-location", e.FFmpeg)
	}
	if e.JSRuntime != "" {
		args = append(args, "--js-runtimes", e.JSRuntime)
	}
	return args
}

// ProbeArgs builds the -J invocation.
func (e *Engine) ProbeArgs(url string, maxHeight int) []string {
	f, s := FormatSelector(maxHeight)
	args := append(e.commonArgs(), "-J", "-f", f, "-S", s)
	return append(args, "--", url)
}

// DownloadArgs builds the download invocation for a probed item.
func (e *Engine) DownloadArgs(infoPath, workDir string, maxHeight int) []string {
	f, s := FormatSelector(maxHeight)
	args := append(e.commonArgs(),
		"--load-info-json", infoPath,
		"-f", f, "-S", s,
		"--merge-output-format", "mp4/mkv",
		"-o", filepath.Join(workDir, "media.%(ext)s"),
		"--newline",
		"--progress-template", progressTemplate,
		"--print-to-file", "after_move:filepath", filepath.Join(workDir, "filepath.txt"),
		"--no-mtime",
		"--retries", "3",
		"--fragment-retries", "5",
	)
	if e.ConcurrentFragments > 1 {
		args = append(args, "-N", strconv.Itoa(e.ConcurrentFragments))
	}
	return args
}

// RequestArgs builds the download invocation for a request: DownloadArgs plus the chosen variant.
// 변형을 고르면 -f에 그 포맷 id를 먼저 두고, 기존 선택기를 대체 경로(/)로 남겨 포맷이 사라진 경우에도 받는다.
func (e *Engine) RequestArgs(req engine.DownloadRequest) []string {
	args := e.DownloadArgs(req.Info.InfoPath, req.WorkDir, req.MaxHeight)
	if req.FormatID != "" {
		for i := range args[:len(args)-1] {
			if args[i] == "-f" {
				args[i+1] = req.FormatID + "/" + args[i+1]
				break
			}
		}
	}
	return args
}

// Probe implements engine.Engine.
func (e *Engine) Probe(ctx context.Context, url, workDir string, maxHeight int) (*engine.Info, perf.ProcStats, error) {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, perf.ProcStats{}, engine.Fail(engine.KindInternal, err.Error())
	}
	out, res, err := proc.Output(ctx, proc.Spec{Path: e.YtDlp, Args: e.ProbeArgs(url, maxHeight), StderrTail: 40})
	if err != nil {
		return nil, res.Stats, e.failure(ctx, err, res)
	}
	infoPath := filepath.Join(workDir, "info.json")
	if err := os.WriteFile(infoPath, out, 0o644); err != nil {
		return nil, res.Stats, engine.Fail(engine.KindInternal, err.Error())
	}
	info, err := ParseInfo(out)
	if err != nil {
		return nil, res.Stats, err
	}
	info.InfoPath = infoPath
	if info.IsLive {
		return nil, res.Stats, engine.Fail(engine.KindLive, info.WebpageURL)
	}
	return info, res.Stats, nil
}

// Download implements engine.Engine.
func (e *Engine) Download(ctx context.Context, req engine.DownloadRequest, onProgress func(engine.Progress)) (*engine.DownloadResult, error) {
	if req.Info == nil || req.Info.InfoPath == "" {
		return nil, engine.Fail(engine.KindInternal, "download without probe")
	}
	agg := newAggregator(req.Info)
	lw := proc.NewLineWriter(func(line string) {
		if p, ok := ParseProgress(line); ok {
			if onProgress != nil {
				onProgress(agg.add(p))
			}
			return
		}
		if strings.HasPrefix(line, "[Merger]") && onProgress != nil {
			onProgress(engine.Progress{Status: "merging", Fraction: 1})
		}
	})
	res, err := proc.Run(ctx, proc.Spec{
		Path:       e.YtDlp,
		Args:       e.RequestArgs(req),
		Stdout:     lw,
		StderrTail: 40,
	})
	lw.Flush()
	if err != nil {
		return nil, e.failure(ctx, err, res)
	}
	path, err := outputPath(req.WorkDir)
	if err != nil {
		return nil, engine.Fail(engine.KindInternal, err.Error())
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, engine.Fail(engine.KindInternal, err.Error())
	}
	return &engine.DownloadResult{Path: path, Bytes: st.Size(), Proc: res.Stats}, nil
}

func (e *Engine) failure(ctx context.Context, err error, res proc.Result) error {
	if ctx.Err() != nil {
		return engine.Fail(engine.KindCancelled, "")
	}
	if errors.Is(err, proc.ErrNotStarted) {
		return engine.Fail(engine.KindToolMissing, err.Error())
	}
	f := Classify(res.StderrTail)
	if e.Log != nil {
		e.Log.Warn("yt-dlp failed", "kind", f.Kind, "exit", res.ExitCode, "detail", f.Detail)
	}
	return f
}

// outputPath reads the final path printed by --print-to-file, falling back to media.*.
func outputPath(workDir string) (string, error) {
	if b, err := os.ReadFile(filepath.Join(workDir, "filepath.txt")); err == nil {
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if p := strings.TrimSpace(lines[len(lines)-1]); p != "" {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	matches, _ := filepath.Glob(filepath.Join(workDir, "media.*"))
	for _, m := range matches {
		if !strings.HasSuffix(m, ".part") && !strings.HasSuffix(m, ".ytdl") {
			return m, nil
		}
	}
	return "", fmt.Errorf("yt-dlp finished but no output file in %s", workDir)
}

type rawFormat struct {
	FormatID       string  `json:"format_id"`
	Ext            string  `json:"ext"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FPS            float64 `json:"fps"`
	Filesize       float64 `json:"filesize"`
	FilesizeApprox float64 `json:"filesize_approx"`
}

type rawInfo struct {
	Type         string      `json:"_type"`
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	Uploader     string      `json:"uploader"`
	Channel      string      `json:"channel"`
	ExtractorKey string      `json:"extractor_key"`
	WebpageURL   string      `json:"webpage_url"`
	Duration     float64     `json:"duration"`
	IsLive       bool        `json:"is_live"`
	LiveStatus   string      `json:"live_status"`
	Requested    []rawFormat `json:"requested_formats"`
	rawFormat
	Formats    []fullFormat        `json:"formats"`
	Thumbnail  string              `json:"thumbnail"`
	Thumbnails []rawThumb          `json:"thumbnails"`
	Subtitles  map[string][]rawSub `json:"subtitles"`
	AutoCaps   map[string][]rawSub `json:"automatic_captions"`
	Language   string              `json:"language"`
}

// ParseInfo extracts the fields we need from yt-dlp's -J output.
func ParseInfo(b []byte) (*engine.Info, error) {
	var r rawInfo
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, engine.Fail(engine.KindExtractorBroken, "invalid -J output: "+err.Error())
	}
	if r.Type == "playlist" || r.Type == "multi_video" {
		return nil, engine.Fail(engine.KindUnsupportedURL, "재생목록/다중 영상 링크는 아직 지원하지 않습니다")
	}
	info := &engine.Info{
		ID:          r.ID,
		Title:       r.Title,
		Uploader:    firstNonEmpty(r.Uploader, r.Channel),
		Extractor:   r.ExtractorKey,
		WebpageURL:  r.WebpageURL,
		DurationSec: r.Duration,
		IsLive:      r.IsLive || r.LiveStatus == "is_live" || r.LiveStatus == "is_upcoming",
		Thumbnail:   pickThumbnail(r.Thumbnails, r.Thumbnail, 192),
		Subtitles:   subtitleList(r.Subtitles, r.AutoCaps, r.Language),
	}
	info.Variants, info.BestAudio = buildVariants(r.Formats)
	sel := r.Requested
	if len(sel) == 0 && r.FormatID != "" {
		sel = []rawFormat{r.rawFormat}
	}
	for _, f := range sel {
		size := int64(f.Filesize)
		if size <= 0 {
			size = int64(f.FilesizeApprox)
		}
		info.Selected = append(info.Selected, engine.Format{
			ID: f.FormatID, Ext: f.Ext, VCodec: noneToEmpty(f.VCodec), ACodec: noneToEmpty(f.ACodec),
			Width: f.Width, Height: f.Height, FPS: f.FPS, SizeBytes: size,
		})
	}
	return info, nil
}

func noneToEmpty(s string) string {
	if s == "none" {
		return ""
	}
	return s
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// aggregator turns per-format progress into overall progress across video+audio.
// 크기를 모두 알면 바이트 가중, 하나라도 모르면 포맷별 동일 가중으로 합친다.
type aggregator struct {
	mu       sync.Mutex
	expected map[string]int64 // format id → expected bytes (probe, then refined by progress)
	done     map[string]int64
	finished map[string]bool
}

func newAggregator(info *engine.Info) *aggregator {
	a := &aggregator{expected: map[string]int64{}, done: map[string]int64{}, finished: map[string]bool{}}
	for _, f := range info.Selected {
		a.expected[f.ID] = f.SizeBytes
	}
	return a
}

func (a *aggregator) add(p engine.Progress) engine.Progress {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.expected) <= 1 {
		return p
	}
	if _, ok := a.expected[p.FormatID]; !ok {
		return p // 모르는 포맷 id면 합산하지 않는다
	}
	a.done[p.FormatID] = p.DownloadedBytes
	if p.TotalBytes > 0 {
		a.expected[p.FormatID] = p.TotalBytes
	}
	if p.Status == "finished" {
		a.finished[p.FormatID] = true
	}
	var sumDone, sumExp int64
	known := true
	for id, exp := range a.expected {
		if exp <= 0 {
			known = false
			continue
		}
		sumExp += exp
		sumDone += min(a.done[id], exp)
	}
	if known && sumExp > 0 {
		p.Fraction = clamp01(float64(sumDone) / float64(sumExp))
		return p
	}
	var f float64
	for id, exp := range a.expected {
		switch {
		case a.finished[id]:
			f++
		case exp > 0:
			f += clamp01(float64(a.done[id]) / float64(exp))
		}
	}
	p.Fraction = f / float64(len(a.expected))
	return p
}
