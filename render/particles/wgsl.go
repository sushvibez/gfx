package particles

// Shade is the WGSL of the particle pass: camera-facing quads of the
// smoke atlas, drawn after every opaque mesh with premultiplied alpha (rgb
// is the radiance a particle adds, alpha how much of what is behind it it
// covers), tested against the scene's depth without writing it.
//
// A puff is one frame of the atlas (SmokeAtlas: billowed lobes, normal in
// RG, thickness in B, density in A), picked and turned by its seed; its
// opacity is the density cut at a threshold that rises with its age, so an
// old puff breaks into wisps rather than only fading. It is shaded by the
// billows' normal bent toward a sphere's: wrapped sunlight, light through
// its thin parts looking toward the sun, sky from above, the ground's
// bounce from below, and the point lights at its centre. A puff with Emit
// is a flame lobe (a hot core, a dim skin, soot over its edges), blended,
// not added, so smoke in front hides it. A chip is a hard irregular flake.
// Under a pixel and a half a particle is drawn at that size and fainter.
//
// Entry points: vs_particle, and fs_particle (the hardware depth test) or
// fs_particle_soft (no depth attachment: each fragment reads the stored
// scene depth itself, is hidden where the scene is nearer and fades over
// the last clamp(0.4·diameter, 0.04, 0.7) m before it).
//
// Group 1 is the pass's own: binding 0 the particles (Part, 64 B each:
// camera-relative centre and radius, albedo and opacity, emit, turn, kind,
// seed, age), 8 the atlas, 9 its sampler, 10 the scene's depth (soft only).
//
// The renderer supplies, in its own module (group 0 is its own):
//
//	fn pt_eye() -> PtEye                  camera right and up (world), the sun's direction, a pixel's angle
//	fn pt_clip(rel : vec3f) -> vec4f      clip position of a camera-relative point, in its own depth convention (w = view depth)
//	fn pt_light(rel : vec3f, radius : f32) -> PtLight   the light at a particle's centre and the air in front of it
//	fn pt_view_dist(d : f32) -> f32       the view distance of a stored depth value (log or perspective)
//	fn pt_display(c : vec3f) -> vec3f     linear radiance to what the target holds (exposure, or a tone curve)
//
// Needs wgsl.Depth.
const Shade = `
// ---- gfx/render/particles: Shade ----------------------------------------
struct Part {
    pos  : vec4f,        // camera-relative centre, w: radius (m)
    col  : vec4f,        // albedo, w: opacity
    misc : vec4f,        // emit, rotation (rad), kind (0 puff, 1 chip), seed
    ext  : vec4f,        // x: age 0..1
}
// What the renderer says about its camera.
struct PtEye {
    right : vec3f,       // the camera's right (world, unit)
    up : vec3f,          // its up
    sun : vec3f,         // toward the sun (unit)
    px : f32,            // a pixel's angle (rad)
}
// The light at a particle's centre, and the air between it and the eye
// (the colour seen is c · tr + insc).
struct PtLight {
    sun : vec3f,         // the sun reaching it (after shadows and cloud)
    sky : vec3f,         // skylight from above
    gnd : vec3f,         // the ground's bounce from below
    plit : vec3f,        // the point lights at its centre
    up : vec3f,          // the local vertical
    tr : vec3f,          // the air's transmittance to it
    insc : vec3f,        // what the air adds over that distance
}
@group(1) @binding(0) var<storage, read> parts : array<Part>;
@group(1) @binding(8) var smoke_atlas : texture_2d<f32>;     // SmokeAtlas: RG normal, B thickness, A density
@group(1) @binding(9) var smoke_samp : sampler;
@group(1) @binding(10) var scene_depth : texture_depth_2d;   // the main pass's (fs_particle_soft)

const SMOKE_COLS : f32 = 4.0;
const SMOKE_ROWS : f32 = 2.0;

// The corner of a two-triangle quad, -1..1.
fn pt_corner(vid : u32) -> vec2f {
    let x = f32((vid == 1u) || (vid == 2u) || (vid == 4u));
    let y = f32((vid == 2u) || (vid == 4u) || (vid == 5u));
    return vec2f(x, y) * 2.0 - 1.0;
}

struct PartOut {
    @builtin(position) pos : vec4f,
    @location(0) uv : vec4f,                           // xy: corner -1..1, z: view depth (m)
    @location(1) @interpolate(flat) sun_col : vec3f,
    @location(2) @interpolate(flat) sky : vec3f,
    @location(3) @interpolate(flat) albedo : vec4f,    // a: opacity (scaled for sub-pixel size)
    @location(4) @interpolate(flat) tr : vec3f,
    @location(5) @interpolate(flat) insc : vec3f,
    @location(6) @interpolate(flat) misc : vec4f,
    @location(7) @interpolate(flat) view : vec3f,      // toward the eye
    @location(8) @interpolate(flat) tx : vec3f,        // the sprite's x axis (world)
    @location(9) @interpolate(flat) ty : vec3f,        // its y axis
    @location(10) @interpolate(flat) gnd : vec3f,
    @location(11) @interpolate(flat) up : vec3f,
    @location(12) @interpolate(flat) ext : vec4f,      // x: age, y: atlas frame, z: soft depth (m)
    @location(13) @interpolate(flat) plit : vec3f,
}

@vertex
fn vs_particle(@builtin(vertex_index) vid : u32, @builtin(instance_index) iid : u32) -> PartOut {
    let p = parts[iid];
    let c = pt_corner(vid);
    let rel = p.pos.xyz;
    let dist = max(length(rel), 1e-3);
    let view = -rel / dist;
    let eye = pt_eye();
    // Never under a pixel and a half: below that the particle is drawn
    // at that size and fainter, so a far puff thins rather than flickers.
    let min_r = dist * eye.px * 1.5;
    let rad = max(p.pos.w, min_r);
    let cover = min(p.pos.w / rad, 1.0);
    let cr = cos(p.misc.y);
    let sr = sin(p.misc.y);
    let ax = eye.right * cr + eye.up * sr;
    let ay = eye.up * cr - eye.right * sr;
    let clip = pt_clip(rel + (ax * c.x + ay * c.y) * rad);
    var out : PartOut;
    out.uv = vec4f(c, clip.w, 0.0);
    out.pos = clip;
    let l = pt_light(rel, p.pos.w);
    out.sun_col = l.sun;
    out.sky = l.sky;
    out.gnd = l.gnd;
    out.plit = l.plit;
    out.up = l.up;
    out.tr = l.tr;
    out.insc = l.insc;
    out.albedo = vec4f(p.col.rgb, p.col.a * cover);
    out.misc = p.misc;
    out.view = view;
    out.tx = ax;
    out.ty = ay;
    // Its own frame of the atlas; soft over 0.4 of its size (diameter).
    let fr = min(floor(fract(p.misc.w * 7.31) * SMOKE_COLS * SMOKE_ROWS), SMOKE_COLS * SMOKE_ROWS - 1.0);
    out.ext = vec4f(p.ext.x, fr, clamp(p.pos.w * 0.8, 0.04, 0.7), 0.0);
    return out;
}

// part_shade is a particle's premultiplied colour, its opacity scaled by
// soft (how far in front of the scene it is, 0..1).
fn part_shade(in : PartOut, soft : f32) -> vec4f {
    let c = in.uv.xy;
    // The atlas first, in uniform control flow (it needs derivatives).
    let cell = vec2f(in.ext.y % SMOKE_COLS, floor(in.ext.y / SMOKE_COLS));
    let tuv = (cell + clamp(vec2f(c.x, -c.y) * 0.5 + 0.5, vec2f(0.0), vec2f(1.0))) / vec2f(SMOKE_COLS, SMOKE_ROWS);
    let t = textureSample(smoke_atlas, smoke_samp, tuv);
    let r2 = dot(c, c);
    let sun = pt_eye().sun;
    var a = 0.0;
    var col = vec3f(0.0);
    if (in.misc.z < 0.5) {
        // A puff: its density cut at a threshold that rises with age, so
        // it erodes into wisps; a flame lobe's edge is crisper.
        let hot = in.misc.x > 0.0;
        let er = in.ext.x * 0.45;
        a = smoothstep(er, er + select(0.6, 0.38, hot), t.a) * in.albedo.a;
        // Shaded by the billows' normal bent toward a ball's.
        let V = in.view;
        let tn = t.rg * 2.0 - 1.0;
        let Ns = in.tx * c.x + in.ty * c.y + V * (sqrt(max(1.0 - r2, 0.0)) * 0.9 + 0.05);
        let Nb = in.tx * tn.x + in.ty * tn.y + V * sqrt(max(1.0 - dot(tn, tn), 0.0));
        let N = normalize(Ns * 0.55 + Nb);
        let wrap = clamp((dot(N, sun) + 0.45) / 1.45, 0.0, 1.0);
        // Looking toward the sun, light comes through where it is thin.
        let back = pow(clamp(dot(-V, sun), 0.0, 1.0), 5.0);
        let thin = 1.0 - t.b * smoothstep(0.0, 0.5, a);
        let sunl = in.sun_col * (wrap * wrap * 0.85 + back * (0.15 + 1.3 * thin));
        let amb = mix(in.gnd, in.sky, dot(N, in.up) * 0.5 + 0.5) * (1.0 - 0.35 * t.b);
        col = in.albedo.rgb * (amb + sunl + in.plit * (1.0 - 0.3 * t.b));
        if (hot) {
            // A flame lobe: a hot dense core, a dimmer lumpy skin, soot
            // over the rest.
            let lump = clamp(dot(tn, vec2f(0.6, 0.8)) * 0.6 + 0.5, 0.0, 1.0);
            let core = t.b * t.b * smoothstep(0.15, 0.9, t.a);
            let hue = mix(in.albedo.rgb, vec3f(1.0, 0.8, 0.5), core * 0.7);
            let fire = hue * in.misc.x * 0.4 * (0.10 + 1.7 * core) * (0.45 + 1.1 * lump);
            col = vec3f(0.045, 0.042, 0.04) * (amb + sunl + in.plit) + fire;
        }
    } else {
        // A chip: a hard, irregular flake.
        if (r2 >= 1.0) { discard; }
        let k = c * vec2f(1.0, 1.7);
        let sh = abs(k.x) + abs(k.y) * (0.7 + 0.6 * fract(in.misc.w * 17.0));
        a = in.albedo.a * step(sh, 0.9);
        col = in.albedo.rgb * (in.sun_col * 0.6 + mix(in.gnd, in.sky, 0.65) + in.plit) + in.albedo.rgb * in.misc.x;
    }
    // Soft against the scene, and gone right at the lens.
    a *= soft * smoothstep(0.1, 0.5, in.uv.z);
    if (a < 0.003) { discard; }
    col = col * in.tr + in.insc;
    return vec4f(pt_display(col) * a, a);
}

// The view distance of what the scene drew at a pixel.
fn pt_scene_dist(px : vec4f) -> f32 {
    return pt_view_dist(textureLoad(scene_depth, vec2i(px.xy), 0));
}

@fragment
fn fs_particle(in : PartOut) -> @location(0) vec4f {
    return part_shade(in, 1.0);
}

@fragment
fn fs_particle_soft(in : PartOut) -> @location(0) vec4f {
    return part_shade(in, soft_fade(pt_scene_dist(in.pos), in.uv.z, in.ext.z));
}
`

// Precip is precipitation as drops in the world, with nothing stored:
// drop i's place is a function of i and the time (a hash for where it
// starts, falling at its own speed with the wind), wrapped into a box of
// air round the camera, so the drops hold still as the camera moves
// through them. A raindrop is a streak as long as a shutter draws it along
// its velocity relative to the camera, as wide as a drop or a pixel (then
// fainter: far rain is a veil, not wires); a flake is a soft dot that
// wanders on its own eddies as it falls. The renderer places, lights and
// stops the drops (a roof, a rain map) in its own vertex stage. Needs
// wgsl.Hash.
const Precip = `
// ---- gfx/render/particles: Precip ---------------------------------------
// A drop's velocity (m/s, in the box's frame: y up) for its hash h: rain
// falls at 7-10 m/s, a flake at about 1, both carried by wind (a flake
// drifts with it a little less steadily).
fn precip_velocity(h : vec4f, wind : vec3f, snow : bool) -> vec3f {
    if (snow) {
        return wind * (0.8 + 0.3 * h.w) + vec3f(0.0, -(0.8 + 0.6 * h.w), 0.0);
    }
    return wind * (0.85 + 0.25 * h.w) + vec3f(0.0, -(7.0 + 3.0 * h.w), 0.0);
}
// A flake's wander on its own eddies (m), for its hash and the time.
fn precip_wobble(h : vec4f, t : f32) -> vec3f {
    let ph = h.xyz * 40.0;
    return vec3f(sin(t * (0.9 + h.w) + ph.x), 0.3 * sin(t * 1.7 + ph.y), cos(t * (0.7 + 0.5 * h.x) + ph.z)) * 0.35;
}
// Where a drop is now, relative to the camera, wrapped into the box of
// air round it: p is its unwrapped place (its start plus its fall), cam
// the camera in the same frame, c0 the box's offset from the eye in boxes
// (a box a quarter above the eye fills the air over the ground).
fn precip_wrap(p : vec3f, cam : vec3f, box : vec3f, c0 : vec3f) -> vec3f {
    return (fract((p - cam) / box + 0.5 - c0) - 0.5 + c0) * box;
}
// The fade toward the box's faces, so no drop pops as it wraps.
fn precip_edge(l : vec3f, box : vec3f, c0 : vec3f) -> f32 {
    let e = (0.5 - abs(l / box - c0)) * 8.0;
    return clamp(min(e.x, min(e.y, e.z)), 0.0, 1.0);
}
// A streak's opacity across its quad (uv -1..1, y along the fall: +1 the
// tail): soft across, tapering toward its tail from tail0, cut at its head
// over head0..-1.
fn precip_streak(uv : vec2f, tail0 : f32, head0 : f32) -> f32 {
    return (1.0 - uv.x * uv.x) * smoothstep(1.0, tail0, uv.y) * smoothstep(-1.0, head0, uv.y);
}
// A flake's opacity: a soft dot.
fn precip_flake(uv : vec2f) -> f32 {
    return smoothstep(1.0, 0.35, length(uv)) * 0.9;
}
`
