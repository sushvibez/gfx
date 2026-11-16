package wgsl

// Shadow is percentage-closer soft shadows (PCSS, Fernando 2005) over one
// depth map: a blocker search finds how far along the light the occluders
// stand, and the filter widens with that distance, so a shadow is crisp
// where it touches and soft far from what casts it. Both kernels are Vogel
// discs turned per pixel.
//
// Bias is the receiver's own plane: its depth gradient in the map's uv,
// worked out from the normal through the (affine) light matrix, is what
// each tap compares against, so a wide kernel on a slanted face does not
// shadow the face with itself.
//
// A renderer supplies the lookup (its point in the map, the reference
// depth after its bias, the rotation from its pixel) and the penumbra's
// width from the blocker distance in its own units. Needs Hash.
const Shadow = `
// ---- gfx/render/wgsl: Shadow (PCSS) ------------------------------------
// The receiver plane's depth gradient in shadow-map uv for normal n,
// through an affine light matrix lvp (a normal transforms by the inverse
// transpose, which for a scaled rotation is the matrix itself over its
// squared row lengths), never steeper than max_slope.
fn pcss_plane(lvp : mat4x4f, n : vec3f, max_slope : f32) -> vec2f {
    let lm = mat3x3f(lvp[0].xyz, lvp[1].xyz, lvp[2].xyz);
    let lt = transpose(lm);
    let nc = (lm * n) / vec3f(dot(lt[0], lt[0]), dot(lt[1], lt[1]), dot(lt[2], lt[2]));
    var dz = vec2f(0.0);
    if (abs(nc.z) > 1e-6) { dz = vec2f(-2.0 * nc.x, 2.0 * nc.y) / nc.z; }
    return clamp(dz, vec2f(-max_slope), vec2f(max_slope));
}
// Blocker search: n taps of the map (read straight, not compared) within
// search of uv; returns the sum of the blockers' depths and their count.
// Each tap compares against the receiver's plane at the centre of the
// texel it read, not at the tap.
fn pcss_blockers(tex : texture_depth_2d, uv : vec2f, zr : f32, dz : vec2f, phi : f32, search : f32, n : i32, dims : vec2f) -> vec2f {
    var blk = 0.0;
    var nb = 0.0;
    for (var i = 0; i < n; i++) {
        let o = vogel(i, n, phi) * search;
        let t = vec2i(clamp((uv + o) * dims, vec2f(0.0), dims - 1.0));
        let d = textureLoad(tex, t, 0);
        let oc = (vec2f(t) + 0.5) / dims - uv;
        if (d < zr + dot(oc, dz)) { blk += d; nb += 1.0; }
    }
    return vec2f(blk, nb);
}
// The filter: n hardware-compared taps over a disc of radius pen (uv),
// each against the receiver's plane; the share lit.
fn pcss_filter(tex : texture_depth_2d, samp : sampler_comparison, uv : vec2f, zr : f32, dz : vec2f, phi : f32, pen : f32, n : i32) -> f32 {
    var lit = 0.0;
    for (var i = 0; i < n; i++) {
        let o = vogel(i, n, phi) * pen;
        lit += textureSampleCompareLevel(tex, samp, uv + o, zr + dot(o, dz));
    }
    return lit / f32(n);
}
// How far uv is from the map's edge, as a fade to lit over its last 3%.
fn pcss_border(uv : vec2f) -> f32 {
    let e = min(uv, 1.0 - uv);
    return smoothstep(0.0, 0.03, min(e.x, e.y));
}
`
