package perf

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"time"
)

// SnapshotFunc returns application state to expose at /debug/perf/snapshot.
type SnapshotFunc func() any

// PprofServer serves net/http/pprof plus a JSON runtime snapshot on loopback.
type PprofServer struct {
	srv  *http.Server
	ln   net.Listener
	Addr string
}

// ServePprof starts the profiling server. addr must be a loopback address
// (127.0.0.1, ::1 or localhost); port 0 picks a free port.
//
// 실행 중 프로파일 수집 예:
//
//	go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=20
//	go tool pprof http://127.0.0.1:6060/debug/pprof/heap
//	curl http://127.0.0.1:6060/debug/perf/snapshot
func ServePprof(addr string, snapshot SnapshotFunc) (*PprofServer, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("pprof addr %q: %w", addr, err)
	}
	if !isLoopback(host) {
		return nil, fmt.Errorf("pprof addr %q: only loopback addresses are allowed", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	// 블록/뮤텍스 프로파일은 기본 꺼져 있으므로 pprof를 켤 때만 저비용 샘플링으로 활성화한다.
	runtime.SetBlockProfileRate(int(time.Millisecond))
	runtime.SetMutexProfileFraction(10)

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("/debug/perf/snapshot", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{"go": ReadGoStats(), "ts": time.Now().UTC()}
		if snapshot != nil {
			out["app"] = snapshot()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	s := &PprofServer{
		srv: &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second},
		ln:  ln,
		// profile?seconds=N 요청을 위해 WriteTimeout은 두지 않는다.
		Addr: ln.Addr().String(),
	}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

// Shutdown stops the server.
func (s *PprofServer) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
