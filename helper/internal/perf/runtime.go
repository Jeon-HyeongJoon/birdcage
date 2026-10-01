package perf

import (
	"context"
	"math"
	"runtime/metrics"
	"time"
)

// GoStats is a cheap snapshot of the Go runtime (runtime/metrics, no stop-the-world).
type GoStats struct {
	HeapLiveBytes   uint64 `json:"heap_live_bytes"`
	HeapObjects     uint64 `json:"heap_objects"`
	TotalAllocBytes uint64 `json:"total_alloc_bytes"`
	Goroutines      uint64 `json:"goroutines"`
	GCCycles        uint64 `json:"gc_cycles"`
	// CPUProcSec is OS-reported user+system CPU of the helper process.
	// (runtime/metrics의 /cpu/classes는 GC 주기마다만 갱신돼 짧은 구간에선 0이 나와 쓰지 않는다.)
	CPUProcSec   float64 `json:"cpu_proc_sec"`
	CPUGCSec     float64 `json:"cpu_gc_sec"`
	GCPauseP99MS float64 `json:"gc_pause_p99_ms"`
}

var sampleNames = []string{
	"/memory/classes/heap/objects:bytes",
	"/gc/heap/objects:objects",
	"/gc/heap/allocs:bytes",
	"/sched/goroutines:goroutines",
	"/gc/cycles/total:gc-cycles",
	"/cpu/classes/gc/total:cpu-seconds",
	"/sched/pauses/total/gc:seconds",
}

// ReadGoStats reads the current runtime metrics.
func ReadGoStats() GoStats {
	samples := make([]metrics.Sample, len(sampleNames))
	for i, n := range sampleNames {
		samples[i].Name = n
	}
	metrics.Read(samples)
	g := GoStats{CPUProcSec: ProcessCPUSeconds()}
	for _, s := range samples {
		switch s.Name {
		case "/memory/classes/heap/objects:bytes":
			g.HeapLiveBytes = u64(s.Value)
		case "/gc/heap/objects:objects":
			g.HeapObjects = u64(s.Value)
		case "/gc/heap/allocs:bytes":
			g.TotalAllocBytes = u64(s.Value)
		case "/sched/goroutines:goroutines":
			g.Goroutines = u64(s.Value)
		case "/gc/cycles/total:gc-cycles":
			g.GCCycles = u64(s.Value)
		case "/cpu/classes/gc/total:cpu-seconds":
			g.CPUGCSec = f64(s.Value)
		case "/sched/pauses/total/gc:seconds":
			if s.Value.Kind() == metrics.KindFloat64Histogram {
				g.GCPauseP99MS = quantile(s.Value.Float64Histogram(), 0.99) * 1000
			}
		}
	}
	return g
}

func u64(v metrics.Value) uint64 {
	if v.Kind() == metrics.KindUint64 {
		return v.Uint64()
	}
	return 0
}

func f64(v metrics.Value) float64 {
	if v.Kind() == metrics.KindFloat64 {
		return v.Float64()
	}
	return 0
}

func quantile(h *metrics.Float64Histogram, q float64) float64 {
	var total uint64
	for _, c := range h.Counts {
		total += c
	}
	if total == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(total) * q))
	var acc uint64
	for i, c := range h.Counts {
		acc += c
		if acc >= target {
			// Buckets[i+1] is the upper bound of bucket i; it may be +Inf.
			ub := h.Buckets[i+1]
			if math.IsInf(ub, 1) {
				return h.Buckets[i]
			}
			return ub
		}
	}
	return 0
}

// Sample writes one "sample" record with the runtime state and CPU utilisation
// since the previous sample.
func (r *Recorder) Sample(name string, prev *GoStats, interval time.Duration, attrs map[string]any) GoStats {
	g := ReadGoStats()
	if r == nil {
		return g
	}
	a := map[string]any{}
	for k, v := range attrs {
		a[k] = v
	}
	if prev != nil && interval > 0 {
		// OS가 보고한 프로세스 CPU 시간 기준 사용률(코어 1개 = 100%).
		a["cpu_pct"] = math.Round((g.CPUProcSec-prev.CPUProcSec)/interval.Seconds()*1000) / 10
		a["alloc_rate_bps"] = float64(g.TotalAllocBytes-prev.TotalAllocBytes) / interval.Seconds()
	}
	r.Write(Record{Kind: "sample", Name: name, Go: &g, Attrs: a})
	return g
}

// RunSampler writes a sample every interval until ctx is done. extra may supply
// additional attributes (e.g. active job count) and may be nil.
func (r *Recorder) RunSampler(ctx context.Context, every time.Duration, extra func() map[string]any) {
	if r == nil || every <= 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	prev := ReadGoStats()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			var a map[string]any
			if extra != nil {
				a = extra()
			}
			g := r.Sample("runtime", &prev, now.Sub(last), a)
			prev, last = g, now
		}
	}
}
