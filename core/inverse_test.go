package core

import (
	"math"
	"testing"
)

func TestMat4Inverse(t *testing.T) {
	m := Mat4Translate(V3(3, -2, 5)).
		Mul(Mat4RotateY(0.7)).Mul(Mat4RotateX(-0.3)).
		Mul(Mat4Scale(V3(2, 0.5, 3)))
	inv, ok := m.Inverse()
	if !ok {
		t.Fatal("invertible matrix reported singular")
	}
	id := m.Mul(inv)
	for i, want := range Mat4Identity() {
		if math.Abs(float64(id[i]-want)) > 1e-4 {
			t.Fatalf("m*inv != I at %d: %v", i, id[i])
		}
	}
	// A perspective matrix must invert too (unprojection path).
	p := Mat4Perspective(1.1, 1.7, 0.1, 100)
	pinv, ok := p.Inverse()
	if !ok {
		t.Fatal("perspective reported singular")
	}
	pid := p.Mul(pinv)
	for i, want := range Mat4Identity() {
		if math.Abs(float64(pid[i]-want)) > 1e-3 {
			t.Fatalf("p*pinv != I at %d: %v", i, pid[i])
		}
	}
	// Singular: zero scale.
	if _, ok := Mat4Scale(V3(0, 1, 1)).Inverse(); ok {
		t.Fatal("singular matrix reported invertible")
	}
}
