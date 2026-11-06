package gml

// QtQuick.Shapes: `Shape { ShapePath { ... PathLine/PathArc/... } }` —
// declarative vector paths, the counterpart to Canvas's imperative
// context2D. A Shape is an item of its own width/height whose ShapePath
// children are evaluated into render.Paths at paint time and filled /
// stroked in the item's rect (paths are authored in item-local
// coordinates, so they translate by the rect origin).
//
// Modeled: ShapePath strokeColor/strokeWidth/fillColor/capStyle,
// startX/startY, PathMove/PathLine/PathQuad/PathCubic/PathArc/PathSvg/
// PathAngleArc, and fillGradient (Linear/Radial/Conical) approximated by
// the batcher's 2-stop linear gradient — the same degradation Canvas's
// createLinearGradient takes. fillRule is honored (OddEvenFill, the
// default, and WindingFill). Not modeled: dash patterns, per-path
// transforms.

import (
	"fmt"
	"math"
	"strings"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// shapePathSpec is one evaluated ShapePath: its geometry plus paint.
type shapePathSpec struct {
	path        *render.Path
	fill        ui.RGBA
	hasFill     bool
	grad        *shapeGradient
	stroke      ui.RGBA
	strokeWidth float32
	roundCap    bool
	rule        render.FillRule
	// dash is the stroke's pattern in PEN WIDTHS (Qt's unit), applied to
	// the geometry at paint time; nil strokes solid.
	dash       []float32
	dashOffset float32
}

// shapeGradient is a ShapePath.fillGradient: an axis plus stops. Radial and
// conical gradients keep their end colors but paint along the item's
// diagonal, since the batcher has one linear-gradient primitive.
type shapeGradient struct {
	x1, y1, x2, y2 float32
	c0, c1         ui.RGBA
	// Every stop, in order — a QML gradient may have any number of them
	// and the middle ones carry the shape of the ramp (the corpus' radial
	// sphere is white → amber → orange; keeping only the ends painted a
	// flat wash of pale peach).
	stops []render.GradientStop
	// A RADIAL gradient runs from the focal circle to the centre one, so
	// colour goes by DISTANCE rather than along an axis.
	radial                 bool
	cx, cy, r1, fx, fy, r0 float32
	ok                     bool
}

func buildShape(n *node, env *Env, doc *docState) (ui.Element, error) {
	var w, h float32
	fill := false
	for _, a := range n.attrs {
		if a.name == "anchors.fill" {
			fill = true
		}
		switch a.name {
		case "id", "containsMode", "preferredRendererType", "asynchronous",
			"vendorExtensionsEnabled", "status", "opacity", "visible", "scale",
			"rotation", "z", "clip", "smooth", "antialiasing", "layer.enabled",
			"layer.effect", "layer.samples", "transformOrigin":
			// accepted, not modeled
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
		default:
			if _, attached := attachedSizeAttr(a.name); attached || strings.HasPrefix(a.name, "anchors.") {
				continue // consumed by the parent's layout pass
			}
			return nil, fmt.Errorf("gml:%d: Shape has no attribute %q", a.line, a.name)
		}
	}
	// Evaluate the paths at BUILD time (bindings live in the build scope);
	// painting only replays the geometry.
	var specs []shapePathSpec
	for _, c := range n.children {
		switch c.typeName {
		case "ShapePath":
			sp, err := buildShapePath(c, env, w, h)
			if err != nil {
				return nil, err
			}
			if sp.path != nil {
				specs = append(specs, sp)
			}
		default:
			// Animations and other non-visual children are handled by the
			// generic child pass; anything else is not part of a Shape.
			if isAnimation(c.typeName) {
				doc.refreshAnimator(c, env)
				continue
			}
			logf("gml:%d: Shape child %q ignored", c.line, c.typeName)
		}
	}
	// `Shape { anchors.fill: parent }` declares no size of its own: the
	// anchor layout hands the real rect to the element at layout time, and
	// paint uses that rect, so only the gradient axis needs the parent's
	// size up front (previous frame's bounds, like other self-geometry).
	if fill && (w == 0 || h == 0) {
		if pw, ph, ok := parentSize(env); ok {
			w, h = pw, ph
		}
	}
	if len(specs) == 0 {
		return ui.Div(), nil
	}
	// Unclipped: a QtQuick Shape's paths may extend past the item's rect
	// (unlike a Canvas, which paints into a texture of its own size).
	return unclipped(ui.NewCanvasFont(w, h, func(r *render.Renderer2D, _ *render.Font, rect ui.Rect) {
		for _, sp := range specs {
			p := sp.path.Translated(rect.X, rect.Y)
			switch {
			case sp.grad != nil && sp.grad.ok:
				g := sp.grad
				switch {
				case g.radial:
					// Beyond the outer circle a radial gradient PADS with
					// its last stop (Qt and SVG both), so the shape is
					// filled with that colour first: the corpus' lit
					// sphere is a circle of radius 35 whose gradient is
					// centred on the FOCAL point 14px off-centre, and the
					// far crescent had nothing painted in it at all.
					// The fan itself paints the whole gradient circle, so
					// it is clipped to the shape — a gradient circle is
					// routinely bigger than the path it fills.
					r.FillPathRule(p, g.stops[len(g.stops)-1].Col, sp.rule)
					r.PushClipPath(p, sp.rule)
					r.FillRadialRing(rect.X+g.fx, rect.Y+g.fy, g.r0, g.r1, g.stops)
					r.PopClip()
				case len(g.stops) > 2:
					r.FillPathRampAxis(p, rect.X+g.x1, rect.Y+g.y1,
						rect.X+g.x2, rect.Y+g.y2, g.stops, sp.rule)
				default:
					ang := float32(math.Atan2(float64(g.y2-g.y1), float64(g.x2-g.x1)))
					r.FillPathGradientRule(p, g.c0, g.c1, ang, sp.rule)
				}
			case sp.hasFill && sp.fill.A > 0:
				r.FillPathRule(p, sp.fill, sp.rule)
			}
			if sp.strokeWidth > 0 && sp.stroke.A > 0 {
				if len(sp.dash) > 0 {
					// Qt measures the pattern in pen widths.
					pat := make([]float32, len(sp.dash))
					for i, d := range sp.dash {
						pat[i] = d * sp.strokeWidth
					}
					r.StrokePathDashed(p, sp.strokeWidth, sp.stroke, pat,
						sp.dashOffset*sp.strokeWidth, sp.roundCap)
				} else if sp.roundCap {
					r.StrokePathRound(p, sp.strokeWidth, sp.stroke)
				} else {
					r.StrokePath(p, sp.strokeWidth, sp.stroke)
				}
			}
		}
	})), nil
}

// unclipped marks a canvas element as painting beyond its own rect.
func unclipped(c *ui.Canvas) ui.Element { return c.Unclipped() }

// buildShapePath evaluates one ShapePath element: its paint attributes and
// its path-element children, in item-local coordinates.
func buildShapePath(n *node, env *Env, w, h float32) (shapePathSpec, error) {
	sp := shapePathSpec{strokeWidth: 1, stroke: ui.RGB(0xffffff), roundCap: true}
	var startX, startY float32
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "strokeColor":
			c, err := attrColor(a, env)
			if err != nil {
				// "transparent" and friends: no stroke.
				sp.strokeWidth = 0
				continue
			}
			sp.stroke = c
		case "fillColor":
			c, err := attrColor(a, env)
			if err != nil {
				continue
			}
			sp.fill, sp.hasFill = c, true
		case "strokeWidth":
			f, err := attrFloat(a, env)
			if err != nil {
				return sp, err
			}
			sp.strokeWidth = f
		case "startX":
			f, err := attrFloat(a, env)
			if err != nil {
				return sp, err
			}
			startX = f
		case "startY":
			f, err := attrFloat(a, env)
			if err != nil {
				return sp, err
			}
			startY = f
		case "capStyle":
			if v, err := a.expr.eval(env); err == nil {
				// ShapePath.FlatCap(0)/SquareCap(16)/RoundCap(32)
				if f, err := toFloat64(v); err == nil {
					sp.roundCap = int(f) == 32
				}
			}
		case "fillRule":
			// ShapePath.OddEvenFill (0, the default) / WindingFill (1):
			// which side of a self-crossing outline is inside.
			if v, err := a.expr.eval(env); err == nil {
				if f, err := toFloat64(v); err == nil && int(f) == 1 {
					sp.rule = render.NonZero
				} else if containsFold(str(v), "Winding") {
					sp.rule = render.NonZero
				}
			}
		case "strokeStyle":
			// Qt::PenStyle values: SolidLine(1), DashLine(2), DotLine(3),
			// DashDotLine(4), DashDotDotLine(5). Each has a stock pattern,
			// which an explicit dashPattern then overrides.
			if v, err := a.expr.eval(env); err == nil {
				sp.dash = penStylePattern(v)
			}
		case "dashPattern":
			if v, err := a.expr.eval(env); err == nil {
				if pat := numberList(v); len(pat) > 0 {
					sp.dash = pat
				}
			}
		case "dashOffset":
			if f, err := attrFloat(a, env); err == nil {
				sp.dashOffset = f
			}
		case "joinStyle",
			"pathHints", "scale", "miterLimit":
			// accepted, not modeled
		case "fillGradient":
			// An element-valued attribute (a.elem, as the parser keeps
			// `fillGradient: LinearGradient { ... }`).
			if a.elem != nil {
				sp.grad = shapeGradientOf(a.elem, env, w, h)
			} else if ee, ok := a.expr.(*elemExpr); ok {
				sp.grad = shapeGradientOf(ee.n, env, w, h)
			}
		default:
			return sp, fmt.Errorf("gml:%d: ShapePath has no attribute %q", a.line, a.name)
		}
	}
	p := render.NewPath()
	p.MoveTo(startX, startY)
	cx, cy := startX, startY
	for _, c := range n.children {
		switch c.typeName {
		case "PathMove":
			x, y := pathXY(c, env, cx, cy)
			p.MoveTo(x, y)
			cx, cy = x, y
		case "PathLine":
			x, y := pathXY(c, env, cx, cy)
			p.LineTo(x, y)
			cx, cy = x, y
		case "PathQuad":
			x, y := pathXY(c, env, cx, cy)
			qx := pathNum(c, env, "controlX", cx)
			qy := pathNum(c, env, "controlY", cy)
			// Quadratic promoted to cubic: controls at 2/3 toward the quad point.
			p.CubicTo(cx+2.0/3*(qx-cx), cy+2.0/3*(qy-cy),
				x+2.0/3*(qx-x), y+2.0/3*(qy-y), x, y)
			cx, cy = x, y
		case "PathCubic":
			x, y := pathXY(c, env, cx, cy)
			p.CubicTo(pathNum(c, env, "control1X", cx), pathNum(c, env, "control1Y", cy),
				pathNum(c, env, "control2X", x), pathNum(c, env, "control2Y", y), x, y)
			cx, cy = x, y
		case "PathArc":
			x, y := pathXY(c, env, cx, cy)
			rx := pathNum(c, env, "radiusX", 0)
			ry := pathNum(c, env, "radiusY", rx)
			large := pathBool(c, env, "useLargeArc")
			// direction: PathArc.Clockwise(0) is Qt's default.
			ccw := false
			if v, ok := pathAttr(c, "direction"); ok {
				if val, err := v.eval(env); err == nil {
					if f, err := toFloat64(val); err == nil {
						ccw = int(f) == 1 // Counterclockwise
					}
				}
			}
			arcSegments(p, cx, cy, x, y, rx, ry, large, ccw)
			cx, cy = x, y
		case "PathAngleArc":
			ax := pathNum(c, env, "centerX", 0)
			ay := pathNum(c, env, "centerY", 0)
			rx := pathNum(c, env, "radiusX", 0)
			ry := pathNum(c, env, "radiusY", rx)
			a0 := float64(pathNum(c, env, "startAngle", 0)) * math.Pi / 180
			sweep := float64(pathNum(c, env, "sweepAngle", 0)) * math.Pi / 180
			// QML's moveToStart defaults to TRUE: the arc starts a new
			// subpath rather than being joined to wherever the path
			// happened to be, which drew a radial line into the centre and
			// turned a 270-degree arc into a pac-man wedge.
			if pathBoolDefault(c, env, "moveToStart", true) {
				p.MoveTo(ax+rx*float32(math.Cos(a0)), ay+ry*float32(math.Sin(a0)))
			}
			segs := 48
			for i := 0; i <= segs; i++ {
				ang := a0 + sweep*float64(i)/float64(segs)
				p.LineTo(ax+rx*float32(math.Cos(ang)), ay+ry*float32(math.Sin(ang)))
			}
			cx = ax + rx*float32(math.Cos(a0+sweep))
			cy = ay + ry*float32(math.Sin(a0+sweep))
		case "PathSvg":
			d := ""
			if e, ok := pathAttr(c, "path"); ok {
				if v, err := e.eval(env); err == nil {
					d = str(v)
				}
			}
			if sub := render.ParsePathData(d); sub != nil {
				// The parser returns its own Path; fold it in by replaying
				// nothing — instead paint it as a separate spec would, so
				// keep it simple: SVG data replaces this path's geometry.
				p = sub
			}
		case "PathPolyline":
			// A `path: [Qt.point(...), ...]` list of points.
			if e, ok := pathAttr(c, "path"); ok {
				if v, err := e.eval(env); err == nil {
					if items, ok := v.([]any); ok {
						for i, it := range items {
							x, y := pointOf(it)
							if i == 0 {
								p.MoveTo(x, y)
							} else {
								p.LineTo(x, y)
							}
							cx, cy = x, y
						}
					}
				}
			}
		default:
			logf("gml:%d: ShapePath child %q ignored", c.line, c.typeName)
		}
	}
	// A FILL closes the outline by itself (a polygon fill always joins the
	// last point to the first), so the path is closed here only when the
	// markup itself comes back to where it started. Closing it for the
	// fill's sake also STROKED the closing chord, which turned the shapes
	// page's 270° PathAngleArc from Qt's open C into a teardrop with a
	// diagonal across it.
	if pathReturnsToStart(p) {
		p.Close()
	}
	sp.path = p
	return sp, nil
}

// pathReturnsToStart reports whether the path's last subpath ends where it
// began — the markup's own way of closing a shape (a star's last PathLine
// going back to startX/startY). Stroking such a path closed is what keeps
// the join at that point sharp.
func pathReturnsToStart(p *render.Path) bool {
	subs := p.Polyline()
	if len(subs) == 0 {
		return false
	}
	last := subs[len(subs)-1]
	if len(last) < 3 {
		return false
	}
	a, b := last[0], last[len(last)-1]
	const eps = 0.5 // half a pixel: the same point, in effect
	return abs32(a.X-b.X) <= eps && abs32(a.Y-b.Y) <= eps
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// shapeGradientOf evaluates a Linear/Radial/ConicalGradient element into the
// axis + end colors the batcher can paint.
func shapeGradientOf(n *node, env *Env, w, h float32) *shapeGradient {
	g := &shapeGradient{}
	switch n.typeName {
	case "LinearGradient":
		g.x1 = pathNum(n, env, "x1", 0)
		g.y1 = pathNum(n, env, "y1", 0)
		g.x2 = pathNum(n, env, "x2", w)
		g.y2 = pathNum(n, env, "y2", h)
	case "RadialGradient":
		// QML's ramp runs from the FOCAL circle (t=0) to the centre circle
		// (t=1); the focal point offset from the centre is what makes a
		// lit sphere. The defaults are Qt's: the item's middle, and a
		// radius that reaches its shorter side.
		g.radial = true
		g.cx = pathNum(n, env, "centerX", w/2)
		g.cy = pathNum(n, env, "centerY", h/2)
		g.r1 = pathNum(n, env, "centerRadius", min(w, h)/2)
		g.fx = pathNum(n, env, "focalX", g.cx)
		g.fy = pathNum(n, env, "focalY", g.cy)
		g.r0 = pathNum(n, env, "focalRadius", 0)
	case "ConicalGradient":
		// No conical primitive: paint along the item diagonal, which keeps
		// the stop colors and the general direction of the ramp.
		g.x1, g.y1, g.x2, g.y2 = 0, 0, w, h
	default:
		return nil
	}
	var stops []ui.RGBA
	for i, c := range n.children {
		if c.typeName != "GradientStop" {
			continue
		}
		col, pos := ui.RGBA{}, float32(i)
		hasCol := false
		for _, a := range c.attrs {
			switch a.name {
			case "color":
				if v, err := attrColor(a, env); err == nil {
					col, hasCol = v, true
				}
			case "position":
				if f, err := attrFloat(a, env); err == nil {
					pos = f
				}
			}
		}
		if !hasCol {
			continue
		}
		stops = append(stops, col)
		g.stops = append(g.stops, render.GradientStop{Pos: pos, Col: col})
	}
	if len(stops) == 0 {
		return nil
	}
	g.c0, g.c1 = stops[0], stops[len(stops)-1]
	g.ok = true
	return g
}

// pathAttr finds a named attribute on a path element.
func pathAttr(n *node, name string) (expr, bool) {
	for _, a := range n.attrs {
		if a.name == name {
			return a.expr, true
		}
	}
	return nil, false
}

// pathNum reads a numeric path attribute, falling back to def.
func pathNum(n *node, env *Env, name string, def float32) float32 {
	e, ok := pathAttr(n, name)
	if !ok {
		return def
	}
	v, err := e.eval(env)
	if err != nil {
		return def
	}
	f, err := toFloat32(v)
	if err != nil {
		return def
	}
	return f
}

// pathBool reads a boolean path attribute (false when absent).
// pathBoolDefault is pathBool with the QML default for an absent attribute.
func pathBoolDefault(n *node, env *Env, name string, def bool) bool {
	if _, ok := pathAttr(n, name); !ok {
		return def
	}
	return pathBool(n, env, name)
}

func pathBool(n *node, env *Env, name string) bool {
	e, ok := pathAttr(n, name)
	if !ok {
		return false
	}
	v, err := e.eval(env)
	if err != nil {
		return false
	}
	b, _ := toBool(v)
	return b
}

// pathXY reads a path element's target point, defaulting to the current one.
func pathXY(n *node, env *Env, cx, cy float32) (float32, float32) {
	return pathNum(n, env, "x", cx), pathNum(n, env, "y", cy)
}

// pointOf reads a Qt.point()-style object as x/y.
func pointOf(v any) (float32, float32) {
	if m, ok := v.(map[string]any); ok {
		x, _ := toFloat32(m["x"])
		y, _ := toFloat32(m["y"])
		return x, y
	}
	return 0, 0
}

// arcSegments appends an SVG-style elliptical arc from (x0,y0) to (x1,y1)
// with radii (rx,ry), flattened to line segments (endpoint parameterization,
// SVG implementation notes F.6).
func arcSegments(p *render.Path, x0, y0, x1, y1, rx, ry float32, largeArc, ccw bool) {
	if rx == 0 || ry == 0 || (x0 == x1 && y0 == y1) {
		p.LineTo(x1, y1)
		return
	}
	rx, ry = float32(math.Abs(float64(rx))), float32(math.Abs(float64(ry)))
	// Midpoint in the ellipse's unit space.
	dx2, dy2 := float64(x0-x1)/2, float64(y0-y1)/2
	// Scale the radii up if they cannot span the chord.
	lam := (dx2*dx2)/float64(rx*rx) + (dy2*dy2)/float64(ry*ry)
	if lam > 1 {
		s := float32(math.Sqrt(lam))
		rx, ry = rx*s, ry*s
	}
	rxf, ryf := float64(rx), float64(ry)
	num := rxf*rxf*ryf*ryf - rxf*rxf*dy2*dy2 - ryf*ryf*dx2*dx2
	den := rxf*rxf*dy2*dy2 + ryf*ryf*dx2*dx2
	coef := 0.0
	if den > 0 && num > 0 {
		coef = math.Sqrt(num / den)
	}
	// SVG's centre sign: positive when the large-arc and sweep flags
	// differ. Our `ccw` is the negation of SVG's sweep flag, so the two
	// agreeing here means the flags differ.
	if largeArc != ccw {
		coef = -coef
	}
	cxp := coef * rxf * dy2 / ryf
	cyp := -coef * ryf * dx2 / rxf
	cx := cxp + float64(x0+x1)/2
	cy := cyp + float64(y0+y1)/2
	ang := func(x, y float64) float64 { return math.Atan2((y-cy)/ryf, (x-cx)/rxf) }
	a0 := ang(float64(x0), float64(y0))
	a1 := ang(float64(x1), float64(y1))
	sweep := a1 - a0
	if !ccw && sweep < 0 {
		sweep += 2 * math.Pi
	}
	if ccw && sweep > 0 {
		sweep -= 2 * math.Pi
	}
	segs := 12 + int(math.Abs(sweep)/(math.Pi/16))
	for i := 1; i <= segs; i++ {
		a := a0 + sweep*float64(i)/float64(segs)
		p.LineTo(float32(cx+rxf*math.Cos(a)), float32(cy+ryf*math.Sin(a)))
	}
}

// parentSize reads the enclosing item's previous-frame size from scope
// (`parent` is bound to a live elemObject by buildBox).
func parentSize(env *Env) (float32, float32, bool) {
	v, ok := env.lookup("parent")
	if !ok {
		return 0, 0, false
	}
	eo, ok := v.(*elemObject)
	if !ok || eo.el == nil {
		return 0, 0, false
	}
	b := ui.Bounds(eo.el)
	if b.W == 0 || b.H == 0 {
		return 0, 0, false
	}
	return b.W, b.H, true
}

// penStylePattern is the stock dash pattern of a Qt::PenStyle value, in pen
// widths; nil for SolidLine (and for anything unrecognized, which strokes
// solid rather than vanishing).
func penStylePattern(v any) []float32 {
	style := 1
	if f, err := toFloat64(v); err == nil {
		style = int(f)
	} else {
		switch s := str(v); {
		case containsFold(s, "DashDotDot"):
			style = 5
		case containsFold(s, "DashDot"):
			style = 4
		case containsFold(s, "Dot"):
			style = 3
		case containsFold(s, "Dash"):
			style = 2
		}
	}
	switch style {
	case 2: // DashLine
		return []float32{4, 2}
	case 3: // DotLine
		return []float32{1, 2}
	case 4: // DashDotLine
		return []float32{4, 2, 1, 2}
	case 5: // DashDotDotLine
		return []float32{4, 2, 1, 2, 1, 2}
	}
	return nil
}

// numberList reads a JS array of numbers (`dashPattern: [4, 2]`).
func numberList(v any) []float32 {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]float32, 0, len(arr))
	for _, e := range arr {
		f, err := toFloat32(e)
		if err != nil {
			return nil
		}
		out = append(out, f)
	}
	return out
}
