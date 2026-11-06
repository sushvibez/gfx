package p3

// Character is the kinematic capsule controller (Godot's move_and_slide
// shape): sweep the capsule along the velocity, slide on contact planes,
// step up low obstacles, snap to the ground, and report floor state. It
// owns no dynamics — the game applies gravity and jumps to Velocity-in.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

type Character struct {
	World *World
	Pos   core.Vec3 // capsule CENTER
	R     float32   // capsule radius
	HH    float32   // capsule cylinder half-height
	Mask  Layer     // what the controller collides with

	MaxSlopeDeg float32 // steepest walkable slope (default 46°)
	StepHeight  float32 // max ledge auto-stepped (default 0.35)
	SnapLen     float32 // ground-snap probe distance (default 0.25)

	OnGround     bool
	GroundNormal core.Vec3
	Ground       BodyID // body stood on (platform carrying)

	groundPos core.Vec3 // ground body's pose when last stood on, for carry
}

// NewCharacter builds a controller with the standard tuning.
func NewCharacter(w *World, pos core.Vec3, r, hh float32) *Character {
	return &Character{
		World: w, Pos: pos, R: r, HH: hh, Mask: LayerAll,
		MaxSlopeDeg: 46, StepHeight: 0.35, SnapLen: 0.25,
		Ground: NilBody,
	}
}

// FootY returns the capsule's lowest point.
func (c *Character) FootY() float32 { return c.Pos.Y - c.HH - c.R }

func (c *Character) seg() (core.Vec3, core.Vec3) {
	return core.V3(c.Pos.X, c.Pos.Y-c.HH, c.Pos.Z), core.V3(c.Pos.X, c.Pos.Y+c.HH, c.Pos.Z)
}

func (c *Character) walkable(n core.Vec3) bool {
	return n.Y >= math32.Cos(c.MaxSlopeDeg*math32.Pi/180)
}

// MoveAndSlide advances the controller by vel*dt, sliding along contact
// planes (≤4 iterations), attempting step-up when a wall blocks grounded
// horizontal motion, then snapping to the ground. Returns the velocity
// with its blocked components removed (feed it back as next frame's
// velocity so gravity accumulates correctly against floors and ceilings).
func (c *Character) MoveAndSlide(vel core.Vec3, dt float32) core.Vec3 {
	const skin = 2e-3
	c.carryPlatform()

	disp := vel.Scale(dt)
	wasGrounded := c.OnGround
	c.OnGround = false
	c.Ground = NilBody

	for iter := 0; iter < 4; iter++ {
		dist := disp.Length()
		if dist < 1e-6 {
			break
		}
		dir := disp.Scale(1 / dist)
		a, b := c.seg()
		hit, ok := c.World.CastCapsule(a, b, c.R, dir, dist, c.Mask)
		if !ok {
			c.Pos = c.Pos.Add(disp)
			break
		}
		// Advance to contact (keeping a skin gap), then slide the remainder.
		adv := math32.Max(hit.T-skin, 0)
		c.Pos = c.Pos.Add(dir.Scale(adv))
		rest := disp.Sub(dir.Scale(adv))

		if c.walkable(hit.Normal) {
			c.setGround(hit)
		} else if wasGrounded && hit.Normal.Y < 0.5 && disp.Y <= 0 {
			// A wall while walking: try stepping up onto the obstacle.
			if c.stepUp(dir, rest.Length()) {
				disp = rest // keep going with what's left
				continue
			}
		}
		// Slide: remove the into-plane component from both the remaining
		// displacement and the returned velocity.
		disp = rest.Sub(hit.Normal.Scale(rest.Dot(hit.Normal)))
		vel = vel.Sub(hit.Normal.Scale(vel.Dot(hit.Normal)))
	}

	c.snapToGround(vel)
	return vel
}

// stepUp attempts the stair maneuver: lift by StepHeight, advance forward,
// and drop back down; succeeds if that lands on walkable ground higher
// than where we stood. Movement is applied on success.
func (c *Character) stepUp(dir core.Vec3, dist float32) bool {
	const skin = 2e-3
	up := core.V3(0, 1, 0)
	fwd := core.V3(dir.X, 0, dir.Z)
	if l := fwd.Length(); l < 1e-5 {
		return false
	} else {
		fwd = fwd.Scale(1 / l)
	}
	start := c.Pos

	// 1. Lift (may be limited by a ceiling).
	a, b := c.seg()
	lift := c.StepHeight
	if h, ok := c.World.CastCapsule(a, b, c.R, up, lift, c.Mask); ok {
		lift = math32.Max(h.T-skin, 0)
	}
	if lift < 1e-3 {
		return false
	}
	c.Pos = c.Pos.Add(up.Scale(lift))

	// 2. Advance forward at the lifted height.
	fdist := math32.Max(dist, c.R*0.5)
	a, b = c.seg()
	if h, ok := c.World.CastCapsule(a, b, c.R, fwd, fdist, c.Mask); ok {
		fdist = math32.Max(h.T-skin, 0)
	}
	if fdist < 1e-3 {
		c.Pos = start
		return false
	}
	c.Pos = c.Pos.Add(fwd.Scale(fdist))

	// 3. Drop back down; must land walkable and no lower than we started.
	a, b = c.seg()
	if h, ok := c.World.CastCapsule(a, b, c.R, up.Scale(-1), lift+skin, c.Mask); ok && c.walkable(h.Normal) {
		c.Pos = c.Pos.Add(up.Scale(-math32.Max(h.T-skin, 0)))
		c.setGround(h)
		return true
	}
	c.Pos = start
	return false
}

// snapToGround keeps the controller glued over small ledges/slope changes:
// when not moving upward, probe down SnapLen and stick to walkable ground.
func (c *Character) snapToGround(vel core.Vec3) {
	if vel.Y > 1e-3 { // ascending (jump): don't snap
		return
	}
	a, b := c.seg()
	if h, ok := c.World.CastCapsule(a, b, c.R, core.V3(0, -1, 0), c.SnapLen, c.Mask); ok && c.walkable(h.Normal) {
		const skin = 2e-3
		c.Pos = c.Pos.Add(core.V3(0, -math32.Max(h.T-skin, 0), 0))
		c.setGround(h)
	}
}

func (c *Character) setGround(h Hit) {
	c.OnGround = true
	c.GroundNormal = h.Normal
	c.Ground = h.Body
	if p, _, ok := c.World.Pose(h.Body); ok {
		c.groundPos = p
	}
}

// carryPlatform applies the ground body's translation since we last stood
// on it (kinematic moving platforms).
func (c *Character) carryPlatform() {
	if c.Ground == NilBody {
		return
	}
	if p, _, ok := c.World.Pose(c.Ground); ok {
		delta := p.Sub(c.groundPos)
		if delta.Length() > 0 {
			c.Pos = c.Pos.Add(delta)
			c.groundPos = p
		}
	}
}

// TriggerEvents tracks which trigger volumes the controller's capsule is
// inside and fires enter/exit callbacks on change.
type TriggerEvents struct {
	inside map[BodyID]bool
	seen   map[BodyID]bool
}

// Update overlap-tests the capsule against trigger volumes and invokes
// onEnter/onExit (either may be nil) for changes since the last call.
func (t *TriggerEvents) Update(c *Character, mask Layer, onEnter, onExit func(BodyID)) {
	if t.inside == nil {
		t.inside = map[BodyID]bool{}
		t.seen = map[BodyID]bool{}
	}
	clear(t.seen)
	a, b := c.seg()
	c.World.OverlapTriggers(a, b, c.R, mask, func(id BodyID, _ core.Vec3) {
		t.seen[id] = true
		if !t.inside[id] {
			t.inside[id] = true
			if onEnter != nil {
				onEnter(id)
			}
		}
	})
	for id := range t.inside {
		if !t.seen[id] {
			delete(t.inside, id)
			if onExit != nil {
				onExit(id)
			}
		}
	}
}
