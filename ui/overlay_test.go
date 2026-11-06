package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

// Escape has to close whatever is open on top — a dropdown, a menu, a modal.
// Without it a viewer who opens a combo box's list by mistake has no way back
// except clicking away, and every popup in the QML corpus takes Escape.
func TestEscapeDismissesTopmostOverlay(t *testing.T) {
	var outer, inner int
	root := Div().Children(
		PositionedModal(Div().Size(50, 50), 10, 10, RGBA{}, func() { outer++ }),
	)
	u := New(nil, nil, func() Element { return root })
	u.Layout(200, 200)
	if !u.Key(app.KeyEvent{Down: true, Key: app.KeyEscape}) {
		t.Fatal("Escape not consumed with a dismissable overlay open")
	}
	if outer != 1 || inner != 0 {
		t.Fatalf("dismiss counts = %d/%d, want the open modal dismissed once", outer, inner)
	}
	// The TOPMOST one goes first: two stacked panels close one Escape at a
	// time, innermost first, as they were opened.
	root = Div().Children(
		PositionedModal(Div().Size(50, 50), 10, 10, RGBA{}, func() { outer++ }),
		PositionedModal(Div().Size(30, 30), 20, 20, RGBA{}, func() { inner++ }),
	)
	u = New(nil, nil, func() Element { return root })
	u.Layout(200, 200)
	u.Key(app.KeyEvent{Down: true, Key: app.KeyEscape})
	if inner != 1 || outer != 1 {
		t.Fatalf("dismiss counts = %d/%d, want only the topmost overlay dismissed", outer, inner)
	}
	// Nothing open: Escape is somebody else's event.
	plain := Div().Children(Text("x", RGBA{}))
	u = New(nil, nil, func() Element { return plain })
	u.Layout(200, 200)
	if u.Key(app.KeyEvent{Down: true, Key: app.KeyEscape}) {
		t.Error("Escape consumed with nothing open")
	}
}
