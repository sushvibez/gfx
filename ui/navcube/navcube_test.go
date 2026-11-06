package navcube

import (
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
)

// 6 faces, 12 bevels, 8 corners; every facet is planar, lies on the
// outside of the cube and faces its normal.
func TestFacets(t *testing.T) {
	fs := Facets()
	if len(fs) != 26 {
		t.Fatalf("facets %d, want 26", len(fs))
	}
	faces := 0
	for i, g := range fs {
		if g.Face >= 0 {
			faces++
			if len(g.UV) != len(g.Points) {
				t.Errorf("face %d: %d uvs for %d points", i, len(g.UV), len(g.Points))
			}
		}
		if math32.Abs(g.Normal.Length()-1) > 1e-5 {
			t.Errorf("facet %d normal %v not unit", i, g.Normal)
		}
		var c core.Vec3
		for _, p := range g.Points {
			c = c.Add(p)
		}
		c = c.Scale(1 / float32(len(g.Points)))
		if c.Dot(g.Normal) <= 0 {
			t.Errorf("facet %d centre %v faces inward", i, c)
		}
		for _, p := range g.Points {
			if d := p.Sub(c).Dot(g.Normal); math32.Abs(d) > 1e-5 {
				t.Errorf("facet %d not planar (%.6f)", i, d)
			}
		}
	}
	if faces != 6 {
		t.Errorf("labelled faces %d", faces)
	}
}

// Snapping to a facet and easing there puts the eye on its normal, the
// short way round; Top stops at the orbit's pitch limit.
func TestSnapEase(t *testing.T) {
	for _, n := range []core.Vec3{core.V3(0, 0, 1), core.V3(1, 0, 0), core.V3(-1, 0, 0), core.V3(1, 1, 1).Normalize()} {
		ty, tp := SnapAngles(n)
		y, p := float32(3), float32(0)
		done := false
		steps := 0
		for ; !done && steps < 200; steps++ {
			y, p, done = Ease(y, p, ty, tp)
		}
		if !done {
			t.Fatalf("%v: never arrived", n)
		}
		eye := core.V3(math32.Cos(p)*math32.Sin(y), math32.Sin(p), math32.Cos(p)*math32.Cos(y))
		if eye.Sub(n).Length() > 1e-3 {
			t.Errorf("%v: eye at %v", n, eye)
		}
	}
	if _, p := SnapAngles(core.V3(0, 1, 0)); math32.Abs(p-1.5) > 1e-6 {
		t.Errorf("top pitch %.3f", p)
	}
}
