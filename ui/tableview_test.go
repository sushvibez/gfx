package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sushvibez/gfx/app"
)

func TestTableViewSelectAndEdit(t *testing.T) {
	var st TableState
	data := [][]string{{"Ada", "36"}, {"Bob", "41"}, {"Cy", "29"}}
	edited := false
	u := New(nil, nil, func() Element {
		tv := TableView(&st, TableOpts{Rows: 3, Cols: 2, ColWidths: []float32{100, 60}, H: 150, Selectable: true,
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
	cell := func(r, c int) Rect { return Bounds(Children(st.rowEls[r])[c]) }
	if w := cell(0, 1).W; w != 60 {
		t.Errorf("column 1 is %v wide, want 60", w)
	}
	clickAt(u, cell(1, 1))
	frame()
	if st.Selected() != 1 {
		t.Fatalf("clicked row 1: selected %d", st.Selected())
	}
	if c, _ := FillColor(Children(Children(st.rowEls[1])[0])[0]); c != CurrentTheme().RowSelected {
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

func TestTableViewSortFilterKeys(t *testing.T) {
	var st TableState
	data := [][]string{{"Cy", "29"}, {"ada", "36"}, {"Bob", "100"}, {"Dee", "7"}}
	hide := ""
	u := New(nil, nil, func() Element {
		return Div().Children(TableView(&st, TableOpts{Rows: len(data), Cols: 2, H: 200, Selectable: true, Sortable: true,
			Title:   func(c int) string { return []string{"Name", "Age"}[c] },
			Text:    func(r, c int) string { return data[r][c] },
			Filter:  func(r int) bool { return hide == "" || data[r][0] != hide },
			CanEdit: func(r, c int) bool { return true },
			Editor:  func(r, c int) Element { return Div() },
		}).(*DivEl).FocusID("table"))
	})
	frame := func() { u.Layout(400, 400); u.Layout(400, 400) }
	frame()
	// By age: numbers compare as numbers (7 < 29 < 36 < 100).
	st.SortBy(1, false)
	frame()
	if got := fmt.Sprint(st.VisibleRows()); got != "[3 0 1 2]" {
		t.Errorf("by age: %s, want [3 0 1 2]", got)
	}
	st.SortBy(0, true) // by name, descending, ignoring case
	frame()
	if got := fmt.Sprint(st.VisibleRows()); got != "[3 0 2 1]" {
		t.Errorf("by name descending: %s, want [3 0 2 1]", got)
	}
	// The selection is a model row: it stays on the record as the order
	// changes, and the keyboard walks the visible order.
	st.Select(2) // Bob, third
	frame()
	u.FocusID("table")
	u.Key(app.KeyEvent{Down: true, Key: app.KeyDown})
	frame()
	if st.Selected() != 1 {
		t.Errorf("Down from Bob selected %d, want 1 (ada, fourth)", st.Selected())
	}
	u.Key(app.KeyEvent{Down: true, Key: app.KeyHome})
	frame()
	if st.Selected() != 3 {
		t.Errorf("Home selected %d, want 3 (Dee)", st.Selected())
	}
	u.Key(app.KeyEvent{Down: true, Key: app.KeyEnter})
	frame()
	if r, c, ok := st.Editing(); !ok || r != 3 || c != 0 {
		t.Errorf("Enter edits %d,%d %v; want 3,0", r, c, ok)
	}
	u.Key(app.KeyEvent{Down: true, Key: app.KeyEscape})
	frame()
	if _, _, ok := st.Editing(); ok {
		t.Error("Escape did not stop editing")
	}
	// The filter hides a record.
	hide = "Cy"
	st.SortBy(-1, false)
	frame()
	if got := fmt.Sprint(st.VisibleRows()); got != "[1 2 3]" {
		t.Errorf("filtered: %s, want [1 2 3]", got)
	}
}

func TestTableViewHeaderSorts(t *testing.T) {
	var st TableState
	var tv Element
	u := New(nil, nil, func() Element {
		tv = TableView(&st, TableOpts{Rows: 2, Cols: 2, H: 120, Sortable: true,
			Title: func(c int) string { return []string{"A", "B"}[c] },
			Text:  func(r, c int) string { return "x" }})
		return Div().Children(tv)
	})
	frame := func() { u.Layout(400, 400); u.Layout(400, 400) }
	frame()
	b := Bounds(Children(Children(tv)[0])[1]) // the table's own header, column B
	clickAt(u, b)
	frame()
	if c, d := st.SortColumn(); c != 1 || d {
		t.Errorf("clicked B: sorted by %d desc %v, want 1 ascending", c, d)
	}
	clickAt(u, b)
	frame()
	if c, d := st.SortColumn(); c != 1 || !d {
		t.Errorf("clicked B again: sorted by %d desc %v, want 1 descending", c, d)
	}
	title := Children(Children(Children(Children(tv)[0])[1])[0])[0]
	if got, _ := TextOf(title); !strings.Contains(got, "▾") {
		t.Errorf("B's title %q has no ▾", got)
	}
}

func TestTableViewVirtual(t *testing.T) {
	var st TableState
	built := 0
	u := New(nil, nil, func() Element {
		return Div().Children(TableView(&st, TableOpts{Rows: 100000, Cols: 3, H: 300, RowHeight: 30, Selectable: true,
			Text: func(r, c int) string { built++; return fmt.Sprint(r, ":", c) }}).(*DivEl).FocusID("big"))
	})
	frame := func() { u.Layout(500, 400); u.Layout(500, 400) }
	frame()
	if built > 3*40*2 {
		t.Errorf("a 100,000-row table built %d cells, want only the rows in view", built)
	}
	// End jumps to the last row and scrolls it into view.
	u.FocusID("big")
	u.Key(app.KeyEvent{Down: true, Key: app.KeyEnd})
	frame()
	if st.Selected() != 99999 {
		t.Fatalf("End selected %d", st.Selected())
	}
	if off := st.list.Offset(); off < 99999*30-300 {
		t.Errorf("after End the table is scrolled to %v, want the last row in view", off)
	}
}
