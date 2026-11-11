package ui

import (
	"time"
	"unsafe"

	"github.com/sushvibez/gfx/core"
)

// ScrollState is the retained offset of a scrollable area.
type ScrollState struct {
	Y        float32 // vertical offset in pixels, 0 = top
	X        float32 // horizontal offset in pixels, 0 = left
	contentH float32
	viewH    float32
	contentW float32
	viewW    float32
	// Kinetic state: the speed a flick left behind (pixels per second) and
	// when it was last advanced. A flick keeps moving after the finger
	// lifts, slowing at flickDeceleration until it stops or hits an end.
	flingX, flingY float32
	flingT         time.Time
	// dragY0/dragX0 are the offsets the current drag started from: the
	// content follows the pointer, so the offset is start + travel.
	dragY0, dragX0 float32
	// moved is the frame clock when the offset last changed, for a bar
	// that shows itself while the view is moving and then fades (Qt's
	// ScrollBar is only `active` while its flickable moves). hasMoved
	// distinguishes "never" from "at clock zero", which is where a test or
	// a freshly started app begins.
	moved    time.Duration
	hasMoved bool
}

// noteMoved records that the view has just moved, on the frame loop's
// clock, so an auto-hiding bar knows to show itself.
func (s *ScrollState) noteMoved() { s.moved, s.hasMoved = core.AnimClock(), true }

// barAlpha is how visible an auto-hiding bar should be: solid while the
// view moves, then fading away over barFade.
func (s *ScrollState) barAlpha() float32 {
	const hold, fade = 400 * time.Millisecond, 300 * time.Millisecond
	if !s.hasMoved {
		return 0
	}
	since := core.AnimClock() - s.moved
	switch {
	case since < hold:
		return 1
	case since < hold+fade:
		return 1 - float32(since-hold)/float32(fade)
	}
	return 0
}

// flickDeceleration is how fast a flick slows, in pixels per second
// squared — Qt's Flickable default, which is what these pages were tuned
// against.
const flickDeceleration = 1500

// advanceFling moves the offsets for the time since the last frame and
// reports whether the flick is still alive (so the frame loop keeps
// running).
func (s *ScrollState) advanceFling(now time.Time) bool {
	if s.flingX == 0 && s.flingY == 0 {
		return false
	}
	if s.flingT.IsZero() {
		s.flingT = now
		return true
	}
	dt := float32(now.Sub(s.flingT).Seconds())
	s.flingT = now
	if dt <= 0 {
		return true
	}
	if dt > 0.1 {
		dt = 0.1 // a stalled frame must not teleport the content
	}
	decay := func(v, offset, maxOff float32, apply func(float32)) float32 {
		if v == 0 {
			return 0
		}
		next := offset + v*dt
		if next <= 0 || next >= maxOff {
			apply(min(max(next, 0), maxOff))
			return 0 // stop dead at the end, as Qt does without bounce
		}
		apply(next)
		drop := flickDeceleration * dt
		if abs32(v) <= drop {
			return 0
		}
		if v > 0 {
			return v - drop
		}
		return v + drop
	}
	s.flingY = decay(s.flingY, s.Y, s.maxY(), func(v float32) { s.Y = v })
	s.flingX = decay(s.flingX, s.X, s.maxX(), func(v float32) { s.X = v })
	return s.flingX != 0 || s.flingY != 0
}

// StopFling ends any kinetic motion (a press on the content catches it).
func (s *ScrollState) StopFling() { s.flingX, s.flingY, s.flingT = 0, 0, time.Time{} }

// maxY reports how far the content can scroll vertically.
func (s *ScrollState) maxY() float32 { return max(0, s.contentH-s.viewH) }

// MaxY exposes the vertical scroll range (content minus viewport, >= 0) —
// for app-drawn scrollbars and scroll-position readouts.
func (s *ScrollState) MaxY() float32 { return s.maxY() }

// ViewH exposes the viewport height from the last layout.
func (s *ScrollState) ViewH() float32 { return s.viewH }

// ContentH exposes the content height from the last layout (0 before the
// first one), the denominator for a scroll-position fraction.
func (s *ScrollState) ContentH() float32 { return s.contentH }

// ContentW / ViewW are ContentH / ViewH on the horizontal axis.
func (s *ScrollState) ContentW() float32 { return s.contentW }
func (s *ScrollState) ViewW() float32    { return s.viewW }

// maxX reports how far the content can scroll horizontally.
func (s *ScrollState) maxX() float32 { return max(0, s.contentW-s.viewW) }

// MaxX is MaxY on the horizontal axis.
func (s *ScrollState) MaxX() float32 { return s.maxX() }

// scrollBy moves the vertical offset, clamped to the content; it reports
// whether the offset moved (so unconsumed wheel deltas bubble to ancestors).
func (s *ScrollState) scrollBy(dy float32) bool {
	old := s.Y
	s.Y = min(max(s.Y+dy, 0), s.maxY())
	if s.Y != old {
		s.noteMoved()
		return true
	}
	return false
}

// scrollByX is scrollBy for the horizontal offset.
func (s *ScrollState) scrollByX(dx float32) bool {
	old := s.X
	s.X = min(max(s.X+dx, 0), s.maxX())
	if s.X != old {
		s.noteMoved()
		return true
	}
	return false
}

// Scroll builds a vertically scrollable column over state. Give it a
// fixed height (or let a parent stretch it) and children; content taller
// than the viewport clips and scrolls with the wheel.
func Scroll(st *ScrollState) *ScrollEl {
	return &ScrollEl{st: st, content: Div()}
}

// ScrollEl is the per-frame element of a ScrollState.
type ScrollEl struct {
	st       *ScrollState
	w, h     float32
	grow     float32
	content  *DivEl
	rect     Rect
	barC     RGBA
	hasBarC  bool
	bg       RGBA    // background behind the content (Bg); A 0 = none
	radius   float32 // background corner radius (Rounded)
	autoHide bool    // the bar shows only while the view moves or is hovered
	hovered  bool    // the pointer is over the viewport, recorded at layout
	flick    bool    // pans by drag, with a kinetic release (QML Flickable)
	// contentSized: the content keeps its own width instead of being
	// stretched to the viewport (QML's contentWidth: -1). See ContentSized.
	contentSized bool
	// fitWin >= 0: as tall as the content, but no taller than the
	// window less fitWin above and below (FitWindow); -1 = off.
	fitWin float32
	fitOn  bool
}

// AutoHideBar shows the scrollbar only while the view is moving OR the
// pointer is over it, fading it afterwards — Qt's ScrollView behaviour: it
// makes its bar `active` while the flickable moves and while the view is
// hovered, and the Basic style fades an inactive bar out. A bar painted
// permanently is a line down the side of every page Qt does not draw.
func (e *ScrollEl) AutoHideBar() *ScrollEl { e.autoHide = true; return e }

// ContentSized lays the content out at its OWN width rather than at least
// the viewport's — QML's `contentWidth: -1` ("adapt to the content"). The
// default stretches, which is what nearly every page wants; a page that
// asks its content to size itself would otherwise see the viewport's width
// in its own bindings (the sprite-stack page's card, whose width is
// max(content, ownWidth), settled at the full viewport instead of its
// content).
func (e *ScrollEl) ContentSized() *ScrollEl { e.contentSized = true; return e }

// FitWindow sizes the area to its content, capped at the window height less
// margin above and below: a list that fits shows whole, a longer one
// (a tall menu) scrolls.
func (e *ScrollEl) FitWindow(margin float32) *ScrollEl { e.fitWin, e.fitOn = margin, true; return e }

// Grow makes the scroll region flex into its parent's leftover space
// (flex_1), instead of a fixed W/H.
func (e *ScrollEl) Grow() *ScrollEl { e.grow = 1; return e }

// GrowBy is Grow with a weight; see DivEl.GrowBy.
func (e *ScrollEl) GrowBy(f float32) *ScrollEl { e.grow = f; return e }

func (e *ScrollEl) growFactor() float32 { return e.grow }

// BarColor sets the scrollbar thumb colour (default a translucent white that
// suits dark themes; set a dark colour for light backgrounds).
func (e *ScrollEl) BarColor(c RGBA) *ScrollEl { e.barC, e.hasBarC = c, true; return e }

// Bg fills the viewport behind the content, and Rounded rounds that fill,
// so a scrolling panel needs no wrapping Div for its background. The
// content is still clipped to the viewport's rectangle.
func (e *ScrollEl) Bg(c RGBA) *ScrollEl { e.bg = c; return e }

// Rounded sets the corner radius of the Bg fill.
func (e *ScrollEl) Rounded(px float32) *ScrollEl { e.radius = px; return e }

func (e *ScrollEl) W(px float32) *ScrollEl   { e.w = px; return e }
func (e *ScrollEl) H(px float32) *ScrollEl   { e.h = px; return e }
func (e *ScrollEl) P(px float32) *ScrollEl   { e.content.P(px); return e }
func (e *ScrollEl) Gap(px float32) *ScrollEl { e.content.Gap(px); return e }
func (e *ScrollEl) Children(els ...Element) *ScrollEl {
	e.content.Children(els...)
	return e
}

func (e *ScrollEl) measure(ctx *Context) (float32, float32) {
	cw, ch := e.content.measure(ctx)
	w, h := e.w, e.h
	if w == 0 {
		w = cw
	}
	if e.fitOn {
		h = ch
		if lim := ctx.winH - 2*e.fitWin; lim > 0 && h > lim {
			h = lim
		}
	}
	if h == 0 {
		h = 120
	}
	return w, h
}

// Flickable makes the area pan by dragging, with a kinetic flick on
// release — QML's Flickable (and ScrollView, which is one). Off by default:
// on the desktop a drag inside a list is usually a selection, and only
// markup that asks for Flickable semantics should get them.
func (e *ScrollEl) Flickable() *ScrollEl { e.flick = true; return e }

// scrollFlick is the scroll area's drag region. It is a separate type
// because the element already implements `contains` for wheel routing,
// which is not gated on flickability.
type scrollFlick struct{ e *ScrollEl }

func (f scrollFlick) contains(x, y float32) bool { return f.e.rect.contains(x, y) }

// The content follows the pointer.
func (f scrollFlick) drag(dx, dy float32, phase DragPhase) {
	st := f.e.st
	switch phase {
	case DragStart:
		st.StopFling() // a press catches the moving content
		st.dragY0, st.dragX0 = st.Y, st.X
	default:
		st.Y = min(max(st.dragY0-dy, 0), st.maxY())
		st.X = min(max(st.dragX0-dx, 0), st.maxX())
	}
}

// And keeps going when the pointer lets go: the content moves opposite the
// pointer, so flicking up scrolls down.
func (f scrollFlick) fling(vx, vy float32) {
	st := f.e.st
	st.flingX, st.flingY, st.flingT = -vx, -vy, time.Time{}
}

func (e *ScrollEl) layout(ctx *Context, r Rect) {
	e.rect = r
	e.hovered = ctx.MouseX >= r.X && ctx.MouseX < r.X+r.W &&
		ctx.MouseY >= r.Y && ctx.MouseY < r.Y+r.H
	if e.flick {
		ctx.drags = append(ctx.drags, scrollFlick{e})
	}
	// The viewport width is what the content has to work with: a wrapping
	// row inside a vertical scroll breaks into lines at the view's width,
	// not at whatever its children add up to.
	prevAvail := ctx.availW
	ctx.availW = r.W
	cw, ch := e.content.measure(ctx)
	ctx.availW = prevAvail
	e.st.contentH, e.st.viewH = ch, r.H
	if e.contentSized {
		e.st.contentW, e.st.viewW = cw, r.W
	} else {
		e.st.contentW, e.st.viewW = max(cw, r.W), r.W
	}
	e.st.Y = min(max(e.st.Y, 0), e.st.maxY())
	e.st.X = min(max(e.st.X, 0), e.st.maxX())
	// Registered BEFORE the content lays out, so nested regions come out
	// outermost-first: the wheel walks the list backwards and has to reach
	// the INNERMOST region under the pointer. Registered after, the page's
	// own scroll view was always last and swallowed every wheel — turning
	// the wheel over a list scrolled the page instead.
	ctx.scrolls = append(ctx.scrolls, e)
	e.content.layout(ctx, Rect{r.X - e.st.X, r.Y - e.st.Y, e.st.contentW, ch})
}

// dirty while a flick is carrying the view or the auto-hide bar is fading:
// both read the clock at paint time.
func (e *ScrollEl) dirty(*Context) bool {
	if e.st.flingX != 0 || e.st.flingY != 0 {
		return true
	}
	if e.autoHide && !e.hovered {
		if a := e.st.barAlpha(); a > 0 && a < 1 {
			return true
		}
	}
	return false
}

// paintSig: the offset and the bar (see damage.go).
func (e *ScrollEl) paintSig() uint64 {
	h := newSig(0x5c5)
	h.ptr(unsafe.Pointer(e.st))
	h.f32(e.st.X)
	h.f32(e.st.Y)
	h.col(e.barC)
	h.b(e.hasBarC)
	h.b(e.autoHide)
	h.b(e.hovered)
	h.col(e.bg)
	h.f32(e.radius)
	return uint64(h)
}

func (e *ScrollEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	// Carry a flick forward. Painting is where the frame clock is, and the
	// frame loop keeps running while an element asks for the next one.
	if e.st.advanceFling(ctx.animT) {
		ctx.wantsFrame = true
	}
	if e.bg.A > 0 {
		ctx.R.FillRoundRect(e.rect.X, e.rect.Y, e.rect.W, e.rect.H, e.radius, e.bg)
	}
	ctx.R.PushClip(e.rect.X, e.rect.Y, e.rect.W, e.rect.H)
	// CPU culling: content scrolled out of the viewport is not recorded at
	// all (the scissor would drop it anyway, after the upload). DivEl.paint
	// asks offView for each child; a nested scroll narrows the view.
	prevView, prevOn := ctx.view, ctx.viewOn
	view := e.rect
	if prevOn {
		view = intersectRect(view, prevView)
	}
	ctx.view, ctx.viewOn = view, true
	e.content.paint(ctx)
	ctx.view, ctx.viewOn = prevView, prevOn
	bc := RGBA{R: 1, G: 1, B: 1, A: 0.25}
	if e.hasBarC {
		bc = e.barC
	}
	// An auto-hiding bar shows itself while the view moves and then fades,
	// as Qt's does: a QML view has no bar of its own unless the markup
	// attaches a ScrollBar, so one painted permanently is a line down the
	// side of every page that Qt does not draw.
	if e.autoHide {
		a := e.st.barAlpha()
		if e.hovered {
			a = 1
		}
		bc.A *= a
		if bc.A <= 0.01 {
			ctx.R.PopClip()
			return
		}
	}
	// Scrollbar indicators when the content overflows.
	if m := e.st.maxY(); m > 0 {
		barH := max(24, e.rect.H*e.st.viewH/e.st.contentH)
		barY := e.rect.Y + (e.rect.H-barH)*(e.st.Y/m)
		ctx.R.FillRoundRect(e.rect.X+e.rect.W-5, barY, 3, barH, 1.5, bc)
	}
	if m := e.st.maxX(); m > 0 {
		barW := max(24, e.rect.W*e.st.viewW/e.st.contentW)
		barX := e.rect.X + (e.rect.W-barW)*(e.st.X/m)
		ctx.R.FillRoundRect(barX, e.rect.Y+e.rect.H-5, barW, 3, 1.5, bc)
	}
	ctx.R.PopClip()
}

// ScrollCulled counts subtrees a scroll viewport skipped painting because
// they lay wholly outside it (for GFX_FPS and tests; reset by the reader).
var ScrollCulled int

// viewCullMargin widens the viewport for the cull test: glyph overhang and
// antialiasing can reach a little past a layout rect.
const viewCullMargin = 4

// offView reports whether e can be skipped while painting inside a scroll
// viewport: no part of its subtree's paint bounds touches the view. Only
// paint is skipped — hit testing, focus and overlays come from layout,
// which still visits everything. Under a transform nothing is culled
// (layout rects are not where those pixels land), as with damage culling.
func (c *Context) offView(e Element) bool {
	if !c.viewOn || c.dmgLock > 0 || e == nil {
		return false
	}
	if d, ok := e.(*DivEl); ok {
		if _, xf := d.xform(); xf {
			return false
		}
	}
	view := inflate(c.view, viewCullMargin)
	if intersects(paintBoundsOf(e), view) {
		return false
	}
	// The element's own box is out of view; its descendants may still
	// overflow into it (anchored children, shadows), so check them all.
	if subtreeTouches(e, view) {
		return false
	}
	ScrollCulled++
	return true
}

// subtreeTouches reports whether any element under e (e excluded) paints
// into view, or might: a transformed box counts as touching, since its
// descendants' rects are pre-transform.
func subtreeTouches(e Element, view Rect) bool {
	for _, ch := range Children(e) {
		if ch == nil {
			continue
		}
		if d, ok := ch.(*DivEl); ok {
			if _, xf := d.xform(); xf {
				return true
			}
		}
		if intersects(paintBoundsOf(ch), view) || subtreeTouches(ch, view) {
			return true
		}
	}
	return false
}

// intersectRect is the overlap of a and b (zero-sized when they are
// disjoint).
func intersectRect(a, b Rect) Rect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	return Rect{x0, y0, max(x1-x0, 0), max(y1-y0, 0)}
}

func (e *ScrollEl) hit(x, y float32) Element {
	if !e.rect.contains(x, y) {
		return nil
	}
	return e.content.hit(x, y)
}

func (e *ScrollEl) onClick() func() { return nil }

// childElems exposes the scrolled content, so a walk of a built tree
// (ui.Children — tooling and tests) reaches what is inside a view instead
// of stopping at it.
func (e *ScrollEl) childElems() []Element {
	if e.content == nil {
		return nil
	}
	return []Element{e.content}
}
func (e *ScrollEl) bounds() Rect { return e.rect }

// scrollTarget implementation.
func (e *ScrollEl) contains(x, y float32) bool { return e.rect.contains(x, y) }
func (e *ScrollEl) scroll(dy float32) bool     { return e.st.scrollBy(dy) }
func (e *ScrollEl) scrollX(dx float32) bool    { return e.st.scrollByX(dx) }
func (e *ScrollEl) viewRect() Rect             { return e.rect }
func (e *ScrollEl) thumbRect() Rect            { return thumbRectFor(e.rect, e.st) }
func (e *ScrollEl) scrollToY(y float32)        { scrollToYFor(e.rect, e.st, y) }

func (e *ScrollEl) fixedWH() (float32, float32) { return e.w, e.h }

// thumbRectFor computes a scrollbar thumb's grab rectangle (a 14px-wide hit
// area on the right edge), zero W when there's nothing to scroll.
func thumbRectFor(view Rect, s *ScrollState) Rect {
	m := s.maxY()
	if m <= 0 {
		return Rect{}
	}
	barH := max(24, view.H*s.viewH/s.contentH)
	barY := view.Y + (view.H-barH)*(s.Y/m)
	return Rect{X: view.X + view.W - 14, Y: barY, W: 14, H: barH}
}

// scrollToYFor maps a dragged thumb's pixel y to a scroll offset.
func scrollToYFor(view Rect, s *ScrollState, y float32) {
	barH := max(24, view.H*s.viewH/s.contentH)
	track := view.H - barH
	if track <= 0 {
		return
	}
	f := (y - view.Y - barH/2) / track
	s.Y = min(max(f, 0), 1) * s.maxY()
}
