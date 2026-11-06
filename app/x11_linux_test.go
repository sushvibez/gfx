package app

import (
	"os"
	"testing"
)

func TestWindowOpenPollDestroy(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	w, err := NewWindow(Options{Title: "gfx test", Width: 320, Height: 200})
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	defer w.Destroy()

	for range 10 {
		if !w.Poll() {
			t.Fatal("window reported closed without a close request")
		}
	}
	ww, wh := w.Size()
	if ww <= 0 || wh <= 0 {
		t.Fatalf("bad size %dx%d", ww, wh)
	}
	if w.XlibDisplay() == 0 || w.XlibWindow() == 0 {
		t.Fatal("native handles missing")
	}
}
