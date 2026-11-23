// Package orbit is the orbit camera of the 3D views — an editor's preview,
// a model viewer, a CAD part: it circles a target, pans it across the
// screen, zooms toward it, and eases to a view (a ui/navcube snap) over a
// fixed time. It shares navcube's convention: the eye sits at
// Target + Distance·(cos p·sin y, sin p, cos p·cos y) looking at the
// target with +Y up, so yaw 0, pitch 0 looks along −Z.
//
//	cam := orbit.New(target, 20)
//	cam.Handle(mouse)                     // or Orbit / Pan / Zoom from your own routing
//	cam.Step(dt)                          // advances an AnimateTo
//	proj, view := cam.Matrices(aspect)    // render.Post wants them apart
package orbit

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// Camera orbits Target. The zero value is usable after New's defaults are
// set; New fills them.
type Camera struct {
	Target   core.Vec3
	Distance float32
	Yaw      float32 // radians about +Y
	Pitch    float32 // radians above the horizon

	// FovY (radians), Near and Far shape Matrices.
	FovY, Near, Far float32
	// MinDistance and MaxDistance bound Zoom; PitchLimit bounds Pitch
	// (radians, under π/2 so the up vector never flips).
	MinDistance, MaxDistance, PitchLimit float32
	// OrbitSpeed is radians per pixel dragged, PanSpeed the target's
	// travel per pixel as a fraction of Distance, ZoomStep the factor one
	// wheel notch zooms by.
	OrbitSpeed, PanSpeed, ZoomStep float32
	// SnapSeconds is how long AnimateTo takes (sine in-out).
	SnapSeconds float32

	home struct {
		target               core.Vec3
		distance, yaw, pitch float32
	}
	anim               bool
	t                  float32
	fromYaw, fromPitch float32
	toYaw, toPitch     float32
	dragging, panning  bool
	lastX, lastY       float32
}

// New is a camera at distance from target, 45° round and 35° up, with the
// usual editor feel; that pose is its home (Reset).
func New(target core.Vec3, distance float32) *Camera {
	c := &Camera{Target: target, Distance: distance, Yaw: math32.Pi / 4, Pitch: 35 * math32.Pi / 180,
		FovY: 50 * math32.Pi / 180, Near: 0.05, Far: 1000,
		MinDistance: 0.5, MaxDistance: 500, PitchLimit: 89 * math32.Pi / 180,
		OrbitSpeed: 0.005, PanSpeed: 0.0015, ZoomStep: 1.1, SnapSeconds: 0.35}
	c.SetHome()
	return c
}

// SetHome makes the current pose the one Reset returns to.
func (c *Camera) SetHome() {
	c.home.target, c.home.distance, c.home.yaw, c.home.pitch = c.Target, c.Distance, c.Yaw, c.Pitch
}

// Reset returns to the home pose.
func (c *Camera) Reset() {
	c.anim = false
	c.Target, c.Distance, c.Yaw, c.Pitch = c.home.target, c.home.distance, c.home.yaw, c.home.pitch
}

// Orbit turns the camera by a drag of (dx, dy) pixels: dragging right
// swings the eye left round the target (the scene turns with the hand),
// dragging down raises it.
func (c *Camera) Orbit(dx, dy float32) {
	c.anim = false
	c.Yaw -= dx * c.OrbitSpeed
	c.Pitch = clamp(c.Pitch+dy*c.OrbitSpeed, -c.PitchLimit, c.PitchLimit)
}

// Pan slides the target with a drag of (dx, dy) pixels across the screen:
// sideways along the view's right, up and down along world +Y.
func (c *Camera) Pan(dx, dy float32) {
	c.anim = false
	s := c.Distance * c.PanSpeed
	right := core.V3(math32.Cos(c.Yaw), 0, -math32.Sin(c.Yaw))
	c.Target = c.Target.Add(right.Scale(-dx * s)).Add(core.V3(0, dy*s, 0))
}

// Zoom multiplies the distance by f within MinDistance…MaxDistance.
func (c *Camera) Zoom(f float32) {
	c.Distance = clamp(c.Distance*f, c.MinDistance, c.MaxDistance)
}

// Handle is a default mouse binding: primary drag orbits, Shift+primary or
// secondary drag pans, the wheel zooms. Apps with their own routing call
// Orbit / Pan / Zoom instead.
func (c *Camera) Handle(events []app.MouseEvent) {
	for _, m := range events {
		switch m.Kind {
		case app.MouseDown:
			if m.Button == 1 || m.Button == 3 {
				c.dragging, c.panning = true, m.Button == 3 || m.Shift
				c.lastX, c.lastY = m.X, m.Y
			}
		case app.MouseUp:
			c.dragging = false
		case app.MouseMove:
			if !c.dragging {
				continue
			}
			dx, dy := m.X-c.lastX, m.Y-c.lastY
			c.lastX, c.lastY = m.X, m.Y
			if c.panning || m.Shift {
				c.Pan(dx, dy)
			} else {
				c.Orbit(dx, dy)
			}
		case app.MouseScroll:
			if m.ScrollY > 0 {
				c.Zoom(c.ZoomStep)
			} else if m.ScrollY < 0 {
				c.Zoom(1 / c.ZoomStep)
			}
		}
	}
}

// AnimateTo eases yaw and pitch to a view over SnapSeconds, yaw the short
// way round (a navcube facet's SnapAngles).
func (c *Camera) AnimateTo(yaw, pitch float32) {
	c.fromYaw, c.fromPitch = c.Yaw, c.Pitch
	d := math32.Mod(yaw-c.Yaw, 2*math32.Pi)
	if d > math32.Pi {
		d -= 2 * math32.Pi
	}
	if d < -math32.Pi {
		d += 2 * math32.Pi
	}
	c.toYaw, c.toPitch = c.Yaw+d, clamp(pitch, -c.PitchLimit, c.PitchLimit)
	c.t, c.anim = 0, true
}

// Animating reports an AnimateTo in progress.
func (c *Camera) Animating() bool { return c.anim }

// Step advances an AnimateTo by dt seconds.
func (c *Camera) Step(dt float32) {
	if !c.anim {
		return
	}
	secs := c.SnapSeconds
	if secs <= 0 {
		secs = 0.35
	}
	c.t += dt / secs
	if c.t >= 1 {
		c.t, c.anim = 1, false
	}
	e := 0.5 - 0.5*math32.Cos(c.t*math32.Pi)
	c.Yaw = c.fromYaw + (c.toYaw-c.fromYaw)*e
	c.Pitch = c.fromPitch + (c.toPitch-c.fromPitch)*e
}

// Eye is the camera's position.
func (c *Camera) Eye() core.Vec3 {
	cp := math32.Cos(c.Pitch)
	return c.Target.Add(core.V3(c.Distance*cp*math32.Sin(c.Yaw), c.Distance*math32.Sin(c.Pitch), c.Distance*cp*math32.Cos(c.Yaw)))
}

// Forward is the unit direction from the eye to the target.
func (c *Camera) Forward() core.Vec3 { return c.Target.Sub(c.Eye()).Normalize() }

// Matrices is the projection and the view for an aspect ratio (width over
// height) — apart, as a render.Post wants them.
func (c *Camera) Matrices(aspect float32) (proj, view core.Mat4) {
	return core.Mat4Perspective(c.FovY, aspect, c.Near, c.Far), core.Mat4LookAt(c.Eye(), c.Target, core.V3(0, 1, 0))
}

// ViewProj is the combined matrix for an aspect ratio.
func (c *Camera) ViewProj(aspect float32) core.Mat4 {
	proj, view := c.Matrices(aspect)
	return proj.Mul(view)
}

func clamp(v, lo, hi float32) float32 { return min(max(v, lo), hi) }
