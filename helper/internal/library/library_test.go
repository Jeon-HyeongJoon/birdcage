package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"birdcage/helper/internal/perf/perftest"
)

func TestMain(m *testing.M) { perftest.Main(m) }

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		`a/b\c:d*e?f"g<h>i|j`:             "a_b_c_d_e_f_g_h_i_j",
		"  spaced\t\nout  ":               "spaced out",
		"CON":                             "_CON",
		"nul.txt":                         "_nul.txt",
		"trailing dots...":                "trailing dots",
		"":                                "untitled",
		"\x00\x01":                        "untitled",
		"youtube-dl test video \"'/\\ä↭𝕐": "youtube-dl test video _'__ä↭𝕐",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	// macOS 파일명에서 흔한 NFD 한글은 NFC로 통일한다.
	nfd := norm.NFD.String("한글 제목")
	if got := Sanitize(nfd); got != "한글 제목" || !norm.NFC.IsNormalString(got) {
		t.Errorf("NFC: %q", got)
	}
	long := strings.Repeat("가", 200)
	if n := utf8.RuneCountInString(Sanitize(long)); n != maxNameRunes {
		t.Errorf("length %d", n)
	}
}

func TestPathAvoidsCollisions(t *testing.T) {
	root := t.TempDir()
	p1, rel1, err := Reserve(root, "Youtube", "Title", "abc", ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != filepath.Join(root, "Youtube", "Title [abc].mp4") {
		t.Fatalf("p1 = %s", p1)
	}
	// 디스크에 아직 없어도 예약 중이면 다른 경로를 받는다.
	p2, rel2, _ := Reserve(root, "Youtube", "Title", "abc", ".mp4")
	if p2 != filepath.Join(root, "Youtube", "Title [abc] (2).mp4") {
		t.Fatalf("p2 = %s", p2)
	}
	rel2()
	if err := os.WriteFile(p1, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	rel1()
	// 파일이 생긴 뒤에는 예약을 풀어도 기존 파일을 피한다.
	p4, rel4, _ := Reserve(root, "Youtube", "Title", "abc", ".mp4")
	defer rel4()
	if p4 != filepath.Join(root, "Youtube", "Title [abc] (2).mp4") {
		t.Fatalf("p4 = %s", p4)
	}
	p3, rel3, _ := Reserve(root, "", "x", "", ".mp4")
	defer rel3()
	if filepath.Base(filepath.Dir(p3)) != "Other" {
		t.Fatalf("p3 = %s", p3)
	}
}

func BenchmarkSanitize(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = Sanitize(`[MV] 아이유(IU) _ 좋은 날 (Good Day) "Official" 4K / Live?`)
	}
}
