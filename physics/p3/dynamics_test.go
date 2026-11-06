package p3

import (
	"testing"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

const dt = float32(1.0 / 60)

func stepN(w *World, n int) {
	for i := 0; i < n; i++ {
		w.Step(dt)
	}
}

// Free fall with no contacts matches the analytic semi-implicit solution.
func TestDynamicsFreeFall(t *testing.T) {
	w := NewWorld()
	id := w.AddDynamicBody(Sphere(0.5), core.V3(0, 100, 0), core.QuatIdentity(), 1, DefaultMaterial, LayerAll)
	const steps = 60
	stepN(w, steps)

	// Semi-implicit Euler with the engine's implicit linear damping:
	// v' = (v + g·dt)/(1 + damp·dt), y' = y + v'·dt.
	wantY, wantV := float32(100), float32(0)
	for i := 0; i < steps; i++ {
		wantV = (wantV - 9.81*dt) / (1 + linearDamp*dt)
		wantY += wantV * dt
	}
	pos, _, _ := w.Pose(id)
	if math32.Abs(pos.Y-wantY) > 1e-3 {
		t.Fatalf("free fall y = %v, want %v", pos.Y, wantY)
	}
	v, _ := w.Velocity(id)
	if math32.Abs(v.Y-wantV) > 1e-3 {
		t.Fatalf("free fall vy = %v, want %v", v.Y, wantV)
	}
}

// A bouncy sphere dropped on the floor rebounds to ~e² of its drop height.
func TestDynamicsRestitution(t *testing.T) {
	w := NewWorld()
	w.AddBody(Box(core.V3(20, 0.5, 20)), core.V3(0, -0.5, 0), core.QuatIdentity(), LayerAll) // floor, top at y=0
	id := w.AddDynamicBody(Sphere(0.5), core.V3(0, 3, 0), core.QuatIdentity(),
		1, Material{Restitution: 0.5, Friction: 0.3}, LayerAll)

	// Drop, then track the apex after the first bounce.
	peak := float32(0)
	bounced := false
	for i := 0; i < 600; i++ {
		w.Step(dt)
		pos, _, _ := w.Pose(id)
		v, _ := w.Velocity(id)
		if v.Y > 0.1 {
			bounced = true
		}
		if bounced {
			if pos.Y > peak {
				peak = pos.Y
			}
			if v.Y < -0.1 && peak > 0 {
				break // falling again: apex recorded
			}
		}
	}
	if !bounced {
		t.Fatal("sphere never bounced")
	}
	// Drop height 2.5 (center 3 → resting 0.5), e=0.5 → rebound ≈ 0.625 above
	// rest, apex ≈ 1.125. Solver bias and discrete impact time smear this;
	// accept a generous band that still rules out e≈0 and e≈1.
	if peak < 0.7 || peak > 1.6 {
		t.Fatalf("bounce apex %v, want ≈1.1 (e=0.5)", peak)
	}
}

// A box resting on the floor under gravity stays put and falls asleep.
func TestDynamicsRestingBoxSleeps(t *testing.T) {
	w := NewWorld()
	w.AddBody(Box(core.V3(20, 0.5, 20)), core.V3(0, -0.5, 0), core.QuatIdentity(), LayerAll)
	id := w.AddDynamicBody(Box(core.V3(0.5, 0.5, 0.5)), core.V3(0, 0.51, 0), core.QuatIdentity(),
		1, DefaultMaterial, LayerAll)
	stepN(w, 300) // 5 seconds
	pos, _, _ := w.Pose(id)
	if math32.Abs(pos.X) > 0.01 || math32.Abs(pos.Z) > 0.01 || math32.Abs(pos.Y-0.5) > 0.05 {
		t.Fatalf("resting box drifted to %v", pos)
	}
	if !w.Asleep(id) {
		t.Fatal("resting box never fell asleep")
	}

	// An impulse wakes it and it moves again.
	w.ApplyImpulse(id, core.V3(4, 0, 0))
	if w.Asleep(id) {
		t.Fatal("impulse did not wake the box")
	}
	stepN(w, 30)
	pos2, _, _ := w.Pose(id)
	if pos2.X <= pos.X+0.05 {
		t.Fatalf("woken box did not slide: x %v → %v", pos.X, pos2.X)
	}
}

// High friction holds a box on a slope; zero friction lets it slide.
func TestDynamicsFrictionSlope(t *testing.T) {
	// ~15° slope around Z: tan(15°)≈0.27, so μ=0.8 sticks and μ=0 slides.
	rot := core.QuatAxisAngle(core.V3(0, 0, 1), 15*math32.Pi/180)
	downSlope := rot.Rotate(core.V3(-1, 0, 0)) // -X tilts downhill for a +Z rotation
	run := func(friction float32) float32 {
		w := NewWorld()
		floorPos := core.V3(0, -0.5, 0)
		w.AddBody(Box(core.V3(20, 0.5, 20)), floorPos, rot, LayerAll)
		start := floorPos.Add(rot.Rotate(core.V3(0, 1.005, 0))) // resting on the tilted top face
		id := w.AddDynamicBody(Box(core.V3(0.5, 0.5, 0.5)), start, rot,
			1, Material{Restitution: 0, Friction: friction}, LayerAll)
		stepN(w, 240)
		pos, _, _ := w.Pose(id)
		return pos.Sub(start).Dot(downSlope) // tangential travel only
	}
	if moved := run(0.8); moved > 0.1 {
		t.Fatalf("high-friction box slid %v down a 15° slope", moved)
	}
	if moved := run(0); moved < 0.5 {
		t.Fatalf("frictionless box only slid %v on a 15° slope", moved)
	}
}

// A 10-box tower stays standing for 1000 steps (physics/PLAN.md bar).
func TestDynamicsBoxStack(t *testing.T) {
	w := NewWorld()
	w.AddBody(Box(core.V3(20, 0.5, 20)), core.V3(0, -0.5, 0), core.QuatIdentity(), LayerAll)
	var ids []BodyID
	for i := 0; i < 10; i++ {
		ids = append(ids, w.AddDynamicBody(Box(core.V3(0.5, 0.5, 0.5)),
			core.V3(0, 0.5+float32(i)*1.001, 0), core.QuatIdentity(), 1, DefaultMaterial, LayerAll))
	}
	stepN(w, 1000)
	for i, id := range ids {
		pos, _, _ := w.Pose(id)
		wantY := 0.5 + float32(i)
		// Height must hold exactly per level; the top boxes are allowed a
		// bounded lateral wander (residual solver jiggle) but must stay
		// stacked, not toppled.
		if math32.Abs(pos.X) > 0.6 || math32.Abs(pos.Z) > 0.6 || math32.Abs(pos.Y-wantY) > 0.2 {
			t.Fatalf("stack box %d ended at %v, want ≈(0,%v,0)", i, pos, wantY)
		}
	}
}

// Two identical worlds stepped identically end in identical states.
func TestDynamicsDeterminism(t *testing.T) {
	build := func() *World {
		w := NewWorld()
		w.AddBody(Box(core.V3(20, 0.5, 20)), core.V3(0, -0.5, 0), core.QuatIdentity(), LayerAll)
		for i := 0; i < 8; i++ {
			f := float32(i)
			w.AddDynamicBody(Box(core.V3(0.4, 0.4, 0.4)), core.V3(f*0.3-1, 2+f*0.9, f*0.2-0.7),
				core.QuatAxisAngle(core.V3(1, 1, 0).Normalize(), f*0.4), 1, DefaultMaterial, LayerAll)
			w.AddDynamicBody(Sphere(0.3), core.V3(f*0.25-1, 4+f*0.5, 0.5-f*0.15),
				core.QuatIdentity(), 0.5, Material{Restitution: 0.4, Friction: 0.4}, LayerAll)
		}
		return w
	}
	w1, w2 := build(), build()
	stepN(w1, 300)
	stepN(w2, 300)
	for i := range w1.bodies {
		a, b := &w1.bodies[i], &w2.bodies[i]
		if a.pos != b.pos || a.rot != b.rot {
			t.Fatalf("body %d diverged: %v/%v vs %v/%v", i, a.pos, a.rot, b.pos, b.rot)
		}
		if a.dyn != nil && (a.dyn.vel != b.dyn.vel || a.dyn.angVel != b.dyn.angVel) {
			t.Fatalf("body %d velocity diverged", i)
		}
	}
}

// Dynamic sphere-sphere collision separates the pair symmetrically.
func TestDynamicsSphereSphere(t *testing.T) {
	w := NewWorld()
	w.SetGravity(core.Vec3{}) // isolate the collision
	a := w.AddDynamicBody(Sphere(0.5), core.V3(-2, 0, 0), core.QuatIdentity(), 1,
		Material{Restitution: 1, Friction: 0}, LayerAll)
	b := w.AddDynamicBody(Sphere(0.5), core.V3(2, 0, 0), core.QuatIdentity(), 1,
		Material{Restitution: 1, Friction: 0}, LayerAll)
	w.SetVelocity(a, core.V3(4, 0, 0))
	w.SetVelocity(b, core.V3(-4, 0, 0))
	stepN(w, 120)
	va, _ := w.Velocity(a)
	vb, _ := w.Velocity(b)
	if va.X > -1 || vb.X < 1 {
		t.Fatalf("equal-mass elastic collision should reverse: va=%v vb=%v", va, vb)
	}
	if math32.Abs(va.X+vb.X) > 0.2 {
		t.Fatalf("momentum not conserved: va=%v vb=%v", va, vb)
	}
}
