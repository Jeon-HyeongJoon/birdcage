package main

import (
	"os"
	"testing"
	"time"

	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/preview"
	"birdcage/helper/internal/protocol"
	"birdcage/helper/internal/tools"
)

// TestEndToEndOnlinePreview: 실제 YouTube 조회 → 미리보기(파형 → 360p H.264 재생본), 다시 열면 캐시.
// 짧은 영상(스토리보드 없음 → 로컬 생성)과 10분 영상(플랫폼 스토리보드)을 둘 다 본다. 단계별 시간을 기록한다.
func TestEndToEndOnlinePreview(t *testing.T) {
	if os.Getenv("BIRDCAGE_E2E") != "1" {
		t.Skip("set BIRDCAGE_E2E=1 to preview real platforms")
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

	for _, tc := range []struct {
		url           string
		platformBoard bool
	}{
		{"https://www.youtube.com/watch?v=jNQXAC9IVRw", false},
		{"https://www.youtube.com/watch?v=aqz-KE-bpKQ", true},
	} {
		t.Run(tc.url, func(t *testing.T) {
			c.send(protocol.TypeMediaProbe, "p", protocol.MediaProbe{URL: tc.url})
			var mi protocol.MediaInfo
			_ = protocol.DecodeData(c.until(2*time.Minute, func(e protocol.Envelope) bool { return e.Type == protocol.TypeMediaInfo }), &mi)
			if mi.Info == nil {
				t.Fatalf("probe: %+v", mi.Failure)
			}
			if (len(mi.Info.Storyboards) > 0) != tc.platformBoard {
				t.Fatalf("platform storyboards %d", len(mi.Info.Storyboards))
			}
			open := func() (preview.State, map[preview.Stage]float64) {
				t0 := time.Now()
				at := map[preview.Stage]float64{}
				c.send(protocol.TypePreviewOpen, "v", protocol.PreviewOpen{ProbeID: mi.ProbeID})
				var st preview.State
				for st.Stage != preview.StageReady && st.Stage != preview.StageFailed {
					st = preview.State{}
					_ = protocol.DecodeData(c.until(5*time.Minute, func(e protocol.Envelope) bool { return e.Type == protocol.TypePreviewState }), &st)
					if _, seen := at[st.Stage]; !seen {
						at[st.Stage] = float64(time.Since(t0).Milliseconds())
					}
				}
				return st, at
			}
			st, at := open()
			if st.Stage != preview.StageReady || st.Peaks == nil || st.Video == nil || st.Video.Height > preview.MaxHeight {
				t.Fatalf("preview %+v", st)
			}
			if !tc.platformBoard && (st.Storyboard == nil || len(st.Storyboard.Sprites) == 0) {
				t.Fatal("no storyboard generated for a source without one")
			}
			if _, err := os.Stat(st.Video.Path); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s (%.0fs): peaks at %.0fms, ready at %.0fms, video %dx%d, buckets %d", mi.Info.ID, mi.Info.DurationSec, at[preview.StageVideo], at[preview.StageReady], st.Video.Width, st.Video.Height, st.Peaks.Count)
			again, at2 := open()
			if !again.Cached || again.Stage != preview.StageReady {
				t.Fatalf("second open not cached: %+v", again)
			}
			t.Logf("  cached reopen ready at %.0fms", at2[preview.StageReady])
		})
	}
}
