package p3

// The P0 query set: RayCast (nearest hit with normal), ClosestPoint
// (nearest surface point), OverlapSphere (bodies within a radius), and
// CastSphere (swept sphere by conservative advancement — the primitive a
// capsule character controller builds on). All queries filter by layer
// mask and go through the world BVH.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// Hit describes the nearest intersection of a query.
type Hit struct {
	Body   BodyID
	T      float32   // distance along the ray / cast direction
	Point  core.Vec3 // world-space contact point
	Normal core.Vec3 // world-space surface normal at the contact
}

// RayCast finds the nearest body surface along origin+t*dir, t ≤ maxDist.
// dir must be normalized.
func (w *World) RayCast(origin, dir core.Vec3, maxDist float32, mask Layer) (Hit, bool) {
	w.refresh()
	best := Hit{T: maxDist, Body: NilBody}
	found := false
	w.walkBVH(func(bmin, bmax core.Vec3) bool {
		t, hit := core.RayAABB(origin, dir, bmin, bmax)
		return hit && t <= best.T
	}, func(bi int32) {
		b := &w.bodies[bi]
		if b.trigger || b.layer&mask == 0 {
			return
		}
		if t, hit := core.RayAABB(origin, dir, b.bmin, b.bmax); !hit || t > best.T {
			return
		}
		lo := b.toLocal(origin)
		ld := b.inv.Rotate(dir)
		t, n, hit := rayShape(&b.shape, lo, ld, best.T)
		if hit && t < best.T {
			best.T = t
			best.Body = BodyID{index: bi, gen: b.gen}
			best.Point = origin.Add(dir.Scale(t))
			best.Normal = b.rot.Rotate(n)
			found = true
		}
	})
	return best, found
}

// rayShape intersects a model-space ray with one shape, returning distance
// and model-space normal.
func rayShape(s *Shape, o, d core.Vec3, maxT float32) (float32, core.Vec3, bool) {
	switch s.Kind {
	case KindSphere:
		t, hit := core.RaySphere(o, d, core.Vec3{}, s.R)
		if !hit || t > maxT {
			return 0, core.Vec3{}, false
		}
		return t, o.Add(d.Scale(t)).Normalize(), true
	case KindBox:
		t, hit := core.RayAABB(o, d, s.Half.Scale(-1), s.Half)
		if !hit || t > maxT {
			return 0, core.Vec3{}, false
		}
		p := o.Add(d.Scale(t))
		return t, boxNormal(p, s.Half), true
	case KindCapsule:
		return rayCapsule(o, d, s.R, s.HalfHeight, maxT)
	case KindMesh:
		if s.Mesh == nil {
			return 0, core.Vec3{}, false
		}
		t, n, hit := s.Mesh.raycast(o, d, maxT)
		if hit && n.Dot(d) > 0 {
			n = n.Scale(-1) // face the ray (triangles are two-sided)
		}
		return t, n, hit
	case KindHull:
		if s.Hull == nil {
			return 0, core.Vec3{}, false
		}
		return s.Hull.raycast(o, d, maxT)
	}
	return 0, core.Vec3{}, false
}

// boxNormal returns the axis face a surface point lies on.
func boxNormal(p, half core.Vec3) core.Vec3 {
	// Distance to each face; the closest face's axis wins.
	dx := half.X - math32.Abs(p.X)
	dy := half.Y - math32.Abs(p.Y)
	dz := half.Z - math32.Abs(p.Z)
	switch {
	case dx <= dy && dx <= dz:
		return core.V3(sign(p.X), 0, 0)
	case dy <= dz:
		return core.V3(0, sign(p.Y), 0)
	default:
		return core.V3(0, 0, sign(p.Z))
	}
}

func sign(v float32) float32 {
	if v < 0 {
		return -1
	}
	return 1
}

// rayCapsule intersects a ray with a Y-axis capsule (cylinder + cap spheres).
func rayCapsule(o, d core.Vec3, r, hh, maxT float32) (float32, core.Vec3, bool) {
	// Origin inside: solids report t=0 (matching sphere/box/hull), normal
	// radially outward from the core segment.
	seg := core.ClosestOnSegment(o, core.V3(0, -hh, 0), core.V3(0, hh, 0))
	if off := o.Sub(seg); off.Length() <= r {
		if l := off.Length(); l > 1e-6 {
			return 0, off.Scale(1 / l), true
		}
		return 0, d.Scale(-1), true
	}
	bestT := maxT
	var bestN core.Vec3
	found := false
	// Infinite cylinder x²+z²=r², clipped to |y| ≤ hh.
	a := d.X*d.X + d.Z*d.Z
	if a > 1e-12 {
		b := o.X*d.X + o.Z*d.Z
		c := o.X*o.X + o.Z*o.Z - r*r
		disc := b*b - a*c
		if disc >= 0 {
			sq := math32.Sqrt(disc)
			for _, t := range [2]float32{(-b - sq) / a, (-b + sq) / a} {
				if t < 0 || t >= bestT {
					continue
				}
				p := o.Add(d.Scale(t))
				if p.Y >= -hh && p.Y <= hh {
					bestT = t
					bestN = core.V3(p.X, 0, p.Z).Normalize()
					found = true
					break // nearer root first
				}
			}
		}
	}
	// Cap spheres.
	for _, cy := range [2]float32{hh, -hh} {
		center := core.V3(0, cy, 0)
		if t, hit := core.RaySphere(o, d, center, r); hit && t < bestT {
			p := o.Add(d.Scale(t))
			// Only the hemisphere beyond the cylinder counts.
			if (cy > 0 && p.Y >= hh) || (cy < 0 && p.Y <= -hh) {
				bestT = t
				bestN = p.Sub(center).Normalize()
				found = true
			}
		}
	}
	if !found {
		return 0, core.Vec3{}, false
	}
	return bestT, bestN, true
}

// ClosestPoint finds the nearest solid-body surface point strictly within
// maxDist of p. Hit.T is the distance, Hit.Normal points from the surface
// toward p (zero when p is inside the body, where T is 0 and Point is p).
func (w *World) ClosestPoint(p core.Vec3, maxDist float32, mask Layer) (Hit, bool) {
	w.refresh()
	best := Hit{T: maxDist, Body: NilBody}
	found := false
	w.walkBVH(func(bmin, bmax core.Vec3) bool {
		return aabbDist(p, bmin, bmax) <= best.T
	}, func(bi int32) {
		b := &w.bodies[bi]
		if b.trigger || b.layer&mask == 0 {
			return
		}
		q := b.toWorld(b.shape.closestLocal(b.toLocal(p)))
		d := q.Sub(p).Length()
		if d < best.T {
			best.T = d
			best.Body = BodyID{index: bi, gen: b.gen}
			best.Point = q
			if d > 1e-6 {
				best.Normal = p.Sub(q).Scale(1 / d)
			} else {
				best.Normal = core.Vec3{}
			}
			found = true
		}
	})
	return best, found
}

// OverlapSphere calls visit for every body whose surface is within r of
// center (traversal order is unspecified).
func (w *World) OverlapSphere(center core.Vec3, r float32, mask Layer, visit func(BodyID, core.Vec3)) {
	w.refresh()
	w.walkBVH(func(bmin, bmax core.Vec3) bool {
		return aabbDist(center, bmin, bmax) <= r
	}, func(bi int32) {
		b := &w.bodies[bi]
		if b.trigger || b.layer&mask == 0 {
			return
		}
		lp := b.toLocal(center)
		q := b.shape.closestLocal(lp)
		if q.Sub(lp).Length() <= r {
			visit(BodyID{index: bi, gen: b.gen}, b.toWorld(q))
		}
	})
}

// CastSphere sweeps a sphere of radius r from origin along dir (normalized)
// up to maxDist, by conservative advancement: the sphere advances by its
// clearance until contact (or free passage). The hit normal points from the
// surface toward the sphere center — the sliding plane a character
// controller needs.
func (w *World) CastSphere(origin core.Vec3, r float32, dir core.Vec3, maxDist float32, mask Layer) (Hit, bool) {
	w.refresh()
	const (
		iterations = 48
		skin       = 1e-3
	)
	t := float32(0)
	for i := 0; i < iterations && t <= maxDist; i++ {
		center := origin.Add(dir.Scale(t))
		// Clearance: nearest surface point across candidate bodies.
		bestD := float32(math32.MaxFloat32)
		var bestP core.Vec3
		var bestB BodyID
		reach := maxDist - t + r
		w.walkBVH(func(bmin, bmax core.Vec3) bool {
			return aabbDist(center, bmin, bmax) <= math32.Min(bestD, reach)
		}, func(bi int32) {
			b := &w.bodies[bi]
			if b.trigger || b.layer&mask == 0 {
				return
			}
			q := b.toWorld(b.shape.closestLocal(b.toLocal(center)))
			d := q.Sub(center).Length()
			// A resting contact we are NOT moving into (normal not opposing
			// dir) must not block or clamp the advance — otherwise sliding
			// along a floor/wall deadlocks at t=0.
			if d <= r+skin {
				n := center.Sub(q)
				if l := n.Length(); l > 1e-6 && n.Scale(1/l).Dot(dir) >= -1e-4 {
					return
				}
			}
			if d < bestD {
				bestD = d
				bestP = q
				bestB = BodyID{index: bi, gen: b.gen}
			}
		})
		if bestD == math32.MaxFloat32 {
			return Hit{Body: NilBody}, false // nothing anywhere near
		}
		if bestD <= r+skin {
			n := center.Sub(bestP)
			if l := n.Length(); l > 1e-6 {
				n = n.Scale(1 / l)
			} else {
				n = dir.Scale(-1) // center on the surface: push back
			}
			return Hit{Body: bestB, T: t, Point: bestP, Normal: n}, true
		}
		t += bestD - r // safe advance
	}
	return Hit{Body: NilBody}, false
}
