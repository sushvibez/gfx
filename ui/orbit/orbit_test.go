package orbit

import (
	"testing"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func near(t *testing.T, what string, got, want float32) {
	t.Helper()
	if math32.Abs(got-want) > 1e-4 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// The eye is where navcube's convention puts it, and the controls move it
// the documented way.
func TestCamera(t *testing.T) {
	c := New(core.V3(1, 0, 2), 10)
	c.Yaw, c.Pitch = 0, 0
	e := c.Eye()
	near(t, "eye x", e.X, 1)
	near(t, "eye z", e.Z, 12) // yaw 0: on +Z, looking along −Z
	c.Orbit(100, 0)
	if c.Yaw >= 0 {
		t.Errorf("a drag right should swing yaw negative: %v", c.Yaw)
	}
	c.Orbit(0, 1e6)
	near(t, "pitch clamped", c.Pitch, c.PitchLimit)
	c.Zoom(1000)
	near(t, "zoom clamped", c.Distance, c.MaxDistance)
	c.Reset()
	near(t, "reset distance", c.Distance, 10)

	// AnimateTo goes the short way round and lands exactly.
	c.Yaw = 3
	c.AnimateTo(-3, 0.5)
	for i := 0; i < 60 && c.Animating(); i++ {
		c.Step(1.0 / 60)
	}
	near(t, "snap pitch", c.Pitch, 0.5)
	if d := math32.Mod(c.Yaw+3+2*math32.Pi, 2*math32.Pi); d > 1e-3 && d < 2*math32.Pi-1e-3 {
		t.Errorf("snap yaw %v, want −3 (mod 2π)", c.Yaw)
	}
	if c.Yaw < 3 || c.Yaw > 3.6 {
		t.Errorf("snap went the long way: yaw %v", c.Yaw)
	}

	// Handle: a primary drag orbits, the wheel zooms, Shift pans.
	c = New(core.V3(0, 0, 0), 10)
	y0 := c.Yaw
	c.Handle([]app.MouseEvent{{Kind: app.MouseDown, Button: 1}, {Kind: app.MouseMove, X: 50}, {Kind: app.MouseUp, Button: 1}})
	if c.Yaw == y0 {
		t.Error("a drag did not orbit")
	}
	c.Handle([]app.MouseEvent{{Kind: app.MouseScroll, ScrollY: 1}})
	near(t, "wheel zoom", c.Distance, 10*c.ZoomStep)
	c.Handle([]app.MouseEvent{{Kind: app.MouseDown, Button: 1, Shift: true}, {Kind: app.MouseMove, X: 100, Shift: true}, {Kind: app.MouseUp, Button: 1}})
	if c.Target == (core.Vec3{}) {
		t.Error("a Shift drag did not pan")
	}
}
