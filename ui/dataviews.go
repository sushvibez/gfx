package ui

import (
	"strconv"
)

// Data views (QML's GridView, TableView with HorizontalHeaderView, and
// TreeView): a scrolling grid of fixed cells here, a table of rows and
// columns (tableview.go) and a tree flattened to its visible rows
// (treeview.go). Each draws its cells through a function the caller gives
// (or plain text in the theme), and keeps what lives across frames in a
// state struct. ListView is List.

// GridViewOpts configures a GridView.
type GridViewOpts struct {
	Count        int
	CellW, CellH float32
	W, H         float32 // 0: as the layout gives it
	// Current is the current cell (-1: none); Highlight draws behind it.
	Current   int
	Highlight func(i int) Element
}

// GridView lays Count cells of CellW×CellH out in rows as wide as the view,
// scrolling down; cell draws cell i (nil: its index).
func GridView(st *ScrollState, o GridViewOpts, cell func(i int) Element) *ScrollEl {
	s := Scroll(st).AutoHideBar()
	if o.W > 0 {
		s.W(o.W)
	}
	if o.H > 0 {
		s.H(o.H)
	}
	tm := CurrentTheme()
	// One wrapping row: how many fit across is decided at layout, from the
	// width the view is given.
	cells := Div().Row().Wrap()
	for i := 0; i < o.Count; i++ {
		var el Element
		if cell != nil {
			el = cell(i)
		} else {
			el = Div().ItemsCenter().JustifyCenter().Children(Text(strconv.Itoa(i), tm.Text))
		}
		if i == o.Current && o.Highlight != nil {
			// The stack is the cell: an anchor container has no size of
			// its own, and a sizeless one would collapse the cell.
			el = Div().Size(o.CellW, o.CellH).AnchorMode().Children(
				Anchored(o.Highlight(i), "", Anchors{Fill: "parent"}),
				Anchored(el, "", Anchors{Fill: "parent"}))
		}
		cells.Children(Div().Size(o.CellW, o.CellH).Children(el))
	}
	return s.Children(cells)
}
