package ui

import "testing"

// TestFrameBuildsOnce: the shell lays a frame out, renders its layers, then
// calls Frame — which must paint THAT tree, not build and lay out a second
// one (every frame used to pay for the whole tree twice).
func TestFrameBuildsOnce(t *testing.T) {
	builds := 0
	u := New(nil, nil, func() Element {
		builds++
		return Div().W(100).H(50)
	})
	u.Layout(300, 200)
	u.Frame(300, 200)
	if builds != 1 {
		t.Fatalf("Layout+Frame built the tree %d times, want 1", builds)
	}
	// Frame on its own still builds (no layout preceded it).
	u.Frame(300, 200)
	if builds != 2 {
		t.Fatalf("a bare Frame built %d times in total, want 2", builds)
	}
	// A size change between Layout and Frame is a rebuild.
	u.Layout(300, 200)
	u.Frame(400, 200)
	if builds != 4 {
		t.Fatalf("Layout then Frame at a new size built %d times in total, want 4", builds)
	}
}
