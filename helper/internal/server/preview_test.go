package server

import (
	"context"
	"path/filepath"
	"testing"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/jobs"
	"birdcage/helper/internal/media"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/preview"
	"birdcage/helper/internal/protocol"
)

// downloadFailEngine probes like probeEngine but every download fails (rate limited).
type downloadFailEngine struct{ probeEngine }

func (downloadFailEngine) Download(context.Context, engine.DownloadRequest, func(engine.Progress)) (*engine.DownloadResult, error) {
	return nil, engine.Fail(engine.KindRateLimited, "HTTP Error 429")
}

func TestPreviewMessages(t *testing.T) {
	perftest.Track(t)
	hs, srv := newServer(t)
	srv.Jobs = jobs.New(jobs.Config{}, jobs.Deps{
		Tools: func(context.Context) (jobs.Toolset, error) {
			return jobs.Toolset{Engine: downloadFailEngine{}, Prober: &media.Prober{FFprobe: "ffprobe"}, Normalizer: &media.Normalizer{FFmpeg: "ffmpeg"}}, nil
		},
		WorkDir: t.TempDir(), LibraryDir: t.TempDir(), Rec: perftest.Recorder(),
	})
	t.Cleanup(srv.Jobs.Close)
	srv.Preview = &preview.Service{Cache: preview.Cache{Root: filepath.Join(t.TempDir(), "preview")}, Rec: perftest.Recorder()}
	conn, _, err := dial(t, wsURL(hs, "secret-token"), "file://")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	c := &client{t: t, conn: conn}

	// 모르는(만료된) 조회 → 요청 id로 실패 상태.
	c.send(protocol.TypePreviewOpen, "v0", protocol.PreviewOpen{ProbeID: "nope"})
	env := c.next(protocol.TypePreviewState)
	var st preview.State
	_ = protocol.DecodeData(env, &st)
	if env.ID != "v0" || st.Stage != preview.StageFailed || st.ProbeID != "nope" {
		t.Fatalf("unknown probe: id=%s %+v", env.ID, st)
	}

	c.send(protocol.TypeMediaProbe, "p1", protocol.MediaProbe{URL: "https://youtu.be/x"})
	var mi protocol.MediaInfo
	_ = protocol.DecodeData(c.next(protocol.TypeMediaInfo), &mi)
	c.send(protocol.TypePreviewOpen, "v1", protocol.PreviewOpen{ProbeID: mi.ProbeID})
	env = c.next(protocol.TypePreviewState)
	st = preview.State{}
	_ = protocol.DecodeData(env, &st)
	if env.ID != "v1" || st.Stage != preview.StageAudio || st.ProbeID != mi.ProbeID {
		t.Fatalf("first state: id=%s %+v", env.ID, st)
	}
	// 다운로드 실패는 분류된 메시지로 끝난다(작업과 같은 실패 문구).
	for st.Stage != preview.StageFailed {
		env = c.next(protocol.TypePreviewState)
		st = preview.State{}
		_ = protocol.DecodeData(env, &st)
	}
	if env.ID != "" || st.Error != engine.MessageFor(engine.KindRateLimited) {
		t.Fatalf("failed state id=%q %+v", env.ID, st)
	}
	c.send(protocol.TypePreviewClose, "x", protocol.PreviewClose{ProbeID: mi.ProbeID}) // 끝난 뒤 닫아도 문제없다
}
