// uilint rejects panel styles that break in the oldest supported host or violate the UI tokens.
//
// CEP 10(Premiere 15.x)의 렌더러는 Chromium 74다. 아래 기능은 그보다 새 Chrome에서 생겨
// 15.x에서는 조용히 무시되거나(레이아웃 붕괴) 선택자 전체가 버려진다. UXP(v2)는 CSS Grid가 없다.
// 반경은 사용자 지시(사각 카드, 라운드 최소)에 따라 토큰($radius ≤ 2px)만 허용한다.
//
//	go run ./cmd/uilint            # 저장소의 panel/** 스타일 검사
//	go run ./cmd/uilint a.scss ... # 지정 파일만
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"birdcage/tools/internal/loopkit"
)

// Finding is one violation.
type Finding struct {
	Path string
	Line int
	Rule string
	Msg  string
}

func (f Finding) String() string { return fmt.Sprintf("%s:%d: %s: %s", f.Path, f.Line, f.Rule, f.Msg) }

type pattern struct {
	rule string
	re   *regexp.Regexp
	msg  string
}

// cssRules: (기능, 최소 Chrome) 근거는 MDN browser-compat-data / caniuse(docs/decisions/0010).
var cssRules = []pattern{
	{"no-flex-gap", regexp.MustCompile(`(^|[\s;{])(gap|row-gap|column-gap)\s*:`), "flex gap은 Chrome 84+ (CEP 10 = Chromium 74): 형제 margin으로 간격을 준다"},
	{"no-grid", regexp.MustCompile(`display\s*:\s*(inline-)?grid\b`), "UXP에 CSS Grid가 없다: flexbox만 쓴다"},
	{"no-aspect-ratio", regexp.MustCompile(`(^|[\s;{])aspect-ratio\s*:`), "aspect-ratio는 Chrome 88+: 고정 width/height를 쓴다"},
	{"no-inset", regexp.MustCompile(`(^|[\s;{])inset\s*:`), "inset 속성은 Chrome 87+: top/right/bottom/left를 쓴다"},
	{"no-focus-visible", regexp.MustCompile(`:focus-visible`), ":focus-visible은 Chrome 86+: 선택자 전체가 무시된다"},
	{"no-is-where-has", regexp.MustCompile(`:(is|where|has)\(`), ":is()/:where()는 Chrome 88+, :has()는 105+: 선택자 전체가 무시된다"},
	{"no-clamp", regexp.MustCompile(`\bclamp\(`), "clamp()는 Chrome 79+"},
}

// tsxRules catch the same features in React inline styles.
var tsxRules = []pattern{
	{"no-flex-gap", regexp.MustCompile(`\b(gap|rowGap|columnGap)\s*:`), "flex gap은 Chrome 84+ (CEP 10 = Chromium 74)"},
	{"no-grid", regexp.MustCompile(`display\s*:\s*["'](inline-)?grid`), "UXP에 CSS Grid가 없다"},
	{"no-aspect-ratio", regexp.MustCompile(`\baspectRatio\s*:`), "aspect-ratio는 Chrome 88+"},
	{"no-inset", regexp.MustCompile(`\binset\s*:`), "inset 속성은 Chrome 87+"},
}

var (
	cssRadius = regexp.MustCompile(`border(-[a-z]+)*-radius\s*:\s*([^;}]+)`)
	tsxRadius = regexp.MustCompile(`border[A-Za-z]*Radius\s*:\s*([^,}]+)`)
	pxValue   = regexp.MustCompile(`^(0|\d+(\.\d+)?px)$`)
)

// MaxRadiusPx is the largest corner radius allowed anywhere in the panel.
const MaxRadiusPx = 2

func radiusOK(v string) bool {
	v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
	v = strings.Trim(v, `"' `)
	for _, part := range strings.Fields(strings.ReplaceAll(v, "/", " ")) {
		if strings.HasPrefix(part, "$radius") { // 토큰
			continue
		}
		if n, err := strconv.ParseFloat(part, 64); err == nil { // TSX 숫자(px)
			if n > MaxRadiusPx {
				return false
			}
			continue
		}
		if !pxValue.MatchString(part) {
			return false // %, em, 변수 등은 반경을 보장할 수 없다
		}
		if n, _ := strconv.ParseFloat(strings.TrimSuffix(part, "px"), 64); n > MaxRadiusPx {
			return false
		}
	}
	return true
}

// stripComments blanks comments but keeps line breaks so line numbers stay right.
func stripComments(src string, lineComments bool) string {
	var b strings.Builder
	inBlock, inLine := false, false
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		next := byte(0)
		if i+1 < len(src) {
			next = src[i+1]
		}
		switch {
		case inBlock:
			if c == '*' && next == '/' {
				inBlock = false
				b.WriteString("  ")
				i++
				continue
			}
		case inLine:
			if c == '\n' {
				inLine = false
			}
		case quote != 0:
			if c == quote && (i == 0 || src[i-1] != '\\') {
				quote = 0
			}
			b.WriteByte(c)
			continue
		case c == '/' && next == '*':
			inBlock = true
			b.WriteString("  ")
			i++
			continue
		case lineComments && c == '/' && next == '/' && (i == 0 || src[i-1] != ':'): // url(http://…) 제외
			inLine = true
		case c == '"' || c == '\'' || c == '`':
			quote = c
			b.WriteByte(c)
			continue
		}
		if c == '\n' {
			b.WriteByte('\n')
		} else if inBlock || inLine {
			b.WriteByte(' ')
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Lint checks one file's content; kind is chosen by extension (.scss/.css or .tsx/.ts).
func Lint(path, src string) []Finding {
	ext := filepath.Ext(path)
	isStyle := ext == ".scss" || ext == ".css"
	code := stripComments(src, true)
	rules, radius := tsxRules, tsxRadius
	if isStyle {
		rules, radius = cssRules, cssRadius
	}
	var out []Finding
	for i, line := range strings.Split(code, "\n") {
		for _, r := range rules {
			if r.re.MatchString(line) {
				out = append(out, Finding{path, i + 1, r.rule, r.msg})
			}
		}
		for _, m := range radius.FindAllStringSubmatch(line, -1) {
			v := m[len(m)-1]
			if !radiusOK(v) {
				out = append(out, Finding{path, i + 1, "radius-token", fmt.Sprintf("반경 %q: $radius 토큰 또는 %dpx 이하만 허용(사각 카드)", strings.TrimSpace(v), MaxRadiusPx)})
			}
		}
	}
	return out
}

// defaultGlobs are the panel sources the gate covers (CEP now, UXP later).
var defaultGlobs = []string{"panel/**/src/**/*.scss", "panel/**/src/**/*.css", "panel/**/src/js/**/*.tsx", "panel/**/test/ui/**/*.tsx"}

func main() {
	var files []string
	root := "."
	if len(os.Args) > 1 {
		files = os.Args[1:]
	} else {
		r, err := loopkit.Root(".")
		if err != nil {
			fmt.Fprintln(os.Stderr, "uilint:", err)
			os.Exit(2)
		}
		root = r
		files, err = loopkit.Files(root, defaultGlobs...)
		if err != nil {
			fmt.Fprintln(os.Stderr, "uilint:", err)
			os.Exit(2)
		}
	}
	var all []Finding
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			fmt.Fprintln(os.Stderr, "uilint:", err)
			os.Exit(2)
		}
		all = append(all, Lint(f, string(b))...)
	}
	for _, f := range all {
		fmt.Println(f)
	}
	if len(all) > 0 {
		fmt.Printf("uilint: %d violation(s) in %d file(s) checked\n", len(all), len(files))
		os.Exit(1)
	}
	fmt.Printf("uilint: %d file(s) clean\n", len(files))
}
