package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

// A sectioned list has rows of differing heights: a header row of its own
// size in front of each group. Positions come from walking the heights, so
// the items below a header do not shift by the wrong amount, and the
// content height is their sum plus the gaps between them.
func TestListPerRowHeights(t *testing.T) {
	st := &ListState{}
	heights := []float32{30, 34, 34, 30, 34}
	rows := map[int]Rect{}
	el := List(st, 34, len(heights), func(i int) Element {
		d := Div()
		rows[i] = Rect{}
		return d
	}).Heights(heights).Spacing(2)
	ctx := &Context{}
	el.layout(ctx, Rect{0, 0, 200, 400})
	// 30+34+34+30+34 = 162 plus four 2px gaps.
	if got := st.scroll.contentH; got != 170 {
		t.Errorf("content height = %v, want 170", got)
	}
	// Row tops: 0, 32, 68, 104, 136.
	for i, want := range []float32{0, 32, 68, 104, 136} {
		top, h := el.rowTop(i)
		if top != want || h != heights[i] {
			t.Errorf("row %d at %v (h %v), want %v (h %v)", i, top, h, want, heights[i])
		}
	}
	// A short height list covers the rows it reaches; the rest take the
	// uniform height. Entries past the row count are unused.
	short := List(&ListState{}, 40, 3, func(int) Element { return Div() }).Heights([]float32{10, 20})
	short.layout(&Context{}, Rect{0, 0, 200, 400})
	if top, h := short.rowTop(2); top != 30 || h != 40 {
		t.Errorf("row 2 past a short list at %v (h %v), want 30 (h 40)", top, h)
	}
	if got := short.contentHeight(); got != 70 {
		t.Errorf("short-list content height = %v, want 70", got)
	}
	long := List(&ListState{}, 40, 2, func(int) Element { return Div() }).Heights([]float32{10, 20, 99})
	if got := long.contentHeight(); got != 30 {
		t.Errorf("long-list content height = %v, want 30 (the extra entry unused)", got)
	}
}

// ListView.spacing is a gap BETWEEN rows: the pitch grows, each row keeps
// its own height (a delegate's background stays the size markup gave it).
func TestListSpacingIsAPitchNotAHeight(t *testing.T) {
	st := &ListState{}
	el := List(st, 52, 5, func(int) Element { return Div() }).Spacing(2)
	el.layout(&Context{}, Rect{0, 0, 200, 300})
	if top, h := el.rowTop(2); top != 108 || h != 52 {
		t.Errorf("row 2 at %v (h %v), want 108 (h 52)", top, h)
	}
	if got := st.scroll.contentH; got != 5*52+4*2 {
		t.Errorf("content height = %v, want %v", got, 5*52+4*2)
	}
}

// BottomUp is QML's verticalLayoutDirection: ListView.BottomToTop — index 0
// takes the BOTTOM of the view and later rows stack above it, so a chat log
// that appends its newest message shows it at the bottom. A list shorter
// than its view sits at the bottom of it, not the top.
func TestListBottomUpStacksFromTheBottom(t *testing.T) {
	st := &ListState{}
	boxes := map[int]Rect{}
	build := func(i int) Element {
		d := Div()
		boxes[i] = Rect{} // built rows are recorded by layout below
		return d
	}
	el := List(st, 40, 3, build).BottomUp()
	ctx := &Context{}
	view := Rect{0, 100, 200, 300}
	el.layout(ctx, view)
	got := make([]float32, 0, 3)
	for _, row := range el.rows {
		got = append(got, row.bounds().Y)
	}
	if len(got) != 3 {
		t.Fatalf("laid out %d rows, want 3", len(got))
	}
	// Index 0 at the bottom of the view (400-40), then upward by the pitch.
	for i, want := range []float32{360, 320, 280} {
		if got[i] != want {
			t.Errorf("row %d at y %v, want %v (all: %v)", i, got[i], want, got)
		}
	}
	// Top-to-bottom is unchanged: index 0 at the top.
	plain := List(st, 40, 3, build)
	plain.layout(&Context{}, view)
	if y := plain.rows[0].bounds().Y; y != 100 {
		t.Errorf("top-to-bottom row 0 at y %v, want 100", y)
	}
}

// A QML delegate sizes itself off its own data — a chat bubble is as tall
// as its message — so a Measured list lets each row be the height it
// builds itself, with the height given to List staying the estimate for
// rows that have not been built. Laying every row out at the first one's
// height stacked the tall ones over each other.
func TestListMeasuredRowsUseTheirOwnHeight(t *testing.T) {
	st := &ListState{}
	heights := []float32{40, 90, 40, 60}
	el := List(st, 40, len(heights), func(i int) Element {
		return Div().Size(200, heights[i])
	}).Measured()
	ctx := &Context{}
	el.layout(ctx, Rect{0, 0, 200, 400})
	got := make([]float32, 0, len(heights))
	for _, row := range el.rows {
		got = append(got, row.bounds().Y)
	}
	if len(got) != len(heights) {
		t.Fatalf("laid out %d rows, want %d", len(got), len(heights))
	}
	for i, want := range []float32{0, 40, 130, 170} {
		if got[i] != want {
			t.Errorf("row %d at y %v, want %v (all: %v)", i, got[i], want, got)
		}
	}
	if h := el.rows[1].bounds().H; h != 90 {
		t.Errorf("row 1 is %v tall, want its own 90", h)
	}
	// The content height is the sum of what the rows measured.
	if got, want := st.scroll.contentH, float32(230); got != want {
		t.Errorf("content height = %v, want %v", got, want)
	}
	// Without Measured the uniform height still rules.
	plain := List(&ListState{}, 40, len(heights), func(i int) Element {
		return Div().Size(200, heights[i])
	})
	plain.layout(&Context{}, Rect{0, 0, 200, 400})
	if h := plain.rows[1].bounds().H; h != 40 {
		t.Errorf("uniform row 1 is %v tall, want 40", h)
	}
}

// A Horizontal list lays its rows out left to right, builds only those in
// view, and scrolls sideways under the wheel.
func TestListHorizontal(t *testing.T) {
	var st ListState
	built := map[int]bool{}
	var cells []*DivEl
	l := List(&st, 50, 1000, func(i int) Element {
		built[i] = true
		d := Div().W(50)
		cells = append(cells, d)
		return d
	}).Horizontal().H(40)
	u := New(nil, nil, func() Element { return Div().Children(l.W(200)) })
	u.Layout(400, 100)
	if len(built) > 8 {
		t.Errorf("a 200 px window over 50 px rows built %d of 1000", len(built))
	}
	r0, r1 := cells[0].Rect(), cells[1].Rect()
	if r1.X-r0.X != 50 || r0.Y != r1.Y || r0.H != 40 {
		t.Errorf("rows at %+v and %+v, want side by side, 50 apart, 40 tall", r0, r1)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 100, Y: 20, ScrollY: 120}})
	if st.scroll.X != 120 {
		t.Errorf("the wheel scrolled X to %v, want 120", st.scroll.X)
	}
	cells, built = nil, map[int]bool{}
	u.Layout(400, 100)
	if !built[3] || built[0] {
		t.Errorf("after scrolling 120 px built rows %v, want 3 in and 0 out", built)
	}
}

// A header and footer are part of the content: they scroll with the rows.
func TestListHeaderFooterScroll(t *testing.T) {
	var st ListState
	head, foot := Div().H(30), Div().H(20)
	l := List(&st, 25, 10, func(i int) Element { return Div() }).Header(head).Footer(foot).H(100)
	u := New(nil, nil, func() Element { return Div().Children(l.W(200)) })
	u.Layout(400, 200)
	if head.Rect().Y != 0 || st.scroll.contentH != 30+250+20 {
		t.Fatalf("header at %v, content %v; want 0 and 300", head.Rect().Y, st.scroll.contentH)
	}
	st.SetOffset(40)
	u.Layout(400, 200)
	if head.Rect().Y != -40 {
		t.Errorf("header at %v after scrolling 40, want -40 (it scrolls away)", head.Rect().Y)
	}
	st.SetOffset(st.MaxY())
	u.Layout(400, 200)
	if got := foot.Rect().Y + foot.Rect().H; got != 100 {
		t.Errorf("footer ends at %v at the bottom, want the view's 100", got)
	}
}
