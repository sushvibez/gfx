package gml

import (
	"math"

	"github.com/sushvibez/gfx/ui"
)

// Dragging an item by hand: MouseArea's drag group and DragHandler.
//
// MouseArea: dragging the area moves drag.target's x and y by the
// pointer's movement, along drag.axis, kept inside drag.minimumX …
// drag.maximumY, once the pointer has moved drag.threshold pixels. The
// target's x/y are written as a handler's `target.x = …` would write them
// (breaking their bindings, as Qt's drag does). mouseArea.drag.active is
// true while a drag moves the target. A MouseArea with a drag target
// still clicks: a press that never passes the threshold is a click.
//
// DragHandler is rewritten onto the same attributes (pointerHandlerAsMouseArea):
// its target (the item it sits in by default) is drag.target, xAxis/yAxis'
// minimum, maximum and enabled are the limits and the axis, dragThreshold
// is the threshold. On top of the MouseArea's drag it reports active,
// activeTranslation (this drag's movement) and persistentTranslation (all
// drags' together), runs onTranslationChanged(delta) and onActiveChanged,
// and with `target: null` moves nothing and only reports — how a page pans
// a map or a canvas by hand.

// Qt's Drag.Axis values.
const (
	dragXAxis     = 1
	dragYAxis     = 2
	dragXAndYAxis = 3
)

// dragAttrs are the drag.* attributes as boxAttrs collected them
// (expressions, evaluated when the drag wires).
type dragAttrs struct {
	target, axis, threshold expr
	minX, maxX, minY, maxY  expr
	// DragHandler's: set by pointerHandlerAsMouseArea's rewrite.
	handler                     bool
	xEnabled, yEnabled          expr
	onTranslation, onActiveChgd []stmt
}

func (g *dragAttrs) read(a attr) bool {
	switch a.name {
	case "drag.target":
		g.target = a.expr
	case "drag.axis":
		g.axis = a.expr
	case "drag.threshold":
		g.threshold = a.expr
	case "drag.minimumX":
		g.minX = a.expr
	case "drag.maximumX":
		g.maxX = a.expr
	case "drag.minimumY":
		g.minY = a.expr
	case "drag.maximumY":
		g.maxY = a.expr
	case "drag.handler":
		g.handler = true
	case "drag.xEnabled":
		g.xEnabled = a.expr
	case "drag.yEnabled":
		g.yEnabled = a.expr
	case "drag.onTranslationChanged":
		g.onTranslation = a.stmt
	case "drag.onActiveChanged":
		g.onActiveChgd = a.stmt
	default:
		return false
	}
	return true
}

// dragAttrNames are the attribute names dragAttrs.read takes.
var dragAttrNames = map[string]bool{
	"drag.target": true, "drag.axis": true, "drag.threshold": true,
	"drag.minimumX": true, "drag.maximumX": true, "drag.minimumY": true, "drag.maximumY": true,
	"drag.handler": true, "drag.xEnabled": true, "drag.yEnabled": true,
	"drag.onTranslationChanged": true, "drag.onActiveChanged": true,
}

// dragLimit evaluates an optional bound (ok false when absent or bad).
func dragLimit(e expr, env *Env) (float32, bool) {
	if e == nil {
		return 0, false
	}
	v, err := e.eval(env)
	if err != nil {
		return 0, false
	}
	f, err := toFloat32(v)
	return f, err == nil
}

// dragEnabled evaluates an optional flag that defaults to true.
func dragEnabled(e expr, env *Env) bool {
	if e == nil {
		return true
	}
	v, err := e.eval(env)
	if err != nil {
		return true
	}
	b, _ := toBool(v)
	return b
}

// boxObjExpr evaluates to a box's own object, as `parent` does for its
// children: DragHandler's default target is the item it sits in, and its
// hoisted attributes are evaluated in that item's own scope, where
// `parent` would be one level too far out.
type boxObjExpr struct {
	box *node
	doc *docState
}

func (e *boxObjExpr) eval(env *Env) (any, error) { return parentObject(e.box, env, e.doc), nil }

// wireDragTarget makes d drag the target through a ui.Mover kept across
// builds. It resolves target.x and target.y now, while the build's scope
// (parent and the ids) is bound, and reconfigures the mover each build.
func (doc *docState) wireDragTarget(n *node, d *ui.DivEl, env *Env, g *dragAttrs) {
	if g.target == nil && !g.handler {
		return
	}
	var target any
	if g.target != nil {
		if v, err := g.target.eval(env); err == nil {
			target = v
		}
	}
	if target == nil && !g.handler {
		return // drag.target: undefined/null — no drag
	}
	if !dragEnabled(attrExpr(n, "enabled"), env) {
		return // a disabled MouseArea or handler does not drag
	}
	axis := ui.MoveXY
	if g.axis != nil {
		if v, err := g.axis.eval(env); err == nil {
			switch a, _ := toInt(v); a {
			case dragXAxis:
				axis = ui.MoveX
			case dragYAxis:
				axis = ui.MoveY
			}
		}
	}
	xOn, yOn := dragEnabled(g.xEnabled, env), dragEnabled(g.yEnabled, env)
	switch {
	case !xOn && !yOn:
		return
	case !xOn:
		axis = ui.MoveY
	case !yOn:
		axis = ui.MoveX
	}

	st := doc.dragState(doc.keyStr(doc.keyID(n)))
	m := st.mover
	m.Axis = axis
	m.Threshold = ui.DefaultDragThreshold
	if t, ok := dragLimit(g.threshold, env); ok && t >= 0 {
		m.Threshold = t
	}
	inf := float32(math.Inf(1))
	bound := func(e expr, def float32) float32 {
		if v, ok := dragLimit(e, env); ok {
			return v
		}
		return def
	}
	m.MinX, m.MaxX = bound(g.minX, -inf), bound(g.maxX, inf)
	m.MinY, m.MaxY = bound(g.minY, -inf), bound(g.maxY, inf)

	// A null DragHandler target moves nothing; the drag only reports.
	m.SetPos = nil
	if target != nil {
		xRef, errX := resolveRef(&memberExpr{base: g.target, name: "x"}, env)
		yRef, errY := resolveRef(&memberExpr{base: g.target, name: "y"}, env)
		if errX != nil || errY != nil {
			logf("gml:%d: drag target: the target's x/y cannot be written", n.line)
			return
		}
		// Where the target is now, as the page reads it: the refs write
		// the target, but before the first write they hold no value of
		// their own (the target's `x: 20` is an attribute, not the ref).
		if v, err := (&memberExpr{base: g.target, name: "x"}).eval(env); err == nil {
			st.curX, _ = toFloat32(v)
		}
		if v, err := (&memberExpr{base: g.target, name: "y"}).eval(env); err == nil {
			st.curY, _ = toFloat32(v)
		}
		// Only the axis that moves is written: writing the other one
		// would break its binding for nothing.
		m.SetPos = func(x, y float32) {
			if axis != ui.MoveY {
				_ = xRef.set(float64(x))
			}
			if axis != ui.MoveX {
				_ = yRef.set(float64(y))
			}
		}
	}

	// The mover's state is mirrored into document properties, so a change
	// rebuilds the page and the id object's values follow.
	var run func([]stmt, string)
	if g.onTranslation != nil || g.onActiveChgd != nil {
		run = doc.handlerRunner(env)
	}
	m.OnActive = func(on bool) {
		_ = st.active.set(on)
		if g.handler && on {
			_ = st.tx.set(0.0)
			_ = st.ty.set(0.0)
		}
		if g.onActiveChgd != nil {
			// The handler reads its own `active` by bare name, as QML
			// resolves it against the handler object.
			prev, had := env.vars["active"]
			env.vars["active"] = on
			run(g.onActiveChgd, "onActiveChanged")
			if had {
				env.vars["active"] = prev
			} else {
				delete(env.vars, "active")
			}
		}
	}
	m.OnMove = nil
	if g.handler {
		m.OnMove = func(dx, dy float32) {
			tx, ty := m.Translation()
			px, py := m.PersistentTranslation()
			_ = st.tx.set(float64(tx))
			_ = st.ty.set(float64(ty))
			_ = st.px.set(float64(px))
			_ = st.py.set(float64(py))
			if g.onTranslation != nil {
				runEvent(run, env, g.onTranslation, "onTranslationChanged", "delta",
					map[string]any{"x": float64(dx), "y": float64(dy)})
			}
		}
	}
	doc.exposeDrag(n, st, g.handler)
	d.DragMove(m)
}

// attrExpr is n's attribute name's expression, or nil.
func attrExpr(n *node, name string) expr {
	for _, a := range n.attrs {
		if a.name == name {
			return a.expr
		}
	}
	return nil
}

// dragTargetState is one drag's mover and its mirrored state, kept
// across builds.
type dragTargetState struct {
	mover      *ui.Mover
	curX, curY float32 // the target's position at the last build
	active     Ref
	// DragHandler's translations: this drag's, and all drags' summed.
	tx, ty, px, py Ref
}

func (doc *docState) dragState(key string) *dragTargetState {
	if doc.dragStates == nil {
		doc.dragStates = map[string]*dragTargetState{}
	}
	st := doc.dragStates[key]
	if st == nil {
		st = &dragTargetState{
			active: doc.boolProp(key+".drag.active", false),
			tx:     doc.floatProp(key+".drag.tx", 0), ty: doc.floatProp(key+".drag.ty", 0),
			px: doc.floatProp(key+".drag.px", 0), py: doc.floatProp(key+".drag.py", 0),
		}
		st.mover = ui.NewMover(func() (float32, float32) { return st.curX, st.curY }, nil)
		doc.dragStates[key] = st
	}
	return st
}

// exposeDrag puts the drag's state on the id object: a MouseArea's
// drag.active, a DragHandler's active, activeTranslation and
// persistentTranslation. Values, not Refs: each is a document property,
// so a change rebuilds the page and refreshes them.
func (doc *docState) exposeDrag(n *node, st *dragTargetState, handler bool) {
	id := nodeID(n)
	if id == "" {
		return
	}
	obj := doc.elemObjs.get(doc.intern(id))
	if obj == nil {
		return
	}
	active, _ := toBool(st.active.get())
	if !handler {
		obj.setProp("drag", map[string]any{"active": active})
		return
	}
	f := func(r Ref) float64 { v, _ := toFloat64(r.get()); return v }
	obj.setProp("active", active)
	obj.setProp("activeTranslation", map[string]any{"x": f(st.tx), "y": f(st.ty)})
	obj.setProp("persistentTranslation", map[string]any{"x": f(st.px), "y": f(st.py)})
}
