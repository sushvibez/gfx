package p3

import (
	"testing"

	"github.com/sushvibez/gfx/core"
)

// Conservative advancement stops within `skin` (1e-3) of the exact TOI.
const toiEps = 5e-3

func TestCastSphereTOI(t *testing.T) {
	w := NewWorld()
	id := core.QuatIdentity()
	target := w.AddBody(Sphere(1), core.V3(10, 0, 0), id, 1)

	// Exact TOI along the center line: 10 - 1 - 0.5.
	h, ok := w.CastSphere(core.V3(0, 0, 0), 0.5, core.V3(1, 0, 0), 20, LayerAll)
	if !ok || h.Body != target {
		t.Fatalf("cast missed: %+v %v", h, ok)
	}
	near(t, h.T, 8.5, toiEps, "sphere-sphere TOI")
	near(t, h.Point.X, 9, toiEps, "contact point X")
	near(t, h.Normal.X, -1, 1e-2, "contact normal X")

	// Mask pass-through.
	if _, ok := w.CastSphere(core.V3(0, 0, 0), 0.5, core.V3(1, 0, 0), 20, 0b10); ok {
		t.Fatal("masked cast reported hit")
	}
}

func TestCastCapsuleTOI(t *testing.T) {
	w := NewWorld()
	id := core.QuatIdentity()
	wall := w.AddBody(Box(core.V3(0.5, 5, 5)), core.V3(10, 0, 0), id, 0b01)

	// Wall face at x=9.5; capsule surface leads by r: TOI = 9.5 - 0.5.
	a, b := core.V3(0, -1, 0), core.V3(0, 1, 0)
	h, ok := w.CastCapsule(a, b, 0.5, core.V3(1, 0, 0), 20, LayerAll)
	if !ok || h.Body != wall {
		t.Fatalf("cast missed: %+v %v", h, ok)
	}
	near(t, h.T, 9, toiEps, "capsule-wall TOI")
	near(t, h.Normal.X, -1, 1e-2, "capsule-wall normal X")

	// Out of range / masked / parallel.
	if _, ok := w.CastCapsule(a, b, 0.5, core.V3(1, 0, 0), 5, LayerAll); ok {
		t.Fatal("cast hit beyond range")
	}
	if _, ok := w.CastCapsule(a, b, 0.5, core.V3(1, 0, 0), 20, 0b10); ok {
		t.Fatal("masked capsule cast reported hit")
	}
	if _, ok := w.CastCapsule(a, b, 0.5, core.V3(0, 0, 1), 20, LayerAll); ok {
		t.Fatal("parallel capsule cast reported hit")
	}
}

// TestCastRestingContact: touching a surface but moving away (or along it)
// must not block — the rule that keeps a character sliding on a floor.
func TestCastRestingContact(t *testing.T) {
	w := NewWorld()
	id := core.QuatIdentity()
	w.AddBody(Box(core.V3(0.5, 5, 5)), core.V3(10, 0, 0), id, 1)

	// Unit sphere resting on the wall face (center x=8.5), cast away.
	if _, ok := w.CastSphere(core.V3(8.5, 0, 0), 1, core.V3(-1, 0, 0), 5, LayerAll); ok {
		t.Fatal("moving away from a resting contact reported hit")
	}
	// Sliding along the face must travel, not deadlock at t=0.
	if h, ok := w.CastSphere(core.V3(8.5, 0, 0), 1, core.V3(0, 0, 1), 3, LayerAll); ok {
		t.Fatalf("sliding along a resting contact blocked at t=%v", h.T)
	}
}

func TestClosestPoint(t *testing.T) {
	w := NewWorld()
	id := core.QuatIdentity()
	sphere := w.AddBody(Sphere(1), core.V3(0, 0, 10), id, 0b01)
	box := w.AddBody(Box(core.V3(1, 1, 1)), core.V3(20, 0, 0), id, 0b10)
	w.AddTrigger(Sphere(5), core.V3(0, 0, 5), id, LayerAll) // must be invisible

	// Nearest is the sphere surface at (0,0,9).
	h, ok := w.ClosestPoint(core.V3(0, 0, 5), 100, LayerAll)
	if !ok || h.Body != sphere {
		t.Fatalf("closest = %+v %v, want sphere", h, ok)
	}
	near(t, h.T, 4, 1e-3, "closest distance")
	near(t, h.Point.Z, 9, 1e-3, "closest point Z")
	near(t, h.Normal.Z, -1, 1e-3, "closest normal Z")

	// Mask selects the box even though the sphere is nearer.
	h, ok = w.ClosestPoint(core.V3(0, 0, 5), 100, 0b10)
	if !ok || h.Body != box {
		t.Fatalf("masked closest = %+v %v, want box", h, ok)
	}
	// Box surface point nearest to (0,0,5) is the clamped (19,0,1).
	near(t, h.T, core.V3(19, 0, 1).Sub(core.V3(0, 0, 5)).Length(), 1e-2, "masked distance")

	// maxDist excludes everything.
	if _, ok := w.ClosestPoint(core.V3(0, 0, 5), 1, LayerAll); ok {
		t.Fatal("closest beyond maxDist reported")
	}
	// Inside a body: distance 0, point is the query point.
	h, ok = w.ClosestPoint(core.V3(0, 0, 10), 100, LayerAll)
	if !ok || h.Body != sphere {
		t.Fatalf("inside closest = %+v %v", h, ok)
	}
	near(t, h.T, 0, 1e-6, "inside distance")
}
