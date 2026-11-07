package gml

import (
	"math"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// WheelHandler, PinchHandler and PointHandler: Qt Quick Input Handlers
// wired straight onto the item they sit in (box), over ui's DivEl.OnWheel,
// OnPinch and OnPointer. (TapHandler, HoverHandler and DragHandler are
// rewritten to a MouseArea instead: pointerHandlerAsMouseArea.)
//
// Each handler's state is mirrored into document properties, so a change
// rebuilds the page and the values on its id follow: WheelHandler's
// active and rotation; PinchHandler's active, activeScale and
// persistentScale; PointHandler's active and point.position /
// point.pressPosition (in the item's coordinates) / point.scenePosition.

// isWiredHandler reports the Input Handlers wireInputHandler takes.
func isWiredHandler(n *node) bool {
	switch n.typeName {
	case "WheelHandler", "PinchHandler", "PointHandler":
		return true
	}
	return false
}

// inputHandlerState is one handler's mirrored state, kept across builds.
type inputHandlerState struct {
	active         Ref
	a, b, c, d     Ref // per handler: rotation | activeScale, persistentScale | position x, y, press x, y
	sceneX, sceneY Ref
	pressX, pressY float32 // PointHandler: the press, in window pixels
	pinchInit      bool
	key            string
	more           map[string]Ref // further mirrored values, by name (handlerRef)
}

func (doc *docState) inputHandlerState(key string) *inputHandlerState {
	if doc.handlerStates == nil {
		doc.handlerStates = map[string]*inputHandlerState{}
	}
	st := doc.handlerStates[key]
	if st == nil {
		f := func(s string, def float32) Ref { return doc.floatProp(key+"."+s, def) }
		st = &inputHandlerState{key: key, active: doc.boolProp(key+".active", false),
			a: f("a", 0), b: f("b", 1), c: f("c", 0), d: f("d", 0),
			sceneX: f("sx", 0), sceneY: f("sy", 0)}
		doc.handlerStates[key] = st
	}
	return st
}

// handlerAttrs reads a handler's attributes: expressions by name and
// handler bodies by name.
func handlerAttrs(n *node) (map[string]expr, map[string][]stmt) {
	exprs, stmts := map[string]expr{}, map[string][]stmt{}
	for _, a := range n.attrs {
		if a.stmt != nil {
			stmts[a.name] = a.stmt
		} else if a.expr != nil {
			exprs[a.name] = a.expr
		}
	}
	return exprs, stmts
}

// wireInputHandler wires one WheelHandler, PinchHandler or PointHandler h
// onto d, the box it sits in.
func (doc *docState) wireInputHandler(h, box *node, d *ui.DivEl, env *Env) {
	exprs, stmts := handlerAttrs(h)
	if !dragEnabled(exprs["enabled"], env) {
		return
	}
	st := doc.inputHandlerState(doc.keyStr(doc.keyID(h)))
	var run func([]stmt, string)
	if len(stmts) > 0 {
		run = doc.handlerRunner(env)
	}
	// The handler's target: the item it sits in unless it names one;
	// `target: null` drives nothing.
	targetExpr := expr(&boxObjExpr{box: box, doc: doc})
	var hasTarget = true
	if e, ok := exprs["target"]; ok {
		targetExpr = e
		if v, err := e.eval(env); err != nil || v == nil {
			hasTarget = false
		}
	}
	setActive := func(on bool) {
		if b, _ := toBool(st.active.get()); b == on {
			return
		}
		_ = st.active.set(on)
		if body := stmts["onActiveChanged"]; body != nil {
			withVar(env, "active", on, func() { run(body, "onActiveChanged") })
		}
	}
	switch h.typeName {
	case "WheelHandler":
		doc.wireWheelHandler(d, env, exprs, stmts, st, run, targetExpr, hasTarget)
	case "PinchHandler":
		doc.wirePinchHandler(d, env, exprs, stmts, st, run, targetExpr, hasTarget, setActive)
	case "PointHandler":
		doc.wirePointHandler(d, env, stmts, st, run, setActive)
	}
	doc.exposeInputHandler(h, st)
}

// withVar runs fn with name bound to v in env (a handler's bare read of
// its own property, as QML resolves it against the handler).
func withVar(env *Env, name string, v any, fn func()) {
	prev, had := env.vars[name]
	env.vars[name] = v
	fn()
	if had {
		env.vars[name] = prev
	} else {
		delete(env.vars, name)
	}
}

// wheelScaleMultiplier is Qt's default targetScaleMultiplier: the eighth
// root of 2, so eight notches double a "scale" target.
var wheelScaleMultiplier = math.Pow(2, 1.0/8)

// wireWheelHandler: onWheel(event) with Qt's angleDelta (eighths of a
// degree, 120 a notch, positive turned away), and `property` on the
// target driven by the rotation — multiplied by targetScaleMultiplier per
// 15° for "scale" and "zoomFactor", else moved by rotationScale per
// degree. orientation picks the wheel axis (Qt.Vertical by default).
func (doc *docState) wireWheelHandler(d *ui.DivEl, env *Env, exprs map[string]expr, stmts map[string][]stmt,
	st *inputHandlerState, run func([]stmt, string), targetExpr expr, hasTarget bool) {
	onWheel := stmts["onWheel"]
	var propRef Ref
	var prop string
	if e, ok := exprs["property"]; ok && hasTarget {
		if v, err := e.eval(env); err == nil {
			prop = str(v)
			if r, err := resolveRef(&memberExpr{base: targetExpr, name: prop}, env); err == nil {
				propRef = r
			}
		}
	}
	if onWheel == nil && propRef == nil {
		return
	}
	num := func(name string, def float64) float64 {
		if e, ok := exprs[name]; ok {
			if v, err := e.eval(env); err == nil {
				if f, err := toFloat64(v); err == nil {
					return f
				}
			}
		}
		return def
	}
	rotationScale := num("rotationScale", 1)
	mult := num("targetScaleMultiplier", wheelScaleMultiplier)
	horizontal := num("orientation", 2) == 1 // Qt.Horizontal is 1
	var cur float64
	if propRef != nil {
		if v, err := (&memberExpr{base: targetExpr, name: prop}).eval(env); err == nil {
			cur, _ = toFloat64(v)
		}
	}
	d.OnWheel(func(dx, dy float32) bool {
		toAngle := func(px float32) float64 { return -float64(px) / app.WheelNotchPx * 120 }
		ax, ay := toAngle(dx), toAngle(dy)
		deg := ay / 8
		if horizontal {
			deg = ax / 8
		}
		rot, _ := toFloat64(st.a.get())
		_ = st.a.set(rot + deg)
		if propRef != nil && deg != 0 {
			if prop == "scale" || prop == "zoomFactor" {
				cur *= math.Pow(mult, deg/15)
			} else {
				cur += deg * rotationScale
			}
			_ = propRef.set(cur)
		}
		if onWheel == nil {
			return true
		}
		ev := map[string]any{
			"angleDelta": map[string]any{"x": ax, "y": ay},
			"pixelDelta": map[string]any{"x": -float64(dx), "y": -float64(dy)},
			"modifiers":  float64(0), "inverted": false, "accepted": true,
		}
		runEvent(run, env, onWheel, "onWheel", "event", ev)
		accepted, _ := toBool(ev["accepted"])
		return accepted
	})
}

// pinchAxis is one thing a pinch changes on its target — scale
// (multiplied), rotation, x or y (added) — with its bounds and the
// active (this pinch) and persistent (all pinches) totals it reports.
type pinchAxis struct {
	ref                Ref // the target's property; nil: report only
	cur                float64
	lo, hi             float64
	hasLo, hasHi       bool
	mul                bool
	active, persistent Ref
}

// apply changes the axis by delta (a factor when mul) within its bounds and
// returns the change that got through.
func (a *pinchAxis) apply(delta float64) float64 {
	if a.ref != nil {
		next := a.cur + delta
		if a.mul {
			next = a.cur * delta
		}
		if a.hasHi && next > a.hi {
			next = a.hi
		}
		if a.hasLo && next < a.lo {
			next = a.lo
		}
		if a.mul {
			delta = next / a.cur
		} else {
			delta = next - a.cur
		}
		a.cur = next
		_ = a.ref.set(next)
	}
	av, _ := toFloat64(a.active.get())
	pv, _ := toFloat64(a.persistent.get())
	if a.mul {
		_ = a.active.set(av * delta)
		_ = a.persistent.set(pv * delta)
	} else {
		_ = a.active.set(av + delta)
		_ = a.persistent.set(pv + delta)
	}
	return delta
}

// wirePinchHandler: a pinch scales, turns and moves the target — scale
// within minimumScale … maximumScale (Qt 6's scaleAxis.*), rotation within
// minimumRotation … maximumRotation (rotationAxis.*), and x/y by the
// fingers' centre within xAxis/yAxis.minimum … maximum; each axis can be
// switched off with <axis>.enabled. It reports activeScale/persistentScale,
// activeRotation/persistentRotation, activeTranslation/
// persistentTranslation and centroid.position, and runs onScaleChanged,
// onRotationChanged (delta) and onTranslationChanged (delta.x/.y), and
// onActiveChanged. The scale is about the item's own origin, not the
// fingers' centre.
func (doc *docState) wirePinchHandler(d *ui.DivEl, env *Env, exprs map[string]expr, stmts map[string][]stmt,
	st *inputHandlerState, run func([]stmt, string), targetExpr expr, hasTarget bool, setActive func(bool)) {
	axis := func(prop string, mul bool, def float64, active, persistent Ref, enabled string, lo, hi []string) *pinchAxis {
		if !dragEnabled(exprs[enabled], env) {
			return nil
		}
		a := &pinchAxis{mul: mul, cur: def, active: active, persistent: persistent}
		for _, n := range lo {
			if v, ok := dragLimit(exprs[n], env); ok {
				a.lo, a.hasLo = float64(v), true
				break
			}
		}
		for _, n := range hi {
			if v, ok := dragLimit(exprs[n], env); ok {
				a.hi, a.hasHi = float64(v), true
				break
			}
		}
		if hasTarget {
			if r, err := resolveRef(&memberExpr{base: targetExpr, name: prop}, env); err == nil {
				a.ref = r
			}
			if v, err := (&memberExpr{base: targetExpr, name: prop}).eval(env); err == nil {
				if f, err := toFloat64(v); err == nil && (!mul || f > 0) {
					a.cur = f
				}
			}
		}
		return a
	}
	if !st.pinchInit { // the scale totals start at 1
		st.pinchInit = true
		_ = st.a.set(1.0)
	}
	scale := axis("scale", true, 1, st.a, st.b, "scaleAxis.enabled",
		[]string{"minimumScale", "scaleAxis.minimum"}, []string{"maximumScale", "scaleAxis.maximum"})
	rot := axis("rotation", false, 0, doc.handlerRef(st, "rot", 0), doc.handlerRef(st, "prot", 0), "rotationAxis.enabled",
		[]string{"minimumRotation", "rotationAxis.minimum"}, []string{"maximumRotation", "rotationAxis.maximum"})
	tx := axis("x", false, 0, doc.handlerRef(st, "tx", 0), doc.handlerRef(st, "ptx", 0), "xAxis.enabled",
		[]string{"xAxis.minimum"}, []string{"xAxis.maximum"})
	ty := axis("y", false, 0, doc.handlerRef(st, "ty", 0), doc.handlerRef(st, "pty", 0), "yAxis.enabled",
		[]string{"yAxis.minimum"}, []string{"yAxis.maximum"})
	cx, cy := doc.handlerRef(st, "cx", 0), doc.handlerRef(st, "cy", 0)
	d.OnPinch(func(e ui.PinchEvent) {
		_ = cx.set(float64(e.CX))
		_ = cy.set(float64(e.CY))
		switch e.Phase {
		case ui.DragStart:
			_ = st.a.set(1.0) // this pinch's totals start over
			for _, a := range []*pinchAxis{rot, tx, ty} {
				if a != nil {
					_ = a.active.set(0.0)
				}
			}
			setActive(true)
			return
		case ui.DragEnd:
			setActive(false)
			return
		}
		if scale != nil && e.Scale != 1 && e.Scale > 0 {
			f := scale.apply(float64(e.Scale))
			if body := stmts["onScaleChanged"]; body != nil {
				runEvent(run, env, body, "onScaleChanged", "delta", f)
			}
		}
		if rot != nil && e.Rotation != 0 {
			r := rot.apply(float64(e.Rotation))
			if body := stmts["onRotationChanged"]; body != nil {
				runEvent(run, env, body, "onRotationChanged", "delta", r)
			}
		}
		var dx, dy float64
		if tx != nil && e.DX != 0 {
			dx = tx.apply(float64(e.DX))
		}
		if ty != nil && e.DY != 0 {
			dy = ty.apply(float64(e.DY))
		}
		if body := stmts["onTranslationChanged"]; body != nil && (dx != 0 || dy != 0) {
			runEvent(run, env, body, "onTranslationChanged", "delta", map[string]any{"x": dx, "y": dy})
		}
	})
}

// handlerRef is one more mirrored value of a handler's state, by name.
func (doc *docState) handlerRef(st *inputHandlerState, name string, def float32) Ref {
	if st.more == nil {
		st.more = map[string]Ref{}
	}
	r := st.more[name]
	if r == nil {
		r = doc.floatProp(st.key+"."+name, def)
		st.more[name] = r
	}
	return r
}

// wirePointHandler: a press over the item makes the handler active and
// tracks the point — point.position and point.pressPosition in the item's
// coordinates, point.scenePosition in the window's — until release.
func (doc *docState) wirePointHandler(d *ui.DivEl, env *Env, stmts map[string][]stmt,
	st *inputHandlerState, run func([]stmt, string), setActive func(bool)) {
	onPoint := stmts["onPointChanged"]
	d.OnPointer(func(x, y float32, phase ui.DragPhase) {
		r := d.Rect()
		if phase == ui.DragStart {
			st.pressX, st.pressY = x, y
			_ = st.a.set(float64(x - r.X))
			_ = st.b.set(float64(y - r.Y))
			_ = st.c.set(float64(x - r.X))
			_ = st.d.set(float64(y - r.Y))
			setActive(true)
		}
		_ = st.a.set(float64(x - r.X))
		_ = st.b.set(float64(y - r.Y))
		_ = st.sceneX.set(float64(x))
		_ = st.sceneY.set(float64(y))
		if onPoint != nil {
			run(onPoint, "onPointChanged")
		}
		if phase == ui.DragEnd {
			setActive(false)
		}
	})
}

// exposeInputHandler puts a handler's mirrored state on its id object.
func (doc *docState) exposeInputHandler(h *node, st *inputHandlerState) {
	id := nodeID(h)
	if id == "" {
		return
	}
	obj := doc.elemObjs.get(doc.intern(id))
	if obj == nil {
		return
	}
	f := func(r Ref) float64 { v, _ := toFloat64(r.get()); return v }
	active, _ := toBool(st.active.get())
	obj.setProp("active", active)
	switch h.typeName {
	case "WheelHandler":
		obj.setProp("rotation", f(st.a))
	case "PinchHandler":
		obj.setProp("activeScale", f(st.a))
		obj.setProp("persistentScale", f(st.b))
		obj.setProp("scaleAxis", map[string]any{"activeValue": f(st.a), "persistentValue": f(st.b)})
		m := func(name string) float64 { return f(doc.handlerRef(st, name, 0)) }
		obj.setProp("activeRotation", m("rot"))
		obj.setProp("persistentRotation", m("prot"))
		obj.setProp("rotationAxis", map[string]any{"activeValue": m("rot"), "persistentValue": m("prot")})
		at := map[string]any{"x": m("tx"), "y": m("ty")}
		obj.setProp("activeTranslation", at)
		obj.setProp("translation", at)
		obj.setProp("persistentTranslation", map[string]any{"x": m("ptx"), "y": m("pty")})
		obj.setProp("centroid", map[string]any{"position": map[string]any{"x": m("cx"), "y": m("cy")}})
	case "PointHandler":
		obj.setProp("point", map[string]any{
			"position":      map[string]any{"x": f(st.a), "y": f(st.b)},
			"pressPosition": map[string]any{"x": f(st.c), "y": f(st.d)},
			"scenePosition": map[string]any{"x": f(st.sceneX), "y": f(st.sceneY)},
			"pressed":       active,
		})
	}
}
