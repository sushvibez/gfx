package p3

import (
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
)

const (
	charR  = 0.4
	charHH = 0.5 // capsule total height 1.8
	grav   = -12.0
)

// gauntlet builds the test level: a big floor at y=0, a wall at x=6, a 30°
// ramp along +Z at x=-6, a 0.3 step at z=6, a low ceiling pocket at z=-6,
// and a 60° steep face at x=12.
func gauntlet() *World {
	w := NewWorld()
	id := core.QuatIdentity()
	// Floor: top surface y=0.
	w.AddBody(Box(core.V3(40, 0.5, 40)), core.V3(0, -0.5, 0), id, 1)
	// Wall at x=6 (facing -X), spanning z ±4.
	w.AddBody(Box(core.V3(0.5, 3, 4)), core.V3(6.5, 3, 0), id, 1)
	// 30° ramp: rotated box, rising toward +Z around x=-6.
	ramp := core.QuatAxisAngle(core.V3(1, 0, 0), -30*math32.Pi/180)
	w.AddBody(Box(core.V3(3, 0.25, 6)), ramp.Rotate(core.V3(0, -0.25, 0)).Add(core.V3(-6, 0, 6)), ramp, 1)
	// Step: a 0.3-high platform starting at z=6 (top at y=0.3).
	w.AddBody(Box(core.V3(3, 0.15, 3)), core.V3(0, 0.15, 9), id, 1)
	// Low ceiling pocket at z=-6: gap from y=0 to y=2.0 (jump reaches it).
	w.AddBody(Box(core.V3(3, 0.5, 3)), core.V3(0, 2.5, -6), id, 1)
	// 60° steep face at x=12.
	steep := core.QuatAxisAngle(core.V3(0, 0, 1), 60*math32.Pi/180)
	w.AddBody(Box(core.V3(4, 0.25, 4)), core.V3(12, 1.5, 0), steep, 1)
	return w
}

// stand places a controller with its feet on y=ground at (x,z).
func stand(w *World, x, z float32) *Character {
	c := NewCharacter(w, core.V3(x, charHH+charR, z), charR, charHH)
	c.MoveAndSlide(core.Vec3{}, 0.016) // settle: snap establishes ground
	return c
}

func TestCharacterWalkAndGravity(t *testing.T) {
	w := gauntlet()
	c := stand(w, 0, 0)
	if !c.OnGround {
		t.Fatal("not grounded after settle")
	}
	// Walk +Z for a second of small steps; stays on the floor.
	vel := core.V3(2, 0, 2)
	for i := 0; i < 60; i++ {
		v := vel
		v.Y = grav * 0.016 * float32(i) // pretend accumulating gravity
		if c.OnGround {
			v.Y = grav * 0.016
		}
		c.MoveAndSlide(v, 0.016)
	}
	if !c.OnGround {
		t.Fatal("walk lost the ground")
	}
	near(t, c.FootY(), 0, 0.02, "foot height on floor")
	if c.Pos.X < 1.5 || c.Pos.Z < 1.5 {
		t.Fatalf("didn't move: %+v", c.Pos)
	}
}

func TestCharacterWallSlide(t *testing.T) {
	w := gauntlet()
	c := stand(w, 4.5, 0)
	// Run diagonally into the wall at x=6: X blocks at 6-0.5-r, Z keeps going.
	for i := 0; i < 60; i++ {
		c.MoveAndSlide(core.V3(4, grav*0.016, 2), 0.016)
	}
	near(t, c.Pos.X, 6-charR-0.002, 0.02, "wall clamp X")
	if c.Pos.Z < 1.2 {
		t.Fatalf("did not slide along the wall: z=%v", c.Pos.Z)
	}
	if !c.OnGround {
		t.Fatal("lost ground while sliding")
	}
}

func TestCharacterRampWalkableAndSteep(t *testing.T) {
	w := gauntlet()
	// 30° ramp: its surface breaks above the floor past z≈6 (it pivots at
	// (-6,0,6)); walk from z=5 long enough to climb well onto it.
	c := stand(w, -6, 5)
	startY := c.Pos.Y
	for i := 0; i < 200; i++ {
		c.MoveAndSlide(core.V3(0, grav*0.016, 2), 0.016)
	}
	if !c.OnGround {
		t.Fatal("lost ground on the 30° ramp")
	}
	if c.Pos.Y < startY+0.6 {
		t.Fatalf("did not climb the ramp: y=%v (start %v) z=%v", c.Pos.Y, startY, c.Pos.Z)
	}
	// 60° face: pushing into it must not gain meaningful height (not walkable).
	c2 := stand(w, 9.2, 0)
	startY = c2.Pos.Y
	for i := 0; i < 60; i++ {
		c2.MoveAndSlide(core.V3(2, grav*0.016, 0), 0.016)
	}
	if c2.Pos.Y > startY+c2.StepHeight+0.1 {
		t.Fatalf("climbed a 60° face: y=%v (start %v)", c2.Pos.Y, startY)
	}
}

func TestCharacterStepUp(t *testing.T) {
	w := gauntlet()
	c := stand(w, 0, 4)
	// Walk +Z into the 0.3-high step platform; StepHeight 0.35 clears it.
	for i := 0; i < 120; i++ {
		c.MoveAndSlide(core.V3(0, grav*0.016, 2), 0.016)
	}
	if !c.OnGround {
		t.Fatal("lost ground around the step")
	}
	if c.Pos.Z < 6.5 {
		t.Fatalf("blocked by a steppable ledge: z=%v", c.Pos.Z)
	}
	near(t, c.FootY(), 0.3, 0.03, "foot height on the step")

	// A step taller than StepHeight blocks.
	tall := NewWorld()
	tid := core.QuatIdentity()
	tall.AddBody(Box(core.V3(40, 0.5, 40)), core.V3(0, -0.5, 0), tid, 1)
	tall.AddBody(Box(core.V3(3, 0.3, 3)), core.V3(0, 0.3, 6), tid, 1) // 0.6 high
	c2 := stand(tall, 0, 3)
	for i := 0; i < 120; i++ {
		c2.MoveAndSlide(core.V3(0, grav*0.016, 2), 0.016)
	}
	if c2.FootY() > 0.1 {
		t.Fatalf("climbed a 0.6 step with StepHeight 0.35: foot=%v", c2.FootY())
	}
}

func TestCharacterCeiling(t *testing.T) {
	w := gauntlet()
	c := stand(w, 0, -6) // under the 1.2-high ceiling pocket
	// Jump: upward velocity must be killed by the ceiling (slide returns
	// vel with Y removed), and the capsule must not penetrate it.
	vel := core.V3(0, 8, 0)
	for i := 0; i < 30; i++ {
		vel = c.MoveAndSlide(vel, 0.016)
		vel.Y += grav * 0.016
	}
	top := c.Pos.Y + charHH + charR
	if top > 2.0+0.02 {
		t.Fatalf("penetrated the ceiling: top=%v", top)
	}
}

func TestCharacterGroundSnap(t *testing.T) {
	w := NewWorld()
	id := core.QuatIdentity()
	// Two slabs with a 0.15 drop between them.
	w.AddBody(Box(core.V3(3, 0.5, 3)), core.V3(0, -0.5, 0), id, 1)
	w.AddBody(Box(core.V3(3, 0.5, 3)), core.V3(0, -0.65, 6), id, 1)
	c := stand(w, 0, 2)
	grounded := 0
	for i := 0; i < 90; i++ {
		c.MoveAndSlide(core.V3(0, grav*0.016, 2.5), 0.016)
		if c.OnGround {
			grounded++
		}
	}
	// Snap keeps the controller grounded across the small drop nearly every
	// frame (allow a couple of transition frames).
	if grounded < 85 {
		t.Fatalf("snap failed: grounded %d/90 frames", grounded)
	}
	near(t, c.FootY(), -0.15, 0.03, "foot on the lower slab")
}

func TestTriggersEnterExit(t *testing.T) {
	w := gauntlet()
	zone := w.AddTrigger(Box(core.V3(1, 2, 1)), core.V3(0, 2, 3), core.QuatIdentity(), 1)
	c := stand(w, 0, 0)
	var events []string
	var tr TriggerEvents
	update := func() {
		tr.Update(c, LayerAll, func(id BodyID) {
			if id == zone {
				events = append(events, "enter")
			}
		}, func(id BodyID) {
			if id == zone {
				events = append(events, "exit")
			}
		})
	}
	for i := 0; i < 150; i++ {
		c.MoveAndSlide(core.V3(0, grav*0.016, 2), 0.016)
		update()
	}
	if len(events) != 2 || events[0] != "enter" || events[1] != "exit" {
		t.Fatalf("trigger events = %v, want [enter exit]", events)
	}
	// Triggers must not block movement.
	if c.Pos.Z < 4 {
		t.Fatalf("trigger blocked the walk: z=%v", c.Pos.Z)
	}
	// And must be invisible to solid queries.
	if _, ok := w.RayCast(core.V3(0, 2, 0), core.V3(0, 0, 1), 100, LayerAll); ok {
		t.Fatal("ray hit a trigger")
	}
}

func TestPlatformCarry(t *testing.T) {
	w := NewWorld()
	id := core.QuatIdentity()
	plat := w.AddBody(Box(core.V3(2, 0.25, 2)), core.V3(0, -0.25, 0), id, 1)
	c := stand(w, 0, 0)
	if !c.OnGround || c.Ground != plat {
		t.Fatalf("not standing on the platform: ground=%v", c.Ground)
	}
	// Move the platform +X over 60 frames; the idle character rides along.
	for i := 1; i <= 60; i++ {
		w.SetPose(plat, core.V3(float32(i)*0.05, -0.25, 0), id)
		c.MoveAndSlide(core.V3(0, grav*0.016, 0), 0.016)
	}
	near(t, c.Pos.X, 3, 0.15, "carried X")
	if !c.OnGround {
		t.Fatal("fell off a moving platform")
	}
}
