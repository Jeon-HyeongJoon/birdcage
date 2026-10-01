// Command perfreport summarizes a test run's performance logs and compares it
// with a baseline run.
//
// 입력(실행 디렉터리 하나): gotest.jsonl(go test -json), <패키지>.jsonl(perftest 기록),
// helper-e2e.jsonl(E2E 중 Helper 자체 기록), bench.txt(-bench 출력, 선택).
// 출력: 표준출력 + <dir>/summary.md. 기준 실행보다 20% 이상 느려진 항목은 "회귀"로 표시한다.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"birdcage/helper/internal/perf"
)

type testResult struct {
	Pkg, Name, Action string
	Elapsed           float64 // seconds
	Output            []string
}

type spanStats struct {
	name                  string
	durs                  []float64
	cpu                   []float64
	maxRSS                int64
	errs                  int
	p50, p95, max, cpuAvg float64
}

type pkgStats struct {
	name                             string
	durMS, cpuSec, gcPauseMS         float64
	allocBytes, peakHeap, mallocs    float64
	gcCycles, peakGoroutines, exitCd float64
}

type bench struct {
	name                    string
	nsOp, bOp, allocsOp, mb float64
}

type run struct {
	dir     string
	tests   map[string]*testResult // pkg/test
	pkgs    map[string]*testResult
	spans   map[string]*spanStats
	pstats  map[string]*pkgStats
	benches map[string]*bench
	samples struct {
		n                     int
		maxHeap, maxGor, maxC float64
	}
}

func main() {
	dir := flag.String("dir", "", "run directory (default: .perf/runs/<.perf/LATEST>)")
	base := flag.String("baseline", "", "baseline run directory to compare with")
	threshold := flag.Float64("threshold", 0.20, "regression threshold (fraction slower)")
	flag.Parse()
	if *dir == "" {
		b, err := os.ReadFile(filepath.Join(".perf", "LATEST"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "no -dir and no .perf/LATEST")
			os.Exit(2)
		}
		*dir = filepath.Join(".perf", "runs", strings.TrimSpace(string(b)))
	}
	cur, err := load(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var baseline *run
	if *base != "" {
		if baseline, err = load(*base); err != nil {
			fmt.Fprintln(os.Stderr, "baseline:", err)
		}
	}
	var sb strings.Builder
	report(&sb, cur, baseline, *threshold)
	fmt.Print(sb.String())
	_ = os.WriteFile(filepath.Join(*dir, "summary.md"), []byte(sb.String()), 0o644)
}

func load(dir string) (*run, error) {
	r := &run{dir: dir, tests: map[string]*testResult{}, pkgs: map[string]*testResult{}, spans: map[string]*spanStats{},
		pstats: map[string]*pkgStats{}, benches: map[string]*bench{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch {
		case e.Name() == "gotest.jsonl":
			if err := r.loadGoTest(p); err != nil {
				return nil, err
			}
		case e.Name() == "bench.txt":
			r.loadBench(p)
		case strings.HasSuffix(e.Name(), ".jsonl"):
			r.loadPerf(p)
		}
	}
	for _, s := range r.spans {
		s.finish()
	}
	return r, nil
}

func (r *run) loadGoTest(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var ev struct {
			Action, Package, Test, Output string
			Elapsed                       float64
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		key := ev.Package + "/" + ev.Test
		m := r.tests
		if ev.Test == "" {
			m, key = r.pkgs, ev.Package
		}
		t := m[key]
		if t == nil {
			t = &testResult{Pkg: ev.Package, Name: ev.Test}
			m[key] = t
		}
		switch ev.Action {
		case "pass", "fail", "skip":
			t.Action, t.Elapsed = ev.Action, ev.Elapsed
		case "output":
			if len(t.Output) < 200 {
				t.Output = append(t.Output, strings.TrimRight(ev.Output, "\n"))
			}
		}
	}
	return sc.Err()
}

func (r *run) loadPerf(path string) {
	src := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	src = strings.TrimPrefix(strings.ReplaceAll(src, "_", "/"), "internal/")
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var rec perf.Record
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		switch rec.Kind {
		case "span":
			// 같은 이름이라도 기록한 패키지(파일)가 다르면 다른 작업이므로 따로 집계한다.
			key := src + " › " + rec.Name
			s := r.spans[key]
			if s == nil {
				s = &spanStats{name: key}
				r.spans[key] = s
			}
			s.durs = append(s.durs, rec.DurMS)
			if rec.Proc != nil {
				s.cpu = append(s.cpu, rec.Proc.CPUUserMS+rec.Proc.CPUSysMS)
				s.maxRSS = max(s.maxRSS, rec.Proc.MaxRSSBytes)
			}
			if rec.Err != "" {
				s.errs++
			}
		case "test":
			a := rec.Attrs
			r.pstats[rec.Name] = &pkgStats{name: rec.Name, durMS: rec.DurMS, cpuSec: num(a["cpu_sec"]), gcPauseMS: num(a["gc_pause_ms"]),
				allocBytes: num(a["alloc_bytes"]), peakHeap: num(a["peak_heap_bytes"]), mallocs: num(a["mallocs"]),
				gcCycles: num(a["gc_cycles"]), peakGoroutines: num(a["peak_goroutines"]), exitCd: num(a["exit"])}
		case "sample":
			r.samples.n++
			if rec.Go != nil {
				r.samples.maxHeap = math.Max(r.samples.maxHeap, float64(rec.Go.HeapLiveBytes))
				r.samples.maxGor = math.Max(r.samples.maxGor, float64(rec.Go.Goroutines))
			}
			r.samples.maxC = math.Max(r.samples.maxC, num(rec.Attrs["cpu_pct"]))
		}
	}
}

var benchRe = regexp.MustCompile(`^(Benchmark\S+?)(?:-\d+)?\s+\d+\s+([\d.]+) ns/op(?:\s+([\d.]+) MB/s)?(?:\s+([\d.]+) B/op)?(?:\s+([\d.]+) allocs/op)?`)

func (r *run) loadBench(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	pkg := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "pkg: ") {
			pkg = strings.TrimPrefix(line, "pkg: ")
			continue
		}
		m := benchRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		b := &bench{name: strings.TrimPrefix(pkg, "birdcage/helper/") + "." + m[1]}
		b.nsOp, _ = strconv.ParseFloat(m[2], 64)
		b.mb, _ = strconv.ParseFloat(m[3], 64)
		b.bOp, _ = strconv.ParseFloat(m[4], 64)
		b.allocsOp, _ = strconv.ParseFloat(m[5], 64)
		r.benches[b.name] = b
	}
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return 0
}

func (s *spanStats) finish() {
	d := append([]float64(nil), s.durs...)
	sort.Float64s(d)
	s.p50, s.p95, s.max = pct(d, 0.5), pct(d, 0.95), d[len(d)-1]
	if len(s.cpu) > 0 {
		var sum float64
		for _, c := range s.cpu {
			sum += c
		}
		s.cpuAvg = sum / float64(len(s.cpu))
	}
}

func pct(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(q*float64(len(sorted)))) - 1
	return sorted[max(0, min(i, len(sorted)-1))]
}

func delta(cur, base float64) string {
	if base <= 0 {
		return ""
	}
	return fmt.Sprintf("%+.0f%%", (cur-base)/base*100)
}

func mib(b float64) string { return fmt.Sprintf("%.1f MiB", b/(1<<20)) }

func report(w io.Writer, cur, base *run, th float64) {
	var pass, fail, skip int
	var total float64
	for _, t := range cur.tests {
		switch t.Action {
		case "pass":
			pass++
		case "fail":
			fail++
		case "skip":
			skip++
		}
	}
	for _, p := range cur.pkgs {
		total += p.Elapsed
	}
	fmt.Fprintf(w, "# 성능 리포트: %s\n\n", filepath.Base(cur.dir))
	if base != nil {
		fmt.Fprintf(w, "기준 실행: %s\n\n", filepath.Base(base.dir))
	}
	fmt.Fprintf(w, "테스트: 통과 %d · 실패 %d · 건너뜀 %d · 패키지 합계 %.2fs\n\n", pass, fail, skip, total)

	var regressions []string

	if fail > 0 {
		fmt.Fprintln(w, "## 실패한 테스트")
		for _, t := range sortedTests(cur.tests) {
			if t.Action == "fail" {
				fmt.Fprintf(w, "- %s %s\n", t.Pkg, t.Name)
				for _, o := range tail(t.Output, 8) {
					fmt.Fprintf(w, "    %s\n", o)
				}
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "## 느린 테스트 Top 10")
	fmt.Fprintln(w, "| 테스트 | 시간 | 기준 대비 |\n|---|---:|---:|")
	ts := sortedTests(cur.tests)
	sort.Slice(ts, func(i, j int) bool { return ts[i].Elapsed > ts[j].Elapsed })
	for i, t := range ts {
		if i >= 10 {
			break
		}
		d := ""
		if base != nil {
			if b := base.tests[t.Pkg+"/"+t.Name]; b != nil {
				d = delta(t.Elapsed, b.Elapsed)
			}
		}
		fmt.Fprintf(w, "| %s %s | %.3fs | %s |\n", short(t.Pkg), t.Name, t.Elapsed, d)
	}
	if base != nil {
		for _, t := range ts {
			// 둘 다 통과한 경우만, 그리고 50ms 이상 차이날 때만(병렬 실행 잡음 제외).
			if b := base.tests[t.Pkg+"/"+t.Name]; b != nil && t.Action == "pass" && b.Action == "pass" &&
				t.Elapsed > b.Elapsed*(1+th) && t.Elapsed-b.Elapsed >= 0.05 {
				regressions = append(regressions, fmt.Sprintf("테스트 %s %s: %.3fs → %.3fs (%s)", short(t.Pkg), t.Name, b.Elapsed, t.Elapsed, delta(t.Elapsed, b.Elapsed)))
			}
		}
	}

	fmt.Fprintln(w, "\n## 패키지 자원 사용 (TestMain 기준)")
	fmt.Fprintln(w, "| 패키지 | 시간 | CPU | 할당 | malloc | GC | GC 정지 | 최대 힙 | 최대 고루틴 |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, name := range sortedKeys(cur.pstats) {
		p := cur.pstats[name]
		fmt.Fprintf(w, "| %s | %.0fms | %.2fs | %s | %.0f | %.0f | %.2fms | %s | %.0f |\n", name, p.durMS, p.cpuSec, mib(p.allocBytes),
			p.mallocs, p.gcCycles, p.gcPauseMS, mib(p.peakHeap), p.peakGoroutines)
	}

	if len(cur.spans) > 0 {
		fmt.Fprintln(w, "\n## 구간(span) 측정")
		fmt.Fprintln(w, "| 구간 | 횟수 | p50 | p95 | 최대 | 자식 CPU 평균 | 자식 최대 RSS | 오류 | p50 기준 대비 |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|")
		for _, name := range sortedKeys(cur.spans) {
			s := cur.spans[name]
			if strings.Contains(name, "› test:") {
				continue // 테스트 단위 시간은 위 표에 있다
			}
			d := ""
			if base != nil {
				// 표본 수가 같을 때만 비교한다(테스트 구성이 바뀌면 분포 자체가 달라짐). 10ms 미만 차이는 무시.
				if b := base.spans[name]; b != nil && len(b.durs) == len(s.durs) {
					d = delta(s.p50, b.p50)
					if s.p50 > b.p50*(1+th) && s.p50-b.p50 >= 10 {
						regressions = append(regressions, fmt.Sprintf("구간 %s p50: %.1fms → %.1fms (%s)", name, b.p50, s.p50, d))
					}
				}
			}
			rss := ""
			if s.maxRSS > 0 {
				rss = mib(float64(s.maxRSS))
			}
			fmt.Fprintf(w, "| %s | %d | %.1fms | %.1fms | %.1fms | %.1fms | %s | %d | %s |\n", name, len(s.durs), s.p50, s.p95, s.max, s.cpuAvg, rss, s.errs, d)
		}
	}

	if cur.samples.n > 0 {
		fmt.Fprintf(w, "\n## Helper 런타임 샘플 (E2E)\n샘플 %d개 · 최대 힙 %s · 최대 고루틴 %.0f · 최대 CPU %.1f%%\n",
			cur.samples.n, mib(cur.samples.maxHeap), cur.samples.maxGor, cur.samples.maxC)
	}

	if len(cur.benches) > 0 {
		fmt.Fprintln(w, "\n## 벤치마크")
		fmt.Fprintln(w, "| 벤치마크 | ns/op | B/op | allocs/op | ns/op 기준 대비 |\n|---|---:|---:|---:|---:|")
		for _, name := range sortedKeys(cur.benches) {
			b := cur.benches[name]
			d := ""
			if base != nil {
				if bb := base.benches[name]; bb != nil {
					d = delta(b.nsOp, bb.nsOp)
					if b.nsOp > bb.nsOp*(1+th) {
						regressions = append(regressions, fmt.Sprintf("벤치 %s: %.0f → %.0f ns/op (%s)", name, bb.nsOp, b.nsOp, d))
					}
				}
			}
			fmt.Fprintf(w, "| %s | %.0f | %.0f | %.0f | %s |\n", name, b.nsOp, b.bOp, b.allocsOp, d)
		}
	}

	if base != nil {
		fmt.Fprintf(w, "\n## 회귀 (기준 대비 +%.0f%% 이상, 같은 조건끼리만 비교)\n", th*100)
		if len(regressions) == 0 {
			fmt.Fprintln(w, "없음")
		}
		for _, r := range regressions {
			fmt.Fprintln(w, "- "+r)
		}
	}
}

func sortedTests(m map[string]*testResult) []*testResult {
	out := make([]*testResult, 0, len(m))
	for _, t := range m {
		if t.Action != "" {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pkg+out[i].Name < out[j].Pkg+out[j].Name })
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func short(pkg string) string { return strings.TrimPrefix(pkg, "birdcage/helper/") }

func tail(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
