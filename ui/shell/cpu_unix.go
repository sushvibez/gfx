//go:build !windows && !js

package shell

import (
	"syscall"
	"time"
)

// processCPU is the process's CPU time so far (all threads), for the
// GFX_FPS exit report.
func processCPU() (time.Duration, bool) {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0, false
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano()), true
}
