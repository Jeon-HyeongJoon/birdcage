package related

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/engine/ytdlp"
	"birdcage/helper/internal/perf/perftest"
)

// onlineSeeds: 분야가 다른 공개 영상 10개(2026-10-04 탐색 실측과 같은 집합).
var onlineSeeds = []string{"jNQXAC9IVRw", "aqz-KE-bpKQ", "dQw4w9WgXcQ", "9bZkp7q19f0", "kJQP7kiw5Fk",
	"M7lc1UVf-VE", "YbJOTdZBX1g", "fJ9rUzIMcZQ", "sVFIm1rKrn8", "ASF56zUwB7Y"}

// TestEndToEndOnlineRelated는 T-036 자동 적합성 기준을 실제 YouTube로 잰다(G-e2e, BIRDCAGE_E2E=1):
// 시드 90% 이상이 10개 이상, 첫 목록 지연 중앙값 1.5s 이하. 형식이 바뀌면 여기서 먼저 깨진다.
func TestEndToEndOnlineRelated(t *testing.T) {
	if os.Getenv("BIRDCAGE_E2E") != "1" {
		t.Skip("set BIRDCAGE_E2E=1 to fetch real watch pages")
	}
	perftest.Track(t)
	s := &Service{Rec: perftest.Recorder()}
	var lat []time.Duration
	enough := 0
	for _, id := range onlineSeeds {
		t0 := time.Now()
		r, err := s.List(context.Background(), Seed{ID: id}, SourceYouTube, nil) // 대체 없이 youtube 출처만
		d := time.Since(t0)
		lat = append(lat, d)
		complete := 0
		for _, it := range r.Items {
			if it.Title != "" && it.Channel != "" && it.DurationSec > 0 && it.Thumbnail != "" {
				complete++
			}
		}
		t.Logf("%s: %v items=%d complete=%d err=%v", id, d.Round(time.Millisecond), len(r.Items), complete, err)
		if err == nil && len(r.Items) >= 10 {
			enough++
		}
		time.Sleep(500 * time.Millisecond)
	}
	slices.Sort(lat)
	median := (lat[len(lat)/2-1] + lat[len(lat)/2]) / 2
	t.Logf("seeds with ≥10 items: %d/%d, median latency %v", enough, len(onlineSeeds), median.Round(time.Millisecond))
	if enough*10 < len(onlineSeeds)*9 {
		t.Errorf("coverage %d/%d < 90%%", enough, len(onlineSeeds))
	}
	if median > 1500*time.Millisecond {
		t.Errorf("median latency %v > 1.5s", median)
	}
}

// TestEndToEndOnlineRelatedListing: 같은 채널·제목 검색 대체 경로가 실제 yt-dlp로 동작하는지.
func TestEndToEndOnlineRelatedListing(t *testing.T) {
	if os.Getenv("BIRDCAGE_E2E") != "1" {
		t.Skip("set BIRDCAGE_E2E=1 to list real channels")
	}
	y := os.Getenv("BIRDCAGE_YTDLP")
	if y == "" {
		if p, err := exec.LookPath("yt-dlp"); err == nil {
			y = p
		}
	}
	if y == "" {
		t.Skip("BIRDCAGE_YTDLP not set")
	}
	perftest.Track(t)
	var lister engine.Lister = &ytdlp.Engine{YtDlp: y}
	s := &Service{Rec: perftest.Recorder()}
	seed := Seed{ID: "aqz-KE-bpKQ", Title: "Big Buck Bunny 60fps 4K - Official Blender Foundation Short Film", Channel: "Blender",
		ChannelURL: "https://www.youtube.com/channel/UCSMOQeBJ2RAnuFungnQOxLg"}
	for _, src := range []Source{SourceChannel, SourceSearch} {
		t0 := time.Now()
		r, err := s.List(context.Background(), seed, src, lister)
		t.Logf("%s: %v source=%s items=%d err=%v", src, time.Since(t0).Round(time.Millisecond), r.Source, len(r.Items), err)
		if err != nil || r.Source != src || len(r.Items) < 5 {
			t.Errorf("%s listing: source=%s items=%d err=%v", src, r.Source, len(r.Items), err)
		}
	}
}
