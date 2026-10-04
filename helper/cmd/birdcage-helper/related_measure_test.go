package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/preview"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/related"
	"birdcage/helper/internal/tools"
)

// TestMeasurePreviewWithRelated는 넓은 패널에서 탐색 목록(시청 페이지 1.2MB)을 미리보기와 동시에 받을 때
// 미리보기 단계 시간이 느려지는지 잰다(T-036 수용 기준, W4). 영상마다 혼자/동시를 번갈아 순서를 바꾸고,
// 매번 미리보기 캐시를 지워 같은 조건에서 받는다. BIRDCAGE_MEASURE=1일 때만(실제 YouTube, 약 1분).
func TestMeasurePreviewWithRelated(t *testing.T) {
	if os.Getenv("BIRDCAGE_MEASURE") != "1" {
		t.Skip("set BIRDCAGE_MEASURE=1 to measure preview contention on real platforms")
	}
	perftest.Track(t)
	tp := toolPaths(t)
	data := t.TempDir()
	args := []string{"--yt-dlp", tp[tools.YtDlp], "--ffmpeg", tp[tools.FFmpeg], "--ffprobe", tp[tools.FFprobe], "--data-dir", data}
	if d := tp[tools.Deno]; d != "" {
		args = append(args, "--deno", d)
	}
	h := startInProcess(t, args...)
	c := connect(t, h.ready)
	c.send(protocol.TypeHello, "h", protocol.Hello{Client: "measure", Host: protocol.Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}})
	c.until(30*time.Second, func(e protocol.Envelope) bool { return e.Type == protocol.TypeHelloOK })

	type result struct{ peaks, ready, list float64 }
	run := func(probeID string, withRelated bool) result {
		if err := os.RemoveAll(filepath.Join(data, "preview")); err != nil {
			t.Fatal(err)
		}
		t0 := time.Now()
		ms := func() float64 { return float64(time.Since(t0).Milliseconds()) }
		c.send(protocol.TypePreviewOpen, "v", protocol.PreviewOpen{ProbeID: probeID})
		if withRelated {
			c.send(protocol.TypeRelatedFetch, "r", protocol.RelatedFetch{ProbeID: probeID, Source: related.SourceYouTube})
		}
		var r result
		for {
			env := c.until(5*time.Minute, func(e protocol.Envelope) bool {
				if e.Type == protocol.TypeRelatedList && r.list == 0 {
					r.list = ms()
				}
				return e.Type == protocol.TypePreviewState
			})
			var st preview.State
			_ = protocol.DecodeData(env, &st)
			if st.Stage == preview.StageVideo && r.peaks == 0 {
				r.peaks = ms()
			}
			if st.Stage == preview.StageFailed {
				t.Fatalf("preview failed: %s", st.Error)
			}
			if st.Stage == preview.StageReady {
				r.ready = ms()
				break
			}
		}
		if withRelated && r.list == 0 { // 목록이 미리보기보다 늦게 오면 기다려 잰다
			c.until(time.Minute, func(e protocol.Envelope) bool { return e.Type == protocol.TypeRelatedList })
			r.list = ms()
		}
		return r
	}

	var alone, with []result
	for i, id := range []string{"aqz-KE-bpKQ", "fJ9rUzIMcZQ", "kJQP7kiw5Fk"} {
		c.send(protocol.TypeMediaProbe, "p", protocol.MediaProbe{URL: "https://www.youtube.com/watch?v=" + id})
		var mi protocol.MediaInfo
		_ = protocol.DecodeData(c.until(2*time.Minute, func(e protocol.Envelope) bool { return e.Type == protocol.TypeMediaInfo }), &mi)
		if mi.Info == nil {
			t.Fatalf("probe %s: %+v", id, mi.Failure)
		}
		order := []bool{false, true}
		if i%2 == 1 {
			order = []bool{true, false} // 순서 효과(CDN 예열)를 상쇄
		}
		for _, w := range order {
			r := run(mi.ProbeID, w)
			t.Logf("%s related=%v: peaks %.0fms ready %.0fms list %.0fms", id, w, r.peaks, r.ready, r.list)
			if w {
				with = append(with, r)
			} else {
				alone = append(alone, r)
			}
		}
	}
	sum := func(rs []result, f func(result) float64) (s float64) {
		for _, r := range rs {
			s += f(r)
		}
		return s
	}
	peaksA, peaksW := sum(alone, func(r result) float64 { return r.peaks }), sum(with, func(r result) float64 { return r.peaks })
	readyA, readyW := sum(alone, func(r result) float64 { return r.ready }), sum(with, func(r result) float64 { return r.ready })
	t.Logf("total peaks alone %.0fms vs with related %.0fms (%+.1f%%); ready %.0fms vs %.0fms (%+.1f%%)",
		peaksA, peaksW, (peaksW/peaksA-1)*100, readyA, readyW, (readyW/readyA-1)*100)
}
