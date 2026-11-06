package p3

import (
	"math/rand"
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
)

func near(t *testing.T, got, want float32, eps float32, what string) {
	t.Helper()
	if math32.Abs(got-want) > eps {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

func TestRayCastShapes(t *testing.T) {
	w := NewWorld()
	id := core.QuatIdentity()
	sphere := w.AddBody(Sphere(1), core.V3(0, 0, 10), id, 1)
	w.AddBody(Box(core.V3(1, 2, 1)), core.V3(5, 0, 10), id, 1)
	w.AddBody(Capsule(0.5, 1), core.V3(-5, 0, 10), id, 1)

	// Sphere: straight on, front face at z=9.
	h, ok := w.RayCast(core.V3(0, 0, 0), core.V3(0, 0, 1), 100, LayerAll)
	if !ok || h.Body != sphere {
		t.Fatalf("sphere not hit: %+v %v", h, ok)
	}
	near(t, h.T, 9, 1e-3, "sphere T")
	near(t, h.Normal.Z, -1, 1e-3, "sphere normal.Z")

	// Box: front face at z=9, normal -Z.
	h, ok = w.RayCast(core.V3(5, 0, 0), core.V3(0, 0, 1), 100, LayerAll)
	if !ok {
		t.Fatal("box not hit")
	}
	near(t, h.T, 9, 1e-3, "box T")
	near(t, h.Normal.Z, -1, 1e-3, "box normal.Z")

	// Capsule side: front at z = 10-0.5.
	h, ok = w.RayCast(core.V3(-5, 0.5, 0), core.V3(0, 0, 1), 100, LayerAll)
	if !ok {
		t.Fatal("capsule side not hit")
	}
	near(t, h.T, 9.5, 1e-3, "capsule side T")
	// Capsule cap: from above, tip at y = 1+0.5.
	h, ok = w.RayCast(core.V3(-5, 10, 10), core.V3(0, -1, 0), 100, LayerAll)
	if !ok {
		t.Fatal("capsule cap not hit")
	}
	near(t, h.T, 10-1.5, 1e-3, "capsule cap T")
	near(t, h.Normal.Y, 1, 1e-3, "capsule cap normal.Y")

	// Miss entirely.
	if _, ok := w.RayCast(core.V3(0, 50, 0), core.V3(0, 0, 1), 100, LayerAll); ok {
		t.Fatal("miss reported hit")
	}
	// Range-limited.
	if _, ok := w.RayCast(core.V3(0, 0, 0), core.V3(0, 0, 1), 5, LayerAll); ok {
		t.Fatal("hit beyond maxDist reported")
	}
}

func TestRayCastRotatedBoxAndLayers(t *testing.T) {
	w := NewWorld()
	// A box rotated 45° about Y: its face plane now diagonal; a ray along +Z
	// from x=0 hits the rotated corner region closer than the axis-aligned
	// face would be.
	rot := core.QuatAxisAngle(core.V3(0, 1, 0), math32.Pi/4)
	w.AddBody(Box(core.V3(1, 1, 1)), core.V3(0, 0, 10), rot, 0b01)
	h, ok := w.RayCast(core.V3(0, 0, 0), core.V3(0, 0, 1), 100, LayerAll)
	if !ok {
		t.Fatal("rotated box not hit")
	}
	// Corner toward the ray sits at z = 10 - sqrt(2).
	near(t, h.T, 10-math32.Sqrt2, 1e-2, "rotated box T")
	// The hit normal must face the ray.
	if h.Normal.Z >= 0 {
		t.Fatalf("normal faces away: %+v", h.Normal)
	}
	// Layer filtering: mask 0b10 must not see the 0b01 body.
	if _, ok := w.RayCast(core.V3(0, 0, 0), core.V3(0, 0, 1), 100, 0b10); ok {
		t.Fatal("layer mask ignored")
	}
}

func TestMeshRayAndRemove(t *testing.T) {
	// Two triangles forming a unit quad in the XY plane at z=5.
	verts := []core.Vec3{{X: -1, Y: -1, Z: 5}, {X: 1, Y: -1, Z: 5}, {X: 1, Y: 1, Z: 5}, {X: -1, Y: 1, Z: 5}}
	mesh := NewTriMesh(verts, []uint32{0, 1, 2, 0, 2, 3})
	w := NewWorld()
	id := w.AddBody(Mesh(mesh), core.Vec3{}, core.QuatIdentity(), 1)

	h, ok := w.RayCast(core.V3(0.2, 0.3, 0), core.V3(0, 0, 1), 100, LayerAll)
	if !ok {
		t.Fatal("mesh not hit")
	}
	near(t, h.T, 5, 1e-3, "mesh T")
	if h.Normal.Z >= 0 {
		t.Fatalf("mesh normal faces away: %+v", h.Normal)
	}
	// Removal invalidates the handle and the geometry.
	w.RemoveBody(id)
	if _, ok := w.RayCast(core.V3(0.2, 0.3, 0), core.V3(0, 0, 1), 100, LayerAll); ok {
		t.Fatal("removed body still hit")
	}
	if _, _, ok := w.Pose(id); ok {
		t.Fatal("stale handle resolved")
	}
}

func TestOverlapSphere(t *testing.T) {
	w := NewWorld()
	id := core.QuatIdentity()
	a := w.AddBody(Sphere(1), core.V3(0, 0, 0), id, 0b01)
	w.AddBody(Sphere(1), core.V3(10, 0, 0), id, 0b01)
	c := w.AddBody(Box(core.V3(1, 1, 1)), core.V3(3, 0, 0), id, 0b10)

	got := map[BodyID]bool{}
	w.OverlapSphere(core.V3(1.5, 0, 0), 1.0, LayerAll, func(b BodyID, p core.Vec3) {
		got[b] = true
	})
	// Sphere a (surface at x=1, dist 0.5) and box c (face at x=2, dist 0.5);
	// traversal order is unspecified.
	if len(got) != 2 || !got[a] || !got[c] {
		t.Fatalf("overlap = %+v, want {a, c}", got)
	}
	// Mask: only layer 0b10.
	got = map[BodyID]bool{}
	w.OverlapSphere(core.V3(1.5, 0, 0), 1.0, 0b10, func(b BodyID, p core.Vec3) { got[b] = true })
	if len(got) != 1 || !got[c] {
		t.Fatalf("masked overlap = %+v, want {c}", got)
	}
	// Far point overlaps nothing.
	w.OverlapSphere(core.V3(0, 50, 0), 1, LayerAll, func(BodyID, core.Vec3) { t.Fatal("false overlap") })
}

func TestCastSphere(t *testing.T) {
	w := NewWorld()
	id := core.QuatIdentity()
	wall := w.AddBody(Box(core.V3(0.5, 5, 5)), core.V3(10, 0, 0), id, 1)

	// A unit sphere cast along +X stops when its surface touches x=9.5:
	// center at 8.5.
	h, ok := w.CastSphere(core.V3(0, 0, 0), 1, core.V3(1, 0, 0), 20, LayerAll)
	if !ok || h.Body != wall {
		t.Fatalf("cast missed the wall: %+v %v", h, ok)
	}
	near(t, h.T, 8.5, 5e-3, "cast T")
	near(t, h.Normal.X, -1, 1e-2, "cast normal.X")

	// Out of range: stops clean.
	if _, ok := w.CastSphere(core.V3(0, 0, 0), 1, core.V3(1, 0, 0), 5, LayerAll); ok {
		t.Fatal("cast hit beyond range")
	}
	// Sideways: parallel to the wall, never contacts.
	if _, ok := w.CastSphere(core.V3(0, 0, 0), 1, core.V3(0, 0, 1), 20, LayerAll); ok {
		t.Fatal("parallel cast reported hit")
	}
}

// TestRayFuzzBVH cross-checks the BVH path against a brute-force scan over
// randomized worlds: both must agree on hit body and distance.
func TestRayFuzzBVH(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	rv := func(scale float32) core.Vec3 {
		return core.V3((rng.Float32()*2-1)*scale, (rng.Float32()*2-1)*scale, (rng.Float32()*2-1)*scale)
	}
	for trial := 0; trial < 20; trial++ {
		w := NewWorld()
		type ref struct {
			id BodyID
		}
		var bodies []ref
		for i := 0; i < 40; i++ {
			pos := rv(20)
			rot := core.QuatAxisAngle(rv(1).Normalize(), rng.Float32()*3)
			var s Shape
			switch i % 4 {
			case 0:
				s = Sphere(0.3 + rng.Float32())
			case 1:
				s = Box(core.V3(0.3+rng.Float32(), 0.3+rng.Float32(), 0.3+rng.Float32()))
			case 2:
				// Hull of jittered box corners (always full-volume).
				pts := boxCorners(core.V3(0.3+rng.Float32(), 0.3+rng.Float32(), 0.3+rng.Float32()))
				for j := range pts {
					pts[j] = pts[j].Add(rv(0.1))
				}
				s = Hull(NewConvexHull(pts))
			default:
				s = Capsule(0.2+rng.Float32()*0.5, 0.3+rng.Float32())
			}
			bodies = append(bodies, ref{w.AddBody(s, pos, rot, 1)})
		}
		for ray := 0; ray < 50; ray++ {
			origin := rv(25)
			dir := rv(1)
			if dir.Length() < 1e-3 {
				continue
			}
			dir = dir.Normalize()
			got, gotOK := w.RayCast(origin, dir, 100, LayerAll)

			// Brute force: intersect every body directly.
			bestT := float32(100)
			bestB := NilBody
			bruteOK := false
			for i := range w.bodies {
				b := &w.bodies[i]
				if !b.live {
					continue
				}
				lo := b.toLocal(origin)
				ld := b.inv.Rotate(dir)
				if bt, _, hit := rayShape(&b.shape, lo, ld, bestT); hit && bt < bestT {
					bestT = bt
					bestB = BodyID{index: int32(i), gen: b.gen}
					bruteOK = true
				}
			}
			if gotOK != bruteOK {
				t.Fatalf("trial %d ray %d: bvh ok=%v brute ok=%v", trial, ray, gotOK, bruteOK)
			}
			if gotOK && (got.Body != bestB || math32.Abs(got.T-bestT) > 1e-3) {
				t.Fatalf("trial %d ray %d: bvh (%v, %v) vs brute (%v, %v)",
					trial, ray, got.Body, got.T, bestB, bestT)
			}
		}
	}
}
