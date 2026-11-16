package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render/wgsl"
)

// The shared WGSL library (render/wgsl) compiles as one module, every
// piece in its documented order, and no piece defines a function another
// does: a renderer includes them all without clashes.
func TestWGSLLibraryCompiles(t *testing.T) {
	pieces := map[string]string{
		"Hash": wgsl.Hash, "Noise": wgsl.Noise, "Depth": wgsl.Depth, "Light": wgsl.Light,
		"Shadow": wgsl.Shadow, "Wet": wgsl.Wet, "Sky": wgsl.Sky,
	}
	fn := regexp.MustCompile(`(?m)^fn ([a-z_0-9]+)\(`)
	seen := map[string]string{}
	for name, src := range pieces {
		for _, m := range fn.FindAllStringSubmatch(src, -1) {
			if other, ok := seen[m[1]]; ok {
				t.Errorf("%s defines %s, which %s does too", name, m[1], other)
			}
			seen[m[1]] = name
		}
	}
	device, _ := headlessDevice(t)
	src := wgsl.Join(wgsl.Hash, wgsl.Noise, wgsl.Depth, wgsl.Light, wgsl.Shadow, wgsl.Wet, wgsl.Sky) + `
@group(0) @binding(0) var tex : texture_depth_2d;
@group(0) @binding(1) var samp : sampler_comparison;
@vertex fn vs(@builtin(vertex_index) i : u32) -> @builtin(position) vec4f {
    return vec4f(f32(i), 0.0, log_depth_z(1.0, 0.1), 1.0);
}
@fragment fn fs(@builtin(position) p : vec4f) -> @location(0) vec4f {
    let dims = vec2f(textureDimensions(tex));
    let dz = pcss_plane(mat4x4f(), vec3f(0.0, 1.0, 0.0), 1.0);
    let b = pcss_blockers(tex, p.xy / dims, 0.5, dz, ign(p.xy), 0.01, 8, dims);
    let lit = pcss_filter(tex, samp, p.xy / dims, 0.5, dz, 0.0, 0.01, 12) * pcss_border(p.xy / dims);
    let pud = puddle_level(p.xyz, 0.01, 0.0, 0.8, 0.0, 1.0);
    let rr = rain_rings(p.xy, 1.0, 0.01, 1.0);
    let c = wet_tone(vec3f(0.5), 1.0, 0.5) * water_fresnel(0.5) * fresnel_schlick(vec3f(0.04), vec3f(1.0), 0.5)
        * ggx_dpi(0.5, 0.1) * smith_schlick(0.5, 0.5, 0.2) * light_window(1.0, 4.0, 0.0) * hg_phase(0.5, 0.7);
    let s = bolt_shape(vec3f(0.0, 0.2, 1.0), vec3f(0.0, 1.0, 0.0), normalize(vec3f(0.0, 0.3, 1.0)), 0.001)
        + shell_hit(vec3f(0.0, 1.0, 0.0), vec3f(0.0, 1.0, 0.0), 2.0) + fbm01(p.xyz, 1.0, 0.01, 3)
        + hash4u(1u).x + hash12s(p.xy) + soft_fade(depth_dist(0.5, 0.0, 1.0, 1.0), log_depth_dist(0.5, 0.1), 1.0);
    return vec4f(c + vec3f(b, lit) + pud + rr, s + mirror_out(0.5, 0.97, true));
}
`
	// wgpu reports a WGSL error through the uncaptured-error callback and
	// still hands back a module: count the errors around the compile.
	before := gpu.ErrorCount()
	m, err := device.CreateShaderModuleWGSL("wgsl-library", src)
	if err != nil || gpu.ErrorCount() != before {
		t.Fatalf("the library does not compile: %v", err)
	}
	m.Release()
	if !strings.Contains(src, "fn pcss_filter") {
		t.Fatal("Join dropped a piece")
	}
}
