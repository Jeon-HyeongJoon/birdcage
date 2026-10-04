package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/jobs"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/related"
)

// listingEngine is a probeEngine that can also list channels and searches.
type listingEngine struct{ probeEngine }

func (listingEngine) List(_ context.Context, url string, _ int) ([]engine.Entry, error) {
	return []engine.Entry{{ID: "c1", Title: "같은 채널 영상", DurationSec: 30}}, nil
}

func TestRelatedFetch(t *testing.T) {
	perftest.Track(t)
	page, err := os.ReadFile("../related/testdata/watch_jNQXAC9IVRw.html")
	if err != nil {
		t.Fatal(err)
	}
	status := http.StatusOK
	yt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
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

	list := func(id string, rf protocol.RelatedFetch) protocol.RelatedList {
		t.Helper()
		c.send(protocol.TypeRelatedFetch, id, rf)
		env := c.next(protocol.TypeRelatedList)
		var rl protocol.RelatedList
		if err := protocol.DecodeData(env, &rl); err != nil || env.ID != id {
			t.Fatalf("related.list id=%s err=%v", env.ID, err)
		}
		return rl
	}

	if rl := list("r0", protocol.RelatedFetch{ProbeID: "p-unknown"}); rl.Error == "" || rl.Items == nil || len(rl.Items) != 0 {
		t.Fatalf("unknown probe %+v", rl)
	}

	c.send(protocol.TypeMediaProbe, "p1", protocol.MediaProbe{URL: "https://youtu.be/x", Via: "related:youtube"})
	var mi protocol.MediaInfo
	_ = protocol.DecodeData(c.next(protocol.TypeMediaInfo), &mi)

	rl := list("r1", protocol.RelatedFetch{ProbeID: mi.ProbeID})
	if rl.Error != "" || rl.Requested != related.SourceYouTube || rl.Source != related.SourceYouTube || len(rl.Items) != 4 {
		t.Fatalf("youtube list %+v", rl)
	}
	// 시청 페이지가 막히면 같은 채널 목록으로 대체하고, 실제 출처를 알려 준다(probeEngine에는 채널 URL이 없어 검색으로).
	status = http.StatusTooManyRequests
	srv.Related = &related.Service{HTTP: yt.Client(), BaseURL: yt.URL}
	rl = list("r2", protocol.RelatedFetch{ProbeID: mi.ProbeID, Source: related.SourceYouTube})
	if rl.Error != "" || rl.Requested != related.SourceYouTube || rl.Source != related.SourceSearch || len(rl.Items) != 1 || rl.Items[0].ID != "c1" {
		t.Fatalf("fallback list %+v", rl)
	}
}
