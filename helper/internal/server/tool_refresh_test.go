package server

import (
	"context"
	"testing"

	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/tools"
)

type countingTools struct{ checks int }

func (f *countingTools) Status(context.Context) []tools.Status {
	f.checks++
	return []tools.Status{{Tool: tools.Tool{Name: tools.YtDlp, Path: "/x/yt-dlp", Version: "2026.08.19", Source: "installed"}}}
}

func (f *countingTools) Ensure(ctx context.Context, _ tools.Channel) ([]tools.Status, error) {
	return f.Status(ctx), nil
}

func TestProbeRefreshesToolStatus(t *testing.T) {
	hs, srv := newServer(t)
	status := &countingTools{}
	srv.Tools = status
	conn, _, err := dial(t, wsURL(hs, "secret-token"), "file://")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	c := &client{t: t, conn: conn}

	c.send(protocol.TypeHello, "h1", protocol.Hello{Client: "test", Host: protocol.Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}})
	c.next(protocol.TypeHelloOK)
	if status.checks != 1 {
		t.Fatalf("status checks after hello = %d, want 1", status.checks)
	}

	c.send(protocol.TypeMediaProbe, "p1", protocol.MediaProbe{URL: "https://youtu.be/x", MaxHeight: 1080})
	env := c.next(protocol.TypeToolsStatus)
	var got protocol.ToolsStatus
	if err := protocol.DecodeData(env, &got); err != nil {
		t.Fatal(err)
	}
	if status.checks != 2 || got.Installing || len(got.Tools) != 1 {
		t.Fatalf("tool refresh checks=%d status=%+v", status.checks, got)
	}
}
