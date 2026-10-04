package tools_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"birdcage/helper/internal/tools"
)

// macOS 배포판: 번들 FFmpeg(Intel 정적 빌드)는 기기에 설치된 네이티브 ffmpeg가 없을 때만 쓴다.
// Windows: 번들이 PATH보다 먼저다.
func TestLocatorBundledOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script tool")
	}
	script := func(dir, version string) string {
		p := filepath.Join(dir, "ffmpeg")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\necho 'ffmpeg version "+version+" test'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	bundled := t.TempDir()
	script(filepath.Join(bundled, "ffmpeg"), "8.1.2")
	system := t.TempDir()
	script(system, "8.0.1")
	noPath := func(string) (string, error) { return "", errors.New("not on PATH") }

	for _, c := range []struct {
		last   bool
		dirs   []string
		source string
		ver    string
	}{
		{last: true, dirs: []string{system}, source: "path", ver: "8.0.1"}, // macOS: Homebrew 먼저
		{last: true, dirs: nil, source: "bundled", ver: "8.1.2"},           // macOS: 설치된 게 없으면 번들
		{last: false, dirs: []string{system}, source: "bundled", ver: "8.1.2"},
	} {
		loc := &tools.Locator{Root: t.TempDir(), BundledDir: bundled, LookPath: noPath, SystemDirs: c.dirs, BundledLast: c.last}
		got, err := loc.Find(context.Background(), tools.FFmpeg)
		if err != nil || got.Source != c.source || got.Version != c.ver {
			t.Errorf("BundledLast=%v dirs=%v: got %+v, %v; want %s %s", c.last, c.dirs, got, err, c.source, c.ver)
		}
	}
	if !tools.NewLocator("", "", nil).BundledLast == (runtime.GOOS == "darwin") {
		t.Errorf("NewLocator BundledLast should be set only on darwin")
	}
}
