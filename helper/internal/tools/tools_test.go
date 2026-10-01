package tools_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/tools"
)

func TestMain(m *testing.M) { perftest.Main(m) }

func TestParseVersion(t *testing.T) {
	cases := []struct {
		n    tools.Name
		out  string
		want string
	}{
		{tools.YtDlp, "2026.08.19\n", "2026.08.19"},
		{tools.YtDlp, "2026.09.27.232945\n", "2026.09.27.232945"},
		{tools.FFmpeg, "ffmpeg version 8.1.1 Copyright (c) 2000-2026 the FFmpeg developers\nbuilt with", "8.1.1"},
		{tools.FFprobe, "ffprobe version n7.1-12-gabc Copyright", "7.1"},
		{tools.Deno, "deno 2.9.7 (stable, release, aarch64-apple-darwin)\nv8 14", "2.9.7"},
		{tools.Node, "v24.20.0\n", "24.20.0"},
	}
	for _, c := range cases {
		got, err := tools.ParseVersion(c.n, c.out)
		if err != nil || got != c.want {
			t.Errorf("%s %q: got %q, %v; want %q", c.n, c.out, got, err, c.want)
		}
	}
}

func TestAtLeast(t *testing.T) {
	for _, c := range []struct {
		v, min string
		want   bool
	}{{"2.9.7", "2.3.0", true}, {"2.3.0", "2.3.0", true}, {"2.2.9", "2.3", false}, {"22.0.0", "22.0.0", true}, {"18.19.1", "22.0.0", false}, {"24.20.0", "22", true}} {
		if got := tools.AtLeast(c.v, c.min); got != c.want {
			t.Errorf("AtLeast(%s,%s)=%v", c.v, c.min, got)
		}
	}
}

type fakeGitHub struct {
	srv       *httptest.Server
	zipBytes  map[string][]byte // asset → bytes
	sums      string            // SHA2-256SUMS content
	tag       string
	downloads atomic.Int32
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o755)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func ytAsset() string {
	switch runtime.GOOS {
	case "darwin":
		return "yt-dlp_macos.zip"
	case "windows":
		return "yt-dlp_win.zip"
	}
	return "yt-dlp_linux.zip"
}

func ytExe() string {
	switch runtime.GOOS {
	case "darwin":
		return "yt-dlp_macos"
	case "windows":
		return "yt-dlp.exe"
	}
	return "yt-dlp_linux"
}

func newFakeGitHub(t *testing.T, tag string, zipBody []byte, corruptSum bool) *fakeGitHub {
	f := &fakeGitHub{tag: tag, zipBytes: map[string][]byte{ytAsset(): zipBody}}
	sum := sha256.Sum256(zipBody)
	hexsum := hex.EncodeToString(sum[:])
	if corruptSum {
		hexsum = strings.Repeat("0", 64)
	}
	f.sums = fmt.Sprintf("%s  %s\n%s  other-asset\n", hexsum, ytAsset(), strings.Repeat("a", 64))
	mux := http.NewServeMux()
	mux.HandleFunc("/yt-dlp/yt-dlp/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://github.com/yt-dlp/yt-dlp/releases/tag/"+f.tag)
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/yt-dlp/yt-dlp/releases/download/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if name == "SHA2-256SUMS" {
			_, _ = w.Write([]byte(f.sums))
			return
		}
		b, ok := f.zipBytes[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		f.downloads.Add(1)
		_, _ = w.Write(b)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func fakeSmoke(version string) func(context.Context, tools.Name, string) (string, error) {
	return func(_ context.Context, _ tools.Name, path string) (string, error) {
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return version, nil
	}
}

func TestInstallYtDlpVerifiesAndActivates(t *testing.T) {
	perftest.Track(t)
	z := makeZip(t, map[string]string{ytExe(): "#!/bin/sh\necho 2026.01.01\n", "_internal/lib.txt": "x"})
	gh := newFakeGitHub(t, "2026.01.01", z, false)
	root := t.TempDir()
	in := &tools.Installer{Root: root, BaseURL: gh.srv.URL, Rec: perftest.Recorder(), Smoke: fakeSmoke("2026.01.01")}

	tool, err := in.InstallYtDlp(context.Background(), tools.Stable)
	if err != nil {
		t.Fatal(err)
	}
	if tool.Version != "2026.01.01" || !strings.HasSuffix(tool.Path, filepath.Join("2026.01.01", ytExe())) {
		t.Fatalf("tool = %+v", tool)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "yt-dlp", "current")); strings.TrimSpace(string(b)) != "2026.01.01" {
		t.Fatalf("current = %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "yt-dlp", "2026.01.01", "_internal", "lib.txt")); err != nil {
		t.Fatal("onedir support files not extracted:", err)
	}

	// 같은 버전 재설치는 다운로드하지 않는다.
	if _, err := in.InstallYtDlp(context.Background(), tools.Stable); err != nil {
		t.Fatal(err)
	}
	if n := gh.downloads.Load(); n != 1 {
		t.Fatalf("downloads = %d, want 1 (cached)", n)
	}

	// 새 버전 설치 → previous 기록 → 롤백.
	gh.tag = "2026.02.02"
	in.Smoke = fakeSmoke("2026.02.02")
	if _, err := in.InstallYtDlp(context.Background(), tools.Stable); err != nil {
		t.Fatal(err)
	}
	prev, err := in.Rollback(tools.YtDlp)
	if err != nil || prev != "2026.01.01" {
		t.Fatalf("rollback = %q, %v", prev, err)
	}

	// Locator는 current가 가리키는 설치본을 찾는다.
	loc := tools.NewLocator(root, "", nil)
	loc.LookPath = nil
	if runtime.GOOS != "windows" {
		found, err := loc.Find(context.Background(), tools.YtDlp)
		if err != nil || found.Source != "installed" || found.Version != "2026.01.01" {
			t.Fatalf("locator = %+v, %v", found, err)
		}
	}
}

func TestInstallRejectsChecksumMismatch(t *testing.T) {
	z := makeZip(t, map[string]string{ytExe(): "x"})
	gh := newFakeGitHub(t, "2026.01.01", z, true)
	root := t.TempDir()
	in := &tools.Installer{Root: root, BaseURL: gh.srv.URL, Smoke: fakeSmoke("2026.01.01")}
	_, err := in.InstallYtDlp(context.Background(), tools.Stable)
	if !errors.Is(err, tools.ErrChecksum) {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	if _, err := os.Stat(filepath.Join(root, "yt-dlp", "current")); err == nil {
		t.Fatal("current must not be written on checksum failure")
	}
}

func TestInstallRejectsZipSlip(t *testing.T) {
	z := makeZip(t, map[string]string{ytExe(): "x", "../../evil": "x"})
	gh := newFakeGitHub(t, "2026.01.01", z, false)
	root := t.TempDir()
	in := &tools.Installer{Root: root, BaseURL: gh.srv.URL, Smoke: fakeSmoke("2026.01.01")}
	if _, err := in.InstallYtDlp(context.Background(), tools.Stable); err == nil || !strings.Contains(err.Error(), "unsafe zip entry") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "..", "evil")); err == nil {
		t.Fatal("zip slip wrote outside root")
	}
}

func TestLocatorPrefersOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script tool")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho 'ffmpeg version 9.9.9 test'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	loc := tools.NewLocator(t.TempDir(), "", map[tools.Name]string{tools.FFmpeg: p})
	got, err := loc.Find(context.Background(), tools.FFmpeg)
	if err != nil || got.Source != "override" || got.Version != "9.9.9" {
		t.Fatalf("got %+v, %v", got, err)
	}
}
