package related

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"birdcage/helper/internal/engine"
)

// 계약 테스트: 2026-10-04에 녹화한 실제 검색 결과(인터뷰 b-roll, hl=ko): 쇼츠 묶음 1개 + 영상 4개 + 더 보기 토큰.
func TestParseSearchPageRecorded(t *testing.T) {
	page, err := os.ReadFile("testdata/search_interview-b-roll.html")
	if err != nil {
		t.Fatal(err)
	}
	items, token, err := ParseSearchPage(page)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, it := range items {
		ids = append(ids, it.ID)
		if it.Title == "" || it.Channel == "" || it.DurationSec <= 0 || it.Meta == "" || !strings.HasPrefix(it.Thumbnail, "https://i.ytimg.com/vi/"+it.ID+"/") {
			t.Errorf("incomplete %+v", it)
		}
	}
	if strings.Join(ids, ",") != "I0-g22cY3WY,O_9pvEBwrQU,PvmtnlPZYdQ,EB-B0VzAtLk" { // 쇼츠는 빠진다
		t.Fatalf("ids %v", ids)
	}
	if items[0].Channel != "빅제임스 [Big James]" || items[0].DurationSec != 346 || items[0].Meta != "조회수 6,938회 · 8년 전" {
		t.Errorf("first %+v", items[0])
	}
	if !strings.Contains(items[0].Thumbnail, "hqdefault") && !strings.Contains(items[0].Thumbnail, "hq720") {
		t.Errorf("thumbnail %q", items[0].Thumbnail)
	}
	if len(token) < 100 {
		t.Fatalf("continuation token %q", token)
	}

	b, err := os.ReadFile("testdata/search_interview-b-roll.cont.json")
	if err != nil {
		t.Fatal(err)
	}
	more, next, err := ParseSearchContinuation(b)
	if err != nil || len(more) != 3 || next == "" || next == token {
		t.Fatalf("continuation: %d items, next %q, err %v", len(more), next, err)
	}
	if _, _, err := ParseSearchContinuation([]byte(`{"responseContext":{}}`)); !errors.Is(err, ErrFormat) {
		t.Fatalf("unknown continuation err %v", err)
	}
	if _, _, err := ParseSearchPage([]byte(`var ytInitialData = {"contents":{}};`)); !errors.Is(err, ErrFormat) {
		t.Fatalf("unknown page err %v", err)
	}
}

func TestNormalizeQuery(t *testing.T) {
	for in, want := range map[string]string{"  인터뷰   b-roll \n": "인터뷰 b-roll", "": "", strings.Repeat("가", 150): strings.Repeat("가", MaxQueryRunes)} {
		if got := NormalizeQuery(in); got != want {
			t.Errorf("NormalizeQuery(%q) = %q", in, got)
		}
	}
}

// 페이지 → 더 보기(같은 클라이언트 버전·방문자 값과 페이지 토큰으로 POST) → 캐시, 형식이 깨지면 yt-dlp로 대체.
func TestSearchPagesCacheAndFallback(t *testing.T) {
	page, _ := os.ReadFile("testdata/search_interview-b-roll.html")
	cont, _ := os.ReadFile("testdata/search_interview-b-roll.cont.json")
	_, token, _ := ParseSearchPage(page)
	var pageHits, moreHits atomic.Int32
	broken := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/results":
			pageHits.Add(1)
			if r.URL.Query().Get("search_query") != "인터뷰 b-roll" || r.URL.Query().Get("hl") != "ko" {
				t.Errorf("results query %s", r.URL.RawQuery)
			}
			if broken {
				_, _ = w.Write([]byte(`<html>새 형식</html>`))
				return
			}
			_, _ = w.Write(page)
		case "/youtubei/v1/search":
			moreHits.Add(1)
			var body struct {
				Context struct {
					Client map[string]string `json:"client"`
				} `json:"context"`
				Continuation string `json:"continuation"`
			}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
			if body.Continuation != token || body.Context.Client["clientVersion"] == "" || body.Context.Client["visitorData"] != "CgtURVNUVklTSVRPUg%3D%3D" ||
				r.Header.Get("X-Youtube-Client-Version") != body.Context.Client["clientVersion"] {
				t.Errorf("continuation request %s headers %v", b, r.Header)
			}
			_, _ = w.Write(cont)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	lister := &fakeLister{entries: map[string][]engine.Entry{"ytsearch30:": {{ID: "y1", Title: "yt-dlp 결과", Channel: "C", DurationSec: 5}}}}
	s := &Service{HTTP: srv.Client(), BaseURL: srv.URL}
	ctx := context.Background()

	p0, err := s.Search(ctx, " 인터뷰  b-roll ", 0, lister)
	if err != nil || len(p0.Items) != 4 || !p0.More {
		t.Fatalf("page 0: %v %+v", err, p0)
	}
	if _, err := s.Search(ctx, "인터뷰 b-roll", 2, lister); err == nil {
		t.Fatal("page 2 before page 1")
	}
	p1, err := s.Search(ctx, "인터뷰 b-roll", 1, lister)
	if err != nil || len(p1.Items) != 3 || !p1.More {
		t.Fatalf("page 1: %v %+v", err, p1)
	}
	if again, _ := s.Search(ctx, "인터뷰 b-roll", 1, lister); len(again.Items) != 3 || pageHits.Load() != 1 || moreHits.Load() != 1 {
		t.Fatalf("cache: page %d more %d", pageHits.Load(), moreHits.Load())
	}

	broken = true
	s = &Service{HTTP: srv.Client(), BaseURL: srv.URL}
	p0, err = s.Search(ctx, "인터뷰 b-roll", 0, lister)
	if err != nil || len(p0.Items) != 1 || p0.Items[0].ID != "y1" || p0.More || lister.calls[0] != "ytsearch30:인터뷰 b-roll" {
		t.Fatalf("fallback: %v %+v %v", err, p0, lister.calls)
	}
	if _, err := s.Search(ctx, "   ", 0, lister); err == nil {
		t.Fatal("empty query accepted")
	}
}
