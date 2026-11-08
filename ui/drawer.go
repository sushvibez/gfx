package ui

import (
	"time"

	"github.com/sushvibez/gfx/core"
)

// Drawers (QML's Drawer): a panel that slides in from an edge of the window
// over a dim scrim. Its position runs from 0 (closed) to 1 (open); Open and
// Close animate it, a drag on the panel toward its edge follows the pointer
// and closes it when let go past half-way, and a click on the scrim or
// Escape closes it.

// DrawerState is a drawer's retained state. Keep one across frames.
type DrawerState struct {
	pos    core.Property[float32] // 0 closed, 1 open
	target bool                   // open, or opening
}

// drawerSlide is how long a drawer takes to slide all the way.
const drawerSlide = 200 * time.Millisecond

// Open slides the drawer in, Close slides it out.
func (s *DrawerState) Open()  { s.slideTo(true) }
func (s *DrawerState) Close() { s.slideTo(false) }

// SetOpen opens or closes it; Toggle flips it.
func (s *DrawerState) SetOpen(open bool) { s.slideTo(open) }
func (s *DrawerState) Toggle()           { s.slideTo(!s.target) }

func (s *DrawerState) slideTo(open bool) {
	s.target = open
	to := float32(0)
	if open {
		to = 1
	}
	// The time left is the distance left, so a half-dragged drawer snaps
	// back as fast as it moves.
	d := s.pos.Get() - to
	if d < 0 {
		d = -d
	}
	core.Tween(&s.pos, to, time.Duration(float32(drawerSlide)*d), core.EaseOutCubic)
}

// SetPosition puts it at p (0..1) without animating — a drawer that starts
// open, or one a page drives itself.
func (s *DrawerState) SetPosition(p float32) {
	core.StopTween(&s.pos)
	s.pos.Set(clamp01(p))
	s.target = p > 0
}

// Position is how far open it is now; IsOpen whether it is open or opening.
func (s *DrawerState) Position() float32 { return s.pos.Get() }
func (s *DrawerState) IsOpen() bool      { return s.target }

// DrawerOpts configures a drawer.
type DrawerOpts struct {
	// Edge is the window edge it slides in from: EdgeLeft (the zero
	// value), EdgeRight, EdgeTop or EdgeBottom.
	Edge Edge
	// Size is the panel's extent across its edge (its width for a left or
	// right drawer); 0 is two thirds of the window.
	Size float32
	// Dark draws the panel for a dark page (DarkTheme's); NoDim leaves out
	// the scrim.
	Dark, NoDim bool
	Theme       *Theme // nil: the current one
	// Dim is the scrim's alpha when fully open; 0 is 0.45.
	Dim float32
	// DragMargin, when above 0, is how close to its edge a press has to be
	// for a drag to pull the closed drawer open (QML's dragMargin); 0 keeps
	// the edge free. OnOpened runs when such a drag opens it.
	DragMargin float32
	OnOpened   func()
	// OnClosed runs when the drawer finishes closing from a scrim click,
	// Escape or a drag (Close from code runs nothing).
	OnClosed func()
}

// Drawer draws content in a panel sliding in from o.Edge, while the drawer
// is open or moving. Closed, it takes no space.
func Drawer(st *DrawerState, content Element, o DrawerOpts) Element {
	p := st.pos.Get()
	if p <= 0 && !st.target {
		if o.DragMargin > 0 {
			return &drawerEdgeEl{st: st, o: o}
		}
		return Div()
	}
	tm := themeOr(o.Theme)
	if o.Dark && o.Theme == nil {
		tm = DarkTheme()
	}
	panel := Div().Col().Bg(tm.Panel)
	panel.Shadow(16, 0, 0, RGBA{A: 0.35})
	if content != nil {
		if c, ok := content.(*DivEl); ok {
			c.Grow()
		}
		panel.Children(content)
	}
	closeUser := func() {
		st.Close()
		if o.OnClosed != nil {
			o.OnClosed()
		}
	}
	d := &DrawerEl{panel: panel, st: st, o: o, dismiss: closeUser}
	// A drag toward the edge pulls the drawer shut with the pointer.
	start := float32(0)
	panel.OnDrag(func(dx, dy float32, phase DragPhase) {
		switch phase {
		case DragStart:
			core.StopTween(&st.pos)
			start = st.pos.Get()
		case DragMove, DragEnd:
			ext := d.extent
			if ext <= 0 {
				return
			}
			var moved float32
			switch o.Edge {
			case EdgeLeft:
				moved = -dx
			case EdgeRight:
				moved = dx
			case EdgeTop:
				moved = -dy
			case EdgeBottom:
				moved = dy
			}
			st.pos.Set(clamp01(start - moved/ext))
			if phase == DragEnd {
				if st.pos.Get() < 0.5 {
					closeUser()
				} else {
					st.Open()
				}
			}
		}
	})
	return d
}

// DrawerEl floats a drawer's panel at its window edge, offset by how far
// open it is, over a scrim that dims with it.
type DrawerEl struct {
	panel   *DivEl
	st      *DrawerState
	o       DrawerOpts
	dismiss func()
	extent  float32 // the panel's size across its edge, last layout
	rect    Rect
}

func (d *DrawerEl) measure(*Context) (float32, float32) { return 0, 0 }
func (d *DrawerEl) outOfFlow() bool                     { return true }

func (d *DrawerEl) layout(ctx *Context, _ Rect) {
	p := d.st.pos.Get()
	ext := drawerExtent(d.o, ctx.winW, ctx.winH)
	d.extent = ext
	dim := RGBA{}
	if !d.o.NoDim {
		a := d.o.Dim
		if a == 0 {
			a = 0.45
		}
		dim.A = a * p
	}
	ctx.overlays = append(ctx.overlays, &scrimEl{rect: Rect{0, 0, ctx.winW, ctx.winH}, dim: dim, dismiss: d.dismiss})
	shown := ext * p
	var r Rect
	switch d.o.Edge {
	case EdgeLeft:
		r = Rect{shown - ext, 0, ext, ctx.winH}
	case EdgeRight:
		r = Rect{ctx.winW - shown, 0, ext, ctx.winH}
	case EdgeTop:
		r = Rect{0, shown - ext, ctx.winW, ext}
	case EdgeBottom:
		r = Rect{0, ctx.winH - shown, ctx.winW, ext}
	}
	d.panel.W(r.W).H(r.H)
	d.rect = r
	d.panel.layout(ctx, r)
	ctx.overlays = append(ctx.overlays, d.panel)
}

func (d *DrawerEl) paint(*Context)               {}
func (d *DrawerEl) hit(float32, float32) Element { return nil }
func (d *DrawerEl) childElems() []Element        { return []Element{d.panel} }
func (d *DrawerEl) onClick() func()              { return nil }
func (d *DrawerEl) bounds() Rect                 { return d.rect }

// drawerExtent is the panel's size across its edge in a w×h window.
func drawerExtent(o DrawerOpts, w, h float32) float32 {
	if o.Size > 0 {
		return o.Size
	}
	if o.Edge == EdgeLeft || o.Edge == EdgeRight {
		return w * 2 / 3
	}
	return h * 2 / 3
}

// drawerEdgeEl is a closed drawer that a drag from its edge can open: it
// takes no space and draws nothing, and registers a strip along the edge as
// a drag region (not an overlay, so clicks at the edge still land).
type drawerEdgeEl struct {
	st   *DrawerState
	o    DrawerOpts
	rect Rect
}

func (d *drawerEdgeEl) measure(*Context) (float32, float32) { return 0, 0 }
func (d *drawerEdgeEl) outOfFlow() bool                     { return true }
func (d *drawerEdgeEl) layout(ctx *Context, _ Rect) {
	m := d.o.DragMargin
	switch d.o.Edge {
	case EdgeLeft:
		d.rect = Rect{0, 0, m, ctx.winH}
	case EdgeRight:
		d.rect = Rect{ctx.winW - m, 0, m, ctx.winH}
	case EdgeTop:
		d.rect = Rect{0, 0, ctx.winW, m}
	case EdgeBottom:
		d.rect = Rect{0, ctx.winH - m, ctx.winW, m}
	}
	ctx.drags = append(ctx.drags, &drawerEdgeDrag{d: d, ext: drawerExtent(d.o, ctx.winW, ctx.winH)})
}
func (d *drawerEdgeEl) paint(*Context)               {}
func (d *drawerEdgeEl) hit(float32, float32) Element { return nil }
func (d *drawerEdgeEl) childElems() []Element        { return nil }
func (d *drawerEdgeEl) onClick() func()              { return nil }
func (d *drawerEdgeEl) bounds() Rect                 { return d.rect }

// drawerEdgeDrag pulls the drawer out with the pointer; let go past
// half-way it opens, short of that it slides back.
type drawerEdgeDrag struct {
	d   *drawerEdgeEl
	ext float32
}

func (g *drawerEdgeDrag) contains(x, y float32) bool { return g.d.rect.contains(x, y) }
func (g *drawerEdgeDrag) drag(dx, dy float32, phase DragPhase) {
	st, o := g.d.st, g.d.o
	if phase == DragStart {
		core.StopTween(&st.pos)
		return
	}
	var in float32
	switch o.Edge {
	case EdgeLeft:
		in = dx
	case EdgeRight:
		in = -dx
	case EdgeTop:
		in = dy
	case EdgeBottom:
		in = -dy
	}
	if g.ext > 0 {
		st.pos.Set(clamp01(in / g.ext))
	}
	if phase == DragEnd {
		if st.pos.Get() >= 0.5 {
			st.Open()
			if o.OnOpened != nil {
				o.OnOpened()
			}
		} else {
			st.Close()
		}
	}
}
