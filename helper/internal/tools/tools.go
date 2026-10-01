// Package tools locates and installs the external binaries the helper drives:
// yt-dlp, ffmpeg/ffprobe and a JavaScript runtime (deno or node) for YouTube.
//
// 탐색 우선순위: 명시 지정(플래그/환경변수) → Helper가 설치한 버전(<data>/tools) → 번들(<exe>/tools) → PATH.
package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"birdcage/helper/internal/proc"
)

// Name identifies a tool.
type Name string

const (
	YtDlp   Name = "yt-dlp"
	FFmpeg  Name = "ffmpeg"
	FFprobe Name = "ffprobe"
	Deno    Name = "deno"
	Node    Name = "node"
)

// All lists every tool the helper knows about.
var All = []Name{YtDlp, FFmpeg, FFprobe, Deno, Node}

// Tool is a located executable.
type Tool struct {
	Name    Name   `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Source  string `json:"source"` // override | installed | bundled | path
}

// ErrNotFound means the tool is not available anywhere.
var ErrNotFound = errors.New("tool not found")

// Locator finds tools and caches the result.
type Locator struct {
	Root       string          // <data>/tools
	BundledDir string          // <helper exe dir>/tools, may be empty
	Overrides  map[Name]string // explicit paths
	LookPath   func(string) (string, error)

	mu    sync.Mutex
	cache map[Name]Tool
}

// NewLocator returns a locator using exec.LookPath for PATH lookups.
func NewLocator(root, bundled string, overrides map[Name]string) *Locator {
	return &Locator{Root: root, BundledDir: bundled, Overrides: overrides, LookPath: exec.LookPath}
}

// Find returns the tool, probing its version on first use.
func (l *Locator) Find(ctx context.Context, n Name) (Tool, error) {
	l.mu.Lock()
	if t, ok := l.cache[n]; ok {
		l.mu.Unlock()
		return t, nil
	}
	l.mu.Unlock()

	t, err := l.find(ctx, n)
	if err != nil {
		return Tool{Name: n}, err
	}
	l.mu.Lock()
	if l.cache == nil {
		l.cache = map[Name]Tool{}
	}
	l.cache[n] = t
	l.mu.Unlock()
	return t, nil
}

// Invalidate drops the cached entry (after install/update).
func (l *Locator) Invalidate(n Name) {
	l.mu.Lock()
	delete(l.cache, n)
	l.mu.Unlock()
}

func (l *Locator) find(ctx context.Context, n Name) (Tool, error) {
	type cand struct{ path, source string }
	var cands []cand
	if p := l.Overrides[n]; p != "" {
		cands = append(cands, cand{p, "override"})
	}
	if l.Root != "" {
		if p, ok := installedPath(l.Root, n); ok {
			cands = append(cands, cand{p, "installed"})
		}
	}
	if l.BundledDir != "" {
		cands = append(cands, cand{filepath.Join(l.BundledDir, string(n), exeFor(n)), "bundled"})
	}
	if l.LookPath != nil {
		if p, err := l.LookPath(string(n)); err == nil {
			cands = append(cands, cand{p, "path"})
		}
	}
	var lastErr error
	for _, c := range cands {
		if st, err := os.Stat(c.path); err != nil || st.IsDir() {
			continue
		}
		v, err := probeVersion(ctx, n, c.path)
		if err != nil {
			lastErr = err
			continue
		}
		return Tool{Name: n, Path: c.path, Version: v, Source: c.source}, nil
	}
	if lastErr != nil {
		return Tool{}, fmt.Errorf("%w: %s: %v", ErrNotFound, n, lastErr)
	}
	return Tool{}, fmt.Errorf("%w: %s", ErrNotFound, n)
}

// installedPath reads <root>/<name>/current and returns the executable path.
func installedPath(root string, n Name) (string, bool) {
	b, err := os.ReadFile(filepath.Join(root, string(n), "current"))
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", false
	}
	return filepath.Join(root, string(n), v, exeFor(n)), true
}

// exeFor is the executable's file name inside an installed/bundled tool directory.
func exeFor(n Name) string {
	switch n {
	case YtDlp:
		switch runtime.GOOS {
		case "darwin":
			return "yt-dlp_macos"
		case "windows":
			return "yt-dlp.exe"
		default:
			return "yt-dlp_linux"
		}
	default:
		if runtime.GOOS == "windows" {
			return string(n) + ".exe"
		}
		return string(n)
	}
}

var versionRe = regexp.MustCompile(`\d+(?:\.\d+)+`)

// probeVersion runs the tool's version command. yt-dlp의 첫 실행은 OS 보안 검사로
// 수 초가 걸릴 수 있어 넉넉한 타임아웃을 둔다.
func probeVersion(ctx context.Context, n Name, path string) (string, error) {
	arg := "--version"
	if n == FFmpeg || n == FFprobe {
		arg = "-version"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, _, err := proc.Output(ctx, proc.Spec{Path: path, Args: []string{arg}, StderrTail: 5})
	if err != nil {
		return "", err
	}
	return ParseVersion(n, string(out))
}

// ParseVersion extracts a version number from a tool's version output.
func ParseVersion(n Name, out string) (string, error) {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0])
	if line == "" {
		return "", fmt.Errorf("%s: empty version output", n)
	}
	if n == YtDlp {
		// yt-dlp는 "2026.08.19" 또는 nightly "2026.09.27.232945" 형식.
		return line, nil
	}
	v := versionRe.FindString(line)
	if v == "" {
		return "", fmt.Errorf("%s: cannot parse version from %q", n, line)
	}
	return v, nil
}

// AtLeast reports whether dotted version v >= min (numeric compare per segment).
func AtLeast(v, min string) bool {
	a, b := strings.Split(v, "."), strings.Split(min, ".")
	for i := 0; i < max(len(a), len(b)); i++ {
		x, y := seg(a, i), seg(b, i)
		if x != y {
			return x > y
		}
	}
	return true
}

func seg(s []string, i int) int {
	if i >= len(s) {
		return 0
	}
	n, _ := strconv.Atoi(s[i])
	return n
}

// JSRuntime picks the runtime yt-dlp should use for YouTube challenges and returns
// the --js-runtimes value. 최소 버전은 yt-dlp EJS 위키 기준(deno 2.3, node 22).
func (l *Locator) JSRuntime(ctx context.Context) (string, Tool, bool) {
	if t, err := l.Find(ctx, Deno); err == nil && AtLeast(t.Version, "2.3.0") {
		return "deno:" + t.Path, t, true
	}
	if t, err := l.Find(ctx, Node); err == nil && AtLeast(t.Version, "22.0.0") {
		return "node:" + t.Path, t, true
	}
	return "", Tool{}, false
}

// Status reports every tool; missing ones carry an error string.
type Status struct {
	Tool
	Error string `json:"error,omitempty"`
}

// StatusAll locates every known tool. 버전 확인(--version 실행)을 병렬로 해서 패널 연결(hello.ok)
// 지연을 줄인다(실측: 순차 1265ms → 병렬 ~305ms, panel/core 통합 테스트 기준).
func (l *Locator) StatusAll(ctx context.Context) []Status {
	out := make([]Status, len(All))
	var wg sync.WaitGroup
	for i, n := range All {
		wg.Go(func() {
			t, err := l.Find(ctx, n)
			s := Status{Tool: t}
			s.Name = n
			if err != nil {
				s.Error = err.Error()
			}
			out[i] = s
		})
	}
	wg.Wait()
	return out
}
