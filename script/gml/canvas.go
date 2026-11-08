package gml

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// buildCanvas implements QML's Canvas: an onPaint handler draws via a 2D
// context (getContext("2d")) backed by the renderer. Supports the common
// HTML5-canvas subset — fillStyle/strokeStyle/lineWidth, fillRect/strokeRect,
// beginPath/moveTo/lineTo/rect/arc/closePath, fill/stroke, save/restore — which
// is enough for the GPUI painting / paths_bench / pattern examples.
func buildCanvas(n *node, env *Env, doc *docState) (ui.Element, error) {
	var w, h float32 = 300, 150
	for _, a := range n.attrs {
		switch a.name {
		case "id", "onPaint", "antialiasing", "renderTarget", "renderStrategy", "contextType":
			// id/onPaint handled below; the rest are accepted no-ops.
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			h = f
		case "onAvailableChanged":
			// Availability is immediate here (we repaint every frame).
		case "layer.enabled", "layer.effect", "layer.samples", "rotation",
			"transform", "transformOrigin", "antialiasing2",
			"scale", "opacity", "visible", "clip", "z", "smooth", "available",
			"tileSize", "canvasSize", "canvasWindow", "onPainted", "onImageLoaded":
			// accepted, not modeled (we repaint into the item's own rect)
		default:
			return nil, fmt.Errorf("gml:%d: Canvas has no attribute %q", a.line, a.name)
		}
	}
	paint := handlerFor(n, "onPaint")
	// Snapshot the build-time scope: paint runs LATER (paint phase), after
	// transient instance/row bindings are restored — the live chain would
	// resolve the wrong values (see Env.snapshot).
	snap := doc.scopeSnapshot(env, "canvas:"+doc.key(n))
	return ui.NewCanvasFont(w, h, func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
		if len(paint) == 0 {
			return
		}
		// A Canvas does not paint before its document has COMPLETED: Qt
		// runs Component.onCompleted before the first frame, and that is
		// where the corpus fills the arrays its onPaint indexes (an audio
		// visualiser's levels). Ours completes on the second build, and
		// since a frame builds once now, the first frame's paint would run
		// ahead of it — `levels[i] * h` with no levels yet.
		if !doc.completed {
			return
		}
		ctx := newContext2D(r, f, rect)
		scope := snap.child()
		scope.vars["getContext"] = jsFunc(func(args []any) (any, error) { return ctx, nil })
		scope.vars["width"] = float64(rect.W)
		scope.vars["height"] = float64(rect.H)
		if err := runStmts(paint, scope); err != nil {
			logf("gml: Canvas onPaint: %v", err)
		}
	}), nil
}

// context2D holds the mutable drawing state behind the JS ctx object.
type context2D struct {
	r          *render.Renderer2D
	font       *render.Font // UI default face, for fillText (nil in tests)
	ox, oy     float32      // canvas origin in framebuffer space
	m          affine       // current transform, canvas space -> framebuffer
	path       *render.Path
	subStarted bool
	fill       string
	fillGrad   *canvasGradient // non-nil when fillStyle is a gradient
	stroke     string
	lineWidth  float64
	fontSpec   string // CSS-ish "13px sans-serif": size, weight and slant
	// Canvas shadows: everything drawn while shadowColor is set gets a
	// blurred, offset copy underneath (QML resets them explicitly, as HTML
	// does).
	shadowCol              string
	shadowBlur             float64
	shadowOffX, shadowOffY float64
	textAlign              string
	textBase               string
	lineCap                string
	lineJoin               string
	alpha                  float64 // globalAlpha
	// clipPoly is the active clip region as a convex polygon in framebuffer
	// space (ctx.clip()). Fills are polygon-clipped against it; strokes and
	// text are not (see the clip() note).
	clipPoly []core.Vec2
	// dash is setLineDash's pattern (canvas pixels, alternating on/off) and
	// lineDashOffset's phase; empty means a solid stroke.
	dash       []float32
	dashOffset float64
	stack      []cstate
}

type cstate struct {
	fill, stroke string
	fillGrad     *canvasGradient
	lineWidth    float64
	fontSpec     string
	textAlign    string
	textBase     string
	lineCap      string
	lineJoin     string
	alpha        float64
	m            affine
	clipPoly     []core.Vec2
	dash         []float32
	dashOffset   float64
}

// affine is the canvas transform: (x,y) -> (a*x+c*y+e, b*x+d*y+f), the same
// six-value form as CanvasRenderingContext2D.setTransform. The identity
// transform carries the canvas origin, so every emitted point is already in
// framebuffer space.
type affine struct{ a, b, c, d, e, f float32 }

func (m affine) apply(x, y float32) (float32, float32) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

// mul returns n applied BEFORE m (canvas order: ctx.translate then ctx.scale
// scales in the translated frame).
func (m affine) mul(n affine) affine {
	return affine{
		a: m.a*n.a + m.c*n.b,
		b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d,
		d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e,
		f: m.b*n.e + m.d*n.f + m.f,
	}
}

// scaleMag is the transform's average scale, used to keep stroke widths and
// font sizes proportional under ctx.scale().
func (m affine) scaleMag() float32 {
	sx := float32(math.Hypot(float64(m.a), float64(m.b)))
	sy := float32(math.Hypot(float64(m.c), float64(m.d)))
	if sx <= 0 || sy <= 0 {
		return 1
	}
	return (sx + sy) / 2
}

// canvasGradient is a CanvasGradient: an axis plus color stops. The path
// renderer supports a 2-color linear gradient, so fill() uses the first and
// last stops.
type canvasGradient struct {
	x0, y0, x1, y1 float32
	stops          []gradStop
	// A radial gradient keeps its circles, since colour by DISTANCE is not
	// something a linear ramp can express (see fillPath).
	radial         bool
	cx, cy, r0, r1 float32
}

type gradStop struct {
	off float32
	col ui.RGBA
}

// fillPath fills a path with this gradient: every stop counts, so a sky
// ramped purple → pink → orange → gold paints all four.
func (g *canvasGradient) fillPath(r *render.Renderer2D, p *render.Path, tf affine, rule render.FillRule) {
	if g.radial && len(g.stops) > 0 {
		// A ring fan around the gradient's own circle, CLIPPED TO THE PATH:
		// the fan paints the whole circle, and a gradient circle is
		// routinely bigger than the shape it fills (a cloud is a small
		// ellipse painted from a radial ramp whose radius is a fraction of
		// the layer's width — without the clip the ramp itself became the
		// cloud, a soft disc many times too big).
		cx, cy := tf.apply(g.cx, g.cy)
		scale := tf.scaleMag()
		stops := g.renderStops()
		r.PushClipPath(p, rule)
		r.FillRadialRing(cx, cy, g.r0*scale, g.r1*scale, stops)
		r.PopClip()
		return
	}
	// A canvas gradient is placed by its own two points and holds its end
	// colours beyond them — it is not stretched over the shape, which is
	// what a QML gradient does. The PFD's sky runs from -2r to the
	// horizon, so only its second half is ever visible.
	x0, y0 := tf.apply(g.x0, g.y0)
	x1, y1 := tf.apply(g.x1, g.y1)
	r.FillPathRampAxis(p, x0, y0, x1, y1, g.renderStops(), rule)
}

// renderStops converts the stop list for the renderer.
func (g *canvasGradient) renderStops() []render.GradientStop {
	out := make([]render.GradientStop, len(g.stops))
	for i, st := range g.stops {
		out[i] = render.GradientStop{Pos: st.off, Col: st.col}
	}
	return out
}

// angle is the gradient axis in the canvas' current transform.
func (g *canvasGradient) angle(tf affine) float32 {
	x0, y0 := tf.apply(g.x0, g.y0)
	x1, y1 := tf.apply(g.x1, g.y1)
	return float32(math.Atan2(float64(y1-y0), float64(x1-x0)))
}

func (c *context2D) col(s string) ui.RGBA {
	rgba, err := parseColor(s)
	if err != nil {
		rgba = ui.RGBA{A: 1}
	}
	if c.alpha > 0 && c.alpha < 1 {
		rgba.A *= float32(c.alpha)
	}
	return rgba
}

// pt maps a canvas-space point through the current transform.
func (c *context2D) pt(x, y float32) (float32, float32) { return c.m.apply(x, y) }

// lw is the stroke width in framebuffer space.
func (c *context2D) lw() float32 { return float32(c.lineWidth) * c.m.scaleMag() }

// strokeCurrent strokes the built path with the current cap style (round
// joins/caps when lineCap says so, which is Qt's default for "round").
func (c *context2D) strokeCurrent() {
	// setLineDash cuts the path into its on-runs first: the pattern is in
	// canvas units, so it scales with the transform like the geometry does.
	p := c.path
	if len(c.dash) > 0 {
		scale := c.m.scaleMag()
		pat := make([]float32, len(c.dash))
		for i, d := range c.dash {
			pat[i] = d * scale
		}
		p = render.DashPath(p, pat, float32(c.dashOffset)*scale)
	}
	c.drawPathShadow(p, render.NonZero, c.lw())
	if c.lineCap == "round" || c.lineJoin == "round" || c.lineCap == "" {
		c.r.StrokePathRound(p, c.lw(), c.col(c.stroke))
		return
	}
	c.r.StrokePath(p, c.lw(), c.col(c.stroke))
}

// dashPatternOf reads setLineDash's array argument (a JS array of lengths).
func dashPatternOf(a []any) []float32 {
	if len(a) == 0 {
		return nil
	}
	arr, ok := a[0].([]any)
	if !ok {
		return nil
	}
	out := make([]float32, 0, len(arr))
	for _, v := range arr {
		f, err := toFloat32(v)
		if err != nil {
			return nil
		}
		out = append(out, f)
	}
	return out
}

// fillClipped fills a path, first clipping it to the active clip region.
// The rule is the canvas default (non-zero) unless fill() named the other
// one, matching HTML's fill("nonzero"|"evenodd").
func (c *context2D) fillClipped(p *render.Path, rule render.FillRule) {
	if len(c.clipPoly) >= 3 {
		p = clipPathTo(p, c.clipPoly)
	}
	c.drawPathShadow(p, rule, 0)
	if g := c.fillGrad; g != nil {
		g.fillPath(c.r, p, c.m, rule)
		return
	}
	c.r.FillPathRule(p, c.col(c.fill), rule)
}

// canvasFillRule reads fill()/clip()'s optional rule argument. The canvas
// default is non-zero — the opposite of a QML ShapePath's.
func canvasFillRule(a []any) render.FillRule {
	if len(a) > 0 && containsFold(str(a[0]), "even") {
		return render.EvenOdd
	}
	return render.NonZero
}

// longestSub returns a path's largest subpath as a polygon — the clip
// region a `beginPath; arc(...); clip()` sequence describes.
func longestSub(p *render.Path) []core.Vec2 {
	var best []core.Vec2
	for _, sub := range p.Polyline() {
		if len(sub) > len(best) {
			best = sub
		}
	}
	return best
}

// intersectClip narrows an existing clip region by a new one (QML's clip()
// intersects, it does not replace).
func intersectClip(cur, next []core.Vec2) []core.Vec2 {
	if len(next) < 3 {
		return cur
	}
	if len(cur) < 3 {
		return append([]core.Vec2{}, next...)
	}
	return clipPolygon(cur, next)
}

// clipPathTo clips every subpath of p against a convex clip polygon.
func clipPathTo(p *render.Path, clip []core.Vec2) *render.Path {
	out := render.NewPath()
	for _, sub := range p.Polyline() {
		poly := clipPolygon(sub, clip)
		if len(poly) < 3 {
			continue
		}
		out.MoveTo(poly[0].X, poly[0].Y)
		for _, pt := range poly[1:] {
			out.LineTo(pt.X, pt.Y)
		}
		out.Close()
	}
	return out
}

// clipPolygon is Sutherland-Hodgman: clip `subject` by each edge of the
// convex polygon `clip`.
func clipPolygon(subject, clip []core.Vec2) []core.Vec2 {
	if len(subject) < 3 || len(clip) < 3 {
		return nil
	}
	// Edge orientation: use the clip polygon's signed area so the "inside"
	// test works whichever way it was wound.
	area := float32(0)
	for i := range clip {
		j := (i + 1) % len(clip)
		area += clip[i].X*clip[j].Y - clip[j].X*clip[i].Y
	}
	sign := float32(1)
	if area < 0 {
		sign = -1
	}
	inside := func(p, a, b core.Vec2) bool {
		return sign*((b.X-a.X)*(p.Y-a.Y)-(b.Y-a.Y)*(p.X-a.X)) >= 0
	}
	intersect := func(p, q, a, b core.Vec2) core.Vec2 {
		d1x, d1y := q.X-p.X, q.Y-p.Y
		d2x, d2y := b.X-a.X, b.Y-a.Y
		den := d1x*d2y - d1y*d2x
		if den == 0 {
			return q
		}
		t := ((a.X-p.X)*d2y - (a.Y-p.Y)*d2x) / den
		return core.Vec2{X: p.X + d1x*t, Y: p.Y + d1y*t}
	}
	out := append([]core.Vec2{}, subject...)
	for i := range clip {
		if len(out) == 0 {
			return nil
		}
		a, b := clip[i], clip[(i+1)%len(clip)]
		// A near-degenerate edge (the closing segment of a sampled circle,
		// whose last point lands a hair off the first) carries no real
		// half-plane: its cross product is tiny but SIGNED, so testing
		// against it would throw the whole polygon away.
		if dx, dy := b.X-a.X, b.Y-a.Y; dx*dx+dy*dy < 1e-6 {
			continue
		}
		in := out
		out = out[:0:0]
		for j := range in {
			cur := in[j]
			prev := in[(j+len(in)-1)%len(in)]
			curIn, prevIn := inside(cur, a, b), inside(prev, a, b)
			switch {
			case curIn && prevIn:
				out = append(out, cur)
			case curIn && !prevIn:
				out = append(out, intersect(prev, cur, a, b), cur)
			case !curIn && prevIn:
				out = append(out, intersect(prev, cur, a, b))
			}
		}
	}
	// Clipping a corner against two edges of the clip polygon lands the
	// same point twice, which gives the triangulator a zero-area ear to
	// chew on — and an ear clipper that meets one stops early, leaving a
	// wedge of the shape unfilled (the PFD's artificial horizon lost a
	// slice right across its middle).
	return dedupeRing(out)
}

// dedupeRing drops consecutive points that land on top of each other,
// including across the ring's closing edge.
func dedupeRing(pts []core.Vec2) []core.Vec2 {
	const eps = 1e-4
	same := func(a, b core.Vec2) bool {
		dx, dy := a.X-b.X, a.Y-b.Y
		return dx*dx+dy*dy < eps
	}
	out := pts[:0:0]
	for i, p := range pts {
		if i > 0 && same(out[len(out)-1], p) {
			continue
		}
		out = append(out, p)
	}
	for len(out) > 1 && same(out[0], out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	return out
}

// newContext2D returns the JS-visible ctx: a map of Ref-backed style props and
// jsFunc drawing methods, all closing over one shared context2D.
func newContext2D(r *render.Renderer2D, f *render.Font, rect ui.Rect) map[string]any {
	c := &context2D{r: r, font: f, ox: rect.X, oy: rect.Y, lineWidth: 1, fill: "#000000", stroke: "#000000",
		m: affine{a: 1, d: 1, e: rect.X, f: rect.Y}, alpha: 1}
	ensure := func() {
		if c.path == nil {
			c.path = render.NewPath()
		}
	}
	m := map[string]any{
		"fillStyle":    fillStyleRef{c},
		"strokeStyle":  strFieldRef{&c.stroke},
		"lineWidth":    numFieldRef{&c.lineWidth},
		"font":         strFieldRef{&c.fontSpec},
		"textAlign":    strFieldRef{&c.textAlign},
		"textBaseline": strFieldRef{&c.textBase},
		"lineCap":      strFieldRef{&c.lineCap},
		"lineJoin":     strFieldRef{&c.lineJoin},
		"globalAlpha":  numFieldRef{&c.alpha},
		"miterLimit":   numFieldRef{new(float64)}, // accepted, not modeled
		"fillText": jsFunc(func(a []any) (any, error) {
			if c.font == nil {
				return nil, nil
			}
			text := str2(a, 0)
			x, y := num(a, 1), num(a, 2)
			f, synthBold := c.faceFor(c.fontSpec)
			w, _ := c.r.MeasureText(f, text)
			switch c.textAlign {
			case "center":
				x -= w / 2
			case "right", "end":
				x -= w
			}
			// Canvas y is the BASELINE by default; DrawText wants the line
			// top, and textBaseline moves the anchor within the line box.
			top := y - f.Ascent()
			switch c.textBase {
			case "top", "hanging":
				top = y
			case "middle":
				top = y - f.Ascent()/2
			case "bottom", "ideographic":
				top = y - f.Height()
			}
			px, py := c.pt(x, top)
			col := c.col(c.fill)
			c.drawTextShadow(f, px, py, text)
			c.r.DrawText(f, px, py, text, col)
			if synthBold {
				for _, o := range synthBoldOffsets(f.Size()) {
					c.r.DrawText(f, px+o[0], py+o[1], text, col)
				}
			}
			return nil, nil
		}),
		"measureText": jsFunc(func(a []any) (any, error) {
			if c.font == nil {
				return map[string]any{"width": float64(0)}, nil
			}
			f, _ := c.faceFor(c.fontSpec)
			w, _ := c.r.MeasureText(f, str2(a, 0))
			return map[string]any{"width": float64(w)}, nil
		}),
		// createRadialGradient: the batcher has one linear-gradient
		// primitive, so a radial ramp paints along the circle's axis
		// (inner center -> outer edge), keeping the stop colors.
		"createRadialGradient": jsFunc(func(a []any) (any, error) {
			r0 := num(a, 2)
			cx, cy, r1 := num(a, 3), num(a, 4), num(a, 5)
			g := &canvasGradient{
				x0: cx - r1, y0: cy - r1, x1: cx + r1, y1: cy + r1,
				radial: true, cx: cx, cy: cy, r0: r0, r1: r1,
			}
			gm := map[string]any{"__grad": g}
			gm["addColorStop"] = jsFunc(func(b []any) (any, error) {
				g.stops = append(g.stops, gradStop{off: num(b, 0), col: c.col(str2(b, 1))})
				return nil, nil
			})
			return gm, nil
		}),
		"createLinearGradient": jsFunc(func(a []any) (any, error) {
			g := &canvasGradient{x0: num(a, 0), y0: num(a, 1), x1: num(a, 2), y1: num(a, 3)}
			gm := map[string]any{"__grad": g}
			gm["addColorStop"] = jsFunc(func(b []any) (any, error) {
				g.stops = append(g.stops, gradStop{off: num(b, 0), col: c.col(str2(b, 1))})
				return nil, nil
			})
			return gm, nil
		}),
		"fillRect": jsFunc(func(a []any) (any, error) {
			x, y, w, h := num(a, 0), num(a, 1), num(a, 2), num(a, 3)
			p := render.NewPath()
			x0, y0 := c.pt(x, y)
			x1, y1 := c.pt(x+w, y)
			x2, y2 := c.pt(x+w, y+h)
			x3, y3 := c.pt(x, y+h)
			p.MoveTo(x0, y0).LineTo(x1, y1).LineTo(x2, y2).LineTo(x3, y3).Close()
			c.fillClipped(p, render.NonZero)
			return nil, nil
		}),
		"strokeRect": jsFunc(func(a []any) (any, error) {
			x, y, w, h := num(a, 0), num(a, 1), num(a, 2), num(a, 3)
			save := c.path
			c.path = render.NewPath()
			c.pathRect(x, y, w, h)
			c.strokeCurrent()
			c.path = save
			return nil, nil
		}),
		"clearRect": jsFunc(func(a []any) (any, error) { return nil, nil }),
		"beginPath": jsFunc(func(a []any) (any, error) { c.path = render.NewPath(); return nil, nil }),
		"closePath": jsFunc(func(a []any) (any, error) {
			ensure()
			c.path.Close()
			return nil, nil
		}),
		"moveTo": jsFunc(func(a []any) (any, error) {
			ensure()
			c.path.MoveTo(c.pt(num(a, 0), num(a, 1)))
			return nil, nil
		}),
		"lineTo": jsFunc(func(a []any) (any, error) {
			ensure()
			c.path.LineTo(c.pt(num(a, 0), num(a, 1)))
			return nil, nil
		}),
		"rect": jsFunc(func(a []any) (any, error) {
			ensure()
			x, y, w, h := num(a, 0), num(a, 1), num(a, 2), num(a, 3)
			c.pathRect(x, y, w, h)
			return nil, nil
		}),
		"arc": jsFunc(func(a []any) (any, error) {
			ensure()
			cx, cy, rad := num(a, 0), num(a, 1), num(a, 2)
			a0, a1 := float64(num(a, 3)), float64(num(a, 4))
			c.arcTo(cx, cy, rad, rad, a0, a1)
			return nil, nil
		}),
		"bezierCurveTo": jsFunc(func(a []any) (any, error) {
			ensure()
			x1, y1 := c.pt(num(a, 0), num(a, 1))
			x2, y2 := c.pt(num(a, 2), num(a, 3))
			x3, y3 := c.pt(num(a, 4), num(a, 5))
			c.path.CubicTo(x1, y1, x2, y2, x3, y3)
			return nil, nil
		}),
		"quadraticCurveTo": jsFunc(func(a []any) (any, error) {
			ensure()
			// promote quadratic to cubic (control points at 2/3 toward the quad ctrl)
			cx, cy := c.pt(num(a, 0), num(a, 1))
			ex, ey := c.pt(num(a, 2), num(a, 3))
			c.path.CubicTo(cx, cy, cx, cy, ex, ey)
			return nil, nil
		}),
		"fill": jsFunc(func(a []any) (any, error) {
			ensure()
			c.fillClipped(c.path, canvasFillRule(a))
			return nil, nil
		}),
		"stroke": jsFunc(func(a []any) (any, error) {
			ensure()
			c.strokeCurrent()
			return nil, nil
		}),
		"save": jsFunc(func(a []any) (any, error) {
			c.stack = append(c.stack, cstate{c.fill, c.stroke, c.fillGrad, c.lineWidth,
				c.fontSpec, c.textAlign, c.textBase, c.lineCap, c.lineJoin, c.alpha,
				c.m, c.clipPoly, c.dash, c.dashOffset})
			return nil, nil
		}),
		"restore": jsFunc(func(a []any) (any, error) {
			if n := len(c.stack); n > 0 {
				s := c.stack[n-1]
				c.stack = c.stack[:n-1]
				c.fill, c.stroke, c.fillGrad, c.lineWidth = s.fill, s.stroke, s.fillGrad, s.lineWidth
				c.fontSpec, c.textAlign = s.fontSpec, s.textAlign
				c.textBase, c.lineCap, c.lineJoin, c.alpha, c.m = s.textBase, s.lineCap, s.lineJoin, s.alpha, s.m
				c.clipPoly = s.clipPoly
				c.dash, c.dashOffset = s.dash, s.dashOffset
			}
			return nil, nil
		}),
		// Transform stack (QML Context2D mirrors the HTML canvas API).
		"translate": jsFunc(func(a []any) (any, error) {
			c.m = c.m.mul(affine{a: 1, d: 1, e: num(a, 0), f: num(a, 1)})
			return nil, nil
		}),
		"rotate": jsFunc(func(a []any) (any, error) {
			ang := float64(num(a, 0))
			sin, cos := float32(math.Sin(ang)), float32(math.Cos(ang))
			c.m = c.m.mul(affine{a: cos, b: sin, c: -sin, d: cos})
			return nil, nil
		}),
		"scale": jsFunc(func(a []any) (any, error) {
			c.m = c.m.mul(affine{a: num(a, 0), d: num(a, 1)})
			return nil, nil
		}),
		"transform": jsFunc(func(a []any) (any, error) {
			c.m = c.m.mul(affine{num(a, 0), num(a, 1), num(a, 2), num(a, 3), num(a, 4), num(a, 5)})
			return nil, nil
		}),
		"setTransform": jsFunc(func(a []any) (any, error) {
			base := affine{a: 1, d: 1, e: c.ox, f: c.oy}
			c.m = base.mul(affine{num(a, 0), num(a, 1), num(a, 2), num(a, 3), num(a, 4), num(a, 5)})
			return nil, nil
		}),
		"resetTransform": jsFunc(func(a []any) (any, error) {
			c.m = affine{a: 1, d: 1, e: c.ox, f: c.oy}
			return nil, nil
		}),
		// reset() clears path, styles AND transform (QML Context2D.reset).
		"reset": jsFunc(func(a []any) (any, error) {
			c.path = render.NewPath()
			c.fill, c.stroke, c.fillGrad, c.lineWidth = "#000000", "#000000", nil, 1
			c.shadowCol, c.shadowBlur, c.shadowOffX, c.shadowOffY = "", 0, 0, 0
			c.fontSpec, c.textAlign, c.textBase = "", "", ""
			c.lineCap, c.lineJoin, c.alpha = "", "", 1
			c.m = affine{a: 1, d: 1, e: c.ox, f: c.oy}
			c.stack = c.stack[:0]
			return nil, nil
		}),
		// Clipping is not modeled: the 2D batcher has no scissor per path, so
		// clip() keeps the path as-is and draws unclipped (documented no-op).
		// clip(): the current path becomes the clip region. Fills are
		// polygon-clipped against it (Sutherland-Hodgman, so a convex
		// region — a circle, a rounded box — is exact and a concave one is
		// approximated by its convex use). Strokes and text are left
		// unclipped: this is what an instrument panel needs, where the
		// clipped thing is the painted sky/ground.
		"clip": jsFunc(func(a []any) (any, error) {
			ensure()
			c.clipPoly = intersectClip(c.clipPoly, longestSub(c.path))
			return nil, nil
		}),
		// Dash patterns and shadows are not modeled by the path batcher:
		// accepted so a page that dashes a line still draws it solid.
		"setLineDash": jsFunc(func(a []any) (any, error) {
			c.dash = dashPatternOf(a)
			return nil, nil
		}),
		"getLineDash": jsFunc(func(a []any) (any, error) {
			out := make([]any, len(c.dash))
			for i, d := range c.dash {
				out[i] = float64(d)
			}
			return out, nil
		}),
		"shadowColor":              strFieldRef{&c.shadowCol},
		"shadowBlur":               numFieldRef{&c.shadowBlur},
		"shadowOffsetX":            numFieldRef{&c.shadowOffX},
		"shadowOffsetY":            numFieldRef{&c.shadowOffY},
		"lineDashOffset":           numFieldRef{&c.dashOffset},
		"globalCompositeOperation": strFieldRef{new(string)},
		// Qt's Context2D takes a BOUNDING BOX here — ellipse(x, y, w, h) —
		// where the HTML canvas takes a centre and two radii. A page
		// written HTML-style (the corpus' clouds pass seven arguments)
		// therefore draws to the lower right of the point it names, at a
		// quarter of the size, and Qt ignores the extra arguments; ours
		// centred it and doubled it.
		"ellipse": jsFunc(func(a []any) (any, error) {
			ensure()
			x, y, w, h := num(a, 0), num(a, 1), num(a, 2), num(a, 3)
			c.arcTo(x+w/2, y+h/2, w/2, h/2, 0, 2*math.Pi)
			return nil, nil
		}),
		// Qt's Context2D extension: a rounded rectangle subpath.
		"roundedRect": jsFunc(func(a []any) (any, error) {
			ensure()
			c.pathRoundedRect(num(a, 0), num(a, 1), num(a, 2), num(a, 3), num(a, 4), num(a, 5))
			return nil, nil
		}),
	}
	return m
}

// pathRect appends a rectangle subpath in canvas space.
func (c *context2D) pathRect(x, y, w, h float32) {
	if c.path == nil {
		c.path = render.NewPath()
	}
	x0, y0 := c.pt(x, y)
	x1, y1 := c.pt(x+w, y)
	x2, y2 := c.pt(x+w, y+h)
	x3, y3 := c.pt(x, y+h)
	c.path.MoveTo(x0, y0).LineTo(x1, y1).LineTo(x2, y2).LineTo(x3, y3).Close()
}

// pathRoundedRect appends Qt's roundedRect subpath (corner radii rx, ry).
func (c *context2D) pathRoundedRect(x, y, w, h, rx, ry float32) {
	if rx <= 0 && ry <= 0 {
		c.pathRect(x, y, w, h)
		return
	}
	if rx <= 0 {
		rx = ry
	}
	if ry <= 0 {
		ry = rx
	}
	if rx > w/2 {
		rx = w / 2
	}
	if ry > h/2 {
		ry = h / 2
	}
	// Corner arcs, clockwise from the top-left, each a quarter ellipse.
	c.moveToPt(x+rx, y)
	c.lineToPt(x+w-rx, y)
	c.arcTo(x+w-rx, y+ry, rx, ry, -math.Pi/2, 0)
	c.lineToPt(x+w, y+h-ry)
	c.arcTo(x+w-rx, y+h-ry, rx, ry, 0, math.Pi/2)
	c.lineToPt(x+rx, y+h)
	c.arcTo(x+rx, y+h-ry, rx, ry, math.Pi/2, math.Pi)
	c.lineToPt(x, y+ry)
	c.arcTo(x+rx, y+ry, rx, ry, math.Pi, 3*math.Pi/2)
	c.path.Close()
}

func (c *context2D) moveToPt(x, y float32) {
	if c.path == nil {
		c.path = render.NewPath()
	}
	c.path.MoveTo(c.pt(x, y))
}

func (c *context2D) lineToPt(x, y float32) {
	if c.path == nil {
		c.path = render.NewPath()
	}
	c.path.LineTo(c.pt(x, y))
}

// arcTo appends an elliptical arc as line segments, continuing the current
// subpath (canvas arc() semantics: it connects from the current point).
func (c *context2D) arcTo(cx, cy, rx, ry float32, a0, a1 float64) {
	if c.path == nil {
		c.path = render.NewPath()
	}
	segs := 32
	if span := math.Abs(a1 - a0); span < math.Pi/2 {
		segs = 8
	}
	for i := 0; i <= segs; i++ {
		ang := a0 + (a1-a0)*float64(i)/float64(segs)
		c.lineToPt(cx+rx*float32(math.Cos(ang)), cy+ry*float32(math.Sin(ang)))
	}
}

// shadowTaps are the offsets a canvas shadow is approximated with: a ring
// at the blur radius plus the centre, each at a fraction of the alpha, which
// reads as a soft halo the way a real gaussian would. QML applies the
// shadow to EVERYTHING drawn while it is set, so both text and paths go
// through here.
func (c *context2D) shadowSpec() (ui.RGBA, float32, float32, bool) {
	if c.shadowCol == "" {
		return ui.RGBA{}, 0, 0, false
	}
	col := c.col(c.shadowCol)
	if col.A <= 0 {
		return ui.RGBA{}, 0, 0, false
	}
	blur := float32(c.shadowBlur)
	ox, oy := float32(c.shadowOffX), float32(c.shadowOffY)
	if blur <= 0 && ox == 0 && oy == 0 {
		return ui.RGBA{}, 0, 0, false
	}
	return col, blur, 0, true
}

// drawTextShadow paints the blurred, offset copy of a run of text that the
// active shadow* state asks for, under the text itself.
func (c *context2D) drawTextShadow(f *render.Font, x, y float32, text string) {
	col, blur, _, ok := c.shadowSpec()
	if !ok || f == nil {
		return
	}
	ox, oy := float32(c.shadowOffX), float32(c.shadowOffY)
	if blur <= 0 {
		c.r.DrawText(f, x+ox, y+oy, text, col)
		return
	}
	// A ring of low-alpha copies at the blur radius, plus a stronger
	// centre: the same cheap halo a glowing Text uses.
	ring := col
	ring.A *= 0.16
	r := blur / 2
	for _, o := range [...][2]float32{{r, 0}, {-r, 0}, {0, r}, {0, -r},
		{r * 0.7, r * 0.7}, {-r * 0.7, r * 0.7}, {r * 0.7, -r * 0.7}, {-r * 0.7, -r * 0.7}} {
		c.r.DrawText(f, x+ox+o[0], y+oy+o[1], text, ring)
	}
	mid := col
	mid.A *= 0.35
	c.r.DrawText(f, x+ox, y+oy, text, mid)
}

// drawPathShadow is drawTextShadow for a filled or stroked path.
func (c *context2D) drawPathShadow(p *render.Path, rule render.FillRule, strokeW float32) {
	col, blur, _, ok := c.shadowSpec()
	if !ok || p == nil {
		return
	}
	ox, oy := float32(c.shadowOffX), float32(c.shadowOffY)
	paint := func(dx, dy float32, col ui.RGBA) {
		q := p.Translated(ox+dx, oy+dy)
		if strokeW > 0 {
			c.r.StrokePath(q, strokeW, col)
			return
		}
		c.r.FillPathRule(q, col, rule)
	}
	if blur <= 0 {
		paint(0, 0, col)
		return
	}
	ring := col
	ring.A *= 0.16
	r := blur / 2
	for _, o := range [...][2]float32{{r, 0}, {-r, 0}, {0, r}, {0, -r},
		{r * 0.7, r * 0.7}, {-r * 0.7, r * 0.7}, {r * 0.7, -r * 0.7}, {-r * 0.7, -r * 0.7}} {
		paint(o[0], o[1], ring)
	}
	mid := col
	mid.A *= 0.35
	paint(0, 0, mid)
}

// faceFor resolves the face a canvas font spec asks for: its size, and its
// WEIGHT and slant, which the corpus states 29 times over
// (`ctx.font = "bold 22px sans-serif"` — a clock's hour numerals, a chart's
// centre label, this page's "Canvas!"). Only the size used to be read, so
// every one of them came out in the regular face. The second result is true
// when no real bold face exists and the caller must over-strike instead.
func (c *context2D) faceFor(spec string) (*render.Font, bool) {
	f := c.font
	if f == nil {
		return nil, false
	}
	if size := cssFontSize(spec); size > 0 {
		f = f.AtSize(size)
	}
	bold, italic := cssFontBold(spec), cssFontItalic(spec)
	if !bold && !italic {
		return f, false
	}
	style := render.StyleRegular
	if bold {
		style |= render.StyleBold
	}
	if italic {
		style |= render.StyleItalic
	}
	if c.r != nil {
		// The family the spec names is a CSS list ("bold 22px sans-serif");
		// the registry knows our own names, and the default family is the
		// one a page means by sans-serif.
		if sf := c.r.NamedFontStyled(cssFontFamily(spec), style, f.Size()); sf != nil {
			return sf, false
		}
		if sf := c.r.NamedFontStyled("", style, f.Size()); sf != nil {
			return sf, false
		}
	}
	return f, bold // no styled face: over-strike for the weight
}

// cssFontBold reports a bold weight in a CSS-ish font spec ("bold", or a
// numeric weight of 600 and up).
func cssFontBold(spec string) bool {
	for _, tok := range strings.Fields(strings.ToLower(spec)) {
		switch tok {
		case "bold", "bolder", "600", "700", "800", "900":
			return true
		}
	}
	return false
}

// cssFontItalic reports a slanted style in a CSS-ish font spec.
func cssFontItalic(spec string) bool {
	for _, tok := range strings.Fields(strings.ToLower(spec)) {
		if tok == "italic" || tok == "oblique" {
			return true
		}
	}
	return false
}

// cssFontFamily is the family a spec names: whatever follows the size, with
// the generic CSS names mapped onto ours ("sans-serif" is the default face,
// which the registry keys as the empty family).
func cssFontFamily(spec string) string {
	toks := strings.Fields(spec)
	for i, tok := range toks {
		if !strings.HasSuffix(tok, "px") && !strings.HasSuffix(tok, "pt") {
			continue
		}
		fam := strings.Trim(strings.Join(toks[i+1:], " "), `"'`)
		if i := strings.IndexByte(fam, ','); i >= 0 {
			fam = strings.TrimSpace(fam[:i])
		}
		switch strings.ToLower(fam) {
		case "sans-serif", "sans", "system-ui", "":
			return ""
		}
		return fam
	}
	return ""
}

// synthBoldPass over-strikes text to fake a bold face, the way the browser
// does when a family has no bold: the offset scales with the size, since a
// real bold carries about 1.5x the ink of its regular.
func synthBoldOffsets(size float64) [][2]float32 {
	d := float32(size) / 26
	if d < 0.35 {
		d = 0.35
	}
	return [][2]float32{{d, 0}, {d / 2, 0}}
}

// cssFontSize extracts the pixel size from a CSS-ish font spec
// ("13px sans-serif", "bold 20px monospace"); 0 when absent.
func cssFontSize(spec string) float64 {
	for _, tok := range strings.Fields(spec) {
		if strings.HasSuffix(tok, "px") {
			if f, err := strconv.ParseFloat(strings.TrimSuffix(tok, "px"), 64); err == nil {
				return f
			}
		}
	}
	return 0
}

// num reads arg i as a float32 (0 when missing/non-numeric).
func num(args []any, i int) float32 {
	if i < len(args) {
		if f, err := toFloat64(args[i]); err == nil {
			return float32(f)
		}
	}
	return 0
}

// str2 reads arg i as a string ("" when missing).
func str2(args []any, i int) string {
	if i < len(args) {
		return str(args[i])
	}
	return ""
}

// fillStyleRef backs ctx.fillStyle: assigning a string sets a solid color,
// assigning a createLinearGradient() result sets a gradient.
type fillStyleRef struct{ c *context2D }

func (r fillStyleRef) get() any {
	if r.c.fillGrad != nil {
		return r.c.fillGrad
	}
	return r.c.fill
}
func (r fillStyleRef) set(v any) error {
	if m, ok := v.(map[string]any); ok {
		if g, ok := m["__grad"].(*canvasGradient); ok {
			r.c.fillGrad = g
			return nil
		}
	}
	r.c.fill = str(v)
	r.c.fillGrad = nil
	return nil
}
func (r fillStyleRef) animate(any, time.Duration) error { return nil }
func (r fillStyleRef) onChange(func())                  {}

// strFieldRef / numFieldRef are Refs backed by a Go pointer, so JS member
// assignment (ctx.fillStyle = "...") writes straight into the context state.
type strFieldRef struct{ p *string }

func (r strFieldRef) get() any                         { return *r.p }
func (r strFieldRef) set(v any) error                  { *r.p = str(v); return nil }
func (r strFieldRef) animate(any, time.Duration) error { return nil }
func (r strFieldRef) onChange(func())                  {}

type numFieldRef struct{ p *float64 }

func (r numFieldRef) get() any { return *r.p }
func (r numFieldRef) set(v any) error {
	f, err := toFloat64(v)
	if err == nil {
		*r.p = f
	}
	return err
}
func (r numFieldRef) animate(any, time.Duration) error { return nil }
func (r numFieldRef) onChange(func())                  {}
