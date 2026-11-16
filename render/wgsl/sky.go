package wgsl

// Sky is the cloud deck's and the storm's geometry: where a ray meets a
// spherical shell (a cloud deck's base or top round a planet), and the
// lightning bolt's channel, a jagged line with a branch from the cloud
// base down to the horizon under the strike. The deck's light and the
// bolt's colour, brightness and the air in front of it are the
// renderer's. Needs Hash.
const Sky = `
// ---- gfx/render/wgsl: Sky ----------------------------------------------
// First positive distance along dir from ro to the sphere of radius rad
// about the origin, or -1.
fn shell_hit(ro : vec3f, dir : vec3f, rad : f32) -> f32 {
    let b = dot(ro, dir);
    let c = dot(ro, ro) - rad * rad;
    let disc = b * b - c;
    if (disc < 0.0) { return -1.0; }
    let sq = sqrt(disc);
    let t0 = -b - sq;
    if (t0 > 0.0) { return t0; }
    let t1 = -b + sq;
    if (t1 > 0.0) { return t1; }
    return -1.0;
}
// The bolt's sideways offset at height u (0 ground … 1 cloud base): straight
// segments with sharp kinks, four octaves.
fn bolt_jag(u : f32, seed : f32) -> f32 {
    var x = 0.0;
    var amp = 1.0;
    var k = 5.0;
    for (var i = 0; i < 4; i++) {
        let a = floor(u * k);
        let f = fract(u * k);
        let h0 = hash13(vec3f(a, seed, f32(i))) - 0.5;
        let h1 = hash13(vec3f(a + 1.0, seed, f32(i))) - 0.5;
        x += mix(h0, h1, f) * amp;
        amp *= 0.45;
        k *= 2.7;
    }
    return x;
}
// How much of a lightning channel the view direction dir sees: strike is
// the unit direction to where the bolt leaves the cloud base, up the local
// vertical, px a pixel's angle (rad). The channel is a pixel-wide core with
// a faint glow, from the cloud base to the ground (the horizon's
// direction under it), with a branch forking off its upper third. 0 away
// from it.
fn bolt_shape(dir : vec3f, up : vec3f, strike : vec3f, px : f32) -> f32 {
    let eb = dot(strike, up);                            // sin elevation of the top
    if (eb <= 0.004) { return 0.0; }
    var fh = strike - up * eb;
    fh = fh / max(length(fh), 1e-4);
    let fwd = dot(dir, fh);
    if (fwd <= 0.0) { return 0.0; }
    let side = normalize(cross(up, fh));
    let tb = eb / max(dot(strike, fh), 1e-3);            // tan elevation of the top
    let u = (dot(dir, up) / fwd) / tb;                   // 0 at the horizon, 1 at the cloud base
    if (u < -0.4 || u > 1.02) { return 0.0; }
    let lat = dot(dir, side) / fwd;                      // tan of the bearing off the strike
    let seed = fract(dot(strike, vec3f(17.13, 31.71, 9.37)) * 43.0) * 97.0;
    let x = bolt_jag(u, seed) * tb * 0.55 - bolt_jag(1.0, seed) * tb * 0.55;
    let d = abs(lat - x);
    var i = exp(-(d * d) / (px * px * 1.4)) * 1.0 + exp(-d / (tb * 0.06)) * 0.05;
    let ub = 0.62 + 0.2 * hash13(vec3f(seed, 3.0, 1.0));
    if (u < ub && u > ub - 0.35) {
        let k = (ub - u) / 0.35;
        let x0 = (bolt_jag(ub, seed) - bolt_jag(1.0, seed)) * tb * 0.55;
        let xb = x0 + (bolt_jag(u, seed + 11.0) - bolt_jag(ub, seed + 11.0)) * tb * 0.4
            + sign(hash13(vec3f(seed, 5.0, 2.0)) - 0.5) * k * tb * 0.3;
        let db = abs(lat - xb);
        i += exp(-(db * db) / (px * px * 1.0)) * 0.5 * (1.0 - k);
    }
    return i * smoothstep(-0.4, -0.3, u) * (1.0 - smoothstep(0.97, 1.02, u));
}
`
