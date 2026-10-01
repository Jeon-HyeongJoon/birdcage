package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/jobs"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/tools"
)

func TestMain(m *testing.M) { perftest.Main(m) }

type fakeTools struct{ ensured int }

func (f *fakeTools) Status(context.Context) []tools.Status {
	return []tools.Status{{Tool: tools.Tool{Name: tools.YtDlp, Path: "/x/yt-dlp", Version: "2026.08.19", Source: "installed"}}}
}

func (f *fakeTools) Ensure(ctx context.Context, ch tools.Channel) ([]tools.Status, error) {
	f.ensured++
	return f.Status(ctx), nil
}

// failingEngine fails every probe as private so job flow can be tested without tools.
type failingEngine struct{}

func (failingEngine) Name() string { return "fake" }
func (failingEngine) Probe(context.Context, string, string, int) (*engine.Info, perf.ProcStats, error) {
	return nil, perf.ProcStats{}, engine.Fail(engine.KindPrivate, "ERROR: Private video")
}
func (failingEngine) Download(context.Context, engine.DownloadRequest, func(engine.Progress)) (*engine.DownloadResult, error) {
	return nil, nil
}

func newServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	mgr := jobs.New(jobs.Config{}, jobs.Deps{
		Tools:   func(context.Context) (jobs.Toolset, error) { return jobs.Toolset{Engine: failingEngine{}}, nil },
		WorkDir: t.TempDir(), LibraryDir: t.TempDir(), Rec: perftest.Recorder(),
	})
	t.Cleanup(mgr.Close)
	s := &Server{Token: "secret-token", Version: "test", LibraryDir: "/lib", DefaultHeight: 1080, Jobs: mgr, Tools: &fakeTools{}, Rec: perftest.Recorder()}
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(hs.Close)
	return hs, s
}

func wsURL(hs *httptest.Server, token string) string {
	return "ws" + strings.TrimPrefix(hs.URL, "http") + "/ws?token=" + token
}

func dial(t *testing.T, url, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	return websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
}

func TestAuth(t *testing.T) {
	hs, _ := newServer(t)
	cases := []struct {
		name, token, origin string
		status              int // 0 = must succeed
	}{
		{"no token", "", "", http.StatusUnauthorized},
		{"wrong token", "nope", "", http.StatusUnauthorized},
		{"web page origin", "secret-token", "https://evil.example", http.StatusForbidden},
		{"localhost page origin", "secret-token", "http://localhost:3000", http.StatusForbidden},
		{"non-browser client", "secret-token", "", 0},
		{"CEP file origin", "secret-token", "file://", 0},
		{"null origin", "secret-token", "null", 0},
	}
	for _, c := range cases {
		conn, resp, err := dial(t, wsURL(hs, c.token), c.origin)
		if c.status == 0 {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
				continue
			}
			conn.Close(websocket.StatusNormalClosure, "")
			continue
		}
		if err == nil {
			conn.Close(websocket.StatusNormalClosure, "")
			t.Errorf("%s: connected, want %d", c.name, c.status)
			continue
		}
		if resp == nil || resp.StatusCode != c.status {
			t.Errorf("%s: resp %v err %v, want %d", c.name, resp, err, c.status)
		}
	}
}

type client struct {
	t    *testing.T
	conn *websocket.Conn
}

func (c *client) send(typ, id string, data any) {
	c.t.Helper()
	b, err := protocol.Encode(typ, id, data)
	if err != nil {
		c.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

// next reads until a message of one of the types arrives.
func (c *client) next(types ...string) protocol.Envelope {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		_, b, err := c.conn.Read(ctx)
		if err != nil {
			c.t.Fatalf("read waiting for %v: %v", types, err)
		}
		env, err := protocol.Decode(b)
		if err != nil {
			c.t.Fatal(err)
		}
		for _, typ := range types {
			if env.Type == typ {
				return env
			}
		}
	}
}

func TestHelloAndJobFlow(t *testing.T) {
	perftest.Track(t)
	hs, srv := newServer(t)
	conn, _, err := dial(t, wsURL(hs, "secret-token"), "file://")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	c := &client{t: t, conn: conn}

	c.send(protocol.TypeHello, "h1", protocol.Hello{Client: "test", Host: protocol.Host{App: "PPRO", Version: "26.1.0", API: "uxp", OS: "mac"}})
	env := c.next(protocol.TypeHelloOK)
	var ok protocol.HelloOK
	if err := protocol.DecodeData(env, &ok); err != nil {
		t.Fatal(err)
	}
	if env.ID != "h1" || ok.Profile != "modern" || ok.Protocol != protocol.Version || len(ok.Tools) != 1 {
		t.Fatalf("hello.ok %+v id=%s", ok, env.ID)
	}

	c.send(protocol.TypeJobCreate, "j1", protocol.JobCreate{URL: "https://youtu.be/x"})
	env = c.next(protocol.TypeJobAccepted)
	var acc protocol.JobAccepted
	_ = protocol.DecodeData(env, &acc)
	if env.ID != "j1" || acc.JobID == "" {
		t.Fatalf("accepted %+v", acc)
	}
	env = c.next(protocol.TypeJobFailed)
	var ev protocol.JobEvent
	_ = protocol.DecodeData(env, &ev)
	if ev.Job.JobID != acc.JobID || ev.Job.Failure.Kind != engine.KindPrivate || ev.Job.Options.MaxHeight != 1080 || ev.Job.Options.Profile != "modern" {
		t.Fatalf("failed event %+v", ev.Job)
	}

	c.send(protocol.TypeJobCreate, "j2", protocol.JobCreate{URL: "file:///etc/passwd"})
	env = c.next(protocol.TypeError)
	if env.ID != "j2" {
		t.Fatalf("error id %s", env.ID)
	}

	c.send(protocol.TypeToolsEnsure, "t1", nil)
	for {
		env = c.next(protocol.TypeToolsStatus)
		var st protocol.ToolsStatus
		_ = json.Unmarshal(env.Data, &st)
		if !st.Installing {
			break
		}
	}
	if srv.Tools.(*fakeTools).ensured != 1 {
		t.Fatal("ensure not called")
	}

	c.send("nope", "x1", nil)
	if env = c.next(protocol.TypeError); env.ID != "x1" {
		t.Fatalf("unknown type error id %s", env.ID)
	}
}

func TestHealthz(t *testing.T) {
	hs, _ := newServer(t)
	resp, err := http.Get(hs.URL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", resp, err)
	}
	resp.Body.Close()
}
