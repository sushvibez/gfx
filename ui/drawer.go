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
	// Dark draws the panel for a dark page; NoDim leaves out the scrim.
	Dark, NoDim bool
	// OnClosed runs when the drawer finishes closing from a scrim click,
	// Escape or a drag (Close from code runs nothing).
	OnClosed func()
}

// Drawer draws content in a panel sliding in from o.Edge, while the drawer
// is open or moving. Closed, it takes no space.
func Drawer(st *DrawerState, content Element, o DrawerOpts) Element {
	p := st.pos.Get()
	if p <= 0 && !st.target {
		return Div()
	}
	panel := Div().Col()
	if o.Dark {
		panel.Bg(RGB(0x2a2d35))
	} else {
		panel.Bg(RGB(0xffffff))
	}
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
	horiz := d.o.Edge == EdgeLeft || d.o.Edge == EdgeRight
	ext := d.o.Size
	if ext <= 0 {
		if horiz {
			ext = ctx.winW * 2 / 3
		} else {
			ext = ctx.winH * 2 / 3
		}
	}
	d.extent = ext
	dim := RGBA{}
	if !d.o.NoDim {
		dim.A = 0.45 * p
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
