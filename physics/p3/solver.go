package p3

// Sequential-impulse contact solver (PGS): per contact point, a normal
// impulse (accumulated, clamped ≥ 0, with Baumgarte position bias and
// restitution) and two friction impulses clamped by the Coulomb cone.
// Static/kinematic bodies participate with infinite mass (zero inverse).

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

const (
	baumgarte = 0.2   // position-error feedback fraction per step
	slop      = 0.005 // penetration allowed before correction (m)
	restVel   = 0.5   // relative speed below which restitution is ignored
)

// velocityAt is a body's velocity at a world point (zero for statics).
func velocityAt(b *body, r core.Vec3) core.Vec3 {
	if b.dyn == nil {
		return core.Vec3{}
	}
	return b.dyn.vel.Add(b.dyn.angVel.Cross(r))
}

func invMass(b *body) float32 {
	if b.dyn == nil || b.dyn.asleep {
		return 0
	}
	return b.dyn.invMass
}

// effMass computes the effective mass along dir for a contact.
func effMass(a, b *body, rA, rB, dir core.Vec3) float32 {
	k := invMass(a) + invMass(b)
	if a.dyn != nil && !a.dyn.asleep {
		c := rA.Cross(dir)
		k += c.Dot(a.invInertiaWorld(c))
	}
	if b.dyn != nil && !b.dyn.asleep {
		c := rB.Cross(dir)
		k += c.Dot(b.invInertiaWorld(c))
	}
	if k <= 0 {
		return 0
	}
	return 1 / k
}

// applyImpulse applies ±imp at the contact to both bodies.
func applyPair(a, b *body, rA, rB, imp core.Vec3) {
	if a.dyn != nil && !a.dyn.asleep {
		a.dyn.vel = a.dyn.vel.Sub(imp.Scale(a.dyn.invMass))
		a.dyn.angVel = a.dyn.angVel.Sub(a.invInertiaWorld(rA.Cross(imp)))
	}
	if b.dyn != nil && !b.dyn.asleep {
		b.dyn.vel = b.dyn.vel.Add(imp.Scale(b.dyn.invMass))
		b.dyn.angVel = b.dyn.angVel.Add(b.invInertiaWorld(rB.Cross(imp)))
	}
}

// cachedContact persists a contact's accumulated impulses across steps
// (warm starting) — deep stacks never converge from zero in 10 iterations.
type cachedContact struct {
	pos          core.Vec3
	jn, jt0, jt1 float32
}

type cachedManifold struct {
	key uint64
	n   int32
	pts [4]cachedContact
}

// warmTol² — a cached point this close to a new one is the same contact.
const warmTol2 = 0.02 * 0.02

// prepare computes per-contact effective masses and bias terms from the
// pre-solve velocities, THEN warm starts every point in a second pass —
// restitution measured after another contact's warm impulse would see a
// fake approach velocity and inject energy every step.
func (w *World) prepare(ms []manifold, dt float32) {
	for i := range ms {
		m := &ms[i]
		for j := range m.pts {
			c := &m.pts[j]
			c.rA = c.point.Sub(m.a.pos)
			c.rB = c.point.Sub(m.b.pos)
			c.nMass = effMass(m.a, m.b, c.rA, c.rB, m.normal)
			c.tMass[0] = effMass(m.a, m.b, c.rA, c.rB, m.tangent[0])
			c.tMass[1] = effMass(m.a, m.b, c.rA, c.rB, m.tangent[1])

			// Position bias (solved on pseudo velocities) + restitution
			// (solved on real velocities, fast approaches only).
			c.bias = baumgarte / dt * math32.Max(c.pen-slop, 0)
			vn := velocityAt(m.b, c.rB).Sub(velocityAt(m.a, c.rA)).Dot(m.normal)
			c.restBias = 0
			if vn < -restVel {
				c.restBias = -m.rest * vn
			}
		}
	}
	for i := range ms {
		m := &ms[i]
		var prev *cachedManifold
		if pi, ok := w.contactIdx[m.key]; ok {
			prev = &w.contactCache[pi]
		}
		if prev == nil {
			continue
		}
		for j := range m.pts {
			c := &m.pts[j]
			for k := int32(0); k < prev.n; k++ {
				p := &prev.pts[k]
				d := p.pos.Sub(c.point)
				if d.Dot(d) > warmTol2 {
					continue
				}
				c.jn, c.jt[0], c.jt[1] = p.jn, p.jt0, p.jt1
				imp := m.normal.Scale(c.jn).
					Add(m.tangent[0].Scale(c.jt[0])).
					Add(m.tangent[1].Scale(c.jt[1]))
				applyPair(m.a, m.b, c.rA, c.rB, imp)
				break
			}
		}
	}
}

// storeContacts snapshots this step's impulses for the next warm start.
func (w *World) storeContacts(ms []manifold) {
	w.contactCache = w.contactCache[:0]
	if w.contactIdx == nil {
		w.contactIdx = make(map[uint64]int32)
	}
	for k := range w.contactIdx {
		delete(w.contactIdx, k)
	}
	for i := range ms {
		m := &ms[i]
		cm := cachedManifold{key: m.key, n: int32(len(m.pts))}
		for j, c := range m.pts {
			cm.pts[j] = cachedContact{pos: c.point, jn: c.jn, jt0: c.jt[0], jt1: c.jt[1]}
		}
		w.contactIdx[m.key] = int32(len(w.contactCache))
		w.contactCache = append(w.contactCache, cm)
	}
}

// solveManifold runs one impulse iteration over a manifold.
func solveManifold(m *manifold) {
	for j := range m.pts {
		c := &m.pts[j]

		// Normal impulse (accumulate, clamp ≥ 0).
		rv := velocityAt(m.b, c.rB).Sub(velocityAt(m.a, c.rA))
		vn := rv.Dot(m.normal)
		dJ := c.nMass * (c.restBias - vn)
		old := c.jn
		c.jn = math32.Max(old+dJ, 0)
		applyPair(m.a, m.b, c.rA, c.rB, m.normal.Scale(c.jn-old))

		// Friction along both tangents, clamped by the Coulomb cone.
		maxF := m.friction * c.jn
		for t := 0; t < 2; t++ {
			rv = velocityAt(m.b, c.rB).Sub(velocityAt(m.a, c.rA))
			vt := rv.Dot(m.tangent[t])
			dF := c.tMass[t] * -vt
			oldF := c.jt[t]
			c.jt[t] = clampf(oldF+dF, -maxF, maxF)
			applyPair(m.a, m.b, c.rA, c.rB, m.tangent[t].Scale(c.jt[t]-oldF))
		}
	}
}

// pseudoVelocityAt mirrors velocityAt for the position-correction pass.
func pseudoVelocityAt(b *body) core.Vec3 {
	if b.dyn == nil {
		return core.Vec3{}
	}
	return b.dyn.pvel
}

// applyPseudoPair is linear-only: angular position correction makes
// stacked boxes tilt a little every step and "walk" sideways — rolling
// that friction (a velocity constraint) can never resist.
func applyPseudoPair(a, b *body, imp core.Vec3) {
	if a.dyn != nil && !a.dyn.asleep {
		a.dyn.pvel = a.dyn.pvel.Sub(imp.Scale(a.dyn.invMass))
	}
	if b.dyn != nil && !b.dyn.asleep {
		b.dyn.pvel = b.dyn.pvel.Add(imp.Scale(b.dyn.invMass))
	}
}

// solvePosition runs one split-impulse iteration: penetration is resolved
// with pseudo velocities that move bodies at integration but carry no
// kinetic energy into the next step.
func solvePosition(m *manifold) {
	for j := range m.pts {
		c := &m.pts[j]
		if c.bias == 0 {
			continue
		}
		vn := pseudoVelocityAt(m.b).Sub(pseudoVelocityAt(m.a)).Dot(m.normal)
		k := invMass(m.a) + invMass(m.b)
		if k <= 0 {
			continue
		}
		dJ := (c.bias - vn) / k
		old := c.jbn
		c.jbn = math32.Max(old+dJ, 0)
		applyPseudoPair(m.a, m.b, m.normal.Scale(c.jbn-old))
	}
}
