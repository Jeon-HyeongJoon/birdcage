package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"birdcage/helper/internal/config"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/proc"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/tools"
)

func TestMain(m *testing.M) { perftest.Main(m) }

type ready struct {
	Type, Token, Version, Pprof string
	Port, Pid, Protocol         int
}

// toolPaths finds real tools: env override → helper-installed (default data dir) → PATH.
func toolPaths(t *testing.T) map[tools.Name]string {
	t.Helper()
	loc := tools.NewLocator(filepath.Join(config.DataDir(), "tools"), "", map[tools.Name]string{
		tools.YtDlp: os.Getenv("BIRDCAGE_YTDLP"), tools.FFmpeg: os.Getenv("BIRDCAGE_FFMPEG"),
		tools.FFprobe: os.Getenv("BIRDCAGE_FFPROBE"), tools.Deno: os.Getenv("BIRDCAGE_DENO"),
	})
	out := map[tools.Name]string{}
	for _, n := range []tools.Name{tools.YtDlp, tools.FFmpeg, tools.FFprobe} {
		tl, err := loc.Find(context.Background(), n)
		if err != nil {
			t.Skipf("%s not available (set BIRDCAGE_YTDLP/BIRDCAGE_FFMPEG/BIRDCAGE_FFPROBE or run birdcage-helper --ensure-tools): %v", n, err)
		}
		out[n] = tl.Path
	}
	if d, err := loc.Find(context.Background(), tools.Deno); err == nil {
		out[tools.Deno] = d.Path
	}
	return out
}

type helperProc struct {
	ready ready
	stdin io.WriteCloser
	done  chan int
}

// startInProcess runs the helper's run() with pipes, exactly as a panel would spawn it.
func startInProcess(t *testing.T, extra ...string) *helperProc {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	data := t.TempDir()
	args := append([]string{"--handshake", "stdout", "--exit-on-stdin-close", "--data-dir", data,
		"--library-dir", filepath.Join(data, "library"), "--pprof", "127.0.0.1:0",
		"--perf-log", perftest.File("helper-e2e.jsonl"), "--perf-sample", "500ms", "--log-level", "warn"}, extra...)
	h := &helperProc{stdin: inW, done: make(chan int, 1)}
	go func() {
		code := run(args, inR, outW, io.Discard)
		outW.Close()
		h.done <- code
	}()
	line, err := bufio.NewReader(outR).ReadString('\n')
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, outR) }()
	if err := json.Unmarshal([]byte(line), &h.ready); err != nil || h.ready.Type != "ready" || h.ready.Port == 0 || len(h.ready.Token) != 64 {
		t.Fatalf("bad handshake %q: %v", line, err)
	}
	t.Cleanup(func() { h.stop(t) })
	return h
}

func (h *helperProc) stop(t *testing.T) {
	if h.stdin == nil {
		return
	}
	h.stdin.Close() // 패널이 닫히면 stdin EOF → Helper 종료
	h.stdin = nil
	select {
	case code := <-h.done:
		if code != 0 {
			t.Errorf("helper exit code %d", code)
		}
	case <-time.After(15 * time.Second):
		t.Error("helper did not exit after stdin close")
	}
}

type wsClient struct {
	t    *testing.T
	conn *websocket.Conn
}

func connect(t *testing.T, r ready) *wsClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{"Origin": []string{"file://"}}
	c, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(r.Port)+"/ws?token="+r.Token, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(4 << 20)
	t.Cleanup(func() { c.CloseNow() })
	return &wsClient{t: t, conn: c}
}

func (c *wsClient) send(typ, id string, data any) {
	b, _ := protocol.Encode(typ, id, data)
	if err := c.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

func (c *wsClient) until(timeout time.Duration, match func(protocol.Envelope) bool) protocol.Envelope {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		_, b, err := c.conn.Read(ctx)
		if err != nil {
			c.t.Fatalf("read: %v", err)
		}
		env, err := protocol.Decode(b)
		if err != nil {
			c.t.Fatal(err)
		}
		if match(env) {
			return env
		}
	}
}

func jobResult(t *testing.T, c *wsClient, url string, maxHeight int, timeout time.Duration) protocol.JobEvent {
	t.Helper()
	c.send(protocol.TypeJobCreate, "c1", protocol.JobCreate{URL: url, MaxHeight: maxHeight})
	acc := c.until(10*time.Second, func(e protocol.Envelope) bool {
		return e.Type == protocol.TypeJobAccepted || e.Type == protocol.TypeError
	})
	if acc.Type == protocol.TypeError {
		t.Fatalf("job.create error: %s", acc.Data)
	}
	var a protocol.JobAccepted
	_ = protocol.DecodeData(acc, &a)
	updates := 0
	env := c.until(timeout, func(e protocol.Envelope) bool {
		if e.Type == protocol.TypeJobUpdate {
			updates++
		}
		if e.Type != protocol.TypeJobDone && e.Type != protocol.TypeJobFailed {
			return false
		}
		var ev protocol.JobEvent
		_ = protocol.DecodeData(e, &ev)
		return ev.Job.JobID == a.JobID
	})
	var ev protocol.JobEvent
	_ = protocol.DecodeData(env, &ev)
	t.Logf("job %s: %s after %d updates, timings %+v", ev.Job.JobID, ev.Job.State, updates, ev.Job.Timings)
	if env.Type == protocol.TypeJobFailed {
		t.Fatalf("job failed: %+v", ev.Job.Failure)
	}
	return ev
}

// TestEndToEndLocal: 실제 yt-dlp(generic 추출기)가 로컬 HTTP 서버의 VP9/Opus webm을 받고,
// Helper가 H.264/AAC mp4로 정규화하는 전체 경로. 인터넷 불필요.
func TestEndToEndLocal(t *testing.T) {
	perftest.Track(t)
	tp := toolPaths(t)
	src := filepath.Join(t.TempDir(), "clip.webm")
	if _, err := proc.Run(context.Background(), proc.Spec{Path: tp[tools.FFmpeg], StderrTail: 5, Args: []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=s=640x360:r=30:d=4", "-f", "lavfi", "-i", "sine=r=48000:d=4",
		"-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-b:v", "300k", "-c:a", "libopus", "-shortest", src}}); err != nil {
		t.Fatal(err)
	}
	fs := httptest.NewServer(http.FileServer(http.Dir(filepath.Dir(src))))
	defer fs.Close()

	args := []string{"--yt-dlp", tp[tools.YtDlp], "--ffmpeg", tp[tools.FFmpeg], "--ffprobe", tp[tools.FFprobe]}
	if d := tp[tools.Deno]; d != "" {
		args = append(args, "--deno", d)
	}
	h := startInProcess(t, args...)
	c := connect(t, h.ready)

	c.send(protocol.TypeHello, "h", protocol.Hello{Client: "e2e", ClientVersion: "0", Host: protocol.Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}})
	helloEnv := c.until(30*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeHelloOK })
	var hello protocol.HelloOK
	_ = protocol.DecodeData(helloEnv, &hello)
	if hello.Profile != "compat" {
		t.Fatalf("profile %s", hello.Profile)
	}

	ev := jobResult(t, c, fs.URL+"/clip.webm", 720, 2*time.Minute)
	j := ev.Job
	if _, err := os.Stat(j.File); err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if j.Media == nil || j.Media.Video.Codec != "h264" || j.Media.Audio.Codec != "aac" || j.Media.Audio.SampleRate != 48000 {
		t.Fatalf("media %+v / %+v", j.Media.Video, j.Media.Audio)
	}
	if j.Plan == nil || j.Plan.Video != "transcode" || len(j.Timings) != 4 {
		t.Fatalf("plan %+v timings %+v", j.Plan, j.Timings)
	}

	// pprof와 런타임 스냅샷이 실제로 열려 있는지 확인한다.
	for _, path := range []string{"/debug/pprof/heap?debug=1", "/debug/pprof/goroutine?debug=1", "/debug/perf/snapshot"} {
		resp, err := http.Get("http://" + h.ready.Pprof + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || len(body) == 0 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
		if strings.HasSuffix(path, "snapshot") && !strings.Contains(string(body), `"done":1`) {
			t.Fatalf("snapshot: %s", body)
		}
	}
}

// TestEndToEndOnline: BIRDCAGE_E2E=1 일 때만 실제 플랫폼에서 받는다.
// 기본은 "Me at the zoo"(jNQXAC9IVRw, 19초, YouTube 최초 영상). yt-dlp 공식 테스트 영상 BaW_jenozKc는
// 2026-10 기준 "This video is unavailable"이라 쓰지 않는다. BIRDCAGE_E2E_URLS로 쉼표 구분 URL 추가 가능.
func TestEndToEndOnline(t *testing.T) {
	if os.Getenv("BIRDCAGE_E2E") != "1" {
		t.Skip("set BIRDCAGE_E2E=1 to download from real platforms")
	}
	perftest.Track(t)
	tp := toolPaths(t)
	args := []string{"--yt-dlp", tp[tools.YtDlp], "--ffmpeg", tp[tools.FFmpeg], "--ffprobe", tp[tools.FFprobe]}
	if d := tp[tools.Deno]; d != "" {
		args = append(args, "--deno", d)
	}
	h := startInProcess(t, args...)
	c := connect(t, h.ready)
	c.send(protocol.TypeHello, "h", protocol.Hello{Client: "e2e", Host: protocol.Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}})
	c.until(30*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeHelloOK })

	urls := []string{"https://www.youtube.com/watch?v=jNQXAC9IVRw"}
	if extra := os.Getenv("BIRDCAGE_E2E_URLS"); extra != "" {
		urls = append(urls, strings.Split(extra, ",")...)
	}
	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			ev := jobResult(t, c, strings.TrimSpace(u), 360, 5*time.Minute)
			if ev.Job.Media == nil || ev.Job.Media.Video == nil || ev.Job.Media.Video.Codec != "h264" {
				t.Fatalf("media %+v", ev.Job.Media)
			}
			t.Logf("file=%s plan=%+v warnings=%v", ev.Job.File, ev.Job.Plan, ev.Job.Warnings)
		})
	}
}

// TestBinaryLifecycle: 실제 실행 파일로 --version, 핸드셰이크, stdin 종료 시 자동 종료를 확인한다.
func TestBinaryLifecycle(t *testing.T) {
	perftest.Track(t)
	bin := filepath.Join(t.TempDir(), config.ExeName("birdcage-helper"))
	build := exec.Command("go", "build", "-ldflags", "-X main.version=9.9.9-test", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin, "--version").Output(); err != nil || strings.TrimSpace(string(out)) != "9.9.9-test" {
		t.Fatalf("--version: %q %v", out, err)
	}

	data := t.TempDir()
	cmd := exec.Command(bin, "--exit-on-stdin-close", "--data-dir", data, "--library-dir", filepath.Join(data, "lib"), "--perf-sample", "0")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	startupMS := time.Since(start).Milliseconds()
	perftest.Recorder().Write(perftestRecord("helper.binary_startup_ms", float64(startupMS)))
	var r ready
	if err := json.Unmarshal([]byte(line), &r); err != nil || r.Version != "9.9.9-test" || r.Pid != cmd.Process.Pid {
		t.Fatalf("handshake %q", line)
	}
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(r.Port) + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz %v %v", resp, err)
	}
	resp.Body.Close()

	stdin.Close()
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("helper did not exit after stdin closed")
	}
	if _, err := os.Stat(filepath.Join(data, "logs", "helper.log")); err != nil {
		t.Fatal("helper.log missing")
	}
	t.Logf("startup to handshake: %dms", startupMS)
}

func perftestRecord(name string, ms float64) perf.Record {
	return perf.Record{Kind: "span", Name: name, DurMS: ms}
}
