package media

import (
	"slices"
	"strings"
	"testing"

	"birdcage/helper/internal/perf/perftest"
)

func TestMain(m *testing.M) { perftest.Main(m) }

func TestRationalAndSnap(t *testing.T) {
	if r := ParseRational("30000/1001"); r.Num != 30000 || r.Den != 1001 {
		t.Fatalf("parse %+v", r)
	}
	if r := ParseRational("0/0"); r.Float() != 0 {
		t.Fatal("0/0 must be 0")
	}
	for _, c := range []struct {
		in   float64
		want string
	}{{29.97, "30000/1001"}, {29.5, "30000/1001"}, {30.2, "30/1"}, {59.94, "60000/1001"}, {23.976, "24000/1001"}, {25.1, "25/1"}, {14.98, "14980/1000"}} {
		if got := SnapFPS(c.in).String(); got != c.want {
			t.Errorf("SnapFPS(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}

const probeJSON = `{"streams":[
 {"index":0,"codec_type":"video","codec_name":"h264","profile":"High","pix_fmt":"yuv420p","width":1920,"height":1080,
  "r_frame_rate":"30/1","avg_frame_rate":"30/1","color_transfer":"bt709","bit_rate":"8000000"},
 {"index":1,"codec_type":"audio","codec_name":"aac","profile":"LC","sample_rate":"44100","channels":2},
 {"index":2,"codec_type":"video","codec_name":"mjpeg","disposition":{"attached_pic":1}}],
 "format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"10.5","size":"1234567"}}`

func TestParseProbe(t *testing.T) {
	info, err := ParseProbe([]byte(probeJSON))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsMP4() || info.DurationSec != 10.5 || info.SizeBytes != 1234567 {
		t.Fatalf("format %+v", info)
	}
	if info.Video.Codec != "h264" || info.Video.FPS != 30 || info.Video.BitDepth() != 8 || info.Video.HDR() {
		t.Fatalf("video %+v", info.Video)
	}
	if info.Audio.SampleRate != 44100 || info.Audio.Channels != 2 {
		t.Fatalf("audio %+v", info.Audio)
	}
}

func times(deltas ...float64) []float64 {
	ts := []float64{0}
	for _, d := range deltas {
		ts = append(ts, ts[len(ts)-1]+d)
	}
	return ts
}

func repeat(d float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = d
	}
	return out
}

func TestVFRDetection(t *testing.T) {
	v := &Video{RFrameRate: Rational{30, 1}, AvgFrameRate: Rational{30, 1}}
	cfr := VFRFromTimestamps(times(repeat(1.0/30, 300)...), v)
	if cfr.Variable || cfr.IrregularPct != 0 {
		t.Fatalf("cfr flagged: %+v", cfr)
	}
	// 프레임 하나 빠진 정도(0.3%)는 CFR로 본다.
	drop := append(repeat(1.0/30, 299), 2.0/30)
	if r := VFRFromTimestamps(times(drop...), v); r.Variable {
		t.Fatalf("single drop flagged: %+v", r)
	}
	mixed := append(repeat(1.0/30, 150), repeat(1.0/20, 150)...)
	if r := VFRFromTimestamps(times(mixed...), v); !r.Variable || r.IrregularPct < 40 {
		t.Fatalf("vfr missed: %+v", r)
	}
	mismatch := &Video{RFrameRate: Rational{30, 1}, AvgFrameRate: Rational{2900, 100}}
	if r := VFRFromTimestamps(times(repeat(1.0/30, 300)...), mismatch); !r.Variable || !r.RAvgMismatch {
		t.Fatalf("r/avg mismatch missed: %+v", r)
	}
}

func info(vcodec, pix, acodec string, rate int, container string) *Info {
	i := &Info{Container: container, DurationSec: 10}
	if vcodec != "" {
		i.Video = &Video{Codec: vcodec, PixFmt: pix, Width: 1920, Height: 1080, FPS: 30, RFrameRate: Rational{30, 1}, AvgFrameRate: Rational{30, 1}}
	}
	if acodec != "" {
		i.Audio = &Audio{Codec: acodec, SampleRate: rate, Channels: 2}
	}
	return i
}

func TestMakePlan(t *testing.T) {
	mp4 := "mov,mp4,m4a,3gp,3g2,mj2"
	cases := []struct {
		name        string
		in          *Info
		vfr         bool
		profile     Profile
		video       Action
		audio       Action
		passthrough bool
	}{
		{"ready h264/aac mp4", info("h264", "yuv420p", "aac", 44100, mp4), false, ProfileCompat, Copy, Copy, true},
		{"h264/aac in mkv → remux", info("h264", "yuv420p", "aac", 48000, "matroska,webm"), false, ProfileCompat, Copy, Copy, false},
		{"vp9/opus", info("vp9", "yuv420p", "opus", 48000, "matroska,webm"), false, ProfileCompat, Transcode, Transcode, false},
		{"av1", info("av1", "yuv420p", "aac", 48000, mp4), false, ProfileCompat, Transcode, Copy, false},
		{"10-bit h264", info("h264", "yuv420p10le", "aac", 48000, mp4), false, ProfileCompat, Transcode, Copy, false},
		{"hevc compat", info("hevc", "yuv420p", "aac", 48000, mp4), false, ProfileCompat, Transcode, Copy, false},
		{"hevc modern", info("hevc", "yuv420p", "aac", 48000, mp4), false, ProfileModern, Copy, Copy, true},
		{"vfr h264", info("h264", "yuv420p", "aac", 48000, mp4), true, ProfileCompat, Transcode, Copy, false},
		{"aac 32k", info("h264", "yuv420p", "aac", 32000, mp4), false, ProfileCompat, Copy, Transcode, false},
		{"audio only mp3", info("", "", "mp3", 44100, "mp3"), false, ProfileCompat, Drop, Transcode, false},
	}
	for _, c := range cases {
		p := MakePlan(c.in, VFRReport{Variable: c.vfr, MedianDelta: 1.0 / 30}, c.profile, "libx264")
		if p.Video != c.video || p.Audio != c.audio || p.Passthrough != c.passthrough {
			t.Errorf("%s: got video=%s audio=%s pass=%v (%v)", c.name, p.Video, p.Audio, p.Passthrough, p.Reasons)
		}
	}
}

func TestPlanArgs(t *testing.T) {
	in := info("vp9", "yuv420p", "opus", 48000, "matroska,webm")
	p := MakePlan(in, VFRReport{}, ProfileCompat, "h264_videotoolbox")
	args := strings.Join(p.Args("in.webm", "out.tmp", in), " ")
	for _, want := range []string{"-c:v h264_videotoolbox", "-fps_mode cfr -r 30/1", "-g 60", "-pix_fmt yuv420p", "-tag:v avc1",
		"-c:a aac", "-ar 48000", "-movflags +faststart", "-f mp4 out.tmp", "-progress pipe:1"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q:\n%s", want, args)
		}
	}
	if p.BitrateBps != TargetBitrate(1920, 1080, 30) || p.BitrateBps < 8e6 || p.BitrateBps > 9e6 {
		t.Errorf("bitrate %d", p.BitrateBps)
	}
	remux := MakePlan(info("h264", "yuv420p", "aac", 48000, "matroska,webm"), VFRReport{}, ProfileCompat, "")
	ra := remux.Args("a.mkv", "b", info("h264", "yuv420p", "aac", 48000, "matroska,webm"))
	if !slices.Contains(ra, "copy") || slices.Contains(ra, "-fps_mode") {
		t.Errorf("remux args %v", ra)
	}
}

func BenchmarkVFRFromTimestamps(b *testing.B) {
	ts := times(append(repeat(1.0/30, 450), repeat(1.0/20, 450)...)...) // 30초 분량
	v := &Video{RFrameRate: Rational{30, 1}, AvgFrameRate: Rational{30, 1}}
	b.ReportAllocs()
	for b.Loop() {
		in := append([]float64(nil), ts...)
		_ = VFRFromTimestamps(in, v)
	}
}
