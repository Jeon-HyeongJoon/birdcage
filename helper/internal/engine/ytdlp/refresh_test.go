//go:build !windows

package ytdlp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"birdcage/helper/internal/engine"
)

func TestDownloadRefreshesExpiredMediaURLOnce(t *testing.T) {
	workDir := t.TempDir()
	tool := filepath.Join(workDir, "fake-yt-dlp")
	calls := filepath.Join(workDir, "calls.txt")
	media := filepath.Join(workDir, "media.mp4")
	stalePart := filepath.Join(workDir, "media.mp4.part")
	fileList := filepath.Join(workDir, "filepath.txt")
	infoPath := filepath.Join(workDir, "info.json")
	infoJSON := `{"id":"clip","title":"Clip","extractor_key":"Youtube","webpage_url":"https://www.youtube.com/watch?v=clip","duration":10,"format_id":"18","ext":"mp4","vcodec":"avc1.42001E","acodec":"mp4a.40.2","width":640,"height":360,"formats":[{"format_id":"18","ext":"mp4","vcodec":"avc1.42001E","acodec":"mp4a.40.2","width":640,"height":360,"protocol":"https"}]}`
	script := fmt.Sprintf(`#!/bin/sh
set -eu
is_probe=0
for arg in "$@"; do
  if [ "$arg" = "-J" ]; then is_probe=1; fi
done
if [ "$is_probe" -eq 1 ]; then
  printf 'probe\n' >> %s
  printf '%%s\n' %s
  exit 0
fi
printf 'download\n' >> %s
count=$(grep -c '^download$' %s)
if [ "$count" -eq 1 ]; then
  : > %s
  printf 'ERROR: unable to download video data: HTTP Error 403: Forbidden\n' >&2
  exit 1
fi
printf 'ok' > %s
printf '%%s\n' %s > %s
`, shellQuote(calls), shellQuote(infoJSON), shellQuote(calls), shellQuote(calls), shellQuote(stalePart), shellQuote(media), shellQuote(media), shellQuote(fileList))
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(infoPath, []byte(infoJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	e := &Engine{YtDlp: tool}
	result, err := e.Download(context.Background(), engine.DownloadRequest{
		Info: &engine.Info{
			ID: "clip", Title: "Clip", Extractor: "Youtube", WebpageURL: "https://www.youtube.com/watch?v=clip",
			InfoPath: infoPath, Selected: []engine.Format{{ID: "18"}},
		},
		FormatID: "18",
		WorkDir:  workDir,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != media || result.Bytes != 2 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(stalePart); !os.IsNotExist(err) {
		t.Fatalf("stale partial file was not removed: %v", err)
	}
	b, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "download\nprobe\ndownload\n"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestExpiredMediaURLIsNetworkFailure(t *testing.T) {
	failure := Classify([]string{"ERROR: unable to download video data: HTTP Error 403: Forbidden"})
	if failure.Kind != engine.KindNetwork || failure.Policy != engine.PolicyRetry {
		t.Fatalf("failure = %+v", failure)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
