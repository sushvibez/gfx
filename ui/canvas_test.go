package ui

import "testing"

// A Canvas is a layout item like any other: QML's easing plot is
// `Layout.fillWidth: true; Layout.preferredHeight: 60`, and a canvas that
// ignored both sat at the 300x150 an HTML canvas defaults to — half as wide
// and twice as tall as Qt's inside the same card.
func TestCanvasFillsAndTakesAPreferredSize(t *testing.T) {
	col := Div().Col().Children(
		Div().Size(200, 40),
		NewCanvas(300, 150, nil).H(60).W(0).CrossFill(),
	)
	col.layout(&Context{}, Rect{0, 0, 460, 300})
	kids := Children(col)
	if len(kids) != 2 {
		t.Fatalf("built %d children", len(kids))
	}
	cv := Bounds(kids[1])
	if cv.H != 60 {
		t.Errorf("canvas height = %v, want its preferred 60", cv.H)
	}
	if cv.W != 460 {
		t.Errorf("canvas width = %v, want the line's 460", cv.W)
	}
	// Growing on the main axis takes the leftover.
	col2 := Div().Col().Children(
		Div().Size(200, 40),
		NewCanvas(300, 150, nil).Grow(),
	)
	col2.layout(&Context{}, Rect{0, 0, 460, 300})
	if h := Bounds(Children(col2)[1]).H; h != 260 {
		t.Errorf("growing canvas height = %v, want 260", h)
	}
}
