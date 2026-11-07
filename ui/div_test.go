package ui

import "testing"

// A wrapping row breaks into lines at the width it is given: children keep
// their measured size and the ones that no longer fit start the next line.
func TestDivWrapBreaksIntoLines(t *testing.T) {
	cell := func() Element { return Div().Size(80, 40) }
	row := Div().Row().Wrap().Gap(0).Children(
		cell(), cell(), cell(), cell(), cell(), cell())
	// The root is stretched to the window, so the sized box goes inside it.
	root := Div().Col().Children(Div().Col().Size(330, 200).Children(row))
	New(nil, nil, func() Element { return root }).Layout(400, 300)
	got := make([][2]float32, 0, 6)
	for _, c := range row.children {
		b := Bounds(c)
		got = append(got, [2]float32{b.X, b.Y})
	}
	// 330px fits four 80px cells; the last two wrap onto a second line.
	want := [][2]float32{{0, 0}, {80, 0}, {160, 0}, {240, 0}, {0, 40}, {80, 40}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cell %d at %v, want %v", i, got[i], want[i])
		}
	}
}

// Its height is the lines it breaks into, measured against the width on
// offer rather than the whole run laid end to end.
func TestDivWrapMeasuresHeightForWidth(t *testing.T) {
	row := Div().Row().Wrap().Gap(10)
	for i := 0; i < 5; i++ {
		row.Children(Div().Size(80, 40))
	}
	root := Div().Col().Children(Div().Col().Size(280, 300).Children(row))
	New(nil, nil, func() Element { return root }).Layout(400, 400)
	// 280px fits three 80px cells with two 10px gaps (260); five cells
	// take two lines, so 40 + 10 + 40.
	if h := Bounds(row).H; h != 90 {
		t.Errorf("wrapped row is %.0fpx tall, want 90", h)
	}
}

// A window with no pointer over it hovers nothing. (0,0) is a real point on
// screen, so starting the pointer there made a control laid out over the
// origin on its first frame latch a hover it never lost — a headless render
// came out with tinted flat buttons.
func TestNoPointerHoversNothing(t *testing.T) {
	var hovers []bool
	box := Div().Size(200, 100).OnHoverChanged(func(v bool) { hovers = append(hovers, v) })
	u := New(nil, nil, func() Element { return box })
	u.Layout(400, 300) // lays the box out over the origin
	box.paint(&u.ctx)
	if len(hovers) != 1 || hovers[0] {
		t.Errorf("hover states %v, want a single false: nothing is under the pointer", hovers)
	}
}

// A hidden element keeps its place: it lays out (so a sibling can anchor to
// it, and an effect can sample it) but neither paints nor takes input —
// QML's `visible: false`.
func TestHiddenLaysOutWithoutPainting(t *testing.T) {
	child := Div().Size(120, 60)
	h := Hidden(child)
	root := Div().Col().Children(h)
	New(nil, nil, func() Element { return root }).Layout(400, 300)
	if b := Bounds(child); b.W != 120 || b.H != 60 {
		t.Errorf("hidden child laid out %vx%v, want 120x60", b.W, b.H)
	}
	if w, _ := h.measure(&Context{}); w != 120 {
		t.Errorf("hidden measure %v, want the child's 120", w)
	}
	if got := h.hit(10, 10); got != nil {
		t.Error("a hidden element took a hit")
	}
}

// hfwBox stands in for a wrapping paragraph: 200px of content that takes
// as many 20px lines as the width it is given needs.
type hfwBox struct {
	rect Rect
	h    float32
}

func (b *hfwBox) measure(ctx *Context) (float32, float32) {
	if ctx != nil && ctx.availW > 0 {
		b.heightFor(ctx, ctx.availW)
		return min(200, ctx.availW), b.h
	}
	return 200, 20
}
func (b *hfwBox) heightFor(_ *Context, w float32) float32 {
	lines := float32(1)
	for lines*w < 200 {
		lines++
	}
	b.h = lines * 20
	return b.h
}
func (b *hfwBox) growFactor() float32          { return 1 }
func (b *hfwBox) layout(_ *Context, r Rect)    { b.rect = r }
func (b *hfwBox) paint(*Context)               {}
func (b *hfwBox) hit(float32, float32) Element { return nil }
func (b *hfwBox) onClick() func()              { return nil }
func (b *hfwBox) bounds() Rect                 { return b.rect }

// A wrapping label in a row is only as tall as the width it ENDS UP with,
// not the width it was first offered: it measures against the whole line
// (all a first pass can know) while its share is much narrower. Without
// asking again, a card's description column reported one line and ran off
// the card — which is what the QML corpus showed.
func TestRowAsksWrappingChildAgainForItsShare(t *testing.T) {
	box := &hfwBox{}
	row := Div().Row().Gap(30).Children(
		Div().Size(60, 150), Div().Size(60, 150), Div().Size(60, 150), box,
	)
	ctx := &Context{availW: 360}
	_, h := row.measure(ctx)
	// The leftover for the growing child is 360 - 3*60 - 3*30 = 90, so its
	// 200px of content takes three lines — and the row has to be tall
	// enough for the tallest child, which is still the 150px boxes.
	if box.h != 60 {
		t.Errorf("child measured %v tall for a 90px share, want 60", box.h)
	}
	if h < 150 {
		t.Errorf("row measured %v tall, want at least its tallest child", h)
	}
	row.layout(ctx, Rect{0, 0, 360, h})
	if got := box.bounds().W; got < 85 || got > 95 {
		t.Errorf("child width = %v, want the row's leftover (~90)", got)
	}
	if got := box.bounds().H; got != 60 {
		t.Errorf("child height = %v, want the 60 it asked for at that width", got)
	}
}

// A row whose wrapping child gets MORE room needs fewer lines, so the same
// content reports a shorter box: the correction runs both ways.
func TestRowWrappingChildShrinksWhenWider(t *testing.T) {
	box := &hfwBox{}
	row := Div().Row().Children(box)
	ctx := &Context{availW: 400}
	if _, h := row.measure(ctx); h != 20 {
		t.Errorf("row with a wide share measured %v, want one 20px line", h)
	}
}

// Layout.alignment: a child that states its own cross-axis alignment is
// PLACED in the space the layout gives it rather than stretched to fill it
// — `Layout.alignment: Qt.AlignHCenter` on a button in a card is how the
// corpus centres one, and ours sat hard against the left edge.
func TestAlignSelfPlacesInsteadOfStretching(t *testing.T) {
	ctx := &Context{}
	centred := Div().H(40).AlignSelf(AlignCenter)
	right := Div().H(40).AlignSelf(AlignEnd)
	stretched := Div().H(40)
	col := Div().Col().Gap(10).Children(centred, right, stretched)
	col.layout(ctx, Rect{0, 0, 400, 200})
	// Nothing states a width, so a stretched child fills the column...
	if got := Bounds(stretched); got.W != 400 || got.X != 0 {
		t.Errorf("unaligned child = %v, want the full width", got)
	}
	// ...while the aligned ones keep their (zero-content) width and are
	// placed by their alignment.
	if got := Bounds(centred); got.W == 400 {
		t.Errorf("centred child was stretched to %v", got)
	}
	if c, r := Bounds(centred).X, Bounds(right).X; !(c <= 200 && r >= c) {
		t.Errorf("centred x=%v, right x=%v — not placed by alignment", c, r)
	}
}

// A stated alignment also wins over the container's own align-items, since
// align-self is the more specific rule.
func TestAlignSelfBeatsContainerAlignment(t *testing.T) {
	ctx := &Context{}
	start := Div().Size(50, 20).AlignSelf(AlignStart)
	col := Div().Col().ItemsCenter().Children(start, Div().Size(50, 20))
	col.layout(ctx, Rect{0, 0, 400, 200})
	if got := Bounds(start).X; got != 0 {
		t.Errorf("align-self start under items-center sits at x=%v, want 0", got)
	}
}

// QtQuick.Layouts share the leftover main-axis space when NOTHING asked to
// expand: every item gets a slice in proportion to its own size and sits
// centred in it. Every card in the QML corpus is written that way — a
// heading and a couple of rows in a cell taller than they need — and a
// flex box left them packed at the top with all the slack below, so every
// row after the first landed too high.
func TestSpreadCellsSharesLeftover(t *testing.T) {
	ctx := &Context{}
	a := Div().Size(100, 23)
	b := Div().Size(100, 40)
	c := Div().Size(100, 20)
	col := Div().Col().Gap(15).SpreadCells().Children(a, b, c)
	col.layout(ctx, Rect{0, 0, 100, 240})
	// Cells: 210 of space (240 less two 15px gaps) split 23:40:20, so
	// 58.2 / 101.2 / 50.6 — each item centred in its own.
	got := []float32{Bounds(a).Y, Bounds(b).Y, Bounds(c).Y}
	want := []float32{17.6, 103.9, 204.6}
	for i := range want {
		if d := got[i] - want[i]; d > 1 || d < -1 {
			t.Errorf("item %d at y=%v, want ~%v (all: %v)", i, got[i], want[i], got)
		}
	}
	// Each keeps its own height: the cell grows, the item does not.
	if Bounds(b).H != 40 {
		t.Errorf("item height = %v, want 40 — the cell grows, not the item", Bounds(b).H)
	}
}

// ...and when something DOES expand, that item takes the surplus and the
// rest stay packed, which is also Qt's rule.
func TestSpreadCellsYieldsToAnExpandingChild(t *testing.T) {
	ctx := &Context{}
	a := Div().Size(100, 23)
	b := Div().Size(100, 40).Grow()
	c := Div().Size(100, 20)
	col := Div().Col().Gap(15).SpreadCells().Children(a, b, c)
	col.layout(ctx, Rect{0, 0, 100, 240})
	if got := Bounds(a).Y; got != 0 {
		t.Errorf("first item at y=%v, want 0 (packed at the top)", got)
	}
	if got := Bounds(b).H; got < 150 {
		t.Errorf("the expanding item is %v tall, want the surplus (~167)", got)
	}
	if got := Bounds(c).Y; got < 200 {
		t.Errorf("last item at y=%v, want the bottom of the column", got)
	}
}

// A nested Qt layout asked to fill its parent still stops at its own
// content when nothing inside it can grow — QtQuick.Layouts reports that
// as Layout.maximumHeight, and the space it does not take is shared out
// around it. The vertical-sliders card is written exactly that way: a
// heading and a row of fixed-height columns, the row filling.
func TestLayoutMaxCapsAFillingChild(t *testing.T) {
	ctx := &Context{}
	title := Div().Size(100, 23)
	inner := Div().Row().SpreadCells().Children(Div().Size(60, 176), Div().Size(60, 176))
	outer := Div().Col().Gap(15).SpreadCells().Children(title, inner.Grow())
	outer.layout(ctx, Rect{0, 0, 360, 300})
	if got := Bounds(inner).H; got != 176 {
		t.Errorf("the filling row is %v tall, want its content 176", got)
	}
	// 285 of space over cells of 23 and 176 → 33 and 252, each item
	// centred: the title 5 down, the row 38 into its own cell.
	if got := Bounds(title).Y; got < 4 || got > 6 {
		t.Errorf("title at y=%v, want ~5", got)
	}
	if got := Bounds(inner).Y; got < 85 || got > 87 {
		t.Errorf("row at y=%v, want ~86", got)
	}
}

// ...but a child that really can use the space still gets it: the ceiling
// comes from the content, so a row holding something that fills its HEIGHT
// (Layout.fillHeight, the row's cross axis) has none.
func TestLayoutMaxYieldsToGrowableContent(t *testing.T) {
	ctx := &Context{}
	inner := Div().Row().SpreadCells().Children(Div().Size(60, 40).CrossFill())
	outer := Div().Col().SpreadCells().Children(Div().Size(100, 23), inner.Grow())
	outer.layout(ctx, Rect{0, 0, 360, 300})
	if got := Bounds(inner).H; got < 250 {
		t.Errorf("the row is %v tall, want the leftover (~277)", got)
	}
	// A row whose items only fill horizontally keeps its ceiling: the
	// height of its own content.
	row := Div().Row().SpreadCells().Children(Div().Size(60, 40).Grow())
	col := Div().Col().SpreadCells().Children(Div().Size(100, 23), row.Grow())
	col.layout(ctx, Rect{0, 0, 360, 300})
	if got := Bounds(row).H; got != 40 {
		t.Errorf("the row is %v tall, want its content 40", got)
	}
}

// Qt lays a layout out in the space it NEEDS and lets the overflow spill,
// and it aligns against that larger extent: a centred button in a card
// whose paragraphs run past the right edge sits where the wide content
// puts the middle, not where the box does.
func TestSpreadCellsAlignsAgainstOverflow(t *testing.T) {
	ctx := &Context{}
	wide := &fixedBox{w: 574, h: 20}
	btn := Div().Size(150, 40).AlignSelf(AlignCenter)
	col := Div().Col().Gap(10).SpreadCells().Children(wide, btn)
	col.layout(ctx, Rect{0, 0, 360, 300})
	// (574-150)/2 = 212, which is where Qt puts it — centring in the box
	// would give 105.
	if got := Bounds(btn).X; got < 211 || got > 213 {
		t.Errorf("centred child at x=%v, want ~212 (centred in the overflow)", got)
	}
}

// fixedBox is a child that measures wider than the box it is put in and
// does not stretch — a non-wrapping label in a narrow card.
type fixedBox struct {
	w, h float32
	rect Rect
}

func (b *fixedBox) measure(*Context) (float32, float32) { return b.w, b.h }
func (b *fixedBox) layout(_ *Context, r Rect)           { b.rect = r }
func (b *fixedBox) paint(*Context)                      {}
func (b *fixedBox) hit(float32, float32) Element        { return nil }
func (b *fixedBox) onClick() func()                     { return nil }
func (b *fixedBox) bounds() Rect                        { return b.rect }

// What a layout cannot do without: an item that FILLS an axis can shrink
// on it — down to its own minimum, and recursively for a nested layout —
// while one that does not is stuck at what it measured. That is the
// difference between a row of buttons, which pushes its card wider, and a
// row of sliders, which simply gets narrower.
func TestRequiredSizeFollowsWhatCanShrink(t *testing.T) {
	ctx := &Context{}
	// A row of three unfilled buttons needs 3*120 + gaps = 390.
	buttons := Div().Row().Gap(15).SpreadCells().Children(
		Div().Size(120, 40), Div().Size(120, 40), Div().Size(120, 40))
	buttons.measure(ctx)
	if w, _ := buttons.layoutMin(); w != 390 {
		t.Errorf("row of buttons needs %v, want 390", w)
	}
	// The same row of FILLING sliders needs only its labels: each slider
	// can shrink to nothing.
	sliders := Div().Row().Gap(15).SpreadCells().Children(
		Div().Size(33, 20), Div().Size(270, 20).Grow(),
		Div().Size(36, 20), Div().Size(270, 20).Grow())
	sliders.measure(ctx)
	if w, _ := sliders.layoutMin(); w != 33+36+3*15 {
		t.Errorf("row of sliders needs %v, want %v", w, 33+36+3*15)
	}
}

// A column stretches a filling child to the extent it WORKS IN, which is
// the wider of its box and what its content needs: the 2D-transforms card
// centres its panel on the middle of the overflowing row above it, not on
// the middle of the card.
func TestFillingChildStretchesToTheOverflow(t *testing.T) {
	ctx := &Context{}
	wide := Div().Row().Gap(15).SpreadCells().Children(
		Div().Size(120, 40), Div().Size(120, 40), Div().Size(120, 40))
	panel := Div() // auto-sized: fills whatever it is given
	col := Div().Col().Gap(8).SpreadCells().Children(wide, panel)
	col.measure(ctx)
	col.layout(ctx, Rect{0, 0, 360, 300})
	if got := Bounds(panel).W; got != 390 {
		t.Errorf("panel is %v wide, want the 390 the row needs", got)
	}
}

// Per-side padding places the content box on each edge independently —
// QML's leftPadding/rightPadding on a control, and the Layout.*Margin of
// a layout item.
func TestPaddingPerSide(t *testing.T) {
	ctx := &Context{}
	child := Div().Size(50, 20)
	box := Div().Padding(4, 8, 12, 16).Children(child)
	box.layout(ctx, Rect{0, 0, 100, 60})
	if got := Bounds(child); got.X != 16 || got.Y != 4 {
		t.Errorf("child at (%v,%v), want (16,4)", got.X, got.Y)
	}
	// And the content box loses both edges: 100 - 16 - 8 wide.
	stretched := Div()
	Div().Padding(4, 8, 12, 16).Children(stretched).layout(ctx, Rect{0, 0, 100, 60})
	if got := Bounds(stretched).W; got != 76 {
		t.Errorf("stretched child is %v wide, want 76", got)
	}
}

// Shrinking stops at what a child cannot do without — for a nested layout,
// what ITS content needs. Squeezing past that piles items on top of each
// other, where Qt lets the column overflow: the corpus' checkbox card
// printed its footer over its own options.
func TestShrinkStopsAtTheRequiredSize(t *testing.T) {
	ctx := &Context{}
	rows := []Element{Div().Size(100, 30), Div().Size(100, 30), Div().Size(100, 30)}
	inner := Div().Col().SpreadCells().Children(rows...)
	footer := Div().Size(100, 20)
	// 90 + 20 of content in a 60-tall column: something must overflow.
	col := Div().Col().SpreadCells().Children(inner.Shrink(), footer)
	col.measure(ctx)
	col.layout(ctx, Rect{0, 0, 100, 60})
	if got := Bounds(inner).H; got < 90 {
		t.Errorf("the nested column was squeezed to %v, want its content 90", got)
	}
	if a, b := Bounds(inner), Bounds(footer); b.Y < a.Y+a.H {
		t.Errorf("footer at y=%v overlaps the column ending at %v", b.Y, a.Y+a.H)
	}
}

// A COLUMN whose content needs more width than it has works in that wider
// extent and offers it to its children: the corpus' dial card is 280 wide
// but its row of three 140px dials needs 460, so the wrapping caption
// underneath stays on ONE line — and its cell keeps a single line's
// height, which is what puts the row of dials where Qt has it.
func TestColumnOffersTheOverflowToWrappingChildren(t *testing.T) {
	ctx := &Context{availW: 280}
	wide := &fixedBox{w: 460, h: 162}
	cap := &hfwBox{} // 200px of content: two lines at 280, one at 460
	col := Div().Col().Gap(15).SpreadCells().Children(wide, cap)
	_, h := col.measure(ctx)
	col.layout(ctx, Rect{0, 0, 280, h})
	if cap.h != 20 {
		t.Errorf("caption is %v tall, want one 20px line at the 460 extent", cap.h)
	}
	// And with no overflow it wraps to the box as before.
	narrow := &fixedBox{w: 100, h: 30}
	cap2 := &hfwBox{}
	col2 := Div().Col().Gap(15).SpreadCells().Children(narrow, cap2)
	ctx2 := &Context{availW: 100}
	_, h2 := col2.measure(ctx2)
	col2.layout(ctx2, Rect{0, 0, 100, h2})
	if cap2.h != 40 {
		t.Errorf("caption is %v tall, want two lines at 100", cap2.h)
	}
}

// A ROW shares its leftover the same way a column does — MEASURED (Qt
// 6.10): two 126px items in a 360px RowLayout (spacing 25) land at 0 and
// 193, each cell taking half of the 83px surplus and the item sitting at
// its left. A row of buttons in a card looks packed instead only because a
// NESTED layout is capped at its own content width (layoutMax) and so has
// nothing to share.
func TestSpreadCellsSharesARowsLeftover(t *testing.T) {
	ctx := &Context{}
	a := Div().Size(126, 60)
	b := Div().Size(126, 60)
	row := Div().Row().Gap(25).SpreadCells().Children(a, b)
	row.layout(ctx, Rect{0, 0, 360, 60})
	if x := Bounds(a).X; x < -1 || x > 21 {
		t.Errorf("first item at x=%v, want ~0..20", x)
	}
	if x := Bounds(b).X; x < 183 || x > 203 {
		t.Errorf("second item at x=%v, want ~193", x)
	}
}

// A WRAPPING row can do without the width its children add up to — it
// breaks them onto more lines — so it must not report their sum as its
// required size. QML's Flow: four 145px buttons in a card 360px wide make
// two lines of two; reporting 625 made the card's column hand the Flow 625
// and it never wrapped (the buttons page's third button ran over the
// neighbouring card).
func TestWrappingRowRequiresOnlyItsWidestChild(t *testing.T) {
	ctx := &Context{}
	flow := Div().Row().Wrap().Gap(15).Children(
		Div().Size(145, 40), Div().Size(145, 40), Div().Size(145, 40), Div().Size(145, 40))
	flow.measure(ctx)
	if w, _ := flow.layoutMin(); w != 145 {
		t.Errorf("wrapping row requires %v, want 145 (its widest child)", w)
	}
	// ...and in a Qt-style column 360 wide it wraps to two lines of two.
	col := Div().Col().SpreadCells().Children(flow)
	col.layout(ctx, Rect{0, 0, 360, 200})
	xs := []float32{}
	for _, c := range flow.children {
		xs = append(xs, Bounds(c).X)
	}
	want := []float32{0, 160, 0, 160}
	for i := range want {
		if xs[i] != want[i] {
			t.Errorf("child %d at x=%v, want %v (all: %v)", i, xs[i], want[i], xs)
		}
	}
}

// A growing box with a ceiling stops there and its sibling takes the rest
// (CSS max-width), in a column too.
func TestMaxWShare(t *testing.T) {
	var a, b, c, e *DivEl
	u := New(nil, nil, func() Element {
		a = Div().Grow().MaxW(100)
		b = Div().Grow()
		c = Div().Grow().MaxH(50)
		e = Div().Grow()
		return Div().Col().Size(400, 300).Children(
			Div().Row().H(100).Children(a, b),
			Div().Col().Grow().Children(c, e),
		)
	})
	u.Layout(400, 300)
	if a.Rect().W != 100 || b.Rect().W != 300 {
		t.Errorf("row: capped %v, other %v; want 100, 300", a.Rect().W, b.Rect().W)
	}
	if c.Rect().H != 50 || e.Rect().H != 150 {
		t.Errorf("column: capped %v, other %v; want 50, 150", c.Rect().H, e.Rect().H)
	}
}
