package main

import "testing"

// 시스템 글꼴 경로(system-ui·BlinkMacSystemFont·-apple-system)는 CEP 10에서 새 글자·단어 배치가 수백 배 느리다.
func TestLintSystemFont(t *testing.T) {
	src := `
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
.a { font-family: system-ui; }
$font-family: "Helvetica Neue", "Apple SD Gothic Neo", "Segoe UI", "Malgun Gothic", sans-serif;
.ok1 { font-family: $font-family; }
.ok2 { font-family: inherit; }
`
	if got := rulesOf(Lint("a.scss", src)); got != "no-system-font,no-system-font" {
		t.Fatalf("scss rules %q", got)
	}
	tsx := `const a = <div style={{ fontFamily: "system-ui" }} />;
const b = <div style={{ fontFamily: "Helvetica Neue" }} />;`
	if got := rulesOf(Lint("a.tsx", tsx)); got != "no-system-font" {
		t.Fatalf("tsx rules %q", got)
	}
}
