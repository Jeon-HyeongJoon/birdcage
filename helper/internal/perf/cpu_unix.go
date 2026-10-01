//go:build unix

package perf

import "syscall"

// ProcessCPUSeconds returns user+system CPU time consumed by this process (from the OS).
func ProcessCPUSeconds() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return tv(ru.Utime) + tv(ru.Stime)
}

func tv(t syscall.Timeval) float64 { return float64(t.Sec) + float64(t.Usec)/1e6 }
