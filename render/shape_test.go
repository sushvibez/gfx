//go:build !js

package render

import (
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/gpu"
)

// One device for the lot — every test in this package opens its own and
// the driver has a ceiling, which three more were enough to reach. Each
// subtest still gets a renderer of its own, since they count what is in
// its cache.
func TestShapeCache(t *testing.T) {
	device, queue := headlessDevice(t)
	t.Run("does not grow with size or position", func(t *testing.T) {
		shapeCacheKeyIsTheRun(t, device, queue)
	})
	t.Run("forgets unused runs", func(t *testing.T) {
		shapeCacheForgets(t, device, queue)
	})
	t.Run("measuring matches drawing", func(t *testing.T) {
		shapeMeasureMatchesDraw(t, device, queue)
	})
}

func shapeTestRenderer(t *testing.T, device *gpu.Device, queue *gpu.Queue) (*Renderer2D, *Font) {
	t.Helper()
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	t.Cleanup(r.Release)
	f, err := r.NewFont(goregular.TTF, 16)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	t.Cleanup(f.Release)
	return r, f
}

// The cache must be keyed on what a run IS, not on where it lands or how
// big it is drawn. A size that slides is the case that matters: it made
// an entry a frame once already, in the example, and putting the scale
// into this key would have done it again a layer down.
func shapeCacheKeyIsTheRun(t *testing.T, device *gpu.Device, queue *gpu.Queue) {
	r, f := shapeTestRenderer(t, device, queue)
	r.SetMSDFText(true)
	if !r.MSDFTextEnabled() {
		t.Skip("no distance-field pipeline on this device")
	}
	white := RGBA{R: 1, G: 1, B: 1, A: 1}
	r.Begin(800, 600)
	for i := 0; i < 600; i++ {
		// A different size and a different position every time.
		r.DrawTextSize(f, float32(i)*0.31, float32(i)*0.17, "Hamburgefonstiv", white, 8+float32(i)*0.13)
	}
	if n := len(r.shapes); n != 1 {
		t.Errorf("600 sizes of one string made %d cache entries, want 1", n)
	}
	// Tracking is applied per glyph at draw, so it is not a key either.
	for i := 0; i < 50; i++ {
		r.drawTextMSDF(f, 10, 10, "Hamburgefonstiv", white, 0, float32(i)*0.1, 1)
	}
	if n := len(r.shapes); n != 1 {
		t.Errorf("50 trackings made %d cache entries, want 1", n)
	}
}

// A run nothing has drawn for a while has to go, or a page that shows a
// clock remembers every second it ever displayed.
func shapeCacheForgets(t *testing.T, device *gpu.Device, queue *gpu.Queue) {
	r, f := shapeTestRenderer(t, device, queue)
	white := RGBA{R: 1, G: 1, B: 1, A: 1}
	r.Begin(800, 600)
	for i := 0; i < 20; i++ {
		r.DrawText(f, 10, 10, string(rune('a'+i))+" transient", white)
	}
	if len(r.shapes) != 20 {
		t.Fatalf("expected 20 cached runs, got %d", len(r.shapes))
	}
	keep := "the one that stays"
	for i := 0; i < 2*shapeSweep; i++ {
		r.Begin(800, 600)
		r.DrawText(f, 10, 10, keep, white)
	}
	if n := len(r.shapes); n != 1 {
		t.Errorf("after %d frames drawing one run, %d runs are still cached", 2*shapeSweep, n)
	}
}

// Measuring and drawing must agree — they share the cache now, so a run
// that is measured for layout and then drawn is walked once between
// them, and both have to mean the same thing by it.
func shapeMeasureMatchesDraw(t *testing.T, device *gpu.Device, queue *gpu.Queue) {
	r, f := shapeTestRenderer(t, device, queue)
	for _, s := range []string{"Hamburgefonstiv", "AV To Wa", "iiii", "the quick brown fox"} {
		w, _ := r.MeasureText(f, s)
		run := r.shape(f, f, nil, s, 1, 1)
		if w != run.width {
			t.Errorf("%q: MeasureText %v, shaped width %v", s, w, run.width)
		}
		if len(run.glyphs) == 0 {
			t.Errorf("%q shaped to nothing", s)
		}
	}
}
