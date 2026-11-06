package gml

import (
	"fmt"
	"strings"
	"time"

	"github.com/sushvibez/gfx/core"
)

// QML states & transitions. An Item declares named states; the active one is
// chosen by its `state:` expression. A State holds PropertyChanges that
// override properties on targeted (id'd) elements while it is active. A
// Transition's NumberAnimation makes the listed numeric properties tween to
// their new values instead of snapping.
//
//	Item {
//	    state: open ? "expanded" : ""
//	    states: [
//	        State { name: "expanded"
//	            PropertyChanges { target: panel; height: 200 }
//	        }
//	    ]
//	    transitions: [
//	        Transition { NumberAnimation { properties: "height"; duration: 250 } }
//	    ]
//	    Rectangle { id: panel; height: 40 }
//	}
//
// `when`-based state activation is not modeled; switch via `state:`.

// stateCtx is the active state/transition scope for a subtree: which
// properties to override per target id, and which numeric properties tween.
type stateCtx struct {
	overrides map[string][]attr // target id -> override attributes
	animated  map[string]bool   // property names that tween on change
	duration  time.Duration
	easing    core.Easing
}

// computeStateCtx reads a box's state/states/transitions attributes and
// returns the active scope, or nil when the box declares no states.
func computeStateCtx(n *node, env *Env, doc *docState) (*stateCtx, error) {
	var statesList, transList []*node
	var stateAttr *attr
	hasStates := false
	for _, a := range n.attrs {
		switch a.name {
		case "state":
			at := a
			stateAttr = &at
		case "states":
			hasStates = true
			ns, err := nodeList(a, env, "State")
			if err != nil {
				return nil, err
			}
			statesList = ns
		case "transitions":
			ns, err := nodeList(a, env, "Transition")
			if err != nil {
				return nil, err
			}
			transList = ns
		}
	}
	if !hasStates {
		return nil, nil
	}
	// QML `state` is live item state: readable and WRITABLE as id.state
	// (`stateBox.state = "expanded"`), with `state: <expr>` as its binding
	// until an imperative assignment breaks it. Back it with a persistent
	// string property so both directions work across per-frame rebuilds.
	active, err := activeState(n, stateAttr, env, doc)
	if err != nil {
		return nil, err
	}
	ctx := &stateCtx{
		overrides: map[string][]attr{},
		animated:  map[string]bool{},
		duration:  200 * time.Millisecond,
		easing:    core.EaseInOutQuad,
	}
	if err := collectOverrides(statesList, active, env, ctx); err != nil {
		return nil, err
	}
	if err := collectTransitions(transList, env, ctx); err != nil {
		return nil, err
	}
	return ctx, nil
}

// collectOverrides gathers the PropertyChanges of the active state into ctx,
// keyed by target id.
func collectOverrides(states []*node, active string, env *Env, ctx *stateCtx) error {
	for _, st := range states {
		name := ""
		for _, a := range st.attrs {
			if a.name == "name" {
				v, err := a.expr.eval(env)
				if err != nil {
					return err
				}
				name = str(v)
			}
		}
		if name != active {
			continue
		}
		for _, pc := range st.children {
			if pc.typeName != "PropertyChanges" {
				return fmt.Errorf("gml:%d: State contains only PropertyChanges, got %q", pc.line, pc.typeName)
			}
			target := ""
			var attrs []attr
			// Qt 6's qualified form names the target per property
			// (`morphRect.width: 100`) instead of with a `target:` line, and
			// one block may then address several elements.
			qualified := map[string][]attr{}
			for _, a := range pc.attrs {
				if a.name == "target" {
					id, ok := a.expr.(*identExpr)
					if !ok {
						return fmt.Errorf("gml:%d: PropertyChanges target wants an id", a.line)
					}
					target = id.name
					continue
				}
				if id, prop, ok := splitQualifiedProp(a.name); ok {
					q := a
					q.name = prop
					qualified[id] = append(qualified[id], q)
					continue
				}
				attrs = append(attrs, a)
			}
			for id, qa := range qualified {
				ctx.overrides[id] = append(ctx.overrides[id], qa...)
			}
			if target == "" {
				if len(attrs) == 0 {
					continue // fully qualified: every property named its target
				}
				return fmt.Errorf("gml:%d: PropertyChanges needs a target", pc.line)
			}
			ctx.overrides[target] = append(ctx.overrides[target], attrs...)
		}
	}
	return nil
}

// splitQualifiedProp splits `morphRect.width` into its target id and
// property. A grouped property (`font.pixelSize`, `anchors.fill`) is NOT a
// qualified target, so the known group prefixes are excluded.
func splitQualifiedProp(name string) (id, prop string, ok bool) {
	i := strings.IndexByte(name, '.')
	if i <= 0 || strings.IndexByte(name[i+1:], '.') >= 0 {
		return "", "", false
	}
	head := name[:i]
	switch head {
	case "font", "anchors", "border", "layer", "Layout", "background",
		"easing", "visibleArea", "palette", "axisX", "axisY", "axisZ":
		return "", "", false
	}
	// A target id starts lowercase like any QML id; a capitalized head is an
	// attached property (`Drag.active`).
	if head[0] >= 'A' && head[0] <= 'Z' {
		return "", "", false
	}
	return head, name[i+1:], true
}

// collectTransitions records which properties animate, and the longest
// duration / last easing across the transition list. Number, Color and
// Property animations all reduce to "this property tweens over d": the
// property's own type decides how it interpolates.
func collectTransitions(trans []*node, env *Env, ctx *stateCtx) error {
	for _, tr := range trans {
		for _, na := range tr.children {
			switch na.typeName {
			case "NumberAnimation", "ColorAnimation", "PropertyAnimation",
				"SmoothedAnimation", "SpringAnimation":
			case "ParallelAnimation", "SequentialAnimation", "PauseAnimation",
				"ScriptAction", "PropertyAction", "AnchorAnimation":
				// Grouping and side-effect actions: the tween itself is
				// what we model, so these contribute nothing.
				continue
			default:
				return fmt.Errorf("gml:%d: Transition animation %q is not modeled", na.line, na.typeName)
			}
			for _, a := range na.attrs {
				switch a.name {
				case "properties", "property":
					v, err := a.expr.eval(env)
					if err != nil {
						return err
					}
					for _, p := range strings.Split(str(v), ",") {
						if p = strings.TrimSpace(p); p != "" {
							ctx.animated[p] = true
						}
					}
				case "duration":
					f, err := attrFloat(a, env)
					if err != nil {
						return err
					}
					if d := time.Duration(f) * time.Millisecond; d > ctx.duration {
						ctx.duration = d
					}
				case "easing.type", "easing":
					v, err := a.expr.eval(env)
					if err != nil {
						return err
					}
					ctx.easing = easingByName(str(v))
				case "target", "targets", "from", "to", "loops", "alwaysRunToEnd",
					"velocity", "spring", "damping", "epsilon", "running", "id":
					// accepted, not modeled (we tween the live value)
				default:
					return fmt.Errorf("gml:%d: %s has no attribute %q", a.line, na.typeName, a.name)
				}
			}
		}
	}
	return nil
}

// nodeList evaluates a list-valued attribute into element nodes of the given
// type (State / Transition).
func nodeList(a attr, env *Env, typ string) ([]*node, error) {
	// Element-valued singular form: `states: State { ... }`.
	if a.elem != nil {
		if a.elem.typeName != typ {
			return nil, fmt.Errorf("gml:%d: %s wants %s objects", a.line, a.name, typ)
		}
		return []*node{a.elem}, nil
	}
	if a.expr == nil {
		return nil, nil
	}
	v, err := a.expr.eval(env)
	if err != nil {
		return nil, err
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("gml:%d: %s wants a list of %s", a.line, a.name, typ)
	}
	out := make([]*node, 0, len(items))
	for _, it := range items {
		n, ok := it.(*node)
		if !ok || n.typeName != typ {
			return nil, fmt.Errorf("gml:%d: %s list wants %s objects", a.line, a.name, typ)
		}
		out = append(out, n)
	}
	return out, nil
}

// numAttr routes a numeric box property through a retained tween when the
// active state animates it: changing the target value starts a tween from
// the current value, and the displayed value is read back each frame. When
// no transition covers the property (or the element has no id) it returns
// the target unchanged.
func (doc *docState) numAttr(id, prop string, target float32) float32 {
	// State-transition animation (driven by the active state's Transition).
	if doc.state != nil && id != "" && doc.state.animated[prop] {
		return doc.tweenTo(id+"|"+prop, target, doc.state.duration, doc.state.easing)
	}
	// Behavior on <prop> animation (driven by the element's own Behavior child).
	if doc.curBehaviors != nil {
		if spec, ok := doc.curBehaviors[prop]; ok {
			return doc.tweenTo("beh:"+doc.curKey+"|"+prop, target, spec.dur, spec.easing)
		}
	}
	return target
}

// tweenTo retains a per-key tween: the first call snaps to target; later calls
// start a tween whenever target changes, returning the in-flight value.
func (doc *docState) tweenTo(key string, target float32, dur time.Duration, easing core.Easing) float32 {
	p := doc.anims[key]
	if p == nil {
		p = &core.Property[float32]{}
		p.Set(target)
		doc.anims[key] = p
		doc.animTarget[key] = target
		return target
	}
	if doc.animTarget[key] != target {
		core.Tween(p, target, dur, easing)
		doc.animTarget[key] = target
	}
	return p.Get()
}

// collectBehaviors reads a node's `Behavior on <prop>` children into a map of
// prop -> tween spec (duration/easing from the contained animation element).
func collectBehaviors(n *node, env *Env) map[string]behaviorSpec {
	var m map[string]behaviorSpec
	for _, c := range n.children {
		if c.typeName != "Behavior" || c.onTarget == "" {
			continue
		}
		spec := behaviorSpec{dur: 200 * time.Millisecond, easing: core.EaseInOutQuad}
		readAnimTiming(c, env, &spec)     // Behavior may carry duration directly
		for _, anim := range c.children { // or in its NumberAnimation child
			readAnimTiming(anim, env, &spec)
		}
		if m == nil {
			m = map[string]behaviorSpec{}
		}
		m[c.onTarget] = spec
	}
	return m
}

// readAnimTiming pulls duration / easing.type off an animation (or Behavior)
// node into spec, leaving unset fields untouched.
func readAnimTiming(n *node, env *Env, spec *behaviorSpec) {
	for _, a := range n.attrs {
		switch a.name {
		case "duration":
			if f, err := attrFloat(a, env); err == nil {
				spec.dur = time.Duration(f) * time.Millisecond
			}
		case "easing.type", "easing":
			if v, err := a.expr.eval(env); err == nil {
				spec.easing = easingByName(str(v))
			}
		}
	}
}

// easingByName maps QML's `easing.type: Easing.<Name>` to its curve. Every
// name QEasingCurve defines is here: with only four of them mapped, four
// differently-eased animations moved as one.
var easingCurves = map[string]core.Easing{
	"Linear":          core.EaseLinear,
	"InQuad":          core.EaseInQuad,
	"OutQuad":         core.EaseOutQuad,
	"InOutQuad":       core.EaseInOutQuad,
	"OutInQuad":       func(t float32) float32 { return outIn(t, core.EaseOutQuad, core.EaseInQuad) },
	"InCubic":         core.EaseInCubic,
	"OutCubic":        core.EaseOutCubic,
	"InOutCubic":      core.EaseInOutCubic,
	"OutInCubic":      func(t float32) float32 { return outIn(t, core.EaseOutCubic, core.EaseInCubic) },
	"InQuart":         core.EaseInQuart,
	"OutQuart":        core.EaseOutQuart,
	"InOutQuart":      core.EaseInOutQuart,
	"OutInQuart":      func(t float32) float32 { return outIn(t, core.EaseOutQuart, core.EaseInQuart) },
	"InQuint":         core.EaseInQuint,
	"OutQuint":        core.EaseOutQuint,
	"InOutQuint":      core.EaseInOutQuint,
	"OutInQuint":      func(t float32) float32 { return outIn(t, core.EaseOutQuint, core.EaseInQuint) },
	"InSine":          core.EaseInSine,
	"OutSine":         core.EaseOutSine,
	"InOutSine":       core.EaseInOutSine,
	"OutInSine":       func(t float32) float32 { return outIn(t, core.EaseOutSine, core.EaseInSine) },
	"InExpo":          core.EaseInExpo,
	"OutExpo":         core.EaseOutExpo,
	"InOutExpo":       core.EaseInOutExpo,
	"OutInExpo":       func(t float32) float32 { return outIn(t, core.EaseOutExpo, core.EaseInExpo) },
	"InCirc":          core.EaseInCirc,
	"OutCirc":         core.EaseOutCirc,
	"InOutCirc":       core.EaseInOutCirc,
	"OutInCirc":       func(t float32) float32 { return outIn(t, core.EaseOutCirc, core.EaseInCirc) },
	"InElastic":       core.EaseInElastic,
	"OutElastic":      core.EaseOutElastic,
	"InOutElastic":    core.EaseInOutElastic,
	"OutInElastic":    func(t float32) float32 { return outIn(t, core.EaseOutElastic, core.EaseInElastic) },
	"InBack":          core.EaseInBack,
	"OutBack":         core.EaseOutBack,
	"InOutBack":       core.EaseInOutBack,
	"OutInBack":       func(t float32) float32 { return outIn(t, core.EaseOutBack, core.EaseInBack) },
	"InBounce":        core.EaseInBounce,
	"OutBounce":       core.EaseOutBounce,
	"InOutBounce":     core.EaseInOutBounce,
	"OutInBounce":     func(t float32) float32 { return outIn(t, core.EaseOutBounce, core.EaseInBounce) },
	"InCurve":         core.EaseInSine,  // Qt's curve pair, approximated
	"OutCurve":        core.EaseOutSine, // by their sine counterparts
	"SineCurve":       func(t float32) float32 { return core.EaseInOutSine(t) },
	"CosineCurve":     func(t float32) float32 { return 1 - core.EaseInOutSine(t) },
	"Bezier":          core.EaseInOutQuad, // needs the curve's own control points
	"BezierSpline":    core.EaseInOutQuad,
	"TCBSpline":       core.EaseInOutQuad,
	"InOutBackSpline": core.EaseInOutBack,
}

// outIn is QEasingCurve's OutIn* family: the "out" half first, then the
// "in" half, each over half the run.
func outIn(t float32, out, in core.Easing) float32 {
	if t < 0.5 {
		return out(2*t) / 2
	}
	return in(2*t-1)/2 + 0.5
}

func easingByName(s string) core.Easing {
	if e, ok := easingCurves[s]; ok {
		return e
	}
	return core.EaseInOutQuad
}

// activeState resolves an item's current state name and publishes the
// backing property as id.state.
func activeState(n *node, stateAttr *attr, env *Env, doc *docState) (string, error) {
	var ref Ref
	if stateAttr != nil {
		// `state: someStringProperty` — that property IS the state.
		if r, err := resolveRef(stateAttr.expr, env); err == nil {
			ref = r
		}
	}
	if ref == nil {
		key := doc.key(n) + ".state"
		raw := doc.strProp(key, "")
		if stateAttr != nil && !doc.assigned[key] {
			v, err := stateAttr.expr.eval(env)
			if err != nil {
				return "", err
			}
			if str(raw.get()) != str(v) {
				_ = raw.set(v)
			}
		}
		ref = trackedRef{Ref: raw, doc: doc, name: key}
	}
	doc.exposeIDProp(n, "state", ref)
	return str(ref.get()), nil
}
