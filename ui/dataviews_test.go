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

func TestTableViewSelectAndEdit(t *testing.T) {
	var st TableState
	data := [][]string{{"Ada", "36"}, {"Bob", "41"}, {"Cy", "29"}}
	var tv *ScrollEl
	edited := false
	u := New(nil, nil, func() Element {
		tv = TableView(&st, TableOpts{Rows: 3, Cols: 2, ColWidths: []float32{100, 60}, H: 150, Selectable: true,
			Text:    func(r, c int) string { return data[r][c] },
			CanEdit: func(r, c int) bool { return c == 0 },
			Editor: func(r, c int) Element {
				edited = true
				return Div().Bg(RGB(0xffff00))
			}})
		return Div().Col().Children(HeaderView(2, []float32{100, 60}, func(c int) string { return []string{"Name", "Age"}[c] }, nil), tv)
	})
	frame := func() { u.Layout(400, 400); u.Layout(400, 400) }
	frame()
	rows := Children(Children(Children(tv)[0])[0])
	cell := func(r, c int) Rect { return Bounds(Children(rows[r])[c]) }
	if w := cell(0, 1).W; w != 60 {
		t.Errorf("column 1 is %v wide, want 60", w)
	}
	clickAt(u, cell(1, 1))
	frame()
	if st.Selected() != 1 {
		t.Fatalf("clicked row 1: selected %d", st.Selected())
	}
	rows = Children(Children(Children(tv)[0])[0])
	if c, _ := FillColor(Children(Children(rows[1])[0])[0]); c != CurrentTheme().RowSelected {
		t.Errorf("selected row drawn %v, want the selection colour", c)
	}
	// A click on an editable cell of the selected row edits it.
	clickAt(u, cell(1, 0))
	frame()
	if r, c, ok := st.Editing(); !ok || r != 1 || c != 0 || !edited {
		t.Errorf("editing %d,%d %v (editor built %v), want 1,0", r, c, ok, edited)
	}
	st.StopEditing()
	frame()
	if _, _, ok := st.Editing(); ok {
		t.Error("still editing after StopEditing")
	}
}

func TestTreeView(t *testing.T) {
	var st TreeState
	roots := []TreeNode{
		{Label: "src", Children: []TreeNode{{Label: "main.go"}, {Label: "ui", Children: []TreeNode{{Label: "div.go"}}}}},
		{Label: "README.md"},
	}
	var tree *ScrollEl
	u := New(nil, nil, func() Element {
		tree = TreeView(&st, roots, TreeOpts{H: 300})
		return Div().Children(tree)
	})
	frame := func() { u.Layout(400, 400); u.Layout(400, 400) }
	frame()
	rows := func() []Element { return Children(Children(Children(tree)[0])[0]) }
	if n := len(rows()); n != 2 {
		t.Fatalf("collapsed tree shows %d rows, want 2", n)
	}
	// The arrow expands; the row click makes it current.
	clickAt(u, Bounds(Children(rows()[0])[0]))
	frame()
	if n := len(rows()); n != 4 || !st.Expanded("0") {
		t.Fatalf("after expanding src: %d rows, want 4", n)
	}
	vis := st.Rows(roots)
	if vis[2].Path != "0/1" || vis[2].Depth != 1 || !vis[2].HasChildren {
		t.Errorf("row 2 is %+v, want src/ui at depth 1", vis[2])
	}
	clickAt(u, Bounds(rows()[3]))
	frame()
	if st.Current() != 3 {
		t.Errorf("clicked README.md: current %d, want 3", st.Current())
	}
	st.Toggle("0")
	frame()
	if n := len(rows()); n != 2 {
		t.Errorf("collapsed again: %d rows, want 2", n)
	}
}
