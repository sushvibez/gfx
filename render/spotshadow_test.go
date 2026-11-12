//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// spotScene renders a floor under a downward spot light with a slab
// hanging between them, seen from above, and returns the floor's
// brightness under the slab and beside it.
func spotScene(t *testing.T, shadow bool) (under, beside float64) {
	t.Helper()
	const w, h = 320, 320
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = 0
	fs.Sun = RGBA{R: 1e-4, G: 1e-4, B: 1e-4, A: 1} // next to no sun (zero would mean the default): the lamp alone
	fs.Ambient = RGBA{R: 0.01, G: 0.01, B: 0.01, A: 1}
	r3.SetFrameSettings(fs)
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	depth := dev.CreateDepthTexture(w, h)
	dview := depth.CreateView()
	defer func() { dview.Release(); depth.Release(); view.Release(); target.Release() }()

	// The camera looks down from the side, so the floor beneath the slab is
	// in view.
	eye := core.V3(0, 9, 14)
	vp := core.Mat4Perspective(0.9, 1, 0.5, 100).Mul(core.Mat4LookAt(eye, core.V3(0, 0, 0), core.V3(0, 1, 0)))
	grey := &Material{Color: RGBA{R: 0.8, G: 0.8, B: 0.8, A: 1}, Roughness: 1}
	r3.SetSpotLights([]SpotLight{{Pos: core.V3(0, 10, 0), Dir: core.V3(0, -1, 0), Color: RGBA{R: 1, G: 1, B: 1, A: 1},
		Energy: 120, Range: 30, Angle: 0.9, Shadow: shadow}})
	r3.Begin(vp, core.V3(0, -1, 0), eye)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(40, 1, 40))), grey)
	// The slab, 4 up: it covers the floor out to 2·10/6 ≈ 3.3 from the axis.
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 4, 0)).Mul(core.Mat4Scale(core.V3(4, 0.2, 4))), grey)
	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, view, dview, gpu.Color{A: 1}); err != nil {
		t.Fatal(err)
	}
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	at := func(p core.Vec3) float64 {
		c := vp.MulVec4(core.V4(p.X, p.Y, p.Z, 1))
		return lumAt(pix, w, int((c.X/c.W*0.5+0.5)*w), int((0.5-c.Y/c.W*0.5)*h))
	}
	// Floor points in front of the slab as the camera sees it: one inside
	// its shadow, one well outside but still in the cone.
	return at(core.V3(0, 0, 2.8)), at(core.V3(0, 0, 6))
}

// A spot light with Shadow set is blocked by what hangs under it; without
// it the same light passes through.
func TestSpotLightShadow(t *testing.T) {
	uOff, bOff := spotScene(t, false)
	uOn, bOn := spotScene(t, true)
	if uOff < 60 || bOff < 40 {
		t.Fatalf("the lamp does not light the floor: under %.0f, beside %.0f", uOff, bOff)
	}
	if uOn > uOff*0.25 {
		t.Fatalf("the slab casts no shadow: floor under it %.0f with shadows, %.0f without", uOn, uOff)
	}
	if bOn < bOff*0.9 {
		t.Fatalf("the floor beside the slab darkened: %.0f with shadows, %.0f without (acne?)", bOn, bOff)
	}
}
