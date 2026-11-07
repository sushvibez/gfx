//go:build linux || windows || js

package shell

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

func TestWheelSteps(t *testing.T) {
	var w WheelSteps
	got := 0
	for i := 0; i < 30; i++ { // three notches as thirty touchpad tenths
		got += w.Take(float32(app.WheelNotchPx) / 10) // float: 72/10 as untyped ints is 7
	}
	if got != 3 {
		t.Errorf("thirty tenths of a notch = %d steps, want 3", got)
	}
	if n := w.Take(-app.WheelNotchPx); n != -1 {
		t.Errorf("a notch back = %d, want -1", n)
	}
	if n := w.Take(app.WheelNotchPx / 2); n != 0 {
		t.Errorf("half a notch = %d, want 0", n)
	}
	if WheelNotches(2*app.WheelNotchPx) != 2 {
		t.Error("WheelNotches(2 notches of pixels) != 2")
	}
}
