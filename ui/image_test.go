package ui

import "testing"

// A cell decodes for the box it is first painted in, and a much bigger box
// later wants a bigger decode: a photo that first painted while the layout
// was still settling (or a thumbnail that grows into a full-size view)
// would otherwise stay that first small decode, upscaled, for ever. It must
// not re-decode for a box that only grew a little, nor once the decode is
// already the source's own resolution.
func TestImageCellRedecodesForABiggerBox(t *testing.T) {
	c := &ImageCell{done: true, decodedAt: 200}
	for _, tc := range []struct {
		maxDim int
		want   bool
		why    string
	}{
		{200, false, "the same box"},
		{240, false, "20% bigger is within the step"},
		{400, true, "twice as big"},
	} {
		if got := c.needsRedecode(tc.maxDim); got != tc.want {
			t.Errorf("needsRedecode(%d) = %v, want %v (%s)", tc.maxDim, got, tc.want, tc.why)
		}
	}
	// Already at the source's resolution: there is nothing to gain.
	atSource := &ImageCell{done: true, decodedAt: 200, atSource: true}
	if atSource.needsRedecode(4000) {
		t.Errorf("a cell decoded at the source's own size asked to decode again")
	}
	// Undecoded cells go through the ordinary path.
	if (&ImageCell{}).needsRedecode(400) {
		t.Errorf("an undecoded cell reported needing a re-decode")
	}
}

// QML reports an Image's implicitWidth/implicitHeight as its SOURCE's own
// pixel size, whatever box the item ends up in — which matters when the
// layout depends on it: the corpus' blueprint viewer sizes its pan area
// from `blueprintImage.implicitWidth` and would otherwise divide by the
// zero it read back from a box that was waiting on the same number. An
// anchored item forwards its child's, since an anchored item is still the
// item.
func TestImageImplicitSizeIsTheSources(t *testing.T) {
	img := LazyImage(&ImageCell{}, 80, 80).Natural(640, 1222)
	if w, h := Implicit(img); w != 640 || h != 1222 {
		t.Errorf("Implicit = %vx%v, want the source's 640x1222", w, h)
	}
	// Laid out into a tiny box, the implicit size does not move.
	img.layout(&Context{}, Rect{0, 0, 10, 10})
	if w, h := Implicit(img); w != 640 || h != 1222 {
		t.Errorf("after layout Implicit = %vx%v, want 640x1222", w, h)
	}
	anchored := Anchored(img, "photo", Anchors{Fill: "parent"})
	if w, h := Implicit(anchored); w != 640 || h != 1222 {
		t.Errorf("anchored Implicit = %vx%v, want the child's 640x1222", w, h)
	}
}

// TestUnsizedImageMeasuresAsItsSource: an image with no size of its own
// measures as its SOURCE — QML's implicit size, and the floor a layout may
// not squeeze it below. The images page's 9-patch tiles are 90x90 sources
// in a two-column grid whose columns are narrower than that: Qt hands them
// their 90 and lets the grid overflow.
func TestUnsizedImageMeasuresAsItsSource(t *testing.T) {
	el := LazyImage(nil, 0, 0).Natural(90, 90)
	w, h := el.measure(&Context{})
	if w != 90 || h != 90 {
		t.Errorf("measured %vx%v, want the source's 90x90", w, h)
	}
	// A stated size still wins.
	sized := LazyImage(nil, 40, 30).Natural(90, 90)
	if w, h := sized.measure(&Context{}); w != 40 || h != 30 {
		t.Errorf("measured %vx%v, want the stated 40x30", w, h)
	}
	// And a grid does not shrink it below its source: two 90px tiles in a
	// 176px box overflow, as Qt's do.
	g := Grid(2, 1).Gap(18).Children(
		LazyImage(nil, 0, 0).Natural(90, 90),
		LazyImage(nil, 0, 0).Natural(90, 90),
	)
	ctx := &Context{}
	g.measure(ctx)
	g.layout(ctx, Rect{0, 0, 176, 120})
	if b := g.items[0].el.bounds(); b.W < 89 {
		t.Errorf("the first tile is %v wide, want its source's 90 (the grid should overflow)", b.W)
	}
}
