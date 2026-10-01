package ytdlp

import (
	"slices"
	"strings"
	"testing"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/perf/perftest"
)

func TestMain(m *testing.M) { perftest.Main(m) }

func TestClassify(t *testing.T) {
	cases := []struct {
		line string
		want engine.Kind
	}{
		{"ERROR: [youtube] abc: Private video. Sign in if you've been granted access to this video", engine.KindPrivate},
		{"ERROR: [youtube] abc: Video unavailable. This video has been removed by the uploader", engine.KindRemoved},
		{"ERROR: [youtube] BaW_jenozKc: This video is unavailable", engine.KindRemoved}, // 2026-10 실측
		{"ERROR: [youtube] abc: Video unavailable. This content isn't available, try again later.", engine.KindRateLimited},
		{"ERROR: [youtube] abc: Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies", engine.KindRateLimited},
		{"ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate for some users.", engine.KindAgeRestricted},
		{"ERROR: [youtube] abc: The uploader has not made this video available in your country", engine.KindGeoBlocked},
		{"ERROR: [Instagram] C1: Requested content is not available, rate-limit reached or login required. Use --cookies", engine.KindRateLimited},
		{"ERROR: [youtube] abc: This video is DRM protected", engine.KindDRM},
		{"ERROR: Unsupported URL: https://example.com/page", engine.KindUnsupportedURL},
		{"ERROR: [youtube] abc: Requested format is not available. Use --list-formats for a list of available formats", engine.KindFormat},
		{"ERROR: [youtube] abc: This live event will begin in 3 hours.", engine.KindLive},
		{"ERROR: [TikTok] 123: Unable to extract universal data for rehydration; please report this issue on  https://github.com/yt-dlp/yt-dlp/issues", engine.KindExtractorBroken},
		{"ERROR: Unable to download webpage: <urlopen error [Errno 8] nodename nor servname provided, or not known>", engine.KindNetwork},
		{"ERROR: [youtube] abc: Join this channel to get access to members-only content like this video", engine.KindLoginRequired},
		{"ERROR: something nobody has seen before", engine.KindExtractorBroken},
	}
	for _, c := range cases {
		f := Classify([]string{"[youtube] Extracting URL", "WARNING: noise about formats", c.line})
		if f.Kind != c.want {
			t.Errorf("%q → %s, want %s", c.line, f.Kind, c.want)
		}
		if f.Detail != c.line || f.Message == "" || f.Policy == "" {
			t.Errorf("incomplete failure %+v", f)
		}
	}
}

func TestClassifyPrefersErrorLines(t *testing.T) {
	// WARNING에 rate-limit 문구가 있어도 ERROR 줄의 분류가 우선한다.
	f := Classify([]string{"WARNING: HTTP Error 429: Too Many Requests (retrying)", "ERROR: [youtube] x: Private video"})
	if f.Kind != engine.KindPrivate {
		t.Fatalf("kind = %s", f.Kind)
	}
}

func TestParseProgress(t *testing.T) {
	p, ok := ParseProgress("[birdcage] downloading|1048576|4194304|NA|524288.5|6|NA|NA|137")
	if !ok || p.Status != "downloading" || p.DownloadedBytes != 1048576 || p.TotalBytes != 4194304 ||
		p.SpeedBps != 524288.5 || p.ETASec != 6 || p.FormatID != "137" || p.Fraction != 0.25 {
		t.Fatalf("got %+v ok=%v", p, ok)
	}
	p, ok = ParseProgress("[birdcage] downloading|500|NA|1000.0|NA|NA|3|12|hls-720")
	if !ok || !p.TotalEstimated || p.TotalBytes != 1000 || p.Fraction != 0.5 || p.FragIndex != 3 || p.FragCount != 12 {
		t.Fatalf("estimated: %+v", p)
	}
	p, ok = ParseProgress("[birdcage] downloading|500|NA|NA|NA|NA|3|12|hls")
	if !ok || p.Fraction != 0.25 {
		t.Fatalf("fragment fraction: %+v", p)
	}
	if _, ok := ParseProgress("[download] Destination: x.mp4"); ok {
		t.Fatal("non-progress line parsed")
	}
	if _, ok := ParseProgress("[birdcage] too|few"); ok {
		t.Fatal("malformed line parsed")
	}
}

func TestArgs(t *testing.T) {
	e := &Engine{YtDlp: "yt-dlp", FFmpeg: "/opt/ffmpeg", JSRuntime: "deno:/x/deno", ConcurrentFragments: 4}
	f, s := FormatSelector(1080)
	if f != "bv*+ba/b" || s != "hdr:sdr,res:1080,vcodec:h264,acodec:aac" {
		t.Fatalf("selector %q %q", f, s)
	}
	pa := e.ProbeArgs("https://youtu.be/x", 1080)
	for _, want := range []string{"--ignore-config", "-J", "--js-runtimes", "deno:/x/deno", "--ffmpeg-location", "/opt/ffmpeg", "--no-playlist"} {
		if !slices.Contains(pa, want) {
			t.Errorf("probe args missing %q: %v", want, pa)
		}
	}
	if pa[len(pa)-2] != "--" || pa[len(pa)-1] != "https://youtu.be/x" {
		t.Errorf("url must follow --: %v", pa)
	}
	da := e.DownloadArgs("/w/info.json", "/w", 720)
	joined := strings.Join(da, " ")
	for _, want := range []string{"--load-info-json /w/info.json", "res:720", "--merge-output-format mp4/mkv", "--newline", "after_move:filepath", "-N 4"} {
		if !strings.Contains(joined, want) {
			t.Errorf("download args missing %q: %s", want, joined)
		}
	}
}

const sampleInfo = `{"id":"BaW_jenozKc","title":"youtube-dl test video","uploader":"Philipp Hagemeister","extractor_key":"Youtube",
"webpage_url":"https://www.youtube.com/watch?v=BaW_jenozKc","duration":10,"is_live":false,"live_status":"not_live","format_id":"137+140",
"requested_formats":[{"format_id":"137","ext":"mp4","vcodec":"avc1.640028","acodec":"none","width":1920,"height":1080,"fps":30,"filesize":2000000},
{"format_id":"140","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","filesize_approx":160000}]}`

func TestParseInfo(t *testing.T) {
	info, err := ParseInfo([]byte(sampleInfo))
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "BaW_jenozKc" || info.Extractor != "Youtube" || info.Uploader != "Philipp Hagemeister" || len(info.Selected) != 2 {
		t.Fatalf("info %+v", info)
	}
	if info.Selected[0].ACodec != "" || info.Selected[1].SizeBytes != 160000 || info.ExpectedBytes() != 2160000 {
		t.Fatalf("formats %+v", info.Selected)
	}
	if _, err := ParseInfo([]byte(`{"_type":"playlist","id":"PL1"}`)); err == nil {
		t.Fatal("playlist must be rejected")
	}
	live, _ := ParseInfo([]byte(`{"id":"x","live_status":"is_live"}`))
	if !live.IsLive {
		t.Fatal("live not detected")
	}
}

func TestAggregatorCombinesVideoAndAudio(t *testing.T) {
	info, _ := ParseInfo([]byte(sampleInfo))
	a := newAggregator(info)
	p := a.add(engine.Progress{Status: "downloading", FormatID: "137", DownloadedBytes: 1000000, TotalBytes: 2000000})
	if p.Fraction < 0.46 || p.Fraction > 0.47 { // 1.0M / 2.16M
		t.Fatalf("video half: %v", p.Fraction)
	}
	a.add(engine.Progress{Status: "finished", FormatID: "137", DownloadedBytes: 2000000, TotalBytes: 2000000})
	p = a.add(engine.Progress{Status: "downloading", FormatID: "140", DownloadedBytes: 80000, TotalBytes: 160000})
	if p.Fraction < 0.96 || p.Fraction > 0.97 {
		t.Fatalf("audio half: %v", p.Fraction)
	}
}

func BenchmarkParseProgress(b *testing.B) {
	line := "[birdcage] downloading|1048576|4194304|NA|524288.5|6|NA|NA|137"
	b.ReportAllocs()
	for b.Loop() {
		_, _ = ParseProgress(line)
	}
}

func BenchmarkClassify(b *testing.B) {
	lines := make([]string, 0, 40)
	for i := 0; i < 39; i++ {
		lines = append(lines, "[youtube] abc: Downloading webpage")
	}
	lines = append(lines, "ERROR: [youtube] abc: Video unavailable. This video has been removed by the uploader")
	b.ReportAllocs()
	for b.Loop() {
		_ = Classify(lines)
	}
}
