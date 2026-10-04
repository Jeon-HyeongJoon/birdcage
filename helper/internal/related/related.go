// Package related builds the browse list shown next to the preview (탐색 목록).
//
// 출처는 셋이고 정직하게 이름을 붙인다(docs/decisions/0012):
//   - youtube: 유튜브 시청 페이지의 '다음 동영상'(유튜브 추천 그대로, 실측 0.9s·19–26개)
//   - channel: 같은 채널의 최근 영상(yt-dlp 평면 목록, 0.8s)
//   - search:  제목 핵심어 검색(yt-dlp ytsearch, 2.2s)
//
// 공식 API가 없는 비공개 형식이라 언제든 깨질 수 있다. 깨지면 다음 출처로 넘어가고(youtube → channel →
// search) 실제로 채운 출처를 결과에 남겨 패널이 그대로 표시한다.
package related

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/perf"
)

// Source names where a list comes from.
type Source string

const (
	SourceYouTube Source = "youtube"
	SourceChannel Source = "channel"
	SourceSearch  Source = "search"
)

// Valid reports a known source.
func (s Source) Valid() bool {
	return s == SourceYouTube || s == SourceChannel || s == SourceSearch
}

// Item is one video in the list.
type Item struct {
	ID          string  `json:"id"`
	URL         string  `json:"url"`
	Title       string  `json:"title"`
	Channel     string  `json:"channel,omitempty"`
	DurationSec float64 `json:"durationSec,omitempty"`
	Thumbnail   string  `json:"thumbnail,omitempty"`
	// Meta is the platform's own text (조회수·게시 시점), shown as is.
	Meta  string `json:"meta,omitempty"`
	Views int64  `json:"views,omitempty"`
}

// Seed is the video the list is for.
type Seed struct {
	ID, Title, Channel, ChannelURL string
}

// SeedOf returns the seed for a probed YouTube video (ok=false for other platforms).
func SeedOf(info *engine.Info) (Seed, bool) {
	if info == nil || info.Extractor != "Youtube" || info.ID == "" {
		return Seed{}, false
	}
	return Seed{ID: info.ID, Title: info.Title, Channel: info.Uploader, ChannelURL: info.ChannelURL}, true
}

// Result is one list and the source that actually filled it.
type Result struct {
	Source Source
	Items  []Item
}

// Limit is how many items a list asks for (시청 페이지는 원래 20개 안팎을 준다).
const Limit = 30

// Service fetches lists with fallback and caches them.
type Service struct {
	HTTP    *http.Client
	BaseURL string        // https://www.youtube.com (tests: httptest server)
	TTL     time.Duration // cache lifetime (0 = 30 min)
	Max     int           // cached lists (0 = 64)
	Rec     *perf.Recorder

	mu       sync.Mutex
	cache    map[string]cached
	searches map[string]*search
}

type cached struct {
	res Result
	at  time.Time
}

// fallbacks is the order tried for each requested source.
var fallbacks = map[Source][]Source{
	SourceYouTube: {SourceYouTube, SourceChannel, SourceSearch},
	SourceChannel: {SourceChannel, SourceSearch},
	SourceSearch:  {SourceSearch},
}

// List returns the list for seed from want or, if that fails or is empty, the next source.
// lister may be nil (then only the youtube source works).
func (s *Service) List(ctx context.Context, seed Seed, want Source, lister engine.Lister) (Result, error) {
	if !want.Valid() {
		want = SourceYouTube
	}
	var errs []error
	for _, src := range fallbacks[want] {
		key := string(src) + "|" + seed.ID
		if r, ok := s.cached(key); ok {
			s.Rec.Start("related.list", "requested", string(want), "source", string(src), "items", len(r.Items), "cached", true).End(nil)
			return r, nil
		}
		span := s.Rec.Start("related.list", "requested", string(want), "source", string(src))
		items, err := s.fetch(ctx, seed, src, lister)
		span.Set("items", len(items), "cached", false)
		span.End(err)
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if err == nil && len(items) == 0 {
			err = fmt.Errorf("%s: no items", src)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		r := Result{Source: src, Items: items}
		s.put(key, r)
		return r, nil
	}
	return Result{}, errors.Join(errs...)
}

func (s *Service) fetch(ctx context.Context, seed Seed, src Source, lister engine.Lister) ([]Item, error) {
	switch src {
	case SourceYouTube:
		return s.watchPage(ctx, seed.ID)
	case SourceChannel:
		if seed.ChannelURL == "" {
			return nil, errors.New("channel: unknown channel url")
		}
		return listed(ctx, lister, strings.TrimRight(seed.ChannelURL, "/")+"/videos", seed)
	case SourceSearch:
		q := Query(seed.Title, seed.Channel)
		if q == "" {
			return nil, errors.New("search: empty query")
		}
		return listed(ctx, lister, fmt.Sprintf("ytsearch%d:%s", Limit, q), seed)
	}
	return nil, fmt.Errorf("unknown source %q", src)
}

func listed(ctx context.Context, lister engine.Lister, u string, seed Seed) ([]Item, error) {
	if lister == nil {
		return nil, errors.New("listing needs yt-dlp")
	}
	es, err := lister.List(ctx, u, Limit)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(es))
	for _, e := range es {
		if e.Live {
			continue // 생방송은 받을 수 없다(KindLive)
		}
		ch := e.Channel
		if ch == "" && strings.HasSuffix(u, "/videos") {
			ch = seed.Channel // 채널 목록 항목에는 채널명이 없다
		}
		items = append(items, Item{ID: e.ID, Title: e.Title, Channel: ch, DurationSec: e.DurationSec, Thumbnail: e.Thumbnail, Views: e.Views})
	}
	return clean(items, seed.ID), nil
}

// watchPage reads the "up next" list from the watch page.
func (s *Service) watchPage(ctx context.Context, id string) ([]Item, error) {
	page, err := s.get(ctx, "/watch?v="+url.QueryEscape(id)+"&hl=ko&gl=KR") // 실측 1.2MB
	if err != nil {
		return nil, err
	}
	return ParseWatchPage(page, id)
}

func (s *Service) cached(key string) (Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cache[key]
	if !ok || time.Since(c.at) > s.ttl() {
		return Result{}, false
	}
	return c.res, true
}

func (s *Service) put(key string, r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[string]cached{}
	}
	now := time.Now()
	s.cache[key] = cached{r, now}
	max := s.Max
	if max <= 0 {
		max = 64
	}
	for len(s.cache) > max {
		oldest := ""
		for k, c := range s.cache {
			if oldest == "" || c.at.Before(s.cache[oldest].at) {
				oldest = k
			}
		}
		delete(s.cache, oldest)
	}
}

func (s *Service) ttl() time.Duration {
	if s.TTL > 0 {
		return s.TTL
	}
	return 30 * time.Minute
}

// Query turns a title into search keywords: the first title segment without brackets, hashtags,
// symbols and the channel name, at most 8 words. 채널명을 빼야 같은 채널 목록과 겹치지 않는다.
func Query(title, channel string) string {
	t := title
	for _, p := range [][2]string{{"[", "]"}, {"(", ")"}, {"【", "】"}, {"「", "」"}, {"『", "』"}, {"<", ">"}} {
		t = stripPairs(t, p[0], p[1])
	}
	for _, sep := range []string{"|", "｜", " - ", " / ", " — "} {
		if i := strings.Index(t, sep); i > 0 && len([]rune(strings.TrimSpace(t[:i]))) >= 4 {
			t = t[:i]
		}
	}
	var words []string
	for _, w := range strings.Fields(t) {
		if strings.HasPrefix(w, "#") || strings.HasPrefix(w, "@") {
			continue
		}
		w = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, w)
		if w != "" {
			words = append(words, w)
		}
	}
	q := strings.Join(words, " ")
	if ch := strings.TrimSpace(channel); ch != "" && !strings.EqualFold(q, ch) {
		q = strings.Join(strings.Fields(caseless(q, ch)), " ")
	}
	if words = strings.Fields(q); len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}

func stripPairs(s, open, close string) string {
	for {
		i := strings.Index(s, open)
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+len(open):], close)
		if j < 0 {
			return s
		}
		s = s[:i] + " " + s[i+len(open)+j+len(close):]
	}
}

// caseless removes every case-insensitive occurrence of sub from s.
func caseless(s, sub string) string {
	ls, lsub := strings.ToLower(s), strings.ToLower(sub)
	if len(ls) != len(s) || lsub == "" {
		return strings.ReplaceAll(s, sub, " ") // 대소문자 변환이 길이를 바꾸면 정확히 일치만 지운다
	}
	var b strings.Builder
	for {
		i := strings.Index(ls, lsub)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i] + " ")
		s, ls = s[i+len(sub):], ls[i+len(lsub):]
	}
}
