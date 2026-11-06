package ui

import (
	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/render"
	"math"
	"unsafe"
)

// DivEl is the container element: a flexbox-lite box with optional fill.
// Build it fluently:
//
//	ui.Div().Row().Gap(8).P(16).Bg(ui.RGB(0x202030)).Children(...)
type DivEl struct {
	row    bool
	w, h   float32 // 0 = auto
	grow   float32
	noPref bool // takes no implicit size from its content (QML Item)
	// minReqW/H is the size this box cannot do without: what its content
	// needs once everything that can shrink has (see requiredSize). Only a
	// Qt-style layout (SpreadCells) consults it.
	minReqW, minReqH float32
	selfAl           Align // this element's own cross-axis alignment (AlignSelf)
	hasSelfAl        bool
	spread           bool    // share leftover space across every child (SpreadCells)
	minW, minH       float32 // size floors (Layout.minimumWidth/Height)
	// xforms are the transforms this element (and its subtree) paints
	// through — QML's rotation / scale / `transform:` list, applied in
	// list order.
	xforms      []Transform
	shrink      float32
	wrap        bool // flex-wrap: a row that runs out of width starts a new line
	crossCenter bool // QML RowLayout: stretch only children that asked, centre the rest
	crossFill   bool // this child wants the whole cross axis (Layout.fillHeight)
	pad         float32
	padX, padY  float32 // asymmetric padding; used when hasPadXY
	hasPadXY    bool
	// Per-side padding (Padding): QML's leftPadding/rightPadding and a
	// layout item's Layout.*Margin, which need not be symmetric.
	padT, padR, padB, padL float32
	hasPadSides            bool
	gap                    float32
	bg                     RGBA
	hasBg                  bool
	hoverBg                RGBA
	hasHover               bool
	pressBg                RGBA
	hasPress               bool
	gradC0                 RGBA
	gradC1                 RGBA
	gradAng                float32
	gradS0                 float32 // ramp stop positions (0..1); equal = hard split
	gradS1                 float32
	gradRadial             bool
	hasGrad                bool
	gradRamp               *render.Ramp                          // multi-stop/colorspace ramp; nil = 2-stop fields
	gradStops              []render.GradientStop                 // more than four stops (drawn as bands)
	swipeFn                func(dir int)                         // horizontal swipe: -1 left, +1 right
	dragFn                 func(dx, dy float32, phase DragPhase) // press-drag-release
	pointerFn              func(x, y float32, phase DragPhase)   // the same, in window coordinates
	pressX, pressY         float32                               // where the press of the drag under way landed
	flingFn                func(vx, vy float32)                  // release velocity, px/s
	radius                 float32
	borderW                float32
	borderC                RGBA
	borderDashed           bool
	sideW                  [4]float32 // per-side border widths (top,right,bottom,left); QML border_*_1
	sideC                  [4]RGBA    // per-side border colours
	hasSide                bool
	shadows                []boxShadow // drop shadows, painted underneath in reverse order (CSS: first = topmost)
	opacity                float32     // subtree opacity; applies when hasOpacity
	hasOpacity             bool        // Opacity() was called: 0 means invisible, not unset
	focusable              bool
	focusW                 float32 // border shown while focused (GPUI .focus style)
	focusC                 RGBA
	focusVisW              float32 // border shown only for keyboard focus (.focus_visible)
	focusVisC              RGBA
	focusSlot              int                // 1-based index into ctx.focusables this frame; 0 = none
	tabIdx                 int                // Tab traversal rank (GPUI tab_index); ties resolve in layout order
	wantFocus              bool               // requests initial keyboard focus (QML focus: true)
	keyFn                  func(app.KeyEvent) // raw key handler (QML Keys.onPressed/onReleased)
	cursor                 app.Cursor
	justify                Align   // main axis
	items                  Align   // cross axis
	anchorEd               bool    // anchor-layout mode: children placed by their Anchors
	anchorFit              bool    // ... and sized from those children (Controls containers)
	natW, natH             float32 // last measured natural size (QML implicitWidth/Height)
	clip                   bool    // clip children to this box (CSS/GPUI overflow_hidden)
	click                  func()
	hoverFn                func(bool) // reports live hover state every paint (QML MouseArea.containsMouse / Control.hovered)
	pressFn                func(bool) // reports live press state every paint (QML MouseArea.pressed / Control.down)
	children               []Element

	rect Rect // assigned by layout
	// measure memo (see measure)
	mFrame uint64
	mAvail float32
	mW, mH float32
}

// Div starts a container; children stack vertically until Row is called.
func Div() *DivEl { return &DivEl{} }

func (d *DivEl) Row() *DivEl         { d.row = true; return d }
func (d *DivEl) Col() *DivEl         { d.row = false; return d }
func (d *DivEl) W(px float32) *DivEl { d.w = px; return d }
func (d *DivEl) H(px float32) *DivEl { d.h = px; return d }

// MinW / MinH set a floor on the element's size (CSS min-width/min-height,
// QtQuick.Layouts' Layout.minimumWidth/Height): the box never measures or
// lays out smaller, but it still stretches and grows, which a fixed W/H
// would prevent.
func (d *DivEl) MinW(px float32) *DivEl { d.minW = px; return d }
func (d *DivEl) MinH(px float32) *DivEl { d.minH = px; return d }

// minWH reports the element's size floors (0 = none).
func (d *DivEl) minWH() (float32, float32) { return d.minW, d.minH }

// Rotate paints the element and its subtree rotated by angle degrees about
// its own centre (QML's `rotation`). Layout is untouched.
func (d *DivEl) Rotate(deg float32) *DivEl {
	if deg != 0 {
		d.xforms = append(d.xforms, Transform{Kind: XformRotate, Angle: deg, Rel: true})
	}
	return d
}

// ScaleBy paints the element and its subtree scaled about its own centre
// (QML's `scale`).
func (d *DivEl) ScaleBy(s float32) *DivEl {
	if s != 1 && s != 0 {
		d.xforms = append(d.xforms, Transform{Kind: XformScale, SX: s, SY: s, Rel: true})
	}
	return d
}

// Transform appends one entry of a QML-style transform list.
func (d *DivEl) Transform(t Transform) *DivEl { d.xforms = append(d.xforms, t); return d }

// Transforms returns the element's transform list, in the order it is
// applied (first entry first, as QML's `transform:` list does).
func (d *DivEl) Transforms() []Transform  { return d.xforms }
func (d *DivEl) Size(w, h float32) *DivEl { d.w, d.h = w, h; return d }
func (d *DivEl) Grow() *DivEl             { d.grow = 1; return d }
func (d *DivEl) GrowBy(f float32) *DivEl  { d.grow = f; return d }

// SpreadCells shares leftover main-axis space across EVERY child in
// proportion to its size, centring each child in the share it gets —
// QtQuick.Layouts' distribution when no item asked to expand.
//
// A card written as a ColumnLayout of a heading and a couple of rows, sat
// in a taller cell, has its content spaced out that way in Qt; a flex box
// would leave it packed at the top with the slack at the bottom, and every
// row below the first lands too high. When some child DOES expand, that
// child takes the surplus instead and this has no effect, which is Qt's
// rule too.
//
// It applies on BOTH axes — MEASURED (Qt 6.10): two 126px items in a
// 360px RowLayout land at 0 and 193, the 83px surplus shared into their
// cells. A row of buttons in a card LOOKS packed instead, but that is the
// cap in layoutMax: a nested layout is only given its own content width,
// so it has no surplus to share.
//
// A card written as a ColumnLayout of a heading and a couple of rows, sat
// in a taller cell, has its content spaced out that way in Qt; a flex box
// would leave it packed at the top with the slack at the bottom, and every
// row below the first lands too high. When some child DOES expand, that
// child takes the surplus instead and this has no effect, which is Qt's
// rule too.
func (d *DivEl) SpreadCells() *DivEl { d.spread = true; return d }

// AlignSelf states this element's own alignment across its parent's axis —
// QML's `Layout.alignment: Qt.AlignHCenter` on an item of a column, CSS
// align-self. It also opts the element out of being stretched: an item
// cannot be both centred and full width.
func (d *DivEl) AlignSelf(a Align) *DivEl { d.selfAl, d.hasSelfAl = a, true; return d }

func (d *DivEl) selfAlign() (Align, bool) { return d.selfAl, d.hasSelfAl }

// NoImplicitSize marks a box that takes no size from its content — QML's
// Item and Rectangle, whose implicitWidth/implicitHeight are 0 unless the
// markup states them. Positioners and layouts (Row, ColumnLayout, ...) DO
// take their size from their children, so they must not be marked.
func (d *DivEl) NoImplicitSize() *DivEl { d.noPref = true; return d }

// layoutMaxer is an element that states how big it can USEFULLY get on
// each axis — 0 meaning "no limit". QtQuick.Layouts' Layout.maximumWidth/
// Height, which for a layout item defaults to its preferred size unless
// something inside it can grow: a RowLayout of fixed-height columns asked
// to fill its parent still stops at the height of its own content, and the
// space it does not take is shared out around it.
type layoutMaxer interface {
	layoutMax() (w, h float32)
}

// layoutMinner is an element that knows the size it cannot do without —
// implemented by DivEl for Qt-style layouts, whose minimum comes from its
// own children.
type layoutMinner interface {
	layoutMin() (w, h float32)
}

func (d *DivEl) layoutMin() (float32, float32) { return d.minReqW, d.minReqH }

// requiredSize is how much room a child cannot do without, given the size
// it measured and which way its parent runs. On an axis the child FILLS it
// can shrink — to its own minimum, or to a nested layout's — and on one it
// does not, it is stuck at what it measured.
func requiredSize(c Element, cw, ch float32, parentRow bool) (float32, float32) {
	fillsMain, fillsCross := false, false
	if gf, ok := c.(interface{ growFactor() float32 }); ok && gf.growFactor() != 0 {
		fillsMain = true
	}
	if cf, ok := c.(interface{ crossFills() bool }); ok && cf.crossFills() {
		fillsCross = true
	}
	fillsW, fillsH := fillsCross, fillsMain
	if parentRow {
		fillsW, fillsH = fillsMain, fillsCross
	}
	// An auto-sized CONTAINER stretches to whatever it is given, so it asks
	// for nothing of its own on that axis — only for what its content
	// cannot do without. A wrapping label is auto-width for the same
	// reason: its long measured line is not a minimum, it can break it. A
	// leaf that does NOT wrap (a one-line label, an image) is stuck at the
	// size it measured, which is exactly Qt's minimum for it.
	switch v := c.(type) {
	case *DivEl:
		if v.w == 0 {
			fillsW = true
		}
		if v.h == 0 {
			fillsH = true
		}
	case *Paragraph:
		if v.w == 0 {
			fillsW = true
		}
		fillsH = true // its height follows the width it is given
	case *GridEl, *ScrollEl:
		if fs, ok := c.(interface{ fixedWH() (float32, float32) }); ok {
			fw, fh := fs.fixedWH()
			if fw == 0 {
				fillsW = true
			}
			if fh == 0 {
				fillsH = true
			}
		}
	}
	var minW, minH float32
	if lm, ok := c.(layoutMinner); ok {
		minW, minH = lm.layoutMin()
	}
	if mf, ok := c.(interface{ minWH() (float32, float32) }); ok {
		mw, mh := mf.minWH()
		minW, minH = max(minW, mw), max(minH, mh)
	}
	w, h := cw, ch
	if fillsW {
		w = minW
	}
	if fillsH {
		h = minH
	}
	return w, h
}

// isQtLayout reports a box laid out by Qt's rules (a RowLayout or a
// ColumnLayout, which is what SpreadCells marks) rather than a plain flex
// container.
func isQtLayout(e Element) bool {
	d, ok := e.(*DivEl)
	return ok && d.spread
}

// layoutMax caps a Qt-style layout at its own content on an axis nothing
// inside it can grow along. A plain flex box states no limit, so nothing
// outside the gml engine changes behaviour.
func (d *DivEl) layoutMax() (float32, float32) {
	if !d.spread {
		return 0, 0
	}
	var grows, crossFills bool
	for _, c := range d.children {
		if isOutOfFlow(c) {
			continue
		}
		if gf, ok := c.(interface{ growFactor() float32 }); ok && gf.growFactor() != 0 {
			grows = true
		}
		if cf, ok := c.(interface{ crossFills() bool }); ok && cf.crossFills() {
			crossFills = true
		}
		// A nested Qt LAYOUT with no ceiling of its own removes this one's:
		// something inside it can use the space. Anything else — a plain
		// box, a control, a label — is bounded by what it measured, so it
		// leaves this layout's ceiling where it is.
		if lm, ok := c.(layoutMaxer); ok {
			mw, mh := lm.layoutMax()
			if bounded := (d.row && mh > 0) || (!d.row && mw > 0); bounded {
				continue
			}
			if _, isDiv := c.(*DivEl); isDiv && !isQtLayout(c) {
				continue
			}
			crossFills = true
		}
	}
	var mw, mh float32
	if !grows { // the main axis stops at the content
		if d.row {
			mw = d.natW
		} else {
			mh = d.natH
		}
	}
	if !crossFills { // and so does the cross axis
		if d.row {
			mh = d.natH
		} else {
			mw = d.natW
		}
	}
	return mw, mh
}

// selfAlignOf reads a child's own cross-axis alignment, if it states one
// (QML's Layout.alignment, CSS align-self). Stated alignment wins over the
// container's align-items AND over stretching, since an item cannot be both
// centred and full width.
func selfAlignOf(e Element) (Align, bool) {
	if sa, ok := e.(interface{ selfAlign() (Align, bool) }); ok {
		return sa.selfAlign()
	}
	return AlignStart, false
}

// have is the width a box has to give its children: its own when it
// states one, otherwise what its parent offered.
func have(ctx *Context, d *DivEl) float32 {
	px, _ := d.padWH()
	if d.w > 0 {
		return d.w - 2*px
	}
	return ctx.availW - 2*px
}

// heightForWidther is an element whose height depends on the width it is
// given — a wrapping paragraph. A row asks it again once the width is
// settled, since a first measure can only offer the whole line.
type heightForWidther interface {
	heightFor(ctx *Context, w float32) float32
}

// Shrink lets this element compress below its measured main-axis size when
// the parent runs out of room (CSS flex-shrink: 1; content should Clip or
// truncate). Opt-in — unlike CSS, elements are rigid by default here, since
// scrollable content relies on overflowing its viewport.
func (d *DivEl) Shrink() *DivEl { d.shrink = 1; return d }

// CrossCenter makes this container fill only the children that asked for
// the cross axis (CrossFill) and centre the rest at their own size. That is
// what a QML RowLayout does: an item with Layout.fillHeight fills the row,
// and a label beside it sits level with it rather than riding at the top of
// the tallest item.
func (d *DivEl) CrossCenter() *DivEl { d.crossCenter = true; return d }

// CrossFill marks this element as wanting its parent's whole cross axis
// (QML's Layout.fillHeight in a row). Only meaningful inside a CrossCenter
// container, which otherwise leaves children at their own cross size.
func (d *DivEl) CrossFill() *DivEl { d.crossFill = true; return d }

func (d *DivEl) crossFills() bool { return d.crossFill }

// Wrap makes a row break into lines when it runs out of width (CSS
// flex-wrap, QML Flow, a GridView's cells): children keep their measured
// size and the ones that no longer fit start the next line.
func (d *DivEl) Wrap() *DivEl { d.wrap = true; return d }

// ShrinkBy is Shrink with a custom flex-shrink weight.
func (d *DivEl) ShrinkBy(f float32) *DivEl { d.shrink = f; return d }
func (d *DivEl) P(px float32) *DivEl       { d.pad = px; return d }

// PadXY sets horizontal and vertical padding independently (CSS
// padding: y x; GPUI px()/py()). Overrides P when set.
func (d *DivEl) PadXY(x, y float32) *DivEl { d.padX, d.padY, d.hasPadXY = x, y, true; return d }

// Padding sets each side on its own, in CSS order (top, right, bottom,
// left) — QML's leftPadding/rightPadding on a control, and the
// Layout.*Margin of a layout item. Overrides P and PadXY.
func (d *DivEl) Padding(t, r, b, l float32) *DivEl {
	d.padT, d.padR, d.padB, d.padL, d.hasPadSides = t, r, b, l, true
	return d
}

// padSides resolves the effective padding per side.
func (d *DivEl) padSides() (t, r, b, l float32) {
	if d.hasPadSides {
		return d.padT, d.padR, d.padB, d.padL
	}
	x, y := d.padWH()
	return y, x, y, x
}

// padWH resolves the effective horizontal/vertical padding, halved per
// side when the sides differ (callers that need the exact edges use
// padSides).
func (d *DivEl) padWH() (float32, float32) {
	if d.hasPadSides {
		return (d.padL + d.padR) / 2, (d.padT + d.padB) / 2
	}
	if d.hasPadXY {
		return d.padX, d.padY
	}
	return d.pad, d.pad
}
func (d *DivEl) Gap(px float32) *DivEl { d.gap = px; return d }
func (d *DivEl) Bg(c RGBA) *DivEl      { d.bg = c; d.hasBg = true; return d }

// fillColor / childElems back ui.FillColor and ui.Children (element.go).
func (d *DivEl) fillColor() (RGBA, bool) { return d.bg, d.hasBg }
func (d *DivEl) childElems() []Element   { return d.children }

// Rounded sets the corner radius in pixels.
func (d *DivEl) Rounded(px float32) *DivEl { d.radius = px; return d }

// CornerRadius reports an element's corner radius (0 for non-divs) — for
// tooling and tests that assert built styling across packages.
func CornerRadius(e Element) float32 {
	if d, ok := e.(*DivEl); ok {
		return d.radius
	}
	return 0
}

// Gradient fills the box with a linear gradient from c0 to c1 along
// angleRad (0 = left→right, π/2 = top→bottom); overrides Bg.
func (d *DivEl) Gradient(c0, c1 RGBA, angleRad float32) *DivEl {
	d.gradC0, d.gradC1, d.gradAng, d.hasGrad = c0, c1, angleRad, true
	d.gradS0, d.gradS1 = 0, 1
	d.gradRadial = false
	return d
}

// GradientStops is Gradient with explicit ramp positions: c0 holds until s0,
// c1 from s1 on (0..1 along the axis). Equal stops give a hard color split.
func (d *DivEl) GradientStops(c0, c1 RGBA, angleRad, s0, s1 float32) *DivEl {
	d.Gradient(c0, c1, angleRad)
	d.gradS0, d.gradS1 = s0, s1
	return d
}

// GradientStopsN is a linear gradient of any number of positioned stops —
// QML puts no limit on a Gradient's stops, and a quad's ramp holds four.
func (d *DivEl) GradientStopsN(stops []render.GradientStop, angleRad float32) *DivEl {
	d.gradStops, d.gradAng, d.hasGrad = stops, angleRad, true
	return d
}

// GradientRamp is the full-fidelity linear gradient: up to 4 positioned
// stops, optionally blended in a non-default color space; overrides Bg.
func (d *DivEl) GradientRamp(ramp render.Ramp, angleRad float32) *DivEl {
	d.gradRamp = &ramp
	d.gradAng, d.hasGrad, d.gradRadial = angleRad, true, false
	return d
}

// RadialGradient fills the box with a radial gradient from c0 at the center to
// c1 at the corners; overrides Bg.
func (d *DivEl) RadialGradient(c0, c1 RGBA) *DivEl {
	d.gradC0, d.gradC1, d.hasGrad, d.gradRadial = c0, c1, true, true
	return d
}

// Border draws an inside border of the given width and color.
func (d *DivEl) Border(w float32, c RGBA) *DivEl { d.borderW, d.borderC = w, c; return d }

// Side identifies one edge of a box for per-side borders.
type Side int

const (
	SideTop Side = iota
	SideRight
	SideBottom
	SideLeft
)

// BorderSide draws an inside border on a single edge (QML border_t_1 /
// border_b_1 / border_r_1 / border_l_1, GPUI border_t/r/b/l). Unlike Border
// (uniform, SDF-evaluated, rounds with the corner) this paints a flat strip,
// so it composes with the uniform border and is meant for dividers/grid lines.
func (d *DivEl) BorderSide(s Side, w float32, c RGBA) *DivEl {
	d.sideW[s], d.sideC[s], d.hasSide = w, c, true
	return d
}

// BorderTop/Right/Bottom/Left are shorthands for BorderSide.
func (d *DivEl) BorderTop(w float32, c RGBA) *DivEl    { return d.BorderSide(SideTop, w, c) }
func (d *DivEl) BorderRight(w float32, c RGBA) *DivEl  { return d.BorderSide(SideRight, w, c) }
func (d *DivEl) BorderBottom(w float32, c RGBA) *DivEl { return d.BorderSide(SideBottom, w, c) }
func (d *DivEl) BorderLeft(w float32, c RGBA) *DivEl   { return d.BorderSide(SideLeft, w, c) }

// BorderDashed makes the border dashed (GPUI border_style: dashed); the dash
// size scales with the border width.
func (d *DivEl) BorderDashed() *DivEl { d.borderDashed = true; return d }

// Clip clips this box's children to its bounds (CSS overflow: hidden, GPUI
// overflow_hidden) — content wider/taller than the box is cut, not painted
// over siblings.
func (d *DivEl) Clip() *DivEl { d.clip = true; return d }

// HoverBg swaps the background while the pointer is over the element.
func (d *DivEl) HoverBg(c RGBA) *DivEl { d.hoverBg = c; d.hasHover = true; return d }

// PressBg swaps the background while the element is being pressed.
func (d *DivEl) PressBg(c RGBA) *DivEl { d.pressBg = c; d.hasPress = true; return d }

// boxShadow is one entry of a CSS-style box-shadow list.
type boxShadow struct {
	blur, ox, oy, spread float32
	c                    RGBA
}

// Shadow casts a Gaussian drop shadow with the given blur sigma, offset
// and color. Each call adds one shadow (CSS box-shadow list; GPUI
// .shadow(vec![...])).
func (d *DivEl) Shadow(blur, offsetX, offsetY float32, c RGBA) *DivEl {
	return d.ShadowSpread(blur, offsetX, offsetY, 0, c)
}

// ShadowSpread is Shadow plus a spread radius (GPUI BoxShadow.spread_radius):
// the shadow shape is grown by spread on every side before blurring, so the
// halo reads larger/darker than blur alone. Negative spread shrinks it.
func (d *DivEl) ShadowSpread(blur, offsetX, offsetY, spread float32, c RGBA) *DivEl {
	d.shadows = append(d.shadows, boxShadow{blur, offsetX, offsetY, spread, c})
	return d
}

// Opacity multiplies the alpha of this element and its whole subtree by a
// (0..1). Per-primitive multiply, not layer compositing — overlapping
// translucent children don't composite as one group (GPUI opacity.rs's
// common case).
func (d *DivEl) Opacity(a float32) *DivEl { d.opacity, d.hasOpacity = a, true; return d }

// Focusable lets this element receive keyboard focus (Tab navigation,
// focus ring, Enter/Space activation of its OnClick). Buttons and other
// interactive containers should set it.
func (d *DivEl) Focusable() *DivEl { d.focusable = true; return d }

// FocusBorder draws an inside border while the element holds focus, however
// focus arrived (GPUI's .focus(|s| s.border...), CSS :focus). Implies
// Focusable and replaces the UI's generic focus ring.
func (d *DivEl) FocusBorder(w float32, c RGBA) *DivEl {
	d.focusW, d.focusC, d.focusable = w, c, true
	return d
}

// FocusVisibleBorder draws an inside border only while the element holds
// KEYBOARD-acquired focus (GPUI's .focus_visible, CSS :focus-visible) —
// clicking focuses without showing it. Implies Focusable and replaces the
// UI's generic focus ring.
func (d *DivEl) FocusVisibleBorder(w float32, c RGBA) *DivEl {
	d.focusVisW, d.focusVisC, d.focusable = w, c, true
	return d
}

// hasFocusStyle reports whether this element paints its own focus state (so
// the UI's generic blue ring should not also be drawn).
func (d *DivEl) hasFocusStyle() bool { return d.focusW > 0 || d.focusVisW > 0 }

// TabIndex ranks this element in Tab traversal (GPUI tab_index): lower
// indices focus first, ties fall back to layout order (elements without an
// explicit index rank 0). Implies Focusable.
func (d *DivEl) TabIndex(n int) *DivEl {
	d.tabIdx, d.focusable = n, true
	return d
}

func (d *DivEl) tabIndex() int { return d.tabIdx }

// OnKey installs a raw keyboard handler (QML Keys.onPressed/onReleased): the
// element becomes focusable and receives key events while focused. The handler
// gets every event; its Down field distinguishes press from release.
func (d *DivEl) OnKey(fn func(app.KeyEvent)) *DivEl {
	d.keyFn = fn
	d.focusable = true
	return d
}

// handleKey delivers a key event to this element's handler, reporting whether
// it consumed the event (false when there's no key handler, so the UI can fall
// through to default Enter/Space activation).
func (d *DivEl) handleKey(ev app.KeyEvent) bool {
	if d.keyFn != nil {
		d.keyFn(ev)
		return true
	}
	return false
}

// Focus requests initial keyboard focus (QML focus: true).
func (d *DivEl) Focus() *DivEl { d.wantFocus = true; d.focusable = true; return d }

// OnSwipe installs a horizontal swipe handler (QML SwipeView): fn gets -1 for
// a leftward swipe (next page) and +1 for rightward (previous page). A press
// that travels ≥48px mostly-horizontally before release is a swipe; it then
// does not also click.
func (d *DivEl) OnSwipe(fn func(dir int)) *DivEl { d.swipeFn = fn; return d }

// OnDrag installs a press-drag-release handler: fn gets the movement since
// the press in pixels, and the phase. Unlike OnSwipe (one flip per gesture)
// it fires continuously, which is what a control that FOLLOWS the pointer
// needs — a tumbler's wheel, a scrubber, a pan surface.
func (d *DivEl) OnDrag(fn func(dx, dy float32, phase DragPhase)) *DivEl {
	d.dragFn = fn
	return d
}

// swipeRegion / dragRegion implementation.
func (d *DivEl) contains(x, y float32) bool { return d.rect.contains(x, y) }
func (d *DivEl) swipe(dir int) {
	if d.swipeFn != nil {
		d.swipeFn(dir)
	}
}

func (d *DivEl) drag(dx, dy float32, phase DragPhase) {
	if d.dragFn != nil {
		d.dragFn(dx, dy, phase)
	}
	if d.pointerFn != nil {
		d.pointerFn(d.pressX+dx, d.pressY+dy, phase)
	}
}

// OnPointer installs a press-move-release handler that is told where the
// pointer is, in window coordinates, rather than how far it has come: for
// content that has to know which part of itself was pressed — a selection
// in a page of text, a link in it. A release close to the press is a
// click; telling the two apart is the handler's business.
func (d *DivEl) OnPointer(fn func(x, y float32, phase DragPhase)) *DivEl {
	d.pointerFn = fn
	return d
}

// pressAt is told where a press on this drag region landed, before its
// DragStart.
func (d *DivEl) pressAt(x, y float32) { d.pressX, d.pressY = x, y }

// OnFling installs a release-velocity handler (pixels per second, positive
// down and right): the kinetic half of a drag. It fires on release, after
// the last OnDrag call, and only when the pointer was still moving.
func (d *DivEl) OnFling(fn func(vx, vy float32)) *DivEl { d.flingFn = fn; return d }

func (d *DivEl) fling(vx, vy float32) {
	if d.flingFn != nil {
		d.flingFn(vx, vy)
	}
}

// PopupSkin gives an unstyled overlay panel a default card look (light bg,
// padding, rounded corners, subtle shadow) — applied only if it has no bg yet.
// DarkPopupSkin is PopupSkin for a dark page: the same card, in the colours
// a dark app's own popup style would use. A light card over a dark page is
// not just a palette mismatch — the content inside it is written for the
// dark ground, so its light text lands on white and vanishes.
func (d *DivEl) DarkPopupSkin() *DivEl {
	if !d.hasBg && !d.hasGrad {
		d.bg, d.hasBg = RGB(0x2a2d35), true
		d.borderC, d.borderW = RGB(0x3a3d47), 1
		d.radius = 8
		if d.pad == 0 {
			d.pad = 16
		}
		d.shadows = []boxShadow{{blur: 24, oy: 6, c: RGBA{A: 0.5}}}
	}
	return d
}

func (d *DivEl) PopupSkin() *DivEl {
	if !d.hasBg && !d.hasGrad {
		d.bg, d.hasBg = RGB(0xf4f5f7), true
		d.radius = 8
		if d.pad == 0 {
			d.pad = 16
		}
		d.shadows = []boxShadow{{blur: 24, oy: 6, c: RGBA{A: 0.4}}}
	}
	return d
}

// wantsInitialFocus reports a QML focus: true request.
func (d *DivEl) wantsInitialFocus() bool { return d.wantFocus }

// Cursor sets the mouse-pointer shape shown while hovering this element.
func (d *DivEl) Cursor(c app.Cursor) *DivEl { d.cursor = c; return d }

func (d *DivEl) cursorShape() app.Cursor { return d.cursor }

// AnchorMode switches this container to QML anchor layout: its children
// (expected to be AnchorEl) are positioned absolutely from their Anchors
// rather than stacked. Used for QML Item/Rectangle with anchored children.
func (d *DivEl) AnchorMode() *DivEl { d.anchorEd = true; return d }

// AnchorFit makes an anchor container take its implicit size from its
// children (Qt Controls containers size to their contentItem).
func (d *DivEl) AnchorFit() *DivEl        { d.anchorFit = true; return d }
func (d *DivEl) Justify(a Align) *DivEl   { d.justify = a; return d }
func (d *DivEl) Items(a Align) *DivEl     { d.items = a; return d }
func (d *DivEl) JustifyCenter() *DivEl    { return d.Justify(AlignCenter) }
func (d *DivEl) ItemsCenter() *DivEl      { return d.Items(AlignCenter) }
func (d *DivEl) OnClick(fn func()) *DivEl { d.click = fn; return d }

// AlsoOnClick adds fn AFTER whatever click handler the element already has,
// rather than replacing it — for a wrapper that has something of its own to
// do once the element's own handler has run (a menu closing after the item
// it holds has been triggered).
func (d *DivEl) AlsoOnClick(fn func()) *DivEl {
	if prev := d.click; prev != nil {
		d.click = func() { prev(); fn() }
		return d
	}
	d.click = fn
	return d
}

// OnHoverChanged reports this element's hover state every paint (the element
// tree is rebuilt each frame, so state that should persist across frames —
// like a Ref backing MouseArea.containsMouse — must be owned by the caller).
// Repeated calls chain (a hoisted MouseArea/TapHandler adds its callback
// alongside the box's own, never replacing it).
func (d *DivEl) OnHoverChanged(fn func(bool)) *DivEl {
	if prev := d.hoverFn; prev != nil {
		d.hoverFn = func(v bool) { prev(v); fn(v) }
	} else {
		d.hoverFn = fn
	}
	return d
}

// OnPressChanged reports this element's press state every paint, same
// caveats as OnHoverChanged.
func (d *DivEl) OnPressChanged(fn func(bool)) *DivEl {
	if prev := d.pressFn; prev != nil {
		d.pressFn = func(v bool) { prev(v); fn(v) }
	} else {
		d.pressFn = fn
	}
	return d
}
func (d *DivEl) Children(els ...Element) *DivEl {
	d.children = append(d.children, els...)
	return d
}

// Rect reports the rectangle assigned by the last layout (for tests and
// tooling).
func (d *DivEl) Rect() Rect { return d.rect }

func (d *DivEl) main(w, h float32) float32 {
	if d.row {
		return w
	}
	return h
}
func (d *DivEl) cross(w, h float32) float32 {
	if d.row {
		return h
	}
	return w
}

func (d *DivEl) natural() (float32, float32) { return d.natW, d.natH }

func (d *DivEl) measure(ctx *Context) (float32, float32) {
	ctx.MeasureCalls++
	// Memoized per frame and offered width (see Context.frame): ten
	// measures per element per frame was the whole of layout's cost.
	if d.mFrame == ctx.frame && d.mAvail == ctx.availW && ctx.frame != 0 {
		return d.mW, d.mH
	}
	w, h := d.measureUncached(ctx)
	d.mFrame, d.mAvail, d.mW, d.mH = ctx.frame, ctx.availW, w, h
	return w, h
}

func (d *DivEl) measureUncached(ctx *Context) (float32, float32) {
	w, h := d.measureNat(ctx)
	d.natW, d.natH = w, h
	// A box with no implicit size of its own reports none, whatever it
	// contains: its content is still measured (children need it, and
	// Implicit still reports it) but a parent LAYOUT is told the size the
	// box states for itself, which for a bare Item or Rectangle is
	// nothing. That is QML's rule, and it is what makes a grid of cards
	// share its width evenly instead of by how much text each card holds.
	if d.noPref {
		return max(d.w, d.minW), max(d.h, d.minH)
	}
	return w, h
}

func (d *DivEl) measureNat(ctx *Context) (float32, float32) {
	if d.w > 0 && d.h > 0 {
		return d.w, d.h
	}
	// Anchored children depend on the container's size, so they cannot inform
	// it; an anchor container sizes only from its own explicit width/height
	// (0 = fill whatever its own parent assigns).
	if d.anchorEd {
		w, h := max(d.w, d.minW), max(d.h, d.minH)
		if d.anchorFit {
			// A Controls container (Pane, ToolBar, ...) takes its implicit
			// size from its content even when that content fills it by
			// anchor — Qt puts declared children in the contentItem, whose
			// implicit size the control adopts.
			px, py := d.padWH()
			var cw, ch float32
			for _, c := range d.children {
				if isOutOfFlow(c) {
					continue
				}
				a, b := c.measure(ctx)
				cw, ch = max(cw, a), max(ch, b)
			}
			w, h = max(w, cw+2*px), max(h, ch+2*py)
		}
		return w, h
	}
	var mainSum, crossMax float32
	var minMain, minCross float32
	inflow := 0
	// A fixed-width box offers that width to its children's measurement.
	prevAvail := ctx.availW
	if d.w > 0 {
		px, _ := d.padWH()
		ctx.availW = d.w - 2*px
		defer func() { ctx.availW = prevAvail }()
	}
	var rowBoxes []childBox
	if d.row {
		rowBoxes = make([]childBox, 0, len(d.children))
	}
	for _, c := range d.children {
		if isOutOfFlow(c) {
			continue // overlays: no size and no gap slot
		}
		cw, ch := c.measure(ctx)
		if inflow > 0 {
			mainSum += d.gap
		}
		inflow++
		mainSum += d.main(cw, ch)
		crossMax = max(crossMax, d.cross(cw, ch))
		if d.row {
			g := float32(0)
			if gf, ok := c.(interface{ growFactor() float32 }); ok {
				g = gf.growFactor()
			}
			rowBoxes = append(rowBoxes, childBox{el: c, w: cw, h: ch, grow: g})
		}
		// What this child cannot do without. An item that FILLS an axis can
		// shrink on it, down to its own minimum (recursively, for a nested
		// layout); one that does not is stuck at the size it measured. That
		// is the difference between a row of buttons, which overflows its
		// card, and a row of sliders, which simply gets narrower.
		reqW, reqH := requiredSize(c, cw, ch, d.row)
		if inflow > 1 && !(d.wrap && d.row) {
			minMain += d.gap
		}
		if d.wrap && d.row {
			// A WRAPPING row can do without the width its children add up
			// to — it breaks them onto more lines. What it cannot do
			// without is its widest single child (QML Flow: four 145px
			// buttons in a 400px card make two lines of two, they do not
			// push the card to 625).
			minMain = max(minMain, d.main(reqW, reqH))
		} else {
			minMain += d.main(reqW, reqH)
		}
		minCross = max(minCross, d.cross(reqW, reqH))
	}
	if d.row {
		d.minReqW, d.minReqH = minMain, minCross
	} else {
		d.minReqW, d.minReqH = minCross, minMain
	}
	// A COLUMN whose content needs more width than it HAS works in that
	// wider extent (the required-size rule), and offers it to its
	// children: a wrapping caption under a row of dials 460px wide stays
	// on one line, and its cell keeps a single line's height — which is
	// what moves everything above it into place.
	// ... but only when this column HAS a width to compare against. Asked
	// to measure with nothing offered (a first pass, or a subtree measured
	// out of band), there is no shortfall to work in — and re-measuring
	// the children at minCross there taught every wrapping label inside a
	// wrap width of its widest SIBLING, which stuck: an 860px-wide card
	// description came back wrapped at 218 (the canvas page's gradient
	// demos then sat 12px low, under a subtitle three lines too tall).
	if !d.row && d.spread && have(ctx, d) > 0 && minCross > have(ctx, d) {
		var sum, cross float32
		n := 0
		for _, c := range d.children {
			if isOutOfFlow(c) {
				continue
			}
			cw, ch := c.measure(ctx)
			if hfw, ok := c.(heightForWidther); ok {
				ch = hfw.heightFor(ctx, minCross)
				cw = minCross
			}
			if n > 0 {
				sum += d.gap
			}
			n++
			sum += d.main(cw, ch)
			cross = max(cross, d.cross(cw, ch))
		}
		mainSum, crossMax = sum, cross
	}
	// A row with a wrapping child cannot know that child's height until it
	// knows its width, so share the offered width out the way layout will
	// and ask again. Without this a row inside a column reports one line's
	// height and the text overflows the space the column gave it.
	if d.row && ctx.availW > 0 {
		var hfwCount int
		for _, b := range rowBoxes {
			if _, ok := b.el.(heightForWidther); ok {
				hfwCount++
			}
		}
		if hfwCount > 0 {
			px, _ := d.padWH()
			innerW := ctx.availW - 2*px
			if d.w > 0 {
				innerW = d.w - 2*px
			}
			var fixed, growSum float32
			for i := range rowBoxes {
				b := &rowBoxes[i]
				if b.grow < 0 {
					b.grow = max(b.w, 0.01) // weight by content; see below
				}
				if b.grow == 0 {
					fixed += b.w
				}
				growSum += b.grow
			}
			gaps := d.gap * float32(len(rowBoxes)-1)
			leftover := max(0, innerW-fixed-gaps)
			crossMax = 0
			for i := range rowBoxes {
				b := &rowBoxes[i]
				bw := b.w
				if b.grow > 0 && growSum > 0 {
					bw = leftover * b.grow / growSum
				}
				if hfw, ok := b.el.(heightForWidther); ok && bw > 0 {
					b.h = hfw.heightFor(ctx, bw)
				}
				crossMax = max(crossMax, b.h)
			}
		}
	}
	var w, h float32
	if d.row && d.wrap {
		// A wrapping row is as wide as it is allowed to be and as tall as
		// the lines it breaks into (CSS height-for-width).
		px, py := d.padWH()
		avail := ctx.availW - 2*px
		if d.w > 0 {
			avail = d.w - 2*px
		}
		w, h = mainSum+2*px, crossMax+2*py
		if avail > 0 {
			w, h = avail+2*px, d.wrappedHeight(ctx, avail)+2*py
		}
	} else if d.row {
		px, py := d.padWH()
		w, h = mainSum+2*px, crossMax+2*py
	} else {
		px, py := d.padWH()
		w, h = crossMax+2*px, mainSum+2*py
	}
	if d.w > 0 {
		w = d.w
	}
	if d.h > 0 {
		h = d.h
	}
	return max(w, d.minW), max(h, d.minH)
}

// childBox is one in-flow child during a layout pass: its measured size and
// its flex weights.
type childBox struct {
	el         Element
	w, h       float32
	natW, natH float32 // measured size, before growing or stretching
	grow       float32
	shrink     float32
	stretched  bool // filled the cross axis, so it must not also be aligned
}

// wrapLayout places a wrapping row: children keep their measured width and
// spill onto the next line when the current one is full. Lines stack by the
// tallest child in each, and `items` aligns a child within its own line.
func (d *DivEl) wrapLayout(ctx *Context, inner Rect, boxes []childBox) {
	line := make([]int, 0, len(boxes))
	y := inner.Y
	flush := func() {
		if len(line) == 0 {
			return
		}
		var lineH float32
		for _, i := range line {
			lineH = max(lineH, boxes[i].h)
		}
		x := inner.X
		for _, i := range line {
			b := &boxes[i]
			off := float32(0)
			switch d.items {
			case AlignCenter:
				off = (lineH - b.h) / 2
			case AlignEnd:
				off = lineH - b.h
			}
			b.el.layout(ctx, Rect{x, y + off, b.w, b.h})
			x += b.w + d.gap
		}
		y += lineH + d.gap
		line = line[:0]
	}
	var used float32
	for i := range boxes {
		w := boxes[i].w
		if len(line) > 0 && used+d.gap+w > inner.W {
			flush()
			used = 0
		}
		if len(line) > 0 {
			used += d.gap
		}
		used += w
		line = append(line, i)
	}
	flush()
}

// wrappedHeight is how tall this row is once it breaks into lines of at
// most `avail` width.
func (d *DivEl) wrappedHeight(ctx *Context, avail float32) float32 {
	var total, lineW, lineH float32
	lines := 0
	for _, c := range d.children {
		if isOutOfFlow(c) {
			continue
		}
		cw, ch := c.measure(ctx)
		if lineW > 0 && lineW+d.gap+cw > avail {
			total += lineH + d.gap
			lines++
			lineW, lineH = 0, 0
		}
		if lineW > 0 {
			lineW += d.gap
		}
		lineW += cw
		lineH = max(lineH, ch)
	}
	if lineH > 0 {
		total += lineH
		lines++
	}
	if lines == 0 {
		return 0
	}
	return total
}

func (d *DivEl) layout(ctx *Context, r Rect) {
	ctx.LayoutCalls++
	d.rect = r
	// Live hover/press state is reported HERE rather than while painting:
	// a control's own state has to be available to anything that lays the
	// tree out — headless renders, tooling, tests — not only to a frame
	// that reaches the renderer. (The hover/press VISUALS still resolve at
	// paint time, from the same rect.)
	if d.hoverFn != nil || d.pressFn != nil {
		hovered := r.contains(ctx.MouseX, ctx.MouseY)
		if d.hoverFn != nil {
			d.hoverFn(hovered)
		}
		if d.pressFn != nil {
			d.pressFn(hovered && ctx.MousePressed && r.contains(ctx.pressX, ctx.pressY))
		}
	}
	if d.focusable {
		d.focusSlot = len(ctx.focusables) + 1
		ctx.focusables = append(ctx.focusables, d)
	}
	if d.swipeFn != nil {
		ctx.swipes = append(ctx.swipes, d)
	}
	if d.dragFn != nil || d.flingFn != nil || d.pointerFn != nil {
		ctx.drags = append(ctx.drags, d)
	}
	if len(d.children) == 0 {
		return
	}
	padT, padR, padB, padL := d.padSides()
	inner := Rect{r.X + padL, r.Y + padT, r.W - padL - padR, r.H - padT - padB}

	if d.anchorEd {
		d.anchorLayout(ctx, inner)
		return
	}

	// Pass 1: measure children, find grow capacity.
	boxes := make([]childBox, 0, len(d.children))
	var outOfFlow []Element // laid out after the flow pass; no size, no gap
	var fixedMain, growSum float32
	// Offer the line width to children while they measure: auto-width
	// wrapped text needs it to know how many lines it takes (CSS
	// height-for-width). A row shares its width between children, so the
	// offer there is the whole line — an upper bound.
	prevAvail := ctx.availW
	ctx.availW = inner.W
	defer func() { ctx.availW = prevAvail }()
	for _, c := range d.children {
		if isOutOfFlow(c) {
			outOfFlow = append(outOfFlow, c)
			continue
		}
		cw, ch := c.measure(ctx)
		g, s := float32(0), float32(0)
		if gf, ok := c.(interface{ growFactor() float32 }); ok {
			g = gf.growFactor() // DivEl, GridEl, … opt in via this method
		}
		if sf, ok := c.(interface{ shrinkFactor() float32 }); ok {
			s = sf.shrinkFactor()
		}
		boxes = append(boxes, childBox{el: c, w: cw, h: ch, natW: cw, natH: ch, grow: g, shrink: s})
		if g == 0 {
			fixedMain += d.main(cw, ch)
		}
		growSum += g
	}
	// A wrapping row places its children line by line instead of sharing
	// one line's width between them.
	if d.wrap && d.row {
		d.wrapLayout(ctx, inner, boxes)
		for _, c := range outOfFlow {
			c.layout(ctx, Rect{})
		}
		return
	}
	// A NEGATIVE grow factor means "weight me by my own content": QML's
	// stretch factor defaults to the item's preferred size, so a row of
	// filling items divides the space in proportion to what each of them
	// naturally wants (probed against Qt: four labelled columns of
	// implicit width 45.5/22.6/79.4/32.7 came out 210/105/365/150 — the
	// same ratio to within a pixel). An equal split is only right when
	// none of them wants anything.
	growSum = 0
	for i := range boxes {
		if b := &boxes[i]; b.grow < 0 {
			b.grow = max(d.main(b.natW, b.natH), 0.01)
		}
		growSum += boxes[i].grow
	}
	gaps := d.gap * float32(len(boxes)-1)
	innerMain := d.main(inner.W, inner.H)
	leftover := max(0, innerMain-fixedMain-gaps)

	// Shrink pass (CSS flex-shrink, opt-in via Shrink): when the fixed run
	// overflows the container, compress shrinkable children in proportion
	// to shrink-weight × basis, no further than each child's own floor
	// (CSS min-*, Layout.minimumWidth — a control does not shrink below
	// its implicit size, so a crowded row overflows the way Qt's does).
	if deficit := fixedMain + gaps - innerMain; deficit > 0 {
		var weight float32
		for i := range boxes {
			if b := &boxes[i]; b.shrink > 0 && b.grow == 0 {
				weight += b.shrink * d.main(b.w, b.h)
			}
		}
		if weight > 0 {
			for i := range boxes {
				b := &boxes[i]
				if b.shrink == 0 || b.grow > 0 {
					continue
				}
				m := d.main(b.w, b.h)
				// The floor is what the child cannot do without — for a
				// nested layout, what ITS content needs (requiredSize).
				// Shrinking past that piles items on top of each other,
				// where Qt just overflows: the corpus' checkbox card
				// printed its footer over its own options.
				rw, rh := requiredSize(b.el, b.natW, b.natH, d.row)
				floor := min(d.main(rw, rh), m)
				if mf, ok := b.el.(interface{ minWH() (float32, float32) }); ok {
					mw, mh := mf.minWH()
					floor = max(floor, min(d.main(mw, mh), m))
				}
				cut := min(deficit*b.shrink*m/weight, m-floor)
				if d.row {
					b.w = m - cut
				} else {
					b.h = m - cut
				}
			}
		}
	}

	// The cross extent this layout works in: what the box HAS, or what its
	// content NEEDS when that is more. Qt lays a layout out in the space it
	// requires and lets the overflow spill (a card's button row runs past
	// its right edge rather than squeezing), which moves both the middle an
	// item is centred on and the width a filling item is given.
	// The cross extent this layout works in: what the box HAS, or what its
	// content cannot do without when that is more. Qt lays a layout out in
	// the space it REQUIRES and lets the overflow spill (a card's row of
	// buttons runs past its right edge rather than squeezing), which moves
	// both the middle an item is centred on and the width a filling item is
	// given.
	alignCross := d.cross(inner.W, inner.H)
	if d.spread {
		var need float32
		for i := range boxes {
			rw, rh := requiredSize(boxes[i].el, boxes[i].natW, boxes[i].natH, d.row)
			need = max(need, d.cross(rw, rh))
		}
		alignCross = max(alignCross, need)
	}

	// Pass 2: resolve sizes; grow children share leftover, others stretch
	// the cross axis when they are containers without a fixed cross size.
	var usedMain, slack float32
	for i := range boxes {
		b := &boxes[i]
		if b.grow > 0 {
			share := leftover * b.grow / growSum
			// A child can be capped at what it can usefully use (a nested
			// layout stops at its own content); what it leaves is shared
			// out below.
			if lm, ok := b.el.(layoutMaxer); ok {
				mw, mh := lm.layoutMax()
				if m := d.main(mw, mh); m > 0 && share > m {
					slack += share - m
					share = m
				}
			}
			if d.row {
				b.w = share
			} else {
				b.h = share
			}
		}
		// A wrapping label is only as tall as the width it ends up with
		// (CSS height-for-width): it measured against the whole line,
		// because that is all a first pass can know, and its share is
		// narrower. Ask it again now that the width is settled.
		if d.row {
			if hfw, ok := b.el.(heightForWidther); ok && b.w > 0 {
				b.h = hfw.heightFor(ctx, b.w)
			}
		} else if hfw, ok := b.el.(heightForWidther); ok && alignCross > inner.W {
			// In a COLUMN the width comes from the cross extent, which is
			// wider than the box when the content overflows it: a caption
			// under a row of dials 460px wide stays on one line, as Qt's
			// does, instead of wrapping to the card's 280.
			b.w = alignCross
			b.h = hfw.heightFor(ctx, alignCross)
		}
		// Stretch the cross axis (flex align-items: stretch — the default):
		// an auto-sized element fills the line. With items center/end the
		// child keeps its natural cross size so it can be aligned instead
		// (proper align-items semantics). Only auto-sized elements (those
		// exposing fixedWH) participate, so a fixed-size element keeps its size.
		stretch := d.items != AlignCenter && d.items != AlignEnd
		if _, ok := selfAlignOf(b.el); ok {
			// A child that states its own cross-axis alignment is placed,
			// not stretched (QML's Layout.alignment, CSS align-self).
			stretch = false
		}
		if d.crossCenter {
			// Qt's layouts stretch only the items that asked to fill (the
			// others keep their height and sit level with them).
			cf, ok := b.el.(interface{ crossFills() bool })
			stretch = ok && cf.crossFills()
		}
		if stretch {
			if fs, ok := b.el.(interface{ fixedWH() (float32, float32) }); ok {
				fw, fh := fs.fixedWH()
				// A stretched child is still capped at what it can
				// usefully use — a nested GRID of fixed cells stops at
				// its own content, the same rule the main axis applies
				// above. Without it a 2-column grid of 280px cards
				// stretched to the card's full row and spread its
				// columns, where Qt keeps them 280 wide side by side.
				cross := alignCross
				if lm, ok := b.el.(layoutMaxer); ok {
					mw, mh := lm.layoutMax()
					if m := d.cross(mw, mh); m > 0 && cross > m {
						cross = m
					}
				}
				if d.row && fh == 0 {
					b.h, b.stretched = cross, true
				}
				if !d.row && fw == 0 {
					b.w, b.stretched = cross, true
				}
			}
		}
		// A clipping box hands its children no more room than it has, so
		// what would spill is laid out to the visible width instead — which
		// is also what lets a truncating text ellipsize (it elides to the
		// width it was given).
		if d.clip {
			b.w, b.h = min(b.w, inner.W), min(b.h, inner.H)
		}
		// A child's size floor holds however the space was distributed
		// (Layout.minimumWidth/Height, CSS min-*).
		if mf, ok := b.el.(interface{ minWH() (float32, float32) }); ok {
			mw, mh := mf.minWH()
			b.w, b.h = max(b.w, mw), max(b.h, mh)
		}
		usedMain += d.main(b.w, b.h)
	}
	usedMain += gaps

	// Justify offsets the run along the main axis when nothing grows.
	start := float32(0)
	if growSum == 0 {
		switch d.justify {
		case AlignCenter:
			start = (innerMain - usedMain) / 2
		case AlignEnd:
			start = innerMain - usedMain
		}
	}

	// QtQuick.Layouts share the leftover along the main axis when nothing
	// expands: every child gets a cell in proportion to its own size and
	// sits centred in it (see SpreadCells).
	cells := make([]float32, len(boxes))
	if d.spread && (growSum == 0 || slack > 0) {
		total := usedMain - gaps
		space := innerMain - gaps
		for i := range boxes {
			cells[i] = d.main(boxes[i].w, boxes[i].h)
		}
		if surplus := space - total; surplus > 0 {
			switch {
			case total > 0:
				for i := range cells {
					cells[i] += surplus * cells[i] / total
				}
			case len(cells) > 0:
				even := space / float32(len(cells))
				for i := range cells {
					cells[i] = even
				}
			}
			start = 0
		}
	}

	pen := start
	for i := range boxes {
		b := &boxes[i]
		var x, y float32
		crossSize := d.cross(b.w, b.h)
		innerCross := alignCross
		crossOff := float32(0)
		self, hasSelf := selfAlignOf(b.el)
		switch {
		case hasSelf && self == AlignCenter:
			crossOff = (innerCross - crossSize) / 2
		case hasSelf && self == AlignEnd:
			crossOff = innerCross - crossSize
		case hasSelf: // AlignStart: hard against the leading edge
		case d.items == AlignCenter, d.crossCenter && !b.stretched:
			crossOff = (innerCross - crossSize) / 2
		case d.items == AlignEnd:
			crossOff = innerCross - crossSize
		}
		mainSize := d.main(b.w, b.h)
		cell := mainSize
		if cells[i] > cell {
			cell = cells[i]
		}
		// Where the item sits in a cell wider than itself: Qt centres it
		// VERTICALLY and leaves it against the leading edge horizontally
		// (its default Layout.alignment), so a row of unfilled controls
		// stays left-packed in each slice while a column's items sit in
		// the middle of theirs.
		mainOff := pen
		if !d.row {
			mainOff += (cell - mainSize) / 2
		}
		if d.row {
			x, y = inner.X+mainOff, inner.Y+crossOff
		} else {
			x, y = inner.X+crossOff, inner.Y+mainOff
		}
		b.el.layout(ctx, Rect{x, y, b.w, b.h})
		pen += cell + d.gap
	}
	// Out-of-flow children (overlays/modals) lay out after every in-flow
	// sibling so element anchors resolve against final rects.
	for _, c := range outOfFlow {
		c.layout(ctx, Rect{})
	}
}

// anchorLayout places children absolutely from their Anchors. It resolves to
// a fixpoint so an item anchored to a sibling waits for that sibling's rect;
// unresolved items (cycles / forward refs to unresolved targets) fall back to
// parent-relative placement. Non-AnchorEl children sit at the parent origin.
func (d *DivEl) anchorLayout(ctx *Context, inner Rect) {
	resolved := map[string]Rect{"parent": inner}
	type item struct {
		ae         *AnchorEl
		natW, natH float32
	}
	var items []item
	for _, c := range d.children {
		ae, ok := c.(*AnchorEl)
		if !ok {
			cw, ch := c.measure(ctx)
			c.layout(ctx, Rect{inner.X, inner.Y, cw, ch})
			continue
		}
		w, h := ae.measure(ctx)
		items = append(items, item{ae: ae, natW: w, natH: h})
	}
	done := make([]bool, len(items))
	for pass := 0; pass <= len(items); pass++ {
		progress := false
		for i := range items {
			if done[i] || !items[i].ae.a.ready(resolved) {
				continue
			}
			it := items[i]
			r := resolveAnchoredRect(ctx, it.ae, it.natW, it.natH, resolved, inner)
			it.ae.layout(ctx, r)
			if it.ae.id != "" {
				resolved[it.ae.id] = r
			}
			done[i] = true
			progress = true
		}
		if !progress {
			break
		}
	}
	// Anything still unresolved: place against parent (missing refs fall back).
	for i := range items {
		if done[i] {
			continue
		}
		it := items[i]
		r := resolveAnchoredRect(ctx, it.ae, it.natW, it.natH, resolved, inner)
		it.ae.layout(ctx, r)
		if it.ae.id != "" {
			resolved[it.ae.id] = r
		}
	}
}

// resolveAnchoredRect resolves one anchored item's box, giving a
// height-for-width child (a wrapping paragraph) its height at the width the
// anchors imposed. Without the second pass a `Label { anchors.left/right;
// wrapMode: WordWrap }` reported the height of ONE line however many it
// wrapped into, and every chat bubble sized off `label.height` came out a
// line short.
func resolveAnchoredRect(ctx *Context, ae *AnchorEl, natW, natH float32,
	resolved map[string]Rect, inner Rect) Rect {
	r := ae.a.resolve(natW, natH, resolved, inner)
	hfw, ok := ae.child.(heightForWidther)
	if !ok || r.W <= 0 {
		return r
	}
	if h := hfw.heightFor(ctx, r.W); h > 0 && h != natH {
		return ae.a.resolve(natW, h, resolved, inner)
	}
	return r
}

func (d *DivEl) paint(ctx *Context) {
	if ctx.culled(d) {
		return
	}
	// Transforms wrap the whole paint — background, borders, children —
	// exactly as QML transforms a whole item.
	if m, ok := d.xform(); ok {
		ctx.R.PushTransform(m)
		defer ctx.R.PopTransform()
		// Nothing under a transform is culled: its layout rects are not
		// where its pixels are (see damage.go).
		ctx.dmgLock++
		defer func() { ctx.dmgLock-- }()
	}
	// Push subtree opacity for the duration of this element's paint. Zero
	// is a real value — a faded-out overlay — not "unset", so the flag
	// decides, not the number.
	if d.hasOpacity {
		prev, prevInit := ctx.opacity, ctx.opacityInit
		ctx.opacity, ctx.opacityInit = d.opacity*ctx.alpha(), true
		defer func() { ctx.opacity, ctx.opacityInit = prev, prevInit }()
	}
	// CSS paints the first shadow of the list topmost, so emit in reverse.
	for i := len(d.shadows) - 1; i >= 0; i-- {
		sh := d.shadows[i]
		if sh.c.A <= 0 {
			continue
		}
		s := sh.spread
		ctx.R.Shadow(d.rect.X+sh.ox-s, d.rect.Y+sh.oy-s, d.rect.W+2*s, d.rect.H+2*s,
			max(d.radius+s, 0), sh.blur, ctx.color(sh.c))
	}
	bg, has := d.bg, d.hasBg
	hovered := d.rect.contains(ctx.MouseX, ctx.MouseY)
	pressed := hovered && ctx.MousePressed && d.rect.contains(ctx.pressX, ctx.pressY)
	if d.hasHover && hovered && !ctx.MousePressed {
		bg, has = d.hoverBg, true
	}
	if d.hasPress && pressed {
		bg, has = d.pressBg, true
	}
	switch {
	case d.hasGrad && !(d.hasHover && hovered) && !(d.hasPress && ctx.MousePressed && hovered):
		switch {
		case d.gradRadial:
			ctx.R.RadialGradientQuad(d.rect.X, d.rect.Y, d.rect.W, d.rect.H,
				ctx.color(d.gradC0), ctx.color(d.gradC1), d.radius, d.borderW, ctx.color(d.borderC))
		case len(d.gradStops) > 0:
			stops := make([]render.GradientStop, len(d.gradStops))
			for i, st := range d.gradStops {
				stops[i] = render.GradientStop{Pos: st.Pos, Col: ctx.color(st.Col)}
			}
			ctx.R.GradientQuadStopsN(d.rect.X, d.rect.Y, d.rect.W, d.rect.H,
				stops, d.gradAng, d.radius, d.borderW, ctx.color(d.borderC))
		case d.gradRamp != nil:
			ramp := *d.gradRamp
			for i := range ramp.Colors {
				ramp.Colors[i] = ctx.color(ramp.Colors[i])
			}
			ctx.R.GradientQuadRamp(d.rect.X, d.rect.Y, d.rect.W, d.rect.H,
				ramp, d.gradAng, d.radius, d.borderW, ctx.color(d.borderC))
		default:
			ctx.R.GradientQuadStops(d.rect.X, d.rect.Y, d.rect.W, d.rect.H,
				ctx.color(d.gradC0), ctx.color(d.gradC1), d.gradAng, d.radius, d.borderW, ctx.color(d.borderC),
				d.gradS0, d.gradS1)
		}
	case has || d.borderW > 0:
		if d.borderDashed && d.borderW > 0 {
			ctx.R.DashedQuad(d.rect.X, d.rect.Y, d.rect.W, d.rect.H, ctx.color(bg), d.radius, d.borderW, ctx.color(d.borderC))
		} else {
			ctx.R.Quad(d.rect.X, d.rect.Y, d.rect.W, d.rect.H, ctx.color(bg), d.radius, d.borderW, ctx.color(d.borderC))
		}
	}
	// Custom focus styling (GPUI .focus / .focus_visible): an inside border
	// painted over the background while this element holds focus.
	if d.focusSlot > 0 && d.focusSlot-1 == ctx.curFocus {
		if d.focusW > 0 {
			ctx.R.Quad(d.rect.X, d.rect.Y, d.rect.W, d.rect.H, RGBA{}, d.radius, d.focusW, ctx.color(d.focusC))
		}
		if d.focusVisW > 0 && ctx.focusVisible {
			ctx.R.Quad(d.rect.X, d.rect.Y, d.rect.W, d.rect.H, RGBA{}, d.radius, d.focusVisW, ctx.color(d.focusVisC))
		}
	}
	if d.hasSide {
		r := d.rect
		if w := d.sideW[SideTop]; w > 0 {
			ctx.R.FillRect(r.X, r.Y, r.W, w, ctx.color(d.sideC[SideTop]))
		}
		if w := d.sideW[SideBottom]; w > 0 {
			ctx.R.FillRect(r.X, r.Y+r.H-w, r.W, w, ctx.color(d.sideC[SideBottom]))
		}
		if w := d.sideW[SideLeft]; w > 0 {
			ctx.R.FillRect(r.X, r.Y, w, r.H, ctx.color(d.sideC[SideLeft]))
		}
		if w := d.sideW[SideRight]; w > 0 {
			ctx.R.FillRect(r.X+r.W-w, r.Y, w, r.H, ctx.color(d.sideC[SideRight]))
		}
	}
	if d.clip {
		// A rounded box clips its content to those corners, not to the
		// bounding rectangle: an image in a rounded card is cut by the
		// card, and one in a circle (radius = half the side) is round.
		ctx.R.PushClipRound(d.rect.X, d.rect.Y, d.rect.W, d.rect.H, d.radius)
	}
	for _, c := range d.children {
		c.paint(ctx)
	}
	if d.clip {
		ctx.R.PopClip()
	}
}

func (d *DivEl) bounds() Rect { return d.rect }

// paintBounds is where this box's OWN paint can land: its rect, plus its
// shadows, a focus border, and — under a transform — a box big enough
// for any rotation about its centre (see damage.go).
func (d *DivEl) paintBounds() Rect {
	r := d.rect
	for _, sh := range d.shadows {
		by := sh.spread + 3*sh.blur + max(abs32(sh.ox), abs32(sh.oy))
		r = unionRect(r, inflate(d.rect, by))
	}
	if d.focusSlot > 0 {
		r = unionRect(r, inflate(d.rect, 4))
	}
	if m, ok := d.xform(); ok {
		// The exact bounding box of the transformed rect: a 150x40 pill
		// scaled by 1.3 is a 195x52 box, not the 460x350 that inflating
		// by the diagonal gave — which was a quarter of the window for a
		// pulsing ring.
		x0, y0 := r.X, r.Y
		x1, y1 := r.X+r.W, r.Y+r.H
		bx0, by0 := float32(math.Inf(1)), float32(math.Inf(1))
		bx1, by1 := float32(math.Inf(-1)), float32(math.Inf(-1))
		for _, c := range [4][2]float32{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}} {
			px, py := m.Apply(c[0], c[1])
			bx0, by0 = min(bx0, px), min(by0, py)
			bx1, by1 = max(bx1, px), max(by1, py)
		}
		r = unionRect(r, Rect{bx0, by0, bx1 - bx0, by1 - by0})
	}
	return r
}

// paintSig hashes everything DivEl.paint reads from the box itself, so a
// rebuilt box that looks the same compares equal to last frame's.
func (d *DivEl) paintSig() uint64 {
	h := newSig(0x1d1)
	h.b(d.hasBg)
	h.col(d.bg)
	h.b(d.hasHover)
	h.col(d.hoverBg)
	h.b(d.hasPress)
	h.col(d.pressBg)
	h.b(d.hasGrad)
	h.b(d.gradRadial)
	h.col(d.gradC0)
	h.col(d.gradC1)
	h.f32(d.gradAng)
	h.f32(d.gradS0)
	h.f32(d.gradS1)
	h.ptr(unsafe.Pointer(d.gradRamp))
	for _, st := range d.gradStops {
		h.f32(st.Pos)
		h.col(st.Col)
	}
	h.f32(d.radius)
	h.f32(d.borderW)
	h.col(d.borderC)
	h.b(d.borderDashed)
	h.b(d.hasSide)
	for i := range d.sideW {
		h.f32(d.sideW[i])
		h.col(d.sideC[i])
	}
	for _, sh := range d.shadows {
		h.f32(sh.ox)
		h.f32(sh.oy)
		h.f32(sh.blur)
		h.f32(sh.spread)
		h.col(sh.c)
	}
	h.b(d.hasOpacity)
	h.f32(d.opacity)
	h.i(d.focusSlot)
	h.f32(d.focusW)
	h.col(d.focusC)
	h.f32(d.focusVisW)
	h.col(d.focusVisC)
	h.b(d.clip)
	if m, ok := d.xform(); ok {
		h.f32(m.A)
		h.f32(m.B)
		h.f32(m.C)
		h.f32(m.D)
		h.f32(m.E)
		h.f32(m.F)
	}
	return uint64(h)
}

// Bounds exposes the div's laid-out pixel rect (last frame) so apps can map
// window-space input events onto it (custom scrollbars, canvases).
func (d *DivEl) Bounds() Rect { return d.rect }

// IsRow reports whether this box lays its children out horizontally —
// callers that must match the layout axis (a QML Layout.fillWidth hint
// means "grow" along the main axis, "stretch" across it).
func (d *DivEl) IsRow() bool { return d.row }

func (d *DivEl) fixedWH() (float32, float32) { return d.w, d.h }

// xform composes this element's transform list into one affine, in QML's
// order (the last list entry applies first). Relative entries (rotation /
// scale with no explicit origin) act about the element's centre.
func (d *DivEl) xform() (render.Affine2, bool) {
	if len(d.xforms) == 0 {
		return render.Identity2(), false
	}
	cx, cy := d.rect.X+d.rect.W/2, d.rect.Y+d.rect.H/2
	// QML applies a transform list IN ORDER: the first entry acts on the
	// item, the next on that result. So each entry pre-multiplies.
	m := render.Identity2()
	for _, t := range d.xforms {
		m = t.affine(d.rect, cx, cy).Mul(m)
	}
	if m.IsIdentity() {
		return m, false
	}
	return m, true
}

func (d *DivEl) growFactor() float32 { return d.grow }

// GrowFactor, CrossFills and SelfAlign expose the layout hints an element
// carries, so a wrapper standing in for it (a margin box) can carry the
// same ones.
func (d *DivEl) GrowFactor() float32      { return d.grow }
func (d *DivEl) CrossFills() bool         { return d.crossFill }
func (d *DivEl) SelfAlign() (Align, bool) { return d.selfAl, d.hasSelfAl }
func (d *DivEl) shrinkFactor() float32    { return d.shrink }

func (d *DivEl) hit(x, y float32) Element {
	// A transformed element hit-tests in its own frame: map the pointer
	// back through the inverse before comparing with the laid-out rects
	// (which are pre-transform, since layout runs untransformed).
	if m, ok := d.xform(); ok {
		if inv, ok := m.Invert(); ok {
			x, y = inv.Apply(x, y)
		}
	}
	// Anchor-mode containers don't clip: their children are absolutely
	// positioned and commonly lie outside the container's own rect (a
	// component's root Item measures 0×0), so children must be consulted
	// regardless — QML items receive events outside parent bounds unless
	// clipped. Stacked containers keep the rect early-out.
	if !d.rect.contains(x, y) && !d.anchorEd {
		return nil
	}
	// Topmost child wins: walk in reverse paint order.
	for i := len(d.children) - 1; i >= 0; i-- {
		if h := d.children[i].hit(x, y); h != nil {
			return h
		}
	}
	if !d.rect.contains(x, y) {
		return nil
	}
	// hoverFn/pressFn deliberately do NOT make an element hit-testable:
	// they are passive per-paint state reporters (containsMouse/pressed),
	// driven from paint, and must not block clicks reaching elements
	// underneath (every gml box carries them).
	if d.click != nil || d.focusable || d.cursor != app.CursorDefault {
		return d
	}
	return nil
}

func (d *DivEl) isFocusable() bool { return d.focusable }

func (d *DivEl) onClick() func() { return d.click }
