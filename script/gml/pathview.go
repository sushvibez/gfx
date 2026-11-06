package gml

// QML PathView: a model's delegates laid out along a Path. The path is the
// QML Path element (startX/startY plus PathLine/PathQuad/PathCubic/PathArc
// segments, sampled to a polyline; arcs approximate as quads through their
// midpoint). Delegates are spaced evenly along the arc length, like PathView
// spreads its items. The carousel turns: dragging rotates it continuously
// along the path, a flick carries it on and settles it on an item, and
// currentIndex — the item at the highlight position — follows, is readable
// through the view's id, and is honoured when the page writes it.

import (
	"fmt"
	"strings"
	"time"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

func buildPathView(n *node, env *Env, doc *docState) (ui.Element, error) {
	var highlightAt float32 // preferredHighlightBegin: where the current item sits
	var dragMargin float32  // how far off the path a drag may start (QML dragMargin)
	// The turn and the coast, installed on the items (or on the whole box
	// when dragMargin widens the reach).
	var turn func(dx, dy float32, phase ui.DragPhase)
	var coast func(vx, vy float32)
	var modelAttr *attr
	var pathNode *node
	var curAttr *attr
	var w, h float32
	for i := range n.attrs {
		a := &n.attrs[i]
		switch a.name {
		case "id":
		case "model", "count":
			modelAttr = a
		case "path":
			if a.elem != nil {
				pathNode = a.elem
			}
		case "width":
			f, err := attrFloat(*a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "height":
			f, err := attrFloat(*a, env)
			if err != nil {
				return nil, err
			}
			h = f
		case "delegate": // consumed via delegateChild
		case "preferredHighlightBegin":
			// Where along the path the CURRENT item sits (0.5 = centred),
			// which is what makes a carousel read as a carousel.
			if f, err := attrFloat(*a, env); err == nil {
				highlightAt = f
			}
		case "currentIndex":
			curAttr = a
		case "dragMargin":
			// How far from the path a press may start and still turn the
			// view. Qt's default 0 means "on an item only".
			if f, err := attrFloat(*a, env); err == nil {
				dragMargin = f
			}
		case "pathItemCount",
			"preferredHighlightEnd", "highlightRangeMode", "snapMode",
			"interactive", "clip":
			// accepted, not modeled (the highlight range is always strict
			// here: the carousel settles on an item)
		default:
			return nil, fmt.Errorf("gml:%d: PathView has no attribute %q", a.line, a.name)
		}
	}
	// Path can also be declared as a child element.
	for _, c := range n.children {
		if c.typeName == "Path" {
			pathNode = c
		}
	}
	if modelAttr == nil || pathNode == nil {
		return nil, fmt.Errorf("gml:%d: PathView needs a model and a path", n.line)
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	rows, count, err := resolveModel(*modelAttr, env)
	if err != nil {
		return nil, err
	}
	if m := modelObject(*modelAttr, env); m != nil {
		doc.exposeIDProp(n, "model", m) // `view.model.get(i)`, as QML reads it
	}
	doc.exposeIDProp(n, "count", float64(count))
	poly, pathAttrs, err := evalPathPolyline(pathNode, env)
	if err != nil {
		return nil, err
	}
	box := ui.Div().AnchorMode()
	if w > 0 {
		box.W(w)
	}
	if h > 0 {
		box.H(h)
	}
	if count == 0 || len(poly) < 2 {
		return box, nil
	}
	// Carousel rotation: a continuous slot offset, which a drag turns and a
	// flick carries on. currentIndex — the item sitting at the highlight
	// position — is derived from it, published on the id for bindings, and
	// adopted back when the page writes it.
	viewKey := doc.key(n)
	offRef := doc.floatProp(viewKey+"|pvOffset", 0)
	curRef := doc.intProp(viewKey+"|currentIndex", 0)
	off := refFloat(offRef)
	offAt := func(i int) { off = -float32(i); _ = offRef.set(off) }
	// The page's own currentIndex: a literal seeds the carousel, a binding
	// turns it. The remembered value starts one off, so the first build
	// always positions the wheel.
	if curAttr != nil {
		if v, err := attrFloat(*curAttr, env); err == nil {
			want := ((int(v) % count) + count) % count
			seen := doc.intProp(viewKey+"|pvCurAttr", want+1)
			if refInt(seen) != want {
				_ = seen.set(want)
				offAt(want)
			}
		}
	}
	cur := pathSlot(-off, count)
	// A write through the id (`view.currentIndex = 3`) turns the carousel.
	// It is a write, rather than the value we published last build, exactly
	// when it differs from what we published — turning the carousel
	// ourselves must not read back as the page turning it.
	pubRef := doc.intProp(viewKey+"|pvPublished", 0)
	if want := ((refInt(curRef) % count) + count) % count; want != refInt(pubRef) && want != cur {
		offAt(want)
		cur = want
	}
	_ = curRef.set(cur)
	_ = pubRef.set(cur)
	if idObj := doc.elemObjs.get(doc.intern(nodeID(n))); idObj != nil {
		idObj.setProp("currentIndex", curRef)
		installPathViewMethods(idObj, offRef, count)
	}
	if count > 1 {
		// How far the pointer drags to turn one item: the path's own
		// spacing. A tall path turns with vertical drags, a wide one with
		// horizontal.
		spacing := max(polylineLength(poly)/float32(count), 1)
		vertical := polylineTall(poly)
		axis := func(dx, dy float32) float32 {
			if vertical {
				return dy
			}
			return dx
		}
		startRef := doc.floatProp(viewKey+"|pvStart", 0)
		turn = func(dx, dy float32, phase ui.DragPhase) {
			p := spinProp(offRef)
			switch phase {
			case ui.DragStart:
				core.StopTween(p) // a press catches a turning carousel
				_ = startRef.set(refFloat(offRef))
			case ui.DragMove:
				_ = offRef.set(refFloat(startRef) + axis(dx, dy)/spacing)
			case ui.DragEnd:
				// Let go and the nearest item settles under the highlight.
				at := refFloat(startRef) + axis(dx, dy)/spacing
				_ = offRef.set(at)
				// Already on an item: nothing to settle, and no animation
				// to keep the window repainting for.
				if snap := math32.Round(at); p != nil && math32.Abs(at-snap) > 0.001 {
					p.Set(at)
					core.Tween(p, snap, 120*time.Millisecond, core.EaseOutQuad)
				}
			}
		}
		// Qt's `dragMargin` is 0 by default, which means the view turns
		// only when the press lands ON an item — a press in the middle of a
		// circular path does nothing. So the DELEGATES carry the drag, and
		// the whole box carries it only when the markup widens the margin.
		if dragMargin > 0 {
			box.OnDrag(turn)
		}
		// Still moving on release: coast on, and settle on an item.
		coast = func(vx, vy float32) {
			p := spinProp(offRef)
			if p == nil {
				return
			}
			v := axis(vx, vy)
			at := refFloat(offRef)
			target := math32.Round(at + flingDistance(v)/spacing)
			p.Set(at)
			core.Tween(p, target, flingDuration(v), core.EaseOutQuad)
		}
		if dragMargin > 0 {
			box.OnFling(coast)
		}
	}
	for i := 0; i < count; i++ {
		var row map[string]any
		if rows != nil {
			row = rows[i]
		}
		// Spread items over the path INCLUDING the implicit return leg
		// (PathView is a closed carousel: item k sits at t = k/count),
		// rotated by the swipe offset.
		slot := pathWrap(float32(i)+off, float32(count))
		t := pathWrap(slot/float32(count)+highlightAt, 1)
		x, y := pointAlong(poly, t)
		restore := enterRow(viewKey, delegate, env, doc, row, i)
		// PathAttribute values at this item's position, readable as
		// `PathView.<name>` — how a carousel scales and fades its items.
		restoreAttrs := bindPathAttrs(env, pathAttrsAt(pathAttrs, len(poly), t))
		el, err := build(delegate, env, doc)
		restoreAttrs()
		restore()
		if err != nil {
			return nil, err
		}
		// A path position is where the item's CENTRE goes, not its
		// top-left corner: Qt places each delegate centred on its point,
		// so a 60px bubble on a circle sat 30px down and right of where it
		// belonged.
		dw, dh := ui.FixedSize(el)
		if dw == 0 || dh == 0 {
			if iw, ih := ui.Implicit(el); iw > 0 || ih > 0 {
				if dw == 0 {
					dw = iw
				}
				if dh == 0 {
					dh = ih
				}
			}
		}
		if turn != nil && dragMargin <= 0 {
			// The item itself is the handle (see dragMargin above).
			if d, ok := el.(*ui.DivEl); ok {
				d.OnDrag(turn)
				if coast != nil {
					d.OnFling(coast)
				}
			} else {
				wrap := ui.Div().Size(dw, dh).Children(el)
				wrap.OnDrag(turn)
				if coast != nil {
					wrap.OnFling(coast)
				}
				el = wrap
			}
		}
		box.Children(ui.Anchored(el, "", ui.Anchors{
			X: x - dw/2, Y: y - dh/2, HasX: true, HasY: true,
		}))
	}
	return box, nil
}

// installPathViewMethods gives the view's id QML's navigation calls. They
// animate, as PathView's do (highlightMoveDuration), by tweening the same
// continuous offset a drag moves.
func installPathViewMethods(obj *elemObject, offRef Ref, count int) {
	if obj.methods == nil {
		obj.methods = map[string]any{}
	}
	turnTo := func(index int) {
		p := spinProp(offRef)
		if p == nil {
			_ = offRef.set(-float32(index))
			return
		}
		// Turn the short way round: an offset of -index modulo the item
		// count, picked nearest to where the carousel already stands.
		at := p.Get()
		target := -float32(index)
		for target-at > float32(count)/2 {
			target -= float32(count)
		}
		for at-target > float32(count)/2 {
			target += float32(count)
		}
		core.Tween(p, target, pathTurnDuration, core.EaseOutQuad)
	}
	step := func(by int) {
		at := pathSlot(-refFloat(offRef), count)
		turnTo(((at+by)%count + count) % count)
	}
	obj.methods["incrementCurrentIndex"] = jsFunc(func([]any) (any, error) { step(1); return nil, nil })
	obj.methods["decrementCurrentIndex"] = jsFunc(func([]any) (any, error) { step(-1); return nil, nil })
	obj.methods["positionViewAtIndex"] = jsFunc(func(args []any) (any, error) {
		if len(args) == 0 {
			return nil, nil
		}
		i, err := toInt(args[0])
		if err != nil {
			return nil, err
		}
		core.StopTween(spinProp(offRef))
		_ = offRef.set(-float32(((i % count) + count) % count)) // no animation: position, not turn
		return nil, nil
	})
}

// pathTurnDuration is how long a programmatic turn takes — QML's
// highlightMoveDuration default.
const pathTurnDuration = 300 * time.Millisecond

// pathWrap folds v into [0, period).
func pathWrap(v, period float32) float32 {
	v -= period * math32.Floor(v/period)
	if v < 0 || v >= period {
		return 0 // a NaN or an infinity is no position at all
	}
	return v
}

// pathSlot is the item index a continuous offset points at.
func pathSlot(off float32, count int) int {
	i := int(math32.Round(off))
	return ((i % count) + count) % count
}

// polylineLength is the arc length of a sampled path.
func polylineLength(poly [][2]float32) float32 {
	var total float32
	for i := 1; i < len(poly); i++ {
		total += math32.Hypot(poly[i][0]-poly[i-1][0], poly[i][1]-poly[i-1][1])
	}
	return total
}

// polylineTall reports whether a path spans more vertically than
// horizontally — a column of items turns by dragging up and down.
func polylineTall(poly [][2]float32) bool {
	minX, maxX := poly[0][0], poly[0][0]
	minY, maxY := poly[0][1], poly[0][1]
	for _, p := range poly[1:] {
		minX, maxX = min(minX, p[0]), max(maxX, p[0])
		minY, maxY = min(minY, p[1]), max(maxY, p[1])
	}
	return maxY-minY > maxX-minX
}

// refFloat reads a ref as a number, 0 when it holds anything else.
func refFloat(r Ref) float32 {
	f, err := toFloat32(r.get())
	if err != nil {
		return 0
	}
	return f
}

// bindPathAttrs merges interpolated PathAttribute values into the row's
// `PathView` attached object for one delegate build.
func bindPathAttrs(env *Env, attrs map[string]any) func() {
	if len(attrs) == 0 {
		return func() {}
	}
	prev, had := env.vars["PathView"]
	merged := map[string]any{}
	if base, ok := prev.(map[string]any); ok {
		for k, v := range base {
			merged[k] = v
		}
	}
	for k, v := range attrs {
		merged[k] = v
	}
	env.vars["PathView"] = merged
	return func() {
		if had {
			env.vars["PathView"] = prev
		} else {
			delete(env.vars, "PathView")
		}
	}
}

// evalPathPolyline samples a QML Path element into a polyline.
// pathAnchor is a PathAttribute: a named value anchored at a point index of
// the sampled polyline.
type pathAnchor struct {
	at    int
	name  string
	value float32
}

// pathAttrsAt interpolates every named attribute at position t (0..1) along
// the polyline, as QML does between successive PathAttribute anchors.
func pathAttrsAt(anchors []pathAnchor, points int, t float32) map[string]any {
	if len(anchors) == 0 || points < 2 {
		return nil
	}
	out := map[string]any{}
	byName := map[string][]pathAnchor{}
	order := []string{}
	for _, a := range anchors {
		if _, seen := byName[a.name]; !seen {
			order = append(order, a.name)
		}
		byName[a.name] = append(byName[a.name], a)
	}
	pos := t * float32(points-1)
	for _, name := range order {
		list := byName[name]
		switch {
		case len(list) == 1:
			out[name] = float64(list[0].value)
			continue
		}
		v := list[0].value
		for i := 1; i < len(list); i++ {
			a, b := list[i-1], list[i]
			af, bf := float32(a.at), float32(b.at)
			switch {
			case pos <= af:
				v = a.value
			case pos >= bf:
				v = b.value
			default:
				f := (pos - af) / max(bf-af, 1)
				v = a.value + (b.value-a.value)*f
			}
			if pos < bf {
				break
			}
		}
		out[name] = float64(v)
	}
	return out
}

func evalPathPolyline(p *node, env *Env) ([][2]float32, []pathAnchor, error) {
	attrF := func(n *node, name string, def float32) float32 {
		for _, a := range n.attrs {
			if a.name == name {
				if f, err := attrFloat(a, env); err == nil {
					return f
				}
			}
		}
		return def
	}
	x, y := attrF(p, "startX", 0), attrF(p, "startY", 0)
	poly := [][2]float32{{x, y}}
	var anchors []pathAnchor
	quadTo := func(cx, cy, ex, ey float32) {
		const segs = 16
		for s := 1; s <= segs; s++ {
			t := float32(s) / segs
			mt := 1 - t
			px := mt*mt*x + 2*mt*t*cx + t*t*ex
			py := mt*mt*y + 2*mt*t*cy + t*t*ey
			poly = append(poly, [2]float32{px, py})
		}
		x, y = ex, ey
	}
	for _, c := range p.children {
		switch c.typeName {
		case "PathLine":
			x, y = attrF(c, "x", x), attrF(c, "y", y)
			poly = append(poly, [2]float32{x, y})
		case "PathQuad":
			quadTo(attrF(c, "controlX", x), attrF(c, "controlY", y),
				attrF(c, "x", x), attrF(c, "y", y))
		case "PathCubic":
			// Approximate the cubic with its control-point midpoint quad.
			c1x, c1y := attrF(c, "control1X", x), attrF(c, "control1Y", y)
			c2x, c2y := attrF(c, "control2X", x), attrF(c, "control2Y", y)
			quadTo((c1x+c2x)/2, (c1y+c2y)/2, attrF(c, "x", x), attrF(c, "y", y))
		case "PathArc":
			// A real SVG endpoint-parameterized arc — the same maths
			// QtQuick.Shapes uses (see shape.go). A circular PathView is
			// two half-circle arcs, which a bulged quad cannot express.
			ex, ey := attrF(c, "x", x), attrF(c, "y", y)
			rx := attrF(c, "radiusX", 0)
			ry := attrF(c, "radiusY", rx)
			large := false
			for _, a := range c.attrs {
				if a.name == "useLargeArc" {
					if b, err := attrBool(a, env); err == nil {
						large = b
					}
				}
			}
			// QML's direction: Clockwise (default) / Counterclockwise.
			ccw := false
			for _, a := range c.attrs {
				if a.name == "direction" {
					if v, err := a.expr.eval(env); err == nil {
						ccw = strings.Contains(str(v), "Counter")
					}
				}
			}
			arc := render.NewPath()
			arc.MoveTo(x, y)
			arcSegments(arc, x, y, ex, ey, rx, ry, large, ccw)
			for _, sub := range arc.Polyline() {
				for _, pt := range sub {
					poly = append(poly, [2]float32{pt.X, pt.Y})
				}
			}
			x, y = ex, ey
		case "PathAttribute":
			// A named value anchored at this point of the path. Delegates
			// read them as `PathView.<name>` and scale/fade with them.
			name := ""
			val := float32(0)
			for _, a := range c.attrs {
				switch a.name {
				case "name":
					if v, err := a.expr.eval(env); err == nil {
						name = str(v)
					}
				case "value":
					if f, err := attrFloat(a, env); err == nil {
						val = f
					}
				}
			}
			if name != "" {
				anchors = append(anchors, pathAnchor{at: len(poly) - 1, name: name, value: val})
			}
			continue
		case "PathPercent":
			// Explicit spacing along the path: the SHAPE is what we sample.
			continue
		default:
			return nil, nil, fmt.Errorf("gml:%d: Path has no segment %q", c.line, c.typeName)
		}
	}
	return poly, anchors, nil
}

// pointAlong returns the point at fraction t (0..1) of the polyline's total
// arc length.
func pointAlong(poly [][2]float32, t float32) (float32, float32) {
	var total float32
	for i := 1; i < len(poly); i++ {
		total += math32.Hypot(poly[i][0]-poly[i-1][0], poly[i][1]-poly[i-1][1])
	}
	if total == 0 {
		return poly[0][0], poly[0][1]
	}
	want := t * total
	var acc float32
	for i := 1; i < len(poly); i++ {
		seg := math32.Hypot(poly[i][0]-poly[i-1][0], poly[i][1]-poly[i-1][1])
		if acc+seg >= want && seg > 0 {
			f := (want - acc) / seg
			return poly[i-1][0] + (poly[i][0]-poly[i-1][0])*f,
				poly[i-1][1] + (poly[i][1]-poly[i-1][1])*f
		}
		acc += seg
	}
	last := poly[len(poly)-1]
	return last[0], last[1]
}
