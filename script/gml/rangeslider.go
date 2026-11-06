package gml

// Qt Quick Controls' RangeSlider: one track, two handles, and a `first` /
// `second` sub-object each carrying its own value, position and pressed
// state. Pages read those back (`priceRange.first.value.toFixed(0)`), so the
// control publishes them under its id every frame.
//
// The handles own their values: `first.value: 20` seeds them once and a
// drag writes them, clamped so the handles cannot cross.

import (
	"fmt"

	"github.com/sushvibez/gfx/ui"
)

func buildRangeSlider(n *node, env *Env, doc *docState) (ui.Element, error) {
	w := float32(200)
	from, to := float32(0), float32(1)
	firstSeed, secondSeed := from, to
	seeded := [2]bool{}
	vertical := false
	var step float32
	var snap bool
	var onMoved []stmt
	for _, a := range n.attrs {
		switch a.name {
		case "stepSize":
			if f, err := attrFloat(a, env); err == nil {
				step = f
			}
		case "snapMode":
			if v, err := a.expr.eval(env); err == nil {
				snap = str(v) == "SnapAlways"
			}
		case "id", "enabled", "live", "wheelEnabled",
			"touchDragThreshold", "focusPolicy", "opacity", "visible", "z",
			"height", "implicitHeight", "background", "handle", "padding",
			"first.onMoved", "second.onMoved", "hoverEnabled":
			// accepted; sizing and chrome are applied below or not modeled
		case "width", "implicitWidth":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "from":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			from = f
		case "to":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			to = f
		case "first.value":
			if f, err := attrFloat(a, env); err == nil {
				firstSeed, seeded[0] = f, true
			}
		case "second.value":
			if f, err := attrFloat(a, env); err == nil {
				secondSeed, seeded[1] = f, true
			}
		case "orientation":
			if v, err := a.expr.eval(env); err == nil {
				if f, err := toFloat64(v); err == nil {
					vertical = int(f) == 2 // Qt.Vertical
				} else {
					vertical = containsFold(str(v), "Vertical")
				}
			}
		case "onMoved", "first.onValueChanged", "second.onValueChanged":
			onMoved = append(onMoved, a.stmt...)
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: RangeSlider has no attribute %q", a.line, a.name)
		}
	}
	if !seeded[0] {
		firstSeed = from
	}
	if !seeded[1] {
		secondSeed = to
	}
	key := doc.key(n)
	firstRef := doc.floatProp(key+".first.value", firstSeed)
	secondRef := doc.floatProp(key+".second.value", secondSeed)
	fp, _, okF := propOf[float32](firstRef)
	sp, _, okS := propOf[float32](secondRef)
	if !okF || !okS {
		return nil, fmt.Errorf("gml:%d: RangeSlider values want real properties", n.line)
	}
	publishRangeHandles(n, doc, firstRef, secondRef, from, to)

	el := ui.RangeSlider(fp.p, sp.p, w).Range(from, to).Step(step, snap)
	if vertical {
		el.Vertical()
	}
	if len(onMoved) > 0 {
		run := doc.handlerRunner(env)
		stmts := onMoved
		el.OnMoved(func() { run(stmts, "onMoved") })
	}
	return el, nil
}

// publishRangeHandles exposes the QML contract a page binds against:
// id.first.value / id.second.value plus each handle's position and state.
func publishRangeHandles(n *node, doc *docState, first, second Ref, from, to float32) {
	pos := func(r Ref) float64 {
		v, _ := toFloat32(r.get())
		if to == from {
			return 0
		}
		return float64(min(max((v-from)/(to-from), 0), 1))
	}
	handle := func(r Ref) map[string]any {
		v, _ := toFloat64(r.get())
		return map[string]any{
			"value": v, "position": pos(r), "visualPosition": pos(r),
			"pressed": false, "hovered": false,
		}
	}
	doc.exposeIDProp(n, "first", handle(first))
	doc.exposeIDProp(n, "second", handle(second))
	doc.exposeIDProp(n, "from", float64(from))
	doc.exposeIDProp(n, "to", float64(to))
}
