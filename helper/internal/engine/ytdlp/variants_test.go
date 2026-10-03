package ytdlp

import (
	"os"
	"testing"

	"birdcage/helper/internal/engine"
)

// testdata/jNQXAC9IVRw.info.json: 실제 -J 출력(2026-10-03, yt-dlp 2026.08.19)에서 URL·대용량 필드만 뺀 것.
func TestParseInfoVariantsFromRealProbe(t *testing.T) {
	b, err := os.ReadFile("testdata/jNQXAC9IVRw.info.json")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParseInfo(b)
	if err != nil {
		t.Fatal(err)
	}
	// 240p: H.264 DASH(133, tbr 183) > H.264 DASH(134, tbr 136) > HLS(229/230) > AV1·VP9. 오디오는 AAC(140, DRC 아님).
	want := []struct {
		id   string
		res  int
		size int64
	}{{"133+140", 240, 433081 + 309288}, {"160+140", 144, 195278 + 309288}}
	if len(info.Variants) != len(want) {
		t.Fatalf("variants = %+v", info.Variants)
	}
	for i, w := range want {
		v := info.Variants[i]
		if v.FormatID != w.id || v.Res != w.res || v.SizeBytes != w.size || engine.CodecFamily(v.VCodec) != "h264" || len(v.Parts) != 2 {
			t.Errorf("variant %d = %+v, want %+v", i, v, w)
		}
	}
	// 오디오만: 품질(abr) 기준, DRC 제외 → 140(129.8k)이 251(106k)보다 높다.
	if info.BestAudio == nil || info.BestAudio.ID != "140" {
		t.Errorf("best audio = %+v", info.BestAudio)
	}
	// 16:9이고 192px 이상인 가장 작은 썸네일(196×110).
	if info.Thumbnail == "" || info.Thumbnail == "https://i.ytimg.com/vi/jNQXAC9IVRw/hqdefault.jpg" {
		t.Errorf("thumbnail = %q", info.Thumbnail)
	}
	var got []string
	for _, s := range info.Subtitles {
		l := s.Lang
		if s.Auto {
			l += "(auto)"
		}
		got = append(got, l)
	}
	if want := "de en de(auto) en(auto)"; joinSpace(got) != want {
		t.Errorf("subtitles = %q, want %q", joinSpace(got), want)
	}
}

func joinSpace(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}

func TestVariantsVerticalMuxedAndTranslatedCaptions(t *testing.T) {
	// TikTok형: 세로 1080×1920 muxed(H.264·bytevc1) + 해상도 짧은 변 기준. 자동 자막은 원어(-orig)만.
	b := []byte(`{"id":"7","title":"t","extractor_key":"TikTok","webpage_url":"https://www.tiktok.com/@a/video/7","language":"",
	 "formats":[
	  {"format_id":"h265_1080","vcodec":"bytevc1","acodec":"aac","width":1080,"height":1920,"fps":30,"filesize":900,"protocol":"https"},
	  {"format_id":"h264_1080","vcodec":"h264","acodec":"aac","width":1080,"height":1920,"fps":30,"filesize":1000,"protocol":"https"},
	  {"format_id":"h264_720","vcodec":"h264","acodec":"aac","width":720,"height":1280,"fps":30,"protocol":"https"}],
	 "automatic_captions":{"en-orig":[{"ext":"vtt","name":"English (Original)"}],"en":[{}],"fr":[{}],"de":[{}],"es":[{}],"it":[{}],"ja":[{}]}}`)
	info, err := ParseInfo(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Variants) != 2 || info.Variants[0].FormatID != "h264_1080" || info.Variants[0].Res != 1080 ||
		info.Variants[0].SizeBytes != 1000 || len(info.Variants[0].Parts) != 1 {
		t.Fatalf("variants = %+v", info.Variants)
	}
	if v := info.Variants[1]; v.FormatID != "h264_720" || v.SizeBytes != 0 {
		t.Errorf("unknown size must stay 0: %+v", v)
	}
	if len(info.Subtitles) != 1 || info.Subtitles[0].Lang != "en-orig" || !info.Subtitles[0].Auto {
		t.Errorf("subtitles = %+v", info.Subtitles)
	}
	if info.BestAudio != nil {
		t.Errorf("no audio-only format, got %+v", info.BestAudio)
	}
}

func TestCodecFamilyAndBitDepth(t *testing.T) {
	for tag, want := range map[string]string{"avc1.640028": "h264", "vp09.00.40.08": "vp9", "vp9": "vp9", "av01.0.08M.08": "av1",
		"hev1.1.6.L120.90": "hevc", "bytevc1": "hevc", "mp4a.40.2": "aac", "opus": "opus"} {
		if got := engine.CodecFamily(tag); got != want {
			t.Errorf("CodecFamily(%q) = %q, want %q", tag, got, want)
		}
	}
	for tag, want := range map[string]bool{"vp09.02.51.10.01.09.16.09.00": true, "vp09.00.40.08": false, "av01.0.12M.10.0.110.09.16.09.0": true,
		"av01.0.08M.08": false, "hvc1.2.4.L153.B0": true, "hev1.1.6.L120.90": false, "avc1.640028": false} {
		if got := engine.TenBit(tag); got != want {
			t.Errorf("TenBit(%q) = %v, want %v", tag, got, want)
		}
	}
}

func TestRequestArgsPutsVariantFirst(t *testing.T) {
	e := &Engine{YtDlp: "yt-dlp"}
	req := engine.DownloadRequest{Info: &engine.Info{InfoPath: "/w/info.json"}, WorkDir: "/w", MaxHeight: 720, FormatID: "137+140"}
	args := e.RequestArgs(req)
	for i, a := range args {
		if a == "-f" {
			if args[i+1] != "137+140/bv*+ba/b" {
				t.Fatalf("-f %q", args[i+1])
			}
			return
		}
	}
	t.Fatal("no -f")
}

func TestVariantNeedsTranscode(t *testing.T) {
	for _, c := range []struct {
		vcodec string
		hevcOK bool
		want   bool
	}{{"avc1.640028", false, false}, {"vp09.00.40.08", true, true}, {"hev1.1.6.L120.90", false, true},
		{"hev1.1.6.L120.90", true, false}, {"hvc1.2.4.L153.B0", true, true}, {"av01.0.08M.08", true, true}} {
		if got := (engine.Variant{VCodec: c.vcodec}).NeedsTranscode(c.hevcOK); got != c.want {
			t.Errorf("%s hevcOK=%v: %v, want %v", c.vcodec, c.hevcOK, got, c.want)
		}
	}
}

// testdata/aqz-KE-bpKQ.info.json: 실제 -J(2026-10-03), 10분 35초. 스토리보드 sb3·sb2·sb1·sb0.
func TestStoryboardsFromRealProbe(t *testing.T) {
	b, err := os.ReadFile("testdata/aqz-KE-bpKQ.info.json")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParseInfo(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Storyboards) != 4 {
		t.Fatalf("storyboards %d", len(info.Storyboards))
	}
	sb2 := info.Storyboards[1] // 80×45, 10×10 타일, 약 5s 간격, 2장
	if sb2.Width != 80 || sb2.Height != 45 || sb2.Cols != 10 || sb2.Rows != 10 || len(sb2.Sprites) != 2 {
		t.Fatalf("sb2 %+v", sb2)
	}
	if sb2.IntervalSec < 4.9 || sb2.IntervalSec > 5.0 {
		t.Fatalf("interval %.3f", sb2.IntervalSec)
	}
	last := sb2.Sprites[len(sb2.Sprites)-1]
	if end := last.StartSec + last.DurationSec; end < 634 || end > 636 {
		t.Fatalf("sprites cover %.1fs, want the 635s duration", end)
	}
	if info.Storyboards[3].Width != 320 {
		t.Fatalf("largest last: %+v", info.Storyboards[3])
	}
	for _, v := range info.Variants {
		if v.Res == 0 || v.VCodec == "" || v.FormatID == "sb0" {
			t.Fatalf("storyboard leaked into variants: %+v", v)
		}
	}
	// 짧은 영상(19s)에는 스토리보드가 없다.
	zoo, _ := os.ReadFile("testdata/jNQXAC9IVRw.info.json")
	if zi, _ := ParseInfo(zoo); len(zi.Storyboards) != 0 {
		t.Fatalf("zoo storyboards %+v", zi.Storyboards)
	}
}
