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
	"birdcage/helper/internal/preview"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/related"
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
	Preview        *preview.Service // nil = previews off
	Related        *related.Service // nil = browse list off
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

	previewMu     sync.Mutex
	previewID     string
	previewCancel context.CancelFunc
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
		if jc.Via != "" {
			ss.srv.Rec.Start("related.download", "via", jc.Via, "range", jc.Range != nil).End(nil) // 적합성 검토용
		}
		id, err := ss.srv.Jobs.Submit(jc.URL, jobs.Options{MaxHeight: h, Profile: prof, OutputDir: jc.OutputDir, ProbeID: jc.ProbeID, FormatID: jc.FormatID, Range: jc.Range, AudioOnly: jc.AudioOnly, Subtitles: jc.Subtitles})
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
		if mp.Via != "" {
			ss.srv.Rec.Start("related.open", "via", mp.Via).End(nil) // 탐색 목록에서 연 링크(적합성 검토용)
		}
		go ss.probe(ctx, env.ID, mp)

	case protocol.TypeRelatedFetch:
		var rf protocol.RelatedFetch
		if err := protocol.DecodeData(env, &rf); err != nil {
			ss.fail(ctx, env.ID, "bad_request", err.Error())
			return
		}
		go ss.related(ctx, env.ID, rf)

	case protocol.TypeSearchFetch:
		var sf protocol.SearchFetch
		if err := protocol.DecodeData(env, &sf); err != nil {
			ss.fail(ctx, env.ID, "bad_request", err.Error())
			return
		}
		go ss.search(ctx, env.ID, sf)

	case protocol.TypePreviewOpen:
		var po protocol.PreviewOpen
		if err := protocol.DecodeData(env, &po); err != nil {
			ss.fail(ctx, env.ID, "bad_request", err.Error())
			return
		}
		ss.openPreview(ctx, env.ID, po.ProbeID)

	case protocol.TypePreviewClose:
		var pc protocol.PreviewClose
		if err := protocol.DecodeData(env, &pc); err != nil {
			ss.fail(ctx, env.ID, "bad_request", err.Error())
			return
		}
		ss.stopPreview(pc.ProbeID)

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
	// 사용자가 Helper 실행 뒤 Homebrew 등으로 도구를 설치했을 수 있다. 조회할 때 다시
	// 탐색해 패널의 시작 시점 도구 상태도 함께 갱신한다(Locator는 실패를 캐시하지 않는다).
	if ss.srv.Tools != nil {
		_ = ss.send(ctx, protocol.TypeToolsStatus, "", protocol.ToolsStatus{Tools: ss.srv.Tools.Status(ctx)})
	}
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

// related answers related.fetch with the browse list (네트워크·yt-dlp를 쓰므로 읽기 루프 밖에서).
func (ss *session) related(ctx context.Context, id string, rf protocol.RelatedFetch) {
	want := rf.Source
	if !want.Valid() {
		want = related.SourceYouTube
	}
	msg := protocol.RelatedList{ProbeID: rf.ProbeID, Requested: want, Items: []related.Item{}}
	info := ss.srv.Jobs.ProbeInfo(rf.ProbeID)
	seed, ok := related.SeedOf(info)
	switch {
	case ss.srv.Related == nil:
		msg.Error = "탐색 목록을 쓸 수 없습니다"
	case info == nil:
		msg.Error = "링크 정보가 만료되었습니다. 다시 확인하세요."
	case !ok:
		msg.Error = "이 링크는 탐색 목록을 지원하지 않습니다"
	default:
		var lister engine.Lister
		if ts, err := ss.srv.Jobs.Toolset(ctx); err == nil {
			lister, _ = ts.Engine.(engine.Lister)
		}
		res, err := ss.srv.Related.List(ctx, seed, want, lister)
		if err != nil {
			ss.log.Warn("related list failed", "source", want, "err", err)
			msg.Error = "목록을 가져오지 못했습니다"
		} else {
			msg.Source, msg.Items = res.Source, res.Items
		}
	}
	_ = ss.send(ctx, protocol.TypeRelatedList, id, msg)
}

// search answers search.fetch with one page of YouTube results.
func (ss *session) search(ctx context.Context, id string, sf protocol.SearchFetch) {
	msg := protocol.SearchList{Query: related.NormalizeQuery(sf.Query), Page: sf.Page, Items: []related.Item{}}
	switch {
	case ss.srv.Related == nil:
		msg.Error = "검색을 쓸 수 없습니다"
	case msg.Query == "":
		msg.Error = "검색어를 입력하세요"
	default:
		var lister engine.Lister
		if ts, err := ss.srv.Jobs.Toolset(ctx); err == nil {
			lister, _ = ts.Engine.(engine.Lister)
		}
		res, err := ss.srv.Related.Search(ctx, msg.Query, sf.Page, lister)
		if err != nil {
			ss.log.Warn("search failed", "page", sf.Page, "err", err)
			msg.Error = "검색 결과를 가져오지 못했습니다"
		} else {
			msg.Items, msg.More = res.Items, res.More
		}
	}
	_ = ss.send(ctx, protocol.TypeSearchList, id, msg)
}

// openPreview cancels the session's previous preview and starts this one in the background.
func (ss *session) openPreview(ctx context.Context, id, probeID string) {
	failed := func(msg string) {
		_ = ss.send(ctx, protocol.TypePreviewState, id, preview.State{ProbeID: probeID, Stage: preview.StageFailed, Progress: -1, Error: msg})
	}
	if ss.srv.Preview == nil {
		failed("미리보기를 쓸 수 없습니다")
		return
	}
	info := ss.srv.Jobs.ProbeInfo(probeID)
	if info == nil {
		failed("링크 정보가 만료되었습니다. 다시 확인하세요.")
		return
	}
	ts, err := ss.srv.Jobs.Toolset(ctx)
	if err != nil {
		failed(err.Error())
		return
	}
	pctx, cancel := context.WithCancel(ctx)
	ss.previewMu.Lock()
	if ss.previewCancel != nil {
		ss.previewCancel()
	}
	ss.previewID, ss.previewCancel = probeID, cancel
	ss.previewMu.Unlock()
	first := true
	go func() {
		defer cancel()
		ss.srv.Preview.Run(pctx, probeID, info, preview.Tools{Engine: ts.Engine, FFmpeg: ts.Normalizer.FFmpeg, Prober: ts.Prober}, func(st preview.State) {
			rid := ""
			if first {
				rid, first = id, false // 첫 상태가 요청의 답이다
			}
			if pctx.Err() == nil {
				_ = ss.send(ctx, protocol.TypePreviewState, rid, st)
			}
		})
	}()
}

func (ss *session) stopPreview(probeID string) {
	ss.previewMu.Lock()
	defer ss.previewMu.Unlock()
	if ss.previewCancel != nil && (probeID == "" || probeID == ss.previewID) {
		ss.previewCancel()
		ss.previewCancel, ss.previewID = nil, ""
	}
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
