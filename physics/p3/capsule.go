package p3

// Capsule queries — the character controller's substrate. Distance from a
// capsule to a shape is the distance from its core segment minus the
// radius; the segment-to-shape closest pair comes from alternating
// projections (exact for convex shapes, a good local minimum on meshes,
// whose triangles are convex individually). CastCapsule then sweeps by
// conservative advancement exactly like CastSphere.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// segmentClosest returns the closest pair between world-space segment ab
// and a body's shape: the point on the shape (world) and on the segment.
func segmentClosest(b *body, a, bb core.Vec3) (onShape, onSeg core.Vec3) {
	// Work in the body's local space.
	la, lb := b.toLocal(a), b.toLocal(bb)
	p := la.Add(lb).Scale(0.5)
	var q core.Vec3
	for i := 0; i < 12; i++ {
		q = b.shape.closestLocal(p)
		np := core.ClosestOnSegment(q, la, lb)
		if np.Sub(p).Length() < 1e-5 {
			p = np
			break
		}
		p = np
	}
	q = b.shape.closestLocal(p)
	return b.toWorld(q), b.toWorld(p)
}

// OverlapCapsule calls visit for every solid body whose surface is within r
// of segment ab (a capsule of radius r). Trigger bodies are skipped; use
// OverlapTriggers for those.
func (w *World) OverlapCapsule(a, b core.Vec3, r float32, mask Layer, visit func(BodyID, core.Vec3)) {
	w.overlapCapsule(a, b, r, mask, false, visit)
}

// OverlapTriggers is OverlapCapsule over trigger bodies only.
func (w *World) OverlapTriggers(a, b core.Vec3, r float32, mask Layer, visit func(BodyID, core.Vec3)) {
	w.overlapCapsule(a, b, r, mask, true, visit)
}

func (w *World) overlapCapsule(a, b core.Vec3, r float32, mask Layer, triggers bool, visit func(BodyID, core.Vec3)) {
	w.refresh()
	// Capsule AABB for the broad phase.
	cmin := vmin(a, b).Sub(core.V3(r, r, r))
	cmax := vmax(a, b).Add(core.V3(r, r, r))
	w.walkBVH(func(bmin, bmax core.Vec3) bool {
		return bmin.X <= cmax.X && bmax.X >= cmin.X &&
			bmin.Y <= cmax.Y && bmax.Y >= cmin.Y &&
			bmin.Z <= cmax.Z && bmax.Z >= cmin.Z
	}, func(bi int32) {
		bd := &w.bodies[bi]
		if bd.trigger != triggers || bd.layer&mask == 0 {
			return
		}
		q, p := segmentClosest(bd, a, b)
		if q.Sub(p).Length() <= r {
			visit(BodyID{index: bi, gen: bd.gen}, q)
		}
	})
}

// CastCapsule sweeps a capsule (segment ab, radius r) along dir up to
// maxDist by conservative advancement over solid bodies. The hit normal
// points from the surface toward the capsule — the sliding plane.
func (w *World) CastCapsule(a, b core.Vec3, r float32, dir core.Vec3, maxDist float32, mask Layer) (Hit, bool) {
	w.refresh()
	const (
		iterations = 48
		skin       = 1e-3
	)
	t := float32(0)
	for i := 0; i < iterations && t <= maxDist; i++ {
		ca := a.Add(dir.Scale(t))
		cb := b.Add(dir.Scale(t))
		bestD := float32(math32.MaxFloat32)
		var bestQ, bestP core.Vec3
		var bestB BodyID
		reach := maxDist - t + r
		mid := ca.Add(cb).Scale(0.5)
		half := cb.Sub(ca).Length()/2 + r
		w.walkBVH(func(bmin, bmax core.Vec3) bool {
			return aabbDist(mid, bmin, bmax) <= math32.Min(bestD, reach)+half
		}, func(bi int32) {
			bd := &w.bodies[bi]
			if bd.trigger || bd.layer&mask == 0 {
				return
			}
			q, p := segmentClosest(bd, ca, cb)
			d := q.Sub(p).Length()
			// Resting contacts we are not moving into don't block (see
			// CastSphere) — without this, walking on a floor deadlocks.
			if d <= r+skin {
				n := p.Sub(q)
				if l := n.Length(); l > 1e-6 && n.Scale(1/l).Dot(dir) >= -1e-4 {
					return
				}
			}
			if d < bestD {
				bestD, bestQ, bestP = d, q, p
				bestB = BodyID{index: bi, gen: bd.gen}
			}
		})
		if bestD == math32.MaxFloat32 {
			return Hit{Body: NilBody}, false
		}
		if bestD <= r+skin {
			n := bestP.Sub(bestQ)
			if l := n.Length(); l > 1e-6 {
				n = n.Scale(1 / l)
			} else {
				n = dir.Scale(-1)
			}
			return Hit{Body: bestB, T: t, Point: bestQ, Normal: n}, true
		}
		t += bestD - r
	}
	return Hit{Body: NilBody}, false
}
