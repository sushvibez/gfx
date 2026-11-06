package app

// Monitor describes one display in the virtual desktop.
//
// X, Y, Width and Height are PHYSICAL pixels in the virtual-desktop
// coordinate space — the same space Window.Geometry/SetGeometry use — so a
// second screen to the right of a 1920-wide primary reports X=1920. Scale is
// the DPI scale (physical pixels per logical pixel) the platform reports for
// that monitor: Windows gives a true per-monitor value, X11 has no
// per-monitor DPI so every monitor carries the desktop-wide Xft.dpi/96.
type Monitor struct {
	Name    string
	X, Y    int
	Width   int
	Height  int
	Scale   float32
	Primary bool
}

// Contains reports whether the point (px, py), in virtual-desktop pixels,
// falls inside this monitor.
func (m Monitor) Contains(px, py int) bool {
	return px >= m.X && px < m.X+m.Width && py >= m.Y && py < m.Y+m.Height
}

// monitorIndexAt returns the index of the monitor containing (px, py), or -1.
// Nothing contains the point (a window dragged off-screen, or an empty
// layout) falls back to the nearest monitor by center distance, so callers
// only see -1 when the layout itself is unavailable.
func monitorIndexAt(mons []Monitor, px, py int) int {
	for i, m := range mons {
		if m.Contains(px, py) {
			return i
		}
	}
	best, bestD := -1, int64(-1)
	for i, m := range mons {
		cx := int64(m.X + m.Width/2 - px)
		cy := int64(m.Y + m.Height/2 - py)
		d := cx*cx + cy*cy
		if bestD < 0 || d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// pickMonitor returns the monitor a window opening at (x, y) belongs on:
// the one containing the point when positioned is true, else the primary
// (or the first). The bool is false when no monitor layout is available.
func pickMonitor(mons []Monitor, x, y int, positioned bool) (Monitor, int, bool) {
	if len(mons) == 0 {
		return Monitor{}, -1, false
	}
	if positioned {
		if i := monitorIndexAt(mons, x, y); i >= 0 {
			return mons[i], i, true
		}
	}
	for i, m := range mons {
		if m.Primary {
			return m, i, true
		}
	}
	return mons[0], 0, true
}
