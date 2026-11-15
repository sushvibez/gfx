//go:build !js

package render

import (
	"math"
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
	var hit *Mesh // the triangles a TraceMesh caster is traced by
	switch shape {
	case TraceCylinder:
		caster = core.Mat4Translate(core.V3(0, 2, 0)).Mul(core.Mat4Scale(core.V3(2, 4, 2)))
		mesh = r3.CylinderMesh()
	case TraceMesh:
		// A sphere of triangles, 4 across, resting on the floor.
		mesh = r3.SphereMesh()
		lo, hi := mesh.Bounds()
		k := 4 / (hi.X - lo.X)
		caster = core.Mat4Translate(core.V3(0, 2, 0)).Mul(core.Mat4Scale(core.V3(k, k, k)))
		hit = mesh
	}
	r3.SetTraceScene([]TraceInstance{{Model: floor}, {Model: caster, Shape: shape, Mesh: hit}})

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

// A triangle mesh casts its own shadow: a sphere's is an ellipse, so a
// point the box's square shadow covers, near its far corner, is lit beside
// the sphere; straight behind it the floor is dark.
func TestTracedMeshShadow(t *testing.T) {
	off := tracedScene(t, TraceBox, false)
	box := tracedScene(t, TraceBox, true)
	ball := tracedScene(t, TraceMesh, true)
	if lit, dark := off(3.5, 0), ball(3.5, 0); lit < 100 || dark > lit*0.35 {
		t.Fatalf("no traced shadow behind the sphere: %v traced, %v without", dark, lit)
	}
	if b, s := box(4.2, 1.7), ball(4.2, 1.7); !(b < s*0.5) {
		t.Fatalf("the sphere's shadow is the box's: floor %v under the box's shadow there, %v with the sphere", b, s)
	}
	if a, b := ball(9, 0), off(9, 0); a < b*0.9 {
		t.Fatalf("the floor past the sphere's shadow is dark: %v, %v without tracing", a, b)
	}
}

// The hierarchy holds every shape once and its root encloses them all.
func TestTraceBuild(t *testing.T) {
	var insts []TraceInstance
	for i := 0; i < 100; i++ {
		insts = append(insts, TraceInstance{Model: core.Mat4Translate(core.V3(float32(i%10)*3, 0, float32(i/10)*3))})
	}
	nodes, gi, _ := buildTrace(insts)
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

// Traced occlusion: under ambient light alone, the floor at the foot of a
// tall block is darker than the open floor with FrameSettings.TracedAO,
// and the same without it.
func TestTracedAO(t *testing.T) {
	shot := func(ao float32) func(x, z float32) float64 {
		const w, h = 320, 320
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
		fs.TracedAO = ao
		r3.SetFrameSettings(fs)
		target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
		view := target.CreateView()
		depth := dev.CreateDepthTexture(w, h)
		dview := depth.CreateView()
		defer func() { dview.Release(); depth.Release(); view.Release(); target.Release() }()
		floor := core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(60, 1, 60)))
		block := core.Mat4Translate(core.V3(0, 4, 0)).Mul(core.Mat4Scale(core.V3(4, 8, 4)))
		r3.SetTraceScene([]TraceInstance{{Model: floor}, {Model: block}})
		eye := core.V3(0, 60, 0)
		vp := core.Mat4Perspective(0.5, 1, 1, 200).Mul(core.Mat4LookAt(eye, core.V3(0, 0, 0), core.V3(0, 0, -1)))
		grey := &Material{Color: RGBA{R: 0.8, G: 0.8, B: 0.8, A: 1}, Roughness: 1}
		r3.Begin(vp, core.V3(0, -1, 0), eye)
		r3.DrawMaterial(r3.CubeMesh(), floor, grey)
		r3.DrawMaterial(r3.CubeMesh(), block, grey)
		enc := dev.CreateCommandEncoder()
		if err := r3.Render(enc, view, dview, gpu.Color{A: 1}); err != nil {
			t.Fatal(err)
		}
		q.Submit(enc.Finish())
		pix, err := dev.ReadTexture(q, target, w, h)
		if err != nil {
			t.Fatal(err)
		}
		// The mean over a small patch: three rays a pixel are grainy.
		return func(x, z float32) float64 {
			c := vp.MulVec4(core.V4(x, 0, z, 1))
			px, py := int((c.X/c.W*0.5+0.5)*w), int((0.5-c.Y/c.W*0.5)*h)
			s := 0.0
			for dy := -3; dy <= 3; dy++ {
				for dx := -3; dx <= 3; dx++ {
					s += lumAt(pix, w, px+dx, py+dy)
				}
			}
			return s / 49
		}
	}
	plain, traced := shot(0), shot(1)
	if foot, open := traced(2.5, 0), traced(12, 0); foot > open*0.85 {
		t.Fatalf("no occlusion at the block's foot: %.0f there, %.0f in the open", foot, open)
	}
	if a, b := traced(12, 0), plain(12, 0); a < b*0.93 {
		t.Fatalf("the open floor darkened: %.0f traced, %.0f without", a, b)
	}
	if foot, open := plain(2.5, 0), plain(12, 0); foot < open*0.95 {
		t.Fatalf("occlusion without TracedAO: %.0f at the foot, %.0f in the open", foot, open)
	}
}

// The temporal filter: one occlusion ray a pixel is grainy in a single
// frame, and the same picture after two dozen frames of PostSettings.
// Temporal is smooth, and has settled (more frames do not change it).
func TestPostTemporalSettlesGrain(t *testing.T) {
	const w, h = 320, 320
	shot := func(frames int) (mean, grain float64) {
		dev, q := headlessDevice(t)
		r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
		if err != nil {
			t.Fatal(err)
		}
		defer r3.Release()
		fs := DefaultFrameSettings()
		fs.Tonemap = TonemapHDR
		fs.Sun = RGBA{R: 1e-4, G: 1e-4, B: 1e-4, A: 1}
		fs.Ambient = RGBA{R: 0.6, G: 0.6, B: 0.6, A: 1}
		fs.TracedAO, fs.Temporal = 1, true
		r3.SetFrameSettings(fs)
		r3.KeepDepth = true
		post, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
		if err != nil {
			t.Fatal(err)
		}
		defer post.Release()
		s := &post.Settings
		s.AutoExposure, s.Bloom, s.Grain, s.ChromaticAberration, s.Vignette, s.Sharpen, s.AO = 0, 0, 0, 0, 0, 0, 0
		s.FXAA = false
		s.Temporal = 0.9
		post.Resize(w, h)
		target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
		tview := target.CreateView()
		defer func() { tview.Release(); target.Release() }()
		floor := core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(60, 1, 60)))
		block := core.Mat4Translate(core.V3(0, 4, 0)).Mul(core.Mat4Scale(core.V3(4, 8, 4)))
		r3.SetTraceScene([]TraceInstance{{Model: floor}, {Model: block}})
		eye := core.V3(0, 60, 0)
		proj := core.Mat4Perspective(0.5, 1, 1, 200)
		view := core.Mat4LookAt(eye, core.V3(0, 0, 0), core.V3(0, 0, -1))
		post.Projection, post.View = proj, view
		grey := &Material{Color: RGBA{R: 0.8, G: 0.8, B: 0.8, A: 1}, Roughness: 1}
		for i := 0; i < frames; i++ {
			r3.Begin(proj.Mul(view), core.V3(0, -1, 0), eye)
			r3.DrawMaterial(r3.CubeMesh(), floor, grey)
			r3.DrawMaterial(r3.CubeMesh(), block, grey)
			post.UseShadow(r3)
			enc := dev.CreateCommandEncoder()
			if err := r3.Render(enc, post.SceneView(), post.DepthView(), gpu.Color{A: 1}); err != nil {
				t.Fatal(err)
			}
			post.Render(enc, tview)
			q.Submit(enc.Finish())
		}
		pix, err := dev.ReadTexture(q, target, w, h)
		if err != nil {
			t.Fatal(err)
		}
		// A patch of floor beside the block, where about half the rays are stopped.
		vp := proj.Mul(view)
		c := vp.MulVec4(core.V4(2.6, 0, 0, 1))
		px, py := int((c.X/c.W*0.5+0.5)*w), int((0.5-c.Y/c.W*0.5)*h)
		var sum, sq float64
		n := 0.0
		for dy := -6; dy <= 6; dy++ {
			for dx := 0; dx <= 4; dx++ {
				v := lumAt(pix, w, px+dx, py+dy)
				sum, sq, n = sum+v, sq+v*v, n+1
			}
		}
		mean = sum / n
		return mean, math.Sqrt(max(sq/n-mean*mean, 0))
	}
	_, g1 := shot(1)
	_, g4 := shot(4)
	m24, g24 := shot(24)
	m96, _ := shot(96)
	if g1 < 6 {
		t.Fatalf("one frame is not grainy (deviation %.1f): the test measures nothing", g1)
	}
	// The first frames count equally, so four already halve the grain.
	if g4 > g1*0.62 {
		t.Fatalf("the grain lingers: deviation %.1f after 4 frames, %.1f after one", g4, g1)
	}
	if g24 > g1*0.5 {
		t.Fatalf("the grain did not settle: deviation %.1f after 24 frames, %.1f after one", g24, g1)
	}
	// Settled: four times as many frames give the same picture. (One frame's
	// mean is no reference: its pixels are tone mapped all-or-nothing, so it
	// reads darker than the average it is a sample of.)
	if m24 < m96*0.95 || m24 > m96*1.05 {
		t.Fatalf("the picture has not settled: mean %.1f after 24 frames, %.1f after 96", m24, m96)
	}
}

// Lamps cast traced shadows: a spot light with no shadow map of its own,
// over a slab hanging above the floor, leaves the floor under the slab
// dark with FrameSettings.TracedLightShadows and lit without.
func TestTracedLightShadow(t *testing.T) {
	shot := func(traced bool) (under, beside float64) {
		const w, h = 320, 320
		dev, q := headlessDevice(t)
		r3, err := New3D(dev, q, gpu.FormatRGBA8Unorm)
		if err != nil {
			t.Fatal(err)
		}
		defer r3.Release()
		fs := DefaultFrameSettings()
		fs.Tonemap = 0
		fs.Sun = RGBA{R: 1e-4, G: 1e-4, B: 1e-4, A: 1}
		fs.Ambient = RGBA{R: 0.01, G: 0.01, B: 0.01, A: 1}
		fs.TracedLightShadows = traced
		r3.SetFrameSettings(fs)
		target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
		view := target.CreateView()
		depth := dev.CreateDepthTexture(w, h)
		dview := depth.CreateView()
		defer func() { dview.Release(); depth.Release(); view.Release(); target.Release() }()
		floor := core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(40, 1, 40)))
		slab := core.Mat4Translate(core.V3(0, 4, 0)).Mul(core.Mat4Scale(core.V3(4, 0.2, 4)))
		r3.SetTraceScene([]TraceInstance{{Model: floor}, {Model: slab}})
		eye := core.V3(0, 9, 14)
		vp := core.Mat4Perspective(0.9, 1, 0.5, 100).Mul(core.Mat4LookAt(eye, core.V3(0, 0, 0), core.V3(0, 1, 0)))
		grey := &Material{Color: RGBA{R: 0.8, G: 0.8, B: 0.8, A: 1}, Roughness: 1}
		r3.SetSpotLights([]SpotLight{{Pos: core.V3(0, 10, 0), Dir: core.V3(0, -1, 0), Color: RGBA{R: 1, G: 1, B: 1, A: 1},
			Energy: 120, Range: 30, Angle: 0.9}})
		r3.Begin(vp, core.V3(0, -1, 0), eye)
		r3.DrawMaterial(r3.CubeMesh(), floor, grey)
		r3.DrawMaterial(r3.CubeMesh(), slab, grey)
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
		// The slab's shadow reaches 2·10/6 ≈ 3.3 from the lamp's axis.
		return at(core.V3(0, 0, 2.8)), at(core.V3(0, 0, 6))
	}
	uOff, bOff := shot(false)
	uOn, bOn := shot(true)
	if uOff < 60 {
		t.Fatalf("the lamp does not light the floor: %.0f", uOff)
	}
	if uOn > uOff*0.25 {
		t.Fatalf("the slab casts no traced shadow: floor under it %.0f traced, %.0f without", uOn, uOff)
	}
	if bOn < bOff*0.9 {
		t.Fatalf("the floor beside the slab darkened: %.0f traced, %.0f without", bOn, bOff)
	}
}

// One bounce: a red wall in the sun reddens the shaded floor at its foot
// with FrameSettings.TracedBounce (Post's bounce pass traces it; the
// surface adds it a frame later), and not without it.
func TestTracedBounce(t *testing.T) {
	shot := func(bounce bool) func(x, z float32) (r, b float64) {
		const w, h = 320, 320
		dev, q := headlessDevice(t)
		r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
		if err != nil {
			t.Fatal(err)
		}
		defer r3.Release()
		fs := DefaultFrameSettings()
		fs.Tonemap = TonemapHDR
		fs.Sun = RGBA{R: 2, G: 2, B: 2, A: 1}
		fs.Ambient = RGBA{R: 0.03, G: 0.03, B: 0.03, A: 1}
		fs.TracedShadows, fs.TracedBounce, fs.Temporal = true, bounce, true
		r3.SetFrameSettings(fs)
		r3.KeepDepth = true
		post, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
		if err != nil {
			t.Fatal(err)
		}
		defer post.Release()
		s := &post.Settings
		s.AutoExposure, s.Bloom, s.Grain, s.ChromaticAberration, s.Vignette, s.Sharpen, s.AO = 0, 0, 0, 0, 0, 0, 0
		s.FXAA = false
		s.Temporal = 0.9
		post.Resize(w, h)
		r3.SetRenderSize(w, h)
		r3.SetBounceLight(post.BounceView())
		target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
		tview := target.CreateView()
		defer func() { tview.Release(); target.Release() }()
		floor := core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(60, 1, 60)))
		// A tall wall at x = -4…-3, its +X face toward the sun.
		wall := core.Mat4Translate(core.V3(-3.5, 5, 0)).Mul(core.Mat4Scale(core.V3(1, 10, 20)))
		// A roof over the floor in front of the wall, so that floor is in
		// shade and only bounced light reaches it.
		roof := core.Mat4Translate(core.V3(8, 6, 0)).Mul(core.Mat4Scale(core.V3(10, 0.3, 20)))
		r3.SetTraceScene([]TraceInstance{
			{Model: floor, Color: [3]float32{0.8, 0.8, 0.8}},
			{Model: wall, Color: [3]float32{0.9, 0.05, 0.05}},
			{Model: roof, Color: [3]float32{0.8, 0.8, 0.8}},
		})
		eye := core.V3(4, 3, 26)
		proj := core.Mat4Perspective(0.7, 1, 0.5, 200)
		view := core.Mat4LookAt(eye, core.V3(0, 1, 0), core.V3(0, 1, 0))
		vp := proj.Mul(view)
		// The sun comes down from +X, under the roof's edge onto the wall.
		sun := core.V3(-1, -0.45, 0)
		post.Projection, post.View = proj, view
		post.Atmosphere = Atmosphere{SunDir: sun, Sun: [3]float32{2, 2, 2}, Ambient: [3]float32{0.03, 0.03, 0.03}}
		grey := &Material{Color: RGBA{R: 0.8, G: 0.8, B: 0.8, A: 1}, Roughness: 1}
		red := &Material{Color: RGBA{R: 0.9, G: 0.05, B: 0.05, A: 1}, Roughness: 1}
		for i := 0; i < 16; i++ {
			r3.Begin(vp, sun, eye)
			r3.DrawMaterial(r3.CubeMesh(), floor, grey)
			r3.DrawMaterial(r3.CubeMesh(), wall, red)
			r3.DrawMaterial(r3.CubeMesh(), roof, grey)
			post.UseShadow(r3)
			enc := dev.CreateCommandEncoder()
			if err := r3.Render(enc, post.SceneView(), post.DepthView(), gpu.Color{A: 1}); err != nil {
				t.Fatal(err)
			}
			post.Render(enc, tview)
			q.Submit(enc.Finish())
		}
		pix, err := dev.ReadTexture(q, target, w, h)
		if err != nil {
			t.Fatal(err)
		}
		// The mean over a patch.
		return func(x, z float32) (r, b float64) {
			c := vp.MulVec4(core.V4(x, 0, z, 1))
			px, py := int((c.X/c.W*0.5+0.5)*w), int((0.5-c.Y/c.W*0.5)*h)
			n := 0.0
			for dy := -4; dy <= 4; dy++ {
				for dx := -4; dx <= 4; dx++ {
					i := ((py+dy)*w + px + dx) * 4
					r, b, n = r+float64(pix[i]), b+float64(pix[i+2]), n+1
				}
			}
			return r / n, b / n
		}
	}
	plain, bounced := shot(false), shot(true)
	// The shaded floor a metre and a half in front of the wall's foot.
	r0, b0 := plain(-1.5, 6)
	r1, b1 := bounced(-1.5, 6)
	// (Through the camera's tone curve, and smoothed over the quarter-resolution
	// pass: the red rises by about seven of 255 and the blue by two.)
	if r1 < r0+5 || r1-b1 < (r0-b0)+4 {
		t.Fatalf("no red bounce at the wall's foot: rgb %.0f,·,%.0f with the bounce, %.0f,·,%.0f without", r1, b1, r0, b0)
	}
}

// Traced reflections: a mirror floor seen from steeply above reflects a red
// block that hangs overhead, out of the picture, with
// FrameSettings.TracedReflections; the screen-space reflection alone
// cannot see it.
func TestTracedReflection(t *testing.T) {
	shot := func(traced bool) (r, g float64) {
		const w, h = 320, 240
		dev, q := headlessDevice(t)
		r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
		if err != nil {
			t.Fatal(err)
		}
		defer r3.Release()
		fs := DefaultFrameSettings()
		fs.Tonemap = TonemapHDR
		fs.SkyReflection, fs.SceneReflection, fs.TracedReflections = 1, 1, traced
		fs.SkyTop, fs.SkyHorizon = RGBA{R: 0.02, G: 0.02, B: 0.03, A: 1}, RGBA{R: 0.03, G: 0.03, B: 0.03, A: 1}
		fs.Sun = RGBA{R: 2, G: 2, B: 2, A: 1}
		fs.Ambient = RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1}
		r3.SetFrameSettings(fs)
		r3.KeepDepth = true
		post, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
		if err != nil {
			t.Fatal(err)
		}
		defer post.Release()
		s := &post.Settings
		s.AutoExposure, s.Bloom, s.Grain, s.ChromaticAberration, s.Vignette, s.Sharpen, s.AO = 0, 0, 0, 0, 0, 0, 0
		s.Lift = [3]float32{}
		s.FXAA = false
		post.Resize(w, h)
		r3.SetRenderSize(w, h)
		target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
		tview := target.CreateView()
		defer func() { tview.Release(); target.Release() }()
		floor := core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(60, 1, 60)))
		// Overhead and beyond the top of the picture: the ray mirrored at
		// the floor's origin from this camera passes through it.
		block := core.Mat4Translate(core.V3(0, 7, -3.5)).Mul(core.Mat4Scale(core.V3(5, 1, 5)))
		r3.SetTraceScene([]TraceInstance{
			{Model: floor, Color: [3]float32{0.5, 0.5, 0.5}},
			{Model: block, Color: [3]float32{0.9, 0.05, 0.05}},
		})
		eye := core.V3(0, 6, 3)
		proj := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.1, 300)
		view := core.Mat4LookAt(eye, core.V3(0, 0, 0), core.V3(0, 1, 0))
		sun := core.V3(0, -1, 0.3)
		post.Projection, post.View = proj, view
		post.Atmosphere = Atmosphere{SunDir: sun, Sun: [3]float32{2, 2, 2}, Ambient: [3]float32{0.5, 0.5, 0.5},
			Zenith: [3]float32{0.02, 0.02, 0.03}, Horizon: [3]float32{0.03, 0.03, 0.03}}
		mirror := &Material{Color: RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1}, Roughness: 0.05, Metallic: 1}
		red := &Material{Color: RGBA{R: 0.9, G: 0.05, B: 0.05, A: 1}, Roughness: 1}
		r3.Begin(proj.Mul(view), sun, eye)
		r3.DrawMaterial(r3.CubeMesh(), floor, mirror)
		r3.DrawMaterial(r3.CubeMesh(), block, red)
		post.UseShadow(r3)
		enc := dev.CreateCommandEncoder()
		if err := r3.Render(enc, post.SceneView(), post.DepthView(), gpu.Color{A: 1}); err != nil {
			t.Fatal(err)
		}
		post.Render(enc, tview)
		q.Submit(enc.Finish())
		pix, err := dev.ReadTexture(q, target, w, h)
		if err != nil {
			t.Fatal(err)
		}
		// The floor at the origin is the centre of the picture.
		i := ((h/2)*w + w/2) * 4
		return float64(pix[i]), float64(pix[i+1])
	}
	r0, g0 := shot(false)
	r1, g1 := shot(true)
	// Without tracing the floor mirrors the grey sky there; with it, the red block.
	if r0 > 1.3*g0+10 {
		t.Fatalf("the floor is red without tracing: rgb %.0f, %.0f", r0, g0)
	}
	if r1 < 60 || r1 < 3*g1 {
		t.Fatalf("the floor does not mirror the block overhead: red %.0f, green %.0f traced (%.0f, %.0f without)", r1, g1, r0, g0)
	}
}

// Traced haze: a roof that is only in the trace scene (no shadow map sees
// it) darkens the air under it.
func TestTracedHaze(t *testing.T) {
	shot := func(traced bool) float64 {
		const w, h = 320, 240
		dev, q := headlessDevice(t)
		r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
		if err != nil {
			t.Fatal(err)
		}
		defer r3.Release()
		fs := DefaultFrameSettings()
		fs.Tonemap = TonemapHDR
		fs.TracedHaze = traced
		fs.Sun = RGBA{R: 2, G: 2, B: 2, A: 1}
		fs.Ambient = RGBA{R: 0.1, G: 0.1, B: 0.1, A: 1}
		r3.SetFrameSettings(fs)
		r3.KeepDepth = true
		post, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
		if err != nil {
			t.Fatal(err)
		}
		defer post.Release()
		s := &post.Settings
		s.AutoExposure, s.Bloom, s.Grain, s.ChromaticAberration, s.Vignette, s.Sharpen, s.AO = 0, 0, 0, 0, 0, 0, 0
		s.Lift = [3]float32{}
		s.FXAA = false
		post.Resize(w, h)
		r3.SetRenderSize(w, h)
		target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
		tview := target.CreateView()
		defer func() { tview.Release(); target.Release() }()
		floor := core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(200, 1, 200)))
		roof := core.Mat4Translate(core.V3(0, 12, -30)).Mul(core.Mat4Scale(core.V3(80, 1, 80)))
		r3.SetTraceScene([]TraceInstance{{Model: floor}, {Model: roof}})
		eye := core.V3(0, 2, 0)
		proj := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.1, 300)
		view := core.Mat4LookAt(eye, core.V3(0, 2, -30), core.V3(0, 1, 0))
		sun := core.V3(0.2, -1, 0.1)
		post.Projection, post.View = proj, view
		post.Atmosphere = Atmosphere{SunDir: sun, Sun: [3]float32{2, 2, 2}, Ambient: [3]float32{0.1, 0.1, 0.1},
			Shafts: 0.03, MaxDistance: 60}
		r3.Begin(proj.Mul(view), sun, eye)
		r3.DrawMaterial(r3.CubeMesh(), floor, &Material{Color: RGBA{A: 1}, Roughness: 1})
		post.UseShadow(r3)
		enc := dev.CreateCommandEncoder()
		if err := r3.Render(enc, post.SceneView(), post.DepthView(), gpu.Color{A: 1}); err != nil {
			t.Fatal(err)
		}
		post.Render(enc, tview)
		q.Submit(enc.Finish())
		pix, err := dev.ReadTexture(q, target, w, h)
		if err != nil {
			t.Fatal(err)
		}
		// Just above the horizon: a long look through the air under the roof.
		i := ((h/2-12)*w + w/2) * 4
		return float64(pix[i]) + float64(pix[i+1]) + float64(pix[i+2])
	}
	open, under := shot(false), shot(true)
	if open < 60 {
		t.Fatalf("the haze is not lit without tracing: %.0f", open)
	}
	if under > 0.6*open {
		t.Fatalf("the air under the roof is as bright traced (%.0f) as untraced (%.0f)", under, open)
	}
}
