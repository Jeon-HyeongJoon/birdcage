package rangefetch

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/proc"
)

func TestMain(m *testing.M) { perftest.Main(m) }

// fragmented builds DASH-like files (ftyp, moov, sidx at the front, 2s fragments) like YouTube's.
func fragmented(t *testing.T) (dir, video, audio string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found")
	}
	dir = t.TempDir()
	run := func(args ...string) {
		if _, err := proc.Run(context.Background(), proc.Spec{Path: ffmpeg, StderrTail: 5, Args: append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)}); err != nil {
			t.Fatal(err)
		}
	}
	video = filepath.Join(dir, "v.mp4")
	audio = filepath.Join(dir, "a.m4a")
	run("-f", "lavfi", "-i", "testsrc2=s=320x180:r=30:d=20", "-c:v", "libx264", "-preset", "ultrafast", "-g", "60", "-keyint_min", "60",
		"-sc_threshold", "0", "-pix_fmt", "yuv420p", "-movflags", "+frag_keyframe+empty_moov+global_sidx+default_base_moof", video)
	run("-f", "lavfi", "-i", "anoisesrc=d=20:r=48000:a=0.3", "-c:a", "aac", "-frag_duration", "2000000",
		"-movflags", "+frag_keyframe+empty_moov+global_sidx+default_base_moof", audio)
	return dir, video, audio
}

func TestParseIndex(t *testing.T) {
	_, video, _ := fragmented(t)
	b, _ := os.ReadFile(video)
	ix, err := ParseIndex(b[:64<<10])
	if err != nil {
		t.Fatal(err)
	}
	if ix.InitEnd != 771 || len(ix.Refs) != 10 || math.Abs(ix.Refs[3].Start-6) > 0.01 || math.Abs(ix.Refs[3].Dur-2) > 0.01 {
		t.Fatalf("index init %d refs %d r3 %+v", ix.InitEnd, len(ix.Refs), ix.Refs[3])
	}
	// 색인은 마지막 조각 끝까지다. 그 뒤는 조각이 아닌 mfra(임의 접근 색인) 상자뿐이다.
	last := ix.Refs[len(ix.Refs)-1]
	if end := last.Offset + last.Size; end > int64(len(b)) || (end < int64(len(b)) && string(b[end+4:end+8]) != "mfra") {
		t.Fatalf("refs end at %d, file is %d bytes", end, len(b))
	}
	if f, l := ix.Span(5.3, 8.1); f != 2 || l != 4 { // [4,6) [6,8) [8,10)
		t.Fatalf("span %d..%d", f, l)
	}
	if f, l := ix.Span(99, 100); f != 9 || l != 9 {
		t.Fatalf("span past end %d..%d", f, l)
	}
	// 비디오·오디오가 한 조각 파일에 섞이면 대체 경로로 넘긴다(조각 시각이 21ms 어긋남, 실측).
	ffmpeg, _ := exec.LookPath("ffmpeg")
	muxed := filepath.Join(t.TempDir(), "muxed.mp4")
	if _, err := proc.Run(context.Background(), proc.Spec{Path: ffmpeg, StderrTail: 5, Args: []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=s=160x90:r=30:d=6", "-f", "lavfi", "-i", "sine=d=6", "-c:v", "libx264", "-preset", "ultrafast", "-g", "60",
		"-c:a", "aac", "-movflags", "+frag_keyframe+empty_moov+global_sidx+default_base_moof", muxed}}); err != nil {
		t.Fatal(err)
	}
	mb, _ := os.ReadFile(muxed)
	if _, err := ParseIndex(mb[:min(len(mb), 64<<10)]); !errors.Is(err, ErrNoIndex) {
		t.Fatalf("muxed fragmented file must fall back: %v", err)
	}
	// 조각 mp4가 아니면(색인 없음) 대체 경로로 넘긴다.
	if _, err := ParseIndex([]byte("\x00\x00\x00\x10ftypisom\x00\x00\x00\x00\x00\x00\x00\x08mdat")); !errors.Is(err, ErrNoIndex) {
		t.Fatalf("plain file: %v", err)
	}
}

func TestFetchSectionKeepsSourceTimes(t *testing.T) {
	perftest.Track(t)
	dir, video, _ := fragmented(t)
	var requests, big atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("X-Test") != "yes" {
			http.Error(w, "missing header", http.StatusBadRequest)
			return
		}
		if rg := r.Header.Get("Range"); rg != "" {
			a, b, _ := strings.Cut(strings.TrimPrefix(rg, "bytes="), "-")
			x, _ := strconv.Atoi(a)
			y, _ := strconv.Atoi(b)
			if y-x+1 > 64<<10 && y-x+1 > 100<<10 {
				big.Add(1)
			}
		}
		http.ServeFile(w, r, filepath.Join(dir, strings.TrimPrefix(r.URL.Path, "/")))
	}))
	defer srv.Close()
	hdr := map[string]string{"X-Test": "yes"}
	c := &Client{ChunkSize: 100 << 10, Parallel: 3}
	outV, outA := filepath.Join(t.TempDir(), "v.mp4"), filepath.Join(t.TempDir(), "a.m4a")
	var reports []Progress
	starts, err := c.Fetch(context.Background(), []Part{{URL: srv.URL + "/v.mp4", Headers: hdr}, {URL: srv.URL + "/a.m4a", Headers: hdr}},
		5.3, 8.1, []string{outV, outA}, func(p Progress) { reports = append(reports, p) })
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(starts[0]-4) > 0.01 || starts[1] > 5.3 || starts[1] < 3.9 {
		t.Fatalf("starts %v", starts)
	}
	last := reports[len(reports)-1]
	if last.Done != last.Total || last.Total <= 0 {
		t.Fatalf("progress %+v", last)
	}
	full, _ := os.Stat(video)
	part, _ := os.Stat(outV)
	if part.Size() > full.Size()/2 {
		t.Fatalf("fetched %d of %d bytes: should be only the section", part.Size(), full.Size())
	}
	if big.Load() > 0 {
		t.Fatalf("%d requests larger than the chunk size", big.Load())
	}
	// 받은 파일은 원래 타임라인 시각(tfdt)을 갖고 끝까지 디코드된다.
	ffprobe, _ := exec.LookPath("ffprobe")
	out, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "stream=start_time:format=duration", "-of", "csv=p=0", outV).Output()
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(strings.ReplaceAll(string(out), ",", " "))
	st, _ := strconv.ParseFloat(fields[0], 64)
	if math.Abs(st-4) > 0.05 {
		t.Fatalf("video start_time %v, want 4 (source time of the first fragment): %s", st, out)
	}
	var stderr bytes.Buffer
	cmd := exec.Command("ffmpeg", "-v", "error", "-i", outV, "-f", "null", "-")
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil || stderr.Len() > 0 {
		t.Fatalf("decode: %v %s", err, stderr.String())
	}
	t.Logf("requests %d, fetched %d bytes (video %d of %d)", requests.Load(), last.Total, part.Size(), full.Size())
}

func TestFetchExpiredURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "expired", http.StatusForbidden)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := (&Client{}).Fetch(ctx, []Part{{URL: srv.URL + "/x"}}, 0, 1, []string{filepath.Join(t.TempDir(), "x")}, nil)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("want ErrExpired, got %v", err)
	}
}
