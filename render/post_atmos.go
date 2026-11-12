package render

// The air and the sky, for Post. Between the 3D pass and the camera:
//
//	shafts  — at half resolution, each pixel's view ray is marched from
//	          the eye to the surface it ends on; at every step the sun's
//	          shadow map says whether that bit of air is lit, and lit air
//	          scatters sunlight toward the eye (single scattering, a
//	          two-lobe Henyey-Greenstein phase, density thinning with
//	          height). Unlit air still scatters the sky's ambient. The
//	          result is in-scattered light and what is left of the
//	          surface's own (transmittance).
//	sky     — where the ray ends on nothing: a zenith-to-horizon gradient,
//	          a warm band toward the sun, the sun's glow and its disc.
//	combine — scene × transmittance + in-scatter, or the sky, into the HDR
//	          image the bloom and the tone curve then see.
//
// The sun's shadow map and matrix come from the Renderer3D (UseShadow;
// View3D does it), the camera from Post.View and Post.Projection.

import (
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// Atmosphere is the air and the sky. The zero value is off: the scene
// passes through untouched and empty pixels keep the clear colour.
type Atmosphere struct {
	// Sky draws the sky where nothing was rendered. Zenith and Horizon are
	// its colours (linear radiance) overhead and at the horizon.
	Sky             bool
	Zenith, Horizon [3]float32
	// Shafts is the air's density: the scattering coefficient per world
	// unit at Height 0 (0 = no volumetric light; 0.01 is a clear evening
	// haze, 0.03 thick). Falloff is the height over which the air thins
	// to about a third (0 = 6). MaxDistance is how far the march goes
	// (0 = 140); beyond it the haze continues analytically into the
	// horizon colour.
	Shafts, Falloff, MaxDistance float32
	// Lamps is how strongly the Renderer3D's point and spot lights light
	// the air (0 = not at all; 1 = as they light surfaces): a spot lamp
	// then shows its cone as a beam in the haze. It needs Shafts > 0 (the
	// air must have something in it) and a renderer (UseShadow). Lamps
	// cast no shadows, so a beam passes through what stands in it.
	Lamps float32
	// SunDir is the direction the sunlight travels, Sun its radiance and
	// Ambient the sky's fill light: normally the Renderer3D's own
	// (FrameSettings.Sun and Ambient, and Begin's light direction).
	SunDir  core.Vec3
	Sun     [3]float32
	Ambient [3]float32
}

// atmosUniforms mirrors the WGSL AtmU struct.
type atmosUniforms struct {
	InvView core.Mat4 // the camera's world matrix
	LightVP core.Mat4
	Cam     core.Vec4 // xyz eye
	SunDir  core.Vec4 // xyz toward the sun, w 1 when there is a shadow map
	Sun     core.Vec4 // rgb radiance, w density (0 = no shafts)
	Amb     core.Vec4 // rgb, w height falloff
	Zenith  core.Vec4 // rgb, w 1 when the sky is drawn
	Horizon core.Vec4 // rgb, w max march distance
	Proj    core.Vec4 // projection x and y scales, depth terms
	Lamps   core.Vec4 // x light count, y how strongly lamps light the air, z 1 when scene reflections are traced
}

// UseShadow gives Post the sun's shadow map and matrix and the frame's
// lights from a Renderer3D (as of its last Begin), for the light shafts
// and the lamps' beams. View3D calls it.
func (p *Post) UseShadow(r *Renderer3D) {
	if r == nil {
		if p.shadowView != nil || p.lightBuf != nil {
			p.atmosDirty = true
		}
		p.shadowView, p.lightBuf, p.lightCount, p.reflect = nil, nil, 0, false
		return
	}
	if p.shadowView != r.shadowView || p.lightBuf != r.lightBuf {
		p.shadowView, p.lightBuf = r.shadowView, r.lightBuf
		p.atmosDirty = true
	}
	p.lightVP = r.lastLightVP
	p.lightCount = r.lastLightCount
	p.reflect = r.settings.SceneReflection > 0 && r.settings.SkyReflection > 0
}

// atmosInit builds the atmosphere's pipelines (NewPost calls it through mk).
func (p *Post) atmosPipelines(mk func(label, body, entry string, format gpu.TextureFormat) *gpu.RenderPipeline) {
	hdr := gpu.FormatRGBA16Float
	p.plVol = mk("post-shafts", postVolWGSL, "fs", hdr)
	p.plVolBlurH = mk("post-shafts-blur-h", postVolBlurWGSL, "fs_h", hdr)
	p.plVolBlurV = mk("post-shafts-blur-v", postVolBlurWGSL, "fs_v", hdr)
	p.plAtmos = mk("post-atmosphere", postAtmosWGSL, "fs", hdr)
}

// atmosTargets allocates the atmosphere's targets (Resize calls it).
func (p *Post) atmosTargets(tex func(w, h int, f gpu.TextureFormat) postTex, w, h int) {
	hdr := gpu.FormatRGBA16Float
	hw, hh := max(w/2, 1), max(h/2, 1)
	p.vol[0], p.vol[1] = tex(hw, hh, hdr), tex(hw, hh, hdr)
	p.lit = tex(w, h, hdr)
	if p.noShadow.tex == nil {
		// A stand-in for the shadow map when no renderer supplied one.
		t := p.device.CreateTexture2D(1, 1, gpu.FormatDepth32Float, gpu.TextureRenderAttachment|gpu.TextureBinding)
		p.noShadow = postTex{tex: t, view: t.CreateView(), w: 1, h: 1}
	}
	if p.noLights == nil {
		// A stand-in for the renderer's light list.
		p.noLights = p.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, int(unsafe.Sizeof(lightBlock{})))
	}
	if p.atmosUni == nil {
		p.atmosUni = p.device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, int(unsafe.Sizeof(atmosUniforms{})))
	}
	p.atmosDirty = true
}

// atmosBind (re)builds the atmosphere's bind groups: after a resize, or
// when the shadow map changed.
func (p *Post) atmosBind() {
	if !p.atmosDirty || p.lit.view == nil {
		return
	}
	p.atmosDirty = false
	p.atmosReleaseBinds()
	d := p.device
	group := func(pl *gpu.RenderPipeline, entries ...gpu.BindGroupEntry) *gpu.BindGroup {
		l := pl.BindGroupLayout(0)
		defer l.Release()
		return d.CreateBindGroup(l, entries...)
	}
	tex := func(b int, v *gpu.TextureView) gpu.BindGroupEntry {
		return gpu.BindGroupEntry{Binding: b, TextureView: v}
	}
	uni := func(b int) gpu.BindGroupEntry { return gpu.BindGroupEntry{Binding: b, Buffer: p.atmosUni} }
	shadow := p.shadowView
	if shadow == nil {
		shadow = p.noShadow.view
	}
	lights := p.lightBuf
	if lights == nil {
		lights = p.noLights
	}
	p.bgVol = group(p.plVol, tex(0, p.depthView), tex(1, shadow), uni(2), gpu.BindGroupEntry{Binding: 3, Buffer: lights})
	p.bgVolBlur[0] = group(p.plVolBlurH, tex(0, p.depthView), tex(1, p.vol[0].view), uni(2))
	p.bgVolBlur[1] = group(p.plVolBlurV, tex(0, p.depthView), tex(1, p.vol[1].view), uni(2))
	p.bgAtmos = group(p.plAtmos, tex(0, p.depthView), tex(1, p.sceneView), tex(2, p.vol[0].view), uni(3))
}

// atmosRender records the shafts and the combine pass; the result is
// p.lit, which everything after reads.
func (p *Post) atmosRender(enc *gpu.CommandEncoder) {
	p.atmosBind()
	a := p.Atmosphere
	inv, ok := p.View.Inverse()
	shafts := a.Shafts > 0 && ok && p.Projection[0] != 0
	sky := a.Sky && ok && p.Projection[0] != 0
	u := atmosUniforms{InvView: inv, LightVP: p.lightVP}
	u.Cam = core.V4(inv[12], inv[13], inv[14], 1)
	sd := a.SunDir.Normalize().Scale(-1)
	hasShadow := float32(0)
	if p.shadowView != nil {
		hasShadow = 1
	}
	u.SunDir = core.V4(sd.X, sd.Y, sd.Z, hasShadow)
	density := float32(0)
	if shafts {
		density = a.Shafts
	}
	u.Sun = core.V4(a.Sun[0], a.Sun[1], a.Sun[2], density)
	falloff := a.Falloff
	if falloff <= 0 {
		falloff = 6
	}
	u.Amb = core.V4(a.Ambient[0], a.Ambient[1], a.Ambient[2], falloff)
	skyOn := float32(0)
	if sky {
		skyOn = 1
	}
	u.Zenith = core.V4(a.Zenith[0], a.Zenith[1], a.Zenith[2], skyOn)
	maxD := a.MaxDistance
	if maxD <= 0 {
		maxD = 140
	}
	u.Horizon = core.V4(a.Horizon[0], a.Horizon[1], a.Horizon[2], maxD)
	u.Proj = core.V4(p.Projection[0], p.Projection[5], p.Projection[10], p.Projection[14])
	if p.lightBuf != nil && a.Lamps > 0 {
		u.Lamps = core.V4(float32(p.lightCount), a.Lamps, 0, 0)
	}
	if p.reflect && ok && p.Projection[0] != 0 {
		u.Lamps.Z = 1
	}
	p.queue.WriteBuffer(p.atmosUni, 0, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u)))

	pass := func(view *gpu.TextureView, pl *gpu.RenderPipeline, bg *gpu.BindGroup, clear gpu.Color) {
		rp := enc.BeginRenderPass(view, clear)
		rp.SetPipeline(pl)
		rp.SetBindGroup(0, bg)
		rp.Draw(3, 1, 0, 0)
		rp.End()
	}
	clear := gpu.Color{A: 1} // no in-scatter, full transmittance
	if shafts {
		pass(p.vol[0].view, p.plVol, p.bgVol, clear)
		pass(p.vol[1].view, p.plVolBlurH, p.bgVolBlur[0], clear)
		pass(p.vol[0].view, p.plVolBlurV, p.bgVolBlur[1], clear)
	}
	pass(p.lit.view, p.plAtmos, p.bgAtmos, gpu.Color{})
}

func (p *Post) atmosReleaseBinds() {
	for _, g := range []**gpu.BindGroup{&p.bgVol, &p.bgVolBlur[0], &p.bgVolBlur[1], &p.bgAtmos} {
		if *g != nil {
			(*g).Release()
			*g = nil
		}
	}
}

func (p *Post) atmosReleaseTargets() {
	p.atmosReleaseBinds()
	p.vol[0].release()
	p.vol[1].release()
	p.lit.release()
}

func (p *Post) atmosRelease() {
	p.atmosReleaseTargets()
	p.noShadow.release()
	for _, pl := range []**gpu.RenderPipeline{&p.plVol, &p.plVolBlurH, &p.plVolBlurV, &p.plAtmos} {
		if *pl != nil {
			(*pl).Release()
			*pl = nil
		}
	}
	if p.atmosUni != nil {
		p.atmosUni.Release()
		p.atmosUni = nil
	}
	if p.noLights != nil {
		p.noLights.Release()
		p.noLights = nil
	}
}

// ---- shaders ---------------------------------------------------------------

// postAtmosCommonWGSL: the atmosphere's uniforms, view rays from depth,
// and the sky.
const postAtmosCommonWGSL = `
struct AtmU {
    inv_view : mat4x4f,
    light_vp : mat4x4f,
    cam : vec4f,
    sun_dir : vec4f,   // xyz toward the sun, w 1 with a shadow map
    sun : vec4f,       // rgb radiance, w density (0 = no shafts)
    amb : vec4f,       // rgb ambient, w height falloff
    zenith : vec4f,    // rgb, w 1 when the sky is drawn
    horizon : vec4f,   // rgb, w max march distance
    proj : vec4f,      // projection x, y scales; depth terms [10], [14]
    lamps : vec4f,     // x light count, y how strongly lamps light the air, z 1 = trace reflections
};
fn view_dist(d : f32) -> f32 { return A.proj.w / (d + A.proj.z); }
// The view-space ray through a uv (not normalised: z is -1).
fn view_ray(uv : vec2f) -> vec3f {
    return vec3f((uv.x * 2.0 - 1.0) / A.proj.x, (1.0 - uv.y * 2.0) / A.proj.y, -1.0);
}
fn world_dir(v : vec3f) -> vec3f {
    return normalize(mat3x3f(A.inv_view[0].xyz, A.inv_view[1].xyz, A.inv_view[2].xyz) * v);
}
fn ign(p : vec2f) -> f32 { return fract(52.9829189 * fract(dot(p, vec2f(0.06711056, 0.00583715)))); }
// Henyey-Greenstein phase function.
fn hg(mu : f32, g : f32) -> f32 {
    let g2 = g * g;
    return (1.0 - g2) / (12.566371 * pow(max(1.0 + g2 - 2.0 * g * mu, 1e-4), 1.5));
}
// The horizon's colour in a direction: warmer and brighter toward the sun.
fn horizon_color(rd : vec3f) -> vec3f {
    let toward = max(dot(normalize(rd.xz + vec2f(1e-5)), normalize(A.sun_dir.xz + vec2f(1e-5))), 0.0);
    let warm = A.horizon.rgb + A.sun.rgb * 0.10 * pow(toward, 6.0) * smoothstep(-0.1, 0.3, A.sun_dir.y + 0.1);
    return mix(A.horizon.rgb, warm, 0.85);
}
// The sky in a direction: the gradient, the sun's glow and its disc.
fn sky_color(rd : vec3f) -> vec3f {
    let up = max(rd.y, 0.0);
    var c = mix(horizon_color(rd), A.zenith.rgb, pow(up, 0.42));
    // Below the horizon: the haze over the ground, a little darker.
    c = mix(c, horizon_color(rd) * 0.55, smoothstep(0.0, -0.25, rd.y));
    let mu = dot(rd, A.sun_dir.xyz);
    // Forward-scattered glow round the sun, then the disc itself.
    c += A.sun.rgb * (hg(mu, 0.80) * 0.55 + hg(mu, 0.965) * 0.035);
    c += A.sun.rgb * 60.0 * smoothstep(0.99975, 0.99991, mu);
    return c;
}
`

// postVolWGSL: the light shafts, at half resolution. Output: rgb the light
// scattered toward the eye, a what is left of the light behind it.
const postVolWGSL = `
@group(0) @binding(0) var depth : texture_depth_2d;
@group(0) @binding(1) var shadow : texture_depth_2d;
@group(0) @binding(2) var<uniform> A : AtmU;
@group(0) @binding(3) var<storage, read> lights : Lights;
` + postAtmosCommonWGSL + `
// The Renderer3D's light list (matshader.go's layout).
struct Light {
    pos_range : vec4f,  // xyz position, w range
    color : vec4f,      // rgb colour * energy, w kind (0 point, 1 spot)
    dir_cos : vec4f,    // xyz cone axis, w cos(outer half angle)
    params : vec4f,     // x cos(inner half angle)
}
struct Lights { items : array<Light, 16> }
const VOL_STEPS = 20;
// The lamps' light reaching a point of air: the surface shader's falloff
// and cone, scattered evenly in all directions.
fn lamp_light(p : vec3f) -> vec3f {
    var sum = vec3f(0.0);
    let count = u32(A.lamps.x);
    for (var i = 0u; i < count; i = i + 1u) {
        let li = lights.items[i];
        let dv = li.pos_range.xyz - p;
        let dist = length(dv);
        if (dist >= li.pos_range.w) { continue; }
        let nd = dist / li.pos_range.w;
        var win = max(1.0 - nd * nd * nd * nd, 0.0);
        win = win * win;
        var att = win / (1.0 + dist * dist);
        if (li.color.w > 0.5) {
            let cd = dot(-dv / max(dist, 1e-4), li.dir_cos.xyz);
            att = att * smoothstep(li.dir_cos.w, li.params.x, cd);
        }
        sum += li.color.rgb * att;
    }
    return sum * (A.lamps.y / 12.566371);
}
// The air's density relative to the ground's, thinning with height.
fn shape(h : f32) -> f32 { return 0.28 + 0.72 * exp(-max(h, 0.0) / A.amb.w); }
// Whether a point of air is in sunlight.
fn sun_vis(p : vec3f) -> f32 {
    let sc = A.light_vp * vec4f(p, 1.0);
    let uv = vec2f(sc.x * 0.5 + 0.5, -sc.y * 0.5 + 0.5);
    if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0 || sc.z < 0.0 || sc.z > 1.0) { return 1.0; }
    let dims = vec2f(textureDimensions(shadow));
    let d = textureLoad(shadow, vec2i(min(uv * dims, dims - 1.0)), 0);
    return step(sc.z - 0.0008, d);
}
@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let dims = vec2f(textureDimensions(depth));
    let c0 = min(vec2i(in.pos.xy) * 2, vec2i(dims) - 1);
    let uv = (vec2f(c0) + 0.5) / dims;
    let d = textureLoad(depth, c0, 0);
    let is_sky = d >= 0.99999;
    let vr = view_ray(uv);
    let rd = world_dir(vr);
    let max_d = A.horizon.w;
    var dist = max_d * 0.6;                      // the sky: march a way into it
    if (!is_sky) { dist = view_dist(d) * length(vr); }
    let march = min(dist, max_d);
    let sigma = A.sun.w;
    let mu = dot(rd, A.sun_dir.xyz);
    // Two lobes: a tight forward one (the glow looking toward the sun) and
    // a broad one, so shafts seen from the side still read.
    let ph = min(hg(mu, 0.76) * 0.6 + hg(mu, 0.2) * 0.4, 1.2);
    let jitter = ign(in.pos.xy);
    var acc = vec3f(0.0);
    var T = 1.0;
    var t0 = 0.0;
    for (var i = 0; i < VOL_STEPS; i++) {
        let f = (f32(i) + 1.0) / f32(VOL_STEPS);
        let t1 = march * f * (0.3 + 0.7 * f);    // denser steps near the eye
        let ds = t1 - t0;
        let p = A.cam.xyz + rd * (t0 + ds * jitter);
        var lit = 1.0;
        if (A.sun_dir.w > 0.5) { lit = sun_vis(p); }
        let ext = sigma * shape(p.y);
        let Ti = exp(-ext * ds);
        var src = A.sun.rgb * (ph * lit) + A.amb.rgb * (0.35 + 0.65 * lit);
        if (A.lamps.x > 0.5) { src += lamp_light(p); }
        acc += T * src * (1.0 - Ti);
        T *= Ti;
        t0 = t1;
    }
    // Past the march: the haze goes on, fading the far field into the
    // horizon's colour in that direction.
    if (!is_sky && dist > max_d) {
        let h1 = A.cam.y + rd.y * max_d;
        let h2 = A.cam.y + rd.y * dist;
        let tau = sigma * 0.5 * (dist - max_d) * 0.5 * (shape(h1) + shape(h2));
        let Tf = exp(-tau);
        acc += T * (horizon_color(rd) * 0.5 + A.amb.rgb * 0.5) * (1.0 - Tf);
        T *= Tf;
    }
    return vec4f(acc, T);
}
`

// postVolBlurWGSL: the depth-aware separable blur, over all four channels.
const postVolBlurWGSL = `
@group(0) @binding(0) var depth : texture_depth_2d;
@group(0) @binding(1) var src : texture_2d<f32>;
@group(0) @binding(2) var<uniform> A : AtmU;
` + postAtmosCommonWGSL + `
fn blur(frag : vec2f, dir : vec2i) -> vec4f {
    let dmax = vec2i(textureDimensions(depth)) - 1;
    let smax = vec2i(textureDimensions(src)) - 1;
    let p = vec2i(frag);
    let z0 = min(view_dist(textureLoad(depth, min(p * 2, dmax), 0)), 400.0);
    var acc = vec4f(0.0); var ws = 0.0;
    for (var i = -3; i <= 3; i++) {
        let q = clamp(p + dir * i, vec2i(0), smax);
        let z = min(view_dist(textureLoad(depth, min(q * 2, dmax), 0)), 400.0);
        let w = (1.0 - abs(f32(i)) * 0.2) * max(0.0, 1.0 - abs(z - z0) / (0.08 * z0 + 0.05));
        acc += textureLoad(src, q, 0) * w; ws += w;
    }
    if (ws > 1e-4) { return acc / ws; }
    return textureLoad(src, min(p, smax), 0);
}
@fragment fn fs_h(in : VOut) -> @location(0) vec4f { return blur(in.pos.xy, vec2i(1, 0)); }
@fragment fn fs_v(in : VOut) -> @location(0) vec4f { return blur(in.pos.xy, vec2i(0, 1)); }
`

// postAtmosWGSL: the combine. The half-resolution shafts are upsampled
// with depth as the guide (so haze does not bleed across silhouettes),
// then scene × transmittance + in-scatter; empty pixels take the sky.
const postAtmosWGSL = `
@group(0) @binding(0) var depth : texture_depth_2d;
@group(0) @binding(1) var scene : texture_2d<f32>;
@group(0) @binding(2) var vol : texture_2d<f32>;
@group(0) @binding(3) var<uniform> A : AtmU;
` + postAtmosCommonWGSL + `
// The view-space position of a texel.
fn view_pos(t : vec2i, dims : vec2f) -> vec3f {
    return view_ray((vec2f(t) + 0.5) / dims) * view_dist(textureLoad(depth, t, 0));
}
// Screen-space reflection: from a mirror-like pixel, march the reflected
// ray through the depth buffer (strides growing with distance, then a
// bisection to the surface) and return what it meets with a confidence in
// w: zero when it leaves the screen, goes behind something, or comes back
// toward the camera.
fn trace_reflection(c : vec2i, dims : vec2f) -> vec4f {
    let dmax = vec2i(dims) - 1;
    let p = view_pos(c, dims);
    // The surface normal from the depth buffer: on each axis, the neighbour
    // whose depth is nearer ours (so an edge does not bend the normal).
    let px0 = view_pos(clamp(c - vec2i(1, 0), vec2i(0), dmax), dims);
    let px1 = view_pos(clamp(c + vec2i(1, 0), vec2i(0), dmax), dims);
    let py0 = view_pos(clamp(c - vec2i(0, 1), vec2i(0), dmax), dims);
    let py1 = view_pos(clamp(c + vec2i(0, 1), vec2i(0), dmax), dims);
    var dx = px1 - p;
    if (abs(px0.z - p.z) < abs(px1.z - p.z)) { dx = p - px0; }
    var dy = py1 - p;
    if (abs(py0.z - p.z) < abs(py1.z - p.z)) { dy = p - py0; }
    var n = normalize(cross(dy, dx));
    let v = normalize(p);
    if (dot(n, v) > 0.0) { n = -n; }
    let r = reflect(v, n);
    if (r.z > 0.15) {
        return vec4f(0.0); // back toward the camera: nothing on screen to meet
    }
    let dist = -p.z;
    var stride = max(0.08, dist * 0.02) * (0.6 + 0.8 * ign(vec2f(c)));
    var t0 = max(0.05, dist * 0.004);
    var t1 = t0;
    var found = false;
    for (var i = 0; i < 40; i++) {
        t1 = t0 + stride;
        let q = p + r * t1;
        if (q.z > -0.05) { break; }
        let uv = vec2f(q.x * A.proj.x / -q.z, q.y * A.proj.y / -q.z) * vec2f(0.5, -0.5) + 0.5;
        if (any(uv < vec2f(0.0)) || any(uv > vec2f(1.0))) { break; }
        let dq = textureLoad(depth, min(vec2i(uv * dims), dmax), 0);
        let zs = view_dist(dq);
        let zr = -q.z;
        // Behind the surface there, but not by more than a plausible thickness.
        if (dq < 0.99999 && zr > zs && zr - zs < max(1.5 * stride, 0.3 + 0.05 * zr)) {
            found = true;
            break;
        }
        t0 = t1;
        stride *= 1.14;
    }
    if (!found) {
        return vec4f(0.0);
    }
    for (var i = 0; i < 5; i++) {
        let tm = 0.5 * (t0 + t1);
        let q = p + r * tm;
        let uv = vec2f(q.x * A.proj.x / -q.z, q.y * A.proj.y / -q.z) * vec2f(0.5, -0.5) + 0.5;
        let zs = view_dist(textureLoad(depth, clamp(vec2i(uv * dims), vec2i(0), dmax), 0));
        if (-q.z > zs) { t1 = tm; } else { t0 = tm; }
    }
    let q = p + r * t1;
    let uv = vec2f(q.x * A.proj.x / -q.z, q.y * A.proj.y / -q.z) * vec2f(0.5, -0.5) + 0.5;
    let hit = max(textureLoad(scene, clamp(vec2i(uv * dims), vec2i(0), dmax), 0).rgb, vec3f(0.0));
    // Fade toward the screen's edges and for rays turning back at the camera.
    let e = min(min(uv.x, 1.0 - uv.x), min(uv.y, 1.0 - uv.y));
    let conf = smoothstep(0.0, 0.08, e) * (1.0 - smoothstep(-0.1, 0.15, r.z));
    return vec4f(hit - sky_color(world_dir(r)), conf);
}

@fragment fn fs(in : VOut) -> @location(0) vec4f {
    let c = vec2i(in.pos.xy);
    let d = textureLoad(depth, c, 0);
    let sc = textureLoad(scene, c, 0);
    var col = max(sc.rgb, vec3f(0.0));
    let is_sky = d >= 0.99999;
    // Reflections of the scene: the material left its mirror strength in
    // alpha and already added the sky's reflection; where the ray meets the
    // scene, swap the sky for what it meets.
    if (A.lamps.z > 0.5 && !is_sky && sc.a < 0.995) {
        let hit = trace_reflection(c, vec2f(textureDimensions(depth)));
        col = max(col + hit.rgb * hit.w * (1.0 - sc.a), vec3f(0.0));
    }
    if (is_sky && A.zenith.w > 0.5) {
        let dims = vec2f(textureDimensions(depth));
        col = sky_color(world_dir(view_ray((vec2f(c) + 0.5) / dims)));
    }
    if (A.sun.w > 0.0) {
        // Joint bilateral upsample: the four half-resolution texels round
        // this pixel, weighted by how close their depth is to ours.
        let vmax = vec2i(textureDimensions(vol)) - 1;
        let dmax = vec2i(textureDimensions(depth)) - 1;
        let hp = (vec2f(c) + 0.5) * 0.5 - 0.5;
        let base = vec2i(floor(hp));
        let f = fract(hp);
        let z0 = min(view_dist(d), 400.0);
        var acc = vec4f(0.0); var ws = 0.0;
        for (var k = 0; k < 4; k++) {
            let o = vec2i(k & 1, k >> 1);
            let q = clamp(base + o, vec2i(0), vmax);
            let z = min(view_dist(textureLoad(depth, min(q * 2, dmax), 0)), 400.0);
            var w = select(1.0 - f.x, f.x, o.x == 1) * select(1.0 - f.y, f.y, o.y == 1);
            w *= 1.0 / (0.02 + abs(z - z0) / z0);
            w = max(w, 1e-5);
            acc += textureLoad(vol, q, 0) * w; ws += w;
        }
        let v = acc / ws;
        col = col * v.a + v.rgb;
    }
    return vec4f(col, 1.0);
}
`
