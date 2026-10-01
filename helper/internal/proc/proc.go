// Package proc runs external tools (yt-dlp, ffmpeg) with process-tree cleanup
// and resource accounting.
//
//   - 취소 시 자식이 만든 손자 프로세스(yt-dlp → ffmpeg)까지 함께 종료한다
//     (Unix: 프로세스 그룹, Windows: Job Object).
//   - 정상 종료 후에도 남은 그룹 구성원을 정리해 고아 프로세스를 남기지 않는다.
//   - 벽시계 시간, CPU(user/sys), 최대 메모리(RSS)를 perf.ProcStats로 돌려준다.
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"birdcage/helper/internal/perf"
)

// Spec describes a process to run.
type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string // nil inherits the helper's environment
	// Stdout/Stderr receive raw output; nil discards. Use LineWriter for line callbacks.
	Stdout io.Writer
	Stderr io.Writer
	// StderrTail keeps the last N stderr lines in Result for error classification.
	StderrTail int
	// KillGrace is the time between the polite stop signal and the hard kill.
	KillGrace time.Duration
}

// Result describes a finished process.
type Result struct {
	ExitCode   int
	Stats      perf.ProcStats
	StderrTail []string
}

// ErrNotStarted wraps failures to start the process (missing binary, permissions).
var ErrNotStarted = errors.New("process not started")

// Run starts the process and waits for it. A non-zero exit returns *ExitError.
// Context cancellation kills the whole process tree and returns ctx.Err().
func Run(ctx context.Context, spec Spec) (Result, error) {
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	tail := newTail(spec.StderrTail)
	cmd.Stdout = orDiscard(spec.Stdout)
	cmd.Stderr = io.MultiWriter(orDiscard(spec.Stderr), tail)
	setup(cmd)

	grace := spec.KillGrace
	if grace <= 0 {
		grace = 2 * time.Second
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("%w: %s: %v", ErrNotStarted, spec.Path, err)
	}
	tree, terr := attach(cmd)
	if terr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return Result{ExitCode: -1}, fmt.Errorf("%w: attach process tree: %v", ErrNotStarted, terr)
	}
	defer tree.close()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var (
		waitErr  error
		canceled bool
	)
	select {
	case waitErr = <-waitDone:
	case <-ctx.Done():
		canceled = true
		_ = tree.stop() // 정중한 종료 요청
		select {
		case waitErr = <-waitDone:
		case <-time.After(grace):
			_ = tree.kill()
			waitErr = <-waitDone
		}
	}
	// 정상 종료여도 그룹에 남은 손자 프로세스를 정리한다.
	_ = tree.kill()

	res := Result{ExitCode: -1, StderrTail: tail.lines()}
	res.Stats.WallMS = perf.MS(time.Since(start))
	if ps := cmd.ProcessState; ps != nil {
		res.ExitCode = ps.ExitCode()
		res.Stats.ExitCode = ps.ExitCode()
		res.Stats.CPUUserMS = perf.MS(ps.UserTime())
		res.Stats.CPUSysMS = perf.MS(ps.SystemTime())
		res.Stats.MaxRSSBytes = maxRSS(ps)
	}
	if peak := tree.peakMemory(); peak > res.Stats.MaxRSSBytes {
		res.Stats.MaxRSSBytes = peak
	}
	if canceled {
		return res, ctx.Err()
	}
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			return res, &ExitError{Path: spec.Path, Code: res.ExitCode, Tail: res.StderrTail}
		}
		return res, waitErr
	}
	return res, nil
}

// ExitError reports a non-zero exit status.
type ExitError struct {
	Path string
	Code int
	Tail []string
}

func (e *ExitError) Error() string {
	last := ""
	if n := len(e.Tail); n > 0 {
		last = ": " + e.Tail[n-1]
	}
	return fmt.Sprintf("%s exited with code %d%s", e.Path, e.Code, last)
}

// Output runs the process and returns its stdout (for small outputs like --version or -J).
func Output(ctx context.Context, spec Spec) ([]byte, Result, error) {
	var buf bytes.Buffer
	spec.Stdout = &buf
	res, err := Run(ctx, spec)
	return buf.Bytes(), res, err
}

func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

// tailBuffer keeps the last n lines written to it.
type tailBuffer struct {
	mu   sync.Mutex
	n    int
	buf  []string
	part []byte
}

func newTail(n int) *tailBuffer { return &tailBuffer{n: n} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	if t.n <= 0 {
		return len(p), nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	data := append(t.part, p...)
	for {
		i := bytes.IndexAny(data, "\r\n")
		if i < 0 {
			break
		}
		if i > 0 {
			t.push(string(data[:i]))
		}
		data = data[i+1:]
	}
	t.part = append(t.part[:0], data...)
	return len(p), nil
}

func (t *tailBuffer) push(s string) {
	t.buf = append(t.buf, s)
	if len(t.buf) > t.n {
		t.buf = t.buf[len(t.buf)-t.n:]
	}
}

func (t *tailBuffer) lines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := append([]string(nil), t.buf...)
	if len(t.part) > 0 {
		out = append(out, string(t.part))
	}
	if len(out) > t.n && t.n > 0 {
		out = out[len(out)-t.n:]
	}
	return out
}
