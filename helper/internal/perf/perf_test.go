package perf_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/perf/perftest"
)

func TestMain(m *testing.M) { perftest.Main(m) }

func decode(t *testing.T, b []byte) []perf.Record {
	t.Helper()
	var out []perf.Record
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		var r perf.Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("bad json line %q: %v", sc.Text(), err)
		}
		out = append(out, r)
	}
	return out
}

func TestSpanWritesRecord(t *testing.T) {
	perftest.Track(t)
	var buf bytes.Buffer
	r := perf.New(&buf, "run-1")
	s := r.StartJob("j1", "stage.download", "engine", "yt-dlp")
	s.Set("bytes", 42)
	s.SetProc(perf.ProcStats{WallMS: 10, CPUUserMS: 3, ExitCode: 0})
	time.Sleep(2 * time.Millisecond)
	d := s.End(errors.New("boom"))
	s.End(nil) // second End is a no-op

	recs := decode(t, buf.Bytes())
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	got := recs[0]
	if got.Kind != "span" || got.Name != "stage.download" || got.Job != "j1" || got.Run != "run-1" {
		t.Fatalf("unexpected record %+v", got)
	}
	if got.DurMS < 2 || d < 2*time.Millisecond {
		t.Fatalf("duration too small: %v / %v", got.DurMS, d)
	}
	if got.Err != "boom" || got.Proc == nil || got.Proc.CPUUserMS != 3 || got.Go == nil {
		t.Fatalf("missing fields: %+v", got)
	}
	if got.Attrs["engine"] != "yt-dlp" || got.Attrs["bytes"].(float64) != 42 {
		t.Fatalf("attrs: %+v", got.Attrs)
	}
}

func TestNilRecorderIsSafe(t *testing.T) {
	var r *perf.Recorder
	s := r.Start("x", "k", 1)
	s.Set("a", 2)
	s.End(nil)
	r.Meta("m", nil)
	r.Write(perf.Record{Kind: "span"})
	_ = r.Sample("s", nil, 0, nil)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSamplerComputesCPU(t *testing.T) {
	perftest.Track(t)
	var buf bytes.Buffer
	r := perf.New(&buf, "run")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	r.RunSampler(ctx, 30*time.Millisecond, func() map[string]any { return map[string]any{"active": 1} })
	recs := decode(t, buf.Bytes())
	if len(recs) < 2 {
		t.Fatalf("want >=2 samples, got %d", len(recs))
	}
	for _, rec := range recs {
		if rec.Kind != "sample" || rec.Go == nil || rec.Go.Goroutines == 0 {
			t.Fatalf("bad sample %+v", rec)
		}
		if _, ok := rec.Attrs["cpu_pct"]; !ok || rec.Attrs["active"].(float64) != 1 {
			t.Fatalf("missing attrs: %+v", rec.Attrs)
		}
	}
}

func TestPprofLoopbackOnly(t *testing.T) {
	if _, err := perf.ServePprof("0.0.0.0:0", nil); err == nil {
		t.Fatal("non-loopback pprof address must be rejected")
	}
}

func TestPprofServes(t *testing.T) {
	perftest.Track(t)
	s, err := perf.ServePprof("127.0.0.1:0", func() any { return map[string]int{"active": 3} })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	for _, path := range []string{"/debug/pprof/heap?debug=1", "/debug/pprof/goroutine?debug=1", "/debug/perf/snapshot"} {
		resp, err := http.Get("http://" + s.Addr + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || len(body) == 0 {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		if strings.HasPrefix(path, "/debug/perf") && !strings.Contains(string(body), `"active":3`) {
			t.Fatalf("snapshot missing app state: %s", body)
		}
	}
}

func BenchmarkSpanEnd(b *testing.B) {
	r := perf.New(io.Discard, "bench")
	b.ReportAllocs()
	for b.Loop() {
		s := r.StartJob("j", "stage.x", "k", 1)
		s.End(nil)
	}
}

func BenchmarkReadGoStats(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = perf.ReadGoStats()
	}
}
