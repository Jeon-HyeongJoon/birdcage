//go:build unix

package proc

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

func setup(cmd *exec.Cmd) {
	// 새 프로세스 그룹으로 띄워 그룹 단위로 종료할 수 있게 한다.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

type tree struct{ pgid int }

func attach(cmd *exec.Cmd) (*tree, error) { return &tree{pgid: cmd.Process.Pid}, nil }

func (t *tree) stop() error { return signalGroup(t.pgid, syscall.SIGTERM) }
func (t *tree) kill() error { return signalGroup(t.pgid, syscall.SIGKILL) }
func (t *tree) close()      {}

// peakMemory is not available per group on Unix; maxRSS covers the direct child.
func (t *tree) peakMemory() int64 { return 0 }

func signalGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 0 {
		return nil
	}
	err := syscall.Kill(-pgid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func maxRSS(ps *os.ProcessState) int64 {
	ru, ok := ps.SysUsage().(*syscall.Rusage)
	if !ok || ru == nil {
		return 0
	}
	// macOS는 바이트, Linux는 KiB 단위로 보고한다.
	if runtime.GOOS == "darwin" {
		return int64(ru.Maxrss)
	}
	return int64(ru.Maxrss) * 1024
}
