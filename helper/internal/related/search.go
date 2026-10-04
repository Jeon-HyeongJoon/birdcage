package related

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"birdcage/helper/internal/engine"
)

// 유튜브 검색(T-026, docs/decisions/0013). 검색 결과 페이지의 ytInitialData를 직접 읽는다(실측 중앙값 1.04s,
// 15–20개, yt-dlp ytsearch 1.7–2.2s보다 빠름). 더 보기는 페이지가 준 continuation으로 /youtubei/v1/search
// (실측 1.1s, +11–13개). 형식이 깨지면 첫 페이지만 yt-dlp ytsearch로 대체한다. TikTok 검색은 로그인 없이
// 결과가 없어(검색 HTML 0개, API 서명 필요, yt-dlp 미지원) 제외했다.

// SearchPage is one page of search results.
type SearchPage struct {
	Items []Item
	More  bool // another page can be fetched
}

// MaxQueryRunes bounds a search query.
const MaxQueryRunes = 100

type search struct {
	pages   [][]Item
	token   string // continuation for the next page ("" = no more)
	version string // INNERTUBE_CLIENT_VERSION of the first page
	visitor string // VISITOR_DATA of the first page
	at      time.Time
}

var (
	clientVersionRe = regexp.MustCompile(`"INNERTUBE_CLIENT_VERSION":"([^"]+)"`)
	visitorDataRe   = regexp.MustCompile(`"VISITOR_DATA":"([^"]+)"`)
)

// NormalizeQuery trims and collapses whitespace and bounds the length.
func NormalizeQuery(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if r := []rune(q); len(r) > MaxQueryRunes {
		q = strings.TrimSpace(string(r[:MaxQueryRunes]))
	}
	return q
}

// Search returns page (0-based) of the YouTube results for query. Pages are fetched in order and
// cached with the list TTL; page n needs page n-1 (패널은 '더 보기'로 순서대로 요청한다).
func (s *Service) Search(ctx context.Context, query string, page int, lister engine.Lister) (SearchPage, error) {
	q := NormalizeQuery(query)
	if q == "" {
		return SearchPage{}, errors.New("empty query")
	}
	key := "search|" + q
	s.mu.Lock()
	st := s.searches[key]
	if st != nil && time.Since(st.at) > s.ttl() {
		st = nil
	}
	if st != nil && page < len(st.pages) {
		res := SearchPage{Items: st.pages[page], More: page < len(st.pages)-1 || st.token != ""}
		s.mu.Unlock()
		s.Rec.Start("search.list", "page", page, "source", "cache", "items", len(res.Items)).End(nil)
		return res, nil
	}
	s.mu.Unlock()

	if page == 0 {
		span := s.Rec.Start("search.list", "page", 0, "source", "page")
		st, err := s.searchPage(ctx, q)
		span.Set("items", itemsOf(st))
		span.End(err)
		if err != nil && ctx.Err() == nil {
			// 결과 페이지 형식이 바뀌었거나 막혔다: yt-dlp 검색으로 첫 페이지만 채운다(더 보기 없음).
			span := s.Rec.Start("search.list", "page", 0, "source", "yt-dlp")
			var items []Item
			items, err = listed(ctx, lister, fmt.Sprintf("ytsearch%d:%s", Limit, q), Seed{})
			span.Set("items", len(items))
			span.End(err)
			st = &search{pages: [][]Item{items}}
		}
		if err != nil {
			return SearchPage{}, err
		}
		st.at = time.Now()
		s.putSearch(key, st)
		return SearchPage{Items: st.pages[0], More: st.token != ""}, nil
	}

	if st == nil || page != len(st.pages) || st.token == "" {
		return SearchPage{}, fmt.Errorf("search page %d is not available (fetch pages in order)", page)
	}
	span := s.Rec.Start("search.list", "page", page, "source", "continuation")
	items, token, err := s.searchMore(ctx, st)
	span.Set("items", len(items))
	span.End(err)
	if err != nil {
		return SearchPage{}, err
	}
	seen := map[string]bool{}
	for _, p := range st.pages {
		for _, it := range p {
			seen[it.ID] = true
		}
	}
	fresh := items[:0]
	for _, it := range items {
		if !seen[it.ID] {
			fresh = append(fresh, it)
		}
	}
	s.mu.Lock()
	if len(st.pages) == page { // 같은 페이지를 동시에 두 번 받았으면 먼저 온 것만 남긴다
		st.pages = append(st.pages, fresh)
		st.token = token
	}
	res := SearchPage{Items: st.pages[page], More: st.token != ""}
	s.mu.Unlock()
	return res, nil
}

func itemsOf(st *search) int {
	if st == nil || len(st.pages) == 0 {
		return 0
	}
	return len(st.pages[0])
}

func (s *Service) searchPage(ctx context.Context, q string) (*search, error) {
	page, err := s.get(ctx, "/results?search_query="+url.QueryEscape(q)+"&hl=ko&gl=KR")
	if err != nil {
		return nil, err
	}
	items, token, err := ParseSearchPage(page)
	if err != nil {
		return nil, err
	}
	st := &search{pages: [][]Item{items}, token: token}
	if m := clientVersionRe.FindSubmatch(page); m != nil {
		st.version = string(m[1])
	}
	if m := visitorDataRe.FindSubmatch(page); m != nil {
		st.visitor = string(m[1])
	}
	if st.version == "" {
		st.token = "" // 더 보기 요청에 필요한 클라이언트 버전이 없다
	}
	return st, nil
}

func (s *Service) searchMore(ctx context.Context, st *search) ([]Item, string, error) {
	body, _ := json.Marshal(map[string]any{
		"context": map[string]any{"client": map[string]any{
			"clientName": "WEB", "clientVersion": st.version, "hl": "ko", "gl": "KR", "visitorData": st.visitor}},
		"continuation": st.token,
	})
	b, err := s.do(ctx, http.MethodPost, "/youtubei/v1/search?prettyPrint=false", body, map[string]string{
		"Content-Type": "application/json", "X-Youtube-Client-Name": "1", "X-Youtube-Client-Version": st.version, "Origin": "https://www.youtube.com"})
	if err != nil {
		return nil, "", err
	}
	return ParseSearchContinuation(b)
}

// ParseSearchPage reads the results and the next-page token from a search results page.
func ParseSearchPage(page []byte) ([]Item, string, error) {
	root, err := initialData(page)
	if err != nil {
		return nil, "", err
	}
	sec, ok := dig(root, "contents", "twoColumnSearchResultsRenderer", "primaryContents", "sectionListRenderer", "contents").([]any)
	if !ok {
		return nil, "", fmt.Errorf("%w: no search results section", ErrFormat)
	}
	return clean(collect(sec), ""), nextToken(sec), nil
}

// ParseSearchContinuation reads one "more results" response.
func ParseSearchContinuation(b []byte) ([]Item, string, error) {
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, "", fmt.Errorf("%w: continuation: %v", ErrFormat, err)
	}
	cmds, ok := root["onResponseReceivedCommands"].([]any)
	if !ok {
		return nil, "", fmt.Errorf("%w: no onResponseReceivedCommands", ErrFormat)
	}
	var all []any
	for _, c := range cmds {
		if items, ok := dig(c, "appendContinuationItemsAction", "continuationItems").([]any); ok {
			all = append(all, items...)
		}
	}
	return clean(collect(all), ""), nextToken(all), nil
}

// nextToken is the page-level continuation: the last continuationItemRenderer of the section list
// (필터·추천 검색어 칩에도 continuationCommand가 있어 아무 토큰이나 쓰면 빈 결과가 온다 — 실측).
func nextToken(items []any) string {
	for i := len(items) - 1; i >= 0; i-- {
		if t := str(dig(items[i], "continuationItemRenderer", "continuationEndpoint", "continuationCommand"), "token"); t != "" {
			return t
		}
	}
	return ""
}

func (s *Service) putSearch(key string, st *search) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.searches == nil {
		s.searches = map[string]*search{}
	}
	s.searches[key] = st
	for len(s.searches) > 32 {
		oldest := ""
		for k, v := range s.searches {
			if oldest == "" || v.at.Before(s.searches[oldest].at) {
				oldest = k
			}
		}
		delete(s.searches, oldest)
	}
}

// get fetches a youtube.com page like a signed-out Korean browser.
func (s *Service) get(ctx context.Context, path string) ([]byte, error) {
	return s.do(ctx, http.MethodGet, path, nil, nil)
}

func (s *Service) do(ctx context.Context, method, path string, body []byte, headers map[string]string) ([]byte, error) {
	base := s.BaseURL
	if base == "" {
		base = "https://www.youtube.com"
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9")
	// EU 동의 화면을 건너뛰는 쿠키(yt-dlp _initialize_consent와 같은 값).
	req.Header.Set("Cookie", "SOCS=CAI")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c := s.HTTP
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s %s: HTTP %d", method, strings.SplitN(path, "?", 2)[0], resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 실측 1.2–1.6MB
}
