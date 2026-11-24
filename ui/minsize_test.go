package ui

import "testing"

// TestUIMinSize: a toolbar row of fixed buttons and a spacer over a
// stretching body holding a wrapping paragraph and a fixed panel — the
// minimum is the buttons side by side (gaps, padding) by the toolbar's and
// panel's heights stacked; the spacer and the paragraph ask for nothing.
func TestUIMinSize(t *testing.T) {
	button := func() Element { return Div().W(200).H(32) }
	view := func() Element {
		return Div().P(8).Gap(6).Children(
			Div().Row().Gap(10).Children(button(), button(), Div().Grow(), button()),
			Div().Grow().Children(
				Wrapped("a long paragraph that wraps to whatever width it is given, so it is no minimum at all", RGBA{A: 1}, 0),
				Div().W(150).H(300),
			),
		)
	}
	u := New(nil, nil, view)
	u.Layout(1600, 900)
	w, h := u.MinSize()
	// 3·200 + 3·10 (the spacer keeps its gap slot) + 2·8 wide; 32 + 6 + 300 + 2·8 tall
	// (the paragraph has no font here, so it measures nothing).
	wantW, wantH := float32(3*200+3*10+16), float32(32+6+300+16)
	if w != wantW || h != wantH {
		t.Fatalf("MinSize = %v×%v, want %v×%v", w, h, wantW, wantH)
	}
	// It does not depend on the size the tree was laid out at.
	u.Layout(500, 300)
	if w2, h2 := u.MinSize(); w2 != w || h2 != h {
		t.Fatalf("MinSize at 500×300 = %v×%v, at 1600×900 %v×%v", w2, h2, w, h)
	}
}
