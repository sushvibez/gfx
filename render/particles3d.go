package render

import (
	"math"
	"sort"
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render/particles"
	"github.com/sushvibez/gfx/render/wgsl"
)

// Particles in a Renderer3D: SetParticles hands over a pool's sprites
// (render/particles: smoke, dust, steam, flame lobes, debris), drawn with
// the particle pass every gfx renderer shares (particles.Shade) after the
// opaque scene. Under a View3D with Post they are soft: drawn in a pass of
// their own after the lit picture is finished (RenderRain, with the rain),
// each fragment reading the stored depth and fading out over the last part
// of a metre in front of the scene; otherwise they go into the main pass,
// depth-tested by the hardware. Each is lit at its centre by the sun
// through the shadow map, the ambient light from above and a little from
// the ground, and every point and spot light (a puff round a lamp glows),
// and fogged like the meshes.

// ptCamData mirrors the WGSL PtCam block.
type ptCamData struct {
	Right [4]float32 // camera right (world), w: a pixel's angle (rad)
	Up    [4]float32 // camera up
	Sun   [4]float32 // toward the sun
	Depth [4]float32 // x, y: the projection's z terms (view distance = y / (d + x)); zw: 0
	Cam   [4]float32 // the eye (world)
}

// ptPartData mirrors particles.Shade's Part (64 B).
type ptPartData struct {
	Pos, Col, Misc, Ext [4]float32
}

type particlePass struct {
	failed           bool
	hardPL, softPL   *gpu.RenderPipeline
	lay0, lay1       *gpu.BindGroupLayout // the hard pipeline's (auto layouts are per pipeline)
	sLay0, sLay      *gpu.BindGroupLayout // the soft one's
	b0, b1, sb0, sb1 *gpu.BindGroup
	boundDepth       *gpu.TextureView
	camBuf, partBuf  *gpu.Buffer
	atlas            *gpu.Texture
	atlasView        *gpu.TextureView
	atlasSamp        *gpu.Sampler
	items            []particles.Sprite
	data             []ptPartData
	n                int
}

// SetParticles replaces the particles drawn from now on (world positions;
// nil clears them). They persist across frames until replaced.
func (r *Renderer3D) SetParticles(ps []particles.Sprite) {
	r.pt.items = append(r.pt.items[:0], ps...)
}

const particles3DWGSL = frameWGSL + wgsl.Hash + wgsl.Depth + wgsl.Light + wgsl.Shadow + particles.Shade + `
struct Light {
    pos_range : vec4f,
    color : vec4f,
    dir_cos : vec4f,
    params : vec4f,
}
struct Lights { items : array<Light, 16>, spot_vp : array<mat4x4f, 4>, rain_vp : mat4x4f }
struct PtCam { right : vec4f, up : vec4f, sun : vec4f, depth : vec4f, cam : vec4f }
@group(0) @binding(0) var<storage, read> frame : Frame;
@group(0) @binding(1) var<storage, read> lights : Lights;
@group(0) @binding(2) var shadow_samp : sampler_comparison;
@group(0) @binding(3) var shadow_tex : texture_depth_2d;
@group(0) @binding(4) var<storage, read> ptcam : PtCam;

fn pt_eye() -> PtEye { return PtEye(ptcam.right.xyz, ptcam.up.xyz, ptcam.sun.xyz, ptcam.right.w); }
fn pt_clip(rel : vec3f) -> vec4f { return frame.view_proj * vec4f(rel + ptcam.cam.xyz, 1.0); }
fn pt_view_dist(d : f32) -> f32 { return depth_dist(d, 0.0, ptcam.depth.x, ptcam.depth.y); }
fn pt_display(c : vec3f) -> vec3f {
    if (u32(frame.misc.x) == 3u) { return c * frame.ambient.w; }
    return clamp(c * frame.ambient.w, vec3f(0.0), vec3f(1.0));
}
// The sun's shadow at a particle's centre: four compared taps a texel
// apart, so a puff in the shade of a wall is in it, softly.
fn pt_sun_shadow(world : vec3f) -> f32 {
    let sc = frame.light_vp * vec4f(world, 1.0);
    let uv = vec2f(sc.x * 0.5 + 0.5, -sc.y * 0.5 + 0.5);
    if (any(uv < vec2f(0.0)) || any(uv > vec2f(1.0))) { return 1.0; }
    let o = 1.5 / f32(textureDimensions(shadow_tex).x);
    var lit = 0.0;
    for (var i = 0; i < 4; i++) {
        lit += textureSampleCompareLevel(shadow_tex, shadow_samp, uv + vogel(i, 4, 0.7) * o, sc.z - 0.002);
    }
    return mix(1.0, lit * 0.25, pcss_border(uv));
}
fn pt_light(rel : vec3f, radius : f32) -> PtLight {
    var l : PtLight;
    let world = rel + ptcam.cam.xyz;
    l.sun = max(frame.sun.rgb, vec3f(0.0)) * pt_sun_shadow(world);
    l.sky = frame.ambient.rgb * 1.15;
    l.gnd = frame.ambient.rgb * 0.45;
    l.up = vec3f(0.0, 1.0, 0.0);
    // The lamps at its centre, whatever their direction (a puff round a
    // light glows from inside), averaged over its ball.
    var acc = vec3f(0.0);
    let count = u32(frame.misc.y);
    for (var i = 0u; i < count; i = i + 1u) {
        let li = lights.items[i];
        let dv = li.pos_range.xyz - world;
        let d2 = dot(dv, dv);
        let r2 = li.pos_range.w * li.pos_range.w;
        if (d2 >= r2) { continue; }
        var att = light_window(d2, r2, radius * radius);
        if (li.color.w > 0.5) {
            att = att * smoothstep(li.dir_cos.w, li.params.x, dot(-dv / max(sqrt(d2), 1e-4), li.dir_cos.xyz));
        }
        acc += li.color.rgb * att;
    }
    l.plit = acc * 0.35;
    // The meshes' height fog, as a transmittance and an in-scatter.
    l.tr = vec3f(1.0);
    l.insc = vec3f(0.0);
    if (frame.fog_params.w > 0.5) {
        let dist = length(rel);
        let mid = (world.y + frame.cam_pos.y) * 0.5 - frame.fog_params.x;
        let hf = exp(-clamp(mid * frame.fog_params.y, -20.0, 20.0));
        let f = clamp(1.0 - exp(-dist * frame.fog_color.w * hf), 0.0, 1.0);
        l.tr = vec3f(1.0 - f);
        l.insc = frame.fog_color.rgb * f;
    }
    return l;
}
`

// initParticles builds the particle pass lazily (only a scene with
// particles pays for it). It reports whether the pass can be drawn.
func (r *Renderer3D) initParticles() bool {
	st := &r.pt
	if st.hardPL != nil {
		return true
	}
	if st.failed {
		return false
	}
	before := gpu.ErrorCount()
	m, err := r.device.CreateShaderModuleWGSL("renderer3d-particles", particles3DWGSL)
	if err != nil || gpu.ErrorCount() != before {
		st.failed = true
		return false
	}
	defer m.Release()
	blend := &gpu.BlendDesc{
		Color: gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendOne, Dst: gpu.BlendOneMinusSrcAlpha},
		Alpha: gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendZero, Dst: gpu.BlendOne},
	}
	st.hardPL, err = r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: m, VertexEntry: "vs_particle", FragmentEntry: "fs_particle", Format: r.format,
		Blend: blend, DepthTest: true, DepthNoWrite: true,
	})
	if err == nil {
		st.softPL, err = r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
			Shader: m, VertexEntry: "vs_particle", FragmentEntry: "fs_particle_soft", Format: r.format, Blend: blend,
		})
	}
	if err != nil || gpu.ErrorCount() != before {
		st.failed = true
		return false
	}
	st.lay0 = st.hardPL.BindGroupLayout(0)
	st.lay1 = st.hardPL.BindGroupLayout(1)
	st.sLay0 = st.softPL.BindGroupLayout(0)
	st.sLay = st.softPL.BindGroupLayout(1)
	st.camBuf = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, int(unsafe.Sizeof(ptCamData{})))
	e0 := []gpu.BindGroupEntry{
		{Binding: 0, Buffer: r.frame},
		{Binding: 1, Buffer: r.lightBuf},
		{Binding: 2, Sampler: r.shadowSampler},
		{Binding: 3, TextureView: r.shadowView},
		{Binding: 4, Buffer: st.camBuf},
	}
	st.b0 = r.device.CreateBindGroup(st.lay0, e0...)
	st.sb0 = r.device.CreateBindGroup(st.sLay0, e0...)
	lv := particles.SmokeAtlas()
	aw, ah := particles.SmokeCols*particles.SmokeCell, particles.SmokeRows*particles.SmokeCell
	st.atlas = r.device.CreateTexture2DMips(aw, ah, gpu.FormatRGBA8Unorm, gpu.TextureBinding|gpu.TextureCopyDst, len(lv))
	for i, px := range lv {
		r.queue.WriteTextureMip(st.atlas, i, aw>>i, ah>>i, px)
	}
	st.atlasView = st.atlas.CreateView()
	st.atlasSamp = r.device.CreateSamplerMip(false, 1)
	r.growParticles(256)
	return true
}

// growParticles sizes the instance buffer for n particles.
func (r *Renderer3D) growParticles(n int) {
	st := &r.pt
	if st.partBuf != nil {
		st.partBuf.Release()
		st.b1.Release()
	}
	st.partBuf = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, n*int(unsafe.Sizeof(ptPartData{})))
	st.b1 = r.device.CreateBindGroup(st.lay1, r.particleEntries()...)
	if st.sb1 != nil {
		st.sb1.Release()
		st.sb1, st.boundDepth = nil, nil
	}
}

func (r *Renderer3D) particleEntries() []gpu.BindGroupEntry {
	return []gpu.BindGroupEntry{
		{Binding: 0, Buffer: r.pt.partBuf},
		{Binding: 8, TextureView: r.pt.atlasView},
		{Binding: 9, Sampler: r.pt.atlasSamp},
	}
}

// prepareParticles writes this frame's particles, camera-relative and
// back to front, and the camera block (Render, after Begin).
func (r *Renderer3D) prepareParticles() {
	st := &r.pt
	st.n = 0
	if len(st.items) == 0 || !r.initParticles() {
		return
	}
	vp := r.viewProj
	row := func(i int) [4]float32 { return [4]float32{vp[i], vp[4+i], vp[8+i], vp[12+i]} }
	r0, r1, r2, r3 := row(0), row(1), row(2), row(3)
	unit := func(v [4]float32) (core.Vec3, float32) {
		l := float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
		if l == 0 {
			return core.Vec3{}, 0
		}
		return core.V3(v[0]/l, v[1]/l, v[2]/l), l
	}
	right, _ := unit(r0)
	up, fy := unit(r1)
	fwd, fw := unit(r3)
	lines := float32(r.renderH)
	if lines <= 0 {
		lines = 900
	}
	// The projection's depth terms from the matrix: z_clip = a·z + b and
	// w = -z for a view-space z, so a depth d is at w = b / (d + a).
	var a, b float32
	if fw > 0 {
		a = -(r2[0]*r3[0] + r2[1]*r3[1] + r2[2]*r3[2]) / (fw * fw)
		b = r2[3] + a*r3[3]
	}
	sun := r.sunDir
	cam := r.camPos
	c := ptCamData{
		Right: [4]float32{right.X, right.Y, right.Z, 2 / (fy * lines)},
		Up:    [4]float32{up.X, up.Y, up.Z, 0},
		Sun:   [4]float32{sun.X, sun.Y, sun.Z, 0},
		Depth: [4]float32{a, b, 0, 0},
		Cam:   [4]float32{cam.X, cam.Y, cam.Z, 0},
	}
	r.queue.WriteBuffer(st.camBuf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&c)), unsafe.Sizeof(c)))
	along := func(p particles.Vec3) float64 {
		return float64(fwd.X)*(p.X-float64(cam.X)) + float64(fwd.Y)*(p.Y-float64(cam.Y)) + float64(fwd.Z)*(p.Z-float64(cam.Z))
	}
	sort.Slice(st.items, func(i, j int) bool { return along(st.items[i].Pos) > along(st.items[j].Pos) })
	st.data = st.data[:0]
	for _, p := range st.items {
		if along(p.Pos) < -p.Radius {
			continue // wholly behind the eye
		}
		st.data = append(st.data, ptPartData{
			Pos:  [4]float32{float32(p.Pos.X - float64(cam.X)), float32(p.Pos.Y - float64(cam.Y)), float32(p.Pos.Z - float64(cam.Z)), float32(p.Radius)},
			Col:  [4]float32{p.Color[0], p.Color[1], p.Color[2], p.Alpha},
			Misc: [4]float32{p.Emit, p.Rot, float32(p.Kind), p.Seed},
			Ext:  [4]float32{p.Age, 0, 0, 0},
		})
	}
	st.n = len(st.data)
	if st.n == 0 {
		return
	}
	size := st.n * int(unsafe.Sizeof(ptPartData{}))
	if size > st.partBuf.Size() {
		r.growParticles(st.n * 3 / 2)
	}
	r.queue.WriteBuffer(st.partBuf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&st.data[0])), size))
}

// drawParticles records the particles into the main pass (hard: the
// hardware depth test).
func (r *Renderer3D) drawParticles(pass *gpu.RenderPass) {
	st := &r.pt
	if st.n == 0 {
		return
	}
	pass.SetPipeline(st.hardPL)
	pass.SetBindGroup(0, st.b0)
	pass.SetBindGroup(1, st.b1)
	pass.Draw(6, st.n, 0, 0)
}

// renderParticlesSoft draws the particles over a finished picture in a
// pass of their own, each fragment testing itself against depth.
func (r *Renderer3D) renderParticlesSoft(enc *gpu.CommandEncoder, color, depth *gpu.TextureView) {
	st := &r.pt
	if st.n == 0 {
		return
	}
	if st.sb1 == nil || st.boundDepth != depth {
		if st.sb1 != nil {
			st.sb1.Release()
		}
		st.sb1 = r.device.CreateBindGroup(st.sLay, append(r.particleEntries(), gpu.BindGroupEntry{Binding: 10, TextureView: depth})...)
		st.boundDepth = depth
	}
	pass := enc.BeginRenderPassLoad(color)
	pass.SetPipeline(st.softPL)
	pass.SetBindGroup(0, st.sb0)
	pass.SetBindGroup(1, st.sb1)
	pass.Draw(6, st.n, 0, 0)
	pass.End()
}

func (r *Renderer3D) releaseParticles() {
	st := &r.pt
	for _, b := range []*gpu.BindGroup{st.b0, st.b1, st.sb0, st.sb1} {
		rel(b)
	}
	for _, b := range []*gpu.Buffer{st.camBuf, st.partBuf} {
		rel(b)
	}
	rel(st.atlasView)
	rel(st.atlas)
	rel(st.atlasSamp)
	rel(st.lay0)
	rel(st.lay1)
	rel(st.sLay0)
	rel(st.sLay)
	rel(st.hardPL)
	rel(st.softPL)
	*st = particlePass{}
}
