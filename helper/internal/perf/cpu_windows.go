//go:build windows

package perf

import "golang.org/x/sys/windows"

// ProcessCPUSeconds returns user+kernel CPU time consumed by this process (from the OS).
func ProcessCPUSeconds() float64 {
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &c, &e, &k, &u); err != nil {
		return 0
	}
	// FILETIME은 100ns 단위.
	return float64(ft(k)+ft(u)) / 1e7
}

func ft(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
