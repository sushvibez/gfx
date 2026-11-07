package gml

import (
	"github.com/sushvibez/gfx/ui"
)

// MouseArea's drag group: dragging the area moves drag.target's x and y
// by the pointer's movement, along drag.axis, kept inside drag.minimumX
// … drag.maximumY, once the pointer has moved drag.threshold pixels. The
// target's x/y are written as a handler's `target.x = …` would write them
// (breaking their bindings, as Qt's drag does). mouseArea.drag.active is
// true while a drag moves the target. A MouseArea with a drag target
// still clicks: a press that never passes the threshold is a click.

// Qt's Drag.Axis values.
const (
	dragXAxis     = 1
	dragYAxis     = 2
	dragXAndYAxis = 3
)

// dragAttrs are a MouseArea's drag.* attributes as boxAttrs collected
// them (expressions, evaluated when the drag wires).
type dragAttrs struct {
	target, axis, threshold expr
	minX, maxX, minY, maxY  expr
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
	default:
		return false
	}
	return true
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

// wireDragTarget makes d drag the target. It resolves target.x and
// target.y now, while the build's scope (parent and the ids) is bound.
func (doc *docState) wireDragTarget(n *node, d *ui.DivEl, env *Env, g *dragAttrs) {
	if g.target == nil {
		return
	}
	if v, err := g.target.eval(env); err != nil || v == nil {
		return // drag.target: undefined/null — no drag
	}
	for _, a := range n.attrs { // a disabled MouseArea does not drag
		if a.name == "enabled" && a.expr != nil {
			if v, err := a.expr.eval(env); err == nil {
				if b, _ := toBool(v); !b {
					return
				}
			}
		}
	}
	xRef, errX := resolveRef(&memberExpr{base: g.target, name: "x"}, env)
	yRef, errY := resolveRef(&memberExpr{base: g.target, name: "y"}, env)
	if errX != nil || errY != nil {
		logf("gml:%d: MouseArea drag.target: the target's x/y cannot be written", n.line)
		return
	}
	axis := dragXAndYAxis
	if g.axis != nil {
		if v, err := g.axis.eval(env); err == nil {
			if a, err := toInt(v); err == nil && a >= dragXAxis && a <= dragXAndYAxis {
				axis = a
			}
		}
	}
	threshold := float32(doc.dragThreshold())
	if t, ok := dragLimit(g.threshold, env); ok && t >= 0 {
		threshold = t
	}
	minX, hasMinX := dragLimit(g.minX, env)
	maxX, hasMaxX := dragLimit(g.maxX, env)
	minY, hasMinY := dragLimit(g.minY, env)
	maxY, hasMaxY := dragLimit(g.maxY, env)
	clamp := func(v, lo, hi float32, hasLo, hasHi bool) float32 {
		if hasHi && v > hi {
			v = hi
		}
		if hasLo && v < lo {
			v = lo
		}
		return v
	}

	// The drag's state lives across builds (the tree rebuilds as the
	// target moves); active is exposed as drag.active on the id.
	st := doc.dragState(doc.keyStr(doc.keyID(n)))
	// Where the target is now, as the page reads it: the x/y refs above
	// write the target, but before the first write they hold no value of
	// their own (the target's `x: 20` is an attribute, not the ref).
	if v, err := (&memberExpr{base: g.target, name: "x"}).eval(env); err == nil {
		st.curX, _ = toFloat32(v)
	}
	if v, err := (&memberExpr{base: g.target, name: "y"}).eval(env); err == nil {
		st.curY, _ = toFloat32(v)
	}
	doc.exposeDragActive(n, st)
	d.OnDrag(func(dx, dy float32, phase ui.DragPhase) {
		switch phase {
		case ui.DragStart:
			st.startX, st.startY = st.curX, st.curY
			st.moving = false
			return
		case ui.DragEnd:
			if st.moving {
				st.moving = false
				_ = st.active.set(false)
			}
			return
		}
		if !st.moving {
			ax, ay := dx, dy
			if axis == dragYAxis {
				ax = 0
			}
			if axis == dragXAxis {
				ay = 0
			}
			if ax*ax+ay*ay <= threshold*threshold {
				return
			}
			st.moving = true
			_ = st.active.set(true)
		}
		if axis != dragYAxis {
			_ = xRef.set(float64(clamp(st.startX+dx, minX, maxX, hasMinX, hasMaxX)))
		}
		if axis != dragXAxis {
			_ = yRef.set(float64(clamp(st.startY+dy, minY, maxY, hasMinY, hasMaxY)))
		}
	})
}

// dragTargetState is one MouseArea's drag, kept across builds.
type dragTargetState struct {
	startX, startY float32
	curX, curY     float32 // the target's position at the last build
	moving         bool
	active         Ref
}

func (doc *docState) dragState(key string) *dragTargetState {
	if doc.dragStates == nil {
		doc.dragStates = map[string]*dragTargetState{}
	}
	st := doc.dragStates[key]
	if st == nil {
		st = &dragTargetState{active: doc.boolProp(key+".drag.active", false)}
		doc.dragStates[key] = st
	}
	return st
}

// exposeDragActive puts the drag group on the area's id object:
// ma.drag.active.
func (doc *docState) exposeDragActive(n *node, st *dragTargetState) {
	id := nodeID(n)
	if id == "" {
		return
	}
	obj := doc.elemObjs.get(doc.intern(id))
	if obj == nil {
		return
	}
	// The value, not the Ref: active is a document property, so setting
	// it rebuilds the page and this map is refreshed with it.
	active, _ := toBool(st.active.get())
	obj.setProp("drag", map[string]any{"active": active})
}

// dragThreshold is Qt's default drag distance (QStyleHints
// startDragDistance: 10 logical pixels).
func (doc *docState) dragThreshold() int { return 10 }
