//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// ReadImage returns the view's picture as RGBA whatever its target format:
// an empty scene reads back as the clear colour, BGRA swizzled.
func TestView3DReadImage(t *testing.T) {
	dev, q := headlessDevice(t)
	for _, f := range []gpu.TextureFormat{gpu.FormatRGBA8Unorm, gpu.FormatBGRA8Unorm} {
		r2, err := New2D(dev, q, f)
		if err != nil {
			t.Fatal(err)
		}
		r3, err := New3D(dev, q, f)
		if err != nil {
			t.Fatal(err)
		}
		v := NewView3D(dev, r2, f)
		if _, err := v.ReadImage(q); err == nil {
			t.Fatal("ReadImage before Resize: want an error")
		}
		v.Resize(70, 3) // rows need padding to the copy alignment
		v.Clear = gpu.Color{R: 1, G: 0, B: 0, A: 1}
		r3.Begin(core.Mat4Identity(), core.V3(0, -1, 0), core.V3(0, 0, 0))
		enc := dev.CreateCommandEncoder()
		if err := v.Render(enc, r3); err != nil {
			t.Fatal(err)
		}
		q.Submit(enc.Finish())
		img, err := v.ReadImage(q)
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != 70 || b.Dy() != 3 {
			t.Fatalf("format %v: bounds %v", f, b)
		}
		if c := img.RGBAAt(69, 2); c.R < 250 || c.G > 5 || c.B > 5 {
			t.Errorf("format %v: pixel %v, want red", f, c)
		}
		v.Release()
	}
}

// Draws counts the records queued since Begin.
func TestRenderer3DDraws(t *testing.T) {
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	r3.Begin(core.Mat4Identity(), core.V3(0, -1, 0), core.V3(0, 0, 0))
	if n := r3.Draws(); n != 0 {
		t.Fatalf("after Begin: %d draws", n)
	}
	r3.DrawPBR(r3.CubeMesh(), core.Mat4Identity(), RGBA{R: 1, A: 1}, 0, 0.5, nil)
	r3.DrawPBR(r3.CubeMesh(), core.Mat4Translate(core.V3(2, 0, 0)), RGBA{G: 1, A: 1}, 0, 0.5, nil)
	if n := r3.Draws(); n != 2 {
		t.Fatalf("Draws = %d, want 2", n)
	}
	r3.Begin(core.Mat4Identity(), core.V3(0, -1, 0), core.V3(0, 0, 0))
	if n := r3.Draws(); n != 0 {
		t.Fatalf("Begin did not reset: %d", n)
	}
}
