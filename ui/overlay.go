package ui

// Overlay floats child at an absolute pixel position above the rest of the
// tree — tooltips, dropdowns, context menus, popovers. It occupies no space
// in normal flow; the child is painted after the main tree (on top, free of
// any clip) and hit-tested before it. Build it inside the normal tree where
// the trigger lives, gating on your own open/visible state.
//
//	if menuOpen {
//	    root.Children(ui.Overlay(menuPanel(), btnX, btnY+btnH))
//	}
//
// Anchor names a corner or edge-midpoint of an overlay for Pin (GPUI's
// Anchor): the named point of the OVERLAY is placed at the computed position.
type Anchor int

const (
	AnchorTopLeft Anchor = iota
	AnchorTopCenter
	AnchorTopRight
	AnchorLeftCenter
	AnchorRightCenter
	AnchorBottomLeft
	AnchorBottomCenter
	AnchorBottomRight
)

type OverlayEl struct {
	child   Element
	x, y    float32
	anchor  Element // when set, position derives from this element's rect
	ax, ay  float32 // offset from the anchor's top-left
	below   bool    // offset from the anchor's BOTTOM-left instead
	beside  bool    // ... or from its top-RIGHT corner (a submenu)
	above   bool    // ... or centred ABOVE it (a tooltip)
	pin     Anchor  // which point of the overlay lands on the position
	snap    float32 // snap-to-window margin (GPUI snap_to_window_with_margin); <0 = off
	outside func()  // dismiss on a click outside the child (on_mouse_down_out)
	rect    Rect
}

// Overlay places child with its top-left at (x, y).
func Overlay(child Element, x, y float32) *OverlayEl {
	return &OverlayEl{child: child, x: x, y: y, snap: -1}
}

// AnchorTo positions the overlay relative to another element's top-left
// (resolved at layout time each frame, so it tracks the anchor through
// resizes and reflows). The anchor must be laid out before this overlay —
// build the overlay after it in the tree.
func (o *OverlayEl) AnchorTo(el Element, dx, dy float32) *OverlayEl {
	o.anchor, o.ax, o.ay, o.below = el, dx, dy, false
	return o
}

// AnchorBelow is AnchorTo from the anchor's bottom-left corner (a dropdown/
// popover under its trigger).
func (o *OverlayEl) AnchorBelow(el Element, gap float32) *OverlayEl {
	o.anchor, o.ax, o.ay, o.below = el, 0, gap, true
	return o
}

// AnchorRight sits the overlay against the anchor's right edge, its top
// dy below the anchor's, gap px clear — a submenu beside its entry.
// Resolved at layout time, when the anchor's width is known.
func (o *OverlayEl) AnchorRight(el Element, gap, dy float32) *OverlayEl {
	o.anchor, o.ax, o.ay, o.below, o.above, o.beside = el, gap, dy, false, false, true
	return o
}

// AnchorAbove sits the overlay above the anchor, centred on it and gap px
// clear — QML's ToolTip placement (`x: (parent.width - implicitWidth) / 2;
// y: -implicitHeight - 4`).
func (o *OverlayEl) AnchorAbove(el Element, gap float32) *OverlayEl {
	o.anchor, o.ax, o.ay, o.above = el, 0, -gap, true
	return o
}

// SnapToWindow keeps the overlay fully on-screen, at least margin px from
// every window edge (GPUI anchored().snap_to_window_with_margin).
func (o *OverlayEl) SnapToWindow(margin float32) *OverlayEl { o.snap = margin; return o }

// Pin places the named point of the overlay (rather than its top-left) at
// the computed position — GPUI's anchored().anchor(corner). Bottom anchors
// open the overlay upward, Right anchors leftward.
func (o *OverlayEl) Pin(a Anchor) *OverlayEl { o.pin = a; return o }

// OnClickOutside dismisses via fn when a press lands outside the overlay's
// child (GPUI on_mouse_down_out). The click is consumed.
func (o *OverlayEl) OnClickOutside(fn func()) *OverlayEl { o.outside = fn; return o }

func (o *OverlayEl) measure(*Context) (float32, float32) { return 0, 0 } // no flow space

// outOfFlow: overlays self-position and must not affect flow layout — no
// size, no gap slot (CSS position:absolute semantics).
func (o *OverlayEl) outOfFlow() bool { return true }

func (o *OverlayEl) layout(ctx *Context, _ Rect) {
	x, y := o.x, o.y
	cw, ch := o.child.measure(ctx)
	if o.anchor != nil {
		b := o.anchor.bounds()
		x, y = b.X+o.ax, b.Y+o.ay
		switch {
		case o.below:
			y += b.H
		case o.beside:
			x += b.W
		case o.above:
			// Centred over the anchor and clear of it (a tooltip): needs
			// both the anchor's width and the overlay's own size, which is
			// why it resolves here rather than at build time.
			x += (b.W - cw) / 2
			y -= ch
		}
	}
	switch o.pin {
	case AnchorTopCenter:
		x -= cw / 2
	case AnchorTopRight:
		x -= cw
	case AnchorLeftCenter:
		y -= ch / 2
	case AnchorRightCenter:
		x, y = x-cw, y-ch/2
	case AnchorBottomLeft:
		y -= ch
	case AnchorBottomCenter:
		x, y = x-cw/2, y-ch
	case AnchorBottomRight:
		x, y = x-cw, y-ch
	}
	if o.snap >= 0 {
		x = min(max(x, o.snap), ctx.winW-cw-o.snap)
		y = min(max(y, o.snap), ctx.winH-ch-o.snap)
	}
	o.rect = Rect{x, y, cw, ch}
	// Register the invisible dismiss scrim under the child, and the child
	// BEFORE laying it out — overlays nested inside the child then register
	// after it, so they paint above it and hit-test before it.
	if o.outside != nil {
		ctx.overlays = append(ctx.overlays, &scrimEl{rect: Rect{0, 0, ctx.winW, ctx.winH}, dismiss: o.outside})
	}
	ctx.overlays = append(ctx.overlays, o.child)
	o.child.layout(ctx, o.rect)
}

// paint is deferred: UI.Frame paints the registered child after the main
// tree, so nothing happens here inline.
func (o *OverlayEl) paint(*Context) {}

func (o *OverlayEl) hit(float32, float32) Element { return nil } // child hit via ctx.overlays

// childElems exposes the floated child so a walk of the tree (ui.Children)
// reaches what an open dropdown or menu is showing.
func (o *OverlayEl) childElems() []Element { return []Element{o.child} }
func (o *OverlayEl) onClick() func()       { return nil }
func (o *OverlayEl) bounds() Rect          { return o.rect }

// ModalEl floats child above everything with an optional dim scrim that catches
// outside clicks (closePolicy). Position is Centered (Popup/Dialog) or an
// explicit point (Menu/dropdown). The scrim and child are both registered as
// overlays so they paint on top and hit-test before the main tree.
type ModalEl struct {
	child     Element
	dim       RGBA   // scrim color (A>0 paints + blocks); zero = no scrim
	onDismiss func() // outside-click / scrim handler
	centered  bool
	x, y      float32 // used when !centered
	rect      Rect
}

// Modal centers child over the window with a dim scrim; onDismiss fires on an
// outside (scrim) click. Use for Popup/Dialog.
func Modal(child Element, dim RGBA, onDismiss func()) *ModalEl {
	return &ModalEl{child: child, dim: dim, onDismiss: onDismiss, centered: true}
}

// PositionedModal floats child at (x,y) with an (optionally transparent) scrim
// for outside-click dismissal. Use for Menu/dropdown.
func PositionedModal(child Element, x, y float32, dim RGBA, onDismiss func()) *ModalEl {
	return &ModalEl{child: child, dim: dim, onDismiss: onDismiss, x: x, y: y}
}

func (m *ModalEl) measure(*Context) (float32, float32) { return 0, 0 }

// outOfFlow: see OverlayEl.outOfFlow.
func (m *ModalEl) outOfFlow() bool { return true }

func (m *ModalEl) layout(ctx *Context, _ Rect) {
	// Scrim fills the window and dismisses on click; registered first so it
	// paints under the content and is hit AFTER it (overlays hit-test in
	// reverse registration order).
	if m.dim.A > 0 || m.onDismiss != nil {
		scrim := &scrimEl{rect: Rect{0, 0, ctx.winW, ctx.winH}, dim: m.dim, dismiss: m.onDismiss}
		ctx.overlays = append(ctx.overlays, scrim)
	}
	cw, ch := m.child.measure(ctx)
	var x, y float32
	if m.centered {
		x, y = (ctx.winW-cw)/2, (ctx.winH-ch)/2
	} else {
		x, y = m.x, m.y
		// Keep the panel on-screen.
		if x+cw > ctx.winW {
			x = ctx.winW - cw
		}
		if y+ch > ctx.winH {
			y = ctx.winH - ch
		}
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
	}
	m.rect = Rect{x, y, cw, ch}
	m.child.layout(ctx, m.rect)
	ctx.overlays = append(ctx.overlays, m.child)
}

func (m *ModalEl) paint(*Context)               {}
func (m *ModalEl) hit(float32, float32) Element { return nil }

// childElems: see OverlayEl.childElems.
func (m *ModalEl) childElems() []Element { return []Element{m.child} }
func (m *ModalEl) onClick() func()       { return nil }
func (m *ModalEl) bounds() Rect          { return m.rect }

// scrimEl is the full-window backdrop behind a modal: paints a dim fill and
// swallows clicks (firing onClick to dismiss).
type scrimEl struct {
	rect    Rect
	dim     RGBA
	dismiss func()
}

func (s *scrimEl) measure(*Context) (float32, float32) { return s.rect.W, s.rect.H }
func (s *scrimEl) layout(*Context, Rect)               {}
func (s *scrimEl) paint(ctx *Context) {
	if ctx.culled(s) {
		return
	}
	if s.dim.A > 0 {
		ctx.R.FillRect(s.rect.X, s.rect.Y, s.rect.W, s.rect.H, ctx.color(s.dim))
	}
}
func (s *scrimEl) hit(x, y float32) Element {
	if s.rect.contains(x, y) {
		return s
	}
	return nil
}
func (s *scrimEl) onClick() func() { return s.dismiss }
func (s *scrimEl) bounds() Rect    { return s.rect }
