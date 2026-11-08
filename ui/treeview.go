package ui

import (
	"strconv"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// TreeView (QML's TreeView): a tree flattened to its visible rows, with
// expand/collapse by path, a current row, virtualised rows for big trees,
// and the keyboard.

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
	list   ListState // the scroll of a virtualised tree (RowHeight set)
	exp    map[string]*core.Property[bool]
	cur    core.Property[int] // current row + 1 (0: none)
	rows   []TreeRow          // the visible rows, last build
	rowEls []Element          // their elements (not virtualised)
	rowH   float32
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
	// OnCurrent runs when the keyboard moves the current row (nil: the
	// state's own current row is set) — for a host keeping it elsewhere.
	OnCurrent func(i int)
	// RowHeight, when set, makes every row that tall and builds only the
	// rows in view (ui.List).
	RowHeight float32
}

// TreeView draws the visible rows of a tree, scrolling. While it holds
// focus, Up/Down move the current row, Right expands it (or steps into an
// expanded one), Left collapses it (or steps out to its parent), and
// Enter or Space toggles it.
func TreeView(st *TreeState, roots []TreeNode, o TreeOpts) Element {
	tm := CurrentTheme()
	cur := st.Current()
	rows := st.Rows(roots)
	st.rows, st.rowH = rows, o.RowHeight
	rowEl := func(r TreeRow) Element {
		if o.Row != nil {
			return o.Row(r, r.Index == cur)
		}
		path, idx := r.Path, r.Index
		h := o.RowHeight
		if h <= 0 {
			h = 28
		}
		arrow := Div().Size(16, h).JustifyCenter().ItemsCenter() // the whole row tall: easy to hit
		if r.HasChildren {
			glyph := "▸"
			if r.Expanded {
				glyph = "▾"
			}
			arrow.Children(Text(glyph, tm.Dim)).OnClick(func() { st.Toggle(path) })
		}
		exp := int8(-1)
		if r.HasChildren {
			exp = checkState(r.Expanded)
		}
		row := Div().Row().H(h).ItemsCenter().Gap(4).Padding(0, 8, 0, 4+16*float32(r.Depth)).
			Access(Access{Role: RoleTreeItem, Name: r.Node.Label, Selected: r.Index == cur, Checked: -1, Expanded: exp, Level: r.Depth + 1}).
			OnClick(func() { st.SetCurrent(idx) }).
			Children(arrow, Text(r.Node.Label, tm.Text).Truncate())
		if r.Index == cur {
			row.Bg(tm.RowSelected)
		} else {
			row.HoverBg(tm.RowHover)
		}
		return row
	}
	var body Element
	if o.RowHeight > 0 {
		st.rowEls = nil
		l := List(&st.list, o.RowHeight, len(rows), func(i int) Element { return rowEl(rows[i]) }).AutoHideBar()
		if o.H > 0 {
			l.H(o.H)
		} else {
			l.Grow()
		}
		body = l
	} else {
		col := Div().Col()
		st.rowEls = st.rowEls[:0]
		for _, r := range rows {
			el := rowEl(r)
			st.rowEls = append(st.rowEls, el)
			col.Children(el)
		}
		s := Scroll(&st.Scroll).AutoHideBar().Children(col)
		if o.H > 0 {
			s.H(o.H)
		}
		body = s
	}
	return Div().Col().Role(RoleTree, "").Focusable().OnKeyAccept(func(ev app.KeyEvent) bool { return treeKey(st, o, ev) }).
		Children(body)
}

// treeKey walks the tree from the keyboard.
func treeKey(st *TreeState, o TreeOpts, ev app.KeyEvent) bool {
	if !ev.Down || len(st.rows) == 0 {
		return false
	}
	at := min(st.Current(), len(st.rows)-1)
	set := func(i int) {
		i = min(max(i, 0), len(st.rows)-1)
		if o.OnCurrent != nil {
			o.OnCurrent(i)
		} else {
			st.SetCurrent(i)
		}
		treeReveal(st, i)
	}
	key := ev.Key
	if ev.Rune == ' ' {
		key = app.KeyEnter // Space toggles as Enter does
	}
	switch key {
	case app.KeyDown:
		set(at + 1)
	case app.KeyUp:
		if at < 0 {
			at = 1
		}
		set(at - 1)
	case app.KeyHome:
		set(0)
	case app.KeyEnd:
		set(len(st.rows) - 1)
	case app.KeyRight:
		if at < 0 {
			return false
		}
		r := st.rows[at]
		switch {
		case r.HasChildren && !r.Expanded:
			st.SetExpanded(r.Path, true)
		case r.HasChildren:
			set(at + 1) // into its first child
		}
	case app.KeyLeft:
		if at < 0 {
			return false
		}
		r := st.rows[at]
		if r.HasChildren && r.Expanded {
			st.SetExpanded(r.Path, false)
			break
		}
		for i := at - 1; i >= 0; i-- { // out to its parent
			if st.rows[i].Depth == r.Depth-1 {
				set(i)
				break
			}
		}
	case app.KeyEnter:
		if at < 0 || !st.rows[at].HasChildren {
			return false
		}
		st.Toggle(st.rows[at].Path)
	default:
		return false
	}
	return true
}

// treeReveal scrolls visible row i into view.
func treeReveal(st *TreeState, i int) {
	if st.rowH > 0 {
		top := float32(i) * st.rowH
		reveal(top, top+st.rowH, st.list.Offset(), st.list.ViewH(), st.list.SetOffset)
		return
	}
	if i >= len(st.rowEls) || len(st.rowEls) == 0 {
		return
	}
	first, r := Bounds(st.rowEls[0]), Bounds(st.rowEls[i])
	top := r.Y - first.Y
	reveal(top, top+r.H, st.Scroll.Y, st.Scroll.ViewH(), func(y float32) {
		st.Scroll.Y = min(max(y, 0), st.Scroll.MaxY())
	})
}
