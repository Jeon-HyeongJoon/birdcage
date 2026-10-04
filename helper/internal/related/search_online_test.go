package related

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"birdcage/helper/internal/perf/perftest"
)

// TestEndToEndOnlineSearch는 T-026 자동 기준을 실제 YouTube로 잰다(G-e2e, BIRDCAGE_E2E=1):
// 검색어 5개 모두 첫 페이지 10개 이상, 첫 페이지 중앙값 1.5s 이하, 더 보기 +3개 이상.
// 유튜브는 같은 검색어에도 요청마다 쇼츠 묶음 비중이 달라 영상 수가 바뀐다(3회 15건 실측: 첫 페이지 12–20개,
// 더 보기 +4–13개). 처음 기준(15개·+10개)은 표본 5개로 정한 값이라 실측 하한에 맞췄다(docs/decisions/0013).
func TestEndToEndOnlineSearch(t *testing.T) {
	if os.Getenv("BIRDCAGE_E2E") != "1" {
		t.Skip("set BIRDCAGE_E2E=1 to search real YouTube")
	}
	perftest.Track(t)
	s := &Service{Rec: perftest.Recorder()}
	var lat []time.Duration
	for i, q := range []string{"코끼리 브이로그", "drone shot city night", "인터뷰 b-roll", "cooking asmr", "축구 하이라이트"} {
		t0 := time.Now()
		p0, err := s.Search(context.Background(), q, 0, nil) // 대체 없이 결과 페이지만
		d := time.Since(t0)
		lat = append(lat, d)
		if err != nil || len(p0.Items) < 10 || !p0.More {
			t.Errorf("%s: %v items=%d more=%v err=%v", q, d, len(p0.Items), p0.More, err)
			continue
		}
		msg := ""
		if i < 2 { // 더 보기는 두 검색어만(요청 수를 줄인다)
			t1 := time.Now()
			p1, err := s.Search(context.Background(), q, 1, nil)
			if err != nil || len(p1.Items) < 3 {
				t.Errorf("%s more: items=%d err=%v", q, len(p1.Items), err)
			}
			msg = " more=" + time.Since(t1).Round(time.Millisecond).String()
		}
		t.Logf("%s: %v items=%d%s", q, d.Round(time.Millisecond), len(p0.Items), msg)
		time.Sleep(500 * time.Millisecond)
	}
	slices.Sort(lat)
	if m := lat[len(lat)/2]; m > 1500*time.Millisecond {
		t.Errorf("median first page %v > 1.5s", m)
	} else {
		t.Logf("median first page %v", m.Round(time.Millisecond))
	}
}
