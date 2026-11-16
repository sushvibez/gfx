package wgsl

// Hash is the per-pixel and per-lattice randomness: interleaved gradient
// noise and the Vogel disc for rotated sample kernels, a 3D lattice hash,
// a sin hash over the plane, and four random numbers from an index (a
// particle's or a drop's). No dependencies.
const Hash = `
// ---- gfx/render/wgsl: Hash ---------------------------------------------
// Interleaved gradient noise (Jimenez): a per-pixel rotation for sample
// kernels, so undersampling shows as fine grain, not bands. A renderer with
// a temporal filter behind it offsets p by a per-frame amount.
fn ign(p : vec2f) -> f32 { return fract(52.9829189 * fract(dot(p, vec2f(0.06711056, 0.00583715)))); }
// Vogel disc: n points spread evenly over the unit disc, rotated by phi.
fn vogel(i : i32, n : i32, phi : f32) -> vec2f {
    let r = sqrt((f32(i) + 0.5) / f32(n));
    let th = f32(i) * 2.39996323 + phi;
    return r * vec2f(cos(th), sin(th));
}
// A lattice hash in 0..1 (Hoskins' hash without sine).
fn hash13(p : vec3f) -> f32 {
    var q = fract(p * vec3f(0.1031, 0.1030, 0.0973));
    q += dot(q, q.yxz + 33.33);
    return fract((q.x + q.y) * q.z);
}
// The classic sin hash over the plane, in 0..1 (cheap; fine for cells a
// few hundred units from the origin).
fn hash12s(p : vec2f) -> f32 { return fract(sin(dot(p, vec2f(127.1, 311.7))) * 43758.5453); }
// Four independent numbers in 0..1 from an index (PCG-style mixing): a
// particle's or a drop's place, speed and look, with nothing stored.
fn hash4u(i : u32) -> vec4f {
    var s = vec4<u32>(i * 747796405u + 2891336453u, i * 2246822519u + 3266489917u,
                      i * 668265263u + 374761393u, i * 1597334677u + 3812015801u);
    s = (s ^ (s >> vec4<u32>(15u))) * vec4<u32>(2246822519u);
    s = (s ^ (s >> vec4<u32>(13u))) * vec4<u32>(3266489917u);
    s = s ^ (s >> vec4<u32>(16u));
    return vec4f(s) * (1.0 / 4294967296.0);
}
`

// Noise is value noise and fbm in three dimensions, with the octaves faded
// against the pixel's footprint so nothing finer than a few pixels
// shimmers. The lattice wraps every NOISE_PERIOD units, so a planet's
// material coordinates (kept modulo that period) stay seamless. Needs Hash.
const Noise = `
// ---- gfx/render/wgsl: Noise --------------------------------------------
const NOISE_PERIOD : f32 = 131072.0;
// Value noise with a C2 (quintic) fade: the cubic fade's gradient kinks on
// every lattice plane, and a finite-difference bump turns those kinks into
// scratch lines. period is the lattice period (NOISE_PERIOD / wavelength)
// so the wrap is seamless.
fn vnoise(p : vec3f, period : f32) -> f32 {
    var i = floor(p);
    let f = fract(p);
    i = i - period * floor(i / period);
    let u = f * f * f * (f * (f * 6.0 - 15.0) + 10.0);
    let i1 = i + vec3f(1.0, 0.0, 0.0) - period * step(vec3f(period), i + vec3f(1.0, 0.0, 0.0));
    let ix = i1.x;
    let iy = (i.y + 1.0) - period * step(period, i.y + 1.0);
    let iz = (i.z + 1.0) - period * step(period, i.z + 1.0);
    let a = mix(hash13(vec3f(i.x, i.y, i.z)), hash13(vec3f(ix, i.y, i.z)), u.x);
    let b = mix(hash13(vec3f(i.x, iy, i.z)), hash13(vec3f(ix, iy, i.z)), u.x);
    let c = mix(hash13(vec3f(i.x, i.y, iz)), hash13(vec3f(ix, i.y, iz)), u.x);
    let d = mix(hash13(vec3f(i.x, iy, iz)), hash13(vec3f(ix, iy, iz)), u.x);
    return mix(mix(a, b, u.y), mix(c, d, u.y), u.z);
}
// A fixed rotation applied between octaves so lattice planes never align
// across scales.
fn octave_rotate(v : vec3f) -> vec3f {
    let c = 0.76484219; let s = 0.64421769;
    let axis = vec3f(0.36, 0.48, 0.80);
    return v * c + cross(axis, v) * s + axis * (dot(axis, v) * (1.0 - c));
}
// fbm in -1..1 with per-octave fade against the pixel footprint (the size
// of a pixel at p, in p's units): no shimmer.
fn fbm(p : vec3f, wavelength : f32, footprint : f32, octaves : i32) -> f32 {
    var sum = 0.0;
    var amp = 0.5;
    var wl = wavelength;
    var q = p;
    for (var i = 0; i < octaves; i++) {
        // Octaves finer than ~4 pixels contribute nothing but aliasing.
        let w = 1.0 - smoothstep(0.25 * wl, 0.6 * wl, footprint);
        if (w <= 0.0) { break; }
        sum += amp * w * (vnoise(q / wl, NOISE_PERIOD / wl) * 2.0 - 1.0);
        // Rotation keeps the period only approximately; the wrap seam is
        // sub-pixel at these amplitudes.
        q = octave_rotate(q) + vec3f(3173.0, 1153.0, 797.0);
        wl *= 0.5;
        amp *= 0.55;
    }
    return sum;
}
fn fbm01(p : vec3f, wavelength : f32, footprint : f32, octaves : i32) -> f32 {
    return clamp(0.5 + 0.5 * fbm(p, wavelength, footprint, octaves), 0.0, 1.0);
}
`
