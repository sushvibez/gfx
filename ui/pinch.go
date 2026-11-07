package ui

import (
	"math"

	"github.com/sushvibez/gfx/app"
)

// Pinching: two fingers spreading, closing, turning or moving together
// over an element (QML's PinchHandler), from either source the platforms
// have:
//
//   - a touchscreen, through UI.Touch: a pinch starts when a second finger
//     lands (DragStart), reports each frame's change (DragMove) and ends
//     when the fingers lift (DragEnd);
//   - a touchpad, whose pinch Windows and the browsers send as Ctrl+wheel:
//     each event is one step of scale alone, a DragMove with no start or
//     end to see.

// PinchEvent is one step of a pinch. Scale is the change in scale since
// the last step (multiply by it; 1 at DragStart and DragEnd), Rotation the
// change in angle in degrees clockwise, DX, DY how far the fingers' centre
// moved, and CX, CY where it is now, in window pixels.
type PinchEvent struct {
	Phase    DragPhase
	Scale    float32
	Rotation float32
	DX, DY   float32
	CX, CY   float32
}

// PinchStepPerNotch is the scale change of one wheel notch's worth of
// touchpad pinch: 15 %, the same step the game shell turns a touchscreen
// pinch into wheel notches at.
const PinchStepPerNotch = 1.15

// OnPinch installs a pinch handler (see above).
func (d *DivEl) OnPinch(fn func(PinchEvent)) *DivEl {
	d.pinchFn = fn
	return d
}

func (d *DivEl) pinch(e PinchEvent) {
	if d.pinchFn != nil {
		d.pinchFn(e)
	}
}

// pinchAt is the deepest pinch region under (x, y), or nil.
func (u *UI) pinchAt(x, y float32) *DivEl {
	for i := len(u.ctx.pinches) - 1; i >= 0; i-- {
		if u.ctx.pinches[i].rect.contains(x, y) {
			return u.ctx.pinches[i]
		}
	}
	return nil
}

// wheelPinch takes a Ctrl+wheel event as a touchpad pinch step when a
// pinch region is under the pointer, reporting whether it did.
func (u *UI) wheelPinch(m app.MouseEvent) bool {
	if !m.Ctrl || m.ScrollY == 0 {
		return false
	}
	d := u.pinchAt(m.X, m.Y)
	if d == nil {
		return false
	}
	// The wheel turned away (negative ScrollY) is a spread: zoom in.
	f := float32(math.Pow(PinchStepPerNotch, float64(-m.ScrollY/app.WheelNotchPx)))
	d.pinch(PinchEvent{Phase: DragMove, Scale: f, CX: m.X, CY: m.Y})
	return true
}

// Touch feeds one frame's touchscreen events (call it every frame the
// window has touch, with the full drain): two fingers over a pinch region
// pinch it. Mouse emulation of the first finger still arrives through
// Mouse.
func (u *UI) Touch(events []app.TouchEvent) {
	g := &u.touch
	g.Feed(events)
	switch {
	case g.Pinching && u.pinching == nil:
		if d := u.pinchAt(g.CX, g.CY); d != nil {
			u.pinching = d
			d.pinch(PinchEvent{Phase: DragStart, Scale: 1, CX: g.CX, CY: g.CY})
		}
	case g.Pinching:
		if g.ScaleD != 1 || g.AngleD != 0 || g.DX != 0 || g.DY != 0 {
			u.pinching.pinch(PinchEvent{Phase: DragMove, Scale: g.ScaleD, Rotation: g.AngleD,
				DX: g.DX, DY: g.DY, CX: g.CX, CY: g.CY})
		}
	case u.pinching != nil:
		u.pinching.pinch(PinchEvent{Phase: DragEnd, Scale: 1, CX: g.CX, CY: g.CY})
		u.pinching = nil
	}
}
