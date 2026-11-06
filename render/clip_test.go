//go:build !js

package render

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/gpu"
)

// drawClipped renders draw() into a WxH target and returns its pixels.
func drawClipped(t *testing.T, w, h int, draw func(r *Renderer2D)) []byte {
	t.Helper()
	device, queue := headlessDevice(t)
	target := device.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	r.Begin(w, h)
	draw(r)
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, w, h)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	return pix
}

// A rounded clip cuts the corners of what it contains: the middle of an
// oversized fill survives, the corners of the clip box do not — and a
// plain PushClip still keeps them, so the rounding is opt-in.
func TestPushClipRoundCutsCorners(t *testing.T) {
	const W, H = 80, 80
	// The clip covers the whole target with a 24px radius; the fill covers
	// everything, so only the clip shape decides what is painted.
	rounded := drawClipped(t, W, H, func(r *Renderer2D) {
		r.PushClipRound(0, 0, W, H, 24)
		r.FillRect(0, 0, W, H, RGBA{R: 1, A: 1})
		r.PopClip()
	})
	square := drawClipped(t, W, H, func(r *Renderer2D) {
		r.PushClip(0, 0, W, H)
		r.FillRect(0, 0, W, H, RGBA{R: 1, A: 1})
		r.PopClip()
	})
	red := func(pix []byte, x, y int) bool {
		i := (y*W + x) * 4
		return pix[i] > 128
	}
	// The centre and the edge midpoints are inside either shape.
	for _, p := range [][2]int{{40, 40}, {40, 1}, {1, 40}, {78, 40}, {40, 78}} {
		if !red(rounded, p[0], p[1]) {
			t.Errorf("rounded clip: (%d,%d) should be painted", p[0], p[1])
		}
	}
	// The very corners are outside the rounded shape and inside the box.
	for _, p := range [][2]int{{1, 1}, {78, 1}, {1, 78}, {78, 78}} {
		if red(rounded, p[0], p[1]) {
			t.Errorf("rounded clip: corner (%d,%d) should be cut", p[0], p[1])
		}
		if !red(square, p[0], p[1]) {
			t.Errorf("plain clip: corner (%d,%d) should survive", p[0], p[1])
		}
	}
}

// The clip applies to every primitive kind, not just quads: an image (the
// sprite pipeline, which path fills also use) is cut the same way.
func TestPushClipRoundAppliesToImages(t *testing.T) {
	const W, H = 60, 60
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		// The image outlives the draw call: its bind group is read when
		// the pass is submitted, not when the draw is queued.
		img := r.NewImage(1, 1, []byte{0, 255, 0, 255})
		r.PushClipRound(0, 0, W, H, 30) // a circle
		r.DrawImage(img, 0, 0, W, H, White)
		r.PopClip()
	})
	green := func(x, y int) bool { return pix[(y*W+x)*4+1] > 128 }
	if !green(30, 30) {
		t.Error("the middle of the circle should be painted")
	}
	if green(2, 2) {
		t.Error("the corner outside the circle should be cut")
	}
}

// Nested clips intersect. A rounded clip inside a box keeps its corners;
// two overlapping ROUNDED clips cannot be expressed as one rounded box, so
// the intersection falls back to the box rather than rounding the wrong
// corners.
func TestClipIntersection(t *testing.T) {
	r := &Renderer2D{}
	r.Begin(200, 200)

	// A rounded clip inside a larger box: the corners are the inner one's.
	r.PushClip(0, 0, 200, 200)
	r.PushClipRound(50, 50, 100, 100, 20)
	if got := r.clip(); got.r != 20 || got.x != 50 || got.w != 100 {
		t.Errorf("rounded inside a box = %+v, want the inner box with radius 20", got)
	}
	r.PopClip()
	r.PopClip()

	// The other way round: a box inside a rounded clip, where the box is
	// the whole intersection, so no corners are rounded.
	r.PushClipRound(0, 0, 200, 200, 40)
	r.PushClip(50, 50, 60, 60)
	if got := r.clip(); got.r != 0 || got.w != 60 {
		t.Errorf("box inside a rounded clip = %+v, want the plain inner box", got)
	}
	r.PopClip()
	// ...and the rounded clip is back on top after the pop.
	if got := r.clip(); got.r != 40 {
		t.Errorf("after PopClip the radius is %v, want 40", got.r)
	}
	r.PopClip()

	// Two rounded clips that only partly overlap: neither box survives, so
	// the shape is the intersection box.
	r.PushClipRound(0, 0, 100, 100, 20)
	r.PushClipRound(50, 50, 100, 100, 20)
	if got := r.clip(); got.r != 0 || got.x != 50 || got.w != 50 {
		t.Errorf("partly overlapping rounded clips = %+v, want the box 50,50 50x50", got)
	}
	r.PopClip()
	r.PopClip()

	// A radius past half the box is a stadium, as everywhere else.
	r.PushClipRound(0, 0, 40, 20, 100)
	if got := r.clip(); got.r != 10 {
		t.Errorf("clamped radius = %v, want 10 (half the short side)", got.r)
	}
	r.PopClip()
}

// A path clip is a coverage mask, so the shape really cuts: a triangular
// clip over a full-bleed fill paints inside the triangle and nowhere else.
func TestPushClipPathTriangle(t *testing.T) {
	const W, H = 80, 80
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		p := NewPath()
		p.MoveTo(40, 4).LineTo(76, 76).LineTo(4, 76).Close()
		r.PushClipPath(p, NonZero)
		r.FillRect(0, 0, W, H, RGBA{R: 1, A: 1})
		r.PopClip()
	})
	red := func(x, y int) bool { return pix[(y*W+x)*4] > 128 }
	// Inside the triangle: its middle and just above each edge's centre.
	for _, p := range [][2]int{{40, 60}, {40, 30}, {20, 70}, {60, 70}} {
		if !red(p[0], p[1]) {
			t.Errorf("(%d,%d) is inside the triangle and should be painted", p[0], p[1])
		}
	}
	// Outside it: the two top corners and the area above the apex.
	for _, p := range [][2]int{{4, 8}, {76, 8}, {40, 1}, {8, 40}, {72, 40}} {
		if red(p[0], p[1]) {
			t.Errorf("(%d,%d) is outside the triangle and should be cut", p[0], p[1])
		}
	}
}

// Nested path clips intersect: a mask is rendered through the mask it sits
// inside, so only the overlap of the two shapes paints.
func TestNestedPathClipsIntersect(t *testing.T) {
	const W, H = 80, 80
	// Two triangles: one pointing down from the top, one up from the
	// bottom. They overlap in a band across the middle.
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		down := NewPath()
		down.MoveTo(4, 4).LineTo(76, 4).LineTo(40, 60).Close()
		up := NewPath()
		up.MoveTo(4, 76).LineTo(76, 76).LineTo(40, 20).Close()
		r.PushClipPath(down, NonZero)
		r.PushClipPath(up, NonZero)
		r.FillRect(0, 0, W, H, RGBA{G: 1, A: 1})
		r.PopClip()
		r.PopClip()
	})
	green := func(x, y int) bool { return pix[(y*W+x)*4+1] > 128 }
	if !green(40, 40) {
		t.Error("the middle is inside both triangles and should be painted")
	}
	// Inside the downward triangle only (its top corners).
	if green(10, 8) || green(70, 8) {
		t.Error("the top corners are outside the upward triangle and should be cut")
	}
	// Inside the upward triangle only (its bottom corners).
	if green(10, 72) || green(70, 72) {
		t.Error("the bottom corners are outside the downward triangle and should be cut")
	}
}

// A path clip applies to text and images too, not only to fills: every
// pipeline reads the same clip binding.
func TestPushClipPathAppliesToImages(t *testing.T) {
	const W, H = 60, 60
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		img := r.NewImage(1, 1, []byte{0, 0, 255, 255})
		p := NewPath()
		// A diamond: the corners of the target are outside it.
		p.MoveTo(30, 2).LineTo(58, 30).LineTo(30, 58).LineTo(2, 30).Close()
		r.PushClipPath(p, NonZero)
		r.DrawImage(img, 0, 0, W, H, White)
		r.PopClip()
	})
	blue := func(x, y int) bool { return pix[(y*W+x)*4+2] > 128 }
	if !blue(30, 30) {
		t.Error("the middle of the diamond should be painted")
	}
	for _, p := range [][2]int{{3, 3}, {56, 3}, {3, 56}, {56, 56}} {
		if blue(p[0], p[1]) {
			t.Errorf("corner (%d,%d) is outside the diamond and should be cut", p[0], p[1])
		}
	}
}

// An empty or degenerate clip path clips everything away, which is what a
// degenerate clip means — not "no clip".
func TestEmptyClipPathClipsAll(t *testing.T) {
	const W, H = 40, 40
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.PushClipPath(NewPath(), NonZero)
		r.FillRect(0, 0, W, H, RGBA{R: 1, A: 1})
		r.PopClip()
	})
	for y := 0; y < H; y += 8 {
		for x := 0; x < W; x += 8 {
			if pix[(y*W+x)*4] > 128 {
				t.Fatalf("(%d,%d) was painted through an empty clip", x, y)
			}
		}
	}
}

// A clip inside a transform is stated in the CALLER's coordinates, so it
// has to travel through the transform stack like the geometry it clips:
// under a translation, the clip moves with the content. A layer's offscreen
// pass draws its child that way (window coordinates shifted onto the
// target), so a card that clips its own photo landed 20px off before this.
func TestPushClipFollowsTheTransform(t *testing.T) {
	const W, H = 80, 80
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		// The content is drawn at (0,0) and translated to (40,40); the clip
		// names the same untranslated box, so the visible red square must
		// be the translated one.
		r.PushTransform(Translate2(40, 40))
		r.PushClip(0, 0, 20, 20)
		r.FillRect(0, 0, W, H, RGBA{R: 1, A: 1})
		r.PopClip()
		r.PopTransform()
	})
	red := func(x, y int) bool { return pix[(y*W+x)*4] > 128 }
	if !red(50, 50) {
		t.Errorf("inside the translated clip is not painted")
	}
	if red(10, 10) {
		t.Errorf("the untranslated box is painted: the clip ignored the transform")
	}
	if red(70, 70) {
		t.Errorf("past the translated clip is painted")
	}
}

// PreserveAspectCrop covers the box and clips the overflow, where a fit
// letterboxes inside it: a tall source in a square box fills the whole box
// (its top and bottom cut off) instead of leaving bars at the sides.
func TestDrawImageCroppedCoversTheBox(t *testing.T) {
	const W, H = 40, 40
	// A 1x4 red source: fitting it into the square would paint one narrow
	// column, cropping it paints the whole square.
	src := make([]byte, 1*4*4)
	for i := 0; i < 4; i++ {
		src[i*4], src[i*4+3] = 255, 255
	}
	fill := func(draw func(*Renderer2D, *Image)) int {
		pix := drawClipped(t, W, H, func(r *Renderer2D) {
			img := r.NewImage(1, 4, src)
			draw(r, img)
		})
		n := 0
		for i := 0; i < W*H; i++ {
			if pix[i*4] > 128 {
				n++
			}
		}
		return n
	}
	cropped := fill(func(r *Renderer2D, img *Image) {
		r.DrawImageCropped(img, 0, 0, W, H, White)
	})
	fitted := fill(func(r *Renderer2D, img *Image) {
		r.DrawImageFit(img, 0, 0, W, H, White)
	})
	if cropped < W*H*9/10 {
		t.Errorf("cropped covers %d of %d pixels, want the whole box", cropped, W*H)
	}
	if fitted >= cropped/2 {
		t.Errorf("fitted covers %d pixels and cropped %d: the crop did not cover more", fitted, cropped)
	}
}

// A blurred colorized composite is both at once: the glow spreads AND
// every texel takes the colour's hue at the source's own luminance. A
// white highlight inside the source must come out coloured — leaving it
// white is what a tint multiply does, and it is visible as a pale blob in
// the middle of a neon glow.
func TestDrawImageBlurredColorizedRecolours(t *testing.T) {
	const W, H = 40, 40
	// One white texel: under a teal colorization no channel may stay at
	// white's own red level.
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		img := r.NewImage(1, 1, []byte{255, 255, 255, 255})
		r.DrawImageBlurredColorized(img, 8, 8, 24, 24, 6, RGBA{R: 0, G: 1, B: 0.66, A: 1})
	})
	at := func(x, y int) (int, int, int) {
		i := (y*W + x) * 4
		return int(pix[i]), int(pix[i+1]), int(pix[i+2])
	}
	cr, cg, cb := at(20, 20)
	if cr > 60 {
		t.Errorf("centre is (%d,%d,%d): the white source was not recoloured", cr, cg, cb)
	}
	if cg < 100 || cb < 60 {
		t.Errorf("centre is (%d,%d,%d): the colorization colour did not come through", cr, cg, cb)
	}
	// And it spreads: a pixel outside the source rect is lit by the blur.
	if _, og, _ := at(20, 4); og == 0 {
		t.Errorf("nothing outside the box: the blur did not spread")
	}
}

// A vector source honours fillMode too: Image.Stretch scales the two axes
// INDEPENDENTLY, filling its box. The uniform scale cannot express that —
// it letterboxes — so the corpus' FillMode demo drew its stretched Qt logo
// 22px short of its frame.
func TestDrawSVGScaledFillsBothAxes(t *testing.T) {
	const W, H = 60, 30
	svg, err := ParseSVG([]byte(`<svg viewBox="0 0 10 10"><rect x="0" y="0" width="10" height="10" fill="#ffffff"/></svg>`))
	if err != nil {
		t.Fatalf("ParseSVG: %v", err)
	}
	filled := func(pix []byte) (minx, maxx, miny, maxy int) {
		minx, miny = W, H
		maxx, maxy = -1, -1
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				if pix[(y*W+x)*4] > 128 {
					minx, maxx = min(minx, x), max(maxx, x)
					miny, maxy = min(miny, y), max(maxy, y)
				}
			}
		}
		return
	}
	// Stretch: the 1:1 source covers the whole 2:1 box.
	stretched := drawClipped(t, W, H, func(r *Renderer2D) {
		r.DrawSVGScaled(svg, 0, 0, W/10, H/10, 0, White)
	})
	x0, x1, y0, y1 := filled(stretched)
	if x0 > 1 || x1 < W-2 || y0 > 1 || y1 < H-2 {
		t.Errorf("stretched covers x %d..%d y %d..%d, want the whole %dx%d box", x0, x1, y0, y1, W, H)
	}
	// A uniform scale letterboxes: it can only reach the shorter axis.
	fitted := drawClipped(t, W, H, func(r *Renderer2D) {
		s := float32(H) / 10
		r.DrawSVG(svg, (W-10*s)/2, 0, s, White)
	})
	fx0, fx1, _, _ := filled(fitted)
	if fx1-fx0 > W/2 {
		t.Errorf("the uniform scale covered x %d..%d — it should letterbox", fx0, fx1)
	}
}

// A gradient interpolates PREMULTIPLIED, as Qt's and CSS's gradients do.
// A plain component lerp mixes a colour a viewer never sees: from opaque
// green to a FAINT red, the unpremultiplied midpoint is full red at half
// alpha, so a bright red band appears in the middle of the ramp (measured
// 129 of red where the end stop only ever contributes 51). Premultiplied,
// the red rises monotonically to the alpha the stop actually carries — and
// a stop fading to transparent then composites straight down to the
// background instead of curving away twice as fast, which is what the top
// of the lenstabs page-wide glow showed.
func TestGradientInterpolatesPremultiplied(t *testing.T) {
	const W, H = 8, 64
	green := RGBA{G: 1, A: 1}
	faintRed := RGBA{R: 1, A: 0.2}
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.GradientRect(0, 0, W, H, 0, green, faintRed, math.Pi/2)
	})
	red := func(y int) int { return int(pix[(y*W+W/2)*4]) }
	greenAt := func(y int) int { return int(pix[(y*W+W/2)*4+1]) }
	// Red only ever arrives as the end stop's 0.2 alpha over black: ~51.
	prev := -1
	for _, y := range []int{1, 8, 16, 24, 32, 40, 48, 56, 62} {
		r := red(y)
		if r > 60 {
			t.Errorf("red = %d at y=%d: the ramp went through a colour neither stop has", r, y)
		}
		if r < prev-2 {
			t.Errorf("red fell from %d to %d at y=%d: the ramp is not monotonic", prev, r, y)
		}
		prev = r
	}
	// The green half runs straight down as the alpha falls.
	if g, want := greenAt(H/2), 125; g < want-12 || g > want+12 {
		t.Errorf("middle green = %d, want ~%d", g, want)
	}
}

// A path clip has to bound a RING FAN too — a canvas radial gradient is
// painted as its own circle, and the shape being filled is routinely much
// smaller than the gradient's radius (a cloud is a small ellipse painted
// from a ramp as wide as the layer). Without the clip the ramp itself
// became the cloud.
func TestClipPathBoundsARadialRing(t *testing.T) {
	const W, H = 64, 64
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		// The clip: a small square in the middle.
		// Deliberately NOT closed: a canvas builds a path with
		// beginPath/ellipse/fill and never closes it, and an unflushed
		// subpath used to leave the clip empty — which hides everything.
		box := NewPath()
		box.MoveTo(24, 24).LineTo(40, 24).LineTo(40, 40).LineTo(24, 40)
		r.PushClipPath(box, NonZero)
		r.FillRadialRing(32, 32, 0, 30, []GradientStop{
			{Pos: 0, Col: RGBA{R: 1, A: 1}}, {Pos: 1, Col: RGBA{R: 1, A: 1}},
		})
		r.PopClip()
	})
	red := func(x, y int) int { return int(pix[(y*W+x)*4]) }
	if got := red(32, 32); got < 200 {
		t.Errorf("inside the clip = %d, want the ring's red", got)
	}
	if got := red(10, 32); got > 30 {
		t.Errorf("outside the clip = %d, want ~0", got)
	}
}

// A gradient of MORE than four stops keeps every one of them: a quad's
// ramp holds four, so the extra stops are drawn as bands. Collapsing them
// instead ran the corpus' five-stop mist straight from its third stop to
// the end, leaving it twice as bright through the bottom third.
func TestGradientQuadStopsNKeepsEveryStop(t *testing.T) {
	const W, H = 8, 100
	stops := []GradientStop{
		{Pos: 0.00, Col: RGBA{}},
		{Pos: 0.45, Col: RGBA{G: 1, A: 0.15}},
		{Pos: 0.65, Col: RGBA{G: 1, A: 0.25}},
		{Pos: 0.80, Col: RGBA{G: 1, A: 0.10}},
		{Pos: 1.00, Col: RGBA{}},
	}
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.GradientQuadStopsN(0, 0, W, H, stops, math.Pi/2, 0, 0, RGBA{})
	})
	green := func(y int) int { return int(pix[(y*W+W/2)*4+1]) }
	// The dip at 0.80 is the stop a four-stop ramp had to drop: it must be
	// lower than the peak at 0.65 and lower than the midpoint between them.
	peak, dip := green(65), green(80)
	if dip >= peak {
		t.Errorf("0.80 = %d, 0.65 = %d: the fourth stop was dropped", dip, peak)
	}
	if mid := green(72); mid <= dip || mid >= peak {
		t.Errorf("between the stops = %d, want between %d and %d", mid, dip, peak)
	}
	if e := green(2); e > 12 {
		t.Errorf("the transparent end = %d, want ~0", e)
	}
}
