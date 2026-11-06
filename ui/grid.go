package ui

// GridEl is a track grid: children are auto-placed in order into cols
// columns, each optionally spanning several columns and/or rows (GPUI's
// .grid().grid_cols(n) with col_span/row_span, and QtQuick.Layouts'
// GridLayout).
//
// Tracks are sized the way Qt's grid engine sizes them, since that is what
// the markup this serves expects:
//
//   - a track's preferred size is the largest preferred size among the
//     cells in it (a spanning cell contributes the part its own columns
//     cannot cover);
//   - space left over is shared out in PROPORTION to those preferred
//     sizes, not equally — a column of wide cards next to a column of
//     narrow ones stays wider — and a shortfall is taken away the same
//     way;
//   - but a grid NESTED in a Qt-style layout is only given as much as it
//     can use: an axis nothing inside can grow along caps at the grid's
//     own content (layoutMax, the rule Qt's engine applies to any nested
//     layout). MEASURED: two 280px cards in a fillWidth 2-column grid sit
//     at 20 and 324 — the grid is 584 wide, not the row's full width;
//   - a cell only stretches to fill its track if it asked to (QML's
//     Layout.fillWidth/fillHeight, which reach here as Grow/CrossFill).
//     One that did not keeps its size, left-aligned and vertically
//     centred in its cell, as Qt aligns it.
//
// Positioner mode (Positioned, for QML's plain `Grid`) skips the sharing
// out: tracks stay at their preferred size and nothing stretches.
type GridEl struct {
	cols, rows int
	gapX, gapY float32
	w, h       float32
	grow       float32
	crossFill  bool
	positioned bool
	items      []gridItem
	placed     []Rect // cell rect (col,row span) resolved in layout, per item
	rect       Rect
	// Track state, from the last prepare: preferred sizes, which tracks
	// hold something that can grow, and where each item landed.
	colPref, rowPref []float32
	// colMin/rowMin is the size a track cannot go below: the largest
	// minimum among the cells in it. Qt shrinks tracks toward these and
	// then OVERFLOWS — a 90px item in a 70px column stays 90 and runs past
	// the layout's edge (measured; ours used to squeeze it to 79).
	colMin, rowMin   []float32
	colFill, rowFill []bool
	natW, natH       float32
}

type gridItem struct {
	el               Element
	colSpan, rowSpan int
	col, row         int     // placement, resolved in prepare
	w, h             float32 // measured preferred size
	minW, minH       float32 // the size it cannot do without
	fillW, fillH     bool
}

// Grid starts a grid with the given track counts. rows may be 0 (or too
// few): the grid grows as many rows as the items need, which is what
// GridLayout does with only `columns` stated. Add cells with Cell (with a
// span) or Children (1×1 each).
func Grid(cols, rows int) *GridEl {
	if cols < 1 {
		cols = 1
	}
	return &GridEl{cols: cols, rows: max(rows, 0)}
}

func (g *GridEl) Gap(px float32) *GridEl     { g.gapX, g.gapY = px, px; return g }
func (g *GridEl) GapXY(x, y float32) *GridEl { g.gapX, g.gapY = x, y; return g }
func (g *GridEl) W(px float32) *GridEl       { g.w = px; return g }
func (g *GridEl) H(px float32) *GridEl       { g.h = px; return g }
func (g *GridEl) Size(w, h float32) *GridEl  { g.w, g.h = w, h; return g }
func (g *GridEl) Grow() *GridEl              { g.grow = 1; return g }

// CrossFill marks a grid that asked to fill its parent's CROSS axis —
// `GridLayout { Layout.fillWidth: true }` inside a ColumnLayout, which is
// how every corpus page lays its cards out.
func (g *GridEl) CrossFill() *GridEl { g.crossFill = true; return g }

func (g *GridEl) crossFills() bool { return g.crossFill }

// GrowBy is Grow with a weight; see DivEl.GrowBy.
func (g *GridEl) GrowBy(f float32) *GridEl { g.grow = f; return g }
func (g *GridEl) growFactor() float32      { return g.grow }

// Positioned makes the grid a positioner rather than a layout: tracks stay
// at their preferred size and cells are never stretched (QML `Grid`).
func (g *GridEl) Positioned() *GridEl { g.positioned = true; return g }

func (g *GridEl) fixedWH() (float32, float32) { return g.w, g.h }
func (g *GridEl) natural() (float32, float32) { return g.natW, g.natH }

// paintSig: a grid paints nothing itself (see damage.go).
func (g *GridEl) paintSig() uint64 { return 0xd1 }

func (g *GridEl) childElems() []Element {
	els := make([]Element, 0, len(g.items))
	for _, it := range g.items {
		els = append(els, it.el)
	}
	return els
}

// Cell adds a child spanning colSpan×rowSpan tracks (the column span is
// clamped to the grid's width; the row span is not, since rows grow).
func (g *GridEl) Cell(el Element, colSpan, rowSpan int) *GridEl {
	colSpan, rowSpan = max(colSpan, 1), max(rowSpan, 1)
	if colSpan > g.cols {
		colSpan = g.cols
	}
	g.items = append(g.items, gridItem{el: el, colSpan: colSpan, rowSpan: rowSpan})
	return g
}

// Children adds 1×1 cells in order.
func (g *GridEl) Children(els ...Element) *GridEl {
	for _, e := range els {
		g.items = append(g.items, gridItem{el: e, colSpan: 1, rowSpan: 1})
	}
	return g
}

// place assigns every item a cell, row-major, growing the row count as
// needed. Independent of sizes, so it only has to run once per frame.
func (g *GridEl) place() {
	rows := max(g.rows, 1)
	occ := map[[2]int]bool{}
	free := func(c, r, cs, rs int) bool {
		if c+cs > g.cols {
			return false
		}
		for dy := 0; dy < rs; dy++ {
			for dx := 0; dx < cs; dx++ {
				if occ[[2]int{c + dx, r + dy}] {
					return false
				}
			}
		}
		return true
	}
	for i := range g.items {
		it := &g.items[i]
		col, row := 0, 0
		for r := 0; ; r++ {
			c := 0
			for ; c < g.cols; c++ {
				if free(c, r, it.colSpan, it.rowSpan) {
					break
				}
			}
			if c < g.cols {
				col, row = c, r
				break
			}
		}
		for dy := 0; dy < it.rowSpan; dy++ {
			for dx := 0; dx < it.colSpan; dx++ {
				occ[[2]int{col + dx, row + dy}] = true
			}
		}
		it.col, it.row = col, row
		rows = max(rows, row+it.rowSpan)
	}
	g.rows = rows
}

// prepare measures the children and sizes the tracks' preferred extents.
// avail is the width the grid has to offer (0 when unknown), which its
// children need while they measure: a wrapping label in a column has to
// know how wide that column is to know how tall it is.
func (g *GridEl) prepare(ctx *Context, avail float32) {
	g.place()
	g.colPref = make([]float32, g.cols)
	g.rowPref = make([]float32, g.rows)
	g.colMin = make([]float32, g.cols)
	g.rowMin = make([]float32, g.rows)
	g.colFill = make([]bool, g.cols)
	g.rowFill = make([]bool, g.rows)
	// The width a cell is offered while measuring: an even share, which is
	// the best guess before the tracks are known.
	share := float32(0)
	if avail > 0 {
		share = (avail - g.gapX*float32(g.cols-1)) / float32(g.cols)
	}
	prevAvail := ctx.availW
	defer func() { ctx.availW = prevAvail }()
	for i := range g.items {
		it := &g.items[i]
		if share > 0 {
			ctx.availW = share * float32(it.colSpan)
		}
		it.w, it.h = it.el.measure(ctx)
		it.minW, it.minH = requiredSize(it.el, it.w, it.h, true)
		it.fillW, it.fillH = false, false
		if gf, ok := it.el.(interface{ growFactor() float32 }); ok && gf.growFactor() != 0 {
			it.fillW = true
		}
		if cf, ok := it.el.(interface{ crossFills() bool }); ok && cf.crossFills() {
			it.fillH = true
		}
		// A track grows only if something in it asked to fill (Qt's rule).
		// A spanning cell marks every track it covers.
		for c := it.col; c < it.col+it.colSpan && c < g.cols; c++ {
			g.colFill[c] = g.colFill[c] || it.fillW
		}
		for rw := it.row; rw < it.row+it.rowSpan && rw < g.rows; rw++ {
			g.rowFill[rw] = g.rowFill[rw] || it.fillH
		}
	}
	// Tracks take their preferred size from the cells that sit in ONE of
	// them, and only then are spanning cells checked for a shortfall. Both
	// in one pass would let a spanning cell inflate a track that its
	// neighbours were about to fill anyway.
	for pass := 0; pass < 2; pass++ {
		for i := range g.items {
			it := &g.items[i]
			if (it.colSpan == 1) == (pass == 0) {
				spread(g.colPref, it.col, it.colSpan, it.w, g.gapX)
				spread(g.colMin, it.col, it.colSpan, it.minW, g.gapX)
			}
			if (it.rowSpan == 1) == (pass == 0) {
				spread(g.rowPref, it.row, it.rowSpan, it.h, g.gapY)
				spread(g.rowMin, it.row, it.rowSpan, it.minH, g.gapY)
			}
		}
	}
	g.natW = sum(g.colPref) + g.gapX*float32(g.cols-1)
	g.natH = sum(g.rowPref) + g.gapY*float32(g.rows-1)
}

// spread grows the tracks a cell covers until they hold it, adding any
// shortfall to the LAST track it covers — Qt's rule, and the one that
// keeps a spanning cell from inflating columns its neighbours share.
func spread(pref []float32, at, span int, size, gap float32) {
	if at+span > len(pref) {
		span = len(pref) - at
	}
	if span <= 0 {
		return
	}
	if span == 1 {
		pref[at] = max(pref[at], size)
		return
	}
	have := gap * float32(span-1)
	for i := at; i < at+span; i++ {
		have += pref[i]
	}
	if have < size {
		pref[at+span-1] += size - have
	}
}

func sum(v []float32) float32 {
	var t float32
	for _, x := range v {
		t += x
	}
	return t
}

// tracks turns preferred sizes into final ones for the space available:
// surplus is shared in proportion to the preferred sizes (evenly when they
// are all zero), and a shortfall is taken away the same way.
// layoutMax caps the grid at its own content on an axis nothing inside can
// grow along — see the type comment. 0 means "no limit on that axis".
func (g *GridEl) layoutMax() (float32, float32) {
	if g.positioned {
		return 0, 0
	}
	var w, h float32
	if !anyTrue(g.colFill) && g.w == 0 {
		w = g.natW
	}
	if !anyTrue(g.rowFill) && g.h == 0 {
		h = g.natH
	}
	return w, h
}

func anyTrue(b []bool) bool {
	for _, v := range b {
		if v {
			return true
		}
	}
	return false
}

func (g *GridEl) tracks(pref, minimum []float32, fill []bool, avail, gap float32) []float32 {
	out := make([]float32, len(pref))
	copy(out, pref)
	if g.positioned || len(pref) == 0 {
		return out
	}
	space := avail - gap*float32(len(pref)-1)
	total := sum(pref)
	delta := space - total
	if delta == 0 {
		return out
	}
	if total <= 0 {
		even := space / float32(len(pref))
		for i := range out {
			out[i] = max(even, 0)
		}
		return out
	}
	if delta > 0 {
		for i := range out {
			out[i] = max(out[i]+delta*out[i]/total, 0)
		}
		return out
	}
	// Shrinking: a track goes down only as far as its content's minimum,
	// and the shortfall it refuses is taken from the tracks that still
	// have slack. When nothing has slack left the grid OVERFLOWS, which is
	// what Qt does rather than squeezing the item.
	want := -delta
	for pass := 0; pass < len(out)+1 && want > 0.01; pass++ {
		var slack float32
		for i := range out {
			slack += max(out[i]-g.trackMin(minimum, i), 0)
		}
		if slack <= 0.01 {
			break
		}
		take := min(want, slack)
		for i := range out {
			s := max(out[i]-g.trackMin(minimum, i), 0)
			if s <= 0 {
				continue
			}
			out[i] = max(out[i]-take*s/slack, g.trackMin(minimum, i))
		}
		want -= take
	}
	return out
}

// trackMin is a track's floor, guarded for a shorter minimum slice.
func (g *GridEl) trackMin(minimum []float32, i int) float32 {
	if i < len(minimum) {
		return max(minimum[i], 0)
	}
	return 0
}

func (g *GridEl) measure(ctx *Context) (float32, float32) {
	g.prepare(ctx, orZero(g.w, ctx.availW))
	w, h := g.natW, g.natH
	if g.w > 0 {
		w = g.w
	}
	if g.h > 0 {
		h = g.h
	}
	return w, h
}

// orZero is the first value that is set.
func orZero(a, b float32) float32 {
	if a > 0 {
		return a
	}
	return b
}

func (g *GridEl) layout(ctx *Context, r Rect) {
	g.rect = r
	g.prepare(ctx, r.W)
	colW := g.tracks(g.colPref, g.colMin, g.colFill, r.W, g.gapX)
	rowH := g.tracks(g.rowPref, g.rowMin, g.rowFill, r.H, g.gapY)
	x0 := make([]float32, len(colW))
	y0 := make([]float32, len(rowH))
	var acc float32
	for i, w := range colW {
		x0[i] = acc
		acc += w + g.gapX
	}
	acc = 0
	for i, h := range rowH {
		y0[i] = acc
		acc += h + g.gapY
	}
	g.placed = make([]Rect, len(g.items))
	for i := range g.items {
		it := &g.items[i]
		cw := g.gapX * float32(it.colSpan-1)
		for c := it.col; c < it.col+it.colSpan && c < len(colW); c++ {
			cw += colW[c]
		}
		ch := g.gapY * float32(it.rowSpan-1)
		for rr := it.row; rr < it.row+it.rowSpan && rr < len(rowH); rr++ {
			ch += rowH[rr]
		}
		cell := Rect{r.X + x0[min(it.col, len(x0)-1)], r.Y + y0[min(it.row, len(y0)-1)], cw, ch}
		box := cell
		// A cell that did not ask to fill keeps its size: left-aligned,
		// vertically centred (Qt's default alignment in a grid).
		if !it.fillW && !g.stretchAll(it, true) && it.w > 0 && it.w < cw {
			box.W = it.w
		}
		if !it.fillH && !g.stretchAll(it, false) && it.h > 0 && it.h < ch {
			box.H = it.h
			box.Y += (ch - it.h) / 2
		}
		g.placed[i] = box
		it.el.layout(ctx, box)
	}
}

// stretchAll reports whether an element has no size of its own on an axis,
// in which case it fills its cell whatever it asked for — an auto-sized
// container has nothing else to be.
func (g *GridEl) stretchAll(it *gridItem, horizontal bool) bool {
	f, ok := it.el.(interface{ fixedWH() (float32, float32) })
	if !ok {
		return false
	}
	w, h := f.fixedWH()
	if horizontal {
		return w == 0 && it.w == 0
	}
	return h == 0 && it.h == 0
}

func (g *GridEl) paint(ctx *Context) {
	if ctx.culled(g) {
		return
	}
	for _, it := range g.items {
		it.el.paint(ctx)
	}
}

func (g *GridEl) hit(x, y float32) Element {
	for i := len(g.items) - 1; i >= 0; i-- {
		if h := g.items[i].el.hit(x, y); h != nil {
			return h
		}
	}
	return nil
}

func (g *GridEl) onClick() func() { return nil }
func (g *GridEl) bounds() Rect    { return g.rect }
