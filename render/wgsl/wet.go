package wgsl

// Wet is rain on surfaces: how water in the pores darkens and saturates a
// colour, where puddles stand on the ground, the rings the drops leave on
// standing water, water's Fresnel, and the mirror strength a pixel leaves
// in the HDR target's alpha for render.Post to trace (Post.UseReflection,
// FrameSettings.SceneReflection). Needs Hash and Noise.
const Wet = `
// ---- gfx/render/wgsl: Wet ----------------------------------------------
// Water's Fresnel reflectance (Schlick, f0 0.02).
fn water_fresnel(ndv : f32) -> f32 {
    return 0.02 + 0.98 * pow(1.0 - clamp(ndv, 0.0, 1.0), 5.0);
}
// A wet surface's colour: water in the pores absorbs more of the light
// scattered back out, so it darkens (porous: soil, plaster, concrete,
// canvas ~1; waxy leaves ~0.3; painted metal ~0.1) and grows more
// saturated.
fn wet_tone(albedo : vec3f, porous : f32, w : f32) -> vec3f {
    let dark = albedo * (1.0 - 0.65 * porous * w);
    let lum = dot(dark, vec3f(0.3, 0.55, 0.15));
    return max(mix(vec3f(lum), dark, 1.0 + 0.3 * w), vec3f(0.0));
}
// Rain rings on standing water, in three layers: in each cell of a hashed
// grid a drop lands at its own moment, and its ring runs outward and
// fades. p is a level coordinate in metres, footprint a pixel's size there
// (rings narrower than a pixel or two fade out), rain 0..1 how hard it
// rains (in light rain a cell sits out more cycles). Returns the water's
// slope (xy, per metre, along p's axes) and how much ring there is (z).
fn rain_rings(p : vec2f, t : f32, footprint : f32, rain : f32) -> vec3f {
    var g = vec2f(0.0);
    var lit = 0.0;
    let sizes = vec3f(0.31, 0.43, 0.23);
    for (var i = 0; i < 3; i++) {
        let s = sizes[i];
        let fade = 1.0 - smoothstep(0.02 * s, 0.08 * s, footprint);
        if (fade <= 0.0) { continue; }
        let fi = f32(i);
        let q = p / s + vec2f(fi * 17.3, fi * 5.1);
        let c = floor(q);
        let h = vec3f(hash13(vec3f(c, fi + 1.0)), hash13(vec3f(c + vec2f(31.7, 11.3), fi + 4.0)), hash13(vec3f(c + vec2f(5.9, 71.1), fi + 9.0)));
        let cyc = t * 1.15 + h.z + fi * 0.37;
        if (hash13(vec3f(c, floor(cyc) + fi * 13.0)) > 0.35 + 0.6 * rain) { continue; }
        let life = fract(cyc);
        let d = q - (c + 0.25 + 0.5 * h.xy);
        let r = length(d);
        let x = (r - life * 0.5) * 16.0;
        let env = (1.0 - life) * (1.0 - life) * fade;
        let e = exp(-x * x);
        g += d / max(r, 1e-3) * (sin(x * 2.4) * e * env * 0.9);
        lit += e * env;
    }
    return vec3f(g, lit);
}
// Puddles on the ground: a noise mask over the flat and the hollows
// (world in metres; footprint a pixel's size there; slope 0 level, past
// 0.09 too steep to hold water; cavity 0.35 concave … 1 ridge, 0.8 for
// neither), more of it the wetter it is (wet 0..1), more on paving (bias,
// about +0.1). x standing water, y soaked, z damp round it.
fn puddle_level(world : vec3f, footprint : f32, slope : f32, cavity : f32, bias : f32, wet : f32) -> vec3f {
    let n = fbm(world + vec3f(91.0, 17.0, 53.0), 8.0, footprint, 3) + 0.45 * fbm(world + vec3f(7.0, 3.0, 11.0), 2.0, footprint, 3);
    let flat = 1.0 - smoothstep(0.015, 0.09, slope);
    let f = (n + 0.6 * clamp(0.8 - cavity, -0.3, 0.45) + bias) * flat - (1.0 - flat) * 0.6;
    let lvl = 0.36 - 0.2 * wet;
    let k = smoothstep(0.15, 0.6, wet);
    return vec3f(smoothstep(lvl, lvl + 0.02, f), smoothstep(lvl - 0.09, lvl + 0.01, f), smoothstep(lvl - 0.26, lvl - 0.07, f)) * k;
}
// The alpha a pixel leaves for render.Post: 1 − its mirror strength m
// (at most cap), negated for a mirror that ripples (moving water, a
// rained-on puddle), so Post bends what it traces there.
fn mirror_out(m : f32, cap : f32, moving : bool) -> f32 {
    let a = 1.0 - clamp(m, 0.0, cap);
    return select(a, -a, moving);
}
`
