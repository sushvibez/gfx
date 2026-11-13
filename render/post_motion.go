package render

// Camera motion blur, for Post. Each pixel's point is rebuilt from the
// depth buffer (perspective or logarithmic, as the other depth passes do)
// and projected with the previous frame's camera; the difference is how
// far that point moved across the screen during the frame. A shutter open
// for a fixed time (1/190 s at strength 1, and never more than half the
// frame) sees a fraction of that motion, and the pixel is averaged along
// it in nine jittered taps. Only the camera's motion is seen: things that
// move by themselves are not blurred.
//
// The pass runs on the HDR image after the atmosphere and before the
// bloom, so a lamp streaks with its glow. It costs nothing while the
// camera is still (the pass is skipped) and nothing for a pixel that moves
// less than a pixel's fraction (an early return).
//
// The previous camera is Post.View and Post.Projection as they were at the
// last Render. A renderer that re-centres its world on the camera every
// frame gives the previous view in this frame's world with
// UsePreviousView. A cut (ResetMotion, the first frame, a turn of more
// than about 30° or a change of zoom in one frame) is not blurred, and
// neither is a pixel whose point jumped more than a quarter of the screen.

import (
	"math"
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// motionUniforms mirrors the WGSL MotionU struct.
type motionUniforms struct {
	Reproj core.Mat4 // this frame's view space to the previous frame's clip space
	M      core.Vec4 // x shutter (fraction of the frame's motion), y longest blur (uv), z cut (uv per frame)
}

// Motion blur's constants: the shutter time at strength 1, the longest
// streak and the per-frame jump that reads as a cut, both in uv (a
// fraction of the screen), and the turn in one frame that reads as a cut.
const (
	motionShutter = 1.0 / 190
	motionMaxLen  = 0.018
	motionCut     = 0.25
	motionCutCos  = 0.866 // cos 30°
)

type postMotion struct {
	prevView, prevProj core.Mat4
	have               bool // prevView and prevProj hold the last frame's camera
	override           core.Mat4
	hasOverride        bool

	uni   *gpu.Buffer
	pl    *gpu.RenderPipeline
	bgPre *gpu.BindGroup    // the blur from p.pre
	bgTAA [2]*gpu.BindGroup // the blur from the anti-aliased image (p.taa.hist[k])
}

// postCamera is what Post knows this frame of the previous one's camera.
type postCamera struct {
	reproj core.Mat4 // this frame's view space to the previous frame's clip space (x, y and w rows)
	moved  bool      // the camera moved, turned or zoomed since
	ok     bool      // reproj holds: the previous camera is known and this is not a cut
	travel float32   // how far the eye moved, in world units
}

// UsePreviousView gives the previous frame's view matrix in this frame's
// world, for the next Render's motion blur and temporal anti-aliasing:
// for a renderer that moves its world origin with the camera
// (camera-relative rendering), where the view remembered from the last
// frame is in a different world. Without it Post uses the View it was
// given at the last Render.
func (p *Post) UsePreviousView(view core.Mat4) {
	p.motion.override, p.motion.hasOverride = view, true
}

// ResetMotion forgets the previous camera, so the next frame is not
// blurred and temporal anti-aliasing starts its history afresh: call it
// on a cut or a teleport that the automatic checks (a turn of more than
// 30° in one frame, a change of zoom, points jumping a quarter of the
// screen) would not catch.
func (p *Post) ResetMotion() {
	p.motion.have, p.motion.hasOverride = false, false
}

// motionPipelines builds the pass (NewPost calls it through mk).
func (p *Post) motionPipelines(mk func(label, body, entry string, format gpu.TextureFormat) *gpu.RenderPipeline) {
	p.motion.pl = mk("post-motion-blur", postMotionWGSL, "fs", gpu.FormatRGBA16Float)
	p.motion.uni = p.device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, int(unsafe.Sizeof(motionUniforms{})))
}

// camera works out how this frame's camera relates to the last one's.
func (p *Post) camera() postCamera {
	m := &p.motion
	prevView := m.prevView
	if m.hasOverride {
		prevView = m.override
	}
	var c postCamera
	if !m.have || p.Projection[0] == 0 || m.prevProj[0] == 0 {
		return c
	}
	inv, ok := p.View.Inverse()
	if !ok {
		return c
	}
	// A cut: the camera turned too far, or zoomed, in one frame.
	fwd := func(v core.Mat4) core.Vec3 { return core.V3(v[2], v[6], v[10]).Normalize() }
	if fwd(prevView).Dot(fwd(p.View)) < motionCutCos ||
		math.Abs(float64(m.prevProj[5]/p.Projection[5])-1) > 0.1 {
		return c
	}
	pinv, ok := prevView.Inverse()
	if !ok {
		return c
	}
	// The previous projection's x, y and w rows only (w = -z): the depth
	// terms are not needed, and a log-depth renderer may not supply them.
	var pp core.Mat4
	pp[0], pp[5], pp[8], pp[9], pp[11] = m.prevProj[0], m.prevProj[5], m.prevProj[8], m.prevProj[9], -1
	c.reproj = pp.Mul(prevView).Mul(inv)
	c.moved = prevView != p.View || m.prevProj != p.Projection
	c.travel = core.V3(inv[12]-pinv[12], inv[13]-pinv[13], inv[14]-pinv[14]).Length()
	c.ok = true
	return c
}

// motionSetup decides whether this frame is blurred and, if so, writes the
// uniforms.
func (p *Post) motionSetup(cam postCamera) bool {
	if p.Settings.MotionBlur <= 0 || !cam.ok || !cam.moved {
		return false
	}
	dt := p.DeltaTime
	if dt <= 0 {
		dt = 1.0 / 60
	}
	u := motionUniforms{
		Reproj: cam.reproj,
		M:      core.V4(min(0.5, p.Settings.MotionBlur*motionShutter/dt), motionMaxLen, motionCut, 0),
	}
	p.queue.WriteBuffer(p.motion.uni, 0, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u)))
	return true
}

// motionRender records the blur from src (p.pre, or the anti-aliased
// p.taa.hist[k]) into p.lit.
func (p *Post) motionRender(enc *gpu.CommandEncoder, src *gpu.TextureView) {
	m := &p.motion
	group := func(bg **gpu.BindGroup) *gpu.BindGroup {
		if *bg == nil {
			l := m.pl.BindGroupLayout(0)
			*bg = p.device.CreateBindGroup(l,
				gpu.BindGroupEntry{Binding: 0, Sampler: p.linear},
				gpu.BindGroupEntry{Binding: 1, TextureView: src},
				gpu.BindGroupEntry{Binding: 2, TextureView: p.depthView},
				gpu.BindGroupEntry{Binding: 3, Buffer: p.uni},
				gpu.BindGroupEntry{Binding: 4, Buffer: m.uni})
			l.Release()
		}
		return *bg
	}
	var bg *gpu.BindGroup
	switch src {
	case p.pre.view:
		bg = group(&m.bgPre)
	case p.taa.hist[0].view:
		bg = group(&m.bgTAA[0])
	default:
		bg = group(&m.bgTAA[1])
	}
	rp := enc.BeginRenderPass(p.lit.view, gpu.Color{})
	rp.SetPipeline(m.pl)
	rp.SetBindGroup(0, bg)
	rp.Draw(3, 1, 0, 0)
	rp.End()
}

// motionRemember keeps this frame's camera for the next one's blur.
func (p *Post) motionRemember() {
	m := &p.motion
	m.prevView, m.prevProj = p.View, p.Projection
	m.have = p.Projection[0] != 0 && p.View != (core.Mat4{})
	m.hasOverride = false
}

func (p *Post) motionReleaseTargets() {
	for _, bg := range []**gpu.BindGroup{&p.motion.bgPre, &p.motion.bgTAA[0], &p.motion.bgTAA[1]} {
		if *bg != nil {
			(*bg).Release()
			*bg = nil
		}
	}
}

func (p *Post) motionRelease() {
	p.motionReleaseTargets()
	if p.motion.pl != nil {
		p.motion.pl.Release()
		p.motion.pl = nil
	}
	if p.motion.uni != nil {
		p.motion.uni.Release()
		p.motion.uni = nil
	}
}

// postMotionWGSL: the blur. The point under the pixel, rebuilt from depth
// in view space, goes through Reproj to where the previous camera saw it;
// the pixel is averaged along a centred stretch of that motion, sky taps
// left out so the sky does not smear into what stands in front of it.
const postMotionWGSL = `
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var src : texture_2d<f32>;
@group(0) @binding(2) var depth : texture_depth_2d;
@group(0) @binding(3) var<uniform> U : PostU;
struct MotionU { reproj : mat4x4f, m : vec4f };
@group(0) @binding(4) var<uniform> M : MotionU;
` + postDepthWGSL + `
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let c = vec2i(in.pos.xy);
    let col0 = textureLoad(src, c, 0).rgb;
    let d = textureLoad(depth, c, 0);
    if (d >= 0.99999) { return vec4f(col0, 1.0); }   // the sky
    let dims = vec2f(textureDimensions(depth));
    let uv = in.uv;
    let pc = M.reproj * vec4f(view_pos(uv, view_dist(d)), 1.0);
    if (pc.w <= 1e-4) { return vec4f(col0, 1.0); }    // behind the previous camera
    let puv = vec2f(pc.x / pc.w * 0.5 + 0.5, 0.5 - pc.y / pc.w * 0.5);
    let mv = unjitter(uv) - puv;                      // this frame's motion, in uv (not the jitter's)
    // A jump of a quarter of the screen in one frame is a cut, not motion.
    var vel = mv * M.m.x * (1.0 - smoothstep(M.m.z * 0.6, M.m.z, length(mv)));
    let len = length(vel);
    if (len * dims.y < 0.75) { return vec4f(col0, 1.0); }
    vel *= min(1.0, M.m.y / len);
    let j = ign(in.pos.xy) - 0.5;
    let dmax = vec2i(dims) - 1;
    var acc = col0; var ws = 1.0;
    for (var i = 0; i < 9; i++) {
        let tuv = uv + vel * ((f32(i) + 0.5 + j) / 9.0 - 0.5);
        let td = textureLoad(depth, clamp(vec2i(tuv * dims), vec2i(0), dmax), 0);
        let w = select(1.0, 0.0, td >= 0.99999);
        acc += textureSampleLevel(src, samp, tuv, 0.0).rgb * w; ws += w;
    }
    return vec4f(acc / ws, 1.0);
}
`
