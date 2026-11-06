//go:build !js

package render

import (
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/gpu"
)

// The renderer picks the text path per draw: the hinted raster for text
// drawn as it was rasterised, the distance field for text that is
// scaled, rotated or drawn at another size — where each measured better.
func TestMSDFAutoPicksPerDraw(t *testing.T) {
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	if !r.MSDFAuto() {
		t.Skip("no distance-field pipeline on this device")
	}
	r.SetSubpixelText(false)
	f, err := r.NewFont(goregular.TTF, 16)
	if err != nil {
		t.Fatal(err)
	}
	field := func(draw func()) bool {
		r.Begin(200, 200)
		draw()
		for _, b := range r.batches {
			if b.kind == kindMSDF {
				return true
			}
		}
		return false
	}
	if field(func() { r.DrawText(f, 10, 10, "plain", White) }) {
		t.Error("untransformed text went through the field")
	}
	if !field(func() {
		r.PushTransform(Rotate2(0.4, 50, 50))
		r.DrawText(f, 10, 10, "turned", White)
		r.PopTransform()
	}) {
		t.Error("rotated text stayed on the raster")
	}
	if !field(func() {
		r.PushTransform(Scale2(3, 3, 0, 0))
		r.DrawText(f, 10, 10, "big", White)
		r.PopTransform()
	}) {
		t.Error("magnified text stayed on the raster")
	}
	if !field(func() { r.DrawTextSize(f, 10, 10, "sliding", White, 23) }) {
		t.Error("text at another size stayed on the raster")
	}
	if field(func() { r.DrawTextSize(f, 10, 10, "same", White, 16) }) {
		t.Error("text at its own size went through the field")
	}
	// Off: the raster for everything; forced: the field for everything.
	r.SetMSDFAuto(false)
	if field(func() {
		r.PushTransform(Rotate2(0.4, 50, 50))
		r.DrawText(f, 10, 10, "turned", White)
		r.PopTransform()
	}) {
		t.Error("with the choice off, rotated text went through the field")
	}
	r.SetMSDFText(true)
	if !field(func() { r.DrawText(f, 10, 10, "plain", White) }) {
		t.Error("forced on, plain text stayed on the raster")
	}
}
