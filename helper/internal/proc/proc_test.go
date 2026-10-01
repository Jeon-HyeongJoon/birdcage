package proc_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/proc"
)

func TestMain(m *testing.M) { perftest.Main(m) }

func sh(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	return "/bin/sh"
}

func TestRunCapturesLinesAndStats(t *testing.T) {
	perftest.Track(t)
	var mu sync.Mutex
	var lines []string
	lw := proc.NewLineWriter(func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() })
	res, err := proc.Run(context.Background(), proc.Spec{
		Path:   sh(t),
		Args:   []string{"-c", `printf 'a\nb\rc\n'; i=0; while [ $i -lt 200000 ]; do i=$((i+1)); done; echo err >&2`},
		Stdout: lw, StderrTail: 3,
	})
	lw.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, ",") != "a,b,c" {
		t.Fatalf("lines = %q", lines)
	}
	if res.ExitCode != 0 || res.Stats.WallMS <= 0 || res.Stats.CPUUserMS+res.Stats.CPUSysMS <= 0 {
		t.Fatalf("stats not recorded: %+v", res.Stats)
	}
	if res.Stats.MaxRSSBytes <= 0 {
		t.Fatalf("max rss not recorded: %+v", res.Stats)
	}
	if len(res.StderrTail) != 1 || res.StderrTail[0] != "err" {
		t.Fatalf("tail = %q", res.StderrTail)
	}
}

func TestRunExitError(t *testing.T) {
	_, err := proc.Run(context.Background(), proc.Spec{Path: sh(t), Args: []string{"-c", "echo one >&2; echo 'ERROR: two' >&2; exit 3"}, StderrTail: 5})
	var ee *proc.ExitError
	if !errors.As(err, &ee) || ee.Code != 3 || ee.Tail[len(ee.Tail)-1] != "ERROR: two" {
		t.Fatalf("err = %#v", err)
	}
}

func TestRunNotStarted(t *testing.T) {
	_, err := proc.Run(context.Background(), proc.Spec{Path: filepath.Join(t.TempDir(), "missing")})
	if !errors.Is(err, proc.ErrNotStarted) {
		t.Fatalf("err = %v", err)
	}
}

func TestLineWriterSplitsAcrossWrites(t *testing.T) {
	var got []string
	w := proc.NewLineWriter(func(s string) { got = append(got, s) })
	for _, chunk := range []string{"hel", "lo\nwor", "ld\r\n", "tail"} {
		_, _ = w.Write([]byte(chunk))
	}
	w.Flush()
	if strings.Join(got, "|") != "hello|world|tail" {
		t.Fatalf("got %q", got)
	}
}

func BenchmarkLineWriter(b *testing.B) {
	chunk := []byte(strings.Repeat("[birdcage] downloading|1048576|10485760|NA|2097152.5|4|NA|NA|137\n", 64))
	w := proc.NewLineWriter(func(string) {})
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	for b.Loop() {
		_, _ = w.Write(chunk)
	}
}
