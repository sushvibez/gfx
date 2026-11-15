package render

import (
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// One bounce of light (FrameSettings.TracedBounce), as a pass of its own:
// at a quarter of the resolution each way (bounced light changes slowly
// across a surface, and this is the dearest ray there is), each pixel finds its surface from the depth buffer,
// sends one ray into the hemisphere over it through the renderer's trace
// scene, and writes the light that comes back: the colour of what the ray
// met times the sun (by its own shadow ray) and the sky on it. The
// material shader adds that, times its own colour, to the next frame: a
// frame late, which the temporal filter that this needs anyway hides.
//
// It is a pass and not rays in the material shader because that is where
// it first went and where, in a scene of thousands of shapes on Intel
// integrated graphics, it lost the GPU device (docs/notes/raytracing.md).

// bounceUniforms mirrors the WGSL BounceU block.
type bounceUniforms struct {
	InvVP  core.Mat4 // clip space to world
	Cam    core.Vec4 // xyz eye
	SunDir core.Vec4 // xyz toward the sun
	Sun    core.Vec4 // rgb radiance
	Amb    core.Vec4 // rgb ambient
	P      core.Vec4 // x the noise's frame
}

const postBounceWGSL = `
struct BounceU { inv_vp : mat4x4f, cam : vec4f, sun_dir : vec4f, sun : vec4f, amb : vec4f, p : vec4f }
@group(0) @binding(0) var depth : texture_depth_2d;
@group(0) @binding(1) var<uniform> B : BounceU;
fn bounce_world(t : vec2i, dims : vec2f) -> vec3f {
    let uv = (vec2f(t) + 0.5) / dims;
    let w = B.inv_vp * vec4f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, textureLoad(depth, t, 0), 1.0);
    return w.xyz / w.w;
}
fn bounce_rand(p : vec2i, k : u32) -> f32 {
    var x = u32(p.x) * 1973u + u32(p.y) * 9277u + k * 26699u + u32(B.p.x) * 7919u;
    x = (x ^ (x >> 15u)) * 2246822519u;
    x = (x ^ (x >> 13u)) * 3266489917u;
    x = x ^ (x >> 16u);
    return f32(x) / 4294967296.0;
}
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let dims = vec2f(textureDimensions(depth));
    let dmax = vec2i(dims) - 1;
    let px = vec2i(in.pos.xy);
    let t = min(px * 4 + 1, dmax);
    if (textureLoad(depth, t, 0) >= 0.99999) {
        return vec4f(0.0, 0.0, 0.0, 1.0); // the sky bounces nothing
    }
    // The surface and its normal, from the depth buffer: on each axis the
    // neighbour nearer in depth, so an edge does not bend the normal.
    let p = bounce_world(t, dims);
    let px0 = bounce_world(clamp(t - vec2i(1, 0), vec2i(0), dmax), dims);
    let px1 = bounce_world(clamp(t + vec2i(1, 0), vec2i(0), dmax), dims);
    let py0 = bounce_world(clamp(t - vec2i(0, 1), vec2i(0), dmax), dims);
    let py1 = bounce_world(clamp(t + vec2i(0, 1), vec2i(0), dmax), dims);
    var dx = px1 - p;
    if (length(px0 - p) < length(dx)) { dx = p - px0; }
    var dy = py1 - p;
    if (length(py0 - p) < length(dy)) { dy = p - py0; }
    var n = normalize(cross(dy, dx));
    if (dot(n, p - B.cam.xyz) > 0.0) { n = -n; }
    // One ray into the hemisphere, weighted by the cosine.
    let a = bounce_rand(px, 1u) * 6.2831853;
    let z = bounce_rand(px, 2u) * 2.0 - 1.0;
    let rr = sqrt(max(1.0 - z * z, 0.0));
    let dir = normalize(n + vec3f(rr * cos(a), rr * sin(a), z) * 0.98);
    let start = p + n * 0.05;
    let h = trace_nearest(start, dir, 0.01, 40.0);
    if (h.t <= 0.0) {
        return vec4f(0.0, 0.0, 0.0, 1.0);
    }
    let hl = max(dot(h.n, B.sun_dir.xyz), 0.0);
    var vis = 0.0;
    if (hl > 0.0) {
        vis = select(1.0, 0.0, trace_any(start + dir * h.t + h.n * 0.03, B.sun_dir.xyz, 0.01, 2000.0));
    }
    return vec4f(h.color * (B.sun.rgb * hl * vis * 0.3183099 + B.amb.rgb * 0.5), 1.0);
}
`

func (p *Post) bouncePipelines(mk func(label, body, entry string, format gpu.TextureFormat) *gpu.RenderPipeline) {
	// Two of each tracing pass: without and with the mesh traversal.
	for i, mesh := range []bool{false, true} {
		p.plBounce[i] = mk("post-bounce", traceWGSL+traceNearestWGSL(mesh)+postBounceWGSL, "fs", gpu.FormatRGBA16Float)
	}
}

// bounceTargets allocates the quarter-resolution bounce target (Resize calls it).
func (p *Post) bounceTargets(tex func(w, h int, f gpu.TextureFormat) postTex, w, h int) {
	p.bounce = tex(max(w/4, 1), max(h/4, 1), gpu.FormatRGBA16Float)
	if p.bounceUni == nil {
		p.bounceUni = p.device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, int(unsafe.Sizeof(bounceUniforms{})))
	}
	p.bounceDirty = true
}

// BounceView is the bounce pass's result, for the renderer to read the
// next frame (Renderer3D.SetBounceLight; View3D connects the two). Nil
// before the first Resize.
func (p *Post) BounceView() *gpu.TextureView { return p.bounce.view }

// bounceRender traces the bounce for this frame's surfaces, when the
// renderer asked for it and gave its trace scene (UseShadow).
func (p *Post) bounceRender(enc *gpu.CommandEncoder) {
	vp := p.Projection.Mul(p.View)
	inv, ok := vp.Inverse()
	pl := p.plBounce[0]
	if p.traceMeshes {
		pl = p.plBounce[1]
	}
	if !p.bounceOn || p.traceNodes == nil || !ok || p.Projection[0] == 0 || pl == nil {
		return
	}
	if p.bounceDirty {
		p.bounceDirty = false
		if p.bgBounce != nil {
			p.bgBounce.Release()
		}
		l := pl.BindGroupLayout(0)
		p.bgBounce = p.device.CreateBindGroup(l,
			gpu.BindGroupEntry{Binding: 0, TextureView: p.depthView},
			gpu.BindGroupEntry{Binding: 1, Buffer: p.bounceUni},
			gpu.BindGroupEntry{Binding: 10, Buffer: p.traceNodes},
			gpu.BindGroupEntry{Binding: 11, Buffer: p.traceInsts},
			gpu.BindGroupEntry{Binding: 13, Buffer: p.traceTris},
		)
		l.Release()
	}
	viewInv, _ := p.View.Inverse()
	a := p.Atmosphere
	sd := a.SunDir.Normalize().Scale(-1)
	p.bframe = p.bframe%64 + 1
	u := bounceUniforms{
		InvVP:  inv,
		Cam:    core.V4(viewInv[12], viewInv[13], viewInv[14], 1),
		SunDir: core.V4(sd.X, sd.Y, sd.Z, 0),
		Sun:    core.V4(a.Sun[0], a.Sun[1], a.Sun[2], 0),
		Amb:    core.V4(a.Ambient[0], a.Ambient[1], a.Ambient[2], 0),
		P:      core.V4(float32(p.bframe), 0, 0, 0),
	}
	p.queue.WriteBuffer(p.bounceUni, 0, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u)))
	rp := enc.BeginRenderPass(p.bounce.view, gpu.Color{})
	rp.SetPipeline(pl)
	rp.SetBindGroup(0, p.bgBounce)
	rp.Draw(3, 1, 0, 0)
	rp.End()
}

func (p *Post) bounceReleaseTargets() {
	if p.bgBounce != nil {
		p.bgBounce.Release()
		p.bgBounce = nil
	}
	p.bounce.release()
}

func (p *Post) bounceRelease() {
	for i, pl := range p.plBounce {
		if pl != nil {
			pl.Release()
			p.plBounce[i] = nil
		}
	}
	if p.bounceUni != nil {
		p.bounceUni.Release()
		p.bounceUni = nil
	}
}
