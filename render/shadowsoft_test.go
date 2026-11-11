//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// softShadowProfile renders a wall's shadow thrown across a floor by a
// slanting sun, seen from above, and returns the penumbra's width in
// pixels near the wall's foot and far from it.
func softShadowProfile(t *testing.T, softness float32) (near, far int) {
	t.Helper()
	const w, h = 480, 480
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = 0
	fs.SunSoftness = softness
	fs.Ambient = RGBA{R: 0.04, G: 0.04, B: 0.04, A: 1}
	r3.SetFrameSettings(fs)
	r3.SetShadowArea(core.V3(0, 0, 0), 16)
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	depth := dev.CreateDepthTexture(w, h)
	dview := depth.CreateView()
	defer func() { dview.Release(); depth.Release(); view.Release(); target.Release() }()

	// From straight above, +X to the right, +Z down the image.
	eye := core.V3(0, 60, 0)
	vp := core.Mat4Perspective(0.4, 1, 1, 200).Mul(core.Mat4LookAt(eye, core.V3(0, 0, 0), core.V3(0, 0, -1)))
	floor := &Material{Color: RGBA{R: 0.8, G: 0.8, B: 0.8, A: 1}, Roughness: 1}
	// A tall thin wall along X at z = -8, its top edge 10 up; the sun comes
	// from -Z at 45°, so the top edge's shadow lands at z = +2 (far from
	// what casts it) and the wall's ends cast edges that start at its foot.
	r3.Begin(vp, core.V3(0, -1, 1), eye)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(60, 1, 60))), floor)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 5, -8)).Mul(core.Mat4Scale(core.V3(8, 10, 0.3))), floor)
	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, view, dview, gpu.Color{A: 1}); err != nil {
		t.Fatal(err)
	}
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	// World to pixel: the view spans 2·60·tan(0.2) ≈ 24.3 units.
	span := 2 * 60 * 0.2027
	px := func(x, z float64) (int, int) {
		return int((x/span + 0.5) * w), int((z/span + 0.5) * h)
	}
	// The penumbra along a line: pixels strictly between shadow and light.
	width := func(x0, z0, x1, z1 float64) int {
		ax, ay := px(x0, z0)
		bx, by := px(x1, z1)
		lo, hi := 255.0, 0.0
		n := max(abs(bx-ax), abs(by-ay))
		vals := make([]float64, n+1)
		for i := 0; i <= n; i++ {
			x, y := ax+(bx-ax)*i/n, ay+(by-ay)*i/n
			vals[i] = lumAt(pix, w, x, y)
			lo, hi = min(lo, vals[i]), max(hi, vals[i])
		}
		if hi-lo < 40 {
			t.Fatalf("no shadow edge on the line (%v…%v)", lo, hi)
		}
		c := 0
		for _, v := range vals {
			if v > lo+0.15*(hi-lo) && v < hi-0.15*(hi-lo) {
				c++
			}
		}
		return c
	}
	// Far: across the top edge's shadow line (z = +2), mid-wall.
	far = width(0, -1, 0, 5)
	// Near: across the side edge of the shadow just past the wall's foot
	// (the wall's end is at x = 4; the shadow runs from z = -8 to +2).
	near = width(2.5, -7, 5.5, -7)
	return near, far
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// PCSS: with SunSoftness the shadow edge far from its caster is much wider
// than the hard shadow's, and wider than the same shadow's edge near the
// caster's foot (contact hardening).
func TestSoftShadowWidensWithDistance(t *testing.T) {
	_, hardFar := softShadowProfile(t, 0)
	softNear, softFar := softShadowProfile(t, 0.05)
	if softFar < hardFar+6 {
		t.Fatalf("far edge: %d px soft against %d px hard, want clearly wider", softFar, hardFar)
	}
	if softFar < softNear+4 {
		t.Fatalf("no contact hardening: %d px far, %d px near the foot", softFar, softNear)
	}
}
