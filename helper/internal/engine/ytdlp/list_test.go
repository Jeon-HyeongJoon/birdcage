package ytdlp

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// 녹화한 yt-dlp 2026.08.19 출력(2026-10-04, 앞 4개만 남김): ytsearch30:Me at the zoo, 채널 /videos.
func TestParseListRecorded(t *testing.T) {
	b, err := os.ReadFile("testdata/search.flat.json")
	if err != nil {
		t.Fatal(err)
	}
	es, err := ParseList(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 4 {
		t.Fatalf("entries %d, want 4", len(es))
	}
	for _, e := range es {
		if e.ID == "" || e.Title == "" || e.Channel == "" || e.DurationSec <= 0 || !strings.HasPrefix(e.Thumbnail, "https://i.ytimg.com/") {
			t.Errorf("incomplete entry %+v", e)
		}
		if e.URL != "https://www.youtube.com/watch?v="+e.ID {
			t.Errorf("url %q", e.URL)
		}
	}

	b, err = os.ReadFile("testdata/channel.flat.json")
	if err != nil {
		t.Fatal(err)
	}
	es, err = ParseList(b)
	if err != nil || len(es) != 1 || es[0].ID != "jNQXAC9IVRw" || es[0].DurationSec != 19 {
		t.Fatalf("channel listing %+v %v", es, err)
	}
}

func TestParseListSkipsNonVideos(t *testing.T) {
	es, err := ParseList([]byte(`{"entries":[
		{"id":"UCx","title":"채널","ie_key":"YoutubeTab","url":"https://www.youtube.com/channel/UCx"},
		{"id":"live1","title":"생방송","ie_key":"Youtube","live_status":"is_live"},
		{"id":"","title":"빈 id"},
		{"id":"v1","title":"영상","ie_key":"Youtube","duration":61,"view_count":1200}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, e := range es {
		ids = append(ids, e.ID)
	}
	if !slices.Equal(ids, []string{"live1", "v1"}) || !es[0].Live || es[1].Views != 1200 {
		t.Fatalf("entries %+v", es)
	}
	if _, err := ParseList([]byte(`<html>`)); err == nil {
		t.Fatal("accepted non-JSON")
	}
}

func TestListArgs(t *testing.T) {
	got := strings.Join((&Engine{YtDlp: "y"}).ListArgs("ytsearch30:q", 30), " ")
	if got != "--ignore-config --no-color --flat-playlist -J --playlist-end 30 --extractor-args youtube:lang=ko -- ytsearch30:q" {
		t.Fatal(got)
	}
}
