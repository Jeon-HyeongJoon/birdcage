// Package perftest makes every test run leave a performance log.
//
// 모든 패키지의 TestMain에서 Main을 호출한다:
//
//	func TestMain(m *testing.M) { perftest.Main(m) }
//
// 기록 위치: $BIRDCAGE_PERF_DIR/<패키지>.jsonl (scripts/test.sh가 실행마다 새 디렉터리를 지정).
// 환경변수가 없으면 <module>/.perf/adhoc/ 에 기록한다 — 즉 `go test`만 돌려도 기록이 남는다.
package perftest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"birdcage/helper/internal/perf"
)

var (
	rec     *perf.Recorder
	pkgName string
)

// Main runs the tests, writing a run header and a package summary record.
func Main(m *testing.M) { os.Exit(run(m)) }

func run(m *testing.M) int {
	root := moduleRoot()
	pkgName = packagePath(root)
	r, err := perf.Open(filepath.Join(Dir(root), strings.ReplaceAll(pkgName, "/", "_")+".jsonl"), RunID())
	if err != nil {
		// 기록 실패로 테스트를 막지는 않는다.
		return m.Run()
	}
	rec = r
	defer r.Close()
	r.Meta("test:"+pkgName, map[string]any{"pkg": pkgName})

	var (
		mu       sync.Mutex
		peakHeap uint64
		peakGor  uint64
		stop     = make(chan struct{})
		done     = make(chan struct{})
	)
	go func() {
		defer close(done)
		t := time.NewTicker(25 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				g := perf.ReadGoStats()
				mu.Lock()
				peakHeap = max(peakHeap, g.HeapLiveBytes)
				peakGor = max(peakGor, g.Goroutines)
				mu.Unlock()
			}
		}
	}()

	var msBefore, msAfter runtime.MemStats
	runtime.ReadMemStats(&msBefore) // 패키지당 1회라 STW 비용은 무시할 수준
	before := perf.ReadGoStats()
	start := time.Now()
	code := m.Run()
	elapsed := time.Since(start)
	close(stop)
	<-done
	after := perf.ReadGoStats()
	runtime.ReadMemStats(&msAfter)

	mu.Lock()
	defer mu.Unlock()
	r.Write(perf.Record{
		Kind:  "test",
		Name:  pkgName,
		DurMS: perf.MS(elapsed),
		Go:    &after,
		Attrs: map[string]any{
			"exit":            code,
			"alloc_bytes":     msAfter.TotalAlloc - msBefore.TotalAlloc,
			"mallocs":         msAfter.Mallocs - msBefore.Mallocs,
			"gc_cycles":       msAfter.NumGC - msBefore.NumGC,
			"gc_pause_ms":     float64(msAfter.PauseTotalNs-msBefore.PauseTotalNs) / 1e6,
			"cpu_sec":         after.CPUProcSec - before.CPUProcSec,
			"heap_sys_bytes":  msAfter.HeapSys,
			"peak_heap_bytes": peakHeap,
			"peak_goroutines": peakGor,
		},
	})
	return code
}

// Recorder returns the package's perf recorder (nil if Main was not used; nil is safe).
func Recorder() *perf.Recorder { return rec }

// Track records the calling test's duration and allocation delta as a span.
// 병렬 테스트에서는 할당량이 섞이므로 참고값이다.
func Track(t testing.TB, kv ...any) *perf.Span {
	t.Helper()
	before := perf.ReadGoStats()
	s := rec.Start("test:"+t.Name(), kv...)
	t.Cleanup(func() {
		after := perf.ReadGoStats()
		s.Set("alloc_bytes", after.TotalAllocBytes-before.TotalAllocBytes, "failed", t.Failed(), "pkg", pkgName)
		s.End(nil)
	})
	return s
}

// File returns a path inside the current perf log directory, for extra logs
// (e.g. the helper's own perf log during end-to-end tests).
func File(name string) string { return filepath.Join(Dir(moduleRoot()), name) }

// Dir returns the directory perf logs are written to.
func Dir(root string) string {
	if d := os.Getenv("BIRDCAGE_PERF_DIR"); d != "" {
		return d
	}
	return filepath.Join(root, ".perf", "adhoc")
}

// RunID identifies the current test run.
func RunID() string {
	if id := os.Getenv("BIRDCAGE_PERF_RUN"); id != "" {
		return id
	}
	return "adhoc-" + time.Now().UTC().Format("20060102T150405Z")
}

func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

func packagePath(root string) string {
	dir, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return "root"
	}
	return filepath.ToSlash(rel)
}
