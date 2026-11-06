package p3

// Narrowphase: contact manifolds between a dynamic body and anything
// solid. Specialized pairs (no GJK yet): sphere-sphere, sphere-box,
// sphere/box vs capsule and mesh via closest points, and box-box by SAT
// with reference-face clipping — the multi-point manifold that makes box
// stacks solid. Normals point from body A toward body B.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// contact is one manifold point.
type contact struct {
	point core.Vec3 // world contact position
	pen   float32   // penetration depth (>0 overlapping)

	// Solver state (prepared per step).
	rA, rB   core.Vec3 // point - center of mass
	nMass    float32   // effective mass along the normal
	tMass    [2]float32
	bias     float32 // position-correction velocity (pseudo pass)
	restBias float32 // restitution target velocity (real pass)
	jn       float32 // accumulated normal impulse
	jt       [2]float32
	jbn      float32 // accumulated pseudo (position) impulse
}

// manifold is a set of contacts sharing one normal between two bodies.
type manifold struct {
	a, b     *body
	key      uint64    // (a index << 32) | b index — warm-start cache key
	normal   core.Vec3 // from a to b
	friction float32
	rest     float32
	tangent  [2]core.Vec3
	pts      []contact
	ptsArr   [4]contact // backing storage (no allocs)
}

// wakeByOverlap wakes every sleeping dynamic body whose (skin-inflated)
// AABB touches a MOVING awake body. Quiet-but-awake bodies must not wake
// sleepers, or the last two members of a settling pile re-wake each other
// alternately and the pile never sleeps.
func (w *World) wakeByOverlap() {
	const skin = 0.01
	for changed := true; changed; {
		changed = false
		for i := range w.bodies {
			a := &w.bodies[i]
			if !a.live || a.dyn == nil || a.dyn.asleep {
				continue
			}
			if a.dyn.vel.Length() < sleepVel && a.dyn.angVel.Length() < sleepAngVel {
				continue
			}
			amin := a.bmin.Sub(core.V3(skin, skin, skin))
			amax := a.bmax.Add(core.V3(skin, skin, skin))
			w.walkBVH(func(bmin, bmax core.Vec3) bool {
				return bmin.X <= amax.X && bmax.X >= amin.X &&
					bmin.Y <= amax.Y && bmax.Y >= amin.Y &&
					bmin.Z <= amax.Z && bmax.Z >= amin.Z
			}, func(bi int32) {
				b := &w.bodies[bi]
				if b == a || !b.live || b.trigger || a.layer&b.layer == 0 {
					return
				}
				if b.dyn != nil && b.dyn.asleep {
					w.wake(b)
					changed = true
				}
			})
		}
	}
}

// collectManifolds walks broadphase pairs (dynamic vs solid) and generates
// contacts. Deterministic: outer loop in body order, inner candidates
// gathered in BVH order then filtered to index-greater dynamics or any
// static, deduped by ordered pair.
func (w *World) collectManifolds() []manifold {
	var out []manifold
	for i := range w.bodies {
		a := &w.bodies[i]
		if !a.live || a.dyn == nil {
			continue
		}
		// Inflate slightly so resting contacts persist.
		const skin = 0.01
		amin := a.bmin.Sub(core.V3(skin, skin, skin))
		amax := a.bmax.Add(core.V3(skin, skin, skin))
		w.walkBVH(func(bmin, bmax core.Vec3) bool {
			return bmin.X <= amax.X && bmax.X >= amin.X &&
				bmin.Y <= amax.Y && bmax.Y >= amin.Y &&
				bmin.Z <= amax.Z && bmax.Z >= amin.Z
		}, func(bi int32) {
			b := &w.bodies[bi]
			if b == a || !b.live || b.trigger || a.layer&b.layer == 0 {
				return
			}
			// Each dynamic pair once (lower index owns it); statics always
			// belong to the dynamic body's pass.
			if b.dyn != nil && int(bi) < i {
				return
			}
			if a.dyn.asleep && (b.dyn == nil || b.dyn.asleep) {
				return
			}
			m := manifold{a: a, b: b, key: uint64(i)<<32 | uint64(bi)}
			if collide(&m) {
				out = append(out, m)
				// Re-anchor pts into the stored copy's own array.
				mm := &out[len(out)-1]
				mm.pts = mm.ptsArr[:len(m.pts)]
			}
		})
	}
	return out
}

// collide fills m for the shape pair (a is always dynamic).
func collide(m *manifold) bool {
	a, b := m.a, m.b
	switch {
	case a.shape.Kind == KindSphere:
		return sphereVs(m, a, b, false)
	case b.shape.Kind == KindSphere && b.dyn != nil:
		// Swap so the sphere runs the specialized path.
		m.a, m.b = b, a
		ok := sphereVs(m, b, a, false)
		return ok
	case a.shape.Kind == KindBox && b.shape.Kind == KindBox:
		return boxBox(m)
	case a.shape.Kind == KindBox:
		return boxVsOther(m)
	}
	return false
}

func (m *manifold) add(p core.Vec3, pen float32) {
	if len(m.pts) < len(m.ptsArr) {
		m.ptsArr[len(m.pts)] = contact{point: p, pen: pen}
		m.pts = m.ptsArr[:len(m.pts)+1]
	}
}

// finish sets shared material/tangents once contacts exist.
func (m *manifold) finish() bool {
	if len(m.pts) == 0 {
		return false
	}
	fa, ra := DefaultMaterial.Friction, DefaultMaterial.Restitution
	if m.a.dyn != nil {
		fa, ra = m.a.dyn.friction, m.a.dyn.restitution
	}
	fb, rb := fa, float32(0)
	if m.b.dyn != nil {
		fb, rb = m.b.dyn.friction, m.b.dyn.restitution
	}
	m.friction = math32.Sqrt(fa * fb)
	m.rest = math32.Max(ra, rb)
	m.tangent[0], m.tangent[1] = tangentBasis(m.normal)
	return true
}

// tangentBasis builds two directions perpendicular to n.
func tangentBasis(n core.Vec3) (core.Vec3, core.Vec3) {
	var t core.Vec3
	if math32.Abs(n.X) < 0.7 {
		t = core.V3(1, 0, 0)
	} else {
		t = core.V3(0, 1, 0)
	}
	t1 := n.Cross(t).Normalize()
	return t1, n.Cross(t1)
}

// sphereVs handles a dynamic sphere against any solid: the closest surface
// point gives a single contact.
func sphereVs(m *manifold, a, b *body, _ bool) bool {
	c := a.pos
	q := b.toWorld(b.shape.closestLocal(b.toLocal(c)))
	d := q.Sub(c)
	l := d.Length()
	r := a.shape.R
	if l > r || l == 0 {
		// Center inside the other shape: push out along the least axis
		// (rare; treat as deep contact using the b-surface normal guess).
		if l != 0 {
			return false
		}
		m.normal = core.V3(0, 1, 0)
		m.add(c, r)
		return m.finish()
	}
	m.normal = d.Scale(1 / l) // a → b
	m.add(q, r-l)
	return m.finish()
}

// boxVsOther handles a dynamic box against capsule/mesh/static-sphere by
// corner-and-center closest-point sampling: up to 4 deepest corner
// contacts (enough to rest a crate on a floor mesh or against a pillar).
func boxVsOther(m *manifold) bool {
	a, b := m.a, m.b
	h := a.shape.Half
	var bestN core.Vec3
	found := false
	type cand struct {
		p   core.Vec3
		pen float32
		n   core.Vec3
	}
	var cands []cand
	for i := 0; i < 8; i++ {
		corner := core.V3(pick(i&1 == 0, -h.X, h.X), pick(i&2 == 0, -h.Y, h.Y), pick(i&4 == 0, -h.Z, h.Z))
		wp := a.toWorld(corner)
		q := b.toWorld(b.shape.closestLocal(b.toLocal(wp)))
		d := q.Sub(wp)
		l := d.Length()
		// The corner is "in contact" when the shape surface is behind it
		// relative to the box center (the closest point is on the far side).
		toC := wp.Sub(a.pos)
		if l == 0 || l > 0.05 {
			continue
		}
		n := d.Scale(1 / l)
		if n.Dot(toC) < 0 {
			continue // surface point is back toward the box: not penetrating
		}
		cands = append(cands, cand{p: q, pen: l, n: n})
		bestN = bestN.Add(n)
		found = true
	}
	if !found {
		// Deep/central contact: box center against the surface.
		c := a.pos
		q := b.toWorld(b.shape.closestLocal(b.toLocal(c)))
		d := q.Sub(c)
		l := d.Length()
		if l == 0 {
			return false
		}
		// Penetrating when the surface point is inside the box.
		lq := a.toLocal(q)
		if math32.Abs(lq.X) <= h.X && math32.Abs(lq.Y) <= h.Y && math32.Abs(lq.Z) <= h.Z {
			m.normal = d.Scale(1 / l)
			support := math32.Abs(m.normal.X)*h.X + math32.Abs(m.normal.Y)*h.Y + math32.Abs(m.normal.Z)*h.Z
			m.add(q, support-l)
			return m.finish()
		}
		return false
	}
	m.normal = bestN.Normalize()
	for _, c := range cands {
		m.add(c.p, c.pen)
	}
	return m.finish()
}

// ---- box-box SAT with face clipping ------------------------------------------

// boxAxes returns a box's world-space axis directions.
func boxAxes(b *body) [3]core.Vec3 {
	return [3]core.Vec3{
		b.rot.Rotate(core.V3(1, 0, 0)),
		b.rot.Rotate(core.V3(0, 1, 0)),
		b.rot.Rotate(core.V3(0, 0, 1)),
	}
}

func boxHalf(b *body) [3]float32 {
	return [3]float32{b.shape.Half.X, b.shape.Half.Y, b.shape.Half.Z}
}

// projectRadius is a box's extent along a direction.
func projectRadius(axes [3]core.Vec3, half [3]float32, dir core.Vec3) float32 {
	return half[0]*math32.Abs(axes[0].Dot(dir)) +
		half[1]*math32.Abs(axes[1].Dot(dir)) +
		half[2]*math32.Abs(axes[2].Dot(dir))
}

// boxBox generates a manifold by SAT: least-penetration axis among the 6
// face normals and 9 edge cross products; face axes clip the incident face
// for up to 4 points, edge axes yield the closest edge-edge point.
func boxBox(m *manifold) bool {
	a, b := m.a, m.b
	axA, axB := boxAxes(a), boxAxes(b)
	hA, hB := boxHalf(a), boxHalf(b)
	d := b.pos.Sub(a.pos)

	bestPen := float32(math32.MaxFloat32)  // raw depth of the chosen axis
	bestRank := float32(math32.MaxFloat32) // biased comparison value
	var bestAxis core.Vec3
	bestKind := -1 // 0..5 face, 6.. edge
	test := func(axis core.Vec3, kind int) bool {
		l := axis.Length()
		if l < 1e-6 {
			return true // degenerate cross: skip
		}
		axis = axis.Scale(1 / l)
		pen := projectRadius(axA, hA, axis) + projectRadius(axB, hB, axis) - math32.Abs(d.Dot(axis))
		if pen < 0 {
			return false // separating axis
		}
		// Slight preference for face axes (stability), like Box2D/Bullet.
		rank := pen
		if kind >= 6 {
			rank = pen * 1.05
		}
		if rank < bestRank {
			bestRank = rank
			bestPen = pen
			bestAxis = axis
			bestKind = kind
		}
		return true
	}
	for i := 0; i < 3; i++ {
		if !test(axA[i], i) || !test(axB[i], 3+i) {
			return false
		}
	}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if !test(axA[i].Cross(axB[j]), 6+i*3+j) {
				return false
			}
		}
	}

	// Point the axis from A to B.
	n := bestAxis
	if n.Dot(d) < 0 {
		n = n.Scale(-1)
	}
	m.normal = n

	if bestKind >= 6 {
		// Edge-edge: closest points between the two contributing edges.
		i, j := (bestKind-6)/3, (bestKind-6)%3
		pa, pb := closestEdgePoints(a, axA, hA, i, b, axB, hB, j, n)
		m.add(pa.Add(pb).Scale(0.5), bestPen)
		return m.finish()
	}

	// Face contact: reference face on the owner of the best axis.
	ref, inc := a, b
	refAxes, incAxes := axA, axB
	refHalf, incHalf := hA, hB
	refN := n
	if bestKind >= 3 {
		ref, inc = b, a
		refAxes, incAxes = axB, axA
		refHalf, incHalf = hB, hA
		refN = n.Scale(-1) // reference normal points away from ref
	}
	pts := clipFace(ref, refAxes, refHalf, refN, inc, incAxes, incHalf)
	faceCenter := ref.pos.Add(refN.Scale(projectRadius(refAxes, refHalf, refN)))
	// Keep points within a small skin of the face (not just penetrating) —
	// a resting box sits at pen≈0 and rejecting those collapses its 4-point
	// base to 1–2 points, which lets it rock forever.
	const clipSkin = 0.01
	for _, p := range pts {
		pen := refN.Dot(faceCenter.Sub(p))
		if pen > -clipSkin {
			m.add(p, math32.Max(pen, 0))
		}
	}
	if len(m.pts) == 0 {
		m.add(ref.pos.Add(refN.Scale(projectRadius(refAxes, refHalf, refN))), bestPen)
	}
	return m.finish()
}

// closestEdgePoints finds the closest points between the contributing
// edges of an edge-edge SAT axis.
func closestEdgePoints(a *body, axA [3]core.Vec3, hA [3]float32, i int,
	b *body, axB [3]core.Vec3, hB [3]float32, j int, n core.Vec3) (core.Vec3, core.Vec3) {
	// Support points on each box along ±n give the edge midpoints.
	suppA := a.pos
	for k := 0; k < 3; k++ {
		if k == i {
			continue
		}
		s := hA[k]
		if axA[k].Dot(n) < 0 {
			s = -s
		}
		suppA = suppA.Add(axA[k].Scale(s))
	}
	suppB := b.pos
	for k := 0; k < 3; k++ {
		if k == j {
			continue
		}
		s := hB[k]
		if axB[k].Dot(n) > 0 {
			s = -s
		}
		suppB = suppB.Add(axB[k].Scale(s))
	}
	a0 := suppA.Sub(axA[i].Scale(hA[i]))
	a1 := suppA.Add(axA[i].Scale(hA[i]))
	b0 := suppB.Sub(axB[j].Scale(hB[j]))
	b1 := suppB.Add(axB[j].Scale(hB[j]))
	pa, pb := closestSegmentSegment(a0, a1, b0, b1)
	return pa, pb
}

// closestSegmentSegment returns the closest points between two segments.
func closestSegmentSegment(p1, q1, p2, q2 core.Vec3) (core.Vec3, core.Vec3) {
	d1 := q1.Sub(p1)
	d2 := q2.Sub(p2)
	r := p1.Sub(p2)
	a := d1.Dot(d1)
	e := d2.Dot(d2)
	f := d2.Dot(r)
	c := d1.Dot(r)
	b := d1.Dot(d2)
	den := a*e - b*b
	var s float32
	if den > 1e-8 {
		s = clampf((b*f-c*e)/den, 0, 1)
	}
	t := (b*s + f) / e
	if t < 0 {
		t = 0
		s = clampf(-c/a, 0, 1)
	} else if t > 1 {
		t = 1
		s = clampf((b-c)/a, 0, 1)
	}
	return p1.Add(d1.Scale(s)), p2.Add(d2.Scale(t))
}

// clipFace clips the incident box face (most anti-parallel to refN)
// against the reference face's four side planes (Sutherland–Hodgman).
func clipFace(ref *body, refAxes [3]core.Vec3, refHalf [3]float32, refN core.Vec3,
	inc *body, incAxes [3]core.Vec3, incHalf [3]float32) []core.Vec3 {
	// Incident face: the inc axis most anti-parallel to refN.
	incIdx, incSign := 0, float32(1)
	best := float32(math32.MaxFloat32)
	for k := 0; k < 3; k++ {
		dot := incAxes[k].Dot(refN)
		if dot < best {
			best, incIdx, incSign = dot, k, 1
		}
		if -dot < best {
			best, incIdx, incSign = -dot, k, -1
		}
	}
	// Incident face quad.
	u, v := (incIdx+1)%3, (incIdx+2)%3
	fc := inc.pos.Add(incAxes[incIdx].Scale(incSign * incHalf[incIdx]))
	quad := make([]core.Vec3, 0, 8)
	for _, s := range [4][2]float32{{1, 1}, {1, -1}, {-1, -1}, {-1, 1}} {
		quad = append(quad, fc.Add(incAxes[u].Scale(s[0]*incHalf[u])).Add(incAxes[v].Scale(s[1]*incHalf[v])))
	}
	// Reference side planes: the two axes perpendicular to refN.
	for k := 0; k < 3; k++ {
		if math32.Abs(refAxes[k].Dot(refN)) > 0.99 {
			continue
		}
		for _, sgn := range [2]float32{1, -1} {
			pn := refAxes[k].Scale(sgn)
			pd := pn.Dot(ref.pos) + refHalf[k]
			quad = clipPlane(quad, pn, pd)
			if len(quad) == 0 {
				return nil
			}
		}
	}
	// At most 4 points (reduce by keeping extremes; simple truncation is
	// fine at this count).
	if len(quad) > 4 {
		quad = quad[:4]
	}
	return quad
}

// clipPlane keeps the polygon parts with pn·x <= pd.
func clipPlane(poly []core.Vec3, pn core.Vec3, pd float32) []core.Vec3 {
	out := make([]core.Vec3, 0, len(poly)+2)
	for i := 0; i < len(poly); i++ {
		cur := poly[i]
		nxt := poly[(i+1)%len(poly)]
		dc := pn.Dot(cur) - pd
		dn := pn.Dot(nxt) - pd
		if dc <= 0 {
			out = append(out, cur)
		}
		if (dc < 0 && dn > 0) || (dc > 0 && dn < 0) {
			t := dc / (dc - dn)
			out = append(out, cur.Add(nxt.Sub(cur).Scale(t)))
		}
	}
	return out
}
