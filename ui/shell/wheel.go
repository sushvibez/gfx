//go:build linux || windows || js

package shell

import "github.com/sushvibez/gfx/app"

// Input.WheelY is a distance in PIXELS, app.WheelNotchPx (72) to a mouse
// notch. A touchpad sends a two-finger scroll as a stream of small deltas,
// so code that treats any nonzero WheelY as one step (or reads it as a
// count of notches) jumps a whole step on every event of the gesture, or
// 72 steps per notch. These turn it into what such code wants.

// WheelNotches is wheelY in notches, fractional for a touchpad: scale a
// continuous change by it (a zoom factor raised to it, a distance times
// it). Positive is the wheel turned toward the user, as WheelY.
func WheelNotches(wheelY float32) float32 { return wheelY / app.WheelNotchPx }

// WheelSteps turns wheel pixels into whole steps for things that move one
// item at a time (a carousel, a list selection): it accumulates the
// deltas, so a touchpad's stream of small ones steps once per notch's
// worth of scrolling instead of once per event. Keep one across frames.
type WheelSteps struct{ acc float32 }

// Take adds this frame's wheelY and returns the whole steps it completes,
// positive toward the user.
func (w *WheelSteps) Take(wheelY float32) int {
	if wheelY == 0 {
		return 0
	}
	if (w.acc > 0) != (wheelY > 0) {
		w.acc = 0 // the scroll turned round: start afresh
	}
	w.acc += wheelY
	// A hair of slack: deltas that add up to whole notches in pixels need
	// not do so exactly in float32 (thirty tenths of a notch were 2.99).
	f := w.acc / app.WheelNotchPx
	if f > 0 {
		f += 1e-3
	} else {
		f -= 1e-3
	}
	n := int(f)
	w.acc -= float32(n) * app.WheelNotchPx
	return n
}
