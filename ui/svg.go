package ui

import (
	"unsafe"

	"github.com/sushvibez/gfx/render"
)

// SVGEl draws a parsed SVG (load once with render.ParseSVG, outside the
// per-frame view) into a fixed box, uniformly scaled to fit.
type SVGEl struct {
	svg      *render.SVG
	w, h     float32
	tint     RGBA
	colorize bool    // tint recolours by luminance instead of multiplying
	stretch  bool    // fillMode: Stretch — fill the box, ignoring aspect
	crop     bool    // fillMode: PreserveAspectCrop — cover it and clip
	angle    float32 // rotation around the center, radians
	rect     Rect
}

// SVG builds an SVG element of the given box size.
func SVG(svg *render.SVG, w, h float32) *SVGEl {
	return &SVGEl{svg: svg, w: w, h: h, tint: render.White}
}

// Tint multiplies the SVG's colors (e.g. for monochrome icon recoloring).
func (e *SVGEl) Tint(c RGBA) *SVGEl { e.tint = c; return e }

// Colorize recolours every shape to c at its own luminance — QML's
// `MultiEffect { colorization: 1; colorizationColor: c }`, which is how a
// Controls style tints an icon. Unlike Tint (a multiply) it works on a
// coloured source.
func (e *SVGEl) Colorize(c RGBA) *SVGEl { e.tint, e.colorize = c, true; return e }

// Stretch fills the box on both axes, ignoring the aspect ratio (QML's
// Image.Stretch); the default fits inside it.
func (e *SVGEl) Stretch() *SVGEl { e.stretch = true; return e }

// Crop covers the box and clips the overflow (Image.PreserveAspectCrop).
func (e *SVGEl) Crop() *SVGEl { e.crop = true; return e }

// Rotate spins the SVG around its center by angleRad (QML rotation).
func (e *SVGEl) Rotate(angleRad float32) *SVGEl { e.angle = angleRad; return e }

func (e *SVGEl) measure(*Context) (float32, float32) { return e.w, e.h }
func (e *SVGEl) layout(_ *Context, r Rect)           { e.rect = r }

// paintSig: the document, its box and its tint (see damage.go).
func (e *SVGEl) paintSig() uint64 {
	h := newSig(0x5a5)
	h.ptr(unsafe.Pointer(e.svg))
	h.f32(e.w)
	h.f32(e.h)
	h.col(e.tint)
	return uint64(h)
}

func (e *SVGEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	if e.svg == nil {
		return
	}
	sw, sh := e.svg.Size()
	if sw == 0 || sh == 0 {
		return
	}
	// The two axes scale together unless the fill mode says otherwise: fit
	// (the default) takes the smaller, crop the larger and clips, stretch
	// takes each axis on its own.
	sx, sy := min(e.rect.W/sw, e.rect.H/sh), min(e.rect.W/sw, e.rect.H/sh)
	switch {
	case e.stretch:
		sx, sy = e.rect.W/sw, e.rect.H/sh
	case e.crop:
		s := max(e.rect.W/sw, e.rect.H/sh)
		sx, sy = s, s
	}
	ox := e.rect.X + (e.rect.W-sw*sx)/2
	oy := e.rect.Y + (e.rect.H-sh*sy)/2
	if e.crop {
		ctx.R.PushClip(e.rect.X, e.rect.Y, e.rect.W, e.rect.H)
		defer ctx.R.PopClip()
	}
	if e.colorize {
		ctx.R.DrawSVGScaledColorized(e.svg, ox, oy, sx, sy, e.angle, ctx.color(e.tint))
		return
	}
	ctx.R.DrawSVGScaled(e.svg, ox, oy, sx, sy, e.angle, ctx.color(e.tint))
}
func (e *SVGEl) hit(float32, float32) Element { return nil }
func (e *SVGEl) onClick() func()              { return nil }
func (e *SVGEl) bounds() Rect                 { return e.rect }
func (e *SVGEl) fixedWH() (float32, float32)  { return e.w, e.h }
