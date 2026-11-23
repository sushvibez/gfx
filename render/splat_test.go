//go:build !js

package render

import (
	"math"
	"strings"
	"testing"

	"github.com/sushvibez/gfx/core"
)

// redShare is the mean of red over (red+green+blue) across the lower
// middle of the frame (the floor in materialScene).
func redShare(pix []byte) float64 {
	var r, s float64
	for y := 150; y < 230; y += 2 {
		for x := 40; x < 200; x += 2 {
			i := (y*testW + x) * 4
			r += float64(pix[i])
			s += float64(pix[i]) + float64(pix[i+1]) + float64(pix[i+2]) + 1
		}
	}
	return r / s
}

// TestMaterialSplat: a material with a red steep layer stays itself on
// level ground and turns red when the same surface is tilted past the
// threshold; a material without a layer is unchanged by the tilt.
func TestMaterialSplat(t *testing.T) {
	scene := func(tilt float32, splat bool) []byte {
		model := core.Mat4RotateX(tilt)
		return materialScene(t, func(r *Renderer3D) {}, func(r *Renderer3D, ground *Mesh, m *Material) {
			if splat {
				m.Splat = &SplatLayer{Color: RGBA{R: 1, G: 0.05, B: 0.05, A: 1}, Roughness: 0.8}
			}
			r.DrawMaterial(ground, model, m)
		})
	}
	steep := float32(60 * math.Pi / 180) // n.y = 0.5, well past cos 35° ≈ 0.82
	flat := redShare(scene(0, true))
	plainSteep := redShare(scene(steep, false))
	splatSteep := redShare(scene(steep, true))
	t.Logf("red share: level with a layer %.3f, steep without %.3f, steep with %.3f", flat, plainSteep, splatSteep)
	if flat > 0.40 {
		t.Errorf("level ground took the steep layer: red share %.3f", flat)
	}
	if splatSteep < 0.6 {
		t.Errorf("steep ground did not take the steep layer: red share %.3f", splatSteep)
	}
	if plainSteep > 0.40 {
		t.Errorf("steep ground without a layer turned red: %.3f", plainSteep)
	}
}

// TestSplatWeightSeam: the shader's blend weight is a smoothstep across
// the threshold, so a surface just under it is mostly the flat layer, one
// just over it mostly the steep layer, and the noise only moves the seam
// (the shader text carries the reference's terms).
func TestSplatWeightSeam(t *testing.T) {
	for _, want := range []string{"fn splat_weight", "smoothstep(th - soft, th + soft, up)", "splat_albedo_tex", "F_SPLAT"} {
		if !strings.Contains(shaderMaterial, want) {
			t.Errorf("material shader is missing %q", want)
		}
	}
	c, p2, p4, f := (&SplatLayer{}).resolve()
	if c != (RGBA{R: 1, G: 1, B: 1, A: 1}) || f != matSplat {
		t.Errorf("defaults: colour %v flags %d", c, f)
	}
	if math.Abs(float64(p2[0])-math.Cos(35*math.Pi/180)) > 1e-6 || p2[1] != 0.12 || p2[2] != 0.12 || p4[2] != 1 || p4[3] != 0.5 {
		t.Errorf("defaults: p2 %v p4 %v", p2, p4)
	}
	if _, p2, _, _ := (&SplatLayer{Jitter: -1}).resolve(); p2[2] != 0 {
		t.Errorf("negative jitter should mean none, got %v", p2[2])
	}
}
