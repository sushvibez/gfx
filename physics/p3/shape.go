// Package p3 is the 3D physics layer, phase P0 (see physics/PLAN.md):
// collision shapes over a static world with ray/overlap/shape-cast queries
// and collision layers — what a kinematic game (a first-person trainer) needs
// before any dynamics. Bodies live in flat slices addressed by generation
// handles; queries allocate nothing on the hot path.
package p3

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// ShapeKind tags the shape union.
type ShapeKind uint8

const (
	KindSphere ShapeKind = iota
	KindCapsule
	KindBox
	KindMesh
	KindHull
)

// Shape is a tagged union: one struct covers every kind so bodies stay
// value types in flat storage. Capsules run along local Y.
type Shape struct {
	Kind       ShapeKind
	R          float32     // sphere/capsule radius
	HalfHeight float32     // capsule cylinder half-length (along Y)
	Half       core.Vec3   // box half extents
	Mesh       *TriMesh    // static triangle soup (KindMesh)
	Hull       *ConvexHull // convex point set (KindHull)
}

// Sphere builds a sphere shape.
func Sphere(r float32) Shape { return Shape{Kind: KindSphere, R: r} }

// Capsule builds a Y-axis capsule: a segment of half-length hh with radius r
// (total height 2*hh + 2*r).
func Capsule(r, hh float32) Shape { return Shape{Kind: KindCapsule, R: r, HalfHeight: hh} }

// Box builds a box shape from half extents.
func Box(half core.Vec3) Shape { return Shape{Kind: KindBox, Half: half} }

// Mesh builds a static triangle-mesh shape (world geometry).
func Mesh(m *TriMesh) Shape { return Shape{Kind: KindMesh, Mesh: m} }

// Hull builds a convex-hull shape (see NewConvexHull).
func Hull(h *ConvexHull) Shape { return Shape{Kind: KindHull, Hull: h} }

// localAABB returns the shape's model-space bounds.
func (s *Shape) localAABB() (core.Vec3, core.Vec3) {
	switch s.Kind {
	case KindSphere:
		e := core.V3(s.R, s.R, s.R)
		return e.Scale(-1), e
	case KindCapsule:
		e := core.V3(s.R, s.HalfHeight+s.R, s.R)
		return e.Scale(-1), e
	case KindBox:
		return s.Half.Scale(-1), s.Half
	case KindMesh:
		if s.Mesh != nil {
			return s.Mesh.bmin, s.Mesh.bmax
		}
	case KindHull:
		if s.Hull != nil {
			return s.Hull.bmin, s.Hull.bmax
		}
	}
	return core.Vec3{}, core.Vec3{}
}

// closestLocal returns the closest point on the shape's surface-or-interior
// to a model-space point (interior points return themselves for solids —
// what conservative-advancement casting needs).
func (s *Shape) closestLocal(p core.Vec3) core.Vec3 {
	switch s.Kind {
	case KindSphere:
		d := p.Length()
		if d <= s.R || d == 0 {
			return p
		}
		return p.Scale(s.R / d)
	case KindCapsule:
		seg := core.ClosestOnSegment(p, core.V3(0, -s.HalfHeight, 0), core.V3(0, s.HalfHeight, 0))
		d := p.Sub(seg)
		l := d.Length()
		if l <= s.R || l == 0 {
			return p
		}
		return seg.Add(d.Scale(s.R / l))
	case KindBox:
		return core.V3(
			clamp(p.X, -s.Half.X, s.Half.X),
			clamp(p.Y, -s.Half.Y, s.Half.Y),
			clamp(p.Z, -s.Half.Z, s.Half.Z))
	case KindMesh:
		if s.Mesh != nil {
			return s.Mesh.closest(p)
		}
	case KindHull:
		if s.Hull != nil {
			return s.Hull.closest(p)
		}
	}
	return p
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// TriMesh is static triangle geometry with its own triangle BVH.
type TriMesh struct {
	verts      []core.Vec3
	idx        []uint32
	bmin, bmax core.Vec3
	nodes      []meshNode // triangle BVH (flat)
	order      []int32    // triangle order after the BVH build
}

type meshNode struct {
	bmin, bmax  core.Vec3
	start, n    int32 // leaf: triangle range in order
	left, right int32 // internal: child node indices (-1 for leaf)
}

// NewTriMesh builds a mesh shape from indexed triangles.
func NewTriMesh(verts []core.Vec3, idx []uint32) *TriMesh {
	m := &TriMesh{verts: verts, idx: idx}
	nt := len(idx) / 3
	m.order = make([]int32, nt)
	cents := make([]core.Vec3, nt)
	for t := range m.order {
		m.order[t] = int32(t)
		a, b, c := m.tri(int32(t))
		cents[t] = a.Add(b).Add(c).Scale(1.0 / 3)
	}
	if nt > 0 {
		m.build(0, int32(nt), cents)
		m.bmin, m.bmax = m.nodes[0].bmin, m.nodes[0].bmax
	}
	return m
}

func (m *TriMesh) tri(t int32) (a, b, c core.Vec3) {
	return m.verts[m.idx[t*3]], m.verts[m.idx[t*3+1]], m.verts[m.idx[t*3+2]]
}

// build creates a BVH node over order[start:start+n]; returns its index.
func (m *TriMesh) build(start, n int32, cents []core.Vec3) int32 {
	self := int32(len(m.nodes))
	m.nodes = append(m.nodes, meshNode{left: -1, right: -1})
	// Bounds over the range.
	bmin := core.V3(math32.MaxFloat32, math32.MaxFloat32, math32.MaxFloat32)
	bmax := bmin.Scale(-1)
	for i := start; i < start+n; i++ {
		a, b, c := m.tri(m.order[i])
		for _, p := range [3]core.Vec3{a, b, c} {
			bmin = vmin(bmin, p)
			bmax = vmax(bmax, p)
		}
	}
	nd := &m.nodes[self]
	nd.bmin, nd.bmax = bmin, bmax
	const leafSize = 4
	if n <= leafSize {
		nd.start, nd.n = start, n
		return self
	}
	// Median split on the widest centroid axis.
	ext := bmax.Sub(bmin)
	axis := 0
	if ext.Y > ext.X {
		axis = 1
	}
	if ext.Z > ext.X && ext.Z > ext.Y {
		axis = 2
	}
	span := m.order[start : start+n]
	sortByCentroid(span, cents, axis)
	half := n / 2
	l := m.build(start, half, cents)
	r := m.build(start+half, n-half, cents)
	// nd may have moved (append); re-take the pointer.
	nd = &m.nodes[self]
	nd.left, nd.right = l, r
	return self
}

// sortByCentroid orders a triangle span by centroid on one axis (insertion
// sort: spans are small after a few splits and it avoids allocations).
func sortByCentroid(span []int32, cents []core.Vec3, axis int) {
	key := func(t int32) float32 {
		c := cents[t]
		switch axis {
		case 0:
			return c.X
		case 1:
			return c.Y
		}
		return c.Z
	}
	for i := 1; i < len(span); i++ {
		v := span[i]
		k := key(v)
		j := i - 1
		for j >= 0 && key(span[j]) > k {
			span[j+1] = span[j]
			j--
		}
		span[j+1] = v
	}
}

// raycast returns the nearest triangle hit ≤ maxT (t, normal, ok).
func (m *TriMesh) raycast(origin, dir core.Vec3, maxT float32) (float32, core.Vec3, bool) {
	if len(m.nodes) == 0 {
		return 0, core.Vec3{}, false
	}
	bestT := maxT
	var bestN core.Vec3
	found := false
	var stack [64]int32
	sp := 0
	stack[sp] = 0
	sp++
	for sp > 0 {
		sp--
		nd := &m.nodes[stack[sp]]
		if et, hit := core.RayAABB(origin, dir, nd.bmin, nd.bmax); !hit || et > bestT {
			continue
		}
		if nd.left < 0 { // leaf
			for i := nd.start; i < nd.start+nd.n; i++ {
				a, b, c := m.tri(m.order[i])
				if t, hit := core.RayTriangle(origin, dir, a, b, c); hit && t < bestT {
					bestT = t
					bestN = b.Sub(a).Cross(c.Sub(a)).Normalize()
					found = true
				}
			}
			continue
		}
		if sp+2 <= len(stack) {
			stack[sp] = nd.left
			stack[sp+1] = nd.right
			sp += 2
		}
	}
	if !found {
		return 0, core.Vec3{}, false
	}
	return bestT, bestN, true
}

// closest returns the closest point on the mesh surface to p.
func (m *TriMesh) closest(p core.Vec3) core.Vec3 {
	best := p
	bestD := float32(math32.MaxFloat32)
	if len(m.nodes) == 0 {
		return p
	}
	var stack [64]int32
	sp := 0
	stack[sp] = 0
	sp++
	for sp > 0 {
		sp--
		nd := &m.nodes[stack[sp]]
		// Prune nodes farther than the best found so far.
		if aabbDist(p, nd.bmin, nd.bmax) > bestD {
			continue
		}
		if nd.left < 0 {
			for i := nd.start; i < nd.start+nd.n; i++ {
				a, b, c := m.tri(m.order[i])
				q := core.ClosestOnTriangle(p, a, b, c)
				if d := q.Sub(p).Length(); d < bestD {
					bestD, best = d, q
				}
			}
			continue
		}
		if sp+2 <= len(stack) {
			stack[sp] = nd.left
			stack[sp+1] = nd.right
			sp += 2
		}
	}
	return best
}

// aabbDist is the distance from p to a box (0 inside).
func aabbDist(p, bmin, bmax core.Vec3) float32 {
	dx := math32.Max(math32.Max(bmin.X-p.X, 0), p.X-bmax.X)
	dy := math32.Max(math32.Max(bmin.Y-p.Y, 0), p.Y-bmax.Y)
	dz := math32.Max(math32.Max(bmin.Z-p.Z, 0), p.Z-bmax.Z)
	return math32.Sqrt(dx*dx + dy*dy + dz*dz)
}

func vmin(a, b core.Vec3) core.Vec3 {
	return core.V3(math32.Min(a.X, b.X), math32.Min(a.Y, b.Y), math32.Min(a.Z, b.Z))
}

func vmax(a, b core.Vec3) core.Vec3 {
	return core.V3(math32.Max(a.X, b.X), math32.Max(a.Y, b.Y), math32.Max(a.Z, b.Z))
}
