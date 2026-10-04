package related

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/perf/perftest"
)

func TestMain(m *testing.M) { perftest.Main(m) }

// 계약 테스트: 2026-10-04에 녹화한 실제 시청 페이지(jNQXAC9IVRw, hl=ko, 앞 4개 항목만 남김).
func TestParseWatchPageRecorded(t *testing.T) {
	page, err := os.ReadFile("testdata/watch_jNQXAC9IVRw.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := ParseWatchPage(page, "jNQXAC9IVRw")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("items %d, want 4", len(items))
	}
	want := Item{ID: "eoisNyKSBic", URL: "https://www.youtube.com/watch?v=eoisNyKSBic", Title: "'우사기' 는 사실 모든 것을 알고 있었습니다",
		Channel: "김채호의 필름찢기", DurationSec: 14*60 + 31, Meta: "조회수 7.8만회 · 4시간 전"}
	got := items[0]
	if !strings.HasPrefix(got.Thumbnail, "https://i.ytimg.com/vi/eoisNyKSBic/") || !strings.Contains(got.Thumbnail, "sqp=-oaymwEcCNACELwBSFXyq4qpAw4IARUAAIhCGAFwAcABBg") {
		t.Errorf("thumbnail %q, want the 336px source", got.Thumbnail)
	}
	got.Thumbnail = ""
	if got != want {
		t.Errorf("first item\n got %+v\nwant %+v", got, want)
	}
	for _, it := range items {
		if it.Title == "" || it.Channel == "" || it.DurationSec <= 0 || it.Meta == "" || it.Thumbnail == "" {
			t.Errorf("incomplete %+v", it)
		}
	}
}

// 형식 변화 대비: 과거 형식(compactVideoRenderer)과 걸러야 할 항목. 녹화본이 없어 공개된 과거 구조로 만든 합성 데이터.
func TestParseWatchPageLegacyAndFilters(t *testing.T) {
	page := `<script>var ytInitialData = {"contents":{"twoColumnWatchNextResults":{"secondaryResults":{"secondaryResults":{"results":[
	{"compactVideoRenderer":{"videoId":"old1","title":{"simpleText":"옛 형식"},"longBylineText":{"runs":[{"text":"채널A"}]},
		"lengthText":{"simpleText":"1:02:03"},"viewCountText":{"simpleText":"조회수 12회"},"publishedTimeText":{"simpleText":"1년 전"},
		"thumbnail":{"thumbnails":[{"url":"https://i.ytimg.com/vi/old1/a.jpg","width":168},{"url":"https://i.ytimg.com/vi/old1/b.jpg","width":336}]}}},
	{"compactVideoRenderer":{"videoId":"live2","title":{"runs":[{"text":"생방송"}]},"badges":[{"metadataBadgeRenderer":{"style":"BADGE_STYLE_TYPE_LIVE_NOW"}}]}},
	{"lockupViewModel":{"contentType":"LOCKUP_CONTENT_TYPE_PLAYLIST","contentId":"PL1","metadata":{"lockupMetadataViewModel":{"title":{"content":"믹스"}}}}},
	{"lockupViewModel":{"contentType":"LOCKUP_CONTENT_TYPE_VIDEO","contentId":"seed","metadata":{"lockupMetadataViewModel":{"title":{"content":"지금 영상"}}}}},
	{"lockupViewModel":{"contentType":"LOCKUP_CONTENT_TYPE_VIDEO","contentId":"live3","metadata":{"lockupMetadataViewModel":{"title":{"content":"라이브"}}},
		"contentImage":{"thumbnailViewModel":{"overlays":[{"thumbnailBottomOverlayViewModel":{"badges":[{"thumbnailBadgeViewModel":{"text":"실시간","badgeStyle":"THUMBNAIL_OVERLAY_BADGE_STYLE_LIVE"}}]}}]}}}},
	{"lockupViewModel":{"contentType":"LOCKUP_CONTENT_TYPE_VIDEO","contentId":"new4","metadata":{"lockupMetadataViewModel":{"title":{"content":"새 형식"}}}}},
	{"lockupViewModel":{"contentType":"LOCKUP_CONTENT_TYPE_VIDEO","contentId":"new4","metadata":{"lockupMetadataViewModel":{"title":{"content":"중복"}}}}}
	]}}}}};</script>`
	items, err := ParseWatchPage([]byte(page), "seed")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "old1" || items[1].ID != "new4" {
		t.Fatalf("items %+v", items)
	}
	old := items[0]
	if old.Title != "옛 형식" || old.Channel != "채널A" || old.DurationSec != 3723 || old.Meta != "조회수 12회 · 1년 전" || old.Thumbnail != "https://i.ytimg.com/vi/old1/b.jpg" {
		t.Errorf("legacy item %+v", old)
	}
	if items[1].Thumbnail != "https://i.ytimg.com/vi/new4/mqdefault.jpg" {
		t.Errorf("fallback thumbnail %q", items[1].Thumbnail)
	}
}

func TestParseWatchPageUnknownFormat(t *testing.T) {
	for name, page := range map[string]string{
		"no data":      `<html>consent.youtube.com</html>`,
		"broken json":  `var ytInitialData = {"contents": ;`,
		"no secondary": `var ytInitialData = {"contents":{"twoColumnWatchNextResults":{}}};`,
	} {
		if _, err := ParseWatchPage([]byte(page), "x"); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err %v, want ErrFormat", name, err)
		}
	}
}

type fakeLister struct {
	calls   []string
	entries map[string][]engine.Entry
	err     error
}

func (f *fakeLister) List(_ context.Context, u string, limit int) ([]engine.Entry, error) {
	f.calls = append(f.calls, u)
	if f.err != nil {
		return nil, f.err
	}
	for prefix, es := range f.entries {
		if strings.HasPrefix(u, prefix) {
			return es, nil
		}
	}
	return nil, nil
}

func TestListFallsBackAndCaches(t *testing.T) {
	var hits atomic.Int32
	page, _ := os.ReadFile("testdata/watch_jNQXAC9IVRw.html")
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("hl") != "ko" || !strings.Contains(r.Header.Get("Cookie"), "SOCS=CAI") {
			t.Errorf("request %s cookie %q", r.URL, r.Header.Get("Cookie"))
		}
		w.WriteHeader(status)
		_, _ = w.Write(page)
	}))
	defer srv.Close()
	seed := Seed{ID: "jNQXAC9IVRw", Title: "Me at the zoo", Channel: "jawed", ChannelURL: "https://www.youtube.com/channel/UC4QobU6STFB0P71PMvOGN5A"}
	lister := &fakeLister{entries: map[string][]engine.Entry{
		"https://www.youtube.com/channel/": {{ID: "other", Title: "Me at the zoo"}},
		"ytsearch30:":                      {{ID: "s1", Title: "검색 결과", Channel: "B", DurationSec: 10}, {ID: "s2", Title: "생방송", Live: true}},
	}}
	s := &Service{HTTP: srv.Client(), BaseURL: srv.URL}
	ctx := context.Background()

	r, err := s.List(ctx, seed, SourceYouTube, lister)
	if err != nil || r.Source != SourceYouTube || len(r.Items) != 4 {
		t.Fatalf("youtube: %v %+v", err, r)
	}
	if _, _ = s.List(ctx, seed, SourceYouTube, lister); hits.Load() != 1 {
		t.Fatalf("cache miss: %d page fetches", hits.Load())
	}

	// 시청 페이지가 깨지면 같은 채널 → (채널에 이 영상뿐이면) 제목 검색으로 넘어간다.
	status = http.StatusTooManyRequests
	seed.ID = "other"
	r, err = s.List(ctx, seed, SourceYouTube, lister)
	if err != nil || r.Source != SourceSearch || len(r.Items) != 1 || r.Items[0].ID != "s1" {
		t.Fatalf("fallback: %v %+v (calls %v)", err, r, lister.calls)
	}
	if lister.calls[0] != seed.ChannelURL+"/videos" || lister.calls[1] != "ytsearch30:Me at the zoo" {
		t.Fatalf("calls %v", lister.calls)
	}

	// 모두 실패하면 오류(출처별 원인 포함).
	lister.err = errors.New("yt-dlp broke")
	seed.ID = "third"
	if _, err := s.List(ctx, seed, SourceYouTube, lister); err == nil || !strings.Contains(err.Error(), "HTTP 429") || !strings.Contains(err.Error(), "yt-dlp broke") {
		t.Fatalf("err %v", err)
	}
	// yt-dlp가 없으면 youtube 출처만 쓸 수 있다.
	if _, err := s.List(ctx, seed, SourceChannel, nil); err == nil {
		t.Fatal("channel without lister")
	}
}

func TestQuery(t *testing.T) {
	for _, c := range []struct{ title, channel, want string }{
		{"Me at the zoo", "jawed", "Me at the zoo"},
		{"동성애자는 동성애자로 태어나는 걸까?｜인류 진화의 최대 미스터리, 동성애의 모든 것", "과학드림", "동성애자는 동성애자로 태어나는 걸까"},
		{"[MV] 아이유 - 밤편지 (Through the Night) #shorts", "1theK", "아이유 밤편지"}, // 4자 미만 조각은 자르지 않는다
		{"침착맨 레전드 모음 【침착맨】", "침착맨", "레전드 모음"},
		{"A B C D E F G H I J", "", "A B C D E F G H"},
		{"!!!", "", ""},
	} {
		if got := Query(c.title, c.channel); got != c.want {
			t.Errorf("Query(%q, %q) = %q, want %q", c.title, c.channel, got, c.want)
		}
	}
}

func TestSeedOf(t *testing.T) {
	if _, ok := SeedOf(&engine.Info{ID: "x", Extractor: "TikTok"}); ok {
		t.Fatal("tiktok seed")
	}
	s, ok := SeedOf(&engine.Info{ID: "x", Extractor: "Youtube", Title: "t", Uploader: "u", ChannelURL: "c"})
	if !ok || s != (Seed{ID: "x", Title: "t", Channel: "u", ChannelURL: "c"}) {
		t.Fatalf("%+v", s)
	}
}
