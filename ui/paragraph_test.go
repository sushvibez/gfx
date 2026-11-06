package ui

import "testing"

// A paragraph nothing gave a width to is as wide as its text: QML's Text
// takes its implicitWidth when no width is imposed, and anything sized off
// the label reads that. It used to report 0 — its measured width — so a
// bar sized off `someLabel.width` drew nothing.
func TestParagraphWithNoBoxIsAsWideAsItsText(t *testing.T) {
	p := Wrapped("one two three", RGB(0xffffff), 0)
	p.natW, p.natH = 120, 20 // what a measure with a renderer would find
	p.layout(nil, Rect{0, 0, 0, 20})
	if got := Bounds(p).W; got != 120 {
		t.Errorf("unconstrained paragraph is %v wide, want its text's 120", got)
	}
	// A box it IS given wins, so a stretched one still wraps to its cell.
	p.layout(nil, Rect{0, 0, 80, 40})
	if got := Bounds(p).W; got != 80 {
		t.Errorf("paragraph given an 80px box is %v wide", got)
	}
}

// TestWrappingLabelGetsItsColumnsWidth: a `Layout.fillWidth` wrapping label
// wraps at the width the COLUMN gives it. A column measured with nothing
// offered used to re-measure its children at the widest sibling's minimum
// and leave that width stuck on every paragraph inside it, so an 860px
// card description came back wrapped at 218 — three lines too tall, which
// pushed everything under it down the card.
func TestWrappingLabelGetsItsColumnsWidth(t *testing.T) {
	para := Wrapped("a wrapping description that runs past the width of a narrow box "+
		"but not past a wide one, which is how every card subtitle in the corpus is written",
		RGB(0xffffff), 0).CrossFill()
	col := Div().Gap(10).SpreadCells().Children(
		Text("a title", RGB(0xffffff)),
		para,
		Div().Grow(), // something that wants no width of its own
	)
	// No renderer, so nothing measures text — the widths under test are
	// the ones the LAYOUT hands out.
	ctx := &Context{}
	// A measure with nothing offered must not teach the paragraph a width.
	col.measure(ctx)
	col.layout(ctx, Rect{0, 0, 860, 200})
	if w := para.bounds().W; w < 850 {
		t.Errorf("the paragraph is %v wide, want the column's 860", w)
	}
	if got := para.wrapWidth(ctx); got < 850 {
		t.Errorf("it wraps at %v, want the width it was given", got)
	}
	_ = para.natW
}
