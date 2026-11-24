//go:build linux && !android

package app

import (
	"os"
	"testing"
)

// TestWindowManagerAndMinSize: HasWM answers (and keeps its answer) and
// SetMinSize sends the hint once per change — on a hidden window, never
// mapped. GFX_EXPECT_WM=1 or 0 pins the answer where the desktop is known.
func TestWindowManagerAndMinSize(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	w, err := NewWindow(Options{Title: "gfx test", Width: 320, Height: 200, Hidden: true})
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	defer w.Destroy()
	wm := w.HasWM()
	t.Logf("window manager: %v", wm)
	if want := os.Getenv("GFX_EXPECT_WM"); want != "" && wm != (want == "1") {
		t.Fatalf("HasWM = %v, GFX_EXPECT_WM=%s", wm, want)
	}
	if w.HasWM() != wm {
		t.Fatal("HasWM changed its answer")
	}
	w.SetMinSize(300, 180)
	if w.minW != 300 || w.minH != 180 {
		t.Fatalf("SetMinSize kept %d×%d", w.minW, w.minH)
	}
	w.SetMinSize(0, -5) // clamped, not sent as nonsense
	if w.minW != 1 || w.minH != 1 {
		t.Fatalf("SetMinSize(0, -5) kept %d×%d", w.minW, w.minH)
	}
}
