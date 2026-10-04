package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"birdcage/helper/internal/jobs"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/related"
)

func TestSearchFetch(t *testing.T) {
	perftest.Track(t)
	page, err := os.ReadFile("../related/testdata/search_interview-b-roll.html")
	if err != nil {
		t.Fatal(err)
	}
	cont, _ := os.ReadFile("../related/testdata/search_interview-b-roll.cont.json")
	yt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/youtubei/v1/search" {
			_, _ = w.Write(cont)
			return
		}
		_, _ = w.Write(page)
	}))
	defer yt.Close()

	hs, srv := newServer(t)
	srv.Jobs = jobs.New(jobs.Config{}, jobs.Deps{
		Tools:   func(context.Context) (jobs.Toolset, error) { return jobs.Toolset{Engine: listingEngine{}}, nil },
		WorkDir: t.TempDir(), LibraryDir: t.TempDir(), Rec: perftest.Recorder(),
	})
	t.Cleanup(srv.Jobs.Close)
	srv.Related = &related.Service{HTTP: yt.Client(), BaseURL: yt.URL, Rec: perftest.Recorder()}
	conn, _, err := dial(t, wsURL(hs, "secret-token"), "file://")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	c := &client{t: t, conn: conn}
	search := func(id string, sf protocol.SearchFetch) protocol.SearchList {
		t.Helper()
		c.send(protocol.TypeSearchFetch, id, sf)
		env := c.next(protocol.TypeSearchList)
		var sl protocol.SearchList
		if err := protocol.DecodeData(env, &sl); err != nil || env.ID != id {
			t.Fatalf("search.list id=%s err=%v", env.ID, err)
		}
		return sl
	}

	if sl := search("s0", protocol.SearchFetch{Query: "  "}); sl.Error == "" || sl.Items == nil {
		t.Fatalf("empty query %+v", sl)
	}
	sl := search("s1", protocol.SearchFetch{Query: " 인터뷰   b-roll "})
	if sl.Error != "" || sl.Query != "인터뷰 b-roll" || sl.Page != 0 || len(sl.Items) != 4 || !sl.More {
		t.Fatalf("page 0 %+v", sl)
	}
	sl = search("s2", protocol.SearchFetch{Query: "인터뷰 b-roll", Page: 1})
	if sl.Error != "" || sl.Page != 1 || len(sl.Items) != 3 || !sl.More {
		t.Fatalf("page 1 %+v", sl)
	}
	if sl := search("s3", protocol.SearchFetch{Query: "다른 검색어", Page: 3}); sl.Error == "" {
		t.Fatalf("out-of-order page accepted %+v", sl)
	}
}
