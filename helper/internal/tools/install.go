package tools

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"birdcage/helper/internal/perf"
)

// Channel selects the yt-dlp release stream.
type Channel string

const (
	Stable  Channel = "stable"
	Nightly Channel = "nightly"
)

// Installer downloads tools into Root with checksum verification.
//
//   - yt-dlp: 압축 해제판(onedir zip). 단일 파일판은 macOS에서 실행마다 ~9초가 걸려
//     (실측 9.19s vs 0.27s) 압축 해제판을 쓴다. 검증은 릴리스의 SHA2-256SUMS.
//   - deno: deno-<target>.zip, 검증은 <asset>.sha256sum.
//
// 설치는 <Root>/<tool>/<version>/ 에 풀고 current/previous 파일로 전환한다(롤백용).
type Installer struct {
	Root    string
	BaseURL string // default https://github.com (tests override)
	Client  *http.Client
	Rec     *perf.Recorder
	Log     *slog.Logger
	// Smoke checks the installed executable and returns its version. nil uses probeVersion.
	Smoke func(ctx context.Context, n Name, path string) (string, error)
}

func (in *Installer) base() string {
	if in.BaseURL != "" {
		return strings.TrimRight(in.BaseURL, "/")
	}
	return "https://github.com"
}

func (in *Installer) client() *http.Client {
	if in.Client != nil {
		return in.Client
	}
	return http.DefaultClient
}

func (in *Installer) log() *slog.Logger {
	if in.Log != nil {
		return in.Log
	}
	return slog.New(slog.DiscardHandler)
}

// LatestTag resolves the latest release tag of a GitHub repo (owner/name) via the
// /releases/latest redirect, avoiding the rate-limited REST API.
func (in *Installer) LatestTag(ctx context.Context, repo string) (string, error) {
	c := *in.client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, in.base()+"/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if i < 0 {
		return "", fmt.Errorf("latest release of %s: unexpected redirect %q (status %d)", repo, loc, resp.StatusCode)
	}
	return loc[i+len("/tag/"):], nil
}

// ytDlpAsset returns the onedir zip asset name for this platform.
func ytDlpAsset() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64", "darwin/amd64":
		return "yt-dlp_macos.zip", nil // universal2
	case "windows/amd64":
		return "yt-dlp_win.zip", nil
	case "windows/arm64":
		return "yt-dlp_win_arm64.zip", nil
	case "windows/386":
		return "yt-dlp_win_x86.zip", nil
	case "linux/amd64":
		return "yt-dlp_linux.zip", nil
	case "linux/arm64":
		return "yt-dlp_linux_aarch64.zip", nil
	}
	return "", fmt.Errorf("yt-dlp: unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
}

func denoTarget() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "aarch64-apple-darwin", nil
	case "darwin/amd64":
		return "x86_64-apple-darwin", nil
	case "windows/amd64":
		return "x86_64-pc-windows-msvc", nil
	case "windows/arm64":
		return "aarch64-pc-windows-msvc", nil
	case "linux/amd64":
		return "x86_64-unknown-linux-gnu", nil
	case "linux/arm64":
		return "aarch64-unknown-linux-gnu", nil
	}
	return "", fmt.Errorf("deno: unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
}

// InstallYtDlp installs the latest yt-dlp from the given channel.
func (in *Installer) InstallYtDlp(ctx context.Context, ch Channel) (Tool, error) {
	repo := "yt-dlp/yt-dlp"
	if ch == Nightly {
		repo = "yt-dlp/yt-dlp-nightly-builds"
	}
	asset, err := ytDlpAsset()
	if err != nil {
		return Tool{}, err
	}
	tag, err := in.LatestTag(ctx, repo)
	if err != nil {
		return Tool{}, err
	}
	sums := in.base() + "/" + repo + "/releases/download/" + tag + "/SHA2-256SUMS"
	return in.install(ctx, YtDlp, repo, tag, asset, func(ctx context.Context) (string, error) {
		return in.sumFromList(ctx, sums, asset)
	})
}

// InstallDeno installs the latest deno release.
func (in *Installer) InstallDeno(ctx context.Context) (Tool, error) {
	repo := "denoland/deno"
	target, err := denoTarget()
	if err != nil {
		return Tool{}, err
	}
	asset := "deno-" + target + ".zip"
	tag, err := in.LatestTag(ctx, repo)
	if err != nil {
		return Tool{}, err
	}
	sumURL := in.base() + "/" + repo + "/releases/download/" + tag + "/" + asset + ".sha256sum"
	return in.install(ctx, Deno, repo, tag, asset, func(ctx context.Context) (string, error) {
		body, err := in.get(ctx, sumURL, 1<<16)
		if err != nil {
			return "", err
		}
		f := strings.Fields(string(body))
		if len(f) == 0 {
			return "", fmt.Errorf("empty checksum file %s", sumURL)
		}
		return strings.ToLower(f[0]), nil
	})
}

// ErrChecksum reports a checksum mismatch.
var ErrChecksum = errors.New("checksum mismatch")

func (in *Installer) install(ctx context.Context, n Name, repo, tag, asset string, expected func(context.Context) (string, error)) (tool Tool, err error) {
	span := in.Rec.Start("tools.install", "tool", string(n), "tag", tag, "asset", asset)
	defer func() { span.End(err) }()

	dir := filepath.Join(in.Root, string(n))
	dest := filepath.Join(dir, tag)
	exe := filepath.Join(dest, exeFor(n))
	if st, e := os.Stat(exe); e == nil && !st.IsDir() {
		// 이미 설치된 버전이면 다운로드 없이 current만 맞춘다.
		v, e := in.smoke(ctx, n, exe)
		if e == nil {
			span.Set("cached", true)
			if err := in.activate(dir, tag); err != nil {
				return Tool{}, err
			}
			return Tool{Name: n, Path: exe, Version: v, Source: "installed"}, nil
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Tool{}, err
	}

	want, err := expected(ctx)
	if err != nil {
		return Tool{}, fmt.Errorf("%s checksum: %w", n, err)
	}

	t0 := time.Now()
	tmp, got, size, err := in.download(ctx, in.base()+"/"+repo+"/releases/download/"+tag+"/"+asset, dir)
	if err != nil {
		return Tool{}, fmt.Errorf("%s download: %w", n, err)
	}
	defer os.Remove(tmp)
	span.Set("bytes", size, "download_ms", perf.MS(time.Since(t0)))
	if got != want {
		return Tool{}, fmt.Errorf("%w: %s: got %s want %s", ErrChecksum, asset, got, want)
	}

	t1 := time.Now()
	partial := dest + ".partial"
	_ = os.RemoveAll(partial)
	if err := unzip(tmp, partial); err != nil {
		_ = os.RemoveAll(partial)
		return Tool{}, fmt.Errorf("%s extract: %w", n, err)
	}
	if err := os.Chmod(filepath.Join(partial, exeFor(n)), 0o755); err != nil {
		_ = os.RemoveAll(partial)
		return Tool{}, fmt.Errorf("%s: executable %s missing in %s: %w", n, exeFor(n), asset, err)
	}
	_ = os.RemoveAll(dest)
	if err := os.Rename(partial, dest); err != nil {
		return Tool{}, err
	}
	span.Set("extract_ms", perf.MS(time.Since(t1)))

	t2 := time.Now()
	v, err := in.smoke(ctx, n, exe)
	span.Set("smoke_ms", perf.MS(time.Since(t2)))
	if err != nil {
		return Tool{}, fmt.Errorf("%s smoke test: %w", n, err)
	}
	if err := in.activate(dir, tag); err != nil {
		return Tool{}, err
	}
	in.log().Info("tool installed", "tool", n, "tag", tag, "version", v, "bytes", size)
	return Tool{Name: n, Path: exe, Version: v, Source: "installed"}, nil
}

func (in *Installer) smoke(ctx context.Context, n Name, exe string) (string, error) {
	if in.Smoke != nil {
		return in.Smoke(ctx, n, exe)
	}
	return probeVersion(ctx, n, exe)
}

// activate points current at tag and remembers the previous version for rollback.
func (in *Installer) activate(dir, tag string) error {
	cur := filepath.Join(dir, "current")
	if b, err := os.ReadFile(cur); err == nil {
		if old := strings.TrimSpace(string(b)); old != "" && old != tag {
			if err := writeAtomic(filepath.Join(dir, "previous"), old); err != nil {
				return err
			}
		}
	}
	return writeAtomic(cur, tag)
}

// Rollback switches current back to previous (last known good).
func (in *Installer) Rollback(n Name) (string, error) {
	dir := filepath.Join(in.Root, string(n))
	b, err := os.ReadFile(filepath.Join(dir, "previous"))
	if err != nil {
		return "", fmt.Errorf("%s: no previous version: %w", n, err)
	}
	prev := strings.TrimSpace(string(b))
	if err := in.activate(dir, prev); err != nil {
		return "", err
	}
	return prev, nil
}

func writeAtomic(p, content string) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(content+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (in *Installer) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := in.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// sumFromList finds asset's hash in a "<hex>  <name>" checksum list.
func (in *Installer) sumFromList(ctx context.Context, url, asset string) (string, error) {
	body, err := in.get(ctx, url, 1<<20)
	if err != nil {
		return "", err
	}
	sc := bufio.NewScanner(strings.NewReader(string(body)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("%s not listed in %s", asset, url)
}

// download streams url to a temp file in dir while hashing it.
func (in *Installer) download(ctx context.Context, url, dir string) (tmp, sum string, size int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", 0, err
	}
	resp, err := in.client().Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", 0, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.CreateTemp(dir, "download-*.zip")
	if err != nil {
		return "", "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if err != nil {
		os.Remove(f.Name())
		return "", "", 0, err
	}
	return f.Name(), hex.EncodeToString(h.Sum(nil)), size, nil
}

// unzip extracts src into dest, rejecting entries that escape dest.
func unzip(src, dest string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, f := range zr.File {
		name := path.Clean(strings.ReplaceAll(f.Name, `\`, "/"))
		if name == "." || strings.HasPrefix(name, "../") || name == ".." || path.IsAbs(name) {
			return fmt.Errorf("unsafe zip entry %q", f.Name)
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		case mode&os.ModeSymlink != 0:
			// PyInstaller onedir(macOS)에는 프레임워크 심볼릭 링크가 있다. 대상이 dest 밖이면 거부.
			rc, err := f.Open()
			if err != nil {
				return err
			}
			link, err := io.ReadAll(io.LimitReader(rc, 4096))
			rc.Close()
			if err != nil {
				return err
			}
			resolved := path.Clean(path.Join(path.Dir(name), string(link)))
			if strings.HasPrefix(resolved, "../") || resolved == ".." || path.IsAbs(string(link)) {
				return fmt.Errorf("unsafe symlink %q -> %q", f.Name, link)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(string(link), target); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractFile(f, target, mode.Perm()|0o600); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, target string, perm os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
