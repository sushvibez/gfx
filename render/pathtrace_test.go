//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// The reference path tracer on a floor with a block and a red wall: the
// block's shadow is darker than the sunlit floor but lit by the sky, the
// floor beside the wall takes its colour, and the mean settles.
func TestPathTracer(t *testing.T) {
	const w, h = 160, 120
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	pt, err := NewPathTracer(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer pt.Release()
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	tview := target.CreateView()
	defer func() { tview.Release(); target.Release() }()

	grey := [3]float32{0.7, 0.7, 0.7}
	floor := core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(40, 1, 40)))
	block := core.Mat4Translate(core.V3(-3, 1, 0)).Mul(core.Mat4Scale(core.V3(2, 2, 2)))
	wall := core.Mat4Translate(core.V3(4, 2, 0)).Mul(core.Mat4Scale(core.V3(0.5, 4, 8)))
	r3.SetTraceScene([]TraceInstance{
		{Model: floor, Color: grey},
		{Model: block, Color: grey},
		{Model: wall, Color: [3]float32{0.9, 0.05, 0.05}},
	})
	eye := core.V3(0, 9, 9)
	pt.Projection = core.Mat4Perspective(0.9, float32(w)/float32(h), 0.1, 300)
	pt.View = core.Mat4LookAt(eye, core.V3(0, 0, 0), core.V3(0, 1, 0))
	pt.SunDir = core.V3(-1, -1.2, 0) // from the wall's side: it lights the wall's near face, and the block shadows away from it
	pt.Sun = [3]float32{3, 3, 3}
	pt.SkyTop, pt.SkyHorizon = [3]float32{0.3, 0.3, 0.3}, [3]float32{0.3, 0.3, 0.3}

	vp := pt.Projection.Mul(pt.View)
	at := func(pix []byte, world core.Vec3) (r, g, b float64) {
		c := vp.MulVec4(core.V4(world.X, world.Y, world.Z, 1))
		x := int((c.X/c.W*0.5 + 0.5) * w)
		y := int((0.5 - c.Y/c.W*0.5) * h)
		i := (y*w + x) * 4
		return float64(pix[i]), float64(pix[i+1]), float64(pix[i+2])
	}
	frame := func() []byte {
		enc := dev.CreateCommandEncoder()
		pt.Render(enc, r3, tview, w, h)
		q.Submit(enc.Finish())
		pix, err := dev.ReadTexture(q, target, w, h)
		if err != nil {
			t.Fatal(err)
		}
		return pix
	}
	var pix []byte
	for i := 0; i < 96; i++ {
		pix = frame()
	}
	if pt.Frames() != 96 {
		t.Fatalf("the mean is of %d frames, want 96", pt.Frames())
	}
	litR, litG, _ := at(pix, core.V3(-1, 0, 5))    // open floor
	shR, shG, _ := at(pix, core.V3(-5.2, 0, 0))    // behind the block, away from the sun
	nearR, nearG, _ := at(pix, core.V3(2.9, 0, 0)) // floor at the wall's sunlit face
	if litG < 80 {
		t.Fatalf("the sunlit floor is dark: %.0f", litG)
	}
	if shG > 0.75*litG || shG < 8 {
		t.Fatalf("the block's shadow: %.0f beside a sunlit %.0f (want darker, and not black)", shG, litG)
	}
	if nearR-nearG < (litR-litG)+6 {
		t.Fatalf("no colour from the wall on the floor beside it: r-g %.0f there, %.0f in the open (shadow %.0f/%.0f)", nearR-nearG, litR-litG, shR, shG)
	}
	// Settled: another 32 frames move the open floor little.
	for i := 0; i < 32; i++ {
		pix = frame()
	}
	if _, g2, _ := at(pix, core.V3(-1, 0, 5)); g2 < litG-6 || g2 > litG+6 {
		t.Fatalf("the mean has not settled: %.0f then %.0f", litG, g2)
	}
	// A moved camera starts again.
	pt.View = core.Mat4LookAt(eye.Add(core.V3(1, 0, 0)), core.V3(0, 0, 0), core.V3(0, 1, 0))
	frame()
	if pt.Frames() != 1 {
		t.Fatalf("after the camera moved the mean is of %d frames, want 1", pt.Frames())
	}
}
