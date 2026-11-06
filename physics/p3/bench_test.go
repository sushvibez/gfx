package p3

import (
	"math/rand"
	"testing"

	"github.com/sushvibez/gfx/core"
)

// benchWorld is a small-level workload: a 20×20-quad floor mesh plus ~400 mixed
// props (boxes, spheres, capsules, hulls) scattered over 100×100 m.
func benchWorld() *World {
	w := NewWorld()
	id := core.QuatIdentity()

	const n, ext = 20, float32(50)
	var verts []core.Vec3
	var idx []uint32
	for z := 0; z <= n; z++ {
		for x := 0; x <= n; x++ {
			verts = append(verts, core.V3(-ext+2*ext*float32(x)/n, 0, -ext+2*ext*float32(z)/n))
		}
	}
	for z := 0; z < n; z++ {
		for x := 0; x < n; x++ {
			a := uint32(z*(n+1) + x)
			b, c, d := a+1, a+n+2, a+n+1
			idx = append(idx, a, b, c, a, c, d)
		}
	}
	w.AddBody(Mesh(NewTriMesh(verts, idx)), core.Vec3{}, id, 1)

	rng := rand.New(rand.NewSource(1))
	rv := func(s float32) float32 { return (rng.Float32()*2 - 1) * s }
	hull := NewConvexHull(boxCorners(core.V3(0.5, 0.5, 0.5)))
	for i := 0; i < 400; i++ {
		pos := core.V3(rv(45), 0.5+rng.Float32()*3, rv(45))
		rot := core.QuatAxisAngle(core.V3(0, 1, 0), rng.Float32()*3)
		switch i % 4 {
		case 0:
			w.AddBody(Box(core.V3(0.3+rng.Float32(), 0.3+rng.Float32(), 0.3+rng.Float32())), pos, rot, 1)
		case 1:
			w.AddBody(Sphere(0.3+rng.Float32()), pos, id, 1)
		case 2:
			w.AddBody(Capsule(0.2+rng.Float32()*0.4, 0.3+rng.Float32()), pos, id, 1)
		default:
			w.AddBody(Hull(hull), pos, rot, 1)
		}
	}
	return w
}

// TestQueryAllocs proves the hot query paths allocate nothing once the BVH
// is built (the repo-wide 0 allocs/op rule).
func TestQueryAllocs(t *testing.T) {
	w := benchWorld()
	w.RayCast(core.V3(0, 10, 0), core.V3(0, -1, 0), 100, LayerAll) // builds the BVH
	visit := func(BodyID, core.Vec3) {}
	dir := core.V3(0.3, -1, 0.2).Normalize()
	a, b := core.V3(2, 1, 2), core.V3(2, 2.6, 2)
	checks := []struct {
		name string
		f    func()
	}{
		{"RayCast", func() { w.RayCast(core.V3(0, 10, 0), dir, 200, LayerAll) }},
		{"ClosestPoint", func() { w.ClosestPoint(core.V3(2, 2, 2), 50, LayerAll) }},
		{"OverlapSphere", func() { w.OverlapSphere(core.V3(3, 1, 2), 2, LayerAll, visit) }},
		{"OverlapCapsule", func() { w.OverlapCapsule(a, b, 1.5, LayerAll, visit) }},
		{"CastSphere", func() { w.CastSphere(core.V3(0, 10, 0), 0.5, core.V3(0, -1, 0), 50, LayerAll) }},
		{"CastCapsule", func() {
			w.CastCapsule(a.Add(core.V3(0, 8, 0)), b.Add(core.V3(0, 8, 0)), 0.4, core.V3(0, -1, 0), 20, LayerAll)
		}},
	}
	for _, c := range checks {
		if got := testing.AllocsPerRun(200, c.f); got != 0 {
			t.Errorf("%s: %v allocs/op, want 0", c.name, got)
		}
	}
}

func BenchmarkRayCast(b *testing.B) {
	w := benchWorld()
	dir := core.V3(0.3, -1, 0.2).Normalize()
	w.RayCast(core.V3(0, 10, 0), dir, 200, LayerAll)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.RayCast(core.V3(0, 10, 0), dir, 200, LayerAll)
	}
}

// BenchmarkRayCast10k is PLAN.md's P0 budget: 10k rays per frame.
func BenchmarkRayCast10k(b *testing.B) {
	w := benchWorld()
	rng := rand.New(rand.NewSource(3))
	type ray struct{ o, d core.Vec3 }
	rays := make([]ray, 256)
	for i := range rays {
		o := core.V3((rng.Float32()*2-1)*40, 1+rng.Float32()*8, (rng.Float32()*2-1)*40)
		d := core.V3(rng.Float32()*2-1, rng.Float32()*2-1, rng.Float32()*2-1)
		for d.Length() < 1e-3 {
			d = core.V3(rng.Float32()*2-1, 1, rng.Float32()*2-1)
		}
		rays[i] = ray{o, d.Normalize()}
	}
	w.RayCast(rays[0].o, rays[0].d, 200, LayerAll)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 10000; j++ {
			r := &rays[j&255]
			w.RayCast(r.o, r.d, 200, LayerAll)
		}
	}
}

func BenchmarkCastCapsule(b *testing.B) {
	w := benchWorld()
	a, bb := core.V3(2, 9, 2), core.V3(2, 10.6, 2)
	w.CastCapsule(a, bb, 0.4, core.V3(0, -1, 0), 20, LayerAll)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.CastCapsule(a, bb, 0.4, core.V3(0, -1, 0), 20, LayerAll)
	}
}

func BenchmarkOverlapSphere(b *testing.B) {
	w := benchWorld()
	visit := func(BodyID, core.Vec3) {}
	w.OverlapSphere(core.V3(3, 1, 2), 2, LayerAll, visit)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.OverlapSphere(core.V3(3, 1, 2), 2, LayerAll, visit)
	}
}

func BenchmarkClosestPoint(b *testing.B) {
	w := benchWorld()
	w.ClosestPoint(core.V3(2, 2, 2), 50, LayerAll)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.ClosestPoint(core.V3(2, 2, 2), 50, LayerAll)
	}
}
