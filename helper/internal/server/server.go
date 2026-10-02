// Package server exposes the helper to panels over a loopback WebSocket.
//
// 보안(조사에서 확인한 흔한 실수를 피함):
//   - 127.0.0.1에만 바인딩(0.0.0.0 금지)
//   - 실행마다 새로 만든 토큰을 쿼리(?token=)로 요구, 상수시간 비교
//   - Origin 허용목록: 없음(비브라우저), "null"/file://(CEP 패널). 일반 웹페이지 Origin은 거부
//     → 브라우저의 임의 웹페이지가 ws://127.0.0.1 에 붙어 다운로드를 시키는 것을 막는다.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/jobs"
	"birdcage/helper/internal/media"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/tools"
)

// ToolManager is what the server needs from the tools layer.
type ToolManager interface {
	Status(ctx context.Context) []tools.Status
	Ensure(ctx context.Context, ch tools.Channel) ([]tools.Status, error)
}

// Server handles /ws and /healthz.
type Server struct {
	Token          string
	Version        string
	LibraryDir     string
	DefaultHeight  int
	AllowedOrigins []string // extra exact origins (e.g. UXP later)
	Jobs           *jobs.Manager
	Tools          ToolManager
	Rec            *perf.Recorder
	Log            *slog.Logger

	ensureMu sync.Mutex // 동시에 여러 설치 요청이 와도 한 번만 실행
	sessions sync.WaitGroup
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/ws", s.serveWS)
	return mux
}

// Wait blocks until all sessions have ended.
func (s *Server) Wait() { s.sessions.Wait() }

func (s *Server) originAllowed(origin string) bool {
	switch {
	case origin == "", origin == "null", strings.HasPrefix(origin, "file://"):
		return true
	}
	for _, o := range s.AllowedOrigins {
		if origin == o {
			return true
		}
	}
	return false
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) != 1 {
		s.log().Warn("ws rejected: bad token", "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if origin := r.Header.Get("Origin"); !s.originAllowed(origin) {
		s.log().Warn("ws rejected: origin", "origin", origin)
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	// Origin은 위에서 직접 검증했으므로 라이브러리의 같은 호스트 검사는 끈다(file:// Origin 허용 목적).
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	s.sessions.Add(1)
	defer s.sessions.Done()
	sess := &session{srv: s, conn: c, log: s.log().With("session", time.Now().UnixNano())}
	sess.run(r.Context())
}

func (s *Server) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.New(slog.DiscardHandler)
}

type session struct {
	srv     *Server
	conn    *websocket.Conn
	log     *slog.Logger
	writeMu sync.Mutex
	profile media.Profile
}

func (ss *session) run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer ss.conn.CloseNow()

	events, unsubscribe := ss.srv.Jobs.Subscribe()
	defer unsubscribe()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				typ := protocol.TypeJobUpdate
				switch ev.Type {
				case "done":
					typ = protocol.TypeJobDone
				case "failed":
					typ = protocol.TypeJobFailed
				}
				if err := ss.send(ctx, typ, "", protocol.JobEvent{Job: ev.Job}); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	for {
		_, data, err := ss.conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == -1 && !errors.Is(err, context.Canceled) {
				ss.log.Debug("ws read ended", "err", err)
			}
			return
		}
		ss.handle(ctx, data)
	}
}

func (ss *session) send(ctx context.Context, typ, id string, data any) error {
	b, err := protocol.Encode(typ, id, data)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ss.writeMu.Lock()
	defer ss.writeMu.Unlock()
	return ss.conn.Write(ctx, websocket.MessageText, b)
}

func (ss *session) fail(ctx context.Context, id, code, msg string) {
	_ = ss.send(ctx, protocol.TypeError, id, protocol.Error{Code: code, Message: msg})
}

func (ss *session) handle(ctx context.Context, data []byte) {
	env, err := protocol.Decode(data)
	if err != nil {
		ss.fail(ctx, "", "bad_request", err.Error())
		return
	}
	span := ss.srv.Rec.Start("ws.handle", "type", env.Type)
	defer span.End(nil)

	switch env.Type {
	case protocol.TypeHello:
		var h protocol.Hello
		if err := protocol.DecodeData(env, &h); err != nil {
			ss.fail(ctx, env.ID, "bad_request", err.Error())
			return
		}
		ss.profile = media.ProfileForHost(h.Host.Version)
		ss.log.Info("panel hello", "client", h.Client, "clientVersion", h.ClientVersion,
			"host", h.Host.App, "hostVersion", h.Host.Version, "api", h.Host.API, "os", h.Host.OS, "profile", ss.profile)
		_ = ss.send(ctx, protocol.TypeHelloOK, env.ID, protocol.HelloOK{
			HelperVersion: ss.srv.Version,
			Protocol:      protocol.Version,
			Profile:       string(ss.profile),
			LibraryDir:    ss.srv.LibraryDir,
			Tools:         ss.srv.Tools.Status(ctx),
			Jobs:          ss.srv.Jobs.List(),
		})

	case protocol.TypeJobCreate:
		var jc protocol.JobCreate
		if err := protocol.DecodeData(env, &jc); err != nil {
			ss.fail(ctx, env.ID, "bad_request", err.Error())
			return
		}
		h := jc.MaxHeight
		if h <= 0 {
			h = ss.srv.DefaultHeight
		}
		prof := ss.profile
		if prof == "" {
			prof = media.ProfileCompat
		}
		id, err := ss.srv.Jobs.Submit(jc.URL, jobs.Options{MaxHeight: h, Profile: prof, ProbeID: jc.ProbeID, FormatID: jc.FormatID, Range: jc.Range})
		if err != nil {
			ss.fail(ctx, env.ID, "bad_request", err.Error())
			return
		}
		_ = ss.send(ctx, protocol.TypeJobAccepted, env.ID, protocol.JobAccepted{JobID: id})

	case protocol.TypeMediaProbe:
		var mp protocol.MediaProbe
		if err := protocol.DecodeData(env, &mp); err != nil {
			ss.fail(ctx, env.ID, "bad_request", err.Error())
			return
		}
		go ss.probe(ctx, env.ID, mp)

	case protocol.TypeJobCancel:
		var jc protocol.JobCancel
		if err := protocol.DecodeData(env, &jc); err != nil {
			ss.fail(ctx, env.ID, "bad_request", err.Error())
			return
		}
		if !ss.srv.Jobs.Cancel(jc.JobID) {
			ss.fail(ctx, env.ID, "bad_request", "no such active job")
		}

	case protocol.TypeJobList:
		_ = ss.send(ctx, protocol.TypeJobListOK, env.ID, protocol.JobList{Jobs: ss.srv.Jobs.List()})

	case protocol.TypeToolsEnsure:
		var te protocol.ToolsEnsure
		if len(env.Data) > 0 {
			_ = json.Unmarshal(env.Data, &te)
		}
		ch := tools.Stable
		if te.Channel == string(tools.Nightly) {
			ch = tools.Nightly
		}
		go ss.ensureTools(ctx, env.ID, ch)

	default:
		ss.fail(ctx, env.ID, "unknown_type", env.Type)
	}
}

// probe runs outside the read loop (yt-dlp -J takes seconds) and answers with media.info.
func (ss *session) probe(ctx context.Context, id string, mp protocol.MediaProbe) {
	h := mp.MaxHeight
	if h <= 0 {
		h = ss.srv.DefaultHeight
	}
	prof := ss.profile
	if prof == "" {
		prof = media.ProfileCompat
	}
	msg := protocol.MediaInfo{URL: mp.URL}
	pid, info, err := ss.srv.Jobs.Probe(ctx, mp.URL, h, prof)
	if err != nil {
		var f *engine.Failure
		if !errors.As(err, &f) {
			f = engine.Fail(engine.KindInternal, err.Error())
		}
		msg.Failure = f
	} else {
		msg.ProbeID, msg.Info = pid, info
	}
	_ = ss.send(ctx, protocol.TypeMediaInfo, id, msg)
}

func (ss *session) ensureTools(ctx context.Context, id string, ch tools.Channel) {
	if !ss.srv.ensureMu.TryLock() {
		_ = ss.send(ctx, protocol.TypeToolsStatus, id, protocol.ToolsStatus{Installing: true, Tools: ss.srv.Tools.Status(ctx)})
		return
	}
	defer ss.srv.ensureMu.Unlock()
	_ = ss.send(ctx, protocol.TypeToolsStatus, id, protocol.ToolsStatus{Installing: true, Tools: ss.srv.Tools.Status(ctx)})
	st, err := ss.srv.Tools.Ensure(ctx, ch)
	msg := protocol.ToolsStatus{Tools: st}
	if err != nil {
		msg.Error = err.Error()
	}
	_ = ss.send(ctx, protocol.TypeToolsStatus, id, msg)
}
