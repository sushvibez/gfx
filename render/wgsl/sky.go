package wgsl

// Sky is the cloud deck's and the storm's geometry and the clouds' light:
// where a ray meets a spherical shell (a cloud deck's base or top round a
// planet); the lightning bolt's channel, a jagged line with a branch from
// the cloud base down to the horizon under the strike; a deck's colour
// from above (deck_top) and from below (deck_under, after Saltline's
// sky: slate from the sky's light, thin spots, warm raking light near a
// low sun, the burning rim, sheet lightning, the far deck melting into
// the horizon); and a cloud volume's scattering (cloud_phases, the sun
// through an optical depth with multiple scattering, cloud_sunlit, the
// silver lining, cloud_silver, and the sky's and the ground's light on
// it, cloud_dome_light and cloud_ground_light). The deck's shape (its
// density field) and the light at the deck are the renderer's, passed
// in. Needs Hash and Light.
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

// ---- the clouds' light
// A deck at one point, as its renderer's density field gives it: kind 0
// a stratus deck, 1 cumulus, 2 cirrus, 3 a snow cloud; dark how far the
// weather darkens it (0…1); thick its thickness (dark cores from below);
// density the raw density (0…1); relief the underside's ripples lit from
// the sun's side (0.5 flat); cover the cloud's cover (0 gap … 1 cloud).
struct DeckCloud {
    kind : f32,
    dark : f32,
    thick : f32,
    density : f32,
    relief : f32,
    cover : f32,
}
// Colour of a deck seen from above: sunlit tops, the domes (dome 0…1)
// brighter than the hollows. sun, sky and moon are the light at the deck
// (the sun's, tinted and dimmed for its elevation sun_elev; the sky's
// fill; the moon's).
fn deck_top(sun : vec3f, sky : vec3f, moon : vec3f, sun_elev : f32, dome : f32) -> vec3f {
    let lit = sun * (0.3 + 0.6 * dome) * mix(0.35, 1.0, max(sun_elev, 0.0)) * 0.7 + sky * 0.5 + moon;
    return lit * vec3f(0.96, 0.97, 1.0);
}
// Colour of a deck's underside seen along dir (unit; up the local
// vertical, sun_dir toward the sun), after Saltline's sky: slate grey
// from the sky's light, darker where the cloud is thick and as the
// weather darkens it; its ripples lit on the sun's side; thin spots
// letting light through; warm light raking it near the sun (more so for
// a low sun); a burning silver rim where the cloud thins toward the sun;
// cirrus lit through; sheet lightning inside it (flash: xyz toward the
// strike, w its strength). bg is what lies behind (the sky): far cloud
// melts into it at the horizon.
fn deck_under(dir : vec3f, up : vec3f, sun_dir : vec3f, sun : vec3f, sky : vec3f, moon : vec3f, c : DeckCloud, flash : vec4f, bg : vec3f) -> vec3f {
    let sun_elev = dot(sun_dir, up);
    let g = dot(sky, vec3f(0.333));
    let e = max(dot(dir, up), 0.0);
    let s = max(dot(dir, sun_dir), 0.0);
    // Toward the sun's bearing (0 away … 1 under it), Saltline's skyToSun.
    var dh = dir - up * dot(dir, up);
    dh = dh / max(length(dh), 1e-4);
    var sh = sun_dir - up * sun_elev;
    sh = sh / max(length(sh), 1e-4);
    // Clamped: at the anti-sun bearing it rounds a hair below zero, and
    // pow() of a negative is NaN (a dotted black line up the sky).
    let ts = clamp(0.5 + 0.5 * dot(dh, sh), 0.0, 1.0);
    let low = 1.0 - smoothstep(0.12, 0.55, sun_elev);      // 1 for a low sun: warm, raking
    let warm_tint = mix(vec3f(1.0, 0.97, 0.92), vec3f(1.30, 0.66, 0.40), low);
    let relief = c.relief;
    let thick = c.thick;
    var col : vec3f;
    if (abs(c.kind - 2.0) < 0.5) {
        // Cirrus: ice, thin, lit through: white in the sun's glow, the
        // sky's tint away from it, fiery with a low sun.
        col = bg + vec3f(g) * 0.3 + sun * warm_tint * (0.15 + 0.55 * pow(s, 4.0) + 0.5 * low * pow(ts, 3.0)) + moon;
    } else {
        // Slate: the sky's light from below, darker where thick, darker
        // still as the weather darkens it.
        col = vec3f(g) * vec3f(0.93, 0.97, 1.05) * mix(1.3, 0.34, thick) * mix(1.0, 0.42, c.dark);
        if (c.kind > 2.5) {
            col *= 0.85 + 0.3 * relief;                        // snow cloud: a soft, even grey
        } else {
            col *= 0.5 + 1.0 * relief * relief;
        }
        // Thin spots let cold light through.
        col += (sun * 0.30 + vec3f(g) * 0.25) * pow(clamp(1.0 - thick, 0.0, 1.0), 3.0) * (1.0 - 0.75 * c.dark);
        // Warm light raking the underside: near the sun, and toward its
        // bearing near the horizon when it is low.
        let near_sun = pow(s, 7.0) * 0.9 + pow(ts, 5.0) * exp(-e * 4.5) * 0.9 * low;
        col += sun * warm_tint * near_sun * (0.05 + 2.2 * relief * relief * relief) * 0.55 * (1.0 - 0.85 * c.dark);
        // The burning rim where cloud thins at its edge, facing the sun.
        let rim = c.cover * (1.0 - c.cover) * 4.0;
        col += sun * warm_tint * rim * (0.015 + 0.3 * pow(ts, 6.0) + 1.6 * pow(s, 6.0)) * (1.0 - 0.9 * c.dark);
        if (c.kind > 0.5 && c.kind < 1.5) {
            // Cumulus toward the horizon shows its sides: sunlit white
            // toward the sun, grey-blue away from it.
            let side = 1.0 - smoothstep(0.02, 0.3, e);
            let face = sun * (0.25 + 0.55 * ts) * (0.6 + 0.4 * c.density) + sky * 0.75;
            col = mix(col, face, side * 0.85);
        }
        col += moon * (0.6 + 0.4 * relief);
    }
    // Sheet lightning inside the deck.
    let fl = flash.w * pow(max(dot(dir, flash.xyz), 0.0), 5.0);
    col += vec3f(0.55, 0.68, 1.0) * (fl * 3.0 * (0.4 + 2.2 * c.density * c.density) + 0.2 * flash.w);
    // Far cloud melts into the horizon behind it.
    col = mix(col, bg * 0.55 + col * 0.45, exp(-e * 11.0));
    return col;
}
// A cloud volume's phase for mu, the cosine between the view and the
// light: x single scattering (a forward lobe, g 0.6), y multiply
// scattered (flatter, g 0.3). Normalised so isotropic is about 1.
fn cloud_phases(mu : f32) -> vec2f {
    return vec2f(0.55 * hg_phase(mu, 0.6) + 0.45, 0.3 * hg_phase(mu, 0.3) + 0.7);
}
// The light's share reaching a point in a cloud through optical depth od:
// the direct beam by the single-scattering phase, and two softer octaves
// of multiple scattering by the flatter one (a cheap sum, so sunlit
// tops outshine the sky and bases stay a flat grey-blue).
fn cloud_sunlit(od : f32, ph : vec2f) -> f32 {
    return exp(-od) * ph.x + 0.5 * exp(-od * 0.25) * ph.y + 0.15 * exp(-od * 0.07);
}
// The silver lining: a cloud's thin edge (edge 0…1, 1 at the thinnest)
// seen toward the light scatters it forward in a narrow lobe (g 0.85);
// depth is the optical depth toward the light through the edge.
fn cloud_silver(edge : f32, depth : f32, mu : f32) -> f32 {
    return edge * exp(-depth * 0.4) * 0.012 * hg_phase(mu, 0.85);
}
// The sky dome's light on a cloud's crowns: day 0 (night) … 1, dark how
// far the weather darkens it, moon the moon's strength.
fn cloud_dome_light(day : f32, dark : f32, moon : f32) -> vec3f {
    return vec3f(0.35, 0.43, 0.56) * mix(0.012, 1.0, day) * (1.0 - 0.6 * dark) + vec3f(0.0025, 0.0032, 0.0065) * moon;
}
// The ground's light on a cloud's base, and a city's sodium and LED glow
// on it at night (city 0 … 1, its share of the night).
fn cloud_ground_light(day : f32, city : f32) -> vec3f {
    return vec3f(0.26, 0.25, 0.22) * mix(0.02, 0.22, day) + vec3f(0.016, 0.0095, 0.0045) * city;
}
`
