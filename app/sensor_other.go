//go:build !android

package app

// Accelerometer reports no sample: only the Android backend reads one.
// (The web could through DeviceMotionEvent; not wired.)
func Accelerometer() (x, y, z float32, ok bool) { return 0, 0, 0, false }

// Orientation reports no sample: only the Android backend reads one.
func Orientation() (x, y, z, w float32, ok bool) { return 0, 0, 0, 1, false }
