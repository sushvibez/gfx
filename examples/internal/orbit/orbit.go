// Package orbit is a drag-to-orbit, wheel-to-zoom camera for the 3D
// examples — and the prototype for the scene module's camera controllers.
package orbit

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// Camera orbits a target point.
type Camera struct {
	Target   core.Vec3
	Distance float32
	Yaw      float32 // radians around +Y
	Pitch    float32 // radians above the horizon

	dragging     bool
	lastX, lastY float32
}

// Handle consumes a frame's mouse events: primary drag orbits, wheel zooms.
func (c *Camera) Handle(events []app.MouseEvent) {
	for _, m := range events {
		switch m.Kind {
		case app.MouseDown:
			if m.Button == 1 {
				c.dragging = true
				c.lastX, c.lastY = m.X, m.Y
			}
		case app.MouseUp:
			if m.Button == 1 {
				c.dragging = false
			}
		case app.MouseMove:
			if c.dragging {
				c.Yaw += (m.X - c.lastX) * 0.008
				c.Pitch += (m.Y - c.lastY) * 0.008
				c.Pitch = min(max(c.Pitch, -1.5), 1.5)
				c.lastX, c.lastY = m.X, m.Y
			}
		case app.MouseScroll:
			c.Distance *= 1 + m.ScrollY*0.002
			c.Distance = min(max(c.Distance, 0.3), 200)
		}
	}
}

// Eye returns the camera position.
func (c *Camera) Eye() core.Vec3 {
	cp := math32.Cos(c.Pitch)
	return c.Target.Add(core.V3(
		c.Distance*cp*math32.Sin(c.Yaw),
		c.Distance*math32.Sin(c.Pitch),
		c.Distance*cp*math32.Cos(c.Yaw),
	))
}

// ViewProj returns the combined matrix and the eye position for a target
// of the given pixel size.
func (c *Camera) ViewProj(w, h int) (core.Mat4, core.Vec3) {
	eye := c.Eye()
	view := core.Mat4LookAt(eye, c.Target, core.V3(0, 1, 0))
	proj := core.Mat4Perspective(math32.Pi/3, float32(w)/float32(h), 0.05, 500)
	return proj.Mul(view), eye
}
