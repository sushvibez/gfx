//go:build !js

package render

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// foreignMirrorWGSL draws a dark floor that is a mirror (it leaves 1 − 0.8 in
// alpha when u.c.y is set, and adds its own reflection of a sky of
// brightness u.c.z, as a material does) and a bright box on it, with logarithmic depth,
// the way a planet renderer that is not a Renderer3D writes its scene.
const foreignMirrorWGSL = `
struct U { vp : mat4x4f, c : vec4f };
@group(0) @binding(0) var<uniform> u : U;
struct V { @builtin(position) pos : vec4f, @location(0) world : vec3f };
@vertex fn vs(@location(0) p : vec3f) -> V {
    var clip = u.vp * vec4f(p, 1.0);
    clip.z = log2(max(1e-6, 1.0 + clip.w)) * u.c.x * clip.w;
    return V(clip, p);
}
@fragment fn fs(in : V) -> @location(0) vec4f {
    if (in.world.y < 0.01) {
        let m = select(0.0, 0.8, u.c.y > 0.5);
        return vec4f(vec3f(0.03 + m * u.c.z), 1.0 - m);
    }
    return vec4f(0.9, 0.9, 0.9, 1.0);
}
`

// foreignMirrorScene draws foreignMirrorWGSL's floor and box (the clear colour, 0.3
// grey at the far depth, is the sky) and shows it through Post with
// setup applied; mirror sets the floor's mirror strength in alpha, and
// sky (one value at most) is the sky the floor's material reflects.
func foreignMirrorScene(t *testing.T, w, h int, mirror bool, setup func(*Post), sky ...float32) ([]byte, core.Mat4) {
	t.Helper()
	dev, q := headlessDevice(t)
	mod, err := dev.CreateShaderModuleWGSL("mirror", foreignMirrorWGSL)
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
	s.FXAA, s.AO, s.Lift = false, 0, [3]float32{}
	post.Resize(w, h)
	const far = 200.0
	proj := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.1, far)
	view := core.Mat4LookAt(core.V3(0, 2, 9), core.V3(0, 0.5, 0), core.V3(0, 1, 0))
	post.Projection, post.View = proj, view
	post.LogDepth = float32(1 / math.Log2(far+1))
	setup(post)

	var v []float32
	quad := func(a, b, c, d core.Vec3) {
		const n = 24 // fine, as a terrain mesh is: log depth is per vertex
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
	quad(core.V3(-30, 0, -40), core.V3(30, 0, -40), core.V3(30, 0, 7), core.V3(-30, 0, 7))
	const b = 1.5
	quad(core.V3(-b, 0, b), core.V3(b, 0, b), core.V3(b, 3, b), core.V3(-b, 3, b))
	quad(core.V3(-b, 0, -b), core.V3(-b, 0, b), core.V3(-b, 3, b), core.V3(-b, 3, -b))
	quad(core.V3(b, 0, b), core.V3(b, 0, -b), core.V3(b, 3, -b), core.V3(b, 3, b))
	quad(core.V3(-b, 3, b), core.V3(b, 3, b), core.V3(b, 3, -b), core.V3(-b, 3, -b))
	vb := dev.CreateBuffer(gpu.BufferVertex|gpu.BufferCopyDst, len(v)*4)
	defer vb.Release()
	q.WriteBuffer(vb, 0, floatBytes(v))
	ub := dev.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, 80)
	defer ub.Release()
	m := float32(0)
	if mirror {
		m = 1
	}
	vp := proj.Mul(view)
	matSky := float32(0)
	if len(sky) > 0 {
		matSky = sky[0]
	}
	q.WriteBuffer(ub, 0, floatBytes(append(append([]float32{}, vp[:]...), post.LogDepth, m, matSky, 0)))
	lay := pl.BindGroupLayout(0)
	bg := dev.CreateBindGroup(lay, gpu.BindGroupEntry{Binding: 0, Buffer: ub})
	lay.Release()
	defer bg.Release()

	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	tv := target.CreateView()
	defer func() { tv.Release(); target.Release() }()
	enc := dev.CreateCommandEncoder()
	pass := enc.BeginRenderPassDepthStore(post.SceneView(), post.DepthView(), gpu.Color{R: 0.3, G: 0.3, B: 0.3, A: 1})
	pass.SetPipeline(pl)
	pass.SetBindGroup(0, bg)
	pass.SetVertexBuffer(0, vb)
	pass.Draw(len(v)/3, 1, 0, 0)
	pass.End()
	post.Render(enc, tv)
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return pix, vp
}

// pixelOf is the pixel a world point projects to.
func pixelOf(vp core.Mat4, p core.Vec3, w, h int) (int, int) {
	c := vp.MulVec4(core.V4(p.X, p.Y, p.Z, 1))
	return int((c.X/c.W*0.5 + 0.5) * float32(w)), int((0.5 - c.Y/c.W*0.5) * float32(h))
}

// A renderer that is not a Renderer3D gets screen-space reflections
// through UseReflection: its mirror (strength in alpha) shows the box
// standing on it, takes the sky from the scene where a ray leaves through
// it, and swaps the sky the material reflected (Reflection.Sky) for what
// the ray meets.
func TestPostUseReflection(t *testing.T) {
	const w, h = 480, 320
	on := func(r *Reflection) func(*Post) { return func(p *Post) { p.UseReflection(r) } }
	off, vp := foreignMirrorScene(t, w, h, true, func(*Post) {})
	level, _ := foreignMirrorScene(t, w, h, true, on(&Reflection{Level: true, SkyInScene: true}))
	dry, _ := foreignMirrorScene(t, w, h, false, on(&Reflection{Level: true, SkyInScene: true}))
	// The box's mirror image: its front face's middle, reflected in the floor.
	bx, by := pixelOf(vp, core.V3(0, -1.2, 1.5), w, h)
	a, b := lumAt(off, w, bx, by), lumAt(level, w, bx, by)
	if b-a < 25 {
		t.Fatalf("the floor under the box reads %v without reflections, %v with: no box in the mirror", a, b)
	}
	if d := lumAt(dry, w, bx, by); math.Abs(d-a) > 2 {
		t.Fatalf("a floor with no mirror strength changed from %v to %v", a, d)
	}
	// Off to the side and far, the floor mirrors the sky drawn in the scene.
	sx, sy := pixelOf(vp, core.V3(9, 0, -12), w, h)
	if a, b := lumAt(off, w, sx, sy), lumAt(level, w, sx, sy); b-a < 15 {
		t.Fatalf("the far floor reads %v without reflections, %v with: the sky in the scene is not mirrored", a, b)
	}
	// A material that reflects a sky has already added it to the mirror.
	// Given that sky as a map, Post takes it out where the ray meets the
	// box, so the box reads as it does over no sky; not given it, the box
	// is added on top of the sky and reads brighter. (Post never takes out
	// more than the surface holds, so the material has to have put it in.)
	const skyLevel = 0.6
	skyMap := func(p *Post) {
		tex := p.device.CreateTexture2D(4, 2, gpu.FormatRGBA16Float, gpu.TextureRenderAttachment|gpu.TextureBinding)
		view := tex.CreateView()
		t.Cleanup(func() { view.Release(); tex.Release() })
		enc := p.device.CreateCommandEncoder()
		enc.BeginRenderPass(view, gpu.Color{R: skyLevel, G: skyLevel, B: skyLevel, A: 1}).End()
		p.queue.Submit(enc.Finish())
		p.UseReflection(&Reflection{Level: true, Sky: view})
	}
	swapped, _ := foreignMirrorScene(t, w, h, true, skyMap, skyLevel)
	stacked, _ := foreignMirrorScene(t, w, h, true, on(&Reflection{Level: true}), skyLevel)
	c, d := lumAt(swapped, w, bx, by), lumAt(stacked, w, bx, by)
	if math.Abs(c-b) > 4 {
		t.Fatalf("with the sky map the box's reflection reads %v, over no sky %v: the sky is not swapped for the box", c, b)
	}
	if d-c < 5 {
		t.Fatalf("the box's reflection reads %v with the sky map and %v without: the map takes nothing out", c, d)
	}
}
