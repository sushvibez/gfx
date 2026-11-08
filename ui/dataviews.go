package ui

import (
	"strconv"

	"github.com/sushvibez/gfx/core"
)

// Data views (QML's GridView, TableView with HorizontalHeaderView, and
// TreeView): a scrolling grid of fixed cells, a table of rows and columns
// with row selection and in-place editing, and a tree flattened to its
// visible rows. Each draws its cells through a function the caller gives
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

// TableState is a table's retained state: its scroll, the selected row and
// the cell being edited. Keep one per table.
type TableState struct {
	Scroll       ScrollState
	sel          core.Property[int] // selected row + 1 (0: none)
	editR, editC core.Property[int] // edited cell + 1 (0: none)
}

// Selected is the selected row, or -1; Select selects row (-1: none).
func (s *TableState) Selected() int  { return s.sel.Get() - 1 }
func (s *TableState) Select(row int) { s.sel.Set(row + 1) }

// Editing is the cell being edited (ok false: none); Edit starts editing a
// cell and StopEditing ends it (an editor calls it once it commits).
func (s *TableState) Editing() (row, col int, ok bool) {
	return s.editR.Get() - 1, s.editC.Get() - 1, s.editR.Get() > 0
}
func (s *TableState) Edit(row, col int) { s.editR.Set(row + 1); s.editC.Set(col + 1) }
func (s *TableState) StopEditing()      { s.editR.Set(0); s.editC.Set(0) }

// TableOpts configures a TableView.
type TableOpts struct {
	Rows, Cols int
	ColWidths  []float32 // per column; a missing one is 120
	H          float32   // 0: the table grows into what the layout leaves
	// Selectable: a click on a cell selects its row.
	Selectable bool
	// Cell draws one cell; nil draws Text's string in the theme, the
	// selected row tinted.
	Cell func(row, col int, selected, editing bool) Element
	Text func(row, col int) string
	// CanEdit and Editor make cells editable in place (a selectable table
	// only): a click on a cell of the selected row edits it, Editor
	// covering the cell until it calls StopEditing.
	CanEdit func(row, col int) bool
	Editor  func(row, col int) Element
}

// TableColumnWidths is the width of each of cols columns: widths where
// given, 120 for the rest.
func TableColumnWidths(widths []float32, cols int) []float32 {
	out := make([]float32, cols)
	for c := range out {
		out[c] = 120
		if c < len(widths) && widths[c] > 0 {
			out[c] = widths[c]
		}
	}
	return out
}

// TableView draws a table of Rows×Cols cells, scrolling.
func TableView(st *TableState, o TableOpts) *ScrollEl {
	widths := TableColumnWidths(o.ColWidths, o.Cols)
	tm := CurrentTheme()
	sel := st.Selected()
	if sel >= o.Rows {
		sel = -1 // the rows shrank under the selection
		st.Select(-1)
	}
	er, ec, editing := st.Editing()
	if editing && (er >= o.Rows || ec >= o.Cols) {
		editing = false
		st.StopEditing()
	}
	grid := Div().Col()
	for r := 0; r < o.Rows; r++ {
		row := Div().Row()
		for c := 0; c < o.Cols; c++ {
			isEditing := editing && r == er && c == ec
			var el Element
			if o.Cell != nil {
				el = o.Cell(r, c, r == sel, isEditing)
			} else {
				text := ""
				if o.Text != nil {
					text = o.Text(r, c)
				}
				d := Div().Row().H(32).PadXY(8, 0).ItemsCenter().Children(Text(text, tm.Text).Truncate())
				switch {
				case r == sel:
					d.Bg(tm.RowSelected)
				case r%2 == 1:
					d.Bg(tm.Window)
				default:
					d.Bg(tm.Field)
				}
				el = d
			}
			if el == nil {
				row.Children(Div().W(widths[c]))
				continue
			}
			// A cell is its column's width, whatever its own size says.
			if d, ok := el.(*DivEl); ok {
				d.W(widths[c])
			}
			box := Div().W(widths[c])
			var ed Element
			if isEditing && o.Editor != nil {
				ed = o.Editor(r, c)
			}
			if ed != nil {
				// The editor covers the cell.
				box.AnchorMode().Children(
					Anchored(el, "", Anchors{Fill: "parent"}),
					Anchored(ed, "", Anchors{Fill: "parent"}))
			} else {
				box.Children(el)
			}
			if o.Selectable {
				rr, cc, wasSelected := r, c, r == sel
				canEdit := o.CanEdit != nil && o.Editor != nil && o.CanEdit(r, c)
				box.OnClick(func() {
					st.Select(rr)
					// A second click on the selected row edits the cell.
					if canEdit && wasSelected {
						st.Edit(rr, cc)
					}
				})
			}
			row.Children(box)
		}
		grid.Children(row)
	}
	s := Scroll(&st.Scroll).AutoHideBar().Children(grid)
	if o.H > 0 {
		s.H(o.H)
	} else {
		s.Grow()
	}
	return s
}

// HeaderView is a table's column headers (QML's HorizontalHeaderView): a
// strip of cols cells at the table's column widths; header draws one (nil
// or a nil result: the title in a theme band).
func HeaderView(cols int, widths []float32, title func(c int) string, header func(c int) Element) *DivEl {
	widths = TableColumnWidths(widths, cols)
	tm := CurrentTheme()
	strip := Div().Row()
	for c := 0; c < cols; c++ {
		var el Element
		if header != nil {
			el = header(c)
		}
		if el == nil {
			t := ""
			if title != nil {
				t = title(c)
			}
			el = Div().Row().H(32).PadXY(8, 0).ItemsCenter().Bg(tm.Band).
				Children(Text(t, tm.BandText).Bold().Truncate())
		}
		if d, ok := el.(*DivEl); ok {
			d.W(widths[c])
		}
		strip.Children(Div().W(widths[c]).Children(el))
	}
	return strip
}

// TreeNode is one node of a TreeView's data.
type TreeNode struct {
	Label    string
	Data     any // the caller's own (a gml node's roles)
	Children []TreeNode
}

// TreeRow is one visible row of a tree: its node, its place and its state.
type TreeRow struct {
	Node        *TreeNode
	Path        string // the child-index chain from the root ("0/2/1"), its identity
	Depth       int
	Index       int // its row among the visible rows
	HasChildren bool
	Expanded    bool
}

// TreeState is a tree's retained state: its scroll, which nodes are
// expanded (by path) and the current row. Keep one per tree.
type TreeState struct {
	Scroll ScrollState
	exp    map[string]*core.Property[bool]
	cur    core.Property[int] // current row + 1 (0: none)
}

func (s *TreeState) expProp(path string) *core.Property[bool] {
	if s.exp == nil {
		s.exp = map[string]*core.Property[bool]{}
	}
	p := s.exp[path]
	if p == nil {
		p = &core.Property[bool]{}
		s.exp[path] = p
	}
	return p
}

// Expanded reports whether the node at path is expanded; SetExpanded and
// Toggle change it.
func (s *TreeState) Expanded(path string) bool          { return s.expProp(path).Get() }
func (s *TreeState) SetExpanded(path string, open bool) { s.expProp(path).Set(open) }
func (s *TreeState) Toggle(path string)                 { p := s.expProp(path); p.Set(!p.Get()) }

// Current is the current row (-1: none); SetCurrent sets it.
func (s *TreeState) Current() int     { return s.cur.Get() - 1 }
func (s *TreeState) SetCurrent(i int) { s.cur.Set(i + 1) }

// Rows flattens roots to the rows now visible: depth first, a node's
// children only under it while it is expanded.
func (s *TreeState) Rows(roots []TreeNode) []TreeRow {
	var out []TreeRow
	var walk func(nodes []TreeNode, parent string, depth int)
	walk = func(nodes []TreeNode, parent string, depth int) {
		for i := range nodes {
			n := &nodes[i]
			path := parent + strconv.Itoa(i)
			open := s.Expanded(path)
			out = append(out, TreeRow{Node: n, Path: path, Depth: depth, Index: len(out),
				HasChildren: len(n.Children) > 0, Expanded: open})
			if open && len(n.Children) > 0 {
				walk(n.Children, path+"/", depth+1)
			}
		}
	}
	walk(roots, "", 0)
	return out
}

// TreeOpts configures a TreeView.
type TreeOpts struct {
	H float32 // 0: as tall as its rows
	// Row draws one visible row; nil draws the label indented by depth
	// behind a ▸/▾ that toggles the node, a click making it current.
	Row func(r TreeRow, current bool) Element
}

// TreeView draws the visible rows of a tree, scrolling.
func TreeView(st *TreeState, roots []TreeNode, o TreeOpts) *ScrollEl {
	tm := CurrentTheme()
	cur := st.Current()
	col := Div().Col()
	for _, r := range st.Rows(roots) {
		if o.Row != nil {
			col.Children(o.Row(r, r.Index == cur))
			continue
		}
		path, idx := r.Path, r.Index
		arrow := Div().Size(16, 28).JustifyCenter().ItemsCenter() // the whole row tall: easy to hit
		if r.HasChildren {
			glyph := "▸"
			if r.Expanded {
				glyph = "▾"
			}
			arrow.Children(Text(glyph, tm.Dim)).OnClick(func() { st.Toggle(path) })
		}
		row := Div().Row().H(28).ItemsCenter().Gap(4).Padding(0, 8, 0, 4+16*float32(r.Depth)).
			OnClick(func() { st.SetCurrent(idx) }).
			Children(arrow, Text(r.Node.Label, tm.Text).Truncate())
		if r.Index == cur {
			row.Bg(tm.RowSelected)
		} else {
			row.HoverBg(tm.RowHover)
		}
		col.Children(row)
	}
	s := Scroll(&st.Scroll).AutoHideBar().Children(col)
	if o.H > 0 {
		s.H(o.H)
	}
	return s
}
