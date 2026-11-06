package ui

import "testing"

// OnHoverChanged/OnPressChanged report an element's live hover and press
// state — the mechanism gml uses to back MouseArea.containsMouse and
// Control.hovered/pressed. They fire from LAYOUT, so the state is there for
// anything that lays the tree out (a headless render, tooling, a test) and
// not only for a frame that reaches the renderer: a styled button held down
// has to report itself pressed to its own label.
func TestDivHoverPressCallbacks(t *testing.T) {
	var hovered, pressed []bool
	d := Div().Size(100, 50)
	d.OnHoverChanged(func(v bool) { hovered = append(hovered, v) })
	d.OnPressChanged(func(v bool) { pressed = append(pressed, v) })
	box := Rect{X: 0, Y: 0, W: 100, H: 50}

	// Pointer outside the box.
	d.layout(&Context{MouseX: 200, MouseY: 200}, box)
	if len(hovered) != 1 || hovered[0] {
		t.Fatalf("outside rect: hovered = %v, want [false]", hovered)
	}
	if len(pressed) != 1 || pressed[0] {
		t.Fatalf("outside rect: pressed = %v, want [false]", pressed)
	}

	// Inside, not pressed.
	d.layout(&Context{MouseX: 50, MouseY: 25}, box)
	if len(hovered) != 2 || !hovered[1] {
		t.Fatalf("inside rect: hovered = %v, want [false true]", hovered)
	}
	if len(pressed) != 2 || pressed[1] {
		t.Fatalf("inside rect, not pressed: pressed = %v, want [false false]", pressed)
	}

	// Pressed inside.
	d.layout(&Context{MouseX: 50, MouseY: 25, MousePressed: true, pressX: 50, pressY: 25}, box)
	if len(pressed) != 3 || !pressed[2] {
		t.Fatalf("pressed inside rect: pressed = %v, want [false false true]", pressed)
	}

	// A press that STARTED elsewhere is not this element's press, however
	// far the pointer has since travelled onto it.
	d.layout(&Context{MouseX: 50, MouseY: 25, MousePressed: true, pressX: 300, pressY: 300}, box)
	if len(pressed) != 4 || pressed[3] {
		t.Fatalf("press from outside: pressed = %v, want a trailing false", pressed)
	}
}
