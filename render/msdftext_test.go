//go:build !js

package render

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/gpu"
)

// renderRun draws one string into a fresh target and hands back the
// pixels, with the renderer left alive for whoever wants to look at it.
func renderRun(t *testing.T, r *Renderer2D, device *gpu.Device, queue *gpu.Queue,
	w, h int, draw func()) []byte {
	t.Helper()
	target := device.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	r.Begin(w, h)
	draw()
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

// ink is the total coverage in a white-on-black render, and the box it
// falls in.
func ink(pix []byte, w, h int) (sum float64, x0, y0, x1, y1 int) {
	x0, y0, x1, y1 = w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := float64(pix[(y*w+x)*4]) / 255
			sum += v
			if v > 0.5 {
				x0, y0 = min(x0, x), min(y0, y)
				x1, y1 = max(x1, x+1), max(y1, y+1)
			}
		}
	}
	return
}

const runSample = "Hamburgefonstiv"

// One device and one renderer for the lot: every test in this package
// opens its own and the driver has a ceiling, which the three of these
// were enough to reach.
func TestMSDFText(t *testing.T) {
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	r.SetMSDFText(true)
	if !r.MSDFTextEnabled() {
		t.Skip("no distance-field pipeline on this device")
	}
	r.SetSubpixelText(false)

	t.Run("matches the raster path", func(t *testing.T) { msdfMatchesRaster(t, r, device, queue) })
	t.Run("one sheet for every size", func(t *testing.T) { msdfOneSheet(t, r, device, queue) })
	t.Run("outline rings the glyph", func(t *testing.T) { msdfOutline(t, r, device, queue) })
	t.Run("two settings in one frame", func(t *testing.T) { msdfTwoSettings(t, r, device, queue) })
	t.Run("a glow wider than the padding", func(t *testing.T) { msdfGlowTapers(t, r, device, queue) })
	t.Run("one weight at every size", func(t *testing.T) { msdfWeightScales(t, r, device, queue) })
	t.Run("body sizes stay crisp", func(t *testing.T) { msdfSmallText(t, r, device, queue) })
	t.Run("magnified and rotated", func(t *testing.T) { msdfNotAtRasterSize(t, r, device, queue) })
}

// The distance field has to draw the same text as the atlas it replaces:
// same place, same size, and within a few per cent of the same amount of
// ink. It will never be pixel-identical — a coverage mask is hinted to
// the pixel grid and a field is not — but a glyph that is a tenth wider,
// or sitting a pixel low, is a bug in the metrics rather than a
// difference in antialiasing.
func msdfMatchesRaster(t *testing.T, r *Renderer2D, device *gpu.Device, queue *gpu.Queue) {
	white := RGBA{R: 1, G: 1, B: 1, A: 1}
	for _, size := range []float64{14, 32, 64} {
		f, err := r.NewFont(goregular.TTF, size)
		if err != nil {
			t.Fatalf("NewFont: %v", err)
		}
		w, h := int(size*11), int(size*2.5)
		want, _ := r.MeasureText(f, runSample)

		r.SetMSDFText(false)
		r.SetMSDFAuto(false) // the raster, transformed or not
		raster := renderRun(t, r, device, queue, w, h, func() {
			r.DrawText(f, 4, 4, runSample, white)
		})
		r.SetMSDFText(true)
		field := renderRun(t, r, device, queue, w, h, func() {
			r.DrawText(f, 4, 4, runSample, white)
		})

		rs, rx0, ry0, rx1, ry1 := ink(raster, w, h)
		fs, fx0, fy0, fx1, fy1 := ink(field, w, h)
		if fs == 0 {
			t.Fatalf("%gpx: the field path drew nothing", size)
		}
		// Ink within a tenth. The field is unhinted, so its stems are
		// a fraction thinner at small sizes.
		if d := math.Abs(fs-rs) / rs; d > 0.12 {
			t.Errorf("%gpx: field ink %.0f, raster %.0f (%.0f%% apart)", size, fs, rs, d*100)
		}
		// The run lands in the same box, to a pixel and a bit.
		tol := 2
		if size >= 48 {
			tol = 3
		}
		for _, c := range []struct {
			what     string
			got, wnt int
		}{{"left", fx0, rx0}, {"top", fy0, ry0}, {"right", fx1, rx1}, {"bottom", fy1, ry1}} {
			if d := c.got - c.wnt; d > tol || d < -tol {
				t.Errorf("%gpx: the field run's %s edge is at %d, the raster's at %d",
					size, c.what, c.got, c.wnt)
			}
		}
		// And it is still the width MeasureText promised the layout.
		if got := float64(fx1 - fx0); math.Abs(got-float64(want)) > float64(want)*0.06+2 {
			t.Errorf("%gpx: the field run is %.0fpx of ink, MeasureText said %.0f", size, got, want)
		}
		t.Logf("%gpx: ink field %.0f raster %.0f, box field %d..%d raster %d..%d",
			size, fs, rs, fx0, fx1, rx0, rx1)

		if size == 32 {
			img := &image.RGBA{Pix: field, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
			out := filepath.Join(t.TempDir(), "msdf-32.png")
			if fh, err := os.Create(out); err == nil {
				_ = png.Encode(fh, img)
				fh.Close()
				t.Logf("screenshot: %s", out)
			}
		}
		f.Release()
	}
}

// The reason for the whole exercise: a family that draws at eight sizes
// builds ONE sheet, where the raster atlas builds eight.
func msdfOneSheet(t *testing.T, r *Renderer2D, device *gpu.Device, queue *gpu.Queue) {
	r.SetMSDFText(true)
	base, err := r.NewFont(goregular.TTF, 16)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer base.Release()

	sizes := []float64{11, 13, 16, 20, 28, 34, 48, 72}
	renderRun(t, r, device, queue, 64, 64, func() {
		for _, s := range sizes {
			r.DrawText(base.AtSize(s), 2, 2, runSample, RGBA{R: 1, G: 1, B: 1, A: 1})
		}
	})

	runes := map[rune]bool{}
	for _, c := range runSample {
		runes[c] = true
	}
	if n := len(base.fam.atlas.Glyphs); n != len(runes) {
		t.Errorf("the sheet holds %d tiles for %d distinct runes", n, len(runes))
	}
	// Every size shares that one sheet, and none of them rasterised a
	// coverage atlas of its own.
	rastered := len(base.glyphs)
	for _, s := range sizes {
		g := base.AtSize(s)
		if g.fam != base.fam {
			t.Errorf("%gpx built its own sheet", s)
		}
		rastered += len(g.glyphs)
	}
	if rastered != 0 {
		t.Errorf("%d glyphs were still rasterised into per-size atlases", rastered)
	}
	// And no size allocated a sheet it never wrote to. A sheet is a
	// megabyte of RGBA and another on the GPU; charging that to every
	// size that only ever drew through the field ran a demo out of
	// video memory.
	for _, s := range append([]float64{0}, sizes...) {
		g := base
		if s > 0 {
			g = base.AtSize(s)
		}
		if len(g.pages) != 0 {
			t.Errorf("%gpx allocated a rasterised sheet without rasterising anything", s)
		}
	}
	sheets, glyphs, rbytes, tiles, fbytes := base.AtlasStats()
	if sheets != 0 || glyphs != 0 || rbytes != 0 || tiles != len(runes) {
		t.Errorf("AtlasStats = %d sheets, %d glyphs, %d bytes, %d tiles; want 0, 0, 0, %d",
			sheets, glyphs, rbytes, tiles, len(runes))
	}
	if fbytes == 0 {
		t.Error("the field sheet reports no bytes")
	}
	t.Logf("%d sizes, %d runes, %d tiles in one %d-square sheet",
		len(sizes), len(runes), len(base.fam.atlas.Glyphs), 1024)
}

// An outline is a second threshold of the same field — something a
// coverage atlas cannot do without rasterising a second set of glyphs.
func msdfOutline(t *testing.T, r *Renderer2D, device *gpu.Device, queue *gpu.Queue) {
	r.SetMSDFText(true)
	f, err := r.NewFont(goregular.TTF, 64)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	const W, H = 200, 120
	white := RGBA{R: 1, G: 1, B: 1, A: 1}
	plain := renderRun(t, r, device, queue, W, H, func() {
		r.DrawText(f, 10, 10, "O", white)
	})
	r.SetTextOutline(0.05, RGBA{R: 1, A: 1}) // red ring, 0.05em = 3.2px at 64
	ringed := renderRun(t, r, device, queue, W, H, func() {
		r.DrawText(f, 10, 10, "O", white)
	})
	r.SetTextOutline(0, RGBA{})

	// Red where the plain render had nothing: that is the ring.
	ring := 0
	for i := 0; i < W*H; i++ {
		red := pixAt(ringed, i)
		if red[0] > 200 && red[1] < 80 && red[2] < 80 && pixAt(plain, i)[0] < 40 {
			ring++
		}
	}
	if ring < 100 {
		t.Errorf("the outline covers %d pixels outside the glyph, expected a ring", ring)
	}
	// The fill is still white in the middle of the stroke.
	ps, _, _, _, _ := ink(plain, W, H)
	rs, _, _, _, _ := ink(ringed, W, H)
	if rs <= ps {
		t.Errorf("the outlined glyph carries no more ink (%.0f) than the plain one (%.0f)", rs, ps)
	}
	t.Logf("a 0.05em outline adds %d ring pixels and %.0f coverage", ring, rs-ps)
}

func pixAt(pix []byte, i int) [3]byte {
	return [3]byte{pix[i*4], pix[i*4+1], pix[i*4+2]}
}

// An outlined heading and plain body text in the same frame. The
// parameters are renderer state but the frame is recorded and submitted
// in one go, so a single shared uniform would hand every batch in it
// whatever was set last — which outlined the body text too.
func msdfTwoSettings(t *testing.T, r *Renderer2D, device *gpu.Device, queue *gpu.Queue) {
	r.SetMSDFText(true)
	f, err := r.NewFont(goregular.TTF, 48)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	const W, H = 300, 180
	white := RGBA{R: 1, G: 1, B: 1, A: 1}
	pix := renderRun(t, r, device, queue, W, H, func() {
		r.SetTextOutline(0.06, RGBA{R: 1, A: 1}) // red
		r.DrawText(f, 10, 10, "OO", white)
		r.SetTextOutline(0, RGBA{})
		r.DrawText(f, 10, 90, "OO", white)
	})
	r.SetTextOutline(0, RGBA{})

	red := func(y0, y1 int) int {
		n := 0
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				p := pixAt(pix, y*W+x)
				if p[0] > 150 && p[1] < 80 && p[2] < 80 {
					n++
				}
			}
		}
		return n
	}
	if top := red(0, 88); top < 100 {
		t.Errorf("the outlined line has %d red pixels, expected a ring", top)
	}
	if bottom := red(90, H); bottom != 0 {
		t.Errorf("the plain line picked up %d pixels of the other line's outline", bottom)
	}
}

// A glow or an outline is asking for ink outside the glyph, and the
// field only reaches as far as the padding — four texels, which at 20 px
// is barely two pixels of screen. Past that the distance saturates, and
// the ink used to cut off square: a faint rectangle round each glyph,
// the shape of its tile.
//
// An 'O' with a glow is round. If it has corners, the tile is showing.
func msdfGlowTapers(t *testing.T, r *Renderer2D, device *gpu.Device, queue *gpu.Queue) {
	r.SetMSDFText(true)
	f, err := r.NewFont(goregular.TTF, 20)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	const W, H = 120, 80
	// Past MaxTextReachEm, so the taper is what is under test.
	r.SetTextSoftness(0.6)
	pix := renderRun(t, r, device, queue, W, H, func() {
		r.DrawText(f, 40, 30, "O", RGBA{R: 1, G: 1, B: 1, A: 1})
	})
	r.SetTextSoftness(0)
	plain := renderRun(t, r, device, queue, W, H, func() {
		r.DrawText(f, 40, 30, "O", RGBA{R: 1, G: 1, B: 1, A: 1})
	})

	// The core has to stay as solid as it was. A glow is a halo around
	// the glyph, not a wider antialias ramp: widening the ramp was the
	// first attempt, and since coverage only reaches 1 a ramp-width
	// inside the outline, a two-pixel stem under a five-pixel ramp came
	// out at half alpha — the text faded instead of glowing, which is
	// exactly what it looked like.
	var peakGlow, peakPlain float64
	for i := 0; i < W*H; i++ {
		if v := float64(plain[i*4]) / 255; v > peakPlain {
			peakPlain = v
			peakGlow = float64(pix[i*4]) / 255
		}
	}
	if peakGlow < peakPlain-0.02 {
		t.Errorf("the glow thinned the glyph itself: core %.2f with, %.2f without", peakGlow, peakPlain)
	}

	lum := func(x, y int) float64 { return float64(pix[(y*W+x)*4]) / 255 }
	x0, y0, x1, y1 := W, H, -1, -1
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			if lum(x, y) > 0.02 {
				x0, y0 = min(x0, x), min(y0, y)
				x1, y1 = max(x1, x), max(y1, y)
			}
		}
	}
	if x1 < 0 {
		t.Fatal("the glowing glyph drew nothing")
	}
	// The brightest corner of the lit box against the middle of its top
	// edge. On a round glyph the corners are the far side of the glow
	// and the edge midpoint is the near side, so the corners must be
	// clearly dimmer. A tile cut-off makes them equal.
	corner := 0.0
	for _, c := range [4][2]int{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}} {
		corner = math.Max(corner, lum(c[0], c[1]))
	}
	mid := math.Max(lum((x0+x1)/2, y0), lum((x0+x1)/2, y1))
	if corner > mid*0.6 {
		t.Errorf("the glow's corners are as bright as its edges (%.3f vs %.3f) — the tile is showing",
			corner, mid)
	}
	t.Logf("glow corner %.3f, edge %.3f", corner, mid)
}

// The weight is in ems, so one setting has to mean the same thing at
// every size: the same amount of bold, in proportion.
//
// Screen pixels were the first unit and the wrong one. 1.6 px added to
// each side of a stem is a hairline on a 96 px heading and closes the
// counters of a 22 px word into blobs — which is exactly what it looked
// like.
func msdfWeightScales(t *testing.T, r *Renderer2D, device *gpu.Device, queue *gpu.Queue) {
	r.SetMSDFText(true)
	base, err := r.NewFont(goregular.TTF, 24)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer base.Release()

	white := RGBA{R: 1, G: 1, B: 1, A: 1}
	gain := func(px float32) float64 {
		w, h := int(px*8), int(px*2.2)
		r.SetTextWeight(0)
		plain := renderRun(t, r, device, queue, w, h, func() {
			r.DrawTextSize(base, 4, 4, "Hamburgefon", white, px)
		})
		r.SetTextWeight(0.035)
		bold := renderRun(t, r, device, queue, w, h, func() {
			r.DrawTextSize(base, 4, 4, "Hamburgefon", white, px)
		})
		r.SetTextWeight(0)
		p, _, _, _, _ := ink(plain, w, h)
		b, _, _, _, _ := ink(bold, w, h)
		if p == 0 {
			t.Fatalf("%gpx drew nothing", px)
		}
		return b / p
	}
	small, large := gain(24), gain(96)
	// Not identical — a stem gains a fixed fraction of an em on each
	// side, and how much ink that is depends on the glyph — but the same
	// to within a few per cent, where pixels were out by a factor.
	if d := math.Abs(small-large) / large; d > 0.08 {
		t.Errorf("0.035em of weight adds %.0f%% of ink at 24px and %.0f%% at 96px",
			(small-1)*100, (large-1)*100)
	}
	if small < 1.05 {
		t.Errorf("0.035em of weight added almost nothing at 24px (x%.3f)", small)
	}
	t.Logf("0.035em of weight: x%.3f at 24px, x%.3f at 96px", small, large)
}

// contrast is how hard the edges are: the mean distance of an inked
// pixel from mid-grey, doubled, so 1.0 is a hard mask and 0 is a render
// where everything came out at 50 %. It is the number that separates
// the two text paths at body sizes — they lay down the same amount of
// ink either way, and what differs is how many pixels it is spread
// over.
func contrast(pix []byte, w, h int) float64 {
	sum, n := 0.0, 0
	for i := 0; i < w*h; i++ {
		v := float64(pix[i*4]) / 255
		if v > 0.02 {
			sum += math.Abs(v-0.5) * 2
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// Small text was the one thing the field path did worse, and this is
// the test that says it no longer does.
//
// The complaint was never the amount of ink — measured below, the two
// paths agree on that to about a per cent all the way down to 10 px.
// It was that a rasterised mask is hinted, so an 11 px stem lands on
// the pixel grid and one pixel goes dark, where the field draws the
// same stem straddling two pixels, correctly, and it reads as mush.
//
// The shader narrows its antialias ramp below 20 px/em to compensate
// (see smallSharpPPEM). The ramp stays centred on the outline, so this
// buys contrast without changing the weight — which is exactly what
// the two assertions here check, one each.
func msdfSmallText(t *testing.T, r *Renderer2D, device *gpu.Device, queue *gpu.Queue) {
	white := RGBA{R: 1, G: 1, B: 1, A: 1}
	for _, size := range []float64{10, 11, 12, 13, 14, 16} {
		f, err := r.NewFont(goregular.TTF, size)
		if err != nil {
			t.Fatalf("NewFont: %v", err)
		}
		w, h := int(size*11), int(size*2.5)

		r.SetMSDFText(false)
		r.SetMSDFAuto(false) // the raster, transformed or not
		raster := renderRun(t, r, device, queue, w, h, func() { r.DrawText(f, 4, 4, runSample, white) })
		r.SetMSDFText(true)
		field := renderRun(t, r, device, queue, w, h, func() { r.DrawText(f, 4, 4, runSample, white) })

		rs, _, _, _, _ := ink(raster, w, h)
		fs, _, _, _, _ := ink(field, w, h)
		rc, fc := contrast(raster, w, h), contrast(field, w, h)
		t.Logf("%.0fpx: ink field %.0f raster %.0f (%+.1f%%), contrast field %.3f raster %.3f",
			size, fs, rs, 100*(fs-rs)/rs, fc, rc)

		// Same weight: the sharpening must not have thickened the text.
		if d := math.Abs(fs-rs) / rs; d > 0.05 {
			t.Errorf("%.0fpx: field ink %.0f against the raster's %.0f (%.0f%% apart) — "+
				"the ramp is off centre, not just narrower", size, fs, rs, d*100)
		}
		// And as hard an edge as the path it replaces. A little slack:
		// hinting quantises, so the raster wins or loses a few
		// hundredths depending on where the stems happen to fall.
		if fc < rc-0.05 {
			t.Errorf("%.0fpx: field contrast %.3f against the raster's %.3f — small text is soft again",
				size, fc, rc)
		}
		f.Release()
	}
}

// The reason to want the field path, stated as a measurement: text
// that is not drawn at the size it was rasterised at.
//
// A coverage atlas holds one axis-aligned bitmap per glyph per size.
// Scale it up and the sampler magnifies that bitmap — the glyph is as
// sharp as the density it was rasterised at and no sharper. Rotate it
// and the same bitmap is resampled at an angle, off the pixel grid it
// was hinted to. A field carries the outline instead and computes its
// antialiasing from the screen-space derivative where it lands, so it
// is as hard-edged at 8x as at 1x.
//
// Contrast (see msdfSmallText) says so. 16 px text, magnified:
//
//	1x   field 0.610  raster 0.578
//	2x   field 0.724  raster 0.471
//	4x   field 0.845  raster 0.481
//	8x   field 0.916  raster 0.482
//
// The raster column flattens at 0.48 — that is its rasterised density,
// and no amount of further magnification recovers any of it.
//
// Rotation is a smaller win and a size-dependent one, which is worth
// knowing before quoting it: at 16 px the field holds 0.62 against the
// raster's 0.47–0.49 at every angle, but at 28 px the two come out
// level (0.620 against 0.631) — a glyph that large loses little to a
// bilinear resample. So rotation is a reason to want a field for UI
// text, not for headlines.
func msdfNotAtRasterSize(t *testing.T, r *Renderer2D, device *gpu.Device, queue *gpu.Queue) {
	white := RGBA{R: 1, G: 1, B: 1, A: 1}
	const size = 16
	f, err := r.NewFont(goregular.TTF, size)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	measure := func(w, h int, draw func()) (fieldC, rasterC, fieldInk, rasterInk float64) {
		r.SetMSDFText(false)
		r.SetMSDFAuto(false) // the raster, transformed or not
		raster := renderRun(t, r, device, queue, w, h, draw)
		r.SetMSDFText(true)
		field := renderRun(t, r, device, queue, w, h, draw)
		rasterInk, _, _, _, _ = ink(raster, w, h)
		fieldInk, _, _, _, _ = ink(field, w, h)
		return contrast(field, w, h), contrast(raster, w, h), fieldInk, rasterInk
	}

	for _, k := range []float32{2, 4, 8} {
		w, h := int(size*12*k), int(size*3*k)
		fc, rc, fs, rs := measure(w, h, func() {
			r.PushTransform(Scale2(k, k, 0, 0))
			r.DrawText(f, 4, 4, runSample, white)
			r.PopTransform()
		})
		t.Logf("%gx: contrast field %.3f raster %.3f, ink field %.0f raster %.0f (%+.1f%%)",
			k, fc, rc, fs, rs, 100*(fs-rs)/rs)
		if fs == 0 {
			t.Fatalf("%gx: the field path drew nothing", k)
		}
		// Still the same text, not a sharper smudge: the field lays
		// down a few per cent more ink because its edges are harder.
		if d := math.Abs(fs-rs) / rs; d > 0.08 {
			t.Errorf("%gx: field ink %.0f, raster %.0f (%.0f%% apart)", k, fs, rs, d*100)
		}
		// A magnified bitmap cannot get sharper; the field must, and
		// by a margin no measurement noise reaches.
		if fc < rc+0.15 {
			t.Errorf("%gx: field contrast %.3f against the raster's %.3f — "+
				"magnified text is the case the field path exists for", k, fc, rc)
		}
	}

	for _, deg := range []float64{15, 30, 45} {
		w, h := size*16, size*10
		fc, rc, fs, _ := measure(w, h, func() {
			r.PushTransform(Rotate2(float32(deg*math.Pi/180), float32(w)/2, float32(h)/2))
			r.DrawText(f, 20, float32(h)/2, runSample, white)
			r.PopTransform()
		})
		t.Logf("%g deg: contrast field %.3f raster %.3f", deg, fc, rc)
		if fs == 0 {
			t.Fatalf("%g deg: the field path drew nothing rotated", deg)
		}
		if fc <= rc {
			t.Errorf("%g deg: field contrast %.3f, raster %.3f — a rotated bitmap is "+
				"resampled off the grid it was hinted to and should lose here", deg, fc, rc)
		}
	}
}
