//go:build !android

package app

import "testing"

// TestJoydevSnapshotRemap verifies the xpad→standard-layout remapping:
// buttons, stick normalization, trigger/dpad axis thresholds.
func TestJoydevSnapshotRemap(t *testing.T) {
	d := &joyDev{}
	d.buttons[0] = true // A
	d.buttons[5] = true // RB
	d.buttons[7] = true // Start
	d.axes[0] = 16383   // LX half right
	d.axes[4] = -32767  // RY full up
	d.axes[2] = 32767   // LT pulled
	d.axes[6] = -32767  // DPad left
	d.axes[7] = 32767   // DPad down

	g := d.snapshot()
	if !g.Connected {
		t.Fatal("snapshot not connected")
	}
	if !g.Pressed(PadA) || !g.Pressed(PadRB) || !g.Pressed(PadStart) {
		t.Fatalf("buttons wrong: %+v", g.Buttons)
	}
	if g.Pressed(PadB) || g.Pressed(PadLB) {
		t.Fatal("unpressed buttons reported pressed")
	}
	if g.Axes[0] < 0.49 || g.Axes[0] > 0.51 {
		t.Fatalf("LX = %v, want ~0.5", g.Axes[0])
	}
	if g.Axes[3] != -1 {
		t.Fatalf("RY = %v, want -1 (up)", g.Axes[3])
	}
	if !g.Pressed(PadLT) || g.Pressed(PadRT) {
		t.Fatal("trigger threshold wrong")
	}
	if !g.Pressed(PadLeft) || !g.Pressed(PadDown) || g.Pressed(PadRight) || g.Pressed(PadUp) {
		t.Fatalf("dpad wrong: %+v", g.Buttons)
	}
	// Polling with no devices present must not panic and reports empty slots.
	pads := Gamepads()
	for i := range pads {
		_ = pads[i].Connected // hardware-dependent; just exercise the path
	}
}
