//go:build !js

package render

import (
	"bytes"
	"math"
	"testing"

	"github.com/sushvibez/gfx/core"
)

// blockMeans is the mean luminance of each 16×16 block across the lower
// half of the frame (the ground in materialScene).
func blockMeans(pix []byte) []float64 {
	var out []float64
	for by := testW / 2; by+16 <= testW; by += 16 {
		for bx := 0; bx+16 <= testW; bx += 16 {
			var s float64
			for y := by; y < by+16; y++ {
				for x := bx; x < bx+16; x++ {
					i := (y*testW + x) * 4
					s += 0.2126*float64(pix[i]) + 0.7152*float64(pix[i+1]) + 0.0722*float64(pix[i+2])
				}
			}
			out = append(out, s/256)
		}
	}
	return out
}

// TestMaterialMacro: Macro varies a tiled material slowly over the ground
// (the blocks' brightness, relative to the same ground without it, spreads
// out), and a zero Macro draws exactly what no Macro does.
func TestMaterialMacro(t *testing.T) {
	scene := func(macro, scale float32) []byte {
		return materialScene(t, func(r *Renderer3D) {}, func(r *Renderer3D, ground *Mesh, m *Material) {
			m.Macro, m.MacroScale = macro, scale
			r.DrawMaterial(ground, core.Mat4Identity(), m)
		})
	}
	plain := scene(0, 0)
	if !bytes.Equal(plain, scene(0, 1.5)) {
		t.Fatal("a zero Macro changed the picture")
	}
	on := scene(0.3, 1.5)
	a, b := blockMeans(plain), blockMeans(on)
	var mean, sq float64
	n := 0
	for i := range a {
		if a[i] < 8 {
			continue // sky or black
		}
		r := b[i] / a[i]
		mean += r
		sq += r * r
		n++
	}
	if n < 20 {
		t.Fatalf("only %d blocks of ground", n)
	}
	mean /= float64(n)
	sd := math.Sqrt(math.Max(sq/float64(n)-mean*mean, 0))
	t.Logf("block brightness ratio with Macro 0.3: mean %.3f, spread %.3f over %d blocks", mean, sd, n)
	if sd < 0.04 {
		t.Errorf("Macro 0.3 barely varied the ground: spread %.3f", sd)
	}
	if mean < 0.85 || mean > 1.15 {
		t.Errorf("Macro moved the ground's mean brightness to %.3f of plain", mean)
	}
}
