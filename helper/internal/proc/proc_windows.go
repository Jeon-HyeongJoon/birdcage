//go:build windows

package proc

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func setup(cmd *exec.Cmd) {
	// 콘솔 창을 띄우지 않고, Ctrl+C 신호가 Premiere 쪽으로 번지지 않게 별도 그룹으로 띄운다.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
	}
}

// tree is a Job Object holding the child and everything it spawns.
type tree struct{ job windows.Handle }

func attach(cmd *exec.Cmd) (*tree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	// Helper가 죽어도 Job 핸들이 닫히면 자식 트리 전체가 종료된다.
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	defer windows.CloseHandle(h)
	// 주의: Start와 Assign 사이에 손자 프로세스가 생기면 Job 밖에 남을 수 있다(yt-dlp는 기동이 느려 실질적 위험 낮음).
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	return &tree{job: job}, nil
}

// Windows에는 콘솔 없는 프로세스에 보낼 정중한 종료 신호가 마땅치 않아 즉시 종료한다.
func (t *tree) stop() error { return t.kill() }

func (t *tree) kill() error {
	if t.job == 0 {
		return nil
	}
	return windows.TerminateJobObject(t.job, 1)
}

func (t *tree) close() {
	if t.job != 0 {
		windows.CloseHandle(t.job)
		t.job = 0
	}
}

func (t *tree) peakMemory() int64 {
	if t.job == 0 {
		return 0
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	var n uint32
	if err := windows.QueryInformationJobObject(t.job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), &n); err != nil {
		return 0
	}
	return int64(info.PeakJobMemoryUsed)
}

// Windows의 ProcessState에는 메모리 정보가 없어 Job의 최대 커밋 메모리를 쓴다.
func maxRSS(ps *os.ProcessState) int64 { return 0 }
