package wgsl

// Light is the shading terms both renderers build their BRDFs from: the
// GGX distribution, Smith-Schlick geometry, Schlick's Fresnel, the
// windowed inverse-square falloff of a punctual light, and the
// Henyey-Greenstein phase function. A renderer composes them in its own
// units (gfx's Renderer3D divides by π, a renderer that folds π into its
// light colours does not). No dependencies.
const Light = `
// ---- gfx/render/wgsl: Light --------------------------------------------
// The GGX (Trowbridge-Reitz) normal distribution times π, for n·h = ndh and
// a2 = roughness⁴.
fn ggx_dpi(ndh : f32, a2 : f32) -> f32 {
    let den = ndh * ndh * (a2 - 1.0) + 1.0;
    return a2 / (den * den);
}
// Smith's geometry term with Schlick's approximation, for both directions
// (k = (r + 1)²/8 for punctual lights, r²/2 for image light).
fn smith_schlick(ndv : f32, ndl : f32, k : f32) -> f32 {
    return (ndv / (ndv * (1.0 - k) + k)) * (ndl / (ndl * (1.0 - k) + k));
}
// Schlick's Fresnel, from f0 at normal incidence toward f90 at grazing.
fn fresnel_schlick(f0 : vec3f, f90 : vec3f, c : f32) -> vec3f {
    return f0 + (f90 - f0) * pow(1.0 - c, 5.0);
}
// A punctual light's falloff at squared distance d2 for a range whose
// square is r2: inverse square softened over the first metre (and over a
// ball of radius √soft2, for a light inside a volume), windowed smoothly
// to zero at the range ((1 − (d/R)⁴)², as UE4 and Godot).
fn light_window(d2 : f32, r2 : f32, soft2 : f32) -> f32 {
    let q = d2 / r2;
    let w = max(1.0 - q * q, 0.0);
    return w * w / (d2 + soft2 + 1.0);
}
// Henyey-Greenstein, normalised so isotropic is 1 (divide by 4π for the
// probability density).
fn hg_phase(mu : f32, g : f32) -> f32 {
    let g2 = g * g;
    return (1.0 - g2) / pow(max(1.0 + g2 - 2.0 * g * mu, 1e-4), 1.5);
}
`
