package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/jobs"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/protocol"
)

// probeEngine answers probes with two variants (VP9 2160p, H.264 1080p); downloads are not used here.
type probeEngine struct{ failingEngine }

func (probeEngine) Probe(_ context.Context, url, workDir string, _ int) (*engine.Info, perf.ProcStats, error) {
	_ = os.MkdirAll(workDir, 0o755)
	p := filepath.Join(workDir, "info.json")
	_ = os.WriteFile(p, []byte(`{}`), 0o644)
	return &engine.Info{ID: "x", Title: "t", Extractor: "Youtube", WebpageURL: url, InfoPath: p,
		Variants: []engine.Variant{{FormatID: "313+140", Res: 2160, VCodec: "vp09.00.51.08"}, {FormatID: "137+140", Res: 1080, VCodec: "avc1.640028"}}}, perf.ProcStats{}, nil
}

func TestMediaProbe(t *testing.T) {
	perftest.Track(t)
	hs, srv := newServer(t)
	srv.Jobs = jobs.New(jobs.Config{}, jobs.Deps{
		Tools:   func(context.Context) (jobs.Toolset, error) { return jobs.Toolset{Engine: probeEngine{}}, nil },
		WorkDir: t.TempDir(), LibraryDir: t.TempDir(), Rec: perftest.Recorder(),
	})
	t.Cleanup(srv.Jobs.Close)
	conn, _, err := dial(t, wsURL(hs, "secret-token"), "file://")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	c := &client{t: t, conn: conn}

	c.send(protocol.TypeMediaProbe, "p1", protocol.MediaProbe{URL: "https://youtu.be/x"})
	env := c.next(protocol.TypeMediaInfo)
	var mi protocol.MediaInfo
	if err := protocol.DecodeData(env, &mi); err != nil {
		t.Fatal(err)
	}
	// hello 전이면 compat 프로필: VP9는 변환, H.264는 그대로.
	if env.ID != "p1" || mi.ProbeID == "" || mi.Info == nil || mi.Failure != nil || len(mi.Info.Variants) != 2 ||
		!mi.Info.Variants[0].Transcode || mi.Info.Variants[1].Transcode {
		t.Fatalf("media.info id=%s %+v", env.ID, mi)
	}

	// 실패는 error가 아니라 분류된 failure로 돌아온다(패널이 메시지·정책을 그대로 쓴다).
	srv.Jobs = jobs.New(jobs.Config{}, jobs.Deps{
		Tools:   func(context.Context) (jobs.Toolset, error) { return jobs.Toolset{Engine: failingEngine{}}, nil },
		WorkDir: t.TempDir(), LibraryDir: t.TempDir(), Rec: perftest.Recorder(),
	})
	t.Cleanup(srv.Jobs.Close)
	c.send(protocol.TypeMediaProbe, "p2", protocol.MediaProbe{URL: "https://youtu.be/private"})
	env = c.next(protocol.TypeMediaInfo)
	mi = protocol.MediaInfo{}
	_ = protocol.DecodeData(env, &mi)
	if env.ID != "p2" || mi.Failure == nil || mi.Failure.Kind != engine.KindPrivate || mi.Info != nil {
		t.Fatalf("failed probe %+v", mi)
	}

	c.send(protocol.TypeMediaProbe, "p3", protocol.MediaProbe{URL: "file:///etc/passwd"})
	env = c.next(protocol.TypeMediaInfo)
	mi = protocol.MediaInfo{}
	_ = protocol.DecodeData(env, &mi)
	if mi.Failure == nil || mi.Failure.Kind != engine.KindUnsupportedURL {
		t.Fatalf("bad url probe %+v", mi)
	}
}
