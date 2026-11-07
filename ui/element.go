package ui

import (
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/render"
)

// Element is one node of the per-frame UI tree. Elements are cheap,
// rebuilt every frame by a view's render function, and hold no state of
// their own — state belongs in core.Property values the render function
// reads (the GPUI hybrid, see package docs).
type Element interface {
	// measure returns the element's intrinsic size given the context.
	measure(ctx *Context) (w, h float32)
	// layout assigns the element's final rectangle and lays out children.
	layout(ctx *Context, r Rect)
	// paint queues draw commands; called after layout.
	paint(ctx *Context)
	// hit returns the deepest element at (x,y) wanting the event, or nil.
	hit(x, y float32) Element
	// onClick reports the element's click handler (nil if none).
	onClick() func()
	// bounds reports the rect assigned by the last layout. Used to match
	// press/release targets across per-frame tree rebuilds.
	bounds() Rect
}

// Context carries the paint dependencies and pointer state through the
// tree. Mouse state lets elements restyle on hover/press without retained
// per-element state.
type Context struct {
	R    *render.Renderer2D
	Font *render.Font // default font for Text

	// dmg is set while the main tree paints with damage tracking on (see
	// damage.go); dmgLock > 0 disables culling inside a transformed
	// subtree, whose layout rects do not say where its pixels land.
	dmg     *damageState
	dmgLock int
	// Per-frame counts for GFX_FPS: how often measure and layout ran.
	MeasureCalls, LayoutCalls int
	// frame numbers the per-element measure memo (see DivEl.measure): a
	// tree is measured many times over on its way to a layout — a parent
	// measures its children, then measures them again as it lays them
	// out, a grid sizes its tracks, a row shares its width — and within
	// one frame, at one offered width, the answer is the same each time.
	frame uint64

	MouseX, MouseY float32
	MousePressed   bool
	pressX, pressY float32     // where the current press started
	focus          *InputState // focused text field, if any (set via setFocus)
	winW, winH     float32     // window size this frame (for centered/modal overlays)

	// opacity is the cumulative alpha multiplier of the current subtree
	// (Div.Opacity pushes onto it during paint). 0 means uninitialized →
	// treated as 1.
	opacity float32
	// opacityInit distinguishes "no opacity pushed yet" from a pushed 0,
	// which is a real value (a faded-out overlay paints nothing).
	opacityInit bool

	// availW is the width a container offers its children while it measures
	// them — what auto-width wrapped text needs to know how many lines it
	// takes (CSS height-for-width). 0 = unknown, so text measures as one
	// line, exactly as before.
	availW float32

	// defLineH is the UI-wide default text line height as a multiple of
	// font size (CSS line-height, gpui's phi() default); 0 keeps each
	// text's box at the face's natural height. Per-element LineHeight wins.
	defLineH float32

	// curFocus mirrors the UI's focused-element index (into focusables)
	// during paint; focusVisible is true when that focus came from the
	// keyboard (CSS :focus-visible). Elements with custom focus styling
	// compare their registration slot against curFocus.
	curFocus     int
	focusVisible bool

	// animT is the timestamp time-driven paints (GIF frames) sample. The
	// shell freezes it while the window is unfocused, so hover-driven
	// repaints don't advance animations that should be paused.
	animT time.Time

	// wantsFrame is set during paint by elements whose content advances
	// with time regardless of input or property changes (animated GIFs);
	// the shell keeps rendering while any painted element wants frames.
	wantsFrame bool

	// scrolls collects the frame's scrollable regions in layout order so
	// wheel events can find the deepest one under the pointer.
	scrolls []scrollTarget

	// overlays collects deferred, z-on-top draws (tooltips, dropdowns,
	// menus) painted and hit-tested above the main tree.
	overlays []Element

	// focusables collects keyboard-focusable elements in layout (tab)
	// order; the UI tracks which one holds focus by index.
	focusables []Element

	// shortcuts collects the frame's window-wide key shortcuts (OnShortcut)
	// in layout order; UI.Key offers a key press to them before the
	// focused element.
	shortcuts []func(app.KeyEvent) bool

	// focusMetas collects the frame's elements with focus metadata
	// (FocusID, RequestFocus, OnFocusChange), with the run of focusables
	// each one spans (focus.go).
	focusMetas []Element

	// dropZones collects drag-and-drop targets registered this frame.
	dropZones []*DropZoneEl

	// layers collects the frame's offscreen-layer items in layout order;
	// each renders into its own target before the main pass (see layer.go).
	layers []*LayerEl

	// swipes collects the frame's swipe-gesture regions (SwipeView pages)
	// in layout order; a horizontal press-drag-release picks the deepest.
	swipes []swipeRegion

	// drags collects the frame's drag regions (a tumbler's wheel, a custom
	// scrubber) in layout order; a press picks the deepest one under it and
	// follows the pointer until release.
	drags []dragRegion

	// pinches collects the frame's pinch regions (OnPinch) in layout
	// order; a pinch goes to the deepest one under its centre.
	pinches []*DivEl
}

// swipeRegion is anything that consumes a horizontal swipe within a rect.
type swipeRegion interface {
	contains(x, y float32) bool
	swipe(dir int)
}

// DragPhase says where in a press-drag-release a callback is being called.
type DragPhase int

const (
	DragStart DragPhase = iota // the press, before any movement
	DragMove                   // the pointer moved (dx, dy are since the press)
	DragEnd                    // the release
)

// dragRegion is anything that follows the pointer between press and release.
type dragRegion interface {
	contains(x, y float32) bool
	drag(dx, dy float32, phase DragPhase)
}

// flingRegion is a drag region that also wants the release VELOCITY, in
// pixels per second — what a kinetic flick is made of. Regions that only
// track the pointer do not implement it.
type flingRegion interface {
	fling(vx, vy float32)
}

// isOutOfFlow reports whether an element self-positions outside normal flow
// (overlays, modals): flow layout gives it no space and no gap slot, only a
// layout call after its in-flow siblings (CSS position:absolute semantics).
func isOutOfFlow(e Element) bool {
	oof, ok := e.(interface{ outOfFlow() bool })
	return ok && oof.outOfFlow()
}

// scrollTarget is anything that consumes wheel scrolling within a rect —
// implemented by both ScrollEl and the virtualized ListEl.
type scrollTarget interface {
	contains(x, y float32) bool
	// scroll moves by dy and reports whether the offset changed; a false
	// return lets the wheel event bubble to an enclosing scroll region.
	scroll(dy float32) bool
	// thumbRect is the draggable scrollbar thumb (zero W if no overflow).
	thumbRect() Rect
	// scrollToY drags the scroll so the thumb tracks pixel y in the track.
	scrollToY(y float32)
	// viewRect is the scroll viewport, for mapping a thumb drag.
	viewRect() Rect
}

// Rect is a pixel rectangle, (X,Y) top-left.
type Rect struct {
	X, Y, W, H float32
}

// Bounds reports the rectangle an element was assigned by the last layout.
// Exposed for tooling and bindings that read element geometry across frames.
func Bounds(e Element) Rect { return e.bounds() }

// Implicit is an element's natural (content) size as of the last measure —
// QML's implicitWidth/implicitHeight. It differs from Bounds for anything
// stretched or squeezed by its parent, which is exactly when markup asks
// for it (`Layout.preferredHeight: col.implicitHeight + 20`). Falls back to
// the laid-out size for elements that do not record one.
func Implicit(e Element) (float32, float32) {
	if n, ok := e.(interface{ natural() (float32, float32) }); ok {
		if w, h := n.natural(); w > 0 || h > 0 {
			return w, h
		}
	}
	r := e.bounds()
	return r.W, r.H
}

// FixedSize reports the size an element states for itself (0 on an axis it
// leaves to its parent). Unlike Implicit it needs no layout pass, so a
// builder can read what a subtree asked for while assembling it.
func FixedSize(e Element) (float32, float32) {
	if f, ok := e.(interface{ fixedWH() (float32, float32) }); ok {
		return f.fixedWH()
	}
	return 0, 0
}

// Children reports an element's child elements (nil for a leaf). For
// tooling and tests that need to walk a built tree; the tree itself is
// rebuilt every frame, so nothing should retain what this returns.
func Children(e Element) []Element {
	if c, ok := e.(interface{ childElems() []Element }); ok {
		return c.childElems()
	}
	return nil
}

// TextOf reports the string a text element renders, and whether the element
// is one at all. For tooling and for tests that assert what a built tree
// SAYS without rendering it — the counterpart of FillColor.
func TextOf(e Element) (string, bool) {
	if t, ok := e.(interface{ textContent() string }); ok {
		return t.textContent(), true
	}
	return "", false
}

// FillColor reports an element's own background colour and whether it has
// one. For tooling and for tests that assert a widget's palette without
// rendering it.
func FillColor(e Element) (RGBA, bool) {
	if f, ok := e.(interface{ fillColor() (RGBA, bool) }); ok {
		return f.fillColor()
	}
	return RGBA{}, false
}

func (r Rect) contains(x, y float32) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// color applies the current subtree opacity to a color's alpha. The
// multiplier is 1 at the root and only ever set explicitly, so a fully
// transparent subtree (opacity: 0) really does paint nothing.
func (c *Context) color(col RGBA) RGBA {
	col.A *= c.alpha()
	return col
}

// alpha returns the active opacity multiplier (1 before any is pushed).
func (c *Context) alpha() float32 {
	if !c.opacityInit {
		return 1
	}
	return c.opacity
}

// RGBA re-exports the render color type for styling.
type RGBA = render.RGBA

// RGB builds an opaque color from 0xRRGGBB.
func RGB(hex uint32) RGBA {
	return RGBA{
		R: float32(hex>>16&0xff) / 255,
		G: float32(hex>>8&0xff) / 255,
		B: float32(hex&0xff) / 255,
		A: 1,
	}
}

// Align positions children on an axis.
type Align int

const (
	AlignStart Align = iota
	AlignCenter
	AlignEnd
)

// setFocus points the paint context at the focused text field (nil to
// blur) and mirrors the flag onto the state objects, so a binding can read
// activeFocus without reaching into the UI.
func (c *Context) setFocus(s *InputState) {
	if c.focus == s {
		return
	}
	if c.focus != nil {
		c.focus.focused = false
	}
	c.focus = s
	if s != nil {
		s.focused = true
	}
}
