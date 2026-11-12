package render

import "github.com/sushvibez/gfx/gpu"

// Rain as particles: FrameSettings.Rain fills a box of air round the camera
// with falling drops and the ground under it with splashes. Nothing is
// simulated or stored: every particle's place is a function of its index
// and the time (a hash for where it starts, falling at its own speed,
// wrapped into the box as the camera moves), so the pass is one instanced
// draw with no buffers and costs the same in the browser.
//
// Drops are streaks along their velocity, as a camera's shutter draws
// them, lit at their own position by the ambient light, the sun and every
// lamp (strongly when the lamp is behind them: water scatters forward),
// which is what makes rain show in a beam and vanish against a bright
// sky. The sky map (lights.go) stops them at roofs and at the ground and
// says how high the ground is, for the splashes.
//
// They are drawn into the main pass after the scene, tested against its
// depth without writing it, and added to the colour without touching
// alpha (the HDR target's alpha is the mirror strength).

const (
	rainDrops    = 28000
	rainSplashes = 5000
)

const rainWGSL = frameWGSL + `
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
const BOX = vec3f(34.0, 22.0, 34.0);    // the air the drops fill, centred on the camera
const PAD = vec2f(26.0, 26.0);          // the ground the splashes cover

struct VOut {
    @builtin(position) pos : vec4f,
    @location(0) uv : vec2f,
    @location(1) color : vec3f,
    @location(2) kind : f32,
}

fn hash4(i : u32) -> vec4f {
    var s = vec4<u32>(i * 747796405u + 2891336453u, i * 2246822519u + 3266489917u,
                      i * 668265263u + 374761393u, i * 1597334677u + 3812015801u);
    s = (s ^ (s >> vec4<u32>(15u))) * vec4<u32>(2246822519u);
    s = (s ^ (s >> vec4<u32>(13u))) * vec4<u32>(3266489917u);
    s = s ^ (s >> vec4<u32>(16u));
    return vec4f(s) * (1.0 / 4294967296.0);
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
        let nd = dist / li.pos_range.w;
        var win = max(1.0 - nd * nd * nd * nd, 0.0);
        win = win * win;
        var att = win / (1.0 + dist * dist);
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
    let h = hash4(ii);
    var centre = vec3f(0.0);
    var right = vec3f(0.0);
    var up = vec3f(0.0);
    var fade = 0.0;
    if (ii < DROPS) {
        o.kind = 0.0;
        // A drop: its own speed, a little wind, wrapped into the box.
        let vel = vec3f(0.7, -(7.0 + 3.0 * h.w), 0.25);
        var p = h.xyz * BOX + vel * t;
        p = cam + (fract((p - cam) / BOX + 0.5) - 0.5) * BOX;
        let rel = p - cam;
        let dist = length(rel);
        // As long as the shutter is open; as wide as a drop, or as a pixel
        // (then fainter, so far rain thins into a veil instead of wires).
        let width = max(0.0028, dist * 0.0008);
        let axis = normalize(vel);
        right = normalize(cross(axis, rel)) * width;
        up = axis * length(vel) * 0.022;
        centre = p;
        let sky = sky_at(p);
        let edge = (0.5 - abs(rel / BOX)) * 8.0; // fade toward the box's faces: no popping
        fade = min(0.0028 / width * 3.0, 1.0) * smoothstep(0.5, 1.5, dist) * sky.x
             * clamp(min(edge.x, min(edge.y, edge.z)), 0.0, 1.0) * step(h.x * 0.999 + fract(h.w * 7.0) * 0.001, rain * 1.25);
    } else {
        o.kind = 1.0;
        // A splash: a new place each time round, on whatever the rain meets.
        let life = 0.5 + 0.7 * h.z;
        let turn = t / life + h.w;
        let hc = hash4(ii * 7919u + u32(floor(turn)));
        let age = fract(turn) * life / 0.16; // seen for the first 0.16 s of each round
        var q = hc.xy * PAD;
        q = cam.xz + (fract((q - cam.xz) / PAD + 0.5) - 0.5) * PAD;
        let sky = sky_at(vec3f(q.x, cam.y + 60.0, q.y));
        let ground = vec3f(q.x, sky.y, q.y);
        let rel = ground - cam;
        // A hand's breadth of spray at most, thrown up and out as it ages.
        let size = 0.012 + 0.03 * age;
        right = normalize(vec3f(-rel.z, 0.0, rel.x)) * size * 1.6;
        up = vec3f(0.0, size * (0.7 + 0.9 * age), 0.0);
        centre = ground + up;
        let edge = (0.5 - abs(rel.xz / PAD)) * 8.0;
        fade = step(age, 1.0) * (1.0 - age) * clamp(min(edge.x, edge.y), 0.0, 1.0)
             * step(-1e4, sky.y) * step(hc.z, rain * 1.25) * 0.45;
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
        a = (1.0 - in.uv.x * in.uv.x) * smoothstep(1.0, 0.2, in.uv.y) * smoothstep(-1.0, -0.8, in.uv.y) * 0.42;
    } else {
        // A splash: a low fan of spray, densest at the ground, thinning as
        // it spreads up and out.
        let r = length(vec2f(in.uv.x, (in.uv.y + 1.0) * 0.5));
        a = smoothstep(1.0, 0.25, r) * (1.0 - 0.6 * (in.uv.y * 0.5 + 0.5)) * 0.5;
    }
    return vec4f(in.color * a, 0.0);
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

// drawRain records the drops and splashes into the main pass.
func (r *Renderer3D) drawRain(pass *gpu.RenderPass) {
	if r.settings.Rain <= 0 || !r.initRain() {
		return
	}
	pass.SetPipeline(r.rainPL)
	pass.SetBindGroup(0, r.rainBind)
	pass.Draw(6, rainDrops+rainSplashes, 0, 0)
}

func (r *Renderer3D) releaseRain() {
	rel(r.rainBind)
	rel(r.rainPL)
	r.rainBind, r.rainPL = nil, nil
}
