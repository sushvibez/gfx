package gml

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sushvibez/gfx/core"
)

// QML states & transitions. An Item declares named states; the active one is
// chosen by its `state:` expression, or, without one, by the first state
// whose `when:` holds. A State holds PropertyChanges that override
// properties on targeted (id'd) elements while it is active, AnchorChanges
// that re-anchor them, and StateChangeScripts that run when it is entered;
// `extend: "other"` takes another state's changes first. On a state change
// the first Transition whose from/to match (comma lists, "*" for any;
// `reversible` also matches the change the other way) animates: each of its
// animations â nested in Parallel/SequentialAnimation or not â tweens the
// properties it lists, on its targets (all, when it names none), over its
// own duration and easing. The first state an item shows is not animated.
//
//	Item {
//	    states: [
//	        State { name: "expanded"; when: open
//	            PropertyChanges { target: panel; height: 200 }
//	        }
//	    ]
//	    transitions: [
//	        Transition { from: ""; to: "expanded"; reversible: true
//	            NumberAnimation { properties: "height"; duration: 250 } }
//	    ]
//	    Rectangle { id: panel; height: 40 }
//	}
//
// ParentChange moves its target under another item while the state is
// active (both inside the item that declares the states). A
// SequentialAnimation's parts run together rather than one after another.

// stateCtx is the active state/transition scope for a subtree: which
// properties to override per target id, and which of them tween, how.
type stateCtx struct {
	overrides map[string][]attr // target id -> override attributes
	anims     []animSpec        // the running transition's animations
	// reparent maps a ParentChange's target id to its new parent's id.
	reparent map[string]string
	// tok changes with the active state: an element that consults the
	// overrides reads it (overridesFor), so the subtree cache rebuilds the
	// state's targets when it changes instead of serving them stale.
	tok *core.Property[uint64]
}

// overridesFor is the active state's changes to the element id, recording
// the read.
func (c *stateCtx) overridesFor(id string) []attr {
	if c.tok != nil {
		c.tok.Get()
	}
	return c.overrides[id]
}

// animSpec is one animation of a transition: the properties it tweens, on
// which targets (nil: any), over how long, with what easing.
type animSpec struct {
	props   map[string]bool
	targets map[string]bool
	dur     time.Duration
	easing  core.Easing
}

// animFor is the animation that tweens prop on the element id, or nil.
func (c *stateCtx) animFor(id, prop string) *animSpec {
	for i := range c.anims {
		a := &c.anims[i]
		if a.props[prop] && (a.targets == nil || a.targets[id]) {
			return a
		}
	}
	return nil
}

// stateDecl is one State element, read.
type stateDecl struct {
	node        *node
	name, extnd string
	when, hasW  bool
}

// computeStateCtx reads a box's state/states/transitions attributes and
// returns the active scope, or nil when the box declares no states. It
// notices a state change (once per change, however many times a build asks)
// to pick the transition and run the entered state's scripts.
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
	decls, err := readStates(statesList, env)
	if err != nil {
		return nil, err
	}
	// With no `state:` binding, the first state whose `when` holds is the
	// active one ("" when none does).
	whenName, hasWhen := "", false
	for _, d := range decls {
		if d.hasW {
			hasWhen = true
			if d.when {
				whenName = d.name
				break
			}
		}
	}
	// QML `state` is live item state: readable and WRITABLE as id.state
	// (`stateBox.state = "expanded"`), with `state: <expr>` (or the when
	// clauses) as its binding until an imperative assignment breaks it.
	active, err := activeState(n, stateAttr, hasWhen, whenName, env, doc)
	if err != nil {
		return nil, err
	}
	ctx := &stateCtx{overrides: map[string][]attr{}}
	var scripts []*node
	if err := collectOverrides(decls, active, env, ctx, &scripts, 0); err != nil {
		return nil, err
	}
	key := doc.key(n)
	if doc.stateSeen == nil {
		doc.stateSeen = map[string]string{}
		doc.stateAnims = map[string][]animSpec{}
	}
	seen, had := doc.stateSeen[key]
	tok := doc.stateToks[key]
	if tok == nil {
		tok = &core.Property[uint64]{}
		if doc.stateToks == nil {
			doc.stateToks = map[string]*core.Property[uint64]{}
		}
		doc.stateToks[key] = tok
	}
	ctx.tok = tok
	if !had || seen != active {
		doc.stateSeen[key] = active
		if had {
			tok.Set(tok.Get() + 1) // the targets rebuild (overridesFor)
		}
		if had {
			anims, err := pickTransition(transList, seen, active, env)
			if err != nil {
				return nil, err
			}
			doc.stateAnims[key] = anims
			runStateScripts(scripts, env, doc)
		} else {
			delete(doc.stateAnims, key) // the first state shown is not animated
		}
	}
	ctx.anims = doc.stateAnims[key]
	return ctx, nil
}

// readStates reads each State's name, extend and when.
func readStates(states []*node, env *Env) ([]stateDecl, error) {
	out := make([]stateDecl, 0, len(states))
	for _, st := range states {
		d := stateDecl{node: st}
		for _, a := range st.attrs {
			if a.expr == nil {
				continue
			}
			switch a.name {
			case "name", "extend":
				v, err := a.expr.eval(env)
				if err != nil {
					return nil, err
				}
				if a.name == "name" {
					d.name = str(v)
				} else {
					d.extnd = str(v)
				}
			case "when":
				d.hasW = true
				if v, err := a.expr.eval(env); err == nil {
					d.when, _ = toBool(v)
				}
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// collectOverrides gathers the changes of the state named active into ctx,
// keyed by target id â those of the state it extends first, so its own
// win â and the StateChangeScripts it holds.
func collectOverrides(decls []stateDecl, active string, env *Env, ctx *stateCtx, scripts *[]*node, depth int) error {
	if active == "" || depth > 8 { // "" is the base state; 8 bounds an extend cycle
		return nil
	}
	for _, d := range decls {
		if d.name != active {
			continue
		}
		if d.extnd != "" {
			if err := collectOverrides(decls, d.extnd, env, ctx, scripts, depth+1); err != nil {
				return err
			}
		}
		for _, pc := range d.node.children {
			switch pc.typeName {
			case "PropertyChanges", "AnchorChanges":
			case "StateChangeScript":
				*scripts = append(*scripts, pc)
				continue
			case "ParentChange":
				// Moves its target under another item (see movedAway / movedInto);
				// its other attributes (x, y, width, …) apply there.
			default:
				return fmt.Errorf("gml:%d: State contains PropertyChanges, AnchorChanges or StateChangeScript, got %q", pc.line, pc.typeName)
			}
			target := ""
			var attrs []attr
			newParent := ""
			// Qt 6's qualified form names the target per property
			// (`morphRect.width: 100`) instead of with a `target:` line, and
			// one block may then address several elements.
			qualified := map[string][]attr{}
			for _, a := range pc.attrs {
				switch a.name {
				case "target":
					id, ok := a.expr.(*identExpr)
					if !ok {
						return fmt.Errorf("gml:%d: %s target wants an id", a.line, pc.typeName)
					}
					target = id.name
					continue
				case "explicit", "restoreEntryValues":
					continue // values are re-evaluated every build either way
				case "parent":
					if pc.typeName == "ParentChange" {
						if id, ok := a.expr.(*identExpr); ok {
							newParent = id.name
						}
						continue
					}
				}
				if pc.typeName == "PropertyChanges" {
					if id, prop, ok := splitQualifiedProp(a.name); ok {
						q := a
						q.name = prop
						qualified[id] = append(qualified[id], q)
						continue
					}
				}
				// AnchorChanges writes anchors.left etc. as the anchor
				// attributes they are; `undefined` releases an edge.
				attrs = append(attrs, a)
			}
			for id, qa := range qualified {
				ctx.overrides[id] = append(ctx.overrides[id], qa...)
			}
			if target == "" {
				if len(attrs) == 0 {
					continue // fully qualified: every property named its target
				}
				return fmt.Errorf("gml:%d: %s needs a target", pc.line, pc.typeName)
			}
			ctx.overrides[target] = append(ctx.overrides[target], attrs...)
			if newParent != "" {
				if ctx.reparent == nil {
					ctx.reparent = map[string]string{}
				}
				ctx.reparent[target] = newParent
			}
		}
		return nil
	}
	return nil
}

// runStateScripts runs the entered state's StateChangeScripts.
func runStateScripts(scripts []*node, env *Env, doc *docState) {
	if len(scripts) == 0 {
		return
	}
	run := doc.handlerRunner(env)
	for _, sc := range scripts {
		for _, a := range sc.attrs {
			if a.name != "script" {
				continue
			}
			switch {
			case a.stmt != nil:
				run(a.stmt, "StateChangeScript")
			case a.expr != nil:
				if _, err := a.expr.eval(env); err != nil {
					logf("gml:%d: StateChangeScript: %v", a.line, err)
				}
			}
		}
	}
}

// pickTransition is the animations of the first Transition matching a
// change from one state to another (or, reversible, the other way).
func pickTransition(trans []*node, from, to string, env *Env) ([]animSpec, error) {
	for _, tr := range trans {
		f, t, rev := "*", "*", false
		for _, a := range tr.attrs {
			if a.expr == nil {
				continue
			}
			switch a.name {
			case "from", "to":
				v, err := a.expr.eval(env)
				if err != nil {
					return nil, err
				}
				if a.name == "from" {
					f = str(v)
				} else {
					t = str(v)
				}
			case "reversible":
				if v, err := a.expr.eval(env); err == nil {
					rev, _ = toBool(v)
				}
			}
		}
		if !(stateMatch(f, from) && stateMatch(t, to)) && !(rev && stateMatch(f, to) && stateMatch(t, from)) {
			continue
		}
		var out []animSpec
		if err := collectAnims(tr.children, env, nil, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, nil
}

// stateMatch reads a Transition's from/to: "*" matches any state, else a
// comma-separated list of names.
func stateMatch(spec, state string) bool {
	for _, s := range strings.Split(spec, ",") {
		if s = strings.TrimSpace(s); s == "*" || s == state {
			return true
		}
	}
	return false
}

// collectAnims reads a transition's animations, walking into Parallel and
// Sequential groups (whose targets their parts inherit).
func collectAnims(nodes []*node, env *Env, targets map[string]bool, out *[]animSpec) error {
	for _, na := range nodes {
		switch na.typeName {
		case "ParallelAnimation", "SequentialAnimation":
			if err := collectAnims(na.children, env, animTargets(na, targets), out); err != nil {
				return err
			}
			continue
		case "PauseAnimation", "ScriptAction", "PropertyAction":
			continue // side effects and gaps: the tweens are what is modelled
		case "NumberAnimation", "ColorAnimation", "PropertyAnimation", "RotationAnimation",
			"SmoothedAnimation", "SpringAnimation", "AnchorAnimation":
		default:
			return fmt.Errorf("gml:%d: Transition animation %q is not modeled", na.line, na.typeName)
		}
		spec := animSpec{props: map[string]bool{}, targets: animTargets(na, targets),
			dur: 250 * time.Millisecond, easing: core.EaseLinear}
		if na.typeName == "AnchorAnimation" {
			// Animates whatever an AnchorChanges moved.
			for _, p := range []string{"x", "y", "width", "height"} {
				spec.props[p] = true
			}
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
						spec.props[p] = true
					}
				}
			case "duration":
				f, err := attrFloat(a, env)
				if err != nil {
					return err
				}
				spec.dur = time.Duration(f) * time.Millisecond
			case "easing.type", "easing":
				v, err := a.expr.eval(env)
				if err != nil {
					return err
				}
				spec.easing = easingByName(str(v))
			case "target", "targets", "from", "to", "loops", "alwaysRunToEnd",
				"velocity", "spring", "damping", "epsilon", "running", "id",
				"direction", "easing.amplitude", "easing.overshoot", "easing.period":
				// targets are read by animTargets; the rest are accepted
			default:
				return fmt.Errorf("gml:%d: %s has no attribute %q", a.line, na.typeName, a.name)
			}
		}
		*out = append(*out, spec)
	}
	return nil
}

// animTargets is an animation's target/targets as a set of ids, or the
// enclosing group's when it names none.
func animTargets(na *node, inherited map[string]bool) map[string]bool {
	var set map[string]bool
	add := func(e expr) {
		if id, ok := e.(*identExpr); ok {
			if set == nil {
				set = map[string]bool{}
			}
			set[id.name] = true
		}
	}
	for _, a := range na.attrs {
		if (a.name != "target" && a.name != "targets") || a.expr == nil {
			continue
		}
		if l, ok := a.expr.(*listExpr); ok {
			for _, e := range l.items {
				add(e)
			}
			continue
		}
		add(a.expr)
	}
	if set == nil {
		return inherited
	}
	return set
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
	if v, ok := doc.stateTween(id, prop, target); ok {
		return v
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
func activeState(n *node, stateAttr *attr, hasWhen bool, whenName string, env *Env, doc *docState) (string, error) {
	var ref Ref
	if stateAttr != nil {
		// `state: someStringProperty` â that property IS the state.
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
		} else if hasWhen && !doc.assigned[key] {
			// No `state:` binding: the when clauses are its binding.
			if str(raw.get()) != whenName {
				_ = raw.set(whenName)
			}
		}
		ref = trackedRef{Ref: raw, doc: doc, name: key}
	}
	doc.exposeIDProp(n, "state", ref)
	return str(ref.get()), nil
}

// stateTween routes a property through the running transition's tween when
// one of its animations covers prop on the element id.
func (doc *docState) stateTween(id, prop string, target float32) (float32, bool) {
	if doc.state == nil || id == "" {
		return target, false
	}
	if doc.state.tok != nil {
		doc.state.tok.Get() // a change starts a tween here: be rebuilt for it
	}
	a := doc.state.animFor(id, prop)
	if a == nil {
		return target, false
	}
	return doc.tweenTo(id+"|"+prop, target, a.dur, a.easing), true
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

// withoutAnchors leaves out an override list's anchors.* attributes.
func withoutAnchors(ov []attr) []attr {
	for i := range ov {
		if strings.HasPrefix(ov[i].name, "anchors.") {
			out := make([]attr, 0, len(ov))
			for _, a := range ov {
				if !strings.HasPrefix(a.name, "anchors.") {
					out = append(out, a)
				}
			}
			return out
		}
	}
	return ov
}

// movedAway reports whether the active state's ParentChange has taken the
// child c out from under parent.
func (c *stateCtx) movedAway(child, parent *node) bool {
	if c == nil || len(c.reparent) == 0 {
		return false
	}
	id := nodeID(child)
	if id == "" {
		return false
	}
	if c.tok != nil {
		c.tok.Get()
	}
	np, ok := c.reparent[id]
	return ok && np != nodeID(parent)
}

// movedInto is the nodes the active state's ParentChange puts under the
// item parent (from elsewhere), in a stable order.
func (doc *docState) movedInto(parent *node) []*node {
	c := doc.state
	if c == nil || len(c.reparent) == 0 {
		return nil
	}
	pid := nodeID(parent)
	if pid == "" {
		return nil
	}
	if c.tok != nil {
		c.tok.Get()
	}
	var ids []string
	for id, np := range c.reparent {
		if np == pid {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var out []*node
	for _, id := range ids {
		if obj := doc.elemObjs.get(doc.intern(id)); obj != nil && obj.node != nil && obj.node.owner != parent {
			out = append(out, obj.node)
		}
	}
	return out
}
