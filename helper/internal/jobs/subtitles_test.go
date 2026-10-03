package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/media"
)

// subEngine is variantEngine plus a scripted subtitle fetch.
type subEngine struct {
	variantEngine
	srt  string
	fail error
}

func (f *subEngine) Subtitles(ctx context.Context, info *engine.Info, lang string, auto bool, workDir string) (string, error) {
	if f.fail != nil {
		return "", f.fail
	}
	_ = os.MkdirAll(workDir, 0o755)
	p := filepath.Join(workDir, "subs."+lang+".srt")
	return p, os.WriteFile(p, []byte(f.srt), 0o644)
}

func TestSubtitlesSidecarAndIsolation(t *testing.T) {
	src, ffmpeg, ffprobe := fixture(t)
	const srt = "1\n00:00:00,500 --> 00:00:01,000\nin range\n\n2\n00:00:05,000 --> 00:00:06,000\nafter the end\n\n"
	ok := &subEngine{variantEngine: variantEngine{fakeEngine: fakeEngine{fixture: src}}, srt: srt}
	m, _ := newManager(t, ok, ffmpeg, ffprobe, Config{})
	events, unsub := m.Subscribe()
	defer unsub()
	id, _ := m.Submit("https://youtu.be/x", Options{MaxHeight: 720, Profile: media.ProfileCompat, Subtitles: &SubtitleRequest{Lang: "en"}})
	snap, _ := waitTerminal(t, events, id)
	if snap.State != Done || snap.SubtitleFile == "" {
		t.Fatalf("state %s subtitle %q warnings %v", snap.State, snap.SubtitleFile, snap.Warnings)
	}
	if !strings.HasSuffix(snap.SubtitleFile, ".en.srt") || filepath.Dir(snap.SubtitleFile) != filepath.Dir(snap.File) {
		t.Fatalf("sidecar %q next to %q", snap.SubtitleFile, snap.File)
	}
	b, _ := os.ReadFile(snap.SubtitleFile)
	if got := string(b); got != "1\n00:00:00,500 --> 00:00:01,000\nin range\n\n" { // 2s 파일 밖의 큐는 버린다
		t.Fatalf("sidecar content %q", got)
	}

	// 자막 실패는 작업 실패가 아니다(경고만).
	bad := &subEngine{variantEngine: variantEngine{fakeEngine: fakeEngine{fixture: src}}, fail: engine.Fail(engine.KindRateLimited, "HTTP Error 429")}
	m2, _ := newManager(t, bad, ffmpeg, ffprobe, Config{})
	ev2, unsub2 := m2.Subscribe()
	defer unsub2()
	id, _ = m2.Submit("https://youtu.be/x", Options{MaxHeight: 720, Profile: media.ProfileCompat, Subtitles: &SubtitleRequest{Lang: "en", Auto: true}})
	snap, _ = waitTerminal(t, ev2, id)
	if snap.State != Done || snap.SubtitleFile != "" || !hasWarning(snap, "자막을 받지 못했습니다") {
		t.Fatalf("subtitle failure must only warn: state %s file %q warnings %v", snap.State, snap.SubtitleFile, snap.Warnings)
	}

	// 자막을 못 받는 엔진도 경고만.
	plain := &variantEngine{fakeEngine: fakeEngine{fixture: src}}
	m3, _ := newManager(t, plain, ffmpeg, ffprobe, Config{})
	ev3, unsub3 := m3.Subscribe()
	defer unsub3()
	id, _ = m3.Submit("https://youtu.be/x", Options{MaxHeight: 720, Profile: media.ProfileCompat, Subtitles: &SubtitleRequest{Lang: "en"}})
	if snap, _ = waitTerminal(t, ev3, id); snap.State != Done || !hasWarning(snap, "자막을 지원하지 않습니다") {
		t.Fatalf("engine without subtitles: state %s warnings %v", snap.State, snap.Warnings)
	}
}

func hasWarning(s Snapshot, sub string) bool {
	for _, w := range s.Warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
