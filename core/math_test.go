package core

import (
	"math"
	"testing"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-5 }

func vecNear(a, b Vec3) bool { return near(a.X, b.X) && near(a.Y, b.Y) && near(a.Z, b.Z) }

func TestMat4MulIdentity(t *testing.T) {
	m := Mat4Translate(V3(1, 2, 3)).Mul(Mat4Identity())
	if !vecNear(m.MulPoint(V3(0, 0, 0)), V3(1, 2, 3)) {
		t.Fatalf("translate: %v", m.MulPoint(V3(0, 0, 0)))
	}
}

func TestMat4Compose(t *testing.T) {
	// Scale then translate: T*S applied to (1,1,1) -> (2,2,2)+(10,0,0).
	m := Mat4Translate(V3(10, 0, 0)).Mul(Mat4Scale(V3(2, 2, 2)))
	if got := m.MulPoint(V3(1, 1, 1)); !vecNear(got, V3(12, 2, 2)) {
		t.Fatalf("got %v", got)
	}
}

func TestMat4RotateZ(t *testing.T) {
	m := Mat4RotateZ(math.Pi / 2)
	if got := m.MulPoint(V3(1, 0, 0)); !vecNear(got, V3(0, 1, 0)) {
		t.Fatalf("rotZ(90)*x = %v, want y", got)
	}
}

func TestPerspectiveDepthRange(t *testing.T) {
	// WebGPU clip space: znear -> z/w = 0, zfar -> z/w = 1 (looking down -Z).
	p := Mat4Perspective(math.Pi/2, 1, 0.1, 100)
	nearClip := p.MulVec4(V4(0, 0, -0.1, 1))
	farClip := p.MulVec4(V4(0, 0, -100, 1))
	if !near(nearClip.Z/nearClip.W, 0) {
		t.Fatalf("near plane z/w = %v, want 0", nearClip.Z/nearClip.W)
	}
	if !near(farClip.Z/farClip.W, 1) {
		t.Fatalf("far plane z/w = %v, want 1", farClip.Z/farClip.W)
	}
}

func TestOrthoUITopLeftOrigin(t *testing.T) {
	// QML-style: (0,0) top-left, (w,h) bottom-right.
	p := Mat4Ortho(0, 800, 600, 0, -1, 1)
	tl := p.MulPoint(V3(0, 0, 0))
	br := p.MulPoint(V3(800, 600, 0))
	if !near(tl.X, -1) || !near(tl.Y, 1) {
		t.Fatalf("top-left -> %v, want (-1,1)", tl)
	}
	if !near(br.X, 1) || !near(br.Y, -1) {
		t.Fatalf("bottom-right -> %v, want (1,-1)", br)
	}
}

func TestLookAt(t *testing.T) {
	v := Mat4LookAt(V3(0, 0, 5), V3(0, 0, 0), V3(0, 1, 0))
	// The look-at target ends up on -Z in view space.
	if got := v.MulPoint(V3(0, 0, 0)); !vecNear(got, V3(0, 0, -5)) {
		t.Fatalf("center in view space = %v, want (0,0,-5)", got)
	}
}

func TestQuatRotate(t *testing.T) {
	q := QuatAxisAngle(V3(0, 0, 1), math.Pi/2)
	if got := q.Rotate(V3(1, 0, 0)); !vecNear(got, V3(0, 1, 0)) {
		t.Fatalf("quat rotZ(90)*x = %v, want y", got)
	}
	// Quaternion and its matrix must agree.
	if got := q.Mat4().MulPoint(V3(1, 0, 0)); !vecNear(got, V3(0, 1, 0)) {
		t.Fatalf("quat.Mat4() disagrees: %v", got)
	}
}

func TestQuatSlerpEndpoints(t *testing.T) {
	a := QuatIdentity()
	b := QuatAxisAngle(V3(0, 1, 0), math.Pi/2)
	if got := a.Slerp(b, 0).Rotate(V3(1, 0, 0)); !vecNear(got, V3(1, 0, 0)) {
		t.Fatalf("slerp t=0: %v", got)
	}
	if got := a.Slerp(b, 1).Rotate(V3(1, 0, 0)); !vecNear(got, V3(0, 0, -1)) {
		t.Fatalf("slerp t=1: %v", got)
	}
	mid := a.Slerp(b, 0.5).Rotate(V3(1, 0, 0))
	want := V3(sqrt(0.5), 0, -sqrt(0.5))
	if !vecNear(mid, want) {
		t.Fatalf("slerp t=0.5: %v, want %v", mid, want)
	}
}

func TestVec3Basics(t *testing.T) {
	if got := V3(1, 0, 0).Cross(V3(0, 1, 0)); !vecNear(got, V3(0, 0, 1)) {
		t.Fatalf("cross: %v", got)
	}
	if !near(V3(3, 4, 0).Length(), 5) {
		t.Fatal("length")
	}
	if got := V3(0, 3, 0).Normalize(); !vecNear(got, V3(0, 1, 0)) {
		t.Fatalf("normalize: %v", got)
	}
	if got := (Vec3{}).Normalize(); !vecNear(got, Vec3{}) {
		t.Fatal("normalize zero vector must not NaN")
	}
}
