package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/proc"
	"birdcage/helper/internal/rangefetch"
)

// 구간 다운로드는 먼저 DASH mp4의 sidx로 필요한 조각만 범위 요청으로 받는다(rangefetch).
// 실측(1080p 34분 영상, 2026-10-03): ffmpeg 구간 받기는 재생 속도 약 1.8배로 제한(32s 구간 19.1s),
// 범위 요청은 9.1MB/s. 조각 mp4가 아니거나(webm 등) URL이 만료되면 yt-dlp 구간 받기로 넘어간다.

type infoFormat struct {
	FormatID string            `json:"format_id"`
	URL      string            `json:"url"`
	Protocol string            `json:"protocol"`
	Ext      string            `json:"ext"`
	VCodec   string            `json:"vcodec"`
	ACodec   string            `json:"acodec"`
	Headers  map[string]string `json:"http_headers"`
}

// sectionParts resolves the requested format ids to direct URLs from the probe's info.json.
// Only https fragmented-MP4 candidates (mp4/m4a) qualify.
func sectionParts(req engine.DownloadRequest) ([]infoFormat, error) {
	b, err := os.ReadFile(req.Info.InfoPath)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Formats []infoFormat `json:"formats"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	var ids []string
	if v, ok := req.Info.Variant(req.FormatID); ok {
		for _, p := range v.Parts {
			ids = append(ids, p.ID)
		}
	} else if req.FormatID != "" && !strings.ContainsAny(req.FormatID, "[]/*") {
		ids = strings.Split(req.FormatID, "+")
	} else {
		for _, f := range req.Info.Selected {
			ids = append(ids, f.ID)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("no format ids")
	}
	var out []infoFormat
	for _, id := range ids {
		var found *infoFormat
		for i := range raw.Formats {
			if raw.Formats[i].FormatID == id {
				found = &raw.Formats[i]
			}
		}
		if found == nil {
			return nil, fmt.Errorf("format %s not in info", id)
		}
		if (found.Protocol != "https" && found.Protocol != "http") || (found.Ext != "mp4" && found.Ext != "m4a") || found.URL == "" {
			return nil, fmt.Errorf("format %s is %s/%s: not a fragmented mp4 candidate", id, found.Protocol, found.Ext)
		}
		out = append(out, *found)
	}
	return out, nil
}

// rangeSection fetches only the fragments covering the section and muxes them (copy, keeping
// the source timeline like -copyts). It returns errSkipRange when this path does not apply.
func (e *Engine) rangeSection(ctx context.Context, req engine.DownloadRequest, onProgress func(engine.Progress)) (*engine.DownloadResult, error) {
	parts, err := sectionParts(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSkipRange, err)
	}
	if err := os.MkdirAll(req.WorkDir, 0o755); err != nil {
		return nil, engine.Fail(engine.KindInternal, err.Error())
	}
	t0 := time.Now()
	paths := make([]string, len(parts))
	rp := make([]rangefetch.Part, len(parts))
	for i, p := range parts {
		ext := ".mp4"
		if p.VCodec == "none" || p.VCodec == "" {
			ext = ".m4a"
		}
		paths[i] = filepath.Join(req.WorkDir, "part"+strconv.Itoa(i)+ext)
		rp[i] = rangefetch.Part{URL: p.URL, Headers: p.Headers}
	}
	c := &rangefetch.Client{}
	_, err = c.Fetch(ctx, rp, req.Section.Start, req.Section.End, paths, func(p rangefetch.Progress) {
		if onProgress == nil || p.Total <= 0 {
			return
		}
		pr := engine.Progress{Status: "downloading", DownloadedBytes: p.Done, TotalBytes: p.Total, SpeedBps: p.SpeedBps,
			Fraction: float64(p.Done) / float64(p.Total)}
		if p.SpeedBps > 0 {
			pr.ETASec = float64(p.Total-p.Done) / p.SpeedBps
		}
		onProgress(pr)
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, engine.Fail(engine.KindCancelled, "")
		}
		if errors.Is(err, rangefetch.ErrNoIndex) || errors.Is(err, rangefetch.ErrExpired) {
			return nil, fmt.Errorf("%w: %v", errSkipRange, err)
		}
		return nil, engine.Fail(engine.KindNetwork, err.Error())
	}
	out := filepath.Join(req.WorkDir, "media.mp4")
	if len(paths) == 1 {
		if err := os.Rename(paths[0], out); err != nil {
			return nil, engine.Fail(engine.KindInternal, err.Error())
		}
	} else {
		ffmpeg := e.FFmpeg
		if ffmpeg == "" {
			ffmpeg = "ffmpeg"
		}
		args := []string{"-hide_banner", "-nostdin", "-y", "-loglevel", "error", "-copyts"}
		for _, p := range paths {
			args = append(args, "-i", p)
		}
		args = append(args, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy", "-f", "mp4", out)
		if _, err := proc.Run(ctx, proc.Spec{Path: ffmpeg, Args: args, StderrTail: 10}); err != nil {
			if ctx.Err() != nil {
				return nil, engine.Fail(engine.KindCancelled, "")
			}
			return nil, engine.Fail(engine.KindMedia, "구간 결합 실패: "+err.Error())
		}
	}
	st, err := os.Stat(out)
	if err != nil {
		return nil, engine.Fail(engine.KindInternal, err.Error())
	}
	if onProgress != nil {
		onProgress(engine.Progress{Status: "finished", DownloadedBytes: st.Size(), TotalBytes: st.Size(), Fraction: 1})
	}
	if e.Log != nil {
		e.Log.Info("section by byte ranges", "start", req.Section.Start, "end", req.Section.End, "bytes", st.Size(), "ms", time.Since(t0).Milliseconds())
	}
	return &engine.DownloadResult{Path: out, Bytes: st.Size(), Proc: perf.ProcStats{WallMS: perf.MS(time.Since(t0))}}, nil
}

var errSkipRange = errors.New("byte-range section not applicable")

// ffmpegTime parses the "time=00:01:02.34" field of ffmpeg's progress line (yt-dlp passes ffmpeg's
// stderr through for section downloads; they print no download progress of their own).
var ffmpegTime = regexp.MustCompile(`time=(\d+):(\d\d):(\d\d(?:\.\d+)?)`)
var ffmpegSpeed = regexp.MustCompile(`speed=\s*([\d.]+)x`)

func parseFFmpegProgress(line string, sectionSec float64) (engine.Progress, bool) {
	m := ffmpegTime.FindStringSubmatch(line)
	if m == nil || sectionSec <= 0 {
		return engine.Progress{}, false
	}
	h, _ := strconv.ParseFloat(m[1], 64)
	mi, _ := strconv.ParseFloat(m[2], 64)
	s, _ := strconv.ParseFloat(m[3], 64)
	t := h*3600 + mi*60 + s
	p := engine.Progress{Status: "downloading", Fraction: min(1, max(0, t/sectionSec))}
	if sm := ffmpegSpeed.FindStringSubmatch(line); sm != nil {
		if sp, err := strconv.ParseFloat(sm[1], 64); err == nil && sp > 0 {
			p.ETASec = (sectionSec - t) / sp
		}
	}
	return p, true
}
