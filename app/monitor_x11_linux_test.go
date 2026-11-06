//go:build !android

package app

import (
	"os"
	"testing"
)

func TestMonitorsX11(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	mons, err := Monitors()
	if err != nil {
		t.Fatalf("Monitors: %v", err)
	}
	if len(mons) == 0 {
		t.Fatal("Monitors returned an empty layout")
	}
	primaries := 0
	for i, m := range mons {
		if m.Width <= 0 || m.Height <= 0 {
			t.Errorf("monitor %d (%q) has no size: %+v", i, m.Name, m)
		}
		if m.Scale <= 0 {
			t.Errorf("monitor %d (%q) has scale %v", i, m.Name, m.Scale)
		}
		if m.Primary {
			primaries++
		}
		t.Logf("monitor %d: %+v", i, m)
	}
	if primaries != 1 {
		t.Errorf("got %d primary monitors; want exactly 1", primaries)
	}
	// No window was ever opened: Monitors must work off its own connection.
	if procDisplay != 0 {
		t.Error("Monitors leaked a process display connection")
	}
}

// Monitors must also work while a window holds the display connection —
// that path reuses procDisplay instead of opening a second connection.
func TestMonitorsWithOpenWindow(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	w, err := NewWindow(Options{Title: "gfx monitors", Width: 200, Height: 150})
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	defer w.Destroy()
	if procDisplay == 0 {
		t.Fatal("NewWindow did not publish its display for Monitors")
	}
	mons, err := Monitors()
	if err != nil {
		t.Fatalf("Monitors with a window open: %v", err)
	}
	if len(mons) == 0 {
		t.Fatal("empty layout")
	}
	if idx := w.MonitorIndex(); idx < 0 || idx >= len(mons) {
		t.Fatalf("MonitorIndex = %d; want an index into %d monitors", idx, len(mons))
	}
}

// Two top-level windows from NewWindow must coexist: each owns its own X
// display connection, so each Poll drains only its own events (see doc.go).
func TestTwoTopLevelWindows(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	a, err := NewWindow(Options{Title: "gfx A", Width: 200, Height: 150})
	if err != nil {
		t.Fatalf("NewWindow A: %v", err)
	}
	defer a.Destroy()
	b, err := NewWindow(Options{Title: "gfx B", Width: 220, Height: 160})
	if err != nil {
		t.Fatalf("NewWindow B: %v", err)
	}
	defer b.Destroy()
	if a.XlibDisplay() == b.XlibDisplay() {
		t.Fatal("two NewWindow calls shared a display connection")
	}
	if a.XlibWindow() == b.XlibWindow() {
		t.Fatal("two NewWindow calls produced the same window id")
	}
	for range 10 {
		if !a.Poll() || !b.Poll() {
			t.Fatal("a window reported closed without a close request")
		}
	}
	// Each window must keep its own size: a shared, mis-routed event queue
	// would apply one window's ConfigureNotify to the other.
	aw, ah := a.Size()
	bw, bh := b.Size()
	if aw == bw && ah == bh {
		t.Fatalf("both windows report %dx%d — events look cross-routed", aw, ah)
	}
	// Closing one must not close the other.
	b.Destroy()
	if !a.Poll() {
		t.Fatal("destroying the second window closed the first")
	}
	if procDisplay == 0 {
		t.Error("destroying a non-owning window cleared the process display")
	}
}
