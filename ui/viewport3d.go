package ui

import "github.com/sushvibez/gfx/render"

// Viewport3DEl embeds a live 3D view (a render.View3D offscreen target) in
// the 2D layout: it reserves a box like any element, sizes the target to its
// laid-out rect, and paints the rendered frame as an image — so a 3D scene
// clips, scrolls and stacks with normal widgets. The app renders the scene
// into the View3D between layout and the UI pass (see render.View3D).
//
// Input: the element is click-targetable; OnTap receives the tap position in
// viewport-local pixels — feed it to render.ScreenRay + Renderer3D.Pick for
// object picking. Rect() exposes the laid-out box so orbit-camera code can
// route drags/wheel that land inside the viewport.
// ViewportOverlay paints 2D graphics over a viewport's rendered frame
// (rect is the viewport's window-space box). CAD-style vector overlays —
// sketch ink, grids, snap markers — draw here with the 2D SDF renderer's
// anti-aliasing instead of as aliased 3D geometry.
type ViewportOverlay func(r *render.Renderer2D, rect Rect)

type Viewport3DEl struct {
	v            *render.View3D
	w, h         float32
	grow         bool
	onTap        func(x, y float32)
	overlay      ViewportOverlay
	lastX, lastY float32
	rect         Rect
}

// Viewport3D wraps a View3D as an element. Default size 320x240; use
// Size/Grow to place it.
func Viewport3D(v *render.View3D) *Viewport3DEl {
	return &Viewport3DEl{v: v, w: 320, h: 240}
}

func (e *Viewport3DEl) Size(w, h float32) *Viewport3DEl { e.w, e.h = w, h; return e }
func (e *Viewport3DEl) Grow() *Viewport3DEl             { e.grow = true; return e }

// OnTap installs a click handler given viewport-local pixel coordinates
// (the picking entry point).
func (e *Viewport3DEl) OnTap(fn func(x, y float32)) *Viewport3DEl { e.onTap = fn; return e }

// Overlay installs a 2D paint pass over the rendered frame (clipped to
// the viewport box, under later UI elements).
func (e *Viewport3DEl) Overlay(fn ViewportOverlay) *Viewport3DEl { e.overlay = fn; return e }

// Rect returns the last laid-out box in window pixels.
func (e *Viewport3DEl) Rect() Rect { return e.rect }

func (e *Viewport3DEl) measure(*Context) (float32, float32) { return e.w, e.h }

func (e *Viewport3DEl) layout(_ *Context, r Rect) {
	e.rect = r
	// Size the offscreen target to the laid-out box (idempotent; allocates
	// only when the size actually changes, e.g. on window resize).
	e.v.Resize(int(r.W), int(r.H))
}

// dirty: the 3D view renders itself every frame.
func (e *Viewport3DEl) dirty(*Context) bool { return true }

func (e *Viewport3DEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	if img := e.v.Image(); img != nil {
		ctx.R.DrawImage(img, e.rect.X, e.rect.Y, e.rect.W, e.rect.H, render.White)
	}
	if e.overlay != nil {
		ctx.R.PushClip(e.rect.X, e.rect.Y, e.rect.W, e.rect.H)
		e.overlay(ctx.R, e.rect)
		ctx.R.PopClip()
	}
}

func (e *Viewport3DEl) hit(x, y float32) Element {
	if !e.rect.contains(x, y) {
		return nil
	}
	// Remember where the pointer is in local coordinates; a click dispatch
	// (which re-hit-tests at the release point just before calling onClick)
	// hands them to OnTap.
	e.lastX, e.lastY = x-e.rect.X, y-e.rect.Y
	if e.onTap == nil {
		return nil
	}
	return e
}

func (e *Viewport3DEl) onClick() func() {
	if e.onTap == nil {
		return nil
	}
	return func() { e.onTap(e.lastX, e.lastY) }
}

func (e *Viewport3DEl) bounds() Rect { return e.rect }

func (e *Viewport3DEl) fixedWH() (float32, float32) {
	if e.grow {
		return 0, 0
	}
	return e.w, e.h
}

// growFactor lets the flex layout stretch a Grow()'n viewport (DivEl's
// grow contract).
func (e *Viewport3DEl) growFactor() float32 {
	if e.grow {
		return 1
	}
	return 0
}
