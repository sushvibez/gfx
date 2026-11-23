package gml

import (
	"fmt"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// numberFieldState is one NumberField's retained value and edit state.
type numberFieldState struct {
	st        *ui.NumberState
	v         core.Property[float64]
	seeded    bool    // the field took its first value
	lastBound float64 // the binding's value at the last seed
}

// buildNumberField is gfx's Blender-style number (ui.NumberField), not a
// Qt type: drag sideways to scrub, click to type, arrows to step.
//
//	NumberField { value: wall.height; from: 0.5; to: 6; stepSize: 0.1
//	              decimals: 2; label: "Height"; unit: "m"; width: 220
//	              onValueModified: setHeight(value)   // live, every change
//	              onDragFinished: commitHeight() }    // once a drag lets go
//
// value is a one-way binding, re-read every build unless the field is
// being typed into (only when the bound number changes: a constant binding is
// the initial value, and the user's changes stand, like a SpinBox's); onValueModified sees the new number as
// `value`, and id.value reads it from elsewhere in the document.
func buildNumberField(n *node, env *Env, doc *docState) (ui.Element, error) {
	o := ui.NumberOpts{Decimals: -1}
	var onModified, onDragEnd []stmt
	bound, enabled, hasValue := 0.0, true, false
	var from, to float64
	hasFrom, hasTo := false, false
	for _, a := range n.attrs {
		switch a.name {
		case "id", "visible", "opacity", "x", "y", "height", "focus", "focusPolicy":
		case "value", "from", "to", "stepSize", "width", "decimals", "pixelsPerStep":
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			f, err := toFloat64(v)
			if err != nil {
				if a.name == "value" { // a value the host has not filled in yet
					continue
				}
				return nil, fmt.Errorf("gml:%d: NumberField %s wants a number: %v", a.line, a.name, err)
			}
			switch a.name {
			case "value":
				bound, hasValue = f, true
			case "from":
				from, hasFrom = f, true
			case "to":
				to, hasTo = f, true
			case "stepSize":
				o.Step = f
			case "width":
				o.Width = float32(f)
			case "decimals":
				o.Decimals = int(f)
			case "pixelsPerStep":
				o.PixelsPerStep = float32(f)
			}
		case "label", "unit":
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			if a.name == "label" {
				o.Label = str(shown(v))
			} else {
				o.Unit = str(shown(v))
			}
		case "enabled":
			b, err := attrBool(a, env)
			if err != nil {
				return nil, err
			}
			enabled = b
		case "onValueModified":
			onModified = a.stmt
		case "onDragFinished":
			onDragEnd = a.stmt
		default:
			return nil, fmt.Errorf("gml:%d: NumberField has no attribute %q", a.line, a.name)
		}
	}
	if hasFrom && hasTo {
		o.Min, o.Max = from, to
	}
	o.Disabled = !enabled
	key := doc.key(n)
	if doc.numbers == nil {
		doc.numbers = map[string]*numberFieldState{}
	}
	ns := doc.numbers[key]
	if ns == nil {
		ns = &numberFieldState{st: ui.NewNumberState()}
		doc.numbers[key] = ns
	}
	// Re-seed when the binding itself changed (or on the first build), so
	// a constant `value: 20` does not undo the user's change every frame
	// while a host-driven value still lands (QML's binding semantics).
	if !ns.st.Editing() && (!ns.seeded || (hasValue && bound != ns.lastBound)) {
		ns.v.Set(bound)
	}
	ns.seeded, ns.lastBound = true, bound
	// id.value reads the field's current number (a dialog's OK button).
	doc.exposeIDProp(n, "value", computedRef{fn: func() any { return ns.v.Get() }})
	run := doc.handlerRunner(env)
	withValue := func(stmts []stmt, what string) func() {
		if stmts == nil {
			return nil
		}
		return func() {
			prev, had := env.vars["value"]
			env.vars["value"] = ns.v.Get()
			run(stmts, what)
			if had {
				env.vars["value"] = prev
			} else {
				delete(env.vars, "value")
			}
		}
	}
	o.OnModified = withValue(onModified, "onValueModified")
	o.OnDragEnd = withValue(onDragEnd, "onDragFinished")
	return ui.NumberField(&ns.v, ns.st, o), nil
}
