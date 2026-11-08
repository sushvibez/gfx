package ui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// TableView (QML's TableView and HorizontalHeaderView): rows and columns
// of cells with row selection, in-place editing, a header that sorts, a
// filter, virtualised rows for big tables, and the keyboard.

// TableState is a table's retained state: its scroll, the selected row,
// the cell being edited and the sort. Rows here are always MODEL rows, so
// sorting or filtering never moves the selection to another record.
type TableState struct {
	Scroll       ScrollState
	list         ListState          // the scroll of a virtualised table (RowHeight set)
	sel          core.Property[int] // selected row + 1 (0: none)
	editR, editC core.Property[int] // edited cell + 1 (0: none)
	sortCol      core.Property[int] // sorted column + 1 (0: model order)
	sortDesc     core.Property[bool]
	order        []int    // model row of each visible row, last build
	rowEls       []*DivEl // each visible row's element, last build (not virtualised)
	rowH         float32  // RowHeight of the last build
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

// SortColumn is the column the table is sorted by (-1: model order) and
// whether descending; SortBy sorts it (col -1: back to model order).
func (s *TableState) SortColumn() (col int, desc bool) { return s.sortCol.Get() - 1, s.sortDesc.Get() }
func (s *TableState) SortBy(col int, desc bool)        { s.sortCol.Set(col + 1); s.sortDesc.Set(desc) }

// VisibleRows is the model row shown at each visible position, after the
// filter and the sort (as of the last build).
func (s *TableState) VisibleRows() []int { return s.order }

// TableOpts configures a TableView.
type TableOpts struct {
	Rows, Cols int
	ColWidths  []float32 // per column; a missing one is 120
	H          float32   // 0: the table grows into what the layout leaves
	// Selectable: a click on a cell selects its row, and the keyboard
	// moves the selection while the table holds focus.
	Selectable bool
	// Cell draws one cell; nil draws Text's string in the theme, the
	// selected row tinted.
	Cell func(row, col int, selected, editing bool) Element
	Text func(row, col int) string
	// CanEdit and Editor make cells editable in place (a selectable table
	// only): a click on a cell of the selected row, or Enter, edits it,
	// Editor covering the cell until it calls StopEditing (Escape too).
	CanEdit func(row, col int) bool
	Editor  func(row, col int) Element
	// Title gives the table a header row of its own; with Sortable a click
	// on a title sorts by that column (again: the other way), ▴/▾ showing.
	Title    func(col int) string
	Sortable bool
	// Less orders two model rows by a column; nil compares Text's strings,
	// as numbers when both are.
	Less func(a, b, col int) bool
	// Filter keeps the model rows it returns true for (nil: all).
	Filter func(row int) bool
	// RowHeight, when set, makes every row that tall and builds only the
	// rows in view (ui.List), so a table of 100,000 rows costs what one
	// screenful does.
	RowHeight float32
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

// lessText orders cell strings: as numbers when both are, else by text
// ignoring case.
func lessText(a, b string) bool {
	fa, ea := strconv.ParseFloat(strings.TrimSpace(a), 64)
	fb, eb := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if ea == nil && eb == nil {
		return fa < fb
	}
	return strings.ToLower(a) < strings.ToLower(b)
}

// tableOrder is the model rows to show, filtered and sorted.
func tableOrder(st *TableState, o TableOpts) []int {
	order := make([]int, 0, o.Rows)
	for r := 0; r < o.Rows; r++ {
		if o.Filter == nil || o.Filter(r) {
			order = append(order, r)
		}
	}
	col, desc := st.SortColumn()
	if col >= 0 && col < o.Cols {
		less := o.Less
		if less == nil {
			less = func(a, b, c int) bool {
				if o.Text == nil {
					return false
				}
				return lessText(o.Text(a, c), o.Text(b, c))
			}
		}
		sort.SliceStable(order, func(i, j int) bool {
			if desc {
				return less(order[j], order[i], col)
			}
			return less(order[i], order[j], col)
		})
	}
	return order
}

// TableView draws a table of Rows×Cols cells, scrolling.
func TableView(st *TableState, o TableOpts) Element {
	widths := TableColumnWidths(o.ColWidths, o.Cols)
	tm := CurrentTheme()
	order := tableOrder(st, o)
	st.order, st.rowH = order, o.RowHeight
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
	rowEl := func(i int) *DivEl {
		r := order[i]
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
				h := o.RowHeight
				if h <= 0 {
					h = 32
				}
				d := Div().Row().H(h).PadXY(8, 0).ItemsCenter().Children(Text(text, tm.Text).Truncate())
				switch {
				case r == sel:
					d.Bg(tm.RowSelected)
				case i%2 == 1:
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
		return row
	}
	// The rows: built in full, or only those in view.
	plain := o.Title == nil && !o.Selectable // no box around the rows
	var body Element
	if o.RowHeight > 0 {
		st.rowEls = nil
		l := List(&st.list, o.RowHeight, len(order), func(i int) Element { return rowEl(i) }).AutoHideBar()
		switch {
		case !plain:
			l.Grow()
		case o.H > 0:
			l.H(o.H)
		default:
			l.Grow()
		}
		body = l
	} else {
		grid := Div().Col()
		st.rowEls = st.rowEls[:0]
		for i := range order {
			row := rowEl(i)
			st.rowEls = append(st.rowEls, row)
			grid.Children(row)
		}
		s := Scroll(&st.Scroll).AutoHideBar().Children(grid)
		switch {
		case !plain:
			s.Grow()
		case o.H > 0:
			s.H(o.H)
		default:
			s.Grow()
		}
		body = s
	}
	if plain {
		return body
	}
	// A header, or the keyboard: the table is a box around the rows.
	box := Div().Col()
	if o.H > 0 {
		box.H(o.H)
	} else {
		box.Grow()
	}
	if o.Title != nil {
		box.Children(tableHeader(st, o, widths, tm))
	}
	box.Children(body)
	if o.Selectable {
		box.Focusable().OnKeyAccept(func(ev app.KeyEvent) bool { return tableKey(st, o, ev) })
	}
	return box
}

// tableHeader is the table's own header row: the titles, sorting on click.
func tableHeader(st *TableState, o TableOpts, widths []float32, tm Theme) *DivEl {
	col, desc := st.SortColumn()
	return HeaderView(o.Cols, widths, nil, func(c int) Element {
		t := o.Title(c)
		if o.Sortable && c == col {
			if desc {
				t += " ▾"
			} else {
				t += " ▴"
			}
		}
		h := Div().Row().H(32).PadXY(8, 0).ItemsCenter().Bg(tm.Band).
			Children(Text(t, tm.BandText).Bold().Truncate())
		if o.Sortable {
			cc := c
			h.HoverBg(tm.BandHover).OnClick(func() {
				if cur, d := st.SortColumn(); cur == cc {
					st.SortBy(cc, !d)
				} else {
					st.SortBy(cc, false)
				}
			})
		}
		return h
	})
}

// tableKey moves the selection through the visible rows, starts an edit
// with Enter and ends one with Escape.
func tableKey(st *TableState, o TableOpts, ev app.KeyEvent) bool {
	if !ev.Down {
		return false
	}
	_, _, editing := st.Editing()
	if ev.Key == app.KeyEscape && editing {
		st.StopEditing()
		return true
	}
	if editing || len(st.order) == 0 {
		return false // the editor has the keys
	}
	at := -1
	for i, r := range st.order {
		if r == st.Selected() {
			at = i
		}
	}
	page := 10
	if st.rowH > 0 && st.list.ViewH() > 0 {
		page = max(int(st.list.ViewH()/st.rowH)-1, 1)
	}
	to := at
	switch ev.Key {
	case app.KeyDown:
		to = at + 1
	case app.KeyUp:
		to = at - 1
	case app.KeyHome:
		to = 0
	case app.KeyEnd:
		to = len(st.order) - 1
	case app.KeyPageDown:
		to = at + page
	case app.KeyPageUp:
		to = at - page
	case app.KeyEnter:
		if at < 0 || o.CanEdit == nil || o.Editor == nil {
			return false
		}
		for c := 0; c < o.Cols; c++ {
			if o.CanEdit(st.order[at], c) {
				st.Edit(st.order[at], c)
				return true
			}
		}
		return false
	default:
		return false
	}
	to = min(max(to, 0), len(st.order)-1)
	st.Select(st.order[to])
	tableReveal(st, to)
	return true
}

// tableReveal scrolls visible row i into view.
func tableReveal(st *TableState, i int) {
	if st.rowH > 0 {
		top := float32(i) * st.rowH
		reveal(top, top+st.rowH, st.list.Offset(), st.list.ViewH(), st.list.SetOffset)
		return
	}
	if i >= len(st.rowEls) || len(st.rowEls) == 0 {
		return
	}
	first, r := st.rowEls[0].Rect(), st.rowEls[i].Rect()
	top := r.Y - first.Y // last frame's offset of the row in the content
	reveal(top, top+r.H, st.Scroll.Y, st.Scroll.ViewH(), func(y float32) {
		st.Scroll.Y = min(max(y, 0), st.Scroll.MaxY())
	})
}

// reveal scrolls a view showing [off, off+view) so [top, bottom) is in it.
func reveal(top, bottom, off, view float32, set func(float32)) {
	if view <= 0 {
		return
	}
	switch {
	case top < off:
		set(top)
	case bottom > off+view:
		set(bottom - view)
	}
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
