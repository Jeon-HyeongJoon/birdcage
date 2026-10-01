// Package perf records performance data as JSON lines.
//
// Helper가 실행 중일 때와 테스트 중일 때 모두 같은 형식으로 기록한다.
// 기록 종류(kind): run(실행 메타), span(구간 측정), sample(주기 런타임 샘플), test(패키지 테스트 요약).
// 모든 메서드는 nil *Recorder에서도 안전하게 동작한다(no-op).
package perf

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Record is one JSON line in a perf log.
type Record struct {
	Kind  string         `json:"kind"`
	Time  time.Time      `json:"ts"`
	Run   string         `json:"run,omitempty"`
	Name  string         `json:"name,omitempty"`
	Job   string         `json:"job,omitempty"`
	DurMS float64        `json:"dur_ms,omitempty"`
	Proc  *ProcStats     `json:"proc,omitempty"`
	Go    *GoStats       `json:"go,omitempty"`
	Attrs map[string]any `json:"attrs,omitempty"`
	Err   string         `json:"err,omitempty"`
}

// ProcStats describes resource usage of a child process tree.
type ProcStats struct {
	WallMS      float64 `json:"wall_ms"`
	CPUUserMS   float64 `json:"cpu_user_ms"`
	CPUSysMS    float64 `json:"cpu_sys_ms"`
	MaxRSSBytes int64   `json:"max_rss_bytes,omitempty"`
	ExitCode    int     `json:"exit_code"`
}

// Recorder appends perf records to a writer. Safe for concurrent use.
type Recorder struct {
	mu     sync.Mutex
	enc    *json.Encoder
	closer io.Closer
	run    string
	now    func() time.Time
}

// New returns a recorder writing to w. run identifies the process/test run.
func New(w io.Writer, run string) *Recorder {
	return &Recorder{enc: json.NewEncoder(w), run: run, now: time.Now}
}

// Open appends to the file at path, creating parent directories.
func Open(path, run string) (*Recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	r := New(f, run)
	r.closer = f
	return r, nil
}

// Close closes the underlying file if the recorder owns one.
func (r *Recorder) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closer.Close()
}

// RunID returns the run identifier.
func (r *Recorder) RunID() string {
	if r == nil {
		return ""
	}
	return r.run
}

// Write emits a record, filling Time and Run when empty.
func (r *Recorder) Write(rec Record) {
	if r == nil {
		return
	}
	if rec.Time.IsZero() {
		rec.Time = r.now().UTC()
	}
	if rec.Run == "" {
		rec.Run = r.run
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.enc.Encode(rec)
}

// Meta writes a "run" record describing the environment.
func (r *Recorder) Meta(name string, attrs map[string]any) {
	if r == nil {
		return
	}
	a := map[string]any{
		"go":     runtime.Version(),
		"os":     runtime.GOOS,
		"arch":   runtime.GOARCH,
		"cpus":   runtime.NumCPU(),
		"pid":    os.Getpid(),
		"gomaxp": runtime.GOMAXPROCS(0),
	}
	for k, v := range attrs {
		a[k] = v
	}
	r.Write(Record{Kind: "run", Name: name, Attrs: a})
}

// Span measures one named section of work.
type Span struct {
	r     *Recorder
	name  string
	job   string
	start time.Time
	mu    sync.Mutex
	attrs map[string]any
	proc  *ProcStats
	ended bool
}

// Start begins a span. kv must be alternating string keys and values.
func (r *Recorder) Start(name string, kv ...any) *Span {
	s := &Span{r: r, name: name, start: time.Now()}
	s.Set(kv...)
	return s
}

// StartJob begins a span attributed to a job.
func (r *Recorder) StartJob(job, name string, kv ...any) *Span {
	s := r.Start(name, kv...)
	s.job = job
	return s
}

// Set adds attributes as alternating key/value pairs.
func (s *Span) Set(kv ...any) {
	if s == nil || len(kv) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attrs == nil {
		s.attrs = make(map[string]any, len(kv)/2)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			k = fmt.Sprint(kv[i])
		}
		s.attrs[k] = kv[i+1]
	}
}

// SetProc attaches child process statistics.
func (s *Span) SetProc(p ProcStats) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.proc = &p
	s.mu.Unlock()
}

// End finishes the span and writes it. Calling End twice is a no-op.
func (s *Span) End(err error) time.Duration {
	if s == nil {
		return 0
	}
	d := time.Since(s.start)
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return d
	}
	s.ended = true
	rec := Record{Kind: "span", Name: s.name, Job: s.job, DurMS: ms(d), Proc: s.proc, Attrs: s.attrs}
	s.mu.Unlock()
	if err != nil {
		rec.Err = err.Error()
	}
	if s.r != nil {
		g := ReadGoStats()
		rec.Go = &g
		s.r.Write(rec)
	}
	return d
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// MS converts a duration to fractional milliseconds.
func MS(d time.Duration) float64 { return ms(d) }
