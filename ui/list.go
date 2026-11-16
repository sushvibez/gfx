package ui

import "unsafe"

// ListState is the retained scroll position of a virtualized list, plus
// what it has learned about its rows: a QML delegate sizes itself off its
// own data (a chat bubble is as tall as its message), so a row's height is
// only known once that row has been built. rows keeps every known height
// (given by Heights or measured) and survives the frame, which is what
// keeps a variable-height list from laying every row out at the first
// one's height; it is an index (see rowIndex), so a layout asks it for the
// content length, the visible range and a row's position in O(log count).
//
// Measured heights are kept by row index. When the model inserts or
// removes rows anywhere but the end, tell the state (InsertRows,
// RemoveRows) so the heights stay with their rows; rows added or dropped
// at the end need nothing.
type ListState struct {
	scroll ScrollState
	rows   rowIndex

	// What the last layout synchronised rows with: how many leading rows
	// came from Heights, whether any row past them was measured, and the
	// slice a HeightsRev list vouched for.
	explicit int
	measured bool
	revData  *float32
	revLen   int
	rev      uint64
	revOK    bool

	// The last layout's geometry, for RowTop and ScrollToRow.
	variable bool
	horiz    bool
	est, gap float32
	head     float32
	count    int
}

// InsertRows tells a Measured list that the model gained n rows at index
// at: the heights measured for the rows from there on move down with them
// and the new rows are measured when they are first built. It costs
// O(rows); a list that is never told keeps heights by position, which is
// right only for rows appended at the end.
func (s *ListState) InsertRows(at, n int) {
	if n <= 0 || at < 0 || at >= s.rows.len() {
		return // past the end: the next layout appends them
	}
	s.rows.insert(at, n)
	s.revOK = false
}

// RemoveRows is InsertRows for n rows removed at index at.
func (s *ListState) RemoveRows(at, n int) {
	if at < 0 || at >= s.rows.len() {
		return
	}
	n = min(n, s.rows.len()-at)
	if n <= 0 {
		return
	}
	s.rows.remove(at, n)
	s.revOK = false
}

// InvalidateRow forgets row i's measured height, so it is the estimate
// until the row is built again. A row is re-measured whenever it is built,
// so this is only for rows that changed while out of view.
func (s *ListState) InvalidateRow(i int) {
	if i >= s.explicit && i < s.rows.len() {
		s.rows.set(i, unknownRow)
	}
}

// ResetRows forgets every measured height (a different model).
func (s *ListState) ResetRows() {
	s.rows.forget(0, s.rows.len())
	s.explicit, s.measured, s.revOK = 0, false, false
}

// RowTop is where row i starts within the list's content (0 is the
// content's start, so a header's length is included) and how long the row
// is, as of the last layout; ok is false for a row the list did not have.
// It is O(log count) for a variable-height list.
func (s *ListState) RowTop(i int) (top, size float32, ok bool) {
	if i < 0 || i >= s.count {
		return 0, 0, false
	}
	if !s.variable || i >= s.rows.len() {
		return s.head + float32(i)*(s.est+s.gap), s.est, true
	}
	return s.head + float32(s.rows.top(i, s.est, s.gap)), s.rows.size(i, s.est), true
}

// ScrollToRow scrolls the least distance that brings row i wholly into
// view (its start, when the row is longer than the view), using the last
// layout's geometry.
func (s *ListState) ScrollToRow(i int) {
	top, size, ok := s.RowTop(i)
	if !ok {
		return
	}
	off, view, limit := &s.scroll.Y, s.scroll.viewH, s.scroll.maxY()
	if s.horiz {
		off, view, limit = &s.scroll.X, s.scroll.viewW, s.scroll.maxX()
	}
	switch {
	case top < *off || size > view:
		*off = top
	case top+size > *off+view:
		*off = top + size - view
	}
	*off = min(max(*off, 0), limit)
}

// Offset is the current scroll offset in pixels (0 = top).
func (s *ListState) Offset() float32 { return s.scroll.Y }

// SetOffset scrolls to y, clamped to the content (app-drawn scrollbars).
func (s *ListState) SetOffset(y float32) {
	s.scroll.Y = min(max(y, 0), s.scroll.maxY())
}

// MaxY exposes the scroll range; ViewH the viewport height (last layout).
func (s *ListState) MaxY() float32  { return s.scroll.maxY() }
func (s *ListState) ViewH() float32 { return s.scroll.viewH }

// List builds a vertically scrolling, virtualized list of count rows, each
// rowHeight pixels tall. Only the rows visible in the viewport are built
// and rendered each frame — build is called O(visible) times, not O(count)
// — so a million-row list costs the same as a ten-row one. This is the
// QML ListView / GPUI uniform_list equivalent.
func List(state *ListState, rowHeight float32, count int, build func(i int) Element) *ListEl {
	// Rows are built during layout, after the tree around the list: they
	// keep the theme scopes the list itself was built in.
	if snap := themeSnapshot(); len(snap) > 0 {
		inner := build
		build = func(i int) (el Element) {
			withThemeScopes(snap, func() { el = inner(i) })
			return el
		}
	}
	return &ListEl{st: state, rowH: rowHeight, count: count, build: build}
}

// Spacing leaves a gap between rows (QML's ListView.spacing): the PITCH
// grows by it while each row keeps its own height, so a delegate's
// background stays the size the markup gave it.
func (e *ListEl) Spacing(px float32) *ListEl { e.gap = max(px, 0); return e }

// Heights gives the rows individual heights instead of one uniform row
// height — what a sectioned list needs, where a section header is a row of
// its own and taller (or shorter) than the items under it. heights[i] is
// row i's height; rows past the end of a short slice take the uniform row
// height (or the measured one, with Measured), and entries past the row
// count are unused, so a slice that lags the model by a frame still lays
// out every row.
func (e *ListEl) Heights(heights []float32) *ListEl {
	e.heights = heights
	return e
}

// HeightsRev is Heights for a long list. Heights compares the slice with
// what the list already knows on every layout, a scan of the whole slice;
// here rev vouches for the contents instead: while the same slice arrives
// with the same rev the scan is skipped and the layout is O(log count).
// Change rev whenever an element changes (a new slice is noticed on its
// own).
func (e *ListEl) HeightsRev(heights []float32, rev uint64) *ListEl {
	e.heights, e.rev, e.hasRev = heights, rev, true
	return e
}

// SetRowHeight tells the state that row i of a HeightsRev list is now h
// long, so one row can change without a new rev and the scan that follows
// it: write heights[i], call this, keep the rev. It does nothing for a row
// the last layout's heights did not cover.
func (s *ListState) SetRowHeight(i int, h float32) {
	if i >= 0 && i < s.explicit && i < s.rows.len() {
		s.rows.set(i, h)
	}
}

// variable reports rows of differing sizes: the list then keeps them in
// its state's index instead of using arithmetic on the uniform height.
func (e *ListEl) variable() bool { return e.heights != nil || e.measure_ }

// syncRows brings the state's row index in line with this frame's list:
// the row count, the heights given by Heights, and which of the remaining
// rows may keep a measured height.
func (e *ListEl) syncRows() {
	st := e.st
	ix := &st.rows
	ix.resize(e.count)
	n := min(len(e.heights), e.count)
	// Rows the slice no longer covers go back to the estimate, and so do
	// measured rows once the list stops measuring.
	if n < st.explicit {
		ix.forget(n, st.explicit)
	}
	if st.measured && !e.measure_ {
		ix.forget(n, e.count)
		st.measured = false
	}
	st.explicit = n
	if n == 0 {
		st.revOK = false
		return
	}
	data := unsafe.SliceData(e.heights)
	if e.hasRev && st.revOK && st.revData == data && st.revLen == n && st.rev == e.rev {
		return
	}
	ix.setPrefix(e.heights[:n])
	st.revData, st.revLen, st.rev, st.revOK = data, n, e.rev, e.hasRev
}

// rowHeightAt is row i's height: the one given by Heights, the one measured when
// the row was last built, else the uniform estimate. For a variable list
// it reads the state's index, so it is valid once layout has synchronised
// it.
func (e *ListEl) rowHeightAt(i int) float32 {
	if !e.variable() {
		return e.rowH
	}
	return e.st.rows.size(i, e.rowH)
}

// rowTop is where row i starts and how tall it is, honouring per-row
// heights when they were given (valid once layout has run, as rowHeightAt).
func (e *ListEl) rowTop(i int) (top, h float32) {
	if !e.variable() {
		return float32(i) * (e.rowH + e.gap), e.rowH
	}
	return float32(e.st.rows.top(i, e.rowH, e.gap)), e.st.rows.size(i, e.rowH)
}

// contentHeight is the whole list's height, however the rows are sized.
func (e *ListEl) contentHeight() float32 {
	if e.count == 0 {
		return 0
	}
	if !e.variable() {
		return float32(e.count)*(e.rowH+e.gap) - e.gap
	}
	return max(float32(e.st.rows.top(e.count, e.rowH, e.gap)-float64(e.gap)), 0)
}

// ListEl is the per-frame element of a ListState.
type ListEl struct {
	st      *ListState
	rowH    float32
	gap     float32   // space between rows; the pitch is rowH + gap
	heights []float32 // per-row heights (sections); nil = uniform rowH
	rev     uint64    // names the contents of heights (see HeightsRev)
	hasRev  bool
	count   int
	build   func(i int) Element
	w, h    float32

	grow     float32
	rect     Rect
	rows     []Element // built visible rows
	first    int       // index of rows[0]
	barC     RGBA
	hasBarC  bool
	autoHide bool // the bar shows only while the list moves (QML views)
	bottomUp bool // index 0 at the BOTTOM (see BottomUp)
	measure_ bool // rows carry their own height (see Measured)
	horiz    bool // rows run left to right (see Horizontal)

	// header and footer scroll with the rows, before the first and after
	// the last (see Header); their sizes along the list, this layout.
	header, footer Element
	headSz, footSz float32
}

// Measured lets each row be as tall as it builds itself — QML's ListView,
// where the DELEGATE's height is the row height and a delegate bound to its
// own data (a chat bubble as tall as its message) differs row by row. The
// height given to List stays the estimate for rows that have not been built
// yet, so virtualization is unaffected.
func (e *ListEl) Measured() *ListEl { e.measure_ = true; return e }

// AutoHideBar shows the scrollbar only while the list is moving — see
// ScrollEl.AutoHideBar.
func (e *ListEl) AutoHideBar() *ListEl { e.autoHide = true; return e }

// BottomUp stacks the rows from the bottom of the view upward, with index 0
// at the BOTTOM — QML's `verticalLayoutDirection: ListView.BottomToTop`,
// which is how a chat log is written: the model appends and the newest
// message appears at the bottom. A list shorter than its view sits at the
// bottom of it rather than the top.
func (e *ListEl) BottomUp() *ListEl { e.bottomUp = true; return e }

// Horizontal lays the rows out left to right and scrolls sideways (QML's
// `orientation: ListView.Horizontal`): a row's height (List's rowHeight,
// Heights, a Measured row) is then its WIDTH, every row is as tall as the
// list unless it measures shorter, and the wheel scrolls it sideways, as
// Qt's does. BottomUp mirrors it right to left.
func (e *ListEl) Horizontal() *ListEl { e.horiz = true; return e }

// Header and Footer put an element before the first row and after the
// last (QML's ListView.header / footer): part of the content, so they
// scroll away with the rows. Each is as tall (wide, for a Horizontal
// list) as it measures.
func (e *ListEl) Header(el Element) *ListEl { e.header = el; return e }
func (e *ListEl) Footer(el Element) *ListEl { e.footer = el; return e }

func (e *ListEl) W(px float32) *ListEl { e.w = px; return e }
func (e *ListEl) H(px float32) *ListEl { e.h = px; return e }

// Grow makes the list flex into its parent's leftover space (flex_1).
func (e *ListEl) Grow() *ListEl { e.grow = 1; return e }

// GrowBy is Grow with a weight; see DivEl.GrowBy.
func (e *ListEl) GrowBy(f float32) *ListEl { e.grow = f; return e }

// Bounds exposes the list's laid-out pixel rect (last frame) for app-drawn
// scrollbars.
func (e *ListEl) Bounds() Rect { return e.rect }

func (e *ListEl) growFactor() float32 { return e.grow }

// BarColor sets the scrollbar thumb colour (default translucent white for dark
// themes; use a dark colour on light backgrounds).
func (e *ListEl) BarColor(c RGBA) *ListEl { e.barC, e.hasBarC = c, true; return e }

func (e *ListEl) measure(*Context) (float32, float32) {
	w, h := e.w, e.h
	if h == 0 {
		h = 200
	}
	return w, h
}

// mainOf picks the along-the-list component of a (w, h) pair.
func (e *ListEl) mainOf(w, h float32) float32 {
	if e.horiz {
		return w
	}
	return h
}

// offset is the scroll position along the list.
func (e *ListEl) offset() float32 {
	if e.horiz {
		return e.st.scroll.X
	}
	return e.st.scroll.Y
}

// setExtent records the content and view lengths along the list and
// clamps the offset to them.
func (e *ListEl) setExtent(content float32, r Rect) {
	s := &e.st.scroll
	if e.horiz {
		s.contentW, s.viewW = content, r.W
		s.contentH, s.viewH = r.H, r.H
		s.X = min(max(s.X, 0), s.maxX())
		return
	}
	s.contentH, s.viewH = content, r.H
	s.Y = min(max(s.Y, 0), s.maxY())
}

// place lays el out at content position pos (along the list, from the
// content start) with length size, mirrored for BottomUp.
func (e *ListEl) place(ctx *Context, el Element, r Rect, off, pos, size float32) {
	view := e.mainOf(r.W, r.H)
	p := pos - off
	if e.bottomUp {
		p = view - (pos + size) + off
	}
	if e.horiz {
		// Across the list a row keeps its own height (a QML delegate is not
		// stretched), filling the list only when it states none.
		h := r.H
		if _, mh := el.measure(ctx); mh > 0 && mh < r.H {
			h = mh
		}
		el.layout(ctx, Rect{r.X + p, r.Y, size, h})
		return
	}
	el.layout(ctx, Rect{r.X, r.Y + p, r.W, size})
}

func (e *ListEl) layout(ctx *Context, r Rect) {
	e.rect = r
	pitch := e.rowH + e.gap
	view := e.mainOf(r.W, r.H)
	// The header and footer, measured for their length along the list.
	e.headSz, e.footSz = 0, 0
	if e.header != nil {
		e.headSz = e.mainOf(e.header.measure(ctx))
	}
	if e.footer != nil {
		e.footSz = e.mainOf(e.footer.measure(ctx))
	}
	variable := e.variable()
	if variable {
		e.syncRows()
	}
	rowsLen := e.contentHeight()
	e.setExtent(e.headSz+rowsLen+e.footSz, r)
	off := e.offset()
	rowsOff := off - e.headSz // the scroll offset within the rows

	// Visible row range, with one row of overscan each side.
	first := max(int(rowsOff/pitch)-1, 0)
	last := min(int((rowsOff+view)/pitch)+1, e.count)
	if variable {
		// Rows differ in size, so the range comes from the index: the rows
		// that end above the offset are skipped, and the range stops at the
		// first row that starts below the view. A measured list answers on
		// what it has learned so far (the estimate for rows it has never
		// built).
		ix := &e.st.rows
		first = ix.find(float64(rowsOff)+float64(e.gap), false, e.rowH, e.gap)
		last = 0
		if bottom := float64(rowsOff + view); bottom >= 0 {
			last = min(ix.find(bottom, true, e.rowH, e.gap)+1, e.count)
		}
		first = max(first-1, 0)
		last = min(last+1, e.count)
	}
	first = min(first, max(last, 0))

	e.first = first
	// Before the rows: see ScrollEl.layout — regions register
	// outermost-first so the wheel reaches the innermost one.
	ctx.scrolls = append(ctx.scrolls, e)
	e.rows = e.rows[:0]
	built := make([]Element, 0, max(last-first, 0))
	for i := first; i < last; i++ {
		row := e.build(i)
		built = append(built, row)
		if e.measure_ {
			// Learn this row's own size BEFORE anything is positioned, so
			// the rows after it land where they belong on this very frame
			// (a size learned a frame late shows as a jump).
			// A row Heights covers keeps the height it was given.
			if ms := e.mainOf(row.measure(ctx)); ms > 0 && i >= len(e.heights) {
				e.st.rows.set(i, ms)
				e.st.measured = true
			}
		}
	}
	if e.measure_ {
		// The content length and the scroll clamp move with what was just
		// measured.
		rowsLen = e.contentHeight()
		e.setExtent(e.headSz+rowsLen+e.footSz, r)
		off = e.offset()
	}
	st := e.st
	st.variable, st.horiz, st.est, st.gap = variable, e.horiz, e.rowH, e.gap
	st.head, st.count = e.headSz, e.count
	if e.header != nil {
		e.place(ctx, e.header, r, off, 0, e.headSz)
	}
	// Rows of differing sizes sit on a running sum carried from the first
	// built row, whose start the index gives.
	var top float64
	if variable && first < last {
		top = e.st.rows.top(first, e.rowH, e.gap)
	}
	for k, row := range built {
		i := first + k
		h := e.rowH
		if variable {
			h = e.st.rows.size(i, e.rowH)
		} else {
			top = float64(float32(i) * pitch)
		}
		e.place(ctx, row, r, off, e.headSz+float32(top), h)
		e.rows = append(e.rows, row)
		if variable {
			top += float64(h) + float64(e.gap)
		}
	}
	if e.footer != nil {
		e.place(ctx, e.footer, r, off, e.headSz+rowsLen, e.footSz)
	}
}

// dirty while a flick is carrying the list or its bar is fading.
func (e *ListEl) dirty(*Context) bool {
	if e.st.scroll.flingX != 0 || e.st.scroll.flingY != 0 {
		return true
	}
	if e.autoHide {
		if a := e.st.scroll.barAlpha(); a > 0 && a < 1 {
			return true
		}
	}
	return false
}

// paintSig: the offset and the bar (see damage.go).
func (e *ListEl) paintSig() uint64 {
	h := newSig(0x115)
	h.ptr(unsafe.Pointer(e.st))
	h.f32(e.st.scroll.Y)
	h.f32(e.st.scroll.X)
	h.col(e.barC)
	h.b(e.hasBarC)
	h.b(e.autoHide)
	return uint64(h)
}

func (e *ListEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	ctx.R.PushClip(e.rect.X, e.rect.Y, e.rect.W, e.rect.H)
	if e.header != nil {
		e.header.paint(ctx)
	}
	for _, row := range e.rows {
		row.paint(ctx)
	}
	if e.footer != nil {
		e.footer.paint(ctx)
	}
	if m := e.st.scroll.maxX(); e.horiz && m > 0 {
		// A horizontal list's bar runs along the bottom.
		barW := max(24, e.rect.W*e.st.scroll.viewW/e.st.scroll.contentW)
		barX := e.rect.X + (e.rect.W-barW)*(e.st.scroll.X/m)
		bc := RGBA{R: 1, G: 1, B: 1, A: 0.25}
		if e.hasBarC {
			bc = e.barC
		}
		if e.autoHide {
			bc.A *= e.st.scroll.barAlpha()
		}
		ctx.R.FillRoundRect(barX, e.rect.Y+e.rect.H-5, barW, 3, 1.5, bc)
	}
	if m := e.st.scroll.maxY(); !e.horiz && m > 0 {
		barH := max(24, e.rect.H*e.st.scroll.viewH/e.st.scroll.contentH)
		barY := e.rect.Y + (e.rect.H-barH)*(e.st.scroll.Y/m)
		bc := RGBA{R: 1, G: 1, B: 1, A: 0.25}
		if e.hasBarC {
			bc = e.barC
		}
		// See ScrollEl.AutoHideBar: a QML view has no bar of its own.
		if e.autoHide {
			bc.A *= e.st.scroll.barAlpha()
		}
		ctx.R.FillRoundRect(e.rect.X+e.rect.W-5, barY, 3, barH, 1.5, bc)
	}
	ctx.R.PopClip()
}

func (e *ListEl) hit(x, y float32) Element {
	if !e.rect.contains(x, y) {
		return nil
	}
	for i := len(e.rows) - 1; i >= 0; i-- {
		if h := e.rows[i].hit(x, y); h != nil {
			return h
		}
	}
	for _, el := range []Element{e.footer, e.header} {
		if el != nil {
			if h := el.hit(x, y); h != nil {
				return h
			}
		}
	}
	return nil
}

func (e *ListEl) onClick() func() { return nil }

// childElems exposes the rows built for the visible range, so a walk of a
// built tree (ui.Children — tooling and tests) reaches a list's delegates
// instead of stopping at the view.
func (e *ListEl) childElems() []Element {
	if e.header == nil && e.footer == nil {
		return e.rows
	}
	out := make([]Element, 0, len(e.rows)+2)
	if e.header != nil {
		out = append(out, e.header)
	}
	out = append(out, e.rows...)
	if e.footer != nil {
		out = append(out, e.footer)
	}
	return out
}
func (e *ListEl) bounds() Rect { return e.rect }

// scrollTarget implementation.
func (e *ListEl) contains(x, y float32) bool { return e.rect.contains(x, y) }

// scroll takes the wheel: a Horizontal list scrolls sideways by it, as
// Qt's does.
func (e *ListEl) scroll(dy float32) bool {
	if e.horiz {
		return e.st.scroll.scrollByX(dy)
	}
	return e.st.scroll.scrollBy(dy)
}

// scrollX takes a sideways wheel (Shift+wheel, a tilt wheel).
func (e *ListEl) scrollX(dx float32) bool {
	if e.horiz {
		return e.st.scroll.scrollByX(dx)
	}
	return false
}
func (e *ListEl) viewRect() Rect { return e.rect }
func (e *ListEl) thumbRect() Rect {
	if e.horiz {
		return Rect{} // no draggable thumb along the bottom
	}
	return thumbRectFor(e.rect, &e.st.scroll)
}
func (e *ListEl) scrollToY(y float32) { scrollToYFor(e.rect, &e.st.scroll, y) }

// VisibleRange reports the row indices built this frame, for tests.
func (e *ListEl) VisibleRange() (first, count int) { return e.first, len(e.rows) }

func (e *ListEl) fixedWH() (float32, float32) { return e.w, e.h }
