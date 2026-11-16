package render

// Temporal anti-aliasing, for Post. The scene renderer offsets its
// projection by a different fraction of a pixel every frame (Jitter, a
// Halton (2, 3) sequence of eight), so over eight frames each pixel is
// sampled at eight places inside it; Post keeps a history of what it showed
// and blends each new frame into it at a tenth. A still picture converges
// to an eight-times supersampled one: a needle thinner than a pixel, which
// a single sample catches in one frame and misses in the next, settles to
// its share of the pixel instead of shimmering.
//
// The history moves with the camera: each pixel's point, rebuilt from the
// nearest depth round it (perspective or logarithmic, as the other depth
// passes do), is projected with the previous frame's camera (the one
// motion blur uses, so UsePreviousView and ResetMotion apply to both), and
// the history is read there through a Catmull-Rom filter, which does not
// soften it the way a bilinear read would. What the history shows that the
// pixel's neighbourhood no longer does is clipped to that neighbourhood's
// colour range (3×3, in YCoCg, tightened to the mean ± 1.25 standard
// deviations), so a camera pan leaves no ghosts. A point that was hidden
// last frame (its distance does not match the one the history kept there)
// starts afresh; this test runs only where the camera's travel moves
// things by more than a fraction of a pixel, because a sub-pixel needle
// comes and goes from a pixel's depth from frame to frame. Such a pixel —
// its nearest surface changing again and again, counted in the history's
// alpha — keeps its history unclipped while the motion is slow and nothing
// nearer arrives: in some of the jitter's frames the needle is missing
// from the whole neighbourhood, and clipping would wipe it each time. (A
// drone leaving a pixel changes it once, and is still clipped away.)
// Things that move by themselves have no motion vectors: the clip alone
// keeps them from ghosting. The blend runs
// on exposed, tone-compressed colour (c / (1 + luma)), so one hot sample
// cannot dominate a pixel.
//
// The pass runs on the HDR image after the atmosphere and before motion
// blur and the bloom. FXAA is skipped while it is on, and the final pass
// sharpens a little more.

import (
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// taaUniforms mirrors the WGSL TaaU struct.
type taaUniforms struct {
	Reproj core.Mat4 // this frame's view space to the previous frame's clip space
	M      core.Vec4 // x 1 = the history holds, y the eye's travel in pixels at distance 1, z the new frame's weight, w how much of the clip a still picture lets go
	Ride   core.Mat4 // Reproj for a point that rides with the eye (Post.RideRadius)
	R      core.Vec4 // x the ride radius
}

// Temporal anti-aliasing's constants: the jitter's cycle, the new frame's
// share of each pixel, and the sharpening added to PostSettings.Sharpen.
const (
	taaSamples = 8
	taaBlend   = 0.1
	taaSharpen = 0.1
	taaHold    = 1
)

type postTAA struct {
	on    bool       // the pass runs this frame
	valid bool       // hist[1-k] holds the last frame's result
	jit   [2]float32 // the scene's jitter this frame (NDC)
	next  [2]float32 // the jitter Jitter handed out
	asked bool       // Jitter was called since the last Render

	hist   [2]postTex // the result, frame by frame in turn; alpha keeps log2(1 + distance)
	uni    *gpu.Buffer
	pl     *gpu.RenderPipeline
	plCopy *gpu.RenderPipeline
	bg     [2]*gpu.BindGroup // frame k: p.pre and hist[1-k] into hist[k]
	bgCopy [2]*gpu.BindGroup // hist[k] into p.lit
}

// Jitter is the offset, in NDC, to add to this frame's projection for
// temporal anti-aliasing (PostSettings.TAA): x and y within half a pixel
// of the scene target either way (a pixel is 2/w by 2/h in NDC). A
// renderer offsets its clip-space position by (dx·w, dy·w) — for a
// projection matrix, JitterProjection does it — and leaves Post.Projection
// as it was: Post takes the jitter back out wherever it rebuilds points
// from depth. Call it once a frame after Resize and before Render; a frame
// that does not call it is taken to be drawn without jitter. View3D does
// it for a Renderer3D. It is (0, 0) while TAA is off.
func (p *Post) Jitter() (dx, dy float32) {
	if !p.Settings.TAA || p.w == 0 {
		p.taa.asked = false
		return 0, 0
	}
	i := p.frame%taaSamples + 1
	dx = (halton(i, 2) - 0.5) * 2 / float32(p.w)
	dy = (halton(i, 3) - 0.5) * 2 / float32(p.h)
	p.taa.next, p.taa.asked = [2]float32{dx, dy}, true
	return dx, dy
}

// JitterProjection offsets a projection (or view-projection) matrix by
// (dx, dy) in NDC: every point lands dx, dy further across the screen,
// whatever its depth. For Post.Jitter.
func JitterProjection(m core.Mat4, dx, dy float32) core.Mat4 {
	for c := 0; c < 4; c++ {
		m[c*4] += dx * m[c*4+3]
		m[c*4+1] += dy * m[c*4+3]
	}
	return m
}

// halton is the i-th element (from 1) of the van der Corput sequence in
// base b.
func halton(i, b int) float32 {
	f, r := float32(1), float32(0)
	for ; i > 0; i /= b {
		f /= float32(b)
		r += f * float32(i%b)
	}
	return r
}

// taaPipelines builds the passes (NewPost calls it through mk).
func (p *Post) taaPipelines(mk func(label, body, entry string, format gpu.TextureFormat) *gpu.RenderPipeline) {
	p.taa.pl = mk("post-taa", postTAAWGSL, "fs", gpu.FormatRGBA16Float)
	p.taa.plCopy = mk("post-taa-copy", postCopyWGSL, "fs", gpu.FormatRGBA16Float)
	p.taa.uni = p.device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, int(unsafe.Sizeof(taaUniforms{})))
}

// preTarget makes sure p.pre, the scene with the air before the passes
// that follow it, exists.
func (p *Post) preTarget() {
	if p.pre.tex == nil {
		t := p.device.CreateTexture2D(p.w, p.h, gpu.FormatRGBA16Float, gpu.TextureRenderAttachment|gpu.TextureBinding)
		p.pre = postTex{tex: t, view: t.CreateView(), w: p.w, h: p.h}
	}
}

// taaSetup decides whether the pass runs this frame and with what history,
// and takes in the jitter the scene was drawn with. It runs before the
// frame's uniforms are written.
func (p *Post) taaSetup(cam postCamera) {
	t := &p.taa
	t.jit = [2]float32{}
	if t.asked {
		t.jit = t.next
	}
	t.asked = false
	t.on = p.Settings.TAA && p.Projection[0] != 0
	if !t.on {
		t.valid = false
		return
	}
	if t.hist[0].tex == nil {
		p.preTarget()
		for i := range t.hist {
			tx := p.device.CreateTexture2D(p.w, p.h, gpu.FormatRGBA16Float, gpu.TextureRenderAttachment|gpu.TextureBinding)
			t.hist[i] = postTex{tex: tx, view: tx.CreateView(), w: p.w, h: p.h}
		}
		l, lc := t.pl.BindGroupLayout(0), t.plCopy.BindGroupLayout(0)
		for k := range t.bg {
			t.bg[k] = p.device.CreateBindGroup(l,
				gpu.BindGroupEntry{Binding: 0, Sampler: p.linear},
				gpu.BindGroupEntry{Binding: 1, TextureView: p.pre.view},
				gpu.BindGroupEntry{Binding: 2, TextureView: t.hist[1-k].view},
				gpu.BindGroupEntry{Binding: 3, TextureView: p.depthView},
				gpu.BindGroupEntry{Binding: 4, Buffer: p.uni},
				gpu.BindGroupEntry{Binding: 5, Buffer: t.uni},
				gpu.BindGroupEntry{Binding: 6, TextureView: p.adapt[1-k].view})
			t.bgCopy[k] = p.device.CreateBindGroup(lc, gpu.BindGroupEntry{Binding: 0, TextureView: t.hist[k].view})
		}
		l.Release()
		lc.Release()
		t.valid = false
	}
	u := taaUniforms{Reproj: cam.reproj, M: core.V4(0, cam.travel*p.Projection[5]*float32(p.h)*0.5, taaBlend, taaHold),
		Ride: cam.ride, R: core.V4(max(p.RideRadius, 0), 0, 0, 0)}
	if t.valid && cam.ok {
		u.M.X = 1
	}
	p.queue.WriteBuffer(t.uni, 0, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u)))
}

// taaRender records the resolve of frame k (p.pre and the history into
// hist[k]) and, unless motion blur follows and reads hist[k] itself, its
// copy into p.lit.
func (p *Post) taaRender(enc *gpu.CommandEncoder, k int, blur bool) {
	t := &p.taa
	pass := func(view *gpu.TextureView, pl *gpu.RenderPipeline, bg *gpu.BindGroup) {
		rp := enc.BeginRenderPass(view, gpu.Color{})
		rp.SetPipeline(pl)
		rp.SetBindGroup(0, bg)
		rp.Draw(3, 1, 0, 0)
		rp.End()
	}
	pass(t.hist[k].view, t.pl, t.bg[k])
	if !blur {
		pass(p.lit.view, t.plCopy, t.bgCopy[k])
	}
	t.valid = true
}

func (p *Post) taaReleaseTargets() {
	t := &p.taa
	for k := range t.bg {
		for _, bg := range []**gpu.BindGroup{&t.bg[k], &t.bgCopy[k]} {
			if *bg != nil {
				(*bg).Release()
				*bg = nil
			}
		}
		t.hist[k].release()
	}
	t.valid = false
}

func (p *Post) taaRelease() {
	p.taaReleaseTargets()
	for _, pl := range []**gpu.RenderPipeline{&p.taa.pl, &p.taa.plCopy} {
		if *pl != nil {
			(*pl).Release()
			*pl = nil
		}
	}
	if p.taa.uni != nil {
		p.taa.uni.Release()
		p.taa.uni = nil
	}
}

// postTAAWGSL: the resolve. The output's alpha keeps log2(1 + distance) of
// the nearest surface round the pixel (taaSky for the sky), for the next
// frame's disocclusion test.
const postTAAWGSL = `
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var src : texture_2d<f32>;
@group(0) @binding(2) var hist : texture_2d<f32>;
@group(0) @binding(3) var depth : texture_depth_2d;
@group(0) @binding(4) var<uniform> U : PostU;
struct TaaU { reproj : mat4x4f, m : vec4f, ride : mat4x4f, r : vec4f };
@group(0) @binding(5) var<uniform> T : TaaU;
@group(0) @binding(6) var adapt : texture_2d<f32>;
` + postDepthWGSL + postExposureWGSL + `
const taaSky = 31.0;
// Exposed colour compressed into 0…1 (and back), so the blend and the
// clip see what the eye will, and a hot sample weighs no more than white.
fn compress(c : vec3f) -> vec3f { return c / (1.0 + luma(c)); }
fn expand(c : vec3f) -> vec3f { return c / max(1.0 - luma(c), 1e-3); }
fn ycocg(c : vec3f) -> vec3f {
    return vec3f(0.25 * c.r + 0.5 * c.g + 0.25 * c.b, 0.5 * c.r - 0.5 * c.b, -0.25 * c.r + 0.5 * c.g - 0.25 * c.b);
}
fn rgb(y : vec3f) -> vec3f { return vec3f(y.x + y.y - y.z, y.x + y.z, y.x - y.y - y.z); }
// The history through a Catmull-Rom filter in five bilinear taps (the
// corners' weights are small enough to leave out).
fn history(uv : vec2f, dims : vec2f) -> vec3f {
    let pos = uv * dims;
    let t1 = floor(pos - 0.5) + 0.5;
    let f = pos - t1;
    let w0 = f * (-0.5 + f * (1.0 - 0.5 * f));
    let w1 = 1.0 + f * f * (-2.5 + 1.5 * f);
    let w2 = f * (0.5 + f * (2.0 - 1.5 * f));
    let w3 = f * f * (-0.5 + 0.5 * f);
    let w12 = w1 + w2;
    let t0 = (t1 - 1.0) / dims;
    let t3 = (t1 + 2.0) / dims;
    let t12 = (t1 + w2 / w12) / dims;
    var c = textureSampleLevel(hist, samp, vec2f(t12.x, t0.y), 0.0).rgb * (w12.x * w0.y);
    c += textureSampleLevel(hist, samp, vec2f(t0.x, t12.y), 0.0).rgb * (w0.x * w12.y);
    c += textureSampleLevel(hist, samp, t12, 0.0).rgb * (w12.x * w12.y);
    c += textureSampleLevel(hist, samp, vec2f(t3.x, t12.y), 0.0).rgb * (w3.x * w12.y);
    c += textureSampleLevel(hist, samp, vec2f(t12.x, t3.y), 0.0).rgb * (w12.x * w3.y);
    let ws = w12.x * w0.y + w0.x * w12.y + w12.x * w12.y + w3.x * w12.y + w12.x * w3.y;
    return max(c / ws, vec3f(0.0));
}
// h pulled toward the box's centre until it lies inside the box.
fn clip_box(lo : vec3f, hi : vec3f, h : vec3f) -> vec3f {
    let c = 0.5 * (lo + hi);
    let ext = 0.5 * (hi - lo) + vec3f(1e-4);
    let v = h - c;
    let a = abs(v / ext);
    let m = max(a.x, max(a.y, a.z));
    if (m > 1.0) { return c + v / m; }
    return h;
}
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let c = vec2i(in.pos.xy);
    let dims = vec2f(textureDimensions(src));
    let cmax = vec2i(dims) - 1;
    let e = max(exposure(), 1e-4);
    let raw = max(textureLoad(src, c, 0).rgb, vec3f(0.0));
    // The 3×3 neighbourhood's colour statistics, and its nearest surface.
    var m1 = vec3f(0.0); var m2 = vec3f(0.0);
    var lo = vec3f(1e9); var hi = vec3f(-1e9);
    var dn = 2.0; var cn = c;
    for (var y = -1; y <= 1; y++) {
        for (var x = -1; x <= 1; x++) {
            let q = clamp(c + vec2i(x, y), vec2i(0), cmax);
            let s = ycocg(compress(max(textureLoad(src, q, 0).rgb, vec3f(0.0)) * e));
            m1 += s; m2 += s * s;
            lo = min(lo, s); hi = max(hi, s);
            let d = textureLoad(depth, q, 0);
            if (d < dn) { dn = d; cn = q; }
        }
    }
    let sky = dn >= 0.99999;
    var keep = taaSky;
    if (!sky) { keep = min(log2(1.0 + view_dist(dn)), 30.0); }
    if (T.m.x < 0.5) { return vec4f(raw, keep); }   // no history: a cut, the first frame
    // Where the nearest point was last frame (the sky: only the turn moves it).
    let un = (vec2f(cn) + 0.5) / dims;
    var pc : vec4f;
    if (sky) {
        pc = T.reproj * vec4f(view_pos(un, 1.0), 0.0);
    } else {
        let vd = view_dist(dn);
        var rp = T.reproj;
        if (vd < T.r.x) { rp = T.ride; }            // it rides with the eye: the turn alone
        pc = rp * vec4f(view_pos(un, vd), 1.0);
    }
    if (pc.w <= 1e-6) { return vec4f(raw, keep); }
    let puv = vec2f(pc.x / pc.w * 0.5 + 0.5, 0.5 - pc.y / pc.w * 0.5);
    let vel = unjitter(un) - puv;                   // the camera's motion here, in uv
    let huv = in.uv - vel;
    if (any(huv < vec2f(0.0)) || any(huv > vec2f(1.0))) { return vec4f(raw, keep); }
    // What the history kept there: the nearest distance, and how much the
    // pixel has been flickering (0…3).
    let ha = textureLoad(hist, vec2i(clamp(huv * dims, vec2f(0.0), dims - 1.0)), 0).a;
    let hf = clamp(floor(ha / 32.0), 0.0, 3.0);
    let hk = ha - hf * 32.0;
    var expect = taaSky;
    if (!sky) { expect = min(log2(1.0 + pc.w), 30.0); }
    // The nearest surface round the pixel changed. A thing thinner than a
    // pixel comes and goes from it with the jitter; a pixel where that
    // keeps happening is flickering, and keeps its history unclipped.
    let changed = abs(hk - expect) > 0.12;
    var f = hf;
    if (changed) {
        f = min(hf + 1.0, 3.0);
    } else if (ign(in.pos.xy + vec2f(U.p3.x * 3593.0, U.p3.x * 1931.0)) < 0.15) {
        f = max(hf - 1.0, 0.0);
    }
    let flick = select(0.0, 1.0, hf >= 2.0);
    let came = select(0.0, 1.0, expect < hk - 0.3);  // something nearer came in
    // A point the history did not see (it kept a different distance there,
    // and the eye's travel moves things by a pixel's fraction).
    let near = exp2(min(hk, expect)) - 1.0;
    let parallax = T.m.y / max(near, 1e-3);
    let fresh = smoothstep(0.15, 0.4, abs(hk - expect)) * smoothstep(0.25, 1.0, parallax);
    // The history, clipped to the colours round the pixel now, except
    // where it flickers: a needle missing from the whole neighbourhood in
    // some of the jitter's frames would be wiped from the history each time.
    let mu = m1 / 9.0;
    let sd = sqrt(max(m2 / 9.0 - mu * mu, vec3f(0.0)));
    let hraw = ycocg(compress(history(huv, dims) * e));
    let px = length(vel * dims);
    let hold = T.m.w * flick * (1.0 - came) * (1.0 - smoothstep(4.0, 16.0, px));
    let h = mix(clip_box(max(lo, mu - 1.25 * sd), min(hi, mu + 1.25 * sd), hraw), hraw, hold);
    let cur = ycocg(compress(raw * e));
    // More of the new frame while moving, where the resampling blurs.
    let a = max(mix(T.m.z, T.m.z * 2.0, clamp(px / 8.0, 0.0, 1.0)), fresh);
    let out = expand(clamp(rgb(mix(h, cur, a)), vec3f(0.0), vec3f(0.999))) / e;
    let alpha = keep + 32.0 * f;
    if (any(out != out) || any(out > vec3f(6e4))) { return vec4f(raw, alpha); }
    return vec4f(out, alpha);
}
`
