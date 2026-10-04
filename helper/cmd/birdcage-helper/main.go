// Command birdcage-helper downloads and normalizes media for the Birdcage Premiere panels.
//
// 패널(CEP/UXP)이 실행하고, 표준출력 첫 줄의 JSON 핸드셰이크로 포트·토큰을 넘긴다:
//
//	{"type":"ready","port":50123,"token":"…","pid":123,"version":"…","protocol":0,"pprof":"127.0.0.1:6060"}
//
// 성능 모니터링:
//   - 성능 로그(JSONL)는 기본으로 켜져 있다: <data>/logs/perf-YYYYMMDD.jsonl (구간 측정 + 주기 런타임 샘플)
//   - --pprof 127.0.0.1:6060 으로 net/http/pprof 와 /debug/perf/snapshot 을 연다
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"birdcage/helper/internal/config"
	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/engine/ytdlp"
	"birdcage/helper/internal/jobs"
	"birdcage/helper/internal/media"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/preview"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/related"
	"birdcage/helper/internal/server"
	"birdcage/helper/internal/tools"
)

// version is set at build time: -ldflags "-X main.version=…".
var version = "0.0.0-dev"

type options struct {
	addr, token, handshake, dataDir, libraryDir, pprofAddr, perfLog, logLevel string
	ytdlp, ffmpeg, ffprobe, deno                                              string
	exitOnStdin, perfOn, keepWork, showVersion, ensureTools                   bool
	channel                                                                   string
	perfEvery                                                                 time.Duration
	maxConcurrent, maxHeight, fragments, previewCacheMB                       int
}

func parseFlags(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("birdcage-helper", flag.ContinueOnError)
	fs.StringVar(&o.addr, "addr", "127.0.0.1:0", "listen address (loopback only; port 0 = random)")
	fs.StringVar(&o.token, "token", os.Getenv("BIRDCAGE_TOKEN"), "auth token (default: random per launch)")
	fs.StringVar(&o.handshake, "handshake", "stdout", "where to print the ready JSON: stdout | none")
	fs.BoolVar(&o.exitOnStdin, "exit-on-stdin-close", false, "exit when stdin closes (panel-owned mode)")
	fs.StringVar(&o.dataDir, "data-dir", config.DataDir(), "data directory (tools, logs, work)")
	fs.StringVar(&o.libraryDir, "library-dir", config.LibraryDir(), "where finished media is stored")
	fs.StringVar(&o.pprofAddr, "pprof", os.Getenv("BIRDCAGE_PPROF"), "serve pprof on this loopback address, e.g. 127.0.0.1:6060")
	fs.BoolVar(&o.perfOn, "perf", true, "write the performance log")
	fs.StringVar(&o.perfLog, "perf-log", "", "performance log path (default <data>/logs/perf-YYYYMMDD.jsonl)")
	fs.DurationVar(&o.perfEvery, "perf-sample", 10*time.Second, "runtime sample interval in the perf log (0 = off)")
	fs.StringVar(&o.logLevel, "log-level", "info", "debug | info | warn | error")
	fs.IntVar(&o.previewCacheMB, "preview-cache-mb", 2048, "preview cache size cap on disk (MB)")
	fs.StringVar(&o.ytdlp, "yt-dlp", os.Getenv("BIRDCAGE_YTDLP"), "yt-dlp path override")
	fs.StringVar(&o.ffmpeg, "ffmpeg", os.Getenv("BIRDCAGE_FFMPEG"), "ffmpeg path override")
	fs.StringVar(&o.ffprobe, "ffprobe", os.Getenv("BIRDCAGE_FFPROBE"), "ffprobe path override")
	fs.StringVar(&o.deno, "deno", os.Getenv("BIRDCAGE_DENO"), "deno path override")
	fs.IntVar(&o.maxConcurrent, "max-concurrent", 2, "max concurrent jobs")
	fs.IntVar(&o.maxHeight, "max-height", 1080, "default max video height")
	fs.IntVar(&o.fragments, "fragments", 4, "concurrent fragment downloads for DASH/HLS (yt-dlp -N)")
	fs.BoolVar(&o.keepWork, "keep-work", false, "keep per-job work directories (debugging)")
	fs.BoolVar(&o.showVersion, "version", false, "print version and exit")
	fs.BoolVar(&o.ensureTools, "ensure-tools", false, "install missing tools (yt-dlp, deno), print status JSON and exit")
	fs.StringVar(&o.channel, "channel", "stable", "yt-dlp channel for --ensure-tools: stable | nightly")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	return o, nil
}

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	o, err := parseFlags(args)
	if err != nil {
		return 2
	}
	if o.showVersion {
		fmt.Fprintln(stdout, version)
		return 0
	}

	logsDir := filepath.Join(o.dataDir, "logs")
	log, closeLog, err := newLogger(logsDir, o.logLevel, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "log:", err)
		return 1
	}
	defer closeLog()

	runID := fmt.Sprintf("helper-%s-%d", time.Now().UTC().Format("20060102T150405Z"), os.Getpid())
	var rec *perf.Recorder
	if o.perfOn {
		path := o.perfLog
		if path == "" {
			path = filepath.Join(logsDir, "perf-"+time.Now().Format("20060102")+".jsonl")
		}
		if rec, err = perf.Open(path, runID); err != nil {
			log.Warn("perf log disabled", "err", err)
			rec = nil
		} else {
			defer rec.Close()
			rec.Meta("helper", map[string]any{"version": version, "args": args})
			log.Info("perf log", "path", path, "run", runID)
		}
	}
	startup := rec.Start("helper.startup")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	exe, _ := os.Executable()
	loc := tools.NewLocator(filepath.Join(o.dataDir, "tools"), filepath.Join(filepath.Dir(exe), "tools"), map[tools.Name]string{
		tools.YtDlp: o.ytdlp, tools.FFmpeg: o.ffmpeg, tools.FFprobe: o.ffprobe, tools.Deno: o.deno,
	})
	tm := &tools.Manager{Loc: loc, Inst: &tools.Installer{Root: loc.Root, Rec: rec, Log: log,
		Client: &http.Client{Timeout: 15 * time.Minute}}}
	resolver := &toolsetResolver{loc: loc, fragments: o.fragments, log: log}

	if o.ensureTools {
		st, err := tm.Ensure(ctx, tools.Channel(o.channel))
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Fprintln(stdout, string(b))
		startup.End(err)
		if err != nil {
			log.Error("ensure tools", "err", err)
			return 1
		}
		return 0
	}

	mgr := jobs.New(jobs.Config{MaxConcurrent: o.maxConcurrent, NetworkRetries: 1, KeepWorkDir: o.keepWork}, jobs.Deps{
		Tools:      resolver.resolve,
		LibraryDir: o.libraryDir,
		WorkDir:    filepath.Join(o.dataDir, "work"),
		Rec:        rec,
		Log:        log,
	})
	defer mgr.Close()

	if o.perfEvery > 0 && rec != nil {
		go rec.RunSampler(ctx, o.perfEvery, mgr.Stats)
	}

	var pp *perf.PprofServer
	if o.pprofAddr != "" {
		if pp, err = perf.ServePprof(o.pprofAddr, func() any { return mgr.Stats() }); err != nil {
			log.Error("pprof", "err", err)
			return 1
		}
		log.Info("pprof listening", "addr", pp.Addr)
		defer pp.Shutdown(context.Background())
	}

	token := o.token
	if token == "" {
		token = randomToken()
	}
	ln, err := listenLoopback(o.addr)
	if err != nil {
		log.Error("listen", "err", err)
		return 1
	}
	pv := &preview.Service{Cache: preview.Cache{Root: filepath.Join(o.dataDir, "preview"), CapBytes: int64(o.previewCacheMB) << 20}, Rec: rec}
	srv := &server.Server{Token: token, Version: version, LibraryDir: o.libraryDir, DefaultHeight: o.maxHeight,
		Jobs: mgr, Preview: pv, Related: &related.Service{Rec: rec}, Tools: tm, Rec: rec, Log: log}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			stop()
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	startup.Set("port", port)
	startup.End(nil)
	if o.handshake == "stdout" {
		ready := map[string]any{"type": "ready", "port": port, "token": token, "pid": os.Getpid(),
			"version": version, "protocol": protocol.Version}
		if pp != nil {
			ready["pprof"] = pp.Addr
		}
		b, _ := json.Marshal(ready)
		fmt.Fprintln(stdout, string(b))
	}
	log.Info("helper ready", "port", port, "version", version, "data", o.dataDir, "library", o.libraryDir)

	if o.exitOnStdin {
		go func() {
			// 패널(부모)이 죽으면 파이프가 닫혀 EOF가 온다 → 고아 Helper를 남기지 않는다.
			_, _ = io.Copy(io.Discard, stdin)
			log.Info("stdin closed, shutting down")
			stop()
		}()
	}

	<-ctx.Done()
	shut := rec.Start("helper.shutdown")
	mgr.Close()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = hs.Shutdown(sctx)
	shut.End(nil)
	log.Info("helper stopped")
	return 0
}

func listenLoopback(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("refusing non-loopback address %q", addr)
	}
	return net.Listen("tcp", addr)
}

func randomToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// toolsetResolver builds the pipeline toolset from currently located tools, reusing
// the Normalizer per ffmpeg path so encoder detection runs once.
type toolsetResolver struct {
	loc       *tools.Locator
	fragments int
	log       *slog.Logger

	mu    sync.Mutex
	norms map[string]*media.Normalizer
}

func (r *toolsetResolver) resolve(ctx context.Context) (jobs.Toolset, error) {
	y, err := r.loc.Find(ctx, tools.YtDlp)
	if err != nil {
		return jobs.Toolset{}, engine.Fail(engine.KindToolMissing, "yt-dlp: 패널에서 '도구 설치'를 실행하세요")
	}
	ff, err := r.loc.Find(ctx, tools.FFmpeg)
	if err != nil {
		return jobs.Toolset{}, engine.Fail(engine.KindToolMissing, "ffmpeg: macOS는 brew install ffmpeg")
	}
	fp, err := r.loc.Find(ctx, tools.FFprobe)
	if err != nil {
		return jobs.Toolset{}, engine.Fail(engine.KindToolMissing, "ffprobe: ffmpeg와 함께 설치하세요")
	}
	var warnings []string
	js, _, ok := r.loc.JSRuntime(ctx)
	if !ok {
		warnings = append(warnings, "JS 런타임(deno/node 22+)이 없어 YouTube 고화질이 막힐 수 있습니다. '도구 설치'로 deno를 설치하세요.")
	}
	r.mu.Lock()
	if r.norms == nil {
		r.norms = map[string]*media.Normalizer{}
	}
	n, ok := r.norms[ff.Path]
	if !ok {
		n = &media.Normalizer{FFmpeg: ff.Path}
		r.norms[ff.Path] = n
	}
	r.mu.Unlock()
	return jobs.Toolset{
		Engine:     &ytdlp.Engine{YtDlp: y.Path, FFmpeg: ff.Path, JSRuntime: js, ConcurrentFragments: r.fragments, Log: r.log},
		Prober:     &media.Prober{FFprobe: fp.Path},
		Normalizer: n,
		Warnings:   warnings,
	}, nil
}

func newLogger(dir, level string, stderr io.Writer) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "helper.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	var lv slog.Level
	_ = lv.UnmarshalText([]byte(strings.ToUpper(level)))
	opts := &slog.HandlerOptions{Level: lv}
	h := teeHandler{slog.NewJSONHandler(f, opts), slog.NewTextHandler(stderr, opts)}
	return slog.New(h), func() { f.Close() }, nil
}

// teeHandler writes each record to several handlers (file JSON + stderr text).
type teeHandler []slog.Handler

func (t teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range t {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (t teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range t {
		if h.Enabled(ctx, r.Level) {
			errs = append(errs, h.Handle(ctx, r.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (t teeHandler) WithAttrs(as []slog.Attr) slog.Handler {
	out := make(teeHandler, len(t))
	for i, h := range t {
		out[i] = h.WithAttrs(as)
	}
	return out
}

func (t teeHandler) WithGroup(name string) slog.Handler {
	out := make(teeHandler, len(t))
	for i, h := range t {
		out[i] = h.WithGroup(name)
	}
	return out
}
