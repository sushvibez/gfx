package p3

// P2 — rigid-body dynamics (physics/PLAN.md): semi-implicit Euler
// integration, sequential-impulse contact solving (solver.go) over
// narrowphase manifolds (contact.go), Baumgarte position correction,
// island-free sleeping. Dynamic shapes v1: sphere and box; they collide
// with each other and with every static shape. Deterministic: bodies
// iterate in slice order, no map iteration anywhere in the step.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// dynBody is the motion state attached to a dynamic body.
type dynBody struct {
	vel     core.Vec3
	angVel  core.Vec3
	invMass float32
	invIL   core.Vec3 // inverse inertia, local diagonal

	// Split-impulse pseudo velocity: position correction accumulates here
	// and is consumed at integration, never entering real velocity (no
	// trampoline energy, warm starting stays clean). Cleared per step.
	pvel core.Vec3

	restitution float32
	friction    float32

	sleepTime float32
	asleep    bool
}

// Material tunes a dynamic body's contact response.
type Material struct {
	Restitution float32 // 0 = no bounce, 1 = perfect
	Friction    float32 // Coulomb coefficient
}

// DefaultMaterial is a dry, barely-bouncy solid.
var DefaultMaterial = Material{Restitution: 0.1, Friction: 0.6}

// AddDynamicBody inserts a simulated rigid body (shape must be Sphere or
// Box in v1). Mass in kg; the inertia tensor derives from the shape.
func (w *World) AddDynamicBody(shape Shape, pos core.Vec3, rot core.Quat, mass float32, mat Material, layer Layer) BodyID {
	id := w.add(shape, pos, rot, layer, false)
	b := w.get(id)
	d := &dynBody{
		invMass:     1 / mass,
		restitution: mat.Restitution,
		friction:    mat.Friction,
	}
	d.invIL = invInertia(shape, mass)
	b.dyn = d
	return id
}

// invInertia returns the inverse of the local diagonal inertia tensor.
func invInertia(s Shape, mass float32) core.Vec3 {
	switch s.Kind {
	case KindSphere:
		i := 0.4 * mass * s.R * s.R
		return core.V3(1/i, 1/i, 1/i)
	case KindBox:
		x, y, z := 2*s.Half.X, 2*s.Half.Y, 2*s.Half.Z
		c := mass / 12
		return core.V3(1/(c*(y*y+z*z)), 1/(c*(x*x+z*z)), 1/(c*(x*x+y*y)))
	case KindCapsule:
		// Approximate as the bounding cylinder.
		h := 2 * (s.HalfHeight + s.R)
		r := s.R
		iy := 0.5 * mass * r * r
		ix := mass * (3*r*r + h*h) / 12
		return core.V3(1/ix, 1/iy, 1/ix)
	case KindHull:
		// Approximate as the bounding box (dynamic hulls stay approximate
		// until GJK/EPA narrowphase lands — see PLAN P2 headroom).
		if s.Hull != nil {
			e := s.Hull.bmax.Sub(s.Hull.bmin)
			c := mass / 12
			return core.V3(1/(c*(e.Y*e.Y+e.Z*e.Z)), 1/(c*(e.X*e.X+e.Z*e.Z)), 1/(c*(e.X*e.X+e.Y*e.Y)))
		}
	}
	return core.V3(1, 1, 1)
}

// Velocity returns a dynamic body's linear velocity.
func (w *World) Velocity(id BodyID) (core.Vec3, bool) {
	if b := w.get(id); b != nil && b.dyn != nil {
		return b.dyn.vel, true
	}
	return core.Vec3{}, false
}

// SetVelocity sets a dynamic body's linear velocity (and wakes it).
func (w *World) SetVelocity(id BodyID, v core.Vec3) {
	if b := w.get(id); b != nil && b.dyn != nil {
		b.dyn.vel = v
		w.wake(b)
	}
}

// ApplyImpulse applies a world-space impulse at the body center (wakes it).
func (w *World) ApplyImpulse(id BodyID, imp core.Vec3) {
	if b := w.get(id); b != nil && b.dyn != nil {
		b.dyn.vel = b.dyn.vel.Add(imp.Scale(b.dyn.invMass))
		w.wake(b)
	}
}

// Asleep reports whether a dynamic body is sleeping.
func (w *World) Asleep(id BodyID) bool {
	b := w.get(id)
	return b != nil && b.dyn != nil && b.dyn.asleep
}

func (w *World) wake(b *body) {
	if b.dyn != nil {
		b.dyn.asleep = false
		b.dyn.sleepTime = 0
	}
}

// Step tuning.
const (
	velIterations = 16
	posIterations = 6
	sleepVel      = 0.10 // linear m/s below which a body drifts to sleep (Godot default)
	sleepAngVel   = 0.15
	sleepDelay    = 0.5  // seconds below threshold before sleeping
	maxAngVel     = 20   // clamp spin (explosion guard)
	linearDamp    = 0.05 // 1/s velocity decay (drag)
	angularDamp   = 0.8  // 1/s spin decay — bleeds solver noise from stacks
)

// Gravity is the world's constant acceleration (default Earth-ish down).
// Exposed as a field-like setter to keep World's zero value usable.
func (w *World) SetGravity(g core.Vec3) { w.gravity = g; w.hasGravity = true }

func (w *World) gravityOrDefault() core.Vec3 {
	if w.hasGravity {
		return w.gravity
	}
	return core.V3(0, -9.81, 0)
}

// Step advances the simulation by dt seconds (call at a fixed timestep).
func (w *World) Step(dt float32) {
	if dt <= 0 {
		return
	}
	g := w.gravityOrDefault()

	// Integrate velocities (awake dynamics only).
	for i := range w.bodies {
		b := &w.bodies[i]
		if !b.live || b.dyn == nil || b.dyn.asleep {
			continue
		}
		b.dyn.vel = b.dyn.vel.Add(g.Scale(dt))
		// Damping (implicit form keeps any dt stable).
		b.dyn.vel = b.dyn.vel.Scale(1 / (1 + linearDamp*dt))
		b.dyn.angVel = b.dyn.angVel.Scale(1 / (1 + angularDamp*dt))
	}

	// Wake sleepers BEFORE narrowphase: waking after collectManifolds
	// leaves the woken body without its own support contacts for a step
	// (they were skipped as asleep), so the stack above hammers it into
	// the ground and the eject cycle pumps energy into the pile.
	w.refresh()
	w.wakeByOverlap()
	manifolds := w.collectManifolds()

	// Sequential impulses (warm-started from the previous step), then the
	// split-impulse position pass on pseudo velocities.
	w.prepare(manifolds, dt)
	for it := 0; it < velIterations; it++ {
		for i := range manifolds {
			solveManifold(&manifolds[i])
		}
	}
	for it := 0; it < posIterations; it++ {
		for i := range manifolds {
			solvePosition(&manifolds[i])
		}
	}
	w.storeContacts(manifolds)

	// Integrate positions and update poses.
	for i := range w.bodies {
		b := &w.bodies[i]
		if !b.live || b.dyn == nil || b.dyn.asleep {
			continue
		}
		d := b.dyn
		if l := d.angVel.Length(); l > maxAngVel {
			d.angVel = d.angVel.Scale(maxAngVel / l)
		}
		pos := b.pos.Add(d.vel.Add(d.pvel).Scale(dt))
		rot := integrateRot(b.rot, d.angVel, dt)
		d.pvel = core.Vec3{}
		w.setPose(b, pos, rot)
		w.dirty = true

		// Sleep readiness: sustained near-rest velocity.
		if d.vel.Length() < sleepVel && d.angVel.Length() < sleepAngVel {
			d.sleepTime += dt
		} else {
			d.sleepTime = 0
		}
	}

	w.sleepIslands(manifolds)
}

// sleepIslands puts whole contact islands to sleep at once. A body must
// never sleep alone inside an active group: a lone sleeper is an
// infinite-mass anvil mid-pile and the solver pumps energy fighting it.
func (w *World) sleepIslands(manifolds []manifold) {
	n := len(w.bodies)
	if cap(w.islandParent) < n {
		w.islandParent = make([]int32, n)
	}
	parent := w.islandParent[:n]
	for i := range parent {
		parent[i] = int32(i)
	}
	var find func(i int32) int32
	find = func(i int32) int32 {
		for parent[i] != i {
			parent[i] = parent[parent[i]] // path halving
			i = parent[i]
		}
		return i
	}
	for i := range manifolds {
		m := &manifolds[i]
		if m.a.dyn == nil || m.b.dyn == nil {
			continue
		}
		ra, rb := find(int32(m.key>>32)), find(int32(m.key&0xffffffff))
		if ra != rb {
			parent[rb] = ra
		}
	}
	// An island sleeps only when every member has been quiet long enough.
	if cap(w.islandBusy) < n {
		w.islandBusy = make([]bool, n)
	}
	busy := w.islandBusy[:n]
	for i := range busy {
		busy[i] = false
	}
	for i := range w.bodies {
		b := &w.bodies[i]
		if !b.live || b.dyn == nil || b.dyn.asleep {
			continue
		}
		if b.dyn.sleepTime <= sleepDelay {
			busy[find(int32(i))] = true
		}
	}
	for i := range w.bodies {
		b := &w.bodies[i]
		if !b.live || b.dyn == nil || b.dyn.asleep {
			continue
		}
		if !busy[find(int32(i))] {
			b.dyn.asleep = true
			b.dyn.vel, b.dyn.angVel = core.Vec3{}, core.Vec3{}
		}
	}
}

// integrateRot advances a rotation by an angular velocity over dt.
func integrateRot(q core.Quat, w core.Vec3, dt float32) core.Quat {
	// dq/dt = 0.5 * (w quat) * q
	dq := core.Quat{X: w.X, Y: w.Y, Z: w.Z, W: 0}.Mul(q)
	return core.Quat{
		X: q.X + 0.5*dq.X*dt,
		Y: q.Y + 0.5*dq.Y*dt,
		Z: q.Z + 0.5*dq.Z*dt,
		W: q.W + 0.5*dq.W*dt,
	}.Normalize()
}

// invInertiaWorld returns the world-space inverse inertia applied to a
// vector: R * diag(invIL) * Rᵀ * v.
func (b *body) invInertiaWorld(v core.Vec3) core.Vec3 {
	if b.dyn == nil {
		return core.Vec3{}
	}
	l := b.inv.Rotate(v) // world → local
	l = core.V3(l.X*b.dyn.invIL.X, l.Y*b.dyn.invIL.Y, l.Z*b.dyn.invIL.Z)
	return b.rot.Rotate(l)
}

func clampf(v, lo, hi float32) float32 {
	return math32.Min(math32.Max(v, lo), hi)
}
