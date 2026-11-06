package p3

import (
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
)

// TestRayShapeTable drives rayShape through every shape kind's hit, miss and
// edge cases: parallel rays, grazing rays, origins inside (solids report
// t=0; meshes are two-sided surfaces and report the exit face).
func TestRayShapeTable(t *testing.T) {
	quad := NewTriMesh(
		[]core.Vec3{{X: -1, Y: -1, Z: 5}, {X: 1, Y: -1, Z: 5}, {X: 1, Y: 1, Z: 5}, {X: -1, Y: 1, Z: 5}},
		[]uint32{0, 1, 2, 0, 2, 3})
	cube := NewConvexHull(boxCorners(core.V3(1, 1, 1)))
	tet := NewConvexHull([]core.Vec3{{}, {X: 1}, {Y: 1}, {Z: 1}})
	if cube == nil || tet == nil {
		t.Fatal("hulls failed to build")
	}
	inv3 := 1 / math32.Sqrt(3)
	grazeT := 5 - math32.Sqrt(1-0.99*0.99) // sphere r=1, ray at x=0.99 from z=-5

	cases := []struct {
		name string
		s    Shape
		o, d core.Vec3
		hit  bool
		t    float32   // checked when hit and >= 0
		n    core.Vec3 // checked when non-zero
	}{
		{"sphere/direct", Sphere(1), core.V3(0, 0, -5), core.V3(0, 0, 1), true, 4, core.V3(0, 0, -1)},
		{"sphere/miss", Sphere(1), core.V3(0, 3, -5), core.V3(0, 0, 1), false, 0, core.Vec3{}},
		{"sphere/pointing-away", Sphere(1), core.V3(0, 0, 5), core.V3(0, 0, 1), false, 0, core.Vec3{}},
		{"sphere/graze-inside", Sphere(1), core.V3(0.99, 0, -5), core.V3(0, 0, 1), true, grazeT, core.Vec3{}},
		{"sphere/graze-outside", Sphere(1), core.V3(1.01, 0, -5), core.V3(0, 0, 1), false, 0, core.Vec3{}},
		{"sphere/inside", Sphere(1), core.V3(0.3, 0, 0), core.V3(0, 0, 1), true, 0, core.Vec3{}},

		{"box/direct", Box(core.V3(1, 2, 1)), core.V3(0, 0, -5), core.V3(0, 0, 1), true, 4, core.V3(0, 0, -1)},
		{"box/parallel-outside", Box(core.V3(1, 2, 1)), core.V3(0, 2.5, -5), core.V3(0, 0, 1), false, 0, core.Vec3{}},
		{"box/corner-graze-outside", Box(core.V3(1, 2, 1)), core.V3(1.05, 2.05, -5), core.V3(0, 0, 1), false, 0, core.Vec3{}},
		{"box/inside", Box(core.V3(1, 2, 1)), core.V3(0.5, 0.5, 0), core.V3(0, 0, 1), true, 0, core.Vec3{}},

		{"capsule/side", Capsule(0.5, 1), core.V3(-5, 0, 0), core.V3(1, 0, 0), true, 4.5, core.V3(-1, 0, 0)},
		{"capsule/cap", Capsule(0.5, 1), core.V3(0, 5, 0), core.V3(0, -1, 0), true, 3.5, core.V3(0, 1, 0)},
		{"capsule/axis-parallel-miss", Capsule(0.5, 1), core.V3(1, -5, 0), core.V3(0, 1, 0), false, 0, core.Vec3{}},
		{"capsule/axis-through", Capsule(0.5, 1), core.V3(0, -5, 0), core.V3(0, 1, 0), true, 3.5, core.V3(0, -1, 0)},
		{"capsule/graze-cylinder", Capsule(0.5, 1), core.V3(0.49, 0, -5), core.V3(0, 0, 1), true, -1, core.Vec3{}},
		{"capsule/graze-outside", Capsule(0.5, 1), core.V3(0.51, 0, -5), core.V3(0, 0, 1), false, 0, core.Vec3{}},
		{"capsule/inside", Capsule(0.5, 1), core.V3(0.1, 0.5, 0), core.V3(0, 0, 1), true, 0, core.Vec3{}},
		{"capsule/inside-cap", Capsule(0.5, 1), core.V3(0, 1.3, 0), core.V3(0, 1, 0), true, 0, core.Vec3{}},

		{"hull/direct", Hull(cube), core.V3(0, 0, -5), core.V3(0, 0, 1), true, 4, core.V3(0, 0, -1)},
		{"hull/parallel-outside", Hull(cube), core.V3(0, 1.5, -5), core.V3(0, 0, 1), false, 0, core.Vec3{}},
		{"hull/graze-inside", Hull(cube), core.V3(0.95, 0.95, -5), core.V3(0, 0, 1), true, 4, core.V3(0, 0, -1)},
		{"hull/graze-outside", Hull(cube), core.V3(1.05, 0, -5), core.V3(0, 0, 1), false, 0, core.Vec3{}},
		{"hull/inside", Hull(cube), core.V3(0.2, -0.3, 0), core.V3(0, 0, 1), true, 0, core.Vec3{}},
		{"hull/nil", Hull(nil), core.V3(0, 0, -5), core.V3(0, 0, 1), false, 0, core.Vec3{}},
		{"tet/slant-face", Hull(tet), core.V3(0.2, 0.2, 5), core.V3(0, 0, -1), true, 4.4, core.V3(inv3, inv3, inv3)},
		{"tet/miss", Hull(tet), core.V3(0.9, 0.9, 5), core.V3(0, 0, -1), false, 0, core.Vec3{}},

		{"mesh/front", Mesh(quad), core.V3(0.2, 0.3, 0), core.V3(0, 0, 1), true, 5, core.V3(0, 0, -1)},
		{"mesh/backface", Mesh(quad), core.V3(0.2, 0.3, 10), core.V3(0, 0, -1), true, 5, core.V3(0, 0, 1)},
		{"mesh/in-plane-parallel", Mesh(quad), core.V3(-3, 0, 5), core.V3(1, 0, 0), false, 0, core.Vec3{}},
		{"mesh/beside", Mesh(quad), core.V3(2, 2, 0), core.V3(0, 0, 1), false, 0, core.Vec3{}},
		{"mesh/nil", Mesh(nil), core.V3(0, 0, 0), core.V3(0, 0, 1), false, 0, core.Vec3{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gt, gn, hit := rayShape(&c.s, c.o, c.d, 100)
			if hit != c.hit {
				t.Fatalf("hit = %v, want %v (t=%v n=%+v)", hit, c.hit, gt, gn)
			}
			if !hit {
				return
			}
			if c.t >= 0 {
				near(t, gt, c.t, 1e-3, "t")
			}
			if c.n != (core.Vec3{}) && gn.Dot(c.n) < 0.999 {
				t.Fatalf("normal = %+v, want %+v", gn, c.n)
			}
		})
	}
}

// TestRayMaxDist: rayShape must respect the range for every kind.
func TestRayMaxDist(t *testing.T) {
	cube := NewConvexHull(boxCorners(core.V3(1, 1, 1)))
	quad := NewTriMesh(
		[]core.Vec3{{X: -1, Y: -1, Z: 5}, {X: 1, Y: -1, Z: 5}, {X: 1, Y: 1, Z: 5}, {X: -1, Y: 1, Z: 5}},
		[]uint32{0, 1, 2, 0, 2, 3})
	shapes := []Shape{Sphere(1), Box(core.V3(1, 1, 1)), Capsule(0.5, 1), Hull(cube), Mesh(quad)}
	for _, s := range shapes {
		// Everything above sits within t≈4..5.5 of this ray; range 2 misses all.
		if _, _, hit := rayShape(&s, core.V3(0, 0, -5), core.V3(0, 0, 1), 2); hit {
			t.Errorf("kind %d: hit beyond maxT", s.Kind)
		}
	}
}
