package app

import "testing"

// The headless host is the portable place to test that placement options
// survive the trip through NewHeadless — every native backend reads the same
// fields off the same struct.
func TestHeadlessPlacementOptions(t *testing.T) {
	h := NewHeadless(Options{Width: 640, Height: 480, X: 1920, Y: 200, Positioned: true})
	if x, y, ok := h.Position(); !ok || x != 1920 || y != 200 {
		t.Fatalf("Position = %d,%d ok=%v; want 1920,200 true", x, y, ok)
	}
	if h.Fullscreen() {
		t.Fatal("Fullscreen set without Options.Fullscreen")
	}
	if got := h.MonitorIndex(); got != 0 {
		t.Fatalf("MonitorIndex = %d; want 0", got)
	}
	mons := h.Monitors()
	if len(mons) != 1 {
		t.Fatalf("Monitors = %d monitors; want 1", len(mons))
	}
	if m := mons[0]; m.Width != 640 || m.Height != 480 || !m.Primary || m.Scale != 1 {
		t.Fatalf("headless monitor = %+v; want 640x480 primary scale 1", m)
	}
}

func TestHeadlessFullscreenOption(t *testing.T) {
	h := NewHeadless(Options{Width: 800, Height: 600, Fullscreen: true})
	if !h.Fullscreen() {
		t.Fatal("Options.Fullscreen did not reach the host")
	}
	// A fullscreen window is positioned by definition: it covers a monitor.
	if _, _, ok := h.Position(); !ok {
		t.Fatal("fullscreen host reports no position")
	}
	h.SetFullscreen(false)
	if h.Fullscreen() {
		t.Fatal("SetFullscreen(false) did not clear the state")
	}
}

// Monitor.Contains / monitorIndexAt are the geometry every backend's
// MonitorIndex and fullscreen placement share.
func TestMonitorIndexAt(t *testing.T) {
	mons := []Monitor{
		{Name: "left", X: 0, Y: 0, Width: 1920, Height: 1080, Primary: true},
		{Name: "right", X: 1920, Y: 0, Width: 1280, Height: 1024},
	}
	for _, tc := range []struct {
		x, y int
		want int
	}{
		{10, 10, 0},
		{1919, 1079, 0},
		{1920, 0, 1},
		{2500, 500, 1},
		{-500, 500, 0},  // off-desktop: nearest monitor, never -1
		{5000, 5000, 1}, // ditto, on the other side
	} {
		if got := monitorIndexAt(mons, tc.x, tc.y); got != tc.want {
			t.Errorf("monitorIndexAt(%d,%d) = %d; want %d", tc.x, tc.y, got, tc.want)
		}
	}
	if got := monitorIndexAt(nil, 0, 0); got != -1 {
		t.Errorf("monitorIndexAt(nil) = %d; want -1", got)
	}
}

func TestPickMonitor(t *testing.T) {
	mons := []Monitor{
		{Name: "left", X: 0, Y: 0, Width: 1920, Height: 1080},
		{Name: "right", X: 1920, Y: 0, Width: 1280, Height: 1024, Primary: true},
	}
	// Not positioned: the primary wins, wherever it sits in the slice.
	if m, i, ok := pickMonitor(mons, 0, 0, false); !ok || i != 1 || m.Name != "right" {
		t.Fatalf("pickMonitor(unpositioned) = %q/%d ok=%v; want right/1", m.Name, i, ok)
	}
	if m, i, ok := pickMonitor(mons, 100, 100, true); !ok || i != 0 || m.Name != "left" {
		t.Fatalf("pickMonitor(100,100) = %q/%d ok=%v; want left/0", m.Name, i, ok)
	}
	if _, _, ok := pickMonitor(nil, 0, 0, true); ok {
		t.Fatal("pickMonitor(no monitors) reported ok")
	}
}
