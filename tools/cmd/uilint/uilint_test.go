package main

import (
	"strings"
	"testing"
)

func rulesOf(fs []Finding) string {
	var r []string
	for _, f := range fs {
		r = append(r, f.Rule)
	}
	return strings.Join(r, ",")
}

func TestLintSCSS(t *testing.T) {
	src := `
// gap: 6px; 주석은 무시
.row { display: flex; gap: 6px; }
.grid { display: grid; }
.thumb { aspect-ratio: 16 / 9; }
.cover { inset: 0; }
button:focus-visible { outline: 1px solid; }
:is(.a, .b) { color: red; }
.w { width: clamp(10px, 50%, 100px); }
.card { border-radius: 8px; }
.ok1 { border-radius: $radius; }
.ok2 { border-radius: 2px 0; border-top-left-radius: 1px; }
.pill { border-radius: 50%; }
/* border-radius: 99px; 블록 주석 */
.bg { background: url(http://example.com/a.png); margin: 0; }
`
	got := rulesOf(Lint("a.scss", src))
	want := "no-flex-gap,no-grid,no-aspect-ratio,no-inset,no-focus-visible,no-is-where-has,no-clamp,radius-token,radius-token"
	if got != want {
		t.Fatalf("rules\n got %s\nwant %s", got, want)
	}
	// 줄 번호는 주석 제거 뒤에도 원본과 같다.
	if f := Lint("a.scss", src)[0]; f.Line != 3 {
		t.Fatalf("line %d, want 3", f.Line)
	}
}

func TestLintTSX(t *testing.T) {
	src := `const a = <div style={{ gap: 4, borderRadius: 8 }} />;
const b = <div style={{ borderRadius: 2, display: "flex" }} />;
const c = <div style={{ display: "grid", aspectRatio: "16/9" }} />;
// style={{ gap: 4 }}
const d = "gap: 4px"; // 문자열 안의 텍스트는 UI 코드가 아니지만 보수적으로 잡는다
`
	got := rulesOf(Lint("a.tsx", src))
	want := "no-flex-gap,radius-token,no-grid,no-aspect-ratio,no-flex-gap"
	if got != want {
		t.Fatalf("rules\n got %s\nwant %s", got, want)
	}
}

func TestRadiusOK(t *testing.T) {
	for v, want := range map[string]bool{"0": true, "2px": true, "1.5px": true, "$radius": true, "$radius-none": true,
		"2px 0 2px 0": true, "3px": false, "50%": false, "0.5em": false, "var(--r)": false, "2": true, "4": false, `"2px"`: true} {
		if got := radiusOK(v); got != want {
			t.Errorf("radiusOK(%q) = %v, want %v", v, got, want)
		}
	}
}
