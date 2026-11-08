package ui

import (
	"slices"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
)

// UI drives a view: every Frame it rebuilds the element tree from the
// render function (immediate), lays it out, and paints it; state read by
// the render function lives in core.Property values (retained). Clicks
// hit-test the most recent layout.
type UI struct {
	ctx Context
	// layerSeen is when each layer was last painted, in RenderLayers
	// calls (layerTick), so idle targets can be freed (layer.go).
	layerSeen   map[*LayerState]uint64
	layerTick   uint64
	view        func() Element
	root        Element
	pressTarget Rect
	// laidOut: the current root was built and laid out for the current
	// window size and not yet painted — Frame can paint it as is.
	laidOut bool
	// dmg is the damage-driven painting state (nil until EnableDamage).
	dmg *damageState

	// dragSlider tracks an in-progress slider drag by value property and
	// layout rect (element pointers go stale across per-frame rebuilds;
	// the property and rect stay valid). dragFrom/dragTo remap the track
	// fraction onto the slider's value range.
	dragSlider       *core.Property[float32]
	dragRect         Rect
	dragFrom, dragTo float32
	dragDial         *DialEl // the dial being turned, if any
	dragStep         float32 // the dragged slider's stepSize (0 = continuous)
	dragSnap         bool    // ... and whether the drag snaps to it
	dragMoved        func()  // slider's user-change callback (OnMoved)
	dragVert         bool    // the dragged track runs bottom-to-top
	// dragRegion is the element following the pointer between press and
	// release (a tumbler's wheel), and dragMoved32 how far it has travelled
	// — past a few pixels the release is a drag, not a click.
	dragRegion dragRegion
	dragTravel float32
	// Pointer velocity for a kinetic release, sampled per frame (mouse
	// events arrive in per-frame batches, so that is the resolution the
	// clock has anyway).
	velX, velY float32
	velT       time.Time
	velLastX   float32
	velLastY   float32
	// The other handle of a range slider, which the dragged one cannot
	// pass: dragLo is set when dragging the upper handle, dragHi the lower.
	dragLo, dragHi *core.Property[float32]

	// focusIdx indexes the frame's focusables (tab order); -1 = nothing
	// focused. Stable across frames as long as the tree shape is.
	focusIdx int

	// focusVisible is true while the current focus was acquired via the
	// keyboard (CSS :focus-visible); pointer focus clears it.
	focusVisible bool

	// Drag-and-drop state. pendDrag holds a pressed-but-not-yet-moved
	// source; once the pointer moves past a threshold dragging begins and
	// dragGhost (a snapshot of the source's child) follows the cursor.
	// gestures: the last click and a held press (pointer.go).
	gestures gestureState
	// touch folds touchscreen events; pinching is the region a touch
	// pinch is driving (pinch.go).
	touch        app.Gestures
	pinching     *DivEl
	pendDrag     *DraggableEl
	dragging     bool
	dragPayload  any
	dragGhost    Element
	dragX, dragY float32

	// selInput is the text field being selected with a mouse drag.
	selInput *InputEl
	// dragBar is the scrollbar thumb being dragged.
	dragBar scrollTarget
}

// New wires a view function to a renderer and default font.
func New(r *render.Renderer2D, font *render.Font, view func() Element) *UI {
	// The pointer starts NOWHERE, not at the origin: (0, 0) is a real point
	// on screen, and a control laid out over it on its first frame would
	// latch a hover state it never loses (a headless render has no pointer
	// at all, and its flat buttons were coming out tinted).
	return &UI{ctx: Context{R: r, Font: font, MouseX: noPointer, MouseY: noPointer},
		view: view, focusIdx: -1}
}

// noPointer is "the pointer is not over this window" — far outside any
// element's rect, so every contains() test is false.
const noPointer = -1e9

// Frame rebuilds, lays out and paints the tree for a target of the given
// pixel size (logical px — fractional under DPI scaling: a 751-physical-px
// window at 2x is 375.5 logical, and truncating it makes centered/space-
// between gaps wobble by a pixel during resizes). Call between
// Renderer2D.Begin and Flush. Property bindings are flushed first so the
// view renders a consistent world.
func (u *UI) Frame(width, height float32) {
	// A frame is ONE build and one layout. The shell lays out first (the
	// offscreen layers need laid-out subtrees before the main paint) and
	// then calls Frame, which used to build and lay out all over again —
	// every frame paid for the whole tree twice, and an animated .gml page
	// spent half its build time on that second pass. Frame rebuilds only
	// when nothing laid this tree out yet, or the window changed size.
	if !u.laidOut || u.ctx.winW != width || u.ctx.winH != height {
		u.build(width, height)
		if u.dmg != nil {
			u.dmg.prepared = false // the tree the diff saw is gone
		}
	}
	u.laidOut = false
	// Damage-driven painting (see damage.go): diff the tree if the caller
	// did not, then paint with culling on for the main tree only —
	// overlays and the focus ring below force a full frame anyway.
	if u.dmg != nil && u.dmg.on {
		if !u.dmg.prepared {
			u.PrepareDamage()
		}
		u.ctx.dmg = u.dmg
	}
	u.root.paint(&u.ctx)
	u.ctx.dmg = nil
	if u.dmg != nil {
		u.dmg.prepared = false
	}
	// Overlays (tooltips, dropdowns, menus) paint on top of everything,
	// in registration order, free of any clip from the main tree.
	for _, o := range u.ctx.overlays {
		o.paint(&u.ctx)
	}
	// Focus ring on top of all — unless the element styles its own focus
	// (inputs draw their accent; FocusBorder divs paint their border).
	if el := u.focusedEl(); el != nil {
		_, isInput := el.(*InputEl)
		custom, hasCustom := el.(interface{ hasFocusStyle() bool })
		if !isInput && !(hasCustom && custom.hasFocusStyle()) {
			b := el.bounds()
			// Two-tone ring (light halo + blue) so focus reads on any
			// background — a single blue ring vanishes on blue-ish elements.
			u.ctx.R.Quad(b.X-4, b.Y-4, b.W+8, b.H+8, RGBA{}, 9, 2, RGBA{R: 1, G: 1, B: 1, A: 0.9})
			u.ctx.R.Quad(b.X-2, b.Y-2, b.W+4, b.H+4, RGBA{}, 8, 2, RGB(0x4c8dff))
		}
	}
	// Drag ghost: a translucent snapshot of the source following the cursor.
	if u.dragging && u.dragGhost != nil {
		gw, gh := u.dragGhost.measure(&u.ctx)
		gx, gy := u.dragX-gw/2, u.dragY-gh/2
		u.dragGhost.layout(&u.ctx, Rect{gx, gy, gw, gh})
		prev, prevInit := u.ctx.opacity, u.ctx.opacityInit
		u.ctx.opacity, u.ctx.opacityInit = 0.7, true
		u.dragGhost.paint(&u.ctx)
		u.ctx.opacity, u.ctx.opacityInit = prev, prevInit
	}
}

// Layout rebuilds and lays out the tree without painting — for tests and
// tools that need hit-testing or geometry but have no GPU.
func (u *UI) Layout(width, height float32) { u.build(width, height) }

func (u *UI) build(width, height float32) {
	core.FlushBindings()
	u.ctx.wantsFrame = false
	u.ctx.scrolls = u.ctx.scrolls[:0]
	u.ctx.overlays = u.ctx.overlays[:0]
	u.ctx.focusables = u.ctx.focusables[:0]
	u.ctx.shortcuts = u.ctx.shortcuts[:0]
	u.ctx.focusMetas = u.ctx.focusMetas[:0]
	u.ctx.dropZones = u.ctx.dropZones[:0]
	u.ctx.layers = u.ctx.layers[:0]
	u.ctx.swipes = u.ctx.swipes[:0]
	u.ctx.drags = u.ctx.drags[:0]
	u.ctx.pinches = u.ctx.pinches[:0]
	u.ctx.opacity, u.ctx.opacityInit = 1, false
	u.ctx.winW, u.ctx.winH = width, height
	u.ctx.frame++
	u.root = u.view()
	u.root.layout(&u.ctx, Rect{0, 0, width, height})
	u.laidOut = true
	// Resolve focus against this frame's focusables: clamp a stale index
	// and mirror an input focus into ctx.focus so it paints its caret.
	if u.focusIdx >= len(u.ctx.focusables) {
		u.focusIdx = -1
	}
	// Honor an element's initial focus request (QML focus: true) once, when
	// nothing is focused yet.
	if u.focusIdx < 0 {
		for i, f := range u.ctx.focusables {
			if wf, ok := f.(interface{ wantsInitialFocus() bool }); ok && wf.wantsInitialFocus() {
				u.focusIdx = i
				break
			}
		}
	}
	u.resolveFocusRequests() // RequestFocus, OnFocusChange (focus.go)
	u.ctx.setFocus(nil)
	if el := u.focusedEl(); el != nil {
		if in, ok := el.(*InputEl); ok {
			u.ctx.setFocus(in.state)
		}
	}
	// Mirror focus state into the paint context so elements with custom
	// focus styling (FocusBorder/FocusVisibleBorder) can paint it.
	u.ctx.curFocus = u.focusIdx
	u.ctx.focusVisible = u.focusVisible
}

// tabOrder returns indices into ctx.focusables in Tab-traversal order:
// ascending tab_index (GPUI/HTML semantics; elements without one rank 0),
// ties in layout order.
// dragToPoint sets the dragged slider's value from a pointer position,
// keeping a range slider's handles from crossing.
func (u *UI) dragToPoint(x, y float32) {
	t := valueAt(u.dragRect, x)
	if u.dragVert {
		t = 1 - valueAtY(u.dragRect, y)
	}
	v := u.dragFrom + t*(u.dragTo-u.dragFrom)
	// A snapping slider only ever holds a step boundary: QML's
	// `snapMode: Slider.SnapAlways` with `stepSize: 10` reports 20, never
	// 19, however carefully the handle is dragged.
	if u.dragSnap && u.dragStep > 0 {
		v = snapTo(v, u.dragFrom, u.dragStep)
		v = min(max(v, min(u.dragFrom, u.dragTo)), max(u.dragFrom, u.dragTo))
	}
	if u.dragLo != nil {
		v = max(v, u.dragLo.Get())
	}
	if u.dragHi != nil {
		v = min(v, u.dragHi.Get())
	}
	u.dragSlider.Set(v)
	if u.dragMoved != nil {
		u.dragMoved()
	}
}

func (u *UI) tabOrder() []int {
	order := make([]int, len(u.ctx.focusables))
	for i := range order {
		order[i] = i
	}
	rank := func(i int) int {
		if t, ok := u.ctx.focusables[i].(interface{ tabIndex() int }); ok {
			return t.tabIndex()
		}
		return 0
	}
	slices.SortStableFunc(order, func(a, b int) int { return rank(a) - rank(b) })
	return order
}

// focusedEl returns the element holding focus this frame, or nil.
func (u *UI) focusedEl() Element {
	if u.focusIdx >= 0 && u.focusIdx < len(u.ctx.focusables) {
		return u.ctx.focusables[u.focusIdx]
	}
	return nil
}

// focusElement sets focus to el if it's one of this frame's focusables.
func (u *UI) focusElement(el Element) {
	for i, f := range u.ctx.focusables {
		if f == el {
			u.focusIdx = i
			return
		}
	}
}

// hitTest finds the topmost element at (x, y): overlays (last registered
// first) before the main tree.
func (u *UI) hitTest(x, y float32) Element {
	for i := len(u.ctx.overlays) - 1; i >= 0; i-- {
		if el := u.ctx.overlays[i].hit(x, y); el != nil {
			return el
		}
		// A floating panel takes the press over its empty parts too: a
		// click between a dialog's widgets must not reach the scrim under
		// it (and dismiss it) or the page below.
		if d, ok := u.ctx.overlays[i].(*DivEl); ok && d.rect.contains(x, y) {
			return d
		}
	}
	if u.root != nil {
		return u.root.hit(x, y)
	}
	return nil
}

// Hit returns the topmost handler under pixel (x, y) in the last laid-out
// tree (overlays first), or nil — what a click there would reach. Hosts
// use it to tell chrome from the viewport before starting a camera drag.
func (u *UI) Hit(x, y float32) Element {
	if u.root == nil {
		return nil
	}
	return u.hitTest(x, y)
}

// Click delivers a full press-and-release at pixel (x, y) to the topmost
// handler in the last laid-out tree. It reports whether anything consumed
// the click. (Synthetic input; real streams go through Mouse.)
func (u *UI) Click(x, y float32) bool {
	if u.root == nil {
		return false
	}
	if el := u.hitTest(x, y); el != nil {
		if fn := el.onClick(); fn != nil {
			fn()
			return true
		}
	}
	return false
}

// Drag drives a press-drag-release over the tree — for tests and tools
// with no real pointer. It reports whether a drag region took the press.
func (u *UI) Drag(x0, y0, x1, y1 float32) bool {
	var region dragRegion
	for i := len(u.ctx.drags) - 1; i >= 0; i-- {
		if u.ctx.drags[i].contains(x0, y0) {
			region = u.ctx.drags[i]
			break
		}
	}
	if region == nil {
		return false
	}
	region.drag(0, 0, DragStart)
	region.drag(x1-x0, y1-y0, DragMove)
	region.drag(x1-x0, y1-y0, DragEnd)
	return true
}

// Fling drives a drag that is still moving when the pointer lets go, at
// (vx, vy) pixels per second — what MouseUp does with the velocity it
// tracked. Reports whether a region took it. For tests.
func (u *UI) Fling(x0, y0, x1, y1, vx, vy float32) bool {
	if !u.Drag(x0, y0, x1, y1) {
		return false
	}
	for i := len(u.ctx.drags) - 1; i >= 0; i-- {
		if f, ok := u.ctx.drags[i].(flingRegion); ok && u.ctx.drags[i].contains(x0, y0) {
			f.fling(vx, vy)
			return true
		}
	}
	return false
}

// SetLineHeight sets the default text line height as a multiple of font
// size (CSS line-height; gpui defaults to phi = 1.618). 0 (the default)
// keeps text boxes at the face's natural height. Individual TextEl/
// Paragraph LineHeight calls override it.
func (u *UI) SetLineHeight(rel float64) { u.ctx.defLineH = float32(rel) }

// SetMSDFText selects the distance-field text path for the whole UI —
// QML's `Text.renderType: Text.QtRendering` as a default rather than a
// per-item property (TextEl.DistanceField overrides it for one run).
//
// It is off by default, and deliberately. The two paths draw the same
// text at the same weight, and which is better depends on the text: a
// hinted coverage mask still has the edge on a static page of 11 px
// body copy, while a field is the only one of the two that survives
// rotation, a size that slides, or a synthetic weight — one sheet
// serves every size, so nothing is rasterised when the size changes.
// Turn it on for the UI when the app has text like that in it.
//
// Reports whether it took: a device without the pipeline stays on the
// raster path.
func (u *UI) SetMSDFText(on bool) bool {
	u.ctx.R.SetMSDFText(on)
	return u.ctx.R.MSDFTextEnabled()
}

// MSDFText reports whether distance-field text is the UI's default path.
func (u *UI) MSDFText() bool { return u.ctx.R.MSDFTextEnabled() }

// SetTextEffect sets the distance-field text effects for runs drawn
// after it — weight and outline in ems, so one setting holds at every
// size (render.Renderer2D.SetTextWeight explains why ems and not
// pixels). They do nothing on the raster path.
func (u *UI) SetTextEffect(weightEm, outlineEm float32, outline RGBA, glowEm float32) {
	u.ctx.R.SetTextWeight(weightEm)
	u.ctx.R.SetTextOutline(outlineEm, outline)
	u.ctx.R.SetTextSoftness(glowEm)
}

// SetAnimTime sets the timestamp time-driven content (GIF frames) renders
// at this frame. Freeze it (keep passing the same time) to pause animations
// while still repainting; zero means "use the wall clock".
func (u *UI) SetAnimTime(t time.Time) { u.ctx.animT = t }

// FrameCounts reports and clears the measure/layout call counts (GFX_FPS).
func (u *UI) FrameCounts() (measure, layout int) {
	m, l := u.ctx.MeasureCalls, u.ctx.LayoutCalls
	u.ctx.MeasureCalls, u.ctx.LayoutCalls = 0, 0
	return m, l
}

// WantsFrame reports whether the last painted frame contained time-driven
// content (an animated GIF) that needs the render loop to keep ticking even
// with no input, animations or property changes.
func (u *UI) WantsFrame() bool { return u.ctx.wantsFrame || u.gestures.holding(u) }

// Mouse feeds a frame's platform mouse events through the UI: hover
// position for styling, press tracking, and click-on-release semantics —
// a click fires only if the press and the release hit a handler with the
// same bounds (element identity across per-frame rebuilds is matched by
// rect, since elements themselves are rebuilt).
func (u *UI) Mouse(events []app.MouseEvent) {
	if len(events) > 0 {
		// Hover and press are read off the pointer at paint time, so a
		// frame with input in it repaints everything (see damage.go).
		u.ForceFullRedraw()
	}
	for _, m := range events {
		u.ctx.MouseX, u.ctx.MouseY = m.X, m.Y
		switch m.Kind {
		case app.MouseScroll:
			// A touchpad pinch arrives as Ctrl+wheel: a pinch region under
			// the pointer takes it (pinch.go) before any scrolling.
			if u.wheelPinch(m) {
				continue
			}
			// Deepest scroll region under the pointer wins (layout order
			// means parents come before children); a delta the region can't
			// consume (already at its limit, or no overflow on that axis)
			// bubbles out to the enclosing scroll region, per axis.
			dy, dx := m.ScrollY, m.ScrollX
			for i := len(u.ctx.scrolls) - 1; i >= 0 && (dy != 0 || dx != 0); i-- {
				s := u.ctx.scrolls[i]
				if !s.contains(m.X, m.Y) {
					continue
				}
				if dy != 0 && s.scroll(dy) {
					dy = 0
				}
				if dx != 0 {
					if h, ok := s.(interface{ scrollX(float32) bool }); ok && h.scrollX(dx) {
						dx = 0
					}
				}
			}
		case app.MouseMove:
			if u.dragSlider != nil {
				u.dragToPoint(m.X, m.Y)
			}
			if u.dragDial != nil {
				u.dragDial.turnTo(m.X, m.Y)
			}
			if u.dragRegion != nil && u.ctx.MousePressed {
				dx, dy := m.X-u.ctx.pressX, m.Y-u.ctx.pressY
				u.dragTravel = max(u.dragTravel, max(abs32(dx), abs32(dy)))
				u.trackVelocity(m.X, m.Y)
				u.dragRegion.drag(dx, dy, DragMove)
			}
			if u.selInput != nil {
				u.selInput.state.caret = u.selInput.caretAt(&u.ctx, m.X, m.Y) // extend selection
			}
			if u.dragBar != nil {
				u.dragBar.scrollToY(m.Y)
			}
			// Begin a drag once the pointer leaves a small threshold.
			if u.pendDrag != nil && !u.dragging {
				dx, dy := m.X-u.dragX, m.Y-u.dragY
				if dx*dx+dy*dy > 16 {
					u.dragging = true
					u.dragGhost = u.pendDrag.child
					u.dragPayload = u.pendDrag.payload
				}
			}
			if u.dragging {
				u.dragX, u.dragY = m.X, m.Y
			}
		case app.MouseDown:
			if m.Button != 1 {
				continue
			}
			u.ctx.MousePressed = true
			u.ctx.pressX, u.ctx.pressY = m.X, m.Y
			u.dragX, u.dragY = m.X, m.Y
			u.pressTarget = Rect{}
			// A press on a scrollbar thumb starts a bar drag and consumes
			// the event (no focus/click/select).
			for i := len(u.ctx.scrolls) - 1; i >= 0; i-- {
				if u.ctx.scrolls[i].thumbRect().contains(m.X, m.Y) {
					u.dragBar = u.ctx.scrolls[i]
					break
				}
			}
			if u.dragBar != nil {
				u.dragBar.scrollToY(m.Y)
				continue
			}
			// A press inside a drag region starts following the pointer.
			// The click still runs on release unless the pointer travelled.
			u.dragRegion, u.dragTravel = nil, 0
			u.velX, u.velY = 0, 0
			u.velT, u.velLastX, u.velLastY = u.ctx.animT, m.X, m.Y
			// ... unless the control under the pointer takes the press for
			// itself. A slider's handle, a range slider, a text field's
			// caret drag: in QML those accept the press, so the Flickable
			// they sit in never sees it. Without this, dragging a slider on
			// a scrollable page moved the slider AND panned the page.
			if !u.pressClaimed(m.X, m.Y) {
				for i := len(u.ctx.drags) - 1; i >= 0; i-- {
					if u.ctx.drags[i].contains(m.X, m.Y) {
						u.dragRegion = u.ctx.drags[i]
						if p, ok := u.dragRegion.(interface{ pressAt(x, y float32) }); ok {
							p.pressAt(m.X, m.Y)
						}
						u.dragRegion.drag(0, 0, DragStart)
						break
					}
				}
			}
			if u.root != nil {
				el := u.hitTest(m.X, m.Y)
				if dr, ok := el.(*DraggableEl); ok {
					u.pendDrag = dr
				}
				u.gestures.pressed(el)
				if el != nil && el.onClick() != nil {
					u.pressTarget = el.bounds()
				}
				// Focus follows the press: focus a focusable element
				// (input, button, …), else blur. Update ctx.focus
				// immediately so caret/typing work before the next build.
				// Pointer-acquired focus is not "focus-visible".
				if el != nil && isFocusable(el) {
					u.focusElement(el)
					u.focusVisible = false
				} else {
					u.focusIdx = -1
				}
				if in, ok := el.(*InputEl); ok {
					u.ctx.setFocus(in.state)
					// Place the caret at the click and begin a selection
					// drag (anchor == caret collapses any prior selection).
					pos := in.caretAt(&u.ctx, m.X, m.Y)
					in.state.caret, in.state.anchor = pos, pos
					u.selInput = in
				} else {
					u.ctx.setFocus(nil)
				}
				// A range slider grabs the handle nearest the press.
				if rs, ok := el.(*RangeSliderEl); ok {
					p := rs.nearest(m.X, m.Y)
					u.dragSlider, u.dragRect = p, rs.rect
					u.dragFrom, u.dragTo = rs.from, rs.to
					u.dragStep, u.dragSnap = rs.step, rs.snap
					u.dragMoved, u.dragVert = rs.moved, rs.vertical
					u.dragLo, u.dragHi = nil, nil
					if p == rs.first {
						u.dragHi = rs.second // the lower handle stops below the upper
					} else {
						u.dragLo = rs.first
					}
					u.dragToPoint(m.X, m.Y)
				}
				// A dial turns to the pointer's angle on press, and keeps
				// following it while the press is held (Qt's Dial).
				if dl, ok := el.(*DialEl); ok {
					u.dragDial = dl
					dl.turnTo(m.X, m.Y)
				}
				// Sliders start dragging immediately on press.
				if sl, ok := el.(*SliderEl); ok {
					u.dragSlider, u.dragRect = sl.value, sl.rect
					u.dragFrom, u.dragTo = sl.from, sl.to
					u.dragStep, u.dragSnap = sl.step, sl.snap
					u.dragMoved = sl.moved
					u.dragVert = sl.vertical
					// The press itself moves the handle (QML's Slider jumps
					// to a press on its groove) — through the same path, so
					// it snaps like the drag that follows.
					u.dragToPoint(m.X, m.Y)
				}
			}
		case app.MouseUp:
			if m.Button != 1 {
				continue
			}
			u.ctx.MousePressed = false
			if u.dragRegion != nil {
				u.dragRegion.drag(m.X-u.ctx.pressX, m.Y-u.ctx.pressY, DragEnd)
				// Still moving on release: hand the velocity over, and the
				// region carries the motion on (kinetic flick).
				if f, ok := u.dragRegion.(flingRegion); ok && u.dragTravel > 4 {
					if abs32(u.velX) > flingFloor || abs32(u.velY) > flingFloor {
						f.fling(u.velX, u.velY)
					}
				}
				u.dragRegion = nil
				// A press that travelled is a drag, not a click: releasing
				// a spun wheel must not also pick the row under the finger.
				if u.dragTravel > 4 {
					u.pressTarget = Rect{}
				}
			}
			u.dragSlider, u.dragMoved, u.dragDial = nil, nil, nil
			u.dragLo, u.dragHi, u.dragVert = nil, nil, false
			u.selInput = nil
			u.dragBar = nil
			// Complete a drag: drop on the topmost accepting zone.
			if u.dragging {
				for i := len(u.ctx.dropZones) - 1; i >= 0; i-- {
					z := u.ctx.dropZones[i]
					if z.rect.contains(m.X, m.Y) && (z.accept == nil || z.accept(u.dragPayload)) {
						if z.onDrop != nil {
							z.onDrop(u.dragPayload)
						}
						break
					}
				}
				u.dragging, u.pendDrag, u.dragGhost, u.dragPayload = false, nil, nil, nil
				u.pressTarget = Rect{}
				continue
			}
			u.pendDrag = nil
			// Swipe: a mostly-horizontal press→release travel over a swipe
			// region flips its page and consumes the release (no click).
			if dx := m.X - u.ctx.pressX; abs32(dx) >= 48 && abs32(dx) > 2*abs32(m.Y-u.ctx.pressY) {
				swiped := false
				for i := len(u.ctx.swipes) - 1; i >= 0; i-- {
					s := u.ctx.swipes[i]
					if s.contains(u.ctx.pressX, u.ctx.pressY) {
						dir := 1
						if dx < 0 {
							dir = -1
						}
						s.swipe(dir)
						swiped = true
						break
					}
				}
				if swiped {
					u.pressTarget = Rect{}
					continue
				}
			}
			if u.root != nil && u.pressTarget.W > 0 {
				if el := u.hitTest(m.X, m.Y); el != nil && el.onClick() != nil && el.bounds() == u.pressTarget {
					el.onClick()()
					u.gestures.clicked(el)
				}
			}
			u.pressTarget = Rect{}
			u.gestures.holdEl = nil
		}
	}
	// A press held long enough becomes a press-and-hold — checked every
	// frame, events or not (WantsFrame keeps frames coming meanwhile) —
	// and the release that ends it is then not a click.
	if u.gestures.tick(u, u.ctx.MouseX, u.ctx.MouseY) {
		u.pressTarget = Rect{}
		u.ForceFullRedraw()
	}
}

// pressClaimed reports whether the element under (x, y) takes a press for
// its own dragging — a slider handle, a range slider, a text field's
// selection. Buttons and plain content do NOT claim it: a list of buttons
// still flicks, which is Qt's behaviour too (a Flickable steals the press
// from a button once the drag passes its threshold, but never from a
// slider, which keeps the grab on its own axis).
func (u *UI) pressClaimed(x, y float32) bool {
	if u.root == nil {
		return false
	}
	switch u.hitTest(x, y).(type) {
	case *SliderEl, *RangeSliderEl, *InputEl, *DialEl:
		return true
	}
	return false
}

// flingFloor is the speed below which a release is a stop, not a flick
// (pixels per second) — letting go of a slow drag must not drift.
const flingFloor = 120

// trackVelocity samples the pointer once per frame and keeps a smoothed
// speed in pixels per second: the last sample dominates, so a flick that
// ends fast is read as fast even after a slow drag.
func (u *UI) trackVelocity(x, y float32) {
	now := u.ctx.animT
	dt := float32(now.Sub(u.velT).Seconds())
	if u.velT.IsZero() || dt <= 0 {
		return // same frame (or no clock): the next one measures it
	}
	if dt > 0.1 {
		dt = 0.1 // a stalled frame must not read as a slow drag
	}
	vx, vy := (x-u.velLastX)/dt, (y-u.velLastY)/dt
	u.velX, u.velY = 0.7*vx+0.3*u.velX, 0.7*vy+0.3*u.velY
	u.velT, u.velLastX, u.velLastY = now, x, y
}

// abs32 is math.Abs for float32 (gesture thresholds).
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// Key handles keyboard focus navigation and routes other keys to the
// focused element: Tab moves focus to the next focusable (wrapping),
// Enter/Space activates a focused button's OnClick, and printable/edit
// keys go to a focused text field. Reports whether the event was consumed.
func (u *UI) Key(ev app.KeyEvent) bool {
	u.ForceFullRedraw() // focus, caret and text can all move
	// KeyNavigation first (focus.go): it overrides the Tab chain and the
	// focused element's own handling of the keys it names.
	if u.keyNav(ev) {
		return true
	}
	if ev.Down && ev.Key == app.KeyTab {
		if order := u.tabOrder(); len(order) > 0 {
			pos := slices.Index(order, u.focusIdx) // -1 (nothing focused) starts at 0
			n := len(order)
			if ev.Shift {
				pos = (pos - 1 + n) % n
			} else {
				pos = (pos + 1) % n
			}
			u.focusIdx = order[pos]
			// Keyboard-acquired focus is "focus-visible" (CSS
			// :focus-visible): elements may style it distinctly.
			u.focusVisible = true
			if in, ok := u.focusedEl().(*InputEl); ok {
				u.ctx.setFocus(in.state)
			} else {
				u.ctx.setFocus(nil)
			}
		}
		return true
	}
	// Escape closes the topmost dismissable overlay — a dropdown, a menu, a
	// modal. Qt's popups all take Escape, and a user who cannot dismiss an
	// open dropdown with it is stuck with a panel over the page.
	if ev.Down && ev.Key == app.KeyEscape {
		for i := len(u.ctx.overlays) - 1; i >= 0; i-- {
			if s, ok := u.ctx.overlays[i].(*scrimEl); ok && s.dismiss != nil {
				s.dismiss()
				return true
			}
		}
	}
	if ev.Down && len(u.ctx.shortcuts) > 0 {
		_, typing := u.focusedEl().(*InputEl)
		if !typing || ev.Ctrl || ev.Alt || ev.Rune == 0 && ev.Key >= app.KeyF1 && ev.Key <= app.KeyF12 {
			for _, fn := range u.ctx.shortcuts {
				if fn(ev) {
					return true
				}
			}
		}
	}
	el := u.focusedEl()
	if el == nil {
		return false
	}
	if in, ok := el.(*InputEl); ok {
		in.state.Key(ev)
		return true
	}
	// A raw key handler (QML Keys.onPressed/onReleased) gets every event.
	if kh, ok := el.(interface{ handleKey(app.KeyEvent) bool }); ok {
		if kh.handleKey(ev) {
			return true
		}
	}
	// Enter or Space activates a focused button.
	if ev.Down && (ev.Key == app.KeyEnter || ev.Rune == ' ') {
		if fn := el.onClick(); fn != nil {
			fn()
		}
		return true
	}
	return false
}

// Focus programmatically focuses a text field (nil to blur).
func (u *UI) Focus(s *InputState) {
	u.focusIdx = -1
	for i, f := range u.ctx.focusables {
		if in, ok := f.(*InputEl); ok && in.state == s {
			u.focusIdx = i
		}
	}
	u.ctx.focus = s
}

// isFocusable reports whether el opts into keyboard focus.
func isFocusable(el Element) bool {
	f, ok := el.(interface{ isFocusable() bool })
	return ok && f.isFocusable()
}

// Cursor returns the mouse-pointer shape for the element currently under
// the pointer (default if none declares one). Drive the window cursor with
// it each frame.
func (u *UI) Cursor() app.Cursor {
	if el := u.hitTest(u.ctx.MouseX, u.ctx.MouseY); el != nil {
		if c, ok := el.(interface{ cursorShape() app.Cursor }); ok {
			return c.cursorShape()
		}
	}
	return app.CursorDefault
}
