package render

// WGSL for the material pass (DESIGN.md §9). The legacy PBR shader in
// renderer3d.go is untouched — DrawMaterial/DrawInstanced run through this
// second pipeline family instead, so existing examples keep their exact
// pixels while material draws get normal maps, ARM packing, triplanar
// projection, stochastic hex tiling, punctual lights, fog and tone mapping.
//
// The two shaders share one Frame block (frameWGSL) and one shadow map, so
// a scene may mix both kinds of draw in a single pass.

// frameWGSL is the per-frame uniform block, shared by both 3D shaders. The
// first four members are the original layout; everything after is the
// environment added with FrameSettings, which the legacy shader simply
// ignores (a storage binding may be larger than the struct that reads it,
// and the prefix is identical).
const frameWGSL = `
struct Frame {
    view_proj : mat4x4f,
    light_vp : mat4x4f,
    light_dir : vec4f,   // xyz normalized sun direction, w sun intensity
    cam_pos : vec4f,
    ambient : vec4f,     // rgb ambient radiance, w exposure
    fog_color : vec4f,   // rgb fog colour, w density
    fog_params : vec4f,  // x height, y height density, z aerial perspective, w enabled
    sky_top : vec4f,
    sky_horizon : vec4f,
    misc : vec4f,        // x tonemap mode, y light count
    sun : vec4f,         // rgb the sun's radiance (colour times energy)
    env : vec4f,         // x sky reflection strength, y time in seconds
}
`

const shaderMaterial = frameWGSL + `
struct MDraw {
    model : mat4x4f,
    color : vec4f,
    p0 : vec4f,   // metallic, roughness, normal scale, uv scale
    p1 : vec4f,   // flags, emission, alpha, roughness channel
    p2 : vec4f,   // x hex-tiling rotation scale
}
struct Light {
    pos_range : vec4f,  // xyz position, w range
    color : vec4f,      // rgb colour * energy, w kind (0 point, 1 spot)
    dir_cos : vec4f,    // xyz cone axis, w cos(outer half angle)
    params : vec4f,     // x cos(inner half angle), y shadow slot + 1, z shadow texel size per metre
}
struct Lights { items : array<Light, 16>, spot_vp : array<mat4x4f, 4> }

@group(0) @binding(0) var<storage, read> frame : Frame;
@group(0) @binding(1) var<storage, read> draws : array<MDraw>;
@group(0) @binding(2) var shadow_samp : sampler_comparison;
@group(0) @binding(3) var shadow_tex : texture_depth_2d;
@group(0) @binding(4) var<storage, read> lights : Lights;
@group(0) @binding(5) var spot_tex0 : texture_depth_2d;
@group(0) @binding(6) var spot_tex1 : texture_depth_2d;
@group(0) @binding(7) var spot_tex2 : texture_depth_2d;
@group(0) @binding(8) var spot_tex3 : texture_depth_2d;

@group(1) @binding(0) var samp : sampler;
@group(1) @binding(1) var albedo_tex : texture_2d<f32>;
@group(1) @binding(2) var normal_tex : texture_2d<f32>;
@group(1) @binding(3) var arm_tex : texture_2d<f32>;
@group(1) @binding(4) var emis_tex : texture_2d<f32>;

const PI = 3.14159265;
const F_TRIPLANAR = 1u;
const F_ANTITILE = 2u;
const F_UNLIT = 4u;
const F_NORMAL = 8u;
const F_ARM = 16u;
const F_ROUGH = 32u;
const F_EMISSIVE = 64u;
const F_WATER = 128u;

struct VSOut {
    @builtin(position) pos : vec4f,
    @location(0) world : vec3f,
    @location(1) normal : vec3f,
    @location(2) uv : vec2f,
    @location(3) tangent : vec4f,
    @location(4) @interpolate(flat) draw_id : u32,
}

struct FSIn {
    @builtin(position) pos : vec4f,
    @builtin(front_facing) front : bool,
    @location(0) world : vec3f,
    @location(1) normal : vec3f,
    @location(2) uv : vec2f,
    @location(3) tangent : vec4f,
    @location(4) @interpolate(flat) draw_id : u32,
}

@vertex
fn vs(@location(0) pos : vec3f, @location(1) normal : vec3f, @location(2) uv : vec2f,
      @location(3) tangent : vec4f, @builtin(instance_index) iid : u32) -> VSOut {
    let m = draws[iid].model;
    let world = m * vec4f(pos, 1.0);
    var out : VSOut;
    out.pos = frame.view_proj * world;
    out.world = world.xyz;
    // Uniform-scale assumption, as in the base renderer.
    out.normal = (m * vec4f(normal, 0.0)).xyz;
    out.uv = uv;
    out.tangent = vec4f((m * vec4f(tangent.xyz, 0.0)).xyz, tangent.w);
    out.draw_id = iid;
    return out;
}

@vertex
fn vs_shadow(@location(0) pos : vec3f, @location(1) normal : vec3f, @location(2) uv : vec2f,
             @location(3) tangent : vec4f, @builtin(instance_index) iid : u32) -> @builtin(position) vec4f {
    return frame.light_vp * (draws[iid].model * vec4f(pos, 1.0));
}

// Interleaved gradient noise (Jimenez): a per-pixel rotation for the
// shadow kernels, so undersampling shows as fine noise, not bands.
fn sh_ign(p : vec2f) -> f32 { return fract(52.9829189 * fract(dot(p, vec2f(0.06711056, 0.00583715)))); }
// Vogel disc: n points spread evenly over the unit disc, rotated by phi.
fn sh_vogel(i : i32, n : i32, phi : f32) -> vec2f {
    let r = sqrt((f32(i) + 0.5) / f32(n));
    let th = f32(i) * 2.39996323 + phi;
    return r * vec2f(cos(th), sin(th));
}

fn spot_cmp(slot : i32, uv : vec2f, z : f32) -> f32 {
    switch slot {
        case 0 { return textureSampleCompareLevel(spot_tex0, shadow_samp, uv, z); }
        case 1 { return textureSampleCompareLevel(spot_tex1, shadow_samp, uv, z); }
        case 2 { return textureSampleCompareLevel(spot_tex2, shadow_samp, uv, z); }
        default { return textureSampleCompareLevel(spot_tex3, shadow_samp, uv, z); }
    }
}

// A spot light's shadow: its own perspective map, the receiver pushed
// along its normal by a couple of that map's texels at its distance, and a
// small rotated disc of taps.
fn spot_shadow(li : Light, world : vec3f, n : vec3f, dist : f32, frag : vec2f) -> f32 {
    let slot = i32(li.params.y) - 1;
    let texel = li.params.z * dist;
    let sc = lights.spot_vp[slot] * vec4f(world + n * texel * 2.0, 1.0);
    if (sc.w <= 0.0) {
        return 1.0;
    }
    let p = sc.xyz / sc.w;
    let uv = vec2f(p.x * 0.5 + 0.5, -p.y * 0.5 + 0.5);
    if (any(uv < vec2f(0.0)) || any(uv > vec2f(1.0))) {
        return 1.0;
    }
    let phi = sh_ign(frag) * 6.2831853;
    let radius = 2.5 / f32(textureDimensions(spot_tex0).x);
    var lit = 0.0;
    for (var i = 0; i < 8; i++) {
        lit += spot_cmp(slot, uv + sh_vogel(i, 8, phi) * radius, p.z - 0.00002);
    }
    return lit / 8.0;
}

// Value noise over the plane, for where puddles gather.
fn wet_hash(p : vec2f) -> f32 {
    return fract(sin(dot(p, vec2f(127.1, 311.7))) * 43758.5453);
}
fn wet_noise(p : vec2f) -> f32 {
    let i = floor(p);
    let f = fract(p);
    let u = f * f * (3.0 - 2.0 * f);
    return mix(mix(wet_hash(i), wet_hash(i + vec2f(1.0, 0.0)), u.x),
               mix(wet_hash(i + vec2f(0.0, 1.0)), wet_hash(i + vec2f(1.0, 1.0)), u.x), u.y);
}
// Raindrop rings on standing water: in each cell of a grid a drop lands at
// its own place and time and a ring spreads from it, fading; the result is
// the slope the rings give the surface.
fn rain_rings(p : vec2f, t : f32) -> vec2f {
    var slope = vec2f(0.0);
    for (var k = 0; k < 2; k++) {
        let q = p * (2.6 + 1.7 * f32(k)) + vec2f(13.7, 5.1) * f32(k);
        let cell = floor(q);
        let h = wet_hash(cell + 3.1 * f32(k));
        let centre = cell + vec2f(0.25 + 0.5 * h, 0.25 + 0.5 * fract(h * 57.3));
        let age = fract(t * (0.9 + 0.5 * fract(h * 13.7)) + h * 9.0);
        let dv = q - centre;
        let dist = length(dv);
        let ring = sin((dist - age * 0.45) * 60.0) * smoothstep(0.07, 0.0, abs(dist - age * 0.45)) * (1.0 - age);
        slope += dv / max(dist, 1e-3) * ring;
    }
    return slope;
}

// The sun's shadow. Hard (one hardware-filtered tap) by default; with
// FrameSettings.SunSoftness it is PCSS (Fernando 2005): a blocker search
// finds how far the occluders are, and the filter widens with that
// distance, so a shadow is crisp where it touches and soft far from what
// casts it. The receiver is pushed along its normal by a texel or two
// instead of a slope bias from screen derivatives (which a branchy
// fragment stage may not take).
fn shadow_factor(world : vec3f, n : vec3f, n_dot_l : f32, frag : vec2f) -> f32 {
    let soft = frame.misc.z;
    let texel_w = frame.misc.w;
    let wp = world + n * texel_w * (1.2 + 2.0 * (1.0 - n_dot_l)) * select(0.0, 1.0, soft > 0.0);
    let sc = frame.light_vp * vec4f(wp, 1.0);
    let uv = vec2f(sc.x * 0.5 + 0.5, -sc.y * 0.5 + 0.5);
    if (any(uv < vec2f(0.0)) || any(uv > vec2f(1.0))) {
        return 1.0;
    }
    let bias = clamp(0.002 * (1.0 - n_dot_l) + 0.0006, 0.0005, 0.004);
    if (soft <= 0.0) {
        return textureSampleCompareLevel(shadow_tex, shadow_samp, uv, sc.z - bias);
    }
    let dims = vec2f(textureDimensions(shadow_tex));
    let zr = sc.z - 0.0008;
    // Receiver-plane bias: across a wide kernel the receiver's own depth
    // changes, and comparing every tap against the centre's depth shadows
    // a slanted surface with itself. The plane's depth gradient in shadow
    // uv follows from the normal and the (affine) light matrix: a normal
    // transforms by the inverse transpose, which for rotation times
    // scale is the matrix itself over the squared row lengths.
    let lm = mat3x3f(frame.light_vp[0].xyz, frame.light_vp[1].xyz, frame.light_vp[2].xyz);
    let lt = transpose(lm);
    let nc = (lm * n) / vec3f(dot(lt[0], lt[0]), dot(lt[1], lt[1]), dot(lt[2], lt[2]));
    var dz = vec2f(0.0);
    if (abs(nc.z) > 1e-6) { dz = vec2f(-2.0 * nc.x, 2.0 * nc.y) / nc.z; }
    let max_slope = 0.004 * dims.x;      // never follow a plane steeper than this
    dz = clamp(dz, vec2f(-max_slope), vec2f(max_slope));
    // A texel of that slope as bias: the map stores one depth per texel,
    // and a tap can sit anywhere inside it.
    let zb = zr - (abs(dz.x) + abs(dz.y)) * 0.9 / dims.x;
    let phi = sh_ign(frag) * 6.2831853;
    // Blocker search: the average depth of what stands between here and the sun.
    let search = clamp(soft * 0.08, 2.0 / dims.x, 0.02);
    var blk = 0.0;
    var nb = 0.0;
    for (var i = 0; i < 8; i++) {
        let o = sh_vogel(i, 8, phi) * search;
        let t = vec2i(clamp((uv + o) * dims, vec2f(0.0), dims - 1.0));
        let d = textureLoad(shadow_tex, t, 0);
        // The plane at the centre of the texel read, not at the tap.
        let oc = (vec2f(t) + 0.5) / dims - uv;
        if (d < zb + dot(oc, dz)) { blk += d; nb += 1.0; }
    }
    if (nb < 0.5) {
        return 1.0;
    }
    // Penumbra: wider the further the blockers are.
    let pen = clamp((zr - blk / nb) * soft, 1.25 / dims.x, search);
    var lit = 0.0;
    for (var i = 0; i < 12; i++) {
        let o = sh_vogel(i, 12, phi + 1.7) * pen;
        lit += textureSampleCompareLevel(shadow_tex, shadow_samp, uv + o, zb + dot(o, dz));
    }
    lit /= 12.0;
    // Fade to lit at the map's edge, so its border never shows.
    let e = min(uv, 1.0 - uv);
    return mix(1.0, lit, smoothstep(0.0, 0.03, min(e.x, e.y)));
}

// ---- stochastic hex tiling (Heitz & Neyret / Mikkelsen) --------------------
//
// A tiled texture repeats visibly because every cell is the same stamp. The
// fix: lay a triangular lattice over the UV plane, take the three lattice
// vertices around the shading point, sample the texture once per vertex
// with that cell's own random offset AND rotation, and blend the three by
// sharpened barycentric weights. A plain weighted average would wash the
// result out (three independent samples average toward the mean, halving
// contrast), so the blend is variance preserving: deviations from the
// texture mean are summed and renormalised by sqrt(sum w^2). The mean comes
// free from the top of the mip chain, which is why material textures are
// uploaded mipmapped.

struct HexCell {
    w : vec3f,
    c1 : vec2f,
    c2 : vec2f,
    c3 : vec2f,
}

fn hex_grid(uv : vec2f) -> HexCell {
    let s = mat2x2f(1.0, 0.0, -0.57735027, 1.15470054) * (uv * 3.4641016);
    let base = floor(s);
    var t = vec3f(fract(s), 0.0);
    t.z = 1.0 - t.x - t.y;
    var o : HexCell;
    if (t.z > 0.0) {
        o.w = vec3f(t.z, t.y, t.x);
        o.c1 = base;
        o.c2 = base + vec2f(0.0, 1.0);
        o.c3 = base + vec2f(1.0, 0.0);
    } else {
        o.w = vec3f(-t.z, 1.0 - t.y, 1.0 - t.x);
        o.c1 = base + vec2f(1.0, 1.0);
        o.c2 = base + vec2f(1.0, 0.0);
        o.c3 = base + vec2f(0.0, 1.0);
    }
    return o;
}

fn hash22(p : vec2f) -> vec2f {
    let q = vec2f(dot(p, vec2f(127.1, 311.7)), dot(p, vec2f(269.5, 183.3)));
    return fract(sin(q) * 43758.5453);
}

fn hex_sample(t : texture_2d<f32>, uv : vec2f, dx : vec2f, dy : vec2f, rot_scale : f32) -> vec4f {
    let cell = hex_grid(uv);
    // The last mip level is the texture average — the blend's mean.
    let mean = textureSampleLevel(t, samp, uv, 32.0);
    var w = pow(max(cell.w, vec3f(0.0)), vec3f(7.0));
    w = w / max(w.x + w.y + w.z, 1e-6);
    var cells = array<vec2f, 3>(cell.c1, cell.c2, cell.c3);
    var ws = array<f32, 3>(w.x, w.y, w.z);
    var acc = vec4f(0.0);
    var wsq = 0.0;
    for (var i = 0; i < 3; i = i + 1) {
        let h = hash22(cells[i]);
        let ang = h.x * 6.2831853 * rot_scale;
        let cs = cos(ang);
        let sn = sin(ang);
        let rot = mat2x2f(cs, sn, -sn, cs);
        let ruv = rot * (uv - cells[i]) + cells[i] + h;
        let c = textureSampleGrad(t, samp, ruv, rot * dx, rot * dy);
        acc = acc + (c - mean) * ws[i];
        wsq = wsq + ws[i] * ws[i];
    }
    return mean + acc / sqrt(max(wsq, 1e-6));
}

fn tex_sample(t : texture_2d<f32>, uv : vec2f, dx : vec2f, dy : vec2f, anti : bool, rot_scale : f32) -> vec4f {
    if (anti) {
        return hex_sample(t, uv, dx, dy, rot_scale);
    }
    return textureSampleGrad(t, samp, uv, dx, dy);
}

// Triplanar: project the map down the three axes and blend by |n|^4, so a
// face perpendicular to an axis takes that plane almost exclusively and
// only the diagonals cross-fade.
fn tri_weights(n : vec3f) -> vec3f {
    var w = abs(n);
    w = w * w;
    w = w * w;
    return w / max(w.x + w.y + w.z, 1e-6);
}

fn tri_sample(t : texture_2d<f32>, p : vec3f, dpx : vec3f, dpy : vec3f, w : vec3f, anti : bool, rs : f32) -> vec4f {
    let cx = tex_sample(t, p.zy, dpx.zy, dpy.zy, anti, rs);
    let cy = tex_sample(t, p.xz, dpx.xz, dpy.xz, anti, rs);
    let cz = tex_sample(t, p.xy, dpx.xy, dpy.xy, anti, rs);
    return cx * w.x + cy * w.y + cz * w.z;
}

// Whiteout blend of three tangent-space normal samples into world space.
fn tri_normal(p : vec3f, dpx : vec3f, dpy : vec3f, w : vec3f, n : vec3f, anti : bool, rs : f32, scale : f32) -> vec3f {
    var tx = tex_sample(normal_tex, p.zy, dpx.zy, dpy.zy, anti, rs).xyz * 2.0 - 1.0;
    var ty = tex_sample(normal_tex, p.xz, dpx.xz, dpy.xz, anti, rs).xyz * 2.0 - 1.0;
    var tz = tex_sample(normal_tex, p.xy, dpx.xy, dpy.xy, anti, rs).xyz * 2.0 - 1.0;
    tx = vec3f(tx.xy * scale, tx.z);
    ty = vec3f(ty.xy * scale, ty.z);
    tz = vec3f(tz.xy * scale, tz.z);
    let nx = vec3f(tx.xy + n.zy, abs(tx.z) * n.x).zyx;
    let ny = vec3f(ty.xy + n.xz, abs(ty.z) * n.y).xzy;
    let nz = vec3f(tz.xy + n.xy, abs(tz.z) * n.z).xyz;
    return normalize(nx * w.x + ny * w.y + nz * w.z);
}

// Cotangent frame from screen-space derivatives — the fallback when a mesh
// carries no tangents (a UV-less primitive, procedural geometry).
fn cotangent_frame(n : vec3f, dpx : vec3f, dpy : vec3f, duvx : vec2f, duvy : vec2f) -> mat3x3f {
    let p2 = cross(dpy, n);
    let p1 = cross(n, dpx);
    let t = p2 * duvx.x + p1 * duvy.x;
    let b = p2 * duvx.y + p1 * duvy.y;
    let inv = inverseSqrt(max(max(dot(t, t), dot(b, b)), 1e-12));
    return mat3x3f(t * inv, b * inv, n);
}

// ---- shading ---------------------------------------------------------------

fn brdf(n : vec3f, v : vec3f, l : vec3f, albedo : vec3f, metallic : f32, roughness : f32) -> vec3f {
    let h = normalize(l + v);
    let ndl = max(dot(n, l), 0.0);
    let ndv = max(dot(n, v), 1e-4);
    let ndh = max(dot(n, h), 0.0);
    let hdv = max(dot(h, v), 0.0);
    let a = roughness * roughness;
    let a2 = a * a;
    let den = ndh * ndh * (a2 - 1.0) + 1.0;
    let ndf = a2 / (PI * den * den);
    let k = (roughness + 1.0) * (roughness + 1.0) / 8.0;
    let g = (ndv / (ndv * (1.0 - k) + k)) * (ndl / (ndl * (1.0 - k) + k));
    let f0 = mix(vec3f(0.04), albedo, metallic);
    let f = f0 + (vec3f(1.0) - f0) * pow(1.0 - hdv, 5.0);
    let spec = ndf * g * f / (4.0 * ndv * ndl + 1e-4);
    let kd = (vec3f(1.0) - f) * (1.0 - metallic);
    return (kd * albedo / PI + spec) * ndl;
}

fn sky_color(dir : vec3f) -> vec3f {
    let t = pow(clamp(dir.y, 0.0, 1.0), 0.5);
    return mix(frame.sky_horizon.rgb, frame.sky_top.rgb, t);
}

// The environment a mirror direction sees: the sky's gradient with the
// sun's glow in it above the horizon, the dim ground below.
fn sky_env(dir : vec3f, to_sun : vec3f) -> vec3f {
    var c = sky_color(dir);
    c = c + frame.sun.rgb * 0.10 * pow(max(dot(dir, to_sun), 0.0), 10.0);
    return mix(frame.sky_horizon.rgb * 0.25, c, smoothstep(-0.15, 0.05, dir.y));
}

// Exponential height fog: density falls off with world height, the optical
// depth is taken at the midpoint of the view ray, and the fog colour may be
// blended toward the sky gradient (aerial perspective).
fn apply_fog(c : vec3f, world : vec3f) -> vec3f {
    if (frame.fog_params.w < 0.5) {
        return c;
    }
    let to = world - frame.cam_pos.xyz;
    let dist = length(to);
    let mid = (world.y + frame.cam_pos.y) * 0.5 - frame.fog_params.x;
    let hf = exp(-clamp(mid * frame.fog_params.y, -20.0, 20.0));
    let f = 1.0 - exp(-dist * frame.fog_color.w * hf);
    let dir = normalize(to + vec3f(0.0, 1e-6, 0.0));
    let fc = mix(frame.fog_color.rgb, sky_color(dir), clamp(frame.fog_params.z, 0.0, 1.0));
    return mix(c, fc, clamp(f, 0.0, 1.0));
}

fn uncharted2(x : vec3f) -> vec3f {
    let A = 0.15; let B = 0.50; let C = 0.10;
    let D = 0.20; let E = 0.02; let F = 0.30;
    return ((x * (A * x + C * B) + D * E) / (x * (A * x + B) + D * F)) - E / F;
}

fn tonemap(c : vec3f) -> vec3f {
    let mode = u32(frame.misc.x);
    if (mode == 1u) {
        return clamp(uncharted2(c * 2.0) / uncharted2(vec3f(11.2)), vec3f(0.0), vec3f(1.0));
    }
    if (mode == 2u) {
        // ACES filmic approximation (Narkowicz).
        let a = 2.51; let b = 0.03; let cc = 2.43; let d = 0.59; let e = 0.14;
        return clamp((c * (a * c + b)) / (c * (cc * c + d) + e), vec3f(0.0), vec3f(1.0));
    }
    return clamp(c, vec3f(0.0), vec3f(1.0));
}

@fragment
fn fs(in : FSIn) -> @location(0) vec4f {
    let d = draws[in.draw_id];
    let flags = u32(d.p1.x);
    let anti = (flags & F_ANTITILE) != 0u;
    let rot_scale = d.p2.x;
    let uvs = d.p0.w;

    // Derivatives must be taken in uniform control flow, so everything the
    // sampling paths need is computed up front and passed down explicitly
    // (all texture reads below are Grad/Level).
    let uv = in.uv * uvs;
    let duvx = dpdx(uv);
    let duvy = dpdy(uv);
    let wp = in.world * uvs;
    let dpx = dpdx(wp);
    let dpy = dpdy(wp);
    let dwx = dpdx(in.world);
    let dwy = dpdy(in.world);

    var n = normalize(in.normal);
    if (!in.front) {
        n = -n;
    }
    let tw = tri_weights(n);
    let triplanar = (flags & F_TRIPLANAR) != 0u;

    var texel : vec4f;
    var arm : vec4f;
    var emis : vec4f;
    var shaded_n = n;
    if (triplanar) {
        texel = tri_sample(albedo_tex, wp, dpx, dpy, tw, anti, rot_scale);
        arm = tri_sample(arm_tex, wp, dpx, dpy, tw, anti, rot_scale);
        emis = tri_sample(emis_tex, wp, dpx, dpy, tw, anti, rot_scale);
        if ((flags & F_NORMAL) != 0u) {
            shaded_n = tri_normal(wp, dpx, dpy, tw, n, anti, rot_scale, d.p0.z);
        }
    } else {
        texel = tex_sample(albedo_tex, uv, duvx, duvy, anti, rot_scale);
        arm = tex_sample(arm_tex, uv, duvx, duvy, anti, rot_scale);
        emis = tex_sample(emis_tex, uv, duvx, duvy, anti, rot_scale);
        if ((flags & F_NORMAL) != 0u) {
            var tn = tex_sample(normal_tex, uv, duvx, duvy, anti, rot_scale).xyz * 2.0 - 1.0;
            tn = vec3f(tn.xy * d.p0.z, tn.z);
            var tbn : mat3x3f;
            if (length(in.tangent.xyz) > 1e-4) {
                let t = normalize(in.tangent.xyz - n * dot(n, in.tangent.xyz));
                let b = cross(n, t) * select(-1.0, 1.0, in.tangent.w >= 0.0);
                tbn = mat3x3f(t, b, n);
            } else {
                tbn = cotangent_frame(n, dwx, dwy, duvx, duvy);
            }
            shaded_n = normalize(tbn * tn);
        }
    }

    // Water: the normal is two layers of the wave map, scrolling across each
    // other in the horizontal plane, so the surface moves and never repeats
    // the same crest twice.
    if ((flags & F_WATER) != 0u) {
        let t = frame.env.y;
        let q = in.world.xz * uvs;
        let gx = dwx.xz * uvs;
        let gy = dwy.xz * uvs;
        let w1 = textureSampleGrad(normal_tex, samp, q + vec2f(0.021, 0.013) * t, gx, gy).xyz * 2.0 - 1.0;
        let w2 = textureSampleGrad(normal_tex, samp, q * 2.3 + vec2f(-0.017, 0.031) * t, gx * 2.3, gy * 2.3).xyz * 2.0 - 1.0;
        let w = (w1.xy + w2.xy * 0.6) * d.p0.z;
        shaded_n = normalize(vec3f(w.x, 1.0, w.y));
    }

    let alpha = d.color.a * d.p1.z * texel.a;
    if (d.p1.z >= 1.0 && alpha < 0.5) {
        discard; // alpha cutout for opaque materials (foliage, grates)
    }

    if ((flags & F_UNLIT) != 0u) {
        if (u32(frame.misc.x) == 3u) {
            // HDR: an unlit surface is a light source — its colour as linear
            // radiance, times Emission (so lamps and signs can bloom).
            let e = select(1.0, d.p1.y, d.p1.y > 0.0);
            return vec4f(pow(max(d.color.rgb * texel.rgb, vec3f(0.0)), vec3f(2.2)) * e, alpha);
        }
        return vec4f(d.color.rgb * texel.rgb, alpha);
    }

    // Base colour maps are sRGB-encoded; shade in linear space.
    var albedo = d.color.rgb * pow(max(texel.rgb, vec3f(0.0)), vec3f(2.2));

    var ao = 1.0;
    var rough_tex = 1.0;
    var metal_tex = 1.0;
    if ((flags & F_ARM) != 0u) {
        ao = arm.r;
        rough_tex = arm.g;
        metal_tex = arm.b;
    } else if ((flags & F_ROUGH) != 0u) {
        let ch = u32(d.p1.w);
        rough_tex = select(select(arm.r, arm.g, ch == 1u), arm.b, ch == 2u);
    }
    var metallic = clamp(d.p0.x * metal_tex, 0.0, 1.0);
    var roughness = clamp(d.p0.y * rough_tex, 0.05, 1.0);

    // Wet: everything damp darkens and glosses over (more where rain lands,
    // on what faces up); on level ground a broad noise gathers standing
    // water into puddles, which are smooth, dark, flat mirrors, ringed by
    // raindrops when it rains. ripples tells Post to bend this pixel's
    // mirror image (the sea, and puddles in the rain).
    var ripples = (flags & F_WATER) != 0u;
    let wet = frame.env.w;
    if (wet > 0.0 && (flags & F_WATER) == 0u) {
        let up = clamp(n.y, 0.0, 1.0);
        let film = wet * (0.5 + 0.5 * up);
        let pn = wet_noise(in.world.xz * 0.13) * 0.6 + wet_noise(in.world.xz * 0.41 + vec2f(7.3, 2.9)) * 0.4;
        let level = 0.22 + 0.30 * wet;
        let puddle = smoothstep(level, level - 0.07, pn) * smoothstep(0.92, 0.99, up);
        albedo = albedo * mix(1.0, 0.55, film * (1.0 - metallic));
        roughness = mix(roughness, max(roughness * 0.4, 0.1), film);
        albedo = albedo * mix(1.0, 0.5, puddle);
        roughness = mix(roughness, 0.05, puddle);
        metallic = mix(metallic, 0.0, puddle);
        shaded_n = normalize(mix(shaded_n, n, puddle));
        let rain = frame.sun.w;
        if (rain > 0.0 && puddle > 0.0) {
            let rs = rain_rings(in.world.xz, frame.env.y) * 0.5 * rain * puddle;
            shaded_n = normalize(shaded_n + vec3f(rs.x, 0.0, rs.y));
            ripples = puddle > 0.5;
        }
    }

    let v = normalize(frame.cam_pos.xyz - in.world);
    let l = normalize(-frame.light_dir.xyz);
    let ndl = max(dot(shaded_n, l), 0.0);
    let sun = max(frame.sun.rgb, vec3f(0.0));
    var lo = brdf(shaded_n, v, l, albedo, metallic, roughness) * sun * shadow_factor(in.world, shaded_n, ndl, in.pos.xy);

    let count = u32(frame.misc.y);
    for (var i = 0u; i < count; i = i + 1u) {
        let li = lights.items[i];
        let dv = li.pos_range.xyz - in.world;
        let dist = length(dv);
        if (dist >= li.pos_range.w) {
            continue;
        }
        let ld = dv / max(dist, 1e-4);
        // Godot-style windowed inverse-square: a smooth cutoff at Range
        // multiplying a bounded 1/(1+d^2) falloff.
        let nd = dist / li.pos_range.w;
        var win = max(1.0 - nd * nd * nd * nd, 0.0);
        win = win * win;
        var att = win / (1.0 + dist * dist);
        if (li.color.w > 0.5) {
            let cd = dot(-ld, li.dir_cos.xyz);
            att = att * smoothstep(li.dir_cos.w, li.params.x, cd);
        }
        if (att <= 0.0) {
            continue;
        }
        if (li.params.y > 0.5) {
            att = att * spot_shadow(li, in.world, shaded_n, dist, in.pos.xy);
        }
        lo = lo + brdf(shaded_n, v, ld, albedo, metallic, roughness) * li.color.rgb * att;
    }

    var c = albedo * frame.ambient.rgb * ao + lo;
    var mirror = 0.0; // how much of a sharp reflection this pixel shows
    // The sky reflected: what the surface mirrors of the environment, by
    // Fresnel (strong at grazing angles and on metal), sharp when glossy
    // and smeared toward the sky's average when rough, dimmed in crevices.
    if (frame.env.x > 0.0) {
        let nv = max(dot(shaded_n, v), 0.0);
        let f0 = mix(vec3f(0.04), albedo, metallic);
        let fres = f0 + (max(vec3f(1.0 - roughness), f0) - f0) * pow(1.0 - nv, 5.0);
        let r = reflect(-v, shaded_n);
        let blur = roughness * roughness;
        let avg = (frame.sky_top.rgb + frame.sky_horizon.rgb * 2.0) * (1.0 / 3.0) * 0.8;
        let env_c = mix(sky_env(r, l), avg, clamp(blur * 1.6, 0.0, 1.0));
        c = c + env_c * fres * ao * frame.env.x * (1.0 - 0.5 * blur);
        mirror = dot(fres, vec3f(1.0 / 3.0)) * ao * frame.env.x * (1.0 - 0.5 * blur) * (1.0 - clamp(blur * 1.6, 0.0, 1.0));
    }
    if ((flags & F_EMISSIVE) != 0u) {
        c = c + pow(max(emis.rgb, vec3f(0.0)), vec3f(2.2)) * d.p1.y;
    } else if (d.p1.y > 0.0) {
        c = c + albedo * d.p1.y;
    }
    c = apply_fog(c, in.world);
    if (u32(frame.misc.x) == 3u) {
        // HDR: linear radiance out, for render.Post to expose, tone map and encode.
        // With scene reflections on, an opaque pixel's alpha says how mirror-like
        // it is (1 = not at all), for Post to trace.
        var a = alpha;
        if (frame.env.z > 0.0 && alpha >= 0.999) {
            a = 1.0 - clamp(mirror * frame.env.z, 0.0, 0.98);
            if (ripples) { a = -a; } // a negative alpha: a mirror that ripples
        }
        return vec4f(max(c * frame.ambient.w, vec3f(0.0)), a);
    }
    c = tonemap(c * frame.ambient.w);
    c = pow(max(c, vec3f(0.0)), vec3f(1.0 / 2.2));
    return vec4f(c, alpha);
}` + skinCommonWGSL + `
// Skinned twin of vs (GPU skinning, gpuskin.go): the joint palette deforms
// position, normal AND tangent in mesh space, then the per-draw model
// matrix places the result. Everything downstream — normal mapping,
// triplanar, lights, fog — is the shared fragment stage.
@vertex
fn vs_skin(@location(0) pos : vec3f, @location(1) normal : vec3f, @location(2) uv : vec2f,
           @location(3) tangent : vec4f,
           @location(4) joints : vec4<u32>, @location(5) weights : vec4f,
           @builtin(instance_index) iid : u32) -> VSOut {
    let s = skin_matrix(joints, weights);
    let m = draws[iid].model;
    let world = m * vec4f((s * vec4f(pos, 1.0)).xyz, 1.0);
    var out : VSOut;
    out.pos = frame.view_proj * world;
    out.world = world.xyz;
    out.normal = (m * vec4f(skin_dir((s * vec4f(normal, 0.0)).xyz), 0.0)).xyz;
    out.uv = uv;
    out.tangent = vec4f((m * vec4f(skin_dir((s * vec4f(tangent.xyz, 0.0)).xyz), 0.0)).xyz, tangent.w);
    out.draw_id = iid;
    return out;
}`
