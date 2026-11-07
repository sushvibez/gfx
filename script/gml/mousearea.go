package gml

import (
	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// MouseArea's gestures beyond click, hover and press: onDoubleClicked,
// onPressAndHold, onWheel, onPositionChanged and the live mouseX/mouseY,
// on the ui layer's DivEl gesture handlers (ui/pointer.go). A MouseArea's
// handlers are hoisted onto the box that holds it, like its onClicked.

// mouseGestures are the handler bodies boxAttrs collected.
type mouseGestures struct {
	doubleClicked, pressAndHold, wheel, positionChanged []stmt
	hoverEnabled                                        bool
}

func (g *mouseGestures) any() bool {
	return g.doubleClicked != nil || g.pressAndHold != nil || g.wheel != nil || g.positionChanged != nil
}

// wireMouseGestures installs the collected handlers on d and keeps
// id.mouseX / id.mouseY live. mouseX/mouseY follow the pointer while it is
// over the area (QML updates them only while pressed unless hoverEnabled;
// reading them on hover is harmless), and onPositionChanged runs when
// they change while pressed or, with hoverEnabled, while hovered.
func (doc *docState) wireMouseGestures(n *node, d *ui.DivEl, env *Env, g *mouseGestures, pressRef Ref) {
	ks := doc.keyStr(doc.keyID(n))
	mx, my := doc.floatProp(ks+".mouseX", 0), doc.floatProp(ks+".mouseY", 0)
	doc.exposeIDProp(n, "mouseX", mx)
	doc.exposeIDProp(n, "mouseY", my)
	run := doc.handlerRunner(env)
	mouse := func() map[string]any {
		x, _ := toFloat64(mx.get())
		y, _ := toFloat64(my.get())
		return map[string]any{"x": x, "y": y, "button": float64(1), "buttons": float64(1),
			"modifiers": float64(0), "wasHeld": false, "accepted": true}
	}
	d.OnHoverMove(func(x, y float32) {
		ox, _ := toFloat32(mx.get())
		oy, _ := toFloat32(my.get())
		if ox == x && oy == y {
			return
		}
		_ = mx.set(x)
		_ = my.set(y)
		pressed, _ := toBool(pressRef.get())
		if g.positionChanged != nil && (pressed || g.hoverEnabled) {
			runEvent(run, env, g.positionChanged, "onPositionChanged", "mouse", mouse())
		}
	})
	if g.doubleClicked != nil {
		d.OnDoubleClick(func() {
			runEvent(run, env, g.doubleClicked, "onDoubleClicked", "mouse", mouse())
		})
	}
	if g.pressAndHold != nil {
		d.OnPressAndHold(func() {
			m := mouse()
			m["wasHeld"] = true
			runEvent(run, env, g.pressAndHold, "onPressAndHold", "mouse", m)
		})
	}
	if g.wheel != nil {
		d.OnWheel(func(dx, dy float32) bool {
			// Qt's angleDelta is eighths of a degree, 120 to a notch, and
			// positive when the wheel turns AWAY from the user; ours is
			// pixels, positive toward the user (app.MouseEvent).
			toAngle := func(px float32) float64 { return -float64(px) / app.WheelNotchPx * 120 }
			w := map[string]any{
				"angleDelta": map[string]any{"x": toAngle(dx), "y": toAngle(dy)},
				"pixelDelta": map[string]any{"x": -float64(dx), "y": -float64(dy)},
				"modifiers":  float64(0), "inverted": false, "accepted": true,
			}
			if x, err := toFloat64(mx.get()); err == nil {
				w["x"] = x
			}
			if y, err := toFloat64(my.get()); err == nil {
				w["y"] = y
			}
			runEvent(run, env, g.wheel, "onWheel", "wheel", w)
			accepted, _ := toBool(w["accepted"])
			return accepted
		})
	}
}

// runEvent runs a handler with its event object bound under name (mouse,
// wheel, event, delta). A handler written as a function value — `onWheel: (wheel) =>
// { … }`, `function(ev) { … }` — is called with the object instead, under
// whatever name it declares.
func runEvent(run func([]stmt, string), env *Env, stmts []stmt, what, name string, obj any) {
	if len(stmts) == 1 {
		if es, ok := stmts[0].(*exprStmt); ok {
			if fe, ok := es.e.(*funcExpr); ok {
				fn, err := fe.eval(env)
				if err == nil {
					if _, err := callAny(fn, []any{obj}); err != nil {
						logf("gml: %s: %v", what, err)
					}
					return
				}
			}
		}
	}
	prev, had := env.vars[name]
	env.vars[name] = obj
	run(stmts, what)
	if had {
		env.vars[name] = prev
	} else {
		delete(env.vars, name)
	}
}

// attrIsTrue reports whether n has attribute name and it evaluates true.
func attrIsTrue(n *node, name string, env *Env) bool {
	for _, a := range n.attrs {
		if a.name == name && a.expr != nil {
			if v, err := a.expr.eval(env); err == nil {
				b, _ := toBool(v)
				return b
			}
		}
	}
	return false
}
