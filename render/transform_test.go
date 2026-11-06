package render

import (
	"math"
	"testing"
)

// TestAffine2Basics: the transform algebra the 2D stack composes with.
func TestAffine2Basics(t *testing.T) {
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }
	// A rotation about a point leaves that point alone and turns the axes.
	r := Rotate2(math.Pi/2, 10, 10)
	if x, y := r.Apply(10, 10); !near(x, 10) || !near(y, 10) {
		t.Errorf("rotation moved its own origin to (%v,%v)", x, y)
	}
	// 90° in a y-down frame sends +x to +y.
	if x, y := r.Apply(20, 10); !near(x, 10) || !near(y, 20) {
		t.Errorf("(20,10) rotated 90° about (10,10) = (%v,%v), want (10,20)", x, y)
	}
	// Scale about a point.
	s := Scale2(2, 3, 0, 0)
	if x, y := s.Apply(4, 4); !near(x, 8) || !near(y, 12) {
		t.Errorf("scale = (%v,%v), want (8,12)", x, y)
	}
	// Composition order: Mul(n) applies n first.
	m := Translate2(5, 0).Mul(Scale2(2, 2, 0, 0))
	if x, y := m.Apply(1, 1); !near(x, 7) || !near(y, 2) {
		t.Errorf("translate∘scale = (%v,%v), want (7,2)", x, y)
	}
	// Inverse round-trips.
	inv, ok := m.Invert()
	if !ok {
		t.Fatal("invertible transform reported singular")
	}
	if x, y := inv.Apply(7, 2); !near(x, 1) || !near(y, 1) {
		t.Errorf("inverse gave (%v,%v), want (1,1)", x, y)
	}
	// A collapsed transform has no inverse.
	if _, ok := (Affine2{}).Invert(); ok {
		t.Error("the zero transform should report no inverse")
	}
	// decompose recovers scale and rotation.
	sx, sy, rot := Scale2(2, 4, 0, 0).decompose()
	if !near(sx, 2) || !near(sy, 4) || !near(rot, 0) {
		t.Errorf("decompose(scale) = %v,%v,%v", sx, sy, rot)
	}
	if _, _, rot := Rotate2(math.Pi/4, 0, 0).decompose(); !near(rot, math.Pi/4) {
		t.Errorf("decompose(rotate 45°) rot = %v", rot)
	}
}

// TestTransformStack: pushes compose, pops restore, and Begin clears.
func TestTransformStack(t *testing.T) {
	r := &Renderer2D{}
	if !r.CurrentTransform().IsIdentity() {
		t.Fatal("a fresh renderer should have no transform")
	}
	r.PushTransform(Translate2(10, 0))
	r.PushTransform(Translate2(0, 5))
	x, y := r.CurrentTransform().Apply(0, 0)
	if x != 10 || y != 5 {
		t.Errorf("composed transform = (%v,%v), want (10,5)", x, y)
	}
	r.PopTransform()
	if x, y := r.CurrentTransform().Apply(0, 0); x != 10 || y != 0 {
		t.Errorf("after one pop = (%v,%v), want (10,0)", x, y)
	}
	r.Begin(100, 100)
	if !r.CurrentTransform().IsIdentity() {
		t.Error("Begin should clear the transform stack")
	}
}

// shearDecompose splits any 2x2 into a scale, a shear and a rotation — the
// pieces the SDF quad shader applies — and loses nothing: rebuilding the
// matrix from them returns the original. That is what lets a quad carry the
// projection of a rotation about a DIAGONAL axis, which shears.
func TestShearDecomposeRebuildsTheMatrix(t *testing.T) {
	for _, m := range []Affine2{
		{A: 1, D: 1},         // identity
		Rotate2(0.7, 0, 0),   // pure rotation
		Scale2(2, 0.5, 0, 0), // pure scale
		{A: 0.8536, B: 0.1464, C: 0.1464, D: 0.8536}, // a 45° turn about (1,1,0)
		{A: 1, B: 0, C: 0.4, D: 1},                   // pure shear
		{A: 1, B: 0, C: 0, D: -0.5},                  // mirrored (past 90°)
	} {
		sx, sy, shear, rot := m.shearDecompose()
		// Rebuild: rotate(rot) ∘ [[sx, shear*sy], [0, sy]]
		s, c := float32(math.Sin(float64(rot))), float32(math.Cos(float64(rot)))
		a := sx * c
		b := sx * s
		cc := shear*sy*c - sy*s
		d := shear*sy*s + sy*c
		for i, pair := range [][2]float32{{a, m.A}, {b, m.B}, {cc, m.C}, {d, m.D}} {
			if diff := pair[0] - pair[1]; diff > 1e-3 || diff < -1e-3 {
				t.Errorf("%+v: component %d rebuilt as %v, want %v", m, i, pair[0], pair[1])
			}
		}
	}
}
