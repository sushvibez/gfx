//go:build !js

package render

import (
	"math"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/gpu"
)

// Sub-pixel POSITIONING: the pen keeps its fractional x and picks a
// glyph pre-shifted by the nearest phase, so a run's spacing does not
// quantize to whole pixels. With N phases the residual is half a phase,
// and it shows up as a glyph landing up to that far from where the
// layout put it.
//
// Measured on one glyph, because a whole run averages the per-glyph
// residuals away and looks fine either way — which is why the first
// version of this test could not tell four phases from sixteen.
func TestSubpixelPhasesPlaceAGlyphEvenly(t *testing.T) {
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	if !r.SubpixelTextEnabled() {
		t.Skip("no subpixel pipeline on this device")
	}
	f, err := r.NewFont(goregular.TTF, 13)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	const W, H = 120, 40
	fg := RGBA{R: 1, G: 1, B: 1, A: 1}
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := 0; i < 32; i++ {
		off := float64(i) / 32
		pix := renderRun(t, r, device, queue, W, H, func() {
			r.DrawText(f, 20+float32(off), 8, "I", fg)
		})
		var sum, wsum float64
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				v := float64(pix[(y*W+x)*4+1]) // green: the middle LCD channel
				sum += v
				wsum += v * float64(x)
			}
		}
		if sum == 0 {
			t.Fatal("the glyph drew nothing")
		}
		e := wsum/sum - off // where it landed, against where it was asked for
		lo, hi = math.Min(lo, e), math.Max(hi, e)
	}
	span := hi - lo
	// Four phases measured 0.36 px of span here and sixteen 0.18 — not
	// the fourfold the arithmetic suggests, because the 3x raster is
	// hinted and quantizes too, and the LCD filter spreads coverage
	// across a third of a pixel either way.
	if span > 0.25 {
		t.Errorf("a glyph lands over a %.3f px range across 32 offsets; %d phases should hold it under 0.25",
			span, subpixelPhases)
	}
	t.Logf("%d phases: a glyph lands within %.3f px of where it was placed", subpixelPhases, span)
}
