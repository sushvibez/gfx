package render

import (
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// The temporal filter: PostSettings.Temporal averages each frame's lit
// picture with the ones before it, so terms sampled with a few rays or
// taps a pixel (traced occlusion, a soft sun's jittered ray, the haze)
// lose their grain. Each pixel finds where its surface was last frame from
// the depth buffer and the two cameras, takes the history there, clamps it
// to the range of colours round the pixel now (so what has changed, or was
// hidden, does not leave a ghost), and blends. It runs after the
// atmosphere and before everything that reads the picture.
//
// It averages only what changes: the renderer must be told to vary its
// noise from frame to frame (FrameSettings.Temporal), or there is nothing
// to converge.

// temporalUniforms mirrors the WGSL TempU block.
type temporalUniforms struct {
	Reproj core.Mat4 // this frame's clip space to last frame's
	P      core.Vec4 // x history weight, y 1 when there is a history
}

const postTemporalWGSL = `
struct TempU { reproj : mat4x4f, p : vec4f }
@group(0) @binding(0) var samp : sampler;
@group(0) @binding(1) var cur : texture_2d<f32>;
@group(0) @binding(2) var hist : texture_2d<f32>;
@group(0) @binding(3) var depth : texture_depth_2d;
@group(0) @binding(4) var<uniform> T : TempU;
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let px = vec2i(in.pos.xy);
    let dmax = vec2i(textureDimensions(cur)) - 1;
    let c = textureLoad(cur, px, 0).rgb;
    if (T.p.y < 0.5) {
        return vec4f(c, 1.0);
    }
    // The colours round this pixel now: what the history may be.
    var lo = c;
    var hi = c;
    for (var y = -1; y <= 1; y++) {
        for (var x = -1; x <= 1; x++) {
            let n = textureLoad(cur, clamp(px + vec2i(x, y), vec2i(0), dmax), 0).rgb;
            lo = min(lo, n);
            hi = max(hi, n);
        }
    }
    // Where this surface was on screen last frame.
    let d = textureLoad(depth, px, 0);
    let prev = T.reproj * vec4f(in.uv.x * 2.0 - 1.0, 1.0 - in.uv.y * 2.0, d, 1.0);
    let puv = vec2f(prev.x, -prev.y) / prev.w * 0.5 + 0.5;
    if (prev.w <= 0.0 || any(puv < vec2f(0.0)) || any(puv > vec2f(1.0))) {
        return vec4f(c, 1.0);
    }
    let h = clamp(textureSampleLevel(hist, samp, puv, 0.0).rgb, lo, hi);
    return vec4f(mix(c, h, T.p.x), 1.0);
}
`

const postCopyWGSL = `
@group(0) @binding(0) var src : texture_2d<f32>;
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    return textureLoad(src, vec2i(in.pos.xy), 0);
}
`

func (p *Post) temporalPipelines(mk func(label, body, entry string, format gpu.TextureFormat) *gpu.RenderPipeline) {
	p.plTemporal = mk("post-temporal", postTemporalWGSL, "fs", gpu.FormatRGBA16Float)
	p.plCopy = mk("post-copy", postCopyWGSL, "fs", gpu.FormatRGBA16Float)
}

// temporalTargets allocates the two history targets (Resize calls it): a
// new size has no history.
func (p *Post) temporalTargets(tex func(w, h int, f gpu.TextureFormat) postTex, w, h int) {
	p.hist[0], p.hist[1] = tex(w, h, gpu.FormatRGBA16Float), tex(w, h, gpu.FormatRGBA16Float)
	if p.temporalUni == nil {
		p.temporalUni = p.device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, int(unsafe.Sizeof(temporalUniforms{})))
	}
	p.histValid = false
	p.temporalDirty = true
}

func (p *Post) temporalBind() {
	if !p.temporalDirty {
		return
	}
	p.temporalDirty = false
	p.temporalReleaseBinds()
	group := func(pl *gpu.RenderPipeline, entries ...gpu.BindGroupEntry) *gpu.BindGroup {
		l := pl.BindGroupLayout(0)
		defer l.Release()
		return p.device.CreateBindGroup(l, entries...)
	}
	for k := range p.bgTemporal {
		// Frame k reads hist[1-k] and writes hist[k], then copies it back
		// over the lit picture for the passes after.
		p.bgTemporal[k] = group(p.plTemporal,
			gpu.BindGroupEntry{Binding: 0, Sampler: p.linear},
			gpu.BindGroupEntry{Binding: 1, TextureView: p.lit.view},
			gpu.BindGroupEntry{Binding: 2, TextureView: p.hist[1-k].view},
			gpu.BindGroupEntry{Binding: 3, TextureView: p.depthView},
			gpu.BindGroupEntry{Binding: 4, Buffer: p.temporalUni},
		)
		p.bgCopy[k] = group(p.plCopy, gpu.BindGroupEntry{Binding: 0, TextureView: p.hist[k].view})
	}
}

// temporalRender filters p.lit in place (through the history targets).
func (p *Post) temporalRender(enc *gpu.CommandEncoder) {
	weight := min(max(p.Settings.Temporal, 0), 0.97)
	vp := p.Projection.Mul(p.View)
	inv, ok := vp.Inverse()
	if weight <= 0 || !ok || p.Projection[0] == 0 || p.plTemporal == nil {
		p.histValid = false
		return
	}
	p.temporalBind()
	// A young history is a plain running mean: the n-th frame counts for
	// 1/n, so the grain falls as fast as it can from the first frame on
	// (the full weight from the start would hold the first, noisiest frame
	// for twenty more).
	if !p.histValid {
		p.histAge = 0
	}
	weight = min(weight, float32(p.histAge)/float32(p.histAge+1))
	p.histAge++
	u := temporalUniforms{Reproj: p.prevVP.Mul(inv), P: core.V4(weight, 0, 0, 0)}
	if p.histValid {
		u.P.Y = 1
	}
	p.queue.WriteBuffer(p.temporalUni, 0, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u)))
	k := p.tframe & 1
	pass := func(view *gpu.TextureView, pl *gpu.RenderPipeline, bg *gpu.BindGroup) {
		rp := enc.BeginRenderPass(view, gpu.Color{})
		rp.SetPipeline(pl)
		rp.SetBindGroup(0, bg)
		rp.Draw(3, 1, 0, 0)
		rp.End()
	}
	pass(p.hist[k].view, p.plTemporal, p.bgTemporal[k])
	pass(p.lit.view, p.plCopy, p.bgCopy[k])
	p.prevVP, p.histValid = vp, true
	p.tframe++
}

func (p *Post) temporalReleaseBinds() {
	for k := range p.bgTemporal {
		if p.bgTemporal[k] != nil {
			p.bgTemporal[k].Release()
			p.bgTemporal[k] = nil
		}
		if p.bgCopy[k] != nil {
			p.bgCopy[k].Release()
			p.bgCopy[k] = nil
		}
	}
}

func (p *Post) temporalReleaseTargets() {
	p.temporalReleaseBinds()
	p.hist[0].release()
	p.hist[1].release()
}

func (p *Post) temporalRelease() {
	for _, pl := range []**gpu.RenderPipeline{&p.plTemporal, &p.plCopy} {
		if *pl != nil {
			(*pl).Release()
			*pl = nil
		}
	}
	if p.temporalUni != nil {
		p.temporalUni.Release()
		p.temporalUni = nil
	}
}
