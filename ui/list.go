package ui

import "unsafe"

// ListState is the retained scroll position of a virtualized list, plus
// what it has learned about its rows: a QML delegate sizes itself off its
// own data (a chat bubble is as tall as its message), so a row's height is
// only known once that row has been built. measured caches it by index and
// survives the frame, which is what keeps a variable-height list from
// laying every row out at the first one's height.
type ListState struct {
	scroll   ScrollState
	measured map[int]float32
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

// rowH_ is row i's height: the one given by Heights, the one measured when
// the row was last built, else the uniform estimate.
func (e *ListEl) rowHeightAt(i int) float32 {
	if i < len(e.heights) {
		return e.heights[i]
	}
	if e.measure_ && e.st.measured != nil {
		if h, ok := e.st.measured[i]; ok {
			return h
		}
	}
	return e.rowH
}

// rowTop is where row i starts and how tall it is, honouring per-row
// heights when they were given.
func (e *ListEl) rowTop(i int) (top, h float32) {
	if e.heights == nil && !e.measure_ {
		return float32(i) * (e.rowH + e.gap), e.rowH
	}
	for k := 0; k < i; k++ {
		top += e.rowHeightAt(k) + e.gap
	}
	return top, e.rowHeightAt(i)
}

// contentHeight is the whole list's height, however the rows are sized.
func (e *ListEl) contentHeight() float32 {
	if e.count == 0 {
		return 0
	}
	if e.heights == nil && !e.measure_ {
		return float32(e.count)*(e.rowH+e.gap) - e.gap
	}
	var total float32
	for i := 0; i < e.count; i++ {
		total += e.rowHeightAt(i) + e.gap
	}
	return max(total-e.gap, 0)
}

// ListEl is the per-frame element of a ListState.
type ListEl struct {
	st      *ListState
	rowH    float32
	gap     float32   // space between rows; the pitch is rowH + gap
	heights []float32 // per-row heights (sections); nil = uniform rowH
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
	rowsLen := e.contentHeight()
	e.setExtent(e.headSz+rowsLen+e.footSz, r)
	off := e.offset()
	rowsOff := off - e.headSz // the scroll offset within the rows

	// Visible row range, with one row of overscan each side.
	first := max(int(rowsOff/pitch)-1, 0)
	last := min(int((rowsOff+view)/pitch)+1, e.count)
	if e.heights != nil || e.measure_ {
		// Rows differ in size, so the visible range is found by walking
		// them; a sectioned list is short enough that this is cheaper than
		// keeping a prefix-sum index up to date. A measured list walks the
		// same way, on what it has learned so far (the uniform estimate
		// for rows it has never built).
		first, last = 0, e.count
		var y float32
		for i := 0; i < e.count; i++ {
			h := e.rowHeightAt(i)
			if y+h < rowsOff {
				first = i + 1
			}
			if y > rowsOff+view {
				last = i
				break
			}
			y += h + e.gap
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
			if ms := e.mainOf(row.measure(ctx)); ms > 0 {
				if e.st.measured == nil {
					e.st.measured = map[int]float32{}
				}
				e.st.measured[i] = ms
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
	if e.header != nil {
		e.place(ctx, e.header, r, off, 0, e.headSz)
	}
	for k, row := range built {
		i := first + k
		top, h := e.rowTop(i)
		if e.heights == nil && !e.measure_ {
			h = e.rowH
		}
		e.place(ctx, row, r, off, e.headSz+top, h)
		e.rows = append(e.rows, row)
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
