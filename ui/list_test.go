package ui

import "testing"

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
	// A height list of the wrong length is ignored — uniform rows stay.
	plain := List(st, 40, 3, func(int) Element { return Div() }).Heights([]float32{1, 2})
	if plain.heights != nil {
		t.Error("a mismatched height list must be ignored")
	}
	if top, h := plain.rowTop(2); top != 80 || h != 40 {
		t.Errorf("uniform row 2 at %v (h %v), want 80 (h 40)", top, h)
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
