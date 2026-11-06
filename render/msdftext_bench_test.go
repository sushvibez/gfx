//go:build !js

package render

import (
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/gpu"
)

// The distance field has to be at least as cheap per frame as the atlas
// it replaces, or the memory it saves is paid back in CPU. Both draw the
// same run into the same buffers; only the glyph lookup and the batch
// kind differ.
func benchDrawText(b *testing.B, msdf bool) {
	device, queue := headlessDevice(b)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		b.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	r.SetSubpixelText(false)
	r.SetMSDFText(msdf)
	if msdf && !r.MSDFTextEnabled() {
		b.Skip("no distance-field pipeline on this device")
	}
	f, err := r.NewFont(goregular.TTF, 24)
	if err != nil {
		b.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	// Warm the sheet, so the benchmark measures drawing and not the one
	// rasterisation (or one field generation) per glyph.
	r.Begin(800, 600)
	r.DrawText(f, 10, 10, runSample, RGBA{A: 1})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Begin(800, 600)
		r.DrawText(f, 10, 10, runSample, RGBA{A: 1})
	}
}

func BenchmarkDrawTextMSDF(b *testing.B)   { benchDrawText(b, true) }
func BenchmarkDrawTextRaster(b *testing.B) { benchDrawText(b, false) }

// One font, many sizes: what an animated size costs each way.
func BenchmarkDrawTextSizeField(b *testing.B) {
	device, queue := headlessDevice(b)
	r, _ := New2D(device, queue, gpu.FormatRGBA8Unorm)
	defer r.Release()
	r.SetMSDFText(true)
	if !r.MSDFTextEnabled() {
		b.Skip("no distance-field pipeline on this device")
	}
	f, _ := r.NewFont(goregular.TTF, 24)
	defer f.Release()
	r.Begin(800, 600)
	r.DrawTextSize(f, 10, 10, runSample, RGBA{A: 1}, 24)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Begin(800, 600)
		r.DrawTextSize(f, 10, 10, runSample, RGBA{A: 1}, 12+float32(i%600)*0.1)
	}
}

// A text-heavy frame: what a page of a UI actually draws. This is the
// case the shaping cache exists for — the same strings, every frame.
func benchPage(b *testing.B, msdf bool) {
	device, queue := headlessDevice(b)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		b.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	r.SetSubpixelText(false)
	r.SetMSDFText(msdf)
	if msdf && !r.MSDFTextEnabled() {
		b.Skip("no distance-field pipeline on this device")
	}
	base, err := r.NewFont(goregular.TTF, 15)
	if err != nil {
		b.Fatalf("NewFont: %v", err)
	}
	defer base.Release()

	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "The quick brown fox jumps over the lazy dog 0123456789"
	}
	white := RGBA{R: 1, G: 1, B: 1, A: 1}
	page := func() {
		r.Begin(1200, 900)
		for i, ln := range lines {
			r.MeasureText(base, ln) // a layout pass measures before it paints
			r.DrawText(base, 20, float32(i)*15, ln, white)
		}
	}
	page()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page()
	}
}

func BenchmarkTextPageMSDF(b *testing.B)   { benchPage(b, true) }
func BenchmarkTextPageRaster(b *testing.B) { benchPage(b, false) }
