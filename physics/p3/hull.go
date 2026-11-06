package p3

// Convex hull shape (P0): built once from a point cloud by quickhull.
// Hulls are authored small (tens of faces), so queries stay linear — rays
// clip against the deduped face planes (Cyrus–Beck), closest-point scans
// the hull triangles; no per-hull BVH and no GJK needed for static queries.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// ConvexHull is an immutable convex shape. Build with NewConvexHull.
type ConvexHull struct {
	verts      []core.Vec3
	tris       []int32 // triangle vertex indices, 3 per face
	planes     []hullPlane
	bmin, bmax core.Vec3
}

type hullPlane struct {
	n core.Vec3 // outward unit normal
	d float32   // n·x = d on the plane; n·x > d is outside
}

// Verts returns the hull's vertices (interior input points are pruned).
func (h *ConvexHull) Verts() []core.Vec3 { return h.verts }

// NewConvexHull computes the convex hull of points (quickhull). Returns nil
// when the cloud is degenerate — fewer than 4 points or no 3D volume —
// because a collision hull must enclose volume.
func NewConvexHull(points []core.Vec3) *ConvexHull {
	if len(points) < 4 {
		return nil
	}
	bmin, bmax := points[0], points[0]
	for _, p := range points[1:] {
		bmin, bmax = vmin(bmin, p), vmax(bmax, p)
	}
	ext := bmax.Sub(bmin)
	eps := 1e-5 * math32.Max(ext.X, math32.Max(ext.Y, ext.Z))
	if eps == 0 {
		return nil
	}

	// Initial tetrahedron: most distant pair among the axis extremes, the
	// point farthest from their line, then farthest from their plane.
	var ex [6]int32
	for i, p := range points {
		if p.X < points[ex[0]].X {
			ex[0] = int32(i)
		}
		if p.X > points[ex[1]].X {
			ex[1] = int32(i)
		}
		if p.Y < points[ex[2]].Y {
			ex[2] = int32(i)
		}
		if p.Y > points[ex[3]].Y {
			ex[3] = int32(i)
		}
		if p.Z < points[ex[4]].Z {
			ex[4] = int32(i)
		}
		if p.Z > points[ex[5]].Z {
			ex[5] = int32(i)
		}
	}
	var i0, i1 int32
	best := float32(-1)
	for a := 0; a < 6; a++ {
		for b := a + 1; b < 6; b++ {
			if d := points[ex[a]].Sub(points[ex[b]]).Length(); d > best {
				best, i0, i1 = d, ex[a], ex[b]
			}
		}
	}
	if best < eps {
		return nil
	}
	u := points[i1].Sub(points[i0]).Normalize()
	i2 := int32(-1)
	best = eps
	for i := range points {
		if d := points[i].Sub(points[i0]).Cross(u).Length(); d > best {
			best, i2 = d, int32(i)
		}
	}
	if i2 < 0 {
		return nil
	}
	pn := points[i1].Sub(points[i0]).Cross(points[i2].Sub(points[i0])).Normalize()
	pd := pn.Dot(points[i0])
	i3 := int32(-1)
	best = eps
	for i := range points {
		if d := math32.Abs(pn.Dot(points[i]) - pd); d > best {
			best, i3 = d, int32(i)
		}
	}
	if i3 < 0 {
		return nil
	}

	// Any interior point orients every face outward regardless of winding.
	interior := points[i0].Add(points[i1]).Add(points[i2]).Add(points[i3]).Scale(0.25)
	type face struct {
		v    [3]int32
		n    core.Vec3
		d    float32
		out  []int32
		far  int32
		farD float32
		dead bool
	}
	var faces []face
	addFace := func(a, b, c int32) bool {
		n := points[b].Sub(points[a]).Cross(points[c].Sub(points[a]))
		l := n.Length()
		if l < 1e-12 {
			return false
		}
		n = n.Scale(1 / l)
		d := n.Dot(points[a])
		if n.Dot(interior) > d {
			b, c = c, b
			n = n.Scale(-1)
			d = -d
		}
		faces = append(faces, face{v: [3]int32{a, b, c}, n: n, d: d})
		return true
	}
	assign := func(f *face, i int32) bool {
		if dist := f.n.Dot(points[i]) - f.d; dist > eps {
			f.out = append(f.out, i)
			if dist > f.farD {
				f.farD, f.far = dist, i
			}
			return true
		}
		return false
	}
	addFace(i0, i1, i2)
	addFace(i0, i1, i3)
	addFace(i0, i2, i3)
	addFace(i1, i2, i3)
	for i := range points {
		ii := int32(i)
		if ii == i0 || ii == i1 || ii == i2 || ii == i3 {
			continue
		}
		for f := range faces {
			if assign(&faces[f], ii) {
				break
			}
		}
	}

	// Expand: take a face's farthest outside point, remove every face it
	// sees, stitch new faces over the horizon (edges owned by exactly one
	// visible face), re-home the orphaned points (Barber's partition lemma:
	// new faces suffice).
	var visible, orphans []int32
	var edges [][2]int32
	for iter := 0; iter < 64+4*len(points); iter++ {
		fi := -1
		for i := range faces {
			if !faces[i].dead && len(faces[i].out) > 0 {
				fi = i
				break
			}
		}
		if fi < 0 {
			break
		}
		apex := faces[fi].far
		ap := points[apex]
		visible, orphans, edges = visible[:0], orphans[:0], edges[:0]
		for i := range faces {
			f := &faces[i]
			if !f.dead && f.n.Dot(ap)-f.d > 0 {
				visible = append(visible, int32(i))
			}
		}
		for _, vi := range visible {
			f := &faces[vi]
			edges = append(edges, [2]int32{f.v[0], f.v[1]}, [2]int32{f.v[1], f.v[2]}, [2]int32{f.v[2], f.v[0]})
			f.dead = true
			orphans = append(orphans, f.out...)
			f.out = nil
		}
		firstNew := len(faces)
		for _, e := range edges {
			twin := false
			for _, o := range edges {
				if o[0] == e[1] && o[1] == e[0] {
					twin = true
					break
				}
			}
			if !twin { // horizon edge, winding preserved
				addFace(e[0], e[1], apex)
			}
		}
		for _, oi := range orphans {
			if oi == apex {
				continue
			}
			for f := firstNew; f < len(faces); f++ {
				if assign(&faces[f], oi) {
					break
				}
			}
		}
	}

	// Compact live faces: remap vertices, dedupe coplanar face planes.
	h := &ConvexHull{}
	remap := make(map[int32]int32, len(points))
	for i := range faces {
		f := &faces[i]
		if f.dead {
			continue
		}
		for _, v := range f.v {
			nv, ok := remap[v]
			if !ok {
				nv = int32(len(h.verts))
				remap[v] = nv
				h.verts = append(h.verts, points[v])
			}
			h.tris = append(h.tris, nv)
		}
		dup := false
		for p := range h.planes {
			if h.planes[p].n.Dot(f.n) > 1-1e-5 && math32.Abs(h.planes[p].d-f.d) <= 4*eps {
				dup = true
				break
			}
		}
		if !dup {
			h.planes = append(h.planes, hullPlane{n: f.n, d: f.d})
		}
	}
	if len(h.planes) < 4 {
		return nil
	}
	h.bmin, h.bmax = h.verts[0], h.verts[0]
	for _, p := range h.verts[1:] {
		h.bmin, h.bmax = vmin(h.bmin, p), vmax(h.bmax, p)
	}
	return h
}

// raycast clips the ray against the face planes. Origin inside returns t=0
// with the nearest face's normal (solids report 0 when starting inside,
// matching sphere/box).
func (h *ConvexHull) raycast(o, d core.Vec3, maxT float32) (float32, core.Vec3, bool) {
	tmin, tmax := float32(0), maxT
	var enterN, inN core.Vec3
	entered := false
	inD := float32(-math32.MaxFloat32)
	for i := range h.planes {
		pl := &h.planes[i]
		dist := pl.n.Dot(o) - pl.d
		if dist > inD {
			inD, inN = dist, pl.n
		}
		dn := pl.n.Dot(d)
		if math32.Abs(dn) < 1e-9 {
			if dist > 0 {
				return 0, core.Vec3{}, false // parallel outside a face plane
			}
			continue
		}
		t := -dist / dn
		if dn < 0 {
			if t > tmin {
				tmin, enterN, entered = t, pl.n, true
			}
		} else if t < tmax {
			tmax = t
		}
		if tmin > tmax {
			return 0, core.Vec3{}, false
		}
	}
	if inD <= 0 {
		return 0, inN, true
	}
	if !entered {
		return 0, core.Vec3{}, false
	}
	return tmin, enterN, true
}

// closest returns the closest point on the hull to p (p itself when inside).
func (h *ConvexHull) closest(p core.Vec3) core.Vec3 {
	inside := true
	for i := range h.planes {
		if h.planes[i].n.Dot(p) > h.planes[i].d {
			inside = false
			break
		}
	}
	if inside {
		return p
	}
	best := p
	bestD := float32(math32.MaxFloat32)
	for t := 0; t < len(h.tris); t += 3 {
		q := core.ClosestOnTriangle(p, h.verts[h.tris[t]], h.verts[h.tris[t+1]], h.verts[h.tris[t+2]])
		if d := q.Sub(p).Length(); d < bestD {
			bestD, best = d, q
		}
	}
	return best
}
