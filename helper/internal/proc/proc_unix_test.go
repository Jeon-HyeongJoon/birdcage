//go:build unix

package proc_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"birdcage/helper/internal/perf/perftest"
	"birdcage/helper/internal/proc"
)

// 취소 시 손자 프로세스(yt-dlp → ffmpeg 같은 구조)까지 정리되는지 확인한다.
func TestCancelKillsProcessTree(t *testing.T) {
	perftest.Track(t)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := proc.Run(ctx, proc.Spec{Path: sh(t), Args: []string{"-c", "sleep 30 & echo $! > " + pidFile + "; wait"}, KillGrace: 200 * time.Millisecond})
		done <- err
	}()
	var pid int
	for i := 0; i < 100; i++ {
		if b, err := os.ReadFile(pidFile); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("grandchild did not start")
	}
	start := time.Now()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("cancel took %v", time.Since(start))
	}
	for i := 0; i < 50; i++ {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild %d still alive after cancel", pid)
}
