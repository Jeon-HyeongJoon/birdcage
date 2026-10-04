package ytdlp

import (
	"context"
	"encoding/json"
	"strconv"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/proc"
)

// ListArgs builds a flat listing: entries only, no per-video extraction (채널 0.8s, 검색 2.2s 실측).
// lang=ko는 YouTube가 제목·채널명을 한국어 표시로 주게 한다(시청 페이지 추천과 같은 표기).
func (e *Engine) ListArgs(url string, limit int) []string {
	return []string{"--ignore-config", "--no-color", "--flat-playlist", "-J",
		"--playlist-end", strconv.Itoa(limit), "--extractor-args", "youtube:lang=ko", "--", url}
}

// List implements engine.Lister.
func (e *Engine) List(ctx context.Context, url string, limit int) ([]engine.Entry, error) {
	out, res, err := proc.Output(ctx, proc.Spec{Path: e.YtDlp, Args: e.ListArgs(url, limit), StderrTail: 20})
	if err != nil {
		return nil, e.failure(ctx, err, res)
	}
	return ParseList(out)
}

type rawEntry struct {
	ID         string     `json:"id"`
	URL        string     `json:"url"`
	Title      string     `json:"title"`
	Channel    string     `json:"channel"`
	Uploader   string     `json:"uploader"`
	Duration   float64    `json:"duration"`
	ViewCount  float64    `json:"view_count"`
	LiveStatus string     `json:"live_status"`
	Thumbnails []rawThumb `json:"thumbnails"`
	IEKey      string     `json:"ie_key"`
}

// ParseList reads yt-dlp's --flat-playlist -J output (a playlist whose entries are videos).
func ParseList(b []byte) ([]engine.Entry, error) {
	var r struct {
		Entries []rawEntry `json:"entries"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, engine.Fail(engine.KindExtractorBroken, "invalid --flat-playlist output: "+err.Error())
	}
	out := make([]engine.Entry, 0, len(r.Entries))
	for _, x := range r.Entries {
		if x.ID == "" || x.Title == "" || (x.IEKey != "" && x.IEKey != "Youtube") {
			continue // 재생목록·채널 항목(YoutubeTab)은 영상이 아니다
		}
		u := x.URL
		if u == "" || x.IEKey == "Youtube" {
			u = "https://www.youtube.com/watch?v=" + x.ID
		}
		out = append(out, engine.Entry{
			ID: x.ID, URL: u, Title: x.Title, Channel: firstNonEmpty(x.Channel, x.Uploader),
			DurationSec: x.Duration, Views: int64(x.ViewCount),
			Thumbnail: pickThumbnail(x.Thumbnails, "", 320),
			Live:      x.LiveStatus == "is_live" || x.LiveStatus == "is_upcoming",
		})
	}
	return out, nil
}
