//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// tracedScene renders a floor seen from straight above with a sun slanting
// in from -X at 45°, and a caster standing on it of the given shape, 4
// across and 4 high (so its shadow reaches 4 beyond its far side). The
// sun's shadow map is pointed elsewhere, so any shadow in the picture was
// traced. It returns the floor's brightness at a world point.
func tracedScene(t *testing.T, shape TraceShape, traced bool) func(x, z float32) float64 {
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
	fs.Ambient = RGBA{R: 0.05, G: 0.05, B: 0.05, A: 1}
	fs.TracedShadows = traced
	r3.SetFrameSettings(fs)
	r3.SetShadowArea(core.V3(500, 0, 500), 5) // the map sees none of this
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	depth := dev.CreateDepthTexture(w, h)
	dview := depth.CreateView()
	defer func() { dview.Release(); depth.Release(); view.Release(); target.Release() }()

	floor := core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(60, 1, 60)))
	// The unit cylinder has radius 1, so half the scale across.
	caster := core.Mat4Translate(core.V3(0, 2, 0)).Mul(core.Mat4Scale(core.V3(4, 4, 4)))
	mesh := r3.CubeMesh()
	if shape == TraceCylinder {
		caster = core.Mat4Translate(core.V3(0, 2, 0)).Mul(core.Mat4Scale(core.V3(2, 4, 2)))
		mesh = r3.CylinderMesh()
	}
	r3.SetTraceScene([]TraceInstance{{Model: floor}, {Model: caster, Shape: shape}})

	eye := core.V3(0, 60, 0)
	vp := core.Mat4Perspective(0.5, 1, 1, 200).Mul(core.Mat4LookAt(eye, core.V3(0, 0, 0), core.V3(0, 0, -1)))
	grey := &Material{Color: RGBA{R: 0.8, G: 0.8, B: 0.8, A: 1}, Roughness: 1}
	r3.Begin(vp, core.V3(1, -1, 0), eye)
	r3.DrawMaterial(r3.CubeMesh(), floor, grey)
	r3.DrawMaterial(mesh, caster, grey)
	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, view, dview, gpu.Color{A: 1}); err != nil {
		t.Fatal(err)
	}
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return func(x, z float32) float64 {
		c := vp.MulVec4(core.V4(x, 0, z, 1))
		return lumAt(pix, w, int((c.X/c.W*0.5+0.5)*w), int((0.5-c.Y/c.W*0.5)*h))
	}
}

// The sun's shadow, traced: with FrameSettings.TracedShadows a caster
// outside the shadow map still shadows the floor behind it, exactly as far
// as its height and the sun's angle say, and not without it. The shadow
// has the caster's own shape: a cylinder's is round where a box's is
// square.
func TestTracedShadow(t *testing.T) {
	off := tracedScene(t, TraceBox, false)
	box := tracedScene(t, TraceBox, true)
	cyl := tracedScene(t, TraceCylinder, true)
	// Behind the caster (it ends at x = 2; its top's shadow reaches x = 6).
	if lit, dark := off(4, 0), box(4, 0); lit < 100 || dark > lit*0.35 {
		t.Fatalf("no traced shadow behind the box: %v traced, %v without", dark, lit)
	}
	// Past the shadow's end, and beside it, the floor is lit.
	for _, p := range [][2]float32{{8, 0}, {4, 4}, {-6, 0}} {
		if a, b := box(p[0], p[1]), off(p[0], p[1]); a < b*0.9 {
			t.Fatalf("the floor at %v is shadowed (%v, %v without tracing)", p, a, b)
		}
	}
	// Near the far corner of the box's shadow (it ends square at x = 6): the
	// cylinder's ends in the round shadow of its top, which at z = 1.8 has
	// already stopped by x = 4.9.
	if b, c := box(5.6, 1.8), cyl(5.6, 1.8); !(b < c*0.5) {
		t.Fatalf("the cylinder's shadow is not round: floor %v under the box's corner shadow, %v with the cylinder", b, c)
	}
	if lit, dark := off(4, 0), cyl(4, 0); dark > lit*0.35 {
		t.Fatalf("no traced shadow behind the cylinder: %v traced, %v without", dark, lit)
	}
}

// The hierarchy holds every shape once and its root encloses them all.
func TestTraceBuild(t *testing.T) {
	var insts []TraceInstance
	for i := 0; i < 100; i++ {
		insts = append(insts, TraceInstance{Model: core.Mat4Translate(core.V3(float32(i%10)*3, 0, float32(i/10)*3))})
	}
	nodes, gi := buildTrace(insts)
	if len(gi) != 100 {
		t.Fatalf("%d shapes kept of 100", len(gi))
	}
	seen := 0
	for _, n := range nodes {
		seen += int(n.count)
	}
	if seen != 100 {
		t.Fatalf("the leaves hold %d shapes, want 100", seen)
	}
	if r := nodes[0]; r.min[0] > -0.49 || r.max[0] < 27.49 || r.min[2] > -0.49 || r.max[2] < 27.49 {
		t.Fatalf("the root does not enclose the scene: %v … %v", r.min, r.max)
	}
}
