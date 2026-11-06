package ui

import (
	"math"
	"testing"
)

// TestTransformComposition: a QML transform list applies IN ORDER — the
// first entry acts on the item, the next on that result. A spinner's dots
// depend on it: translate out from the centre, THEN rotate about the centre.
func TestTransformComposition(t *testing.T) {
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }
	d := Div().Size(10, 10)
	d.layout(&Context{}, Rect{X: 100, Y: 100, W: 10, H: 10})
	d.Transform(Translate(0, -50))
	d.Transform(Rotation(90, 5, 5)) // about the item's own (5,5) = (105,105)

	m, ok := d.xform()
	if !ok {
		t.Fatal("no transform composed")
	}
	// The item's centre (105,105) translates to (105,55), then rotates 90°
	// about (105,105): in a y-down frame that lands 50px to the RIGHT.
	x, y := m.Apply(105, 105)
	if !near(x, 155) || !near(y, 105) {
		t.Errorf("centre maps to (%v,%v), want (155,105) — translate then rotate", x, y)
	}
	// Reversing the list gives the other composition, as it should.
	e := Div().Size(10, 10)
	e.layout(&Context{}, Rect{X: 100, Y: 100, W: 10, H: 10})
	e.Transform(Rotation(90, 5, 5))
	e.Transform(Translate(0, -50))
	m2, _ := e.xform()
	if x, y := m2.Apply(105, 105); !near(x, 105) || !near(y, 55) {
		t.Errorf("rotate then translate gives (%v,%v), want (105,55)", x, y)
	}
}

// TestRotateScaleAboutCentre: `rotation` and `scale` act about the item's
// centre and leave layout alone.
func TestRotateScaleAboutCentre(t *testing.T) {
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }
	d := Div().Size(40, 20).Rotate(90)
	d.layout(&Context{}, Rect{X: 0, Y: 0, W: 40, H: 20})
	m, ok := d.xform()
	if !ok {
		t.Fatal("rotation did not compose")
	}
	// The centre is fixed; the right edge midpoint (40,10) swings down.
	if x, y := m.Apply(20, 10); !near(x, 20) || !near(y, 10) {
		t.Errorf("centre moved to (%v,%v)", x, y)
	}
	if x, y := m.Apply(40, 10); !near(x, 20) || !near(y, 30) {
		t.Errorf("(40,10) maps to (%v,%v), want (20,30)", x, y)
	}
	// Layout is unaffected: measure still reports the box.
	if w, h := d.measure(&Context{}); w != 40 || h != 20 {
		t.Errorf("measure = %vx%v, want 40x20 — a transform must not change layout", w, h)
	}
	s := Div().Size(10, 10).ScaleBy(2)
	s.layout(&Context{}, Rect{X: 0, Y: 0, W: 10, H: 10})
	ms, _ := s.xform()
	if x, y := ms.Apply(0, 0); !near(x, -5) || !near(y, -5) {
		t.Errorf("scale about centre maps (0,0) to (%v,%v), want (-5,-5)", x, y)
	}
}

// QML's `Rotation { axis { x: 0; y: 1; z: 0 } }` turns an item about an axis
// in the PLANE, which tilts it away from the viewer. Qt projects that
// orthographically (MEASURED: a 100px square turned 30/60/80° about Y is
// 87/50/17px wide and full height), so the perpendicular dimension scales
// by cos(angle) — and past 90° it goes negative, mirroring the item.
func TestAxisRotationProjectsOrthographically(t *testing.T) {
	rect := Rect{0, 0, 100, 100}
	corner := func(tr Transform, x, y float32) (float32, float32) {
		m := tr.affine(rect, 50, 50)
		return m.Apply(x, y)
	}
	// About Y: x scales by cos, y untouched.
	for _, c := range []struct {
		deg, want float32
	}{{30, 0.866}, {60, 0.5}, {80, 0.1736}, {120, -0.5}} {
		tr := Rotation3D(c.deg, 50, 50, 0, 1, 0)
		x, y := corner(tr, 100, 100)
		wantX := 50 + 50*c.want
		if d := x - wantX; d > 0.5 || d < -0.5 {
			t.Errorf("%.0f° about Y: corner x=%v, want %v", c.deg, x, wantX)
		}
		if d := y - 100; d > 0.01 || d < -0.01 {
			t.Errorf("%.0f° about Y moved y to %v, want 100", c.deg, y)
		}
	}
	// About X: y scales by cos, x untouched.
	tr := Rotation3D(60, 50, 50, 1, 0, 0)
	x, y := corner(tr, 100, 100)
	if d := x - 100; d > 0.01 || d < -0.01 {
		t.Errorf("60° about X moved x to %v, want 100", x)
	}
	if d := y - 75; d > 0.5 || d < -0.5 {
		t.Errorf("60° about X: corner y=%v, want 75", y)
	}
	// A z-only axis is the plain in-plane rotation, and so is no axis.
	for _, tr := range []Transform{Rotation3D(90, 50, 50, 0, 0, 1), Rotation(90, 50, 50)} {
		x, y := corner(tr, 100, 50)
		if d := x - 50; d > 0.01 || d < -0.01 {
			t.Errorf("z rotation: x=%v, want 50", x)
		}
		if d := y - 100; d > 0.01 || d < -0.01 {
			t.Errorf("z rotation: y=%v, want 100", y)
		}
	}
}
