package subs

import (
	"testing"

	"birdcage/helper/internal/perf/perftest"
)

func TestMain(m *testing.M) { perftest.Main(m) }

const sample = "\ufeff1\r\n00:00:01,000 --> 00:00:03,500\r\nhello\r\n\r\n2\r\n00:00:04,000 --> 00:00:06,250 align:start\r\nsecond\r\nline two\r\n\r\n3\r\n00:01:00,000 --> 00:01:02,000\r\nlate\r\n\r\nbroken block\r\n\r\n"

func TestParseShiftFormat(t *testing.T) {
	cues := Parse(sample)
	if len(cues) != 3 || cues[1].Text != "second\nline two" || cues[1].End != 6.25 || cues[2].Start != 60 {
		t.Fatalf("parse: %+v", cues)
	}
	// 파일이 원본 3.0s에서 시작하고 길이 10s: 첫 큐는 앞이 잘리고(0–0.5), 세 번째는 범위 밖.
	got := Format(Shift(cues, 3.0, 10))
	want := "1\n00:00:00,000 --> 00:00:00,500\nhello\n\n2\n00:00:01,000 --> 00:00:03,250\nsecond\nline two\n\n"
	if got != want {
		t.Fatalf("shifted:\n%q\nwant\n%q", got, want)
	}
	if n := len(Shift(cues, 0, 0)); n != 3 {
		t.Fatalf("no-op shift dropped cues: %d", n)
	}
	if got := Format(Shift(cues, 59.5, 0)); got != "1\n00:00:00,500 --> 00:00:02,500\nlate\n\n" {
		t.Fatalf("late only: %q", got)
	}
}

func TestStampRounding(t *testing.T) {
	for in, want := range map[float64]string{0: "00:00:00,000", 61.0005: "00:01:01,001", 3723.4: "01:02:03,400"} {
		if got := stamp(in); got != want {
			t.Errorf("stamp(%v) = %s, want %s", in, got, want)
		}
	}
}
