//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// roofScene renders a matt floor lit only by ambient light, half of it
// under a roof, seen from the side, and returns the floor's brightness
// under the roof and out in the open.
func roofScene(t *testing.T, wetness float32) (under, open float64) {
	t.Helper()
	pix, vp := roofPixels(t, wetness, 0)
	const w, h = 320, 240
	// The mean over a patch of floor, so puddles and dry spots average out.
	patch := func(cx float32) float64 {
		sum, n := 0.0, 0
		for x := cx - 1.5; x <= cx+1.5; x += 0.25 {
			for z := float32(2); z <= 6; z += 0.25 {
				c := vp.MulVec4(core.V4(x, 0, z, 1))
				sum += lumAt(pix, w, int((c.X/c.W*0.5+0.5)*w), int((0.5-c.Y/c.W*0.5)*h))
				n++
			}
		}
		return sum / float64(n)
	}
	return patch(-5), patch(5)
}

// roofPixels renders roofScene's picture (320×240) at the given wetness
// and rain.
func roofPixels(t *testing.T, wetness, rain float32) ([]byte, core.Mat4) {
	t.Helper()
	const w, h = 320, 240
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = 0
	fs.Sun = RGBA{R: 1e-4, G: 1e-4, B: 1e-4, A: 1}
	fs.Ambient = RGBA{R: 0.6, G: 0.6, B: 0.6, A: 1}
	fs.Wetness, fs.Rain = wetness, rain
	r3.SetFrameSettings(fs)
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	depth := dev.CreateDepthTexture(w, h)
	dview := depth.CreateView()
	defer func() { dview.Release(); depth.Release(); view.Release(); target.Release() }()

	eye := core.V3(0, 3, 16)
	vp := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.5, 100).Mul(core.Mat4LookAt(eye, core.V3(0, 0, 0), core.V3(0, 1, 0)))
	grey := &Material{Color: RGBA{R: 0.7, G: 0.7, B: 0.7, A: 1}, Roughness: 1}
	r3.Begin(vp, core.V3(0, -1, 0), eye)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(60, 1, 60))), grey)
	// The roof: over x < 0, five up.
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(-8, 5, 0)).Mul(core.Mat4Scale(core.V3(16, 0.3, 30))), grey)
	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, view, dview, gpu.Color{A: 1}); err != nil {
		t.Fatal(err)
	}
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return pix, vp
}

// Wetness darkens what is open to the sky and leaves what has a roof over
// it dry.
func TestWetnessStopsUnderRoof(t *testing.T) {
	uDry, oDry := roofScene(t, 0)
	uWet, oWet := roofScene(t, 1)
	if oWet > oDry*0.85 {
		t.Fatalf("the open floor did not darken when wet: %.0f wet, %.0f dry", oWet, oDry)
	}
	if uWet < uDry*0.95 {
		t.Fatalf("the floor under the roof got wet: %.0f wet, %.0f dry", uWet, uDry)
	}
}

// FrameSettings.Rain fills the air with drops: bright streaks over part of
// the picture (not none of it, not most of it).
func TestRainDrops(t *testing.T) {
	const w, h = 320, 240
	dry, _ := roofPixels(t, 0, 0)
	rain, _ := roofPixels(t, 0, 1)
	n := 0
	for i := 0; i < w*h; i++ {
		if int(rain[i*4+1]) > int(dry[i*4+1])+5 {
			n++
		}
	}
	if frac := float64(n) / float64(w*h); frac < 0.002 || frac > 0.5 {
		t.Fatalf("drops cover %.2f%% of the picture", frac*100)
	}
	if testing.Verbose() {
		dumpPNG(t, rain, w, h, "rain-drops")
	}
}
