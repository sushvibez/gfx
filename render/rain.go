package render

import (
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render/particles"
	"github.com/sushvibez/gfx/render/wgsl"
)

// Rain as particles: FrameSettings.Rain fills a box of air round the camera
// with falling drops and the ground under it with splashes, and drips from
// edges (particles.Splash); FrameSettings.Snow fills it with flakes
// instead, wandering as they fall (particles.Precip). Nothing is
// simulated or stored: every particle's place is a function of its index
// and the time (a hash for where it starts, falling at its own speed,
// wrapped into the box as the camera moves), so the pass is one instanced
// draw with no buffers and costs the same in the browser.
//
// Drops are streaks along their velocity, as a camera's shutter draws
// them, lit at their own position by the ambient light, the sun and every
// lamp (strongly when the lamp is behind them: water scatters forward),
// which is what makes rain show in a beam and vanish against a bright
// sky. Drips are the same streaks falling from edges: wherever the sky
// map shows the surface falling away by most of a metre, water gathers on
// the rim and falls from it. The sky map (lights.go) stops them at roofs and at the ground and
// says how high the ground is, for the splashes.
//
// They are drawn into the main pass after the scene, tested against its
// depth without writing it, and added to the colour without touching
// alpha (the HDR target's alpha is the mirror strength).

const (
	rainDrops    = 28000
	rainSplashes = 5000
	rainDrips    = 9000
)

const rainWGSL = frameWGSL + wgsl.Hash + wgsl.Light + particles.Precip + rainGroundWGSL + particles.Splash + `
// The light a drop of water at p sends toward the camera.
fn drop_light(p : vec3f) -> vec3f {
    let view = normalize(p - frame.cam_pos.xyz);
    var sum = frame.ambient.rgb * 0.8 + max(frame.sun.rgb, vec3f(0.0)) * 0.10;
    let count = u32(frame.misc.y);
    for (var i = 0u; i < count; i = i + 1u) {
        let li = lights.items[i];
        let dv = li.pos_range.xyz - p;
        let dist = length(dv);
        if (dist >= li.pos_range.w) { continue; }
        var att = light_window(dist * dist, li.pos_range.w * li.pos_range.w, 0.0);
        let travel = -dv / max(dist, 1e-4); // the way the light travels to the drop
        if (li.color.w > 0.5) {
            att = att * smoothstep(li.dir_cos.w, li.params.x, dot(travel, li.dir_cos.xyz));
        }
        // Forward scattering: brightest looking toward the lamp through the drop.
        let fwd = max(dot(view, travel), 0.0);
        sum += li.color.rgb * att * (0.6 + 5.0 * fwd * fwd * fwd * fwd);
    }
    return sum;
}

@vertex
fn vs(@builtin(vertex_index) vi : u32, @builtin(instance_index) ii : u32) -> VOut {
    var o : VOut;
    let corner = vec2f(f32((vi == 1u) || (vi == 2u) || (vi == 4u)), f32((vi == 2u) || (vi == 4u) || (vi == 5u))) * 2.0 - 1.0;
    o.uv = corner;
    let rain = frame.sun.w;
    let t = frame.env.y;
    let cam = frame.cam_pos.xyz;
    let h = hash4u(ii);
    var centre = vec3f(0.0);
    var right = vec3f(0.0);
    var up = vec3f(0.0);
    var fade = 0.0;
    let snow = frame.view.z;
    if (ii < DROPS && snow > 0.0) {
        o.kind = 2.0;
        // A flake (particles.Precip): slow, wandering on its own eddies,
        // in a smaller box a quarter above the eye.
        let c0 = vec3f(0.0, 0.25, 0.0);
        let box = PRECIP_SNOW_BOX;
        let vel = precip_velocity(h, vec3f(0.5, 0.0, 0.2), true);
        let rel = precip_wrap(h.xyz * box + vel * t + precip_wobble(h, t), cam, box, c0);
        let p = cam + rel;
        let dist = length(rel);
        let fy = length(vec3f(frame.view_proj[0][1], frame.view_proj[1][1], frame.view_proj[2][1]));
        let lines = select(900.0, frame.view.y, frame.view.y > 0.0);
        let size = precip_flake_size(h, dist * 2.0 / (fy * lines), length(vel));
        let axis = normalize(vel);
        right = normalize(cross(axis, rel)) * size.x;
        up = axis * size.y;
        centre = p;
        fade = size.z * smoothstep(0.25, 0.9, dist) * sky_at(p).x * precip_edge(rel, box, c0)
             * step(h.x * 0.999 + fract(h.w * 7.0) * 0.001, snow * 1.25) * 0.8;
    } else if (ii < DROPS) {
        o.kind = 0.0;
        // A drop: its own speed, a little wind, wrapped into the box
        // (particles.Precip, as every gfx renderer's rain).
        let vel = precip_velocity(h, vec3f(0.7, 0.0, 0.25), false);
        let rel = precip_wrap(h.xyz * BOX + vel * t, cam, BOX, vec3f(0.0));
        let p = cam + rel;
        let dist = length(rel);
        // As long as the shutter is open; as wide as a drop, or as a pixel
        // (then fainter, so far rain thins into a veil instead of wires).
        // A pixel's angle, from the projection's vertical scale and the
        // target's height (a guess at 900 lines when the size is not told).
        let fy = length(vec3f(frame.view_proj[0][1], frame.view_proj[1][1], frame.view_proj[2][1]));
        let lines = select(900.0, frame.view.y, frame.view.y > 0.0);
        let width = max(0.0028, dist * 1.7 / (fy * lines));
        let axis = normalize(vel);
        right = normalize(cross(axis, rel)) * width;
        up = axis * length(vel) * 0.022;
        centre = p;
        let sky = sky_at(p);
        fade = min(0.0028 / width * 3.0, 1.0) * smoothstep(0.5, 1.5, dist) * sky.x
             * precip_edge(rel, BOX, vec3f(0.0)) * step(h.x * 0.999 + fract(h.w * 7.0) * 0.001, rain * 1.25);
    } else if (ii >= DROPS + SPLASHES) {
        o.kind = 0.0;
        // A drip: water running off an edge (particles.Splash).
        let fy = length(vec3f(frame.view_proj[0][1], frame.view_proj[1][1], frame.view_proj[2][1]));
        let lines = select(900.0, frame.view.y, frame.view.y > 0.0);
        let q = precip_drip(ii, t, cam, PAD, rain, 2.0 / (fy * lines));
        centre = q.centre; right = q.right; up = q.up; fade = q.fade;
    } else {
        o.kind = 1.0;
        // A splash: a new place each time round, on whatever the rain
        // meets (particles.Splash).
        let q = precip_splash(ii, t, cam, PAD, rain);
        centre = q.centre; right = q.right; up = q.up; fade = q.fade;
    }
    let world = centre + right * corner.x + up * corner.y;
    o.pos = frame.view_proj * vec4f(world, 1.0);
    o.color = drop_light(centre) * fade * frame.ambient.w;
    if (u32(frame.misc.x) != 3u) {
        o.color = o.color * 0.5; // not HDR: no camera after this to expose it
    }
    return o;
}

@fragment
fn fs(in : VOut) -> @location(0) vec4f {
    var a = 0.0;
    if (in.kind < 0.5) {
        // A streak: soft across, tapering toward its tail (the top).
        a = precip_streak(in.uv, 0.2, -0.8) * 0.42;
    } else if (in.kind < 1.5) {
        a = precip_spray(in.uv);
    } else {
        a = precip_flake(in.uv) * 0.6;
    }
    return vec4f(in.color * a, 0.0);
}
`

// rainGroundWGSL is the rain pass's bindings and the sky map's reading,
// which particles.Splash asks for (precip_ground).
const rainGroundWGSL = `
struct Light {
    pos_range : vec4f,
    color : vec4f,
    dir_cos : vec4f,
    params : vec4f,
}
struct Lights { items : array<Light, 16>, spot_vp : array<mat4x4f, 4>, rain_vp : mat4x4f }
@group(0) @binding(0) var<storage, read> frame : Frame;
@group(0) @binding(1) var<storage, read> lights : Lights;
@group(0) @binding(2) var sky_tex : texture_depth_2d;

const DROPS = ` + "28000u" + `;
const SPLASHES = ` + "5000u" + `;
const BOX = vec3f(34.0, 22.0, 34.0);    // the air the drops fill, centred on the camera
const PAD = vec2f(26.0, 26.0);          // the ground the splashes cover

struct VOut {
    @builtin(position) pos : vec4f,
    @location(0) uv : vec2f,
    @location(1) color : vec3f,
    @location(2) kind : f32,
}

// The sky map at a world point: x is 1 when nothing is over it, y the
// height of whatever the rain meets first above or below it.
fn sky_at(w : vec3f) -> vec2f {
    if (lights.rain_vp[3][3] == 0.0) {
        return vec2f(1.0, -1e5);
    }
    let sc = lights.rain_vp * vec4f(w, 1.0);
    let uv = vec2f(sc.x * 0.5 + 0.5, -sc.y * 0.5 + 0.5);
    if (any(uv < vec2f(0.0)) || any(uv > vec2f(1.0))) {
        return vec2f(1.0, -1e5);
    }
    let dims = vec2f(textureDimensions(sky_tex));
    let top = textureLoad(sky_tex, min(vec2i(uv * dims), vec2i(dims) - 1), 0);
    // Looking straight down, depth is a linear function of height alone.
    let height = (top - lights.rain_vp[3][2]) / lights.rain_vp[1][2];
    return vec2f(step(sc.z, top + 0.0004), height);
}

// What the rain meets at plan point q (particles.Splash's hook).
fn precip_ground(q : vec2f) -> f32 {
    return sky_at(vec3f(q.x, 0.0, q.y)).y;
}
`

// initRain builds the rain pass's pipeline (lazily: only frames that rain
// pay for it). It reports whether the pass can be drawn.
func (r *Renderer3D) initRain() bool {
	if r.rainPL != nil {
		return true
	}
	if r.rainFailed {
		return false
	}
	m, err := r.device.CreateShaderModuleWGSL("renderer3d-rain", rainWGSL)
	if err != nil {
		r.rainFailed = true
		return false
	}
	defer m.Release()
	add := gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendOne, Dst: gpu.BlendOne}
	keep := gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendZero, Dst: gpu.BlendOne}
	pl, err := r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: m, VertexEntry: "vs", FragmentEntry: "fs",
		Format: r.format, DepthTest: true, DepthNoWrite: true,
		Blend: &gpu.BlendDesc{Color: add, Alpha: keep},
	})
	if err != nil {
		r.rainFailed = true
		return false
	}
	r.rainPL = pl
	l := pl.BindGroupLayout(0)
	defer l.Release()
	r.rainBind = r.device.CreateBindGroup(l,
		gpu.BindGroupEntry{Binding: 0, Buffer: r.frame},
		gpu.BindGroupEntry{Binding: 1, Buffer: r.lightBuf},
		gpu.BindGroupEntry{Binding: 2, TextureView: r.skyView},
	)
	return true
}

// RenderRain draws the rain in a pass of its own over a finished picture:
// color already holds the scene and depth its depth, both kept. It is for
// a caller that set DeferRain so that something could come between the
// scene and the rain (View3D does, so the temporal filter averages the
// scene and not the falling drops).
//
// The particles (SetParticles) are drawn here too, first, soft against
// the depth.
func (r *Renderer3D) RenderRain(enc *gpu.CommandEncoder, color, depth *gpu.TextureView) {
	r.renderParticlesSoft(enc, color, depth)
	if !r.precipOn() || !r.initRain() {
		return
	}
	pass := enc.BeginRenderPassOver(color, depth)
	r.drawRain(pass)
	pass.End()
}

// drawRain records the drops and splashes into the main pass.
func (r *Renderer3D) drawRain(pass *gpu.RenderPass) {
	if !r.precipOn() || !r.initRain() {
		return
	}
	pass.SetPipeline(r.rainPL)
	pass.SetBindGroup(0, r.rainBind)
	pass.Draw(6, rainDrops+rainSplashes+rainDrips, 0, 0)
}

// precipOn reports whether rain or snow falls this frame.
func (r *Renderer3D) precipOn() bool { return r.settings.Rain > 0 || r.settings.Snow > 0 }

func (r *Renderer3D) releaseRain() {
	rel(r.rainBind)
	rel(r.rainPL)
	r.rainBind, r.rainPL = nil, nil
}
