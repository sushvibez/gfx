package render

import (
	"fmt"
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// PathTracer is the reference renderer of the trace scene: everything by
// rays, nothing rasterised. Each frame sends one path through every pixel
// (the eye's ray, then up to Bounces diffuse bounces, each asking the sun
// by a shadow ray and ending in the sky), and the frames are averaged for
// as long as nothing moves, so the picture starts as noise and converges
// on what the light really does. It is what the fast traced terms
// (shadows, occlusion, bounce, reflections) are compared against, not a
// way to draw a game: see docs/notes/raytracing.md.
//
// It knows what the trace scene knows: shapes in their plain colour
// (TraceInstance.Color), all of them matt. No textures, no lamps, no
// mirrors, no air.
//
// Set View3D.Reference to show it in place of the rasterised picture.
type PathTracer struct {
	// The camera, and the light: SunDir is the direction the light travels.
	Projection, View   core.Mat4
	SunDir             core.Vec3
	Sun                [3]float32 // the sun's light on a surface facing it
	SkyTop, SkyHorizon [3]float32
	// Bounces is how many times a path may bounce (1 to 4; 0 means 3).
	Bounces int
	// Exposure scales the picture before the tone curve (0 means 1).
	Exposure float32
	// Scale is the fraction of the target's pixels traced, each way (0 or
	// more than 1 means 1). A path is dear: half is a quarter of the work.
	Scale float32

	device *gpu.Device
	queue  *gpu.Queue
	plAcc  [2]*gpu.RenderPipeline // without and with mesh traversal
	plShow *gpu.RenderPipeline
	acc    [2]postTex // the running mean, ping-ponged
	uni    *gpu.Buffer
	bgAcc  [2]*gpu.BindGroup
	bgShow [2]*gpu.BindGroup
	// What the groups were built over, and what the mean was taken of.
	nodes, insts, tris *gpu.Buffer
	meshes             bool
	last               pathUniforms
	frames             int // frames in the mean
	cur                int // which acc holds it
}

// pathUniforms mirrors the WGSL PathU block.
type pathUniforms struct {
	InvVP   core.Mat4
	Cam     core.Vec4
	SunDir  core.Vec4 // xyz toward the sun
	Sun     core.Vec4
	Top     core.Vec4
	Horizon core.Vec4
	P       core.Vec4 // x frames in the mean before this one, y bounces, z exposure
}

const pathAccWGSL = `
struct PathU { inv_vp : mat4x4f, cam : vec4f, sun_dir : vec4f, sun : vec4f, top : vec4f, horizon : vec4f, p : vec4f }
@group(0) @binding(0) var<uniform> U : PathU;
@group(0) @binding(1) var prev : texture_2d<f32>;
const PATH_BOUNCES = 4;
// Hashed in turn (pixel x, then y, then the frame, then which number): a
// sum of the four hashed once repeats along lines across the picture.
fn path_mix(v : u32) -> u32 {
    var x = (v ^ (v >> 16u)) * 2246822519u;
    x = (x ^ (x >> 13u)) * 3266489917u;
    return x ^ (x >> 16u);
}
fn path_rand(p : vec2i, k : u32) -> f32 {
    let x = path_mix(path_mix(path_mix(path_mix(u32(p.x)) + u32(p.y)) + u32(U.p.x)) + k);
    return f32(x >> 8u) / 16777216.0;
}
fn path_sky(rd : vec3f) -> vec3f {
    return mix(U.horizon.rgb, U.top.rgb, pow(max(rd.y, 0.0), 0.42));
}
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let px = vec2i(in.pos.xy);
    let dims = vec2f(textureDimensions(prev));
    // The eye's ray, through a random point of the pixel.
    let uv = (vec2f(px) + vec2f(path_rand(px, 1u), path_rand(px, 2u))) / dims;
    let ndc = vec2f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
    let far = U.inv_vp * vec4f(ndc, 1.0, 1.0);
    var ro = U.cam.xyz;
    var rd = normalize(far.xyz / far.w - ro);
    var light = vec3f(0.0);
    var through = vec3f(1.0);
    let bounces = i32(U.p.y);
    for (var b = 0; b <= PATH_BOUNCES; b++) {
        let h = trace_nearest(ro, rd, 0.01, 4000.0);
        if (h.t <= 0.0) {
            light += through * path_sky(rd);
            break;
        }
        if (b >= bounces) { break; } // the path ends on a surface: no more light
        let p = ro + rd * h.t + h.n * 0.02;
        through *= h.color;
        let nl = dot(h.n, U.sun_dir.xyz);
        if (nl > 0.0 && !trace_any(p, U.sun_dir.xyz, 0.01, 4000.0)) {
            light += through * U.sun.rgb * (nl * 0.3183099);
        }
        // On: a direction over the surface, weighted by the cosine.
        let a = path_rand(px, 3u + u32(b) * 2u) * 6.2831853;
        let z = path_rand(px, 4u + u32(b) * 2u) * 2.0 - 1.0;
        let r = sqrt(max(1.0 - z * z, 0.0));
        rd = normalize(h.n + vec3f(r * cos(a), r * sin(a), z) * 0.98);
        ro = p;
    }
    let n = U.p.x;
    let old = textureLoad(prev, px, 0).rgb;
    return vec4f(mix(old, min(light, vec3f(64.0)), 1.0 / (n + 1.0)), 1.0);
}
`

const pathShowWGSL = `
struct PathU { inv_vp : mat4x4f, cam : vec4f, sun_dir : vec4f, sun : vec4f, top : vec4f, horizon : vec4f, p : vec4f }
@group(0) @binding(0) var<uniform> U : PathU;
@group(0) @binding(1) var acc : texture_2d<f32>;
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let dims = vec2f(textureDimensions(acc));
    let t = min(vec2i(in.uv * dims), vec2i(dims) - 1);
    let c = textureLoad(acc, t, 0).rgb * U.p.z;
    // A plain filmic curve, then the display's gamma.
    let m = (c * (2.51 * c + 0.03)) / (c * (2.43 * c + 0.59) + 0.14);
    return vec4f(pow(clamp(m, vec3f(0.0), vec3f(1.0)), vec3f(1.0 / 2.2)), 1.0);
}
`

// NewPathTracer makes a path tracer that draws to targets of the given format.
func NewPathTracer(device *gpu.Device, queue *gpu.Queue, format gpu.TextureFormat) (*PathTracer, error) {
	p := &PathTracer{device: device, queue: queue}
	mk := func(label, body string, f gpu.TextureFormat) (*gpu.RenderPipeline, error) {
		m, err := device.CreateShaderModuleWGSL(label, postCommonWGSL+body)
		if err != nil {
			return nil, fmt.Errorf("render: %s: %w", label, err)
		}
		defer m.Release()
		pl, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{Shader: m, VertexEntry: "vs", FragmentEntry: "fs", Format: f})
		if err != nil {
			return nil, fmt.Errorf("render: %s: %w", label, err)
		}
		return pl, nil
	}
	var err error
	for i, mesh := range []bool{false, true} {
		if p.plAcc[i], err = mk("path-trace", traceWGSL+traceNearestWGSL(mesh)+pathAccWGSL, gpu.FormatRGBA16Float); err != nil {
			p.Release()
			return nil, err
		}
	}
	if p.plShow, err = mk("path-show", pathShowWGSL, format); err != nil {
		p.Release()
		return nil, err
	}
	p.uni = device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, int(unsafe.Sizeof(pathUniforms{})))
	return p, nil
}

// Frames is how many frames the picture is the mean of.
func (p *PathTracer) Frames() int { return p.frames }

// Reset starts the picture again. Render does it by itself when the camera,
// the light, the size or the trace scene changes; call it when the shapes
// moved inside the same trace scene buffers.
func (p *PathTracer) Reset() { p.frames = 0 }

// Render adds one frame of paths through r's trace scene to the mean and
// draws the mean to target (w by h pixels). Without a trace scene it draws
// nothing.
func (p *PathTracer) Render(enc *gpu.CommandEncoder, r *Renderer3D, target *gpu.TextureView, w, h int) {
	inv, ok := p.Projection.Mul(p.View).Inverse()
	viewInv, ok2 := p.View.Inverse()
	if r == nil || r.traceNodes == nil || r.traceShapes == 0 || !ok || !ok2 || w <= 0 || h <= 0 {
		return
	}
	s := p.Scale
	if s <= 0 || s > 1 {
		s = 1
	}
	aw, ah := max(int(float32(w)*s+0.5), 1), max(int(float32(h)*s+0.5), 1)
	rebind := p.nodes != r.traceNodes || p.insts != r.traceInsts || p.tris != r.traceTris || p.meshes != r.traceMeshes
	if p.acc[0].w != aw || p.acc[0].h != ah {
		for i := range p.acc {
			p.acc[i].release()
			t := p.device.CreateTexture2D(aw, ah, gpu.FormatRGBA16Float, gpu.TextureRenderAttachment|gpu.TextureBinding)
			p.acc[i] = postTex{tex: t, view: t.CreateView(), w: aw, h: ah}
		}
		rebind = true
	}
	pl := p.plAcc[0]
	if r.traceMeshes {
		pl = p.plAcc[1]
	}
	if rebind {
		p.nodes, p.insts, p.tris, p.meshes = r.traceNodes, r.traceInsts, r.traceTris, r.traceMeshes
		p.releaseBinds()
		for i := range p.acc {
			// Group i reads acc[i]: the pass that writes the other, and the
			// show pass after acc[i] was written.
			l := pl.BindGroupLayout(0)
			p.bgAcc[i] = p.device.CreateBindGroup(l,
				gpu.BindGroupEntry{Binding: 0, Buffer: p.uni},
				gpu.BindGroupEntry{Binding: 1, TextureView: p.acc[i].view},
				gpu.BindGroupEntry{Binding: 10, Buffer: p.nodes},
				gpu.BindGroupEntry{Binding: 11, Buffer: p.insts},
				gpu.BindGroupEntry{Binding: 13, Buffer: p.tris})
			l.Release()
			l = p.plShow.BindGroupLayout(0)
			p.bgShow[i] = p.device.CreateBindGroup(l,
				gpu.BindGroupEntry{Binding: 0, Buffer: p.uni},
				gpu.BindGroupEntry{Binding: 1, TextureView: p.acc[i].view})
			l.Release()
		}
		p.frames = 0
	}
	bounces := p.Bounces
	if bounces <= 0 {
		bounces = 3
	}
	exposure := p.Exposure
	if exposure <= 0 {
		exposure = 1
	}
	sd := p.SunDir.Normalize().Scale(-1)
	u := pathUniforms{
		InvVP:   inv,
		Cam:     core.V4(viewInv[12], viewInv[13], viewInv[14], 1),
		SunDir:  core.V4(sd.X, sd.Y, sd.Z, 0),
		Sun:     core.V4(p.Sun[0], p.Sun[1], p.Sun[2], 0),
		Top:     core.V4(p.SkyTop[0], p.SkyTop[1], p.SkyTop[2], 0),
		Horizon: core.V4(p.SkyHorizon[0], p.SkyHorizon[1], p.SkyHorizon[2], 0),
		P:       core.V4(0, float32(min(bounces, 4)), exposure, 0),
	}
	// Anything but the frame count changing starts the mean again.
	if u != p.last {
		p.last = u
		p.frames = 0
	}
	u.P.X = float32(p.frames)
	p.queue.WriteBuffer(p.uni, 0, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u)))
	next := 1 - p.cur
	rp := enc.BeginRenderPass(p.acc[next].view, gpu.Color{})
	rp.SetPipeline(pl)
	rp.SetBindGroup(0, p.bgAcc[p.cur])
	rp.Draw(3, 1, 0, 0)
	rp.End()
	p.cur = next
	p.frames++
	rp = enc.BeginRenderPass(target, gpu.Color{A: 1})
	rp.SetPipeline(p.plShow)
	rp.SetBindGroup(0, p.bgShow[p.cur])
	rp.Draw(3, 1, 0, 0)
	rp.End()
}

func (p *PathTracer) releaseBinds() {
	for i := range p.bgAcc {
		if p.bgAcc[i] != nil {
			p.bgAcc[i].Release()
			p.bgAcc[i] = nil
		}
		if p.bgShow[i] != nil {
			p.bgShow[i].Release()
			p.bgShow[i] = nil
		}
	}
}

// Release frees the path tracer's GPU resources.
func (p *PathTracer) Release() {
	p.releaseBinds()
	for i := range p.acc {
		p.acc[i].release()
		p.acc[i] = postTex{}
	}
	for i, pl := range p.plAcc {
		if pl != nil {
			pl.Release()
			p.plAcc[i] = nil
		}
	}
	if p.plShow != nil {
		p.plShow.Release()
		p.plShow = nil
	}
	if p.uni != nil {
		p.uni.Release()
		p.uni = nil
	}
}
