package main

import (
	"strings"
	"testing"
)

// 긴 sh -c 스크립트가 증적 로그 파일 이름을 255바이트 넘게 만들던 문제(2026-10-03 T-022에서 발견).
func TestLogSlugBounded(t *testing.T) {
	long := []string{"sh", "-c", strings.Repeat("cp src/js/main/composer.tsx composer.bak; ", 20)}
	if s := logSlug(long); len(s) > maxSlug || strings.HasSuffix(s, "-") || !strings.HasPrefix(s, "sh-c-cp-src-js") {
		t.Fatalf("slug %q (%d)", s, len(s))
	}
	if s := logSlug([]string{"go", "test", "-count=1", "./..."}); s != "go-test-count-1" {
		t.Fatalf("short slug %q", s)
	}
	if s := logSlug([]string{"/usr/bin/ÿ"}); s != "run" {
		t.Fatalf("empty slug %q", s)
	}
}
