//go:build windows || js

package shell

import "time"

// processCPU is not reported on this platform.
func processCPU() (time.Duration, bool) { return 0, false }
