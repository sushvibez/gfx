package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

func clickAt(u *UI, r Rect) {
	x, y := r.X+r.W/2, r.Y+r.H/2
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: x, Y: y, Button: 1}, {Kind: app.MouseUp, X: x, Y: y, Button: 1}})
}

func TestGridView(t *testing.T) {
	var st ScrollState
	var g *ScrollEl
	u := New(nil, nil, func() Element {
		g = GridView(&st, GridViewOpts{Count: 7, CellW: 50, CellH: 40, W: 160, H: 200, Current: 2,
			Highlight: func(int) Element { return Div().Bg(RGB(0xff0000)) }}, nil)
		return Div().Children(g)
	})
	u.Layout(400, 400)
	u.Layout(400, 400)
	cells := Children(Children(Children(g)[0])[0])
	if len(cells) != 7 {
		t.Fatalf("%d cells, want 7", len(cells))
	}
	// Three 50 px cells fit across 160: the fourth starts the second row.
	if r := Bounds(cells[3]); r.Y != Bounds(cells[0]).Y+40 || r.X != Bounds(cells[0]).X {
		t.Errorf("cell 3 at %+v, want the start of row two", r)
	}
}

func TestTreeView(t *testing.T) {
	var st TreeState
	roots := []TreeNode{
		{Label: "src", Children: []TreeNode{{Label: "main.go"}, {Label: "ui", Children: []TreeNode{{Label: "div.go"}}}}},
		{Label: "README.md"},
	}
	u := New(nil, nil, func() Element {
		return Div().Children(TreeView(&st, roots, TreeOpts{H: 300}).(*DivEl).FocusID("tree"))
	})
	frame := func() { u.Layout(400, 400); u.Layout(400, 400) }
	frame()
	if n := len(st.rowEls); n != 2 {
		t.Fatalf("collapsed tree shows %d rows, want 2", n)
	}
	// The arrow expands; the row click makes it current.
	clickAt(u, Bounds(Children(st.rowEls[0])[0]))
	frame()
	if n := len(st.rowEls); n != 4 || !st.Expanded("0") {
		t.Fatalf("after expanding src: %d rows, want 4", n)
	}
	vis := st.Rows(roots)
	if vis[2].Path != "0/1" || vis[2].Depth != 1 || !vis[2].HasChildren {
		t.Errorf("row 2 is %+v, want src/ui at depth 1", vis[2])
	}
	clickAt(u, Bounds(st.rowEls[3]))
	frame()
	if st.Current() != 3 {
		t.Errorf("clicked README.md: current %d, want 3", st.Current())
	}
	// The keyboard: Up to ui, Right expands it, Right again steps in,
	// Left steps out, Left collapses, Space toggles.
	u.FocusID("tree")
	key := func(k app.Key) { u.Key(app.KeyEvent{Down: true, Key: k}); frame() }
	key(app.KeyUp)
	if st.Current() != 2 {
		t.Fatalf("Up: current %d, want 2 (ui)", st.Current())
	}
	key(app.KeyRight)
	if !st.Expanded("0/1") {
		t.Error("Right did not expand ui")
	}
	key(app.KeyRight)
	if st.Current() != 3 {
		t.Errorf("Right on an open node: current %d, want 3 (div.go)", st.Current())
	}
	key(app.KeyLeft)
	if st.Current() != 2 {
		t.Errorf("Left on a leaf: current %d, want 2 (its parent)", st.Current())
	}
	key(app.KeyLeft)
	if st.Expanded("0/1") {
		t.Error("Left on an open node did not collapse it")
	}
	u.Key(app.KeyEvent{Down: true, Rune: ' '})
	frame()
	if !st.Expanded("0/1") {
		t.Error("Space did not toggle ui open")
	}
	st.Toggle("0")
	frame()
	if n := len(st.rowEls); n != 2 {
		t.Errorf("collapsed again: %d rows, want 2", n)
	}
}
