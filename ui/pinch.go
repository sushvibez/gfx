package ui

import (
	"math"

	"github.com/sushvibez/gfx/app"
)

// Pinching: two fingers spreading or closing over an element (QML's
// PinchHandler), from either source the platforms have:
//
//   - a touchscreen, through UI.Touch: a pinch starts when a second finger
//     lands (DragStart), reports each frame's change (DragMove) and ends
//     when the fingers lift (DragEnd);
//   - a touchpad, whose pinch Windows and the browsers send as Ctrl+wheel:
//     each event is one step of the pinch, reported as a DragMove alone
//     (there is no start or end to see).
//
// factor is the change in scale since the last call (multiply a scale by
// it; 1 at DragStart and DragEnd), cx, cy the pinch's centre in window
// pixels.

// PinchStepPerNotch is the scale change of one wheel notch's worth of
// touchpad pinch: 15 %, the same step the game shell turns a touchscreen
// pinch into wheel notches at.
const PinchStepPerNotch = 1.15

// OnPinch installs a pinch handler (see above).
func (d *DivEl) OnPinch(fn func(factor, cx, cy float32, phase DragPhase)) *DivEl {
	d.pinchFn = fn
	return d
}

func (d *DivEl) pinch(factor, cx, cy float32, phase DragPhase) {
	if d.pinchFn != nil {
		d.pinchFn(factor, cx, cy, phase)
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
	d.pinch(f, m.X, m.Y, DragMove)
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
			d.pinch(1, g.CX, g.CY, DragStart)
		}
	case g.Pinching:
		if g.ScaleD > 0 && g.ScaleD != 1 {
			u.pinching.pinch(g.ScaleD, g.CX, g.CY, DragMove)
		}
	case u.pinching != nil:
		u.pinching.pinch(1, g.CX, g.CY, DragEnd)
		u.pinching = nil
	}
}
