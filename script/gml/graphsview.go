package gml

// QtGraphs' 2D charts: GraphsView with LineSeries, ScatterSeries,
// SplineSeries, AreaSeries and BarSeries/BarSet over ValueAxis or
// BarCategoryAxis. The view draws the plot itself — grid, axes, then each
// series — and series point data is retained per element so the QML
// mutators (append, replace, removeMultiple, clear) work from handlers.
//
// Not modeled: GraphsTheme beyond transparent backgrounds, axis labels and
// titles, legends, and interactive zoom/pan.

import (
	"fmt"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// seriesData is one series' retained points, in data coordinates.
type seriesData struct {
	pts []point
}

type point struct{ x, y float32 }

// axisSpec is a resolved axis: a numeric range, or categories for a bar
// chart (which imply the range 0..len).
type axisSpec struct {
	min, max   float32
	tick       float32
	categories []string
	grid       bool
}

// span is the axis range, guarded against a zero divisor.
func (a axisSpec) span() float32 {
	if a.max > a.min {
		return a.max - a.min
	}
	return 1
}

// buildGraphsView renders a GraphsView element.
func buildGraphsView(n *node, env *Env, doc *docState) (ui.Element, error) {
	var w, h float32
	axisX := axisSpec{min: 0, max: 1, grid: true}
	axisY := axisSpec{min: 0, max: 1, grid: true}
	for _, a := range n.attrs {
		switch a.name {
		case "id", "theme", "anchors.fill", "anchors.margins", "anchors.centerIn",
			"marginTop", "marginBottom", "marginLeft", "marginRight",
			"antialiasing", "clip", "z", "opacity", "visible", "shadowVisible":
			// accepted, not modeled
		case "axisX":
			if a.elem != nil {
				axisX = readAxis(a.elem, env)
			}
		case "axisY":
			if a.elem != nil {
				axisY = readAxis(a.elem, env)
			}
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
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: GraphsView has no attribute %q", a.line, a.name)
		}
	}
	// Series children, in declaration order (which is paint order).
	type lineSpec struct {
		data    *seriesData
		color   ui.RGBA
		width   float32
		scatter bool
		area    bool
	}
	var lines []lineSpec
	type barSpec struct {
		values []float32
		color  ui.RGBA
	}
	var bars []barSpec
	for _, c := range n.children {
		switch c.typeName {
		case "LineSeries", "SplineSeries", "ScatterSeries", "AreaSeries":
			data := doc.series(doc.key(c))
			spec := lineSpec{
				data:    data,
				color:   ui.RGB(0x00d1a9),
				width:   2,
				scatter: c.typeName == "ScatterSeries",
				area:    c.typeName == "AreaSeries",
			}
			for _, a := range c.attrs {
				switch a.name {
				case "color", "borderColor":
					if col, err := attrColor(a, env); err == nil {
						spec.color = col
					}
				case "width", "borderWidth":
					if f, err := attrFloat(a, env); err == nil && f > 0 {
						spec.width = f
					}
				}
			}
			// The series' id exposes the QML point mutators, and its own
			// declarations run: a series fills itself from
			// Component.onCompleted and updates from a FrameAnimation or
			// Timer CHILD, neither of which is a visual element we build.
			doc.exposeSeriesMethods(c, data)
			doc.runSeriesDeclarations(c, env)
			lines = append(lines, spec)
		case "BarSeries":
			for _, set := range c.children {
				if set.typeName != "BarSet" {
					continue
				}
				b := barSpec{color: ui.RGB(0x00d1a9)}
				for _, a := range set.attrs {
					switch a.name {
					case "values":
						if v, err := a.expr.eval(env); err == nil {
							if arr, ok := v.([]any); ok {
								for _, x := range arr {
									f, _ := toFloat32(x)
									b.values = append(b.values, f)
								}
							}
						}
					case "color":
						if col, err := attrColor(a, env); err == nil {
							b.color = col
						}
					}
				}
				bars = append(bars, b)
			}
		}
	}
	if w <= 0 {
		w = 300
	}
	if h <= 0 {
		h = 200
	}
	return ui.NewCanvas(w, h, func(r *render.Renderer2D, rect ui.Rect) {
		pad := float32(6)
		plot := ui.Rect{X: rect.X + pad, Y: rect.Y + pad,
			W: max(rect.W-2*pad, 1), H: max(rect.H-2*pad, 1)}
		gridColor := ui.RGBA{R: 0.42, G: 0.44, B: 0.48, A: 0.3}
		if axisY.grid {
			steps := 4
			if axisY.tick > 0 && axisY.span()/axisY.tick <= 12 {
				steps = int(axisY.span() / axisY.tick)
			}
			for i := 0; i <= steps; i++ {
				y := plot.Y + plot.H*float32(i)/float32(max(steps, 1))
				r.StrokePath(render.NewPath().MoveTo(plot.X, y).
					LineTo(plot.X+plot.W, y), 1, gridColor)
			}
		}
		// Bars: grouped by category across the x range.
		if len(bars) > 0 {
			slots := len(axisX.categories)
			for _, b := range bars {
				if len(b.values) > slots {
					slots = len(b.values)
				}
			}
			slotW := plot.W / float32(max(slots, 1))
			for bi, b := range bars {
				bw := slotW / float32(len(bars)) * 0.7
				for i, v := range b.values {
					t := (v - axisY.min) / axisY.span()
					t = min(max(t, 0), 1)
					bh := plot.H * t
					x := plot.X + slotW*float32(i) + slotW*0.15 + bw*float32(bi)
					r.FillRoundRect(x, plot.Y+plot.H-bh, bw, bh, 2, b.color)
				}
			}
		}
		// Lines, areas and scatter points.
		for _, s := range lines {
			pts := s.data.pts
			if len(pts) == 0 {
				continue
			}
			at := func(p point) (float32, float32) {
				tx := (p.x - axisX.min) / axisX.span()
				ty := (p.y - axisY.min) / axisY.span()
				return plot.X + plot.W*min(max(tx, 0), 1),
					plot.Y + plot.H*(1-min(max(ty, 0), 1))
			}
			if s.scatter {
				for _, p := range pts {
					x, y := at(p)
					d := max(s.width*2, 4)
					r.FillRoundRect(x-d/2, y-d/2, d, d, d/2, s.color)
				}
				continue
			}
			path := render.NewPath()
			for i, p := range pts {
				x, y := at(p)
				if i == 0 {
					path.MoveTo(x, y)
				} else {
					path.LineTo(x, y)
				}
			}
			if s.area {
				// Close the path down to the baseline and fill it.
				lastX, _ := at(pts[len(pts)-1])
				firstX, _ := at(pts[0])
				fill := path
				fill.LineTo(lastX, plot.Y+plot.H)
				fill.LineTo(firstX, plot.Y+plot.H)
				c := s.color
				c.A *= 0.35
				r.FillPath(fill.Close(), c)
			}
			r.StrokePathRound(path, s.width, s.color)
		}
	}), nil
}

// readAxis resolves a ValueAxis or BarCategoryAxis element.
func readAxis(n *node, env *Env) axisSpec {
	a := axisSpec{min: 0, max: 1, grid: true}
	for _, at := range n.attrs {
		switch at.name {
		case "min":
			if f, err := attrFloat(at, env); err == nil {
				a.min = f
			}
		case "max":
			if f, err := attrFloat(at, env); err == nil {
				a.max = f
			}
		case "tickInterval":
			if f, err := attrFloat(at, env); err == nil {
				a.tick = f
			}
		case "gridVisible":
			if b, err := attrBool(at, env); err == nil {
				a.grid = b
			}
		case "categories":
			if v, err := at.expr.eval(env); err == nil {
				if arr, ok := v.([]any); ok {
					for _, c := range arr {
						a.categories = append(a.categories, str(c))
					}
					a.min, a.max = 0, float32(len(arr))
				}
			}
		}
	}
	return a
}

// series returns the retained point data for a series element.
func (doc *docState) series(key string) *seriesData {
	if doc.seriesData == nil {
		doc.seriesData = map[string]*seriesData{}
	}
	if s, ok := doc.seriesData[key]; ok {
		return s
	}
	s := &seriesData{}
	doc.seriesData[key] = s
	return s
}

// exposeSeriesMethods gives a series' id the QML point API, so handlers can
// build and update the plot: append(x, y), replace(i, x, y), remove(i),
// removeMultiple(i, n), clear(), count.
func (doc *docState) exposeSeriesMethods(n *node, data *seriesData) {
	id := nodeID(n)
	if id == "" {
		return
	}
	obj := doc.elemObjs.get(doc.here(id))
	if obj == nil {
		obj = &elemObject{}
		doc.elemObjs.set(doc.here(id), obj)
	}
	if obj.props == nil {
		obj.props = map[string]any{}
	}
	obj.setProp("count", float64(len(data.pts)))
	if obj.methods == nil {
		obj.methods = map[string]any{}
	}
	num := func(args []any, i int) float32 {
		if i < len(args) {
			if f, err := toFloat32(args[i]); err == nil {
				return f
			}
		}
		return 0
	}
	obj.methods["append"] = jsFunc(func(args []any) (any, error) {
		data.pts = append(data.pts, point{num(args, 0), num(args, 1)})
		return nil, nil
	})
	obj.methods["replace"] = jsFunc(func(args []any) (any, error) {
		// QML has replace(index, x, y) and replace(oldX, oldY, newX, newY);
		// the corpus uses the index form.
		i := int(num(args, 0))
		if i >= 0 && i < len(data.pts) {
			data.pts[i] = point{num(args, 1), num(args, 2)}
		} else if i == len(data.pts) {
			data.pts = append(data.pts, point{num(args, 1), num(args, 2)})
		}
		return nil, nil
	})
	obj.methods["remove"] = jsFunc(func(args []any) (any, error) {
		i := int(num(args, 0))
		if i >= 0 && i < len(data.pts) {
			data.pts = append(data.pts[:i], data.pts[i+1:]...)
		}
		return nil, nil
	})
	obj.methods["removeMultiple"] = jsFunc(func(args []any) (any, error) {
		i, count := int(num(args, 0)), int(num(args, 1))
		if i < 0 || i >= len(data.pts) {
			return nil, nil
		}
		end := min(i+max(count, 0), len(data.pts))
		data.pts = append(data.pts[:i], data.pts[end:]...)
		return nil, nil
	})
	obj.methods["clear"] = jsFunc(func([]any) (any, error) {
		data.pts = nil
		return nil, nil
	})
	obj.methods["at"] = jsFunc(func(args []any) (any, error) {
		i := int(num(args, 0))
		if i < 0 || i >= len(data.pts) {
			return nil, nil
		}
		return map[string]any{
			"x": float64(data.pts[i].x), "y": float64(data.pts[i].y),
		}, nil
	})
}

// runSeriesDeclarations services a series' non-visual children (a
// FrameAnimation or Timer that rewrites its points) and collects its
// Component.onCompleted, which is where QML fills the initial curve.
func (doc *docState) runSeriesDeclarations(n *node, env *Env) {
	doc.collectCompleted(n, env)
	for _, c := range n.children {
		if isNonVisual(c) {
			doc.refreshNonVisual(c, env)
		}
	}
}
