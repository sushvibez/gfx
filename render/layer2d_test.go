//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/gpu"
)

// Compositing the taps in order, each with its own alpha over the ones
// before it, must leave the weighted average of all of them — that is what
// separates a blur from a stack of offset ghosts.
func TestBlurTapAlphasAverage(t *testing.T) {
	alphas := blurTapAlphas()
	if len(alphas) != len(blurTaps) {
		t.Fatalf("got %d alphas for %d taps", len(alphas), len(blurTaps))
	}
	// Each tap carries a distinct value; the composite must land on the
	// weight-weighted mean of those values.
	var want, total float32
	for i, tap := range blurTaps {
		want += tap[2] * float32(i)
		total += tap[2]
	}
	want /= total

	var got float32 // running "over" composite of the taps
	for i, a := range alphas {
		if a <= 0 || a > 1 {
			t.Fatalf("tap %d alpha %v out of range", i, a)
		}
		got = a*float32(i) + (1-a)*got
	}
	if diff := got - want; diff > 1e-4 || diff < -1e-4 {
		t.Errorf("composite = %v, want the weighted average %v", got, want)
	}
}

// A blur radius picks a downscale that keeps the offscreen target small
// without collapsing it: bigger radii render smaller, and it never reaches
// zero (a zero-sized target cannot be allocated).
func TestBlurDownscaleRange(t *testing.T) {
	last := 0
	for _, r := range []float32{0, 1, 2, 4, 8, 16, 32, 64, 200} {
		ds := BlurDownscale(r)
		if ds < 1 || ds > 8 {
			t.Fatalf("BlurDownscale(%v) = %d, want 1..8", r, ds)
		}
		if ds < last {
			t.Errorf("BlurDownscale(%v) = %d, smaller than the previous %d", r, ds, last)
		}
		last = ds
	}
}

// A TRANSLUCENT captured layer composites at its own alpha. An offscreen
// target holds PREMULTIPLIED pixels, so blending them with a straight-alpha
// equation multiplies by the alpha twice — a blob painted at 0.3 came out
// at 0.084, a third of Qt's — and a blur over them has to be a weighted SUM
// (drawn additively into a cleared target), where compositing the taps over
// one another gains alpha that was never there (0.3 became 0.55 in one
// pass). Both together put the corpus' ambient blobs a factor of two out in
// either direction.
func TestTranslucentLayerCompositesAtItsOwnAlpha(t *testing.T) {
	const W, H = 64, 64
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	// A layer holding a half-transparent red square, blurred a little.
	l := NewLayer2D(r)
	defer l.Release()
	l.Resize(W, H)
	if err := l.Render(func() {
		r.FillRect(8, 8, W-16, H-16, RGBA{R: 1, A: 0.3})
	}); err != nil {
		t.Fatalf("layer render: %v", err)
	}
	if err := l.BlurTexture(2); err != nil {
		t.Fatalf("blur: %v", err)
	}

	r.Begin(W, H)
	r.DrawImagePremul(l.Image(), 0, 0, W, H, White)
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1}) // black, opaque
	if err := r.Flush(pass); err != nil {
		t.Fatalf("flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	// Over black, alpha 0.3 of pure red is ~77. Squaring the alpha gives
	// ~23; accumulating the blur taps "over" one another gives ~140.
	got := int(pix[(32*W+32)*4])
	if got < 62 || got > 92 {
		t.Errorf("centre red = %d, want ~77 (alpha 0.3 over black)", got)
	}
}

// TestStencilDrawPaintsTheShapeNotTheContent: a drop shadow is the
// SILHOUETTE of what casts it. A layer holding an opaque BLACK square,
// drawn through the stencil path in warm brown, must come out brown —
// tinting the texture instead multiplies the shadow colour by the content,
// so anything dark cast a black shadow however warm the colour (the paper
// diorama's trees are black and their shadows rgba(.08,.02,0,.7)).
func TestStencilDrawPaintsTheShapeNotTheContent(t *testing.T) {
	const W, H = 64, 64
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	l := NewLayer2D(r)
	defer l.Release()
	l.Resize(W, H)
	if err := l.Render(func() {
		r.FillRect(16, 16, 32, 32, RGBA{A: 1}) // opaque black
	}); err != nil {
		t.Fatalf("layer render: %v", err)
	}

	r.Begin(W, H)
	r.DrawImageStencil(l.Image(), 0, 0, W, H, RGBA{R: 1, G: 0.5, B: 0, A: 1})
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1}) // black, opaque
	if err := r.Flush(pass); err != nil {
		t.Fatalf("flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	i := (32*W + 32) * 4
	rr, gg, bb := int(pix[i]), int(pix[i+1]), int(pix[i+2])
	if rr < 200 || gg < 100 || gg > 160 || bb > 40 {
		t.Errorf("inside the silhouette = (%d,%d,%d), want the orange it was drawn in", rr, gg, bb)
	}
	// Outside the square there is no coverage, so nothing is painted.
	j := (4*W + 4) * 4
	if int(pix[j]) > 8 {
		t.Errorf("outside the silhouette = %d, want the black background", pix[j])
	}
}

// TestBlurredLayerSpreadsPastItsItem: Qt's MultiEffect pads the effect item
// by its blur (autoPaddingEnabled, on by default), so a blurred blob fades
// on OUTSIDE the rectangle that cast it. Ours blurred inside the item's own
// texture, so a 200px circle blurred by 60 stopped dead at the circle's
// edge where Qt's carries on for another 60px — probed against Qt, whose
// spread runs 290..600 for a circle at 350..550.
func TestBlurredLayerSpreadsPastItsItem(t *testing.T) {
	const W, H = 200, 200
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	// A layer the size of a 60x60 square, PADDED by the blur radius: what
	// ui.LayerEl allocates for a blurred item.
	const pad = 20
	l := NewLayer2D(r)
	defer l.Release()
	l.Resize(60+2*pad, 60+2*pad)
	if err := l.Render(func() {
		r.FillRect(pad, pad, 60, 60, RGBA{R: 1, A: 1})
	}); err != nil {
		t.Fatalf("layer render: %v", err)
	}
	if err := l.BlurTexture(10); err != nil {
		t.Fatalf("blur: %v", err)
	}
	if err := l.BlurTexture(10); err != nil {
		t.Fatalf("blur: %v", err)
	}

	r.Begin(W, H)
	// Composited at the padded rect: the square sits at 70..130, so the
	// padding covers 50..150.
	r.DrawImagePremul(l.Image(), 50, 50, 60+2*pad, 60+2*pad, White)
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	at := func(x, y int) int { return int(pix[(y*W+x)*4]) }
	if in := at(100, 100); in < 120 {
		t.Errorf("centre = %d, want the blurred square", in)
	}
	// 8px OUTSIDE the square's own edge — inside the padding — the blur
	// must still be there.
	if out := at(100, 62); out < 10 {
		t.Errorf("8px past the item's edge = %d, want the blur spreading into the padding", out)
	}
	// Well outside the padded rect, nothing.
	if far := at(100, 20); far > 6 {
		t.Errorf("far outside = %d, want the background", far)
	}
}

// A blur is a weighted average, so it must leave a flat region alone: the
// chain sums its levels additively into an 8-bit target and every tap is
// written back a shade low, which dimmed a solid white layer to 250/255
// until addGain compensated for it.
func TestBlurChainPreservesFlats(t *testing.T) {
	const W, H = 128, 128
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	l := NewLayer2D(r)
	defer l.Release()
	l.Resize(W, H)
	for _, radius := range []float32{8, 16, 32} {
		if err := l.Render(func() { r.FillRect(0, 0, W, H, RGBA{R: 1, G: 1, B: 1, A: 1}) }); err != nil {
			t.Fatal(err)
		}
		if err := l.BlurChain(radius); err != nil {
			t.Fatal(err)
		}
		if got := layerCentre(t, device, queue, r, l); got < 254 {
			t.Errorf("radius %v: flat white came back %d, want 255", radius, got)
		}
	}
}

// The chain must actually BLUR, not just soften an edge: halving alone
// gives the sampler's 2x2 box, which upscales back to a square (probed —
// a 32px square through a plain chain came back square).
func TestBlurChainSpreads(t *testing.T) {
	const W, H = 128, 128
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	l := NewLayer2D(r)
	defer l.Release()
	l.Resize(W, H)
	if err := l.Render(func() { r.FillRect(48, 48, 32, 32, RGBA{R: 1, G: 1, B: 1, A: 1}) }); err != nil {
		t.Fatal(err)
	}
	if err := l.BlurChain(16); err != nil {
		t.Fatal(err)
	}
	row := layerRow(t, device, queue, r, l, 64)
	// The square's own edge is at x=48: a blur reaches well past it and
	// falls off smoothly rather than stepping.
	if row[40] < 8 {
		t.Errorf("8px outside the square = %d, want the blur reaching out", row[40])
	}
	if row[24] > 8 {
		t.Errorf("24px outside the square = %d, want the falloff finished", row[24])
	}
	if row[64] <= row[48] || row[48] <= row[40] {
		t.Errorf("falloff is not monotone: %d %d %d", row[40], row[48], row[64])
	}
}

// layerRow paints the layer's image over black and reads one scanline.
func layerRow(t *testing.T, device *gpu.Device, queue *gpu.Queue, r *Renderer2D, l *Layer2D, y int) []int {
	t.Helper()
	w, h := l.Size()
	target := device.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r.Begin(w, h)
	r.DrawImagePremul(l.Image(), 0, 0, float32(w), float32(h), White)
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatal(err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	row := make([]int, w)
	for x := range row {
		row[x] = int(pix[(y*w+x)*4])
	}
	return row
}

func layerCentre(t *testing.T, device *gpu.Device, queue *gpu.Queue, r *Renderer2D, l *Layer2D) int {
	t.Helper()
	w, _ := l.Size()
	return layerRow(t, device, queue, r, l, w/2)[w/2]
}
