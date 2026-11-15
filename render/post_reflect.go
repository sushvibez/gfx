package render

import "github.com/sushvibez/gfx/gpu"

// Traced reflections (FrameSettings.TracedReflections): what the
// screen-space reflection cannot see. The screen-space ray (post_atmos.go)
// reflects the picture itself, with every material and light in it, but
// only what is on screen and not hidden; where it finds nothing, this pass
// has the answer from the trace scene: the shape the mirrored ray meets,
// in its plain colour, lit by the sun (its own shadow ray) and the sky.
//
// It runs at half resolution, and only on pixels the material marked as
// mirrors (the HDR target's alpha), so a dry scene pays almost nothing. It
// reads the atmosphere's uniforms, and so is drawn inside the atmosphere's
// render, after they are written and before the combine that uses it.

const postReflectWGSL = traceWGSL + `
@group(0) @binding(0) var depth : texture_depth_2d;
@group(0) @binding(1) var scene : texture_2d<f32>;
@group(0) @binding(2) var<uniform> A : AtmU;
` + postAtmosCommonWGSL + `
fn refl_view_pos(t : vec2i, dims : vec2f) -> vec3f {
    return view_ray((vec2f(t) + 0.5) / dims) * view_dist(textureLoad(depth, t, 0));
}
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let dims = vec2f(textureDimensions(depth));
    let dmax = vec2i(dims) - 1;
    let t = min(vec2i(in.pos.xy) * 2, dmax);
    let mark = textureLoad(scene, t, 0).a;
    if (abs(mark) >= 0.995 || textureLoad(depth, t, 0) >= 0.99999) {
        return vec4f(0.0); // not a mirror, or the sky
    }
    // The surface and its normal in view space, as the screen-space ray
    // finds them, and the same waves on water.
    let p = refl_view_pos(t, dims);
    let px0 = refl_view_pos(clamp(t - vec2i(1, 0), vec2i(0), dmax), dims);
    let px1 = refl_view_pos(clamp(t + vec2i(1, 0), vec2i(0), dmax), dims);
    let py0 = refl_view_pos(clamp(t - vec2i(0, 1), vec2i(0), dmax), dims);
    let py1 = refl_view_pos(clamp(t + vec2i(0, 1), vec2i(0), dmax), dims);
    var dx = px1 - p;
    if (abs(px0.z - p.z) < abs(px1.z - p.z)) { dx = p - px0; }
    var dy = py1 - p;
    if (abs(py0.z - p.z) < abs(py1.z - p.z)) { dy = p - py0; }
    var n = normalize(cross(dy, dx));
    let v = normalize(p);
    if (dot(n, v) > 0.0) { n = -n; }
    let rot = mat3x3f(A.inv_view[0].xyz, A.inv_view[1].xyz, A.inv_view[2].xyz);
    let world = A.cam.xyz + rot * p;
    if (mark < 0.0) {
        let w = world.xz;
        let tm = A.rain.y;
        var g = vec2f(0.0);
        g += vec2f(0.8, 0.6) * 0.045 * cos(dot(w, vec2f(0.8, 0.6)) * 1.7 + tm * 1.3);
        g += vec2f(-0.5, 0.87) * 0.040 * cos(dot(w, vec2f(-0.5, 0.87)) * 3.9 - tm * 1.9);
        g += vec2f(0.3, -0.95) * 0.030 * cos(dot(w, vec2f(0.3, -0.95)) * 8.3 + tm * 2.7);
        g += vec2f(-0.9, -0.44) * 0.022 * cos(dot(w, vec2f(-0.9, -0.44)) * 17.0 - tm * 3.6);
        g = g / (1.0 + 0.015 * length(p));
        n = normalize(n + transpose(rot) * vec3f(g.x, 0.0, g.y));
    }
    let rd = normalize(rot * reflect(v, n));
    let nw = normalize(rot * n);
    let start = world + nw * 0.05;
    let h = trace_nearest(start, rd, 0.01, 400.0);
    if (h.t <= 0.0) {
        return vec4f(0.0); // the sky: the material's own reflection stands
    }
    let hl = max(dot(h.n, A.sun_dir.xyz), 0.0);
    var vis = 0.0;
    if (hl > 0.0) {
        vis = select(1.0, 0.0, trace_any(start + rd * h.t + h.n * 0.03, A.sun_dir.xyz, 0.01, 2000.0));
    }
    // What stands in the sky's place in that direction.
    return vec4f(h.color * (A.sun.rgb * hl * vis * 0.3183099 + A.amb.rgb), 1.0);
}
`

func (p *Post) reflectPipelines(mk func(label, body, entry string, format gpu.TextureFormat) *gpu.RenderPipeline) {
	p.plReflect = mk("post-reflect", postReflectWGSL, "fs", gpu.FormatRGBA16Float)
}

// reflectTargets allocates the half-resolution reflection target (Resize
// calls it, before the atmosphere's binds are rebuilt: the combine reads it).
func (p *Post) reflectTargets(tex func(w, h int, f gpu.TextureFormat) postTex, w, h int) {
	p.refl = tex(max(w/2, 1), max(h/2, 1), gpu.FormatRGBA16Float)
	p.reflDirty = true
}

// reflectRender traces the mirrors' reflections; atmosRender calls it once
// the atmosphere's uniforms are written. It reports whether it drew.
func (p *Post) reflectRender(enc *gpu.CommandEncoder) bool {
	if !p.reflectTraced || p.traceNodes == nil || p.plReflect == nil {
		return false
	}
	if p.reflDirty {
		p.reflDirty = false
		if p.bgReflect != nil {
			p.bgReflect.Release()
		}
		l := p.plReflect.BindGroupLayout(0)
		p.bgReflect = p.device.CreateBindGroup(l,
			gpu.BindGroupEntry{Binding: 0, TextureView: p.depthView},
			gpu.BindGroupEntry{Binding: 1, TextureView: p.sceneView},
			gpu.BindGroupEntry{Binding: 2, Buffer: p.atmosUni},
			gpu.BindGroupEntry{Binding: 10, Buffer: p.traceNodes},
			gpu.BindGroupEntry{Binding: 11, Buffer: p.traceInsts},
		)
		l.Release()
	}
	rp := enc.BeginRenderPass(p.refl.view, gpu.Color{})
	rp.SetPipeline(p.plReflect)
	rp.SetBindGroup(0, p.bgReflect)
	rp.Draw(3, 1, 0, 0)
	rp.End()
	return true
}

func (p *Post) reflectReleaseTargets() {
	if p.bgReflect != nil {
		p.bgReflect.Release()
		p.bgReflect = nil
	}
	p.refl.release()
}

func (p *Post) reflectRelease() {
	if p.plReflect != nil {
		p.plReflect.Release()
		p.plReflect = nil
	}
}
