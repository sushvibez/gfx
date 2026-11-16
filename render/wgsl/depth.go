package wgsl

// Depth turns depth-buffer values into view distances and back, for a
// perspective projection or for logarithmic depth (a planet-scale
// renderer's: z_ndc = log2(1 + w) · c, c = 1 / log2(1 + far)), and fades a
// soft particle against the scene. No dependencies.
const Depth = `
// ---- gfx/render/wgsl: Depth --------------------------------------------
// Clip-space z for logarithmic depth, given clip w (the distance in front
// of the camera), so that z/w = log2(1 + w) · c.
//
// Behind the camera it is w scaled almost to nothing. A triangle that
// reaches behind the eye is clipped where z crosses 0, and log depth makes
// z tiny next to w in front (z ≈ w²·c near the eye); with z ≈ 0 behind,
// the crossing sits at the eye and the frustum's side planes cut the
// triangle as they should (any larger value cuts the ground away at the
// viewer's feet from some headings).
fn log_depth_z(w : f32, c : f32) -> f32 {
    if (w <= 0.0) { return w * 1e-6; }
    return log2(1.0 + w) * c * w;
}
// The view distance of a logarithmic depth value d (the inverse of
// log_depth_z's z/w).
fn log_depth_dist(d : f32, c : f32) -> f32 { return exp2(d / c) - 1.0; }
// The view distance of a depth value: logarithmic when logc > 0, else a
// perspective projection's (proj_z, proj_w: the projection's [2][2] and
// [3][2], so w = proj_w / (d + proj_z)).
fn depth_dist(d : f32, logc : f32, proj_z : f32, proj_w : f32) -> f32 {
    if (logc > 0.0) { return log_depth_dist(d, logc); }
    return proj_w / (d + proj_z);
}
// A soft particle's opacity against the scene behind it: 0 where the scene
// is nearer than the fragment, rising to 1 over the last range metres in
// front of it, so smoke on the ground thins into it instead of cutting a
// hard line.
fn soft_fade(scene : f32, frag : f32, range : f32) -> f32 {
    return clamp((scene - frag) / range, 0.0, 1.0);
}
`
