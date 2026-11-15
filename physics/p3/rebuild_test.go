package p3

import (
	"math/rand"
	"testing"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// rebuildWorld is n unit spheres for timing the BVH rebuild that every
// pose change triggers: sorted puts them in a line already ordered on the
// split axis, otherwise they are scattered.
func rebuildWorld(n int, sorted bool) *World {
	w := NewWorld()
	rng := rand.New(rand.NewSource(7))
	for i := range n {
		pos := core.V3(float32(i)*3, 0, 0)
		if !sorted {
			pos = core.V3(rng.Float32()*1000, rng.Float32()*1000, rng.Float32()*1000)
		}
		w.AddBody(Sphere(1), pos, core.QuatIdentity(), 1)
	}
	return w
}

func benchRebuild(b *testing.B, n int, sorted bool) {
	w := rebuildWorld(n, sorted)
	b.ReportAllocs()
	for b.Loop() {
		w.dirty = true
		w.refresh()
	}
}

func BenchmarkBVHRebuildRandom400(b *testing.B) { benchRebuild(b, 400, false) }
func BenchmarkBVHRebuildRandom2k(b *testing.B)  { benchRebuild(b, 2000, false) }
func BenchmarkBVHRebuildRandom20k(b *testing.B) { benchRebuild(b, 20000, false) }
func BenchmarkBVHRebuildSorted20k(b *testing.B) { benchRebuild(b, 20000, true) }

// The rebuild orders bodies the way it always has, by a stable sort on the
// split axis: the leaf order decides the order queries and contacts are
// visited in, which a simulation's repeatability rests on.
func TestBVHLeafOrderIsStable(t *testing.T) {
	w := rebuildWorld(500, false)
	// Coincident centres: only a stable sort keeps their insertion order.
	for range 40 {
		w.AddBody(Sphere(1), core.V3(500, 500, 500), core.QuatIdentity(), 1)
	}
	w.dirty = true
	w.refresh()
	got := append([]int32(nil), w.bvh.order...)

	// The reference: the same recursion with a plain insertion sort.
	want := make([]int32, 0, len(got))
	for i := range w.bodies {
		if w.bodies[i].live {
			want = append(want, int32(i))
		}
	}
	var build func(span []int32)
	build = func(span []int32) {
		if len(span) <= 2 {
			return
		}
		bmin := core.V3(math32.MaxFloat32, math32.MaxFloat32, math32.MaxFloat32)
		bmax := bmin.Scale(-1)
		for _, bi := range span {
			bmin = vmin(bmin, w.bodies[bi].bmin)
			bmax = vmax(bmax, w.bodies[bi].bmax)
		}
		ext := bmax.Sub(bmin)
		key := func(bi int32) float32 { return w.bodies[bi].bmin.X + w.bodies[bi].bmax.X }
		if ext.Y > ext.X {
			key = func(bi int32) float32 { return w.bodies[bi].bmin.Y + w.bodies[bi].bmax.Y }
		}
		if ext.Z > ext.X && ext.Z > ext.Y {
			key = func(bi int32) float32 { return w.bodies[bi].bmin.Z + w.bodies[bi].bmax.Z }
		}
		for i := 1; i < len(span); i++ {
			v := span[i]
			j := i - 1
			for j >= 0 && key(span[j]) > key(v) {
				span[j+1] = span[j]
				j--
			}
			span[j+1] = v
		}
		build(span[:len(span)/2])
		build(span[len(span)/2:])
	}
	build(want)
	if len(got) != len(want) {
		t.Fatalf("%d bodies in the tree, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("leaf order differs at %d: body %d, want %d", i, got[i], want[i])
		}
	}
}
