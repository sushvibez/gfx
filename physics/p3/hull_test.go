package p3

import (
	"math/rand"
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
)

// boxCorners returns the 8 corners of an axis-aligned box.
func boxCorners(half core.Vec3) []core.Vec3 {
	out := make([]core.Vec3, 0, 8)
	for i := 0; i < 8; i++ {
		out = append(out, core.V3(
			pick(i&1 == 0, -half.X, half.X),
			pick(i&2 == 0, -half.Y, half.Y),
			pick(i&4 == 0, -half.Z, half.Z)))
	}
	return out
}

func TestQuickhullCube(t *testing.T) {
	// Interior and duplicate points must be pruned; a cube resolves to
	// 8 vertices, 12 triangles, 6 deduped face planes.
	pts := append(boxCorners(core.V3(1, 1, 1)),
		core.V3(0, 0, 0), core.V3(0.1, 0.2, 0.3), core.V3(-1, -1, -1))
	h := NewConvexHull(pts)
	if h == nil {
		t.Fatal("cube hull failed to build")
	}
	if len(h.verts) != 8 {
		t.Fatalf("verts = %d, want 8", len(h.verts))
	}
	if len(h.tris) != 12*3 {
		t.Fatalf("tris = %d, want 12", len(h.tris)/3)
	}
	if len(h.planes) != 6 {
		t.Fatalf("planes = %d, want 6", len(h.planes))
	}
	// Every vertex must lie inside-or-on every plane; every plane must
	// touch at least one vertex (supporting).
	for pi, pl := range h.planes {
		touch := false
		for _, v := range h.verts {
			d := pl.n.Dot(v) - pl.d
			if d > 1e-4 {
				t.Fatalf("plane %d cuts a hull vertex off (d=%v)", pi, d)
			}
			if math32.Abs(d) < 1e-4 {
				touch = true
			}
		}
		if !touch {
			t.Fatalf("plane %d supports no vertex", pi)
		}
	}
}

func TestQuickhullDegenerate(t *testing.T) {
	cases := []struct {
		name string
		pts  []core.Vec3
	}{
		{"too-few", []core.Vec3{{X: 0}, {X: 1}, {Y: 1}}},
		{"coplanar", []core.Vec3{{}, {X: 1}, {Y: 1}, {X: 1, Y: 1}, {X: 0.5, Y: 0.5}}},
		{"collinear", []core.Vec3{{}, {X: 1}, {X: 2}, {X: 3}}},
		{"coincident", []core.Vec3{{X: 1}, {X: 1}, {X: 1}, {X: 1}}},
	}
	for _, c := range cases {
		if h := NewConvexHull(c.pts); h != nil {
			t.Errorf("%s: got a hull from degenerate points", c.name)
		}
	}
}

// TestHullBoxOracle cross-checks the hull built from box corners against
// the analytic box shape: rays and closest points must agree.
func TestHullBoxOracle(t *testing.T) {
	half := core.V3(0.7, 1.3, 0.9)
	hull := Hull(NewConvexHull(boxCorners(half)))
	box := Box(half)
	if hull.Hull == nil {
		t.Fatal("hull failed to build")
	}
	rng := rand.New(rand.NewSource(11))
	rv := func(scale float32) core.Vec3 {
		return core.V3((rng.Float32()*2-1)*scale, (rng.Float32()*2-1)*scale, (rng.Float32()*2-1)*scale)
	}
	for i := 0; i < 2000; i++ {
		o := rv(5)
		d := rv(1)
		if d.Length() < 1e-3 {
			continue
		}
		d = d.Normalize()
		ht, hn, hok := rayShape(&hull, o, d, 100)
		bt, bn, bok := rayShape(&box, o, d, 100)
		if hok != bok {
			t.Fatalf("ray %d: hull hit=%v box hit=%v (o=%+v d=%+v)", i, hok, bok, o, d)
		}
		if !hok {
			continue
		}
		if math32.Abs(ht-bt) > 1e-3 {
			t.Fatalf("ray %d: hull t=%v box t=%v", i, ht, bt)
		}
		// Entry through an edge/corner makes the face choice ambiguous;
		// compare normals only when the ts agree and both are surface hits.
		if ht > 0 && hn.Dot(bn) < 0.99 {
			// Ambiguous only when the hit point is within tolerance of two faces.
			p := o.Add(d.Scale(bt))
			nearFaces := 0
			for _, fd := range [3]float32{half.X - math32.Abs(p.X), half.Y - math32.Abs(p.Y), half.Z - math32.Abs(p.Z)} {
				if fd < 1e-3 {
					nearFaces++
				}
			}
			if nearFaces < 2 {
				t.Fatalf("ray %d: hull n=%+v box n=%+v at p=%+v", i, hn, bn, p)
			}
		}
	}
	for i := 0; i < 2000; i++ {
		p := rv(3)
		hq := hull.closestLocal(p)
		bq := box.closestLocal(p)
		if hq.Sub(bq).Length() > 1e-3 {
			t.Fatalf("point %d: hull closest %+v, box closest %+v (p=%+v)", i, hq, bq, p)
		}
	}
}

func TestHullBody(t *testing.T) {
	cube := NewConvexHull(boxCorners(core.V3(1, 1, 1)))
	w := NewWorld()
	// Rotated 45° about Y the near edge sits at z = 10 - √2 (same oracle as
	// the rotated-box test).
	rot := core.QuatAxisAngle(core.V3(0, 1, 0), math32.Pi/4)
	id := w.AddBody(Hull(cube), core.V3(0, 0, 10), rot, 0b01)
	h, ok := w.RayCast(core.V3(0, 0, 0), core.V3(0, 0, 1), 100, LayerAll)
	if !ok || h.Body != id {
		t.Fatalf("hull body not hit: %+v %v", h, ok)
	}
	near(t, h.T, 10-math32.Sqrt2, 1e-2, "rotated hull T")
	if h.Normal.Z >= 0 {
		t.Fatalf("hull normal faces away: %+v", h.Normal)
	}
	// Mask filtering applies to hull bodies like any other.
	if _, ok := w.RayCast(core.V3(0, 0, 0), core.V3(0, 0, 1), 100, 0b10); ok {
		t.Fatal("layer mask ignored for hull body")
	}
	// Swept sphere stops on the rotated edge: surface reaches z = 10-√2-r.
	ch, ok := w.CastSphere(core.V3(0, 0, 0), 0.5, core.V3(0, 0, 1), 20, LayerAll)
	if !ok {
		t.Fatal("cast missed hull body")
	}
	near(t, ch.T, 10-math32.Sqrt2-0.5, 1e-2, "hull cast T")
	// Overlap near the edge.
	hit := false
	w.OverlapSphere(core.V3(0, 0, 10-math32.Sqrt2-0.2), 0.3, LayerAll, func(b BodyID, p core.Vec3) {
		hit = hit || b == id
	})
	if !hit {
		t.Fatal("overlap missed hull body")
	}
}
