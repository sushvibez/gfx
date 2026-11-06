package ui

import "github.com/sushvibez/gfx/render"

// Canvas is an immediate-mode drawing leaf: it occupies a laid-out
// rectangle and calls draw with the renderer and that rect, so callers can
// emit arbitrary 2D primitives (paths, gradients, images) positioned
// within it. The escape hatch beneath the widget set.
type Canvas struct {
	w, h      float32
	grow      float32
	crossFill bool
	noClip    bool
	draw      func(r *render.Renderer2D, rect Rect)
	drawFont  func(r *render.Renderer2D, f *render.Font, rect Rect)
	// flowH, when set, gives the canvas's height for its width at
	// measure time — content laid out like text, whose height is only
	// known once it is measured with the renderer's fonts.
	flowH func(r *render.Renderer2D, f *render.Font, w float32) float32
	rect  Rect
}

// NewCanvas builds a fixed-size canvas; draw is called each paint with the
// canvas's pixel rect.
func NewCanvas(w, h float32, draw func(r *render.Renderer2D, rect Rect)) *Canvas {
	return &Canvas{w: w, h: h, draw: draw}
}

// NewCanvasFont is NewCanvas for draws that also render text: the callback
// receives the UI's default font (nil in fontless tests).
func NewCanvasFont(w, h float32, draw func(r *render.Renderer2D, f *render.Font, rect Rect)) *Canvas {
	return &Canvas{w: w, h: h, drawFont: draw}
}

// NewCanvasFlow is a canvas w wide whose height is worked out from that
// width when the canvas is measured: height is called with the renderer
// and the UI's default font (it is not called in fontless tests, where the
// canvas measures 0 tall). For content that flows like text — a Markdown
// page, a wrapped diagram — where the height depends on the width.
func NewCanvasFlow(w float32, height func(r *render.Renderer2D, f *render.Font, w float32) float32,
	draw func(r *render.Renderer2D, f *render.Font, rect Rect)) *Canvas {
	return &Canvas{w: w, flowH: height, drawFont: draw}
}

// Unclipped lets the draw callback paint outside the canvas rect — for
// vector content that is positioned by an item but not bounded by it
// (QtQuick Shapes render unclipped; a QML Canvas does not).
func (c *Canvas) Unclipped() *Canvas { c.noClip = true; return c }

// W and H override the size a canvas asks for — a layout item states it as
// Layout.preferredWidth/Height.
func (c *Canvas) W(px float32) *Canvas { c.w = px; return c }
func (c *Canvas) H(px float32) *Canvas { c.h = px; return c }

// Grow makes the canvas share the leftover space along its parent box's
// main axis (Layout.fill* on that axis), and CrossFill stretches it across
// the other one. Without these a QML Canvas in a layout sat at the 300x150
// an HTML canvas defaults to, however much room its cell had — the states
// page's easing plot came out half as wide and twice as tall as Qt's.
func (c *Canvas) Grow() *Canvas { c.grow = 1; return c }

// GrowBy is Grow with a WEIGHT: leftover space is shared between growing
// siblings in proportion to it (QML's stretch factor, which defaults to the
// item's preferred size).
func (c *Canvas) GrowBy(f float32) *Canvas { c.grow = f; return c }
func (c *Canvas) CrossFill() *Canvas       { c.crossFill = true; return c }

func (c *Canvas) growFactor() float32 { return c.grow }
func (c *Canvas) crossFills() bool    { return c.crossFill }

func (c *Canvas) measure(ctx *Context) (float32, float32) {
	if c.flowH != nil && ctx != nil && ctx.R != nil {
		c.h = c.flowH(ctx.R, ctx.Font, c.w)
	}
	return c.w, c.h
}
func (c *Canvas) layout(_ *Context, r Rect) { c.rect = r }

// dirty: a canvas runs its draw function every paint, and nothing here
// can tell what that function will draw.
func (c *Canvas) dirty(*Context) bool { return true }

func (c *Canvas) paint(ctx *Context) {
	if ctx.culled(c) {
		return
	}
	// Drawing is confined to the canvas's own rect — a QML Canvas paints
	// into a texture of its item size, so anything outside is not visible
	// there either, and a path that overruns its item must not bleed over
	// the surrounding UI.
	clip := !c.noClip && c.rect.W > 0 && c.rect.H > 0
	if clip {
		ctx.R.PushClip(c.rect.X, c.rect.Y, c.rect.W, c.rect.H)
	}
	if c.draw != nil {
		c.draw(ctx.R, c.rect)
	}
	if c.drawFont != nil {
		c.drawFont(ctx.R, ctx.Font, c.rect)
	}
	if clip {
		ctx.R.PopClip()
	}
}

// Bounds exposes the canvas's laid-out pixel rect (last frame) so apps can
// map window-space input events into canvas-local coordinates.
func (c *Canvas) Bounds() Rect { return c.rect }

func (c *Canvas) hit(float32, float32) Element { return nil }
func (c *Canvas) onClick() func()              { return nil }
func (c *Canvas) bounds() Rect                 { return c.rect }

func (c *Canvas) fixedWH() (float32, float32) { return c.w, c.h }
