package ui

import "time"

// Pointer gestures beyond press, click and drag, for QML's MouseArea:
// double-click, press-and-hold, the wheel, and where the pointer is while
// it hovers. Each is a DivEl builder; the UI drives them from Mouse.

// Gesture timings, Qt's defaults.
const (
	doubleClickInterval = 400 * time.Millisecond // QStyleHints::mouseDoubleClickInterval
	pressAndHoldDelay   = 800 * time.Millisecond // QStyleHints::mousePressAndHoldInterval
	pressAndHoldSlop    = 8                      // pixels the pointer may wander and still be held
)

// nowFn is the gesture clock; tests replace it.
var nowFn = time.Now

// OnDoubleClick runs fn when a second click lands on the element within
// Qt's double-click interval of the first. Both clicks still run OnClick,
// as QML's MouseArea emits clicked for each before doubleClicked.
func (d *DivEl) OnDoubleClick(fn func()) *DivEl {
	d.dblFn = fn
	if d.click == nil {
		d.click = func() {} // the element must be a click target to see clicks
	}
	return d
}

// OnPressAndHold runs fn once a press has been held still on the element
// for Qt's press-and-hold delay; the release that follows is then not a
// click (MouseArea.onPressAndHold).
func (d *DivEl) OnPressAndHold(fn func()) *DivEl {
	d.holdFn = fn
	if d.click == nil {
		d.click = func() {}
	}
	return d
}

// OnWheel makes the element a wheel target: fn gets each wheel delta in
// pixels (positive down and right, see app.MouseEvent) and reports whether
// it used it; a delta it leaves bubbles to the scroll region around it.
func (d *DivEl) OnWheel(fn func(dx, dy float32) bool) *DivEl {
	d.wheelFn = fn
	return d
}

// OnHoverMove reports where the pointer is over the element, in its own
// coordinates, each layout while it hovers (MouseArea.mouseX/mouseY with
// hoverEnabled; during a press OnPointer gives the same in window terms).
func (d *DivEl) OnHoverMove(fn func(x, y float32)) *DivEl {
	d.hoverMoveFn = fn
	return d
}

// wheelTarget adapts a DivEl with OnWheel to the scroll-target list the
// UI routes the wheel through. It has no scrollbar.
type wheelTarget struct{ d *DivEl }

func (w wheelTarget) contains(x, y float32) bool { return w.d.rect.contains(x, y) }
func (w wheelTarget) scroll(dy float32) bool     { return w.d.wheelFn(0, dy) }
func (w wheelTarget) scrollX(dx float32) bool    { return w.d.wheelFn(dx, 0) }
func (w wheelTarget) thumbRect() Rect            { return Rect{} }
func (w wheelTarget) scrollToY(float32)          {}
func (w wheelTarget) viewRect() Rect             { return w.d.rect }

// gestureState is the UI's memory of the last click and the press being
// held, for double-click and press-and-hold.
type gestureState struct {
	lastClick   time.Time
	lastClickAt Rect
	holdEl      *DivEl
	holdAt      time.Time
	holdFired   bool
}

// pressed records a press for press-and-hold.
func (g *gestureState) pressed(el Element) {
	g.holdEl, g.holdFired = nil, false
	if d, ok := el.(*DivEl); ok && d.holdFn != nil {
		g.holdEl, g.holdAt = d, nowFn()
	}
}

// tick fires a press-and-hold whose delay has passed, while the press is
// still down and has not wandered; reports whether one fired.
func (g *gestureState) tick(u *UI, x, y float32) bool {
	if g.holdEl == nil || g.holdFired || !u.ctx.MousePressed {
		return false
	}
	if abs32(x-u.ctx.pressX) > pressAndHoldSlop || abs32(y-u.ctx.pressY) > pressAndHoldSlop {
		g.holdEl = nil
		return false
	}
	if nowFn().Sub(g.holdAt) < pressAndHoldDelay {
		return false
	}
	g.holdFired = true
	g.holdEl.holdFn()
	return true
}

// clicked is told of a click on el and runs its double-click handler when
// it is the second one in time.
func (g *gestureState) clicked(el Element) {
	d, ok := el.(*DivEl)
	if !ok || d.dblFn == nil {
		g.lastClick = time.Time{}
		return
	}
	now := nowFn()
	if !g.lastClick.IsZero() && g.lastClickAt == d.rect && now.Sub(g.lastClick) <= doubleClickInterval {
		g.lastClick = time.Time{} // a third click starts a new pair
		d.dblFn()
		return
	}
	g.lastClick, g.lastClickAt = now, d.rect
}

// holding reports a press-and-hold still waiting for its delay, so the
// frame loop keeps running (WantsFrame) to fire it on time.
func (g *gestureState) holding(u *UI) bool {
	return g.holdEl != nil && !g.holdFired && u.ctx.MousePressed
}
