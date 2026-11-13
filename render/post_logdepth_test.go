//go:build !js

package render

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// logDepthWGSL draws flat grey geometry with logarithmic depth, the way a
// planet-scale renderer writes it: z_ndc = log2(1 + w) · C (with C = 0,
// ordinary perspective depth).
const logDepthWGSL = `
struct U { vp : mat4x4f, c : vec4f };
@group(0) @binding(0) var<uniform> u : U;
@vertex fn vs(@location(0) p : vec3f) -> @builtin(position) vec4f {
    var clip = u.vp * vec4f(p, 1.0);
    if (u.c.x > 0.0) { clip.z = log2(max(1e-6, 1.0 + clip.w)) * u.c.x * clip.w; }
    return clip;
}
@fragment fn fs() -> @location(0) vec4f { return vec4f(0.35, 0.35, 0.35, 1.0); }
`

// logDepthAOScene is aoScene's floor and box drawn with logarithmic depth
// and shown through Post with LogDepth set.
func logDepthAOScene(t *testing.T, w, h int, ao float32) []byte {
	t.Helper()
	return logDepthScene(t, w, h, true, func(p *Post) { p.Settings.AO = ao })
}

// logDepthScene draws a floor and a box, with logarithmic depth (and
// Post.LogDepth set) or with perspective depth, and shows it through Post
// after setup has adjusted it (AO is off unless setup turns it on).
func logDepthScene(t *testing.T, w, h int, logDepth bool, setup func(*Post)) []byte {
	t.Helper()
	dev, q := headlessDevice(t)
	mod, err := dev.CreateShaderModuleWGSL("logdepth", logDepthWGSL)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	pl, err := dev.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: mod, VertexEntry: "vs", FragmentEntry: "fs", Format: gpu.FormatRGBA16Float,
		VertexLayouts: []gpu.VertexLayout{{Stride: 12, Attrs: []gpu.VertexAttr{{Format: gpu.VertexFloat32x3, Offset: 0, Location: 0}}}},
		DepthTest:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Release()
	post, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer post.Release()
	s := &post.Settings
	*s = DefaultPostSettings()
	s.AutoExposure, s.Bloom, s.Grain, s.ChromaticAberration, s.Vignette, s.Sharpen = 0, 0, 0, 0, 0, 0
	s.FXAA = false
	s.AO, s.AORadius = 0, 1.5
	setup(post)
	post.Resize(w, h)

	const far = 100.0
	eye := core.V3(0, 5, 9)
	proj := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.1, far)
	vp := proj.Mul(core.Mat4LookAt(eye, core.V3(0, 0.5, 0), core.V3(0, 1, 0)))
	post.Projection = proj
	post.View = core.Mat4LookAt(eye, core.V3(0, 0.5, 0), core.V3(0, 1, 0))
	if logDepth {
		post.LogDepth = float32(1 / math.Log2(far+1))
	}

	// Floor (y = 0) and a 3 m box on it, as triangles.
	var v []float32
	// Each quad is cut into a fine grid, as a terrain mesh is: depth is
	// written per vertex and interpolated linearly, so one long triangle
	// would bow away from the true logarithm between its corners.
	quad := func(a, b, c, d core.Vec3) {
		const n = 24
		at := func(i, j int) core.Vec3 {
			u, w := float32(i)/n, float32(j)/n
			ab := a.Add(b.Sub(a).Scale(u))
			dc := d.Add(c.Sub(d).Scale(u))
			return ab.Add(dc.Sub(ab).Scale(w))
		}
		for j := 0; j < n; j++ {
			for i := 0; i < n; i++ {
				for _, p := range []core.Vec3{at(i, j), at(i+1, j), at(i+1, j+1), at(i, j), at(i+1, j+1), at(i, j+1)} {
					v = append(v, p.X, p.Y, p.Z)
				}
			}
		}
	}
	// The floor stops short of the camera (z = 9): a triangle reaching
	// behind the eye cannot be drawn with per-vertex logarithmic depth.
	quad(core.V3(-20, 0, -20), core.V3(20, 0, -20), core.V3(20, 0, 7), core.V3(-20, 0, 7))
	const b = 1.5
	quad(core.V3(-b, 0, b), core.V3(b, 0, b), core.V3(b, 3, b), core.V3(-b, 3, b))     // front
	quad(core.V3(-b, 0, -b), core.V3(-b, 0, b), core.V3(-b, 3, b), core.V3(-b, 3, -b)) // left
	quad(core.V3(b, 0, b), core.V3(b, 0, -b), core.V3(b, 3, -b), core.V3(b, 3, b))     // right
	quad(core.V3(-b, 3, b), core.V3(b, 3, b), core.V3(b, 3, -b), core.V3(-b, 3, -b))   // top
	vb := dev.CreateBuffer(gpu.BufferVertex|gpu.BufferCopyDst, len(v)*4)
	defer vb.Release()
	q.WriteBuffer(vb, 0, floatBytes(v))
	ub := dev.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, 80)
	defer ub.Release()
	q.WriteBuffer(ub, 0, floatBytes(append(append([]float32{}, vp[:]...), post.LogDepth, 0, 0, 0)))
	lay := pl.BindGroupLayout(0)
	bg := dev.CreateBindGroup(lay, gpu.BindGroupEntry{Binding: 0, Buffer: ub})
	lay.Release()
	defer bg.Release()

	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	defer func() { view.Release(); target.Release() }()
	enc := dev.CreateCommandEncoder()
	pass := enc.BeginRenderPassDepthStore(post.SceneView(), post.DepthView(), gpu.Color{R: 0.2, G: 0.2, B: 0.2, A: 1}) // Post reads the depth
	pass.SetPipeline(pl)
	pass.SetBindGroup(0, bg)
	pass.SetVertexBuffer(0, vb)
	pass.Draw(len(v)/3, 1, 0, 0)
	pass.End()
	post.Render(enc, view)
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return pix
}

// With Post.LogDepth, ambient occlusion works on a logarithmic depth
// buffer: the crease where the box meets the floor darkens, open floor
// does not.
func TestPostAOLogDepth(t *testing.T) {
	const w, h = 480, 320
	off, on := logDepthAOScene(t, w, h, 0), logDepthAOScene(t, w, h, 1)
	maxDrop, mx, my := 0.0, 0, 0
	for y := 0; y < h; y += 2 {
		for x := 0; x < w; x += 2 {
			if d := lumAt(off, w, x, y) - lumAt(on, w, x, y); d > maxDrop {
				maxDrop, mx, my = d, x, y
			}
		}
	}
	if maxDrop < 12 {
		t.Fatalf("AO on log depth darkened nothing: the largest drop is %v", maxDrop)
	}
	for _, p := range [][2]int{{30, h - 20}, {w - 30, h - 20}} {
		if d := lumAt(off, w, p[0], p[1]) - lumAt(on, w, p[0], p[1]); d > 4 || d < -4 {
			t.Fatalf("open floor at %v changed by %v with AO", p, d)
		}
	}
	if mx < w/5 || mx > 4*w/5 || my < h/4 {
		t.Fatalf("the darkest occlusion is at (%d, %d), not by the box", mx, my)
	}
}

// The atmosphere reads a logarithmic depth buffer as it reads a
// perspective one: the same floor and box in the same haze come out the
// same either way (they did not while its passes assumed perspective).
func TestPostShaftsLogDepth(t *testing.T) {
	const w, h = 480, 320
	haze := func(p *Post) {
		p.Settings.Lift = [3]float32{}
		p.Atmosphere = Atmosphere{Shafts: 0.15, Falloff: 1000, MaxDistance: 60,
			SunDir: core.V3(0.3, -1, -0.2), Sun: [3]float32{2, 1.8, 1.5}, Ambient: [3]float32{0.2, 0.25, 0.3}}
	}
	persp, logd := logDepthScene(t, w, h, false, haze), logDepthScene(t, w, h, true, haze)
	clear := logDepthScene(t, w, h, true, func(p *Post) { p.Settings.Lift = [3]float32{} })
	worst := 0.0
	for _, p := range [][2]int{{40, h - 20}, {w / 2, h - 30}, {w - 40, h - 20}, {60, h/2 + 10}, {w / 2, h / 2}, {w - 60, h/2 + 10}} {
		a, b := lumAt(persp, w, p[0], p[1]), lumAt(logd, w, p[0], p[1])
		worst = math.Max(worst, math.Abs(a-b))
	}
	if worst > 3 {
		t.Fatalf("haze over log depth differs from perspective by up to %v", worst)
	}
	// And the haze is there: the far floor turns toward the air's colour.
	if d := lumAt(logd, w, w/2, h/2) - lumAt(clear, w, w/2, h/2); math.Abs(d) < 3 {
		t.Fatalf("the haze over log depth changes the far floor by only %v", d)
	}
}

// UseSunShadow takes a shadow map from another renderer: with every point
// of air behind its occluder the sun no longer scatters toward the eye.
func TestPostUseSunShadow(t *testing.T) {
	const w, h = 480, 320
	sun := func(shadowed bool) func(*Post) {
		return func(p *Post) {
			p.Settings.Lift = [3]float32{}
			p.Atmosphere = Atmosphere{Shafts: 0.06, Falloff: 1000, MaxDistance: 60,
				SunDir: core.V3(0, -1, 0), Sun: [3]float32{3, 3, 3}}
			if !shadowed {
				return
			}
			// A map holding depth 0 (an occluder at the sun) everywhere, and
			// a light matrix looking down: x, z across it, height along it.
			dev := p.device
			mod, err := dev.CreateShaderModuleWGSL("occluder", `
@vertex fn vs(@builtin(vertex_index) i : u32) -> @builtin(position) vec4f {
    let xy = vec2f(f32((i << 1u) & 2u), f32(i & 2u)) * 2.0 - 1.0;
    return vec4f(xy, 0.0, 1.0);
}`)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Release()
			pl, err := dev.CreateRenderPipeline(&gpu.RenderPipelineDesc{Shader: mod, VertexEntry: "vs", DepthTest: true})
			if err != nil {
				t.Fatal(err)
			}
			defer pl.Release()
			tex := dev.CreateShadowMap(64)
			view := tex.CreateView()
			t.Cleanup(func() { view.Release(); tex.Release() })
			enc := dev.CreateCommandEncoder()
			pass := enc.BeginShadowPass(view)
			pass.SetPipeline(pl)
			pass.Draw(3, 1, 0, 0)
			pass.End()
			p.queue.Submit(enc.Finish())
			var lvp core.Mat4
			lvp[0], lvp[9], lvp[6], lvp[14], lvp[15] = 1.0/50, 1.0/50, -1.0/100, 0.5, 1
			p.UseSunShadow(view, lvp)
		}
	}
	lit, dark := logDepthScene(t, w, h, true, sun(false)), logDepthScene(t, w, h, true, sun(true))
	a, b := lumAt(lit, w, w/2, h/2), lumAt(dark, w, w/2, h/2)
	if a-b < 10 {
		t.Fatalf("the shadowed air is not darker: %v lit, %v in the map's shadow", a, b)
	}
}
