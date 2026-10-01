package protocol

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/jobs"
	"birdcage/helper/internal/media"
	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/tools"
)

func TestMain(m *testing.M) { perftest.Main(m) }

// go test ./internal/protocol -update 로 /protocol/fixtures 를 다시 만든다.
var update = flag.Bool("update", false, "rewrite golden fixtures")

var fixedTime = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func sampleJob() jobs.Snapshot {
	return jobs.Snapshot{
		JobID: "j0a1b2c3d4e5f", URL: "https://www.youtube.com/watch?v=BaW_jenozKc", State: jobs.Done,
		Options:  jobs.Options{MaxHeight: 1080, Profile: media.ProfileCompat},
		Progress: 1,
		Download: &engine.Progress{Status: "finished", DownloadedBytes: 2160000, TotalBytes: 2160000, SpeedBps: 1.5e6, Fraction: 1, FormatID: "140"},
		Info: &engine.Info{ID: "BaW_jenozKc", Title: "youtube-dl test video", Uploader: "Philipp Hagemeister", Extractor: "Youtube",
			WebpageURL: "https://www.youtube.com/watch?v=BaW_jenozKc", DurationSec: 10,
			Selected: []engine.Format{{ID: "137", Ext: "mp4", VCodec: "avc1.640028", Width: 1920, Height: 1080, FPS: 30, SizeBytes: 2000000},
				{ID: "140", Ext: "m4a", ACodec: "mp4a.40.2", SizeBytes: 160000}}},
		Media: &media.Info{Container: "mov,mp4,m4a,3gp,3g2,mj2", DurationSec: 10, SizeBytes: 2150000,
			Video: &media.Video{Codec: "h264", Profile: "High", PixFmt: "yuv420p", Width: 1920, Height: 1080, FPS: 30},
			Audio: &media.Audio{Codec: "aac", Profile: "LC", SampleRate: 44100, Channels: 2}},
		Plan:      &media.Plan{Profile: media.ProfileCompat, Video: media.Copy, Audio: media.Copy, Passthrough: true, Reasons: []string{"already Premiere-ready"}},
		File:      "/Users/me/Movies/Birdcage/Youtube/youtube-dl test video [BaW_jenozKc].mp4",
		Timings:   []jobs.StageTiming{{Stage: "probe", MS: 1830.5}, {Stage: "download", MS: 2210.2}, {Stage: "analyze", MS: 61.3}, {Stage: "normalize", MS: 3.1}},
		CreatedAt: fixedTime, UpdatedAt: fixedTime.Add(5 * time.Second),
	}
}

func fixtures() map[string]struct {
	typ, id string
	data    any
} {
	failed := sampleJob()
	failed.State = jobs.Failed
	failed.File, failed.Media, failed.Plan, failed.Timings = "", nil, nil, nil
	failed.Failure = engine.Fail(engine.KindPrivate, "ERROR: [youtube] x: Private video")
	return map[string]struct {
		typ, id string
		data    any
	}{
		"hello": {TypeHello, "1", Hello{Client: "birdcage-cep", ClientVersion: "0.1.0", Host: Host{App: "PPRO", Version: "15.2.0", API: "cep", OS: "mac"}}},
		"hello.ok": {TypeHelloOK, "1", HelloOK{HelperVersion: "0.1.0", Protocol: Version, Profile: "compat", LibraryDir: "/Users/me/Movies/Birdcage",
			Tools: []tools.Status{{Tool: tools.Tool{Name: tools.YtDlp, Path: "/x/yt-dlp_macos", Version: "2026.08.19", Source: "installed"}},
				{Tool: tools.Tool{Name: tools.Deno}, Error: "tool not found: deno"}},
			Jobs: []jobs.Snapshot{}}},
		"job.create":   {TypeJobCreate, "2", JobCreate{URL: "https://youtu.be/BaW_jenozKc", MaxHeight: 1080}},
		"job.accepted": {TypeJobAccepted, "2", JobAccepted{JobID: "j0a1b2c3d4e5f"}},
		"job.cancel":   {TypeJobCancel, "3", JobCancel{JobID: "j0a1b2c3d4e5f"}},
		"job.done":     {TypeJobDone, "", JobEvent{Job: sampleJob()}},
		"job.failed":   {TypeJobFailed, "", JobEvent{Job: failed}},
		"tools.status": {TypeToolsStatus, "4", ToolsStatus{Installing: false, Tools: []tools.Status{{Tool: tools.Tool{Name: tools.YtDlp, Path: "/x", Version: "2026.08.19", Source: "installed"}}}}},
		"error":        {TypeError, "5", Error{Code: "bad_request", Message: "invalid url"}},
	}
}

func fixtureDir() string { return filepath.Join("..", "..", "..", "protocol", "fixtures") }

func TestFixtures(t *testing.T) {
	for name, f := range fixtures() {
		b, err := Encode(f.typ, f.id, f.data)
		if err != nil {
			t.Fatal(err)
		}
		var pretty bytes.Buffer
		_ = json.Indent(&pretty, b, "", "  ")
		pretty.WriteByte('\n')
		path := filepath.Join(fixtureDir(), name+".json")
		if *update {
			if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run with -update)", name, err)
		}
		if !bytes.Equal(want, pretty.Bytes()) {
			t.Errorf("%s: fixture differs from Go encoding — the protocol changed; run `go test ./internal/protocol -update` and update panel/core types", name)
		}
		env, err := Decode(want)
		if err != nil || env.Type != f.typ {
			t.Errorf("%s: decode %v type %q", name, err, env.Type)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	for _, in := range []string{`{"v":1,"type":"hello"}`, `{"v":0}`, `not json`} {
		if _, err := Decode([]byte(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
}

func BenchmarkEncodeJobUpdate(b *testing.B) {
	j := sampleJob()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = Encode(TypeJobUpdate, "", JobEvent{Job: j})
	}
}
