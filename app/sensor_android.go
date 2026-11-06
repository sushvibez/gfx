//go:build android

package app

/*
#include "android_glue.h"
*/
import "C"

// Accelerometer returns the latest accelerometer sample in the device's
// axes (m/s²; Android's convention: lying flat, screen up, z ≈ +9.8), and
// whether one has arrived yet. Samples flow at ~60 Hz while the frame
// loop polls (Poll pumps the sensor queue with the input queue).
func Accelerometer() (x, y, z float32, ok bool) {
	var v [3]C.float
	seq := C.gfx_accel(&v[0])
	return float32(v[0]), float32(v[1]), float32(v[2]), seq != 0
}

// Orientation returns the latest device orientation as a unit quaternion
// (x, y, z, w) rotating device axes into the world frame — Android's game
// rotation vector: gyroscope and accelerometer, no magnetometer, so there
// is no absolute north and no drift from nearby metal; a game takes its
// heading relative to where it was calibrated. ok is false until a sample
// has arrived.
func Orientation() (x, y, z, w float32, ok bool) {
	var v [4]C.float
	seq := C.gfx_rotation(&v[0])
	return float32(v[0]), float32(v[1]), float32(v[2]), float32(v[3]), seq != 0
}
