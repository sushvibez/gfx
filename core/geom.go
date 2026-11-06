package core

// Geometric intersection primitives shared by picking (render) and physics
// queries — one implementation of the raw math, wrapped by both layers.

import "github.com/chewxy/math32"

// RayAABB reports whether a ray hits the axis-aligned box, and the entry
// distance along dir (0 when the origin starts inside). Slab method.
func RayAABB(origin, dir, bmin, bmax Vec3) (float32, bool) {
	tmin, tmax := float32(0), float32(math32.MaxFloat32)
	o := [3]float32{origin.X, origin.Y, origin.Z}
	d := [3]float32{dir.X, dir.Y, dir.Z}
	lo := [3]float32{bmin.X, bmin.Y, bmin.Z}
	hi := [3]float32{bmax.X, bmax.Y, bmax.Z}
	for i := 0; i < 3; i++ {
		if d[i] == 0 {
			if o[i] < lo[i] || o[i] > hi[i] {
				return 0, false
			}
			continue
		}
		t0 := (lo[i] - o[i]) / d[i]
		t1 := (hi[i] - o[i]) / d[i]
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		tmin = math32.Max(tmin, t0)
		tmax = math32.Min(tmax, t1)
		if tmin > tmax {
			return 0, false
		}
	}
	return tmin, true
}

// RayTriangle intersects a ray with triangle abc (Möller–Trumbore,
// both-sided). Returns the distance along dir and whether it hits.
func RayTriangle(origin, dir, a, b, c Vec3) (float32, bool) {
	const eps = 1e-7
	e1 := b.Sub(a)
	e2 := c.Sub(a)
	p := dir.Cross(e2)
	det := e1.Dot(p)
	if det > -eps && det < eps {
		return 0, false // parallel
	}
	inv := 1 / det
	tv := origin.Sub(a)
	// Barycentric bounds with a hair of slack, so a ray that lands exactly on
	// an edge shared by two triangles (the diagonal of a quad) hits one of
	// them instead of slipping between both.
	const slack = 1e-5
	u := tv.Dot(p) * inv
	if u < -slack || u > 1+slack {
		return 0, false
	}
	q := tv.Cross(e1)
	v := dir.Dot(q) * inv
	if v < -slack || u+v > 1+slack {
		return 0, false
	}
	t := e2.Dot(q) * inv
	if t <= eps {
		return 0, false
	}
	return t, true
}

// RaySphere intersects a ray with a sphere; returns the nearest positive
// distance along dir (0 when the origin is inside).
func RaySphere(origin, dir, center Vec3, r float32) (float32, bool) {
	oc := origin.Sub(center)
	b := oc.Dot(dir)
	c := oc.Dot(oc) - r*r
	if c <= 0 {
		return 0, true // inside
	}
	if b > 0 {
		return 0, false // pointing away
	}
	disc := b*b - c
	if disc < 0 {
		return 0, false
	}
	t := -b - math32.Sqrt(disc)
	if t < 0 {
		t = 0
	}
	return t, true
}

// ClosestOnSegment returns the point on segment ab closest to p.
func ClosestOnSegment(p, a, b Vec3) Vec3 {
	ab := b.Sub(a)
	denom := ab.Dot(ab)
	if denom == 0 {
		return a
	}
	t := p.Sub(a).Dot(ab) / denom
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return a.Add(ab.Scale(t))
}

// ClosestOnTriangle returns the point on triangle abc closest to p
// (Ericson, Real-Time Collision Detection §5.1.5).
func ClosestOnTriangle(p, a, b, c Vec3) Vec3 {
	ab := b.Sub(a)
	ac := c.Sub(a)
	ap := p.Sub(a)
	d1 := ab.Dot(ap)
	d2 := ac.Dot(ap)
	if d1 <= 0 && d2 <= 0 {
		return a
	}
	bp := p.Sub(b)
	d3 := ab.Dot(bp)
	d4 := ac.Dot(bp)
	if d3 >= 0 && d4 <= d3 {
		return b
	}
	vc := d1*d4 - d3*d2
	if vc <= 0 && d1 >= 0 && d3 <= 0 {
		return a.Add(ab.Scale(d1 / (d1 - d3)))
	}
	cp := p.Sub(c)
	d5 := ab.Dot(cp)
	d6 := ac.Dot(cp)
	if d6 >= 0 && d5 <= d6 {
		return c
	}
	vb := d5*d2 - d1*d6
	if vb <= 0 && d2 >= 0 && d6 <= 0 {
		return a.Add(ac.Scale(d2 / (d2 - d6)))
	}
	va := d3*d6 - d5*d4
	if va <= 0 && (d4-d3) >= 0 && (d5-d6) >= 0 {
		return b.Add(c.Sub(b).Scale((d4 - d3) / ((d4 - d3) + (d5 - d6))))
	}
	denom := 1 / (va + vb + vc)
	return a.Add(ab.Scale(vb * denom)).Add(ac.Scale(vc * denom))
}
