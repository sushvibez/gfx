package gml

import (
	"fmt"
	"math"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// QML standalone animations: a NumberAnimation/PropertyAnimation/
// RotationAnimation/ColorAnimation that runs on its own (running: true),
// driving a target property over time with optional looping. Unlike a
// Behavior (tween-on-change) or a Transition (state-driven), these free-run.
//
//	NumberAnimation { target: box; property: "phase"; from: 0; to: 1
//	                  duration: 1000; loops: Animation.Infinite; running: true }
//	NumberAnimation on phase { from: 0; to: 1; duration: 1000; running: true }
//
// The target must be an assignable property (a declared `property`), since
// built-in geometry (x/width/...) is layout-derived, not settable. Driven from
// the per-frame View() rebuild, like Timer.
type gmlAnimator struct {
	node       *node
	env        *Env
	methods    map[string]any // start/stop/..., built once (see refreshAnimator)
	steps      []*animStep    // one for a standalone animation, several in a container
	parallel   bool           // a ParallelAnimation runs them all at once
	container  bool
	accumMs    float64 // elapsed within the current cycle
	loopsDone  int
	wasRunning bool
	done       bool
	// Imperative control: QML runs an animation by CALLING it
	// (`easingAnim.restart()` from a Play button), which overrides whatever
	// its `running:` attribute says until it is stopped again.
	forced, forcedSet bool
}

// isContainerAnimation reports the composition types: they hold other
// animations rather than driving a property of their own.
func isContainerAnimation(t string) bool {
	return t == "SequentialAnimation" || t == "ParallelAnimation"
}

// bindSteps (re)reads the animation's shape from its markup: a standalone
// animation is one step, a container is its children in order. Called every
// build, so a live scope change is picked up while the captured start
// values survive mid-flight.
func (a *gmlAnimator) bindSteps(n *node, env *Env, doc *docState, ownerKey keyID) {
	a.node, a.env = n, env
	a.container = isContainerAnimation(n.typeName)
	a.parallel = n.typeName == "ParallelAnimation"
	if !a.container {
		if len(a.steps) == 1 {
			a.steps[0].n, a.steps[0].env = n, env
			a.steps[0].doc, a.steps[0].key = doc, ownerKey
			return
		}
		a.steps = []*animStep{{n: n, env: env, doc: doc, key: ownerKey}}
		return
	}
	// The container's own target, which its children inherit when they
	// state none: `SequentialAnimation on opacity { NumberAnimation {...} }`.
	own := &animStep{n: n, env: env, doc: doc, key: ownerKey}
	inherit := func() Ref { return own.target() }
	kids := make([]*node, 0, len(n.children))
	for _, c := range n.children {
		if isAnimation(c.typeName) || isAnimationAction(c.typeName) {
			kids = append(kids, c)
		}
	}
	if len(a.steps) == len(kids) {
		for i, c := range kids {
			a.steps[i].n, a.steps[i].env = c, env
			a.steps[i].doc, a.steps[i].key = doc, ownerKey
			a.steps[i].inherit = inherit
		}
		return
	}
	a.steps = make([]*animStep, len(kids))
	for i, c := range kids {
		a.steps[i] = &animStep{n: c, env: env, doc: doc, key: ownerKey, inherit: inherit}
	}
}

// isAnimationAction reports the non-tween members of an animation sequence:
// a pause, an instant property set, a snippet of script.
func isAnimationAction(t string) bool {
	switch t {
	case "PauseAnimation", "PropertyAction", "ScriptAction":
		return true
	}
	return false
}

// restart plays the animation from the beginning, running or not — QML's
// restart(). start() is the same when it is stopped.
func (a *gmlAnimator) restart() {
	a.accumMs, a.loopsDone, a.done = 0, 0, false
	for _, st := range a.steps {
		st.reset()
	}
	a.forced, a.forcedSet, a.wasRunning = true, true, true
}

// stop halts it where it is; start() from there begins again.
func (a *gmlAnimator) stop() { a.forced, a.forcedSet, a.wasRunning = false, true, false }

// pause holds the animation at its current position without rewinding.
func (a *gmlAnimator) pause() { a.forced, a.forcedSet, a.wasRunning = false, true, false }

// resume continues from where a pause left it.
func (a *gmlAnimator) resume() { a.forced, a.forcedSet, a.wasRunning = true, true, true }

// complete jumps to the end, as QML's complete() does: every step lands on
// its final value.
func (a *gmlAnimator) complete() {
	for _, st := range a.steps {
		st.applyAt(1)
	}
	a.done = true
	a.forced, a.forcedSet = false, true
}

// running reports whether the animation is playing: what it was last told
// imperatively, else what its `running:` attribute says.
func (a *gmlAnimator) running() bool {
	if a.forcedSet {
		return a.forced
	}
	// An animation attached to a property — `NumberAnimation on x { ... }`,
	// QML's property-value-source form — RUNS BY DEFAULT; a standalone one
	// waits to be started. Defaulting both to stopped left every attached
	// animation in the corpus inert: a linear ramp probe read 0ms where
	// Qt's reference was at 2510ms.
	def := a.node != nil && a.node.onTarget != ""
	return timerBool(a.node, a.env, "running", def)
}

// totalMs is how long one pass takes: the sum of the steps for a sequence,
// the longest of them for a parallel one.
func (a *gmlAnimator) totalMs() float64 {
	var total float64
	for _, st := range a.steps {
		d := st.dur()
		if a.parallel || !a.container {
			total = math.Max(total, d)
			continue
		}
		total += d
	}
	return total
}

// applyAtElapsed puts every step where it belongs at this point in the
// pass. A sequence's earlier steps are held at their end (they have already
// happened) and its later ones are left alone until they are reached, which
// is what makes a sequence read as one move after another.
func (a *gmlAnimator) applyAtElapsed(elapsed float64) {
	if !a.container || a.parallel {
		for _, st := range a.steps {
			d := st.dur()
			t := float32(1)
			if d > 0 {
				t = float32(math.Min(elapsed/d, 1))
			}
			st.applyAt(t)
		}
		return
	}
	var start float64
	for _, st := range a.steps {
		d := st.dur()
		switch {
		case elapsed >= start+d:
			st.applyAt(1) // already done
		case elapsed >= start:
			t := float32(1)
			if d > 0 {
				t = float32((elapsed - start) / d)
			}
			st.applyAt(t)
			return // the steps after this one have not begun
		default:
			return
		}
		start += d
	}
}

// isAnimation reports the standalone animation element types this drives.
func isAnimation(t string) bool {
	switch t {
	case "NumberAnimation", "PropertyAnimation", "RotationAnimation",
		"ColorAnimation", "SmoothedAnimation", "SpringAnimation",
		// The composition types: a SequentialAnimation runs its children
		// one after another, a ParallelAnimation all at once. They are
		// animations in their own right — started, stopped and looped the
		// same way — with steps instead of a property of their own.
		"SequentialAnimation", "ParallelAnimation",
		// Qt's *Animator types run on the render thread but drive the same
		// property tween; they are that same animation here.
		"NumberAnimator", "RotationAnimator", "OpacityAnimator",
		"ScaleAnimator", "XAnimator", "YAnimator", "UniformAnimator":
		return true
	}
	return false
}

// refreshAnimator registers/updates a standalone animation node's retained
// state during the build pass (config re-read each tick from the live node).
// animatorKey is the retained-state key for an animation node: its id, or
// its line when it has none.
func animatorKey(n *node) string {
	if id := nodeID(n); id != "" {
		return id
	}
	return fmt.Sprintf("__anim%d", n.line)
}

func (doc *docState) refreshAnimator(n *node, env *Env) {
	key := animatorKey(n)
	a := doc.animators[key]
	if a == nil {
		a = &gmlAnimator{}
		doc.animators[key] = a
	}
	// Snapshot the scope — animators run from the frame tick, after any
	// per-instance scope has been restored (see Env.snapshot) — and read
	// the animation's shape: one step, or a container's children in order.
	// The element the animation was declared in: an `on <prop>` animation
	// drives ITS property, and a built-in one is written as that element's
	// attribute override.
	var ownerKey keyID
	if n.owner != nil {
		ownerKey = doc.keyID(n.owner)
	}
	a.bindSteps(n, doc.scopeSnapshot(env, "anim:"+key), doc, ownerKey)
	// QML's imperative animation API, on the animation's own id: a Play
	// button calls restart(), which is how every animation on the corpus'
	// animations page is started. Without it the page sat still.
	if a.methods == nil { // once per animator: the closures only need `a`
		a.methods = map[string]any{
			"start":    jsFunc(func([]any) (any, error) { a.restart(); return nil, nil }),
			"restart":  jsFunc(func([]any) (any, error) { a.restart(); return nil, nil }),
			"stop":     jsFunc(func([]any) (any, error) { a.stop(); return nil, nil }),
			"pause":    jsFunc(func([]any) (any, error) { a.pause(); return nil, nil }),
			"resume":   jsFunc(func([]any) (any, error) { a.resume(); return nil, nil }),
			"complete": jsFunc(func([]any) (any, error) { a.complete(); return nil, nil }),
		}
	}
	doc.exposeIDMethodsIn(n, env, a.methods)
	doc.exposeIDProp(n, "running", a.running())
}

// advanceAnimators steps every registered animator by dtMs.
func (doc *docState) advanceAnimators(dtMs float64) {
	for _, a := range doc.animators {
		a.advance(dtMs)
	}
}

func (a *gmlAnimator) advance(dtMs float64) {
	running := a.running()
	if running && !a.wasRunning { // (re)start
		a.accumMs, a.loopsDone, a.done = 0, 0, false
		for _, st := range a.steps {
			st.reset()
		}
	}
	a.wasRunning = running
	if !running || a.done || len(a.steps) == 0 {
		return
	}
	total := a.totalMs()
	if total <= 0 {
		// Instant: actions and zero-duration animations still happen.
		a.applyAtElapsed(0)
		a.done = true
		return
	}
	a.accumMs += dtMs
	loops := a.loops()
	for a.accumMs >= total {
		if loops < 0 { // infinite
			a.accumMs = math.Mod(a.accumMs, total)
			for _, st := range a.steps {
				st.reset()
			}
			break
		}
		a.loopsDone++
		if a.loopsDone >= loops {
			a.applyAtElapsed(total)
			a.done = true
			return
		}
		a.accumMs -= total
		for _, st := range a.steps {
			st.reset()
		}
	}
	a.applyAtElapsed(a.accumMs)
}

// animStep is ONE animation in a container, or the whole of a standalone
// one: the node, the scope it runs in, and the starting value it captured
// the first time it ran (QML's implicit `from`, which is the property's
// value at that moment).
type animStep struct {
	n    *node
	env  *Env
	from float32
	// A ColorAnimation interpolates colours, not numbers; its start value
	// is the colour the property held when the step began.
	fromCol   ui.RGBA
	haveFrom  bool
	ranScript bool // a ScriptAction fires once per pass
	// inherit is the container's target, for a step that states none of
	// its own: `SequentialAnimation on opacity { NumberAnimation { from:
	// 1; to: 0.3 } }` — the children drive the container's property.
	inherit func() Ref
	// doc and key let an `on <prop>` animation write a BUILT-IN property
	// (opacity, x, scale) of the element it was declared in, as an
	// attribute override — those are layout-derived rather than
	// properties, so there is no ref to resolve.
	doc *docState
	key keyID
}

// dur is how long this step lasts (QML's default is 250ms; a PauseAnimation
// IS its duration, and an instant action takes none).
func (s *animStep) dur() float64 {
	switch s.n.typeName {
	case "PropertyAction", "ScriptAction":
		return 0
	}
	return float64(timerFloat(s.n, s.env, "duration", 250))
}

// reset forgets the captured start value, so the next pass starts from
// wherever the property is then — QML re-reads it on every run.
func (s *animStep) reset() { s.haveFrom, s.ranScript = false, false }

// target resolves the property this step drives: the `on <prop>` form, or
// target + property, or a bare property name — and failing all of those,
// whatever its container drives.
func (s *animStep) target() Ref {
	if s.n.onTarget != "" {
		if r, err := resolveRef(&identExpr{name: s.n.onTarget}, s.env); err == nil {
			return r
		}
		// A built-in property of the element the animation sits in
		// (`NumberAnimation on opacity` inside a Rectangle): those are not
		// properties at all, so it is written as that element's attribute.
		if s.doc != nil && s.key != 0 {
			return attrOverrideRef{doc: s.doc, key: s.key, name: s.n.onTarget}
		}
		return nil
	}
	var targetExpr, propName expr
	var prop string
	for _, at := range s.n.attrs {
		switch at.name {
		case "target", "targets":
			targetExpr = at.expr
		case "property", "properties":
			propName = at.expr
		}
	}
	if propName != nil {
		if v, err := propName.eval(s.env); err == nil {
			prop = str(v)
		}
	}
	if prop == "" {
		// A step inside a container states nothing of its own: it drives
		// what the container drives.
		if s.inherit != nil {
			return s.inherit()
		}
		return nil
	}
	if targetExpr != nil {
		if id, ok := targetExpr.(*identExpr); ok {
			if r, err := resolveRef(&memberExpr{base: id, name: prop}, s.env); err == nil {
				return r
			}
		}
	}
	// Fall back to a bare property in the current scope (target omitted, or
	// its id not resolvable).
	if prop != "" {
		if r, err := resolveRef(&identExpr{name: prop}, s.env); err == nil {
			return r
		}
	}
	if s.inherit != nil {
		return s.inherit()
	}
	return nil
}

// isColour reports whether this step interpolates a colour: a
// ColorAnimation always does, and so does any animation whose `to` is one.
func (s *animStep) isColour(ref Ref) bool {
	if s.n.typeName == "ColorAnimation" {
		return true
	}
	v, ok := animValue(s.n, s.env, "to")
	if !ok {
		return false
	}
	if _, err := toFloat32(v); err == nil {
		return false // a number is a number
	}
	if _, err := parseColor(str(v)); err != nil {
		return false
	}
	// The property has to be holding a colour too, or this is a string
	// animation we cannot interpolate.
	_, err := parseColor(str(ref.get()))
	return err == nil
}

// applyAt writes the value this step holds at t (0..1 within the step).
// The first call captures the starting value, as QML does.
func (s *animStep) applyAt(t float32) {
	switch s.n.typeName {
	case "PauseAnimation":
		return // time passing is the whole of it
	case "ScriptAction":
		if !s.ranScript {
			s.ranScript = true
			s.runScript()
		}
		return
	}
	ref := s.target()
	if ref == nil {
		return
	}
	if s.n.typeName == "PropertyAction" {
		// An instant set, at the point it sits in the sequence.
		if v, ok := animValue(s.n, s.env, "value"); ok {
			_ = ref.set(v)
		}
		return
	}
	eased := applyEasing(s.easing(), t)
	if s.isColour(ref) {
		to, ok := animValue(s.n, s.env, "to")
		if !ok {
			return
		}
		toC, err := parseColor(str(to))
		if err != nil {
			return
		}
		if !s.haveFrom {
			if fromV, ok := animValue(s.n, s.env, "from"); ok {
				if c, err := parseColor(str(fromV)); err == nil {
					s.fromCol, s.haveFrom = c, true
				}
			}
			if !s.haveFrom {
				if c, err := parseColor(str(ref.get())); err == nil {
					s.fromCol, s.haveFrom = c, true
				}
			}
		}
		f := s.fromCol
		mix := ui.RGBA{
			R: f.R + (toC.R-f.R)*eased,
			G: f.G + (toC.G-f.G)*eased,
			B: f.B + (toC.B-f.B)*eased,
			A: f.A + (toC.A-f.A)*eased,
		}
		_ = ref.set(colorHex(mix))
		return
	}
	to, hasTo := animFloat(s.n, s.env, "to")
	if !hasTo {
		return // nothing to animate toward
	}
	if !s.haveFrom {
		if f, ok := animFloat(s.n, s.env, "from"); ok {
			s.from, s.haveFrom = f, true
		} else if cur, err := toFloat32(ref.get()); err == nil {
			s.from, s.haveFrom = cur, true
		}
	}
	_ = ref.set(s.from + (to-s.from)*eased)
}

// runScript runs a ScriptAction's `script:` statements.
func (s *animStep) runScript() {
	for _, at := range s.n.attrs {
		if at.name != "script" {
			continue
		}
		if at.stmt != nil {
			if err := runStmts(at.stmt, s.env); err != nil {
				logf("gml: ScriptAction: %v", err)
			}
			return
		}
		if at.expr != nil {
			if _, err := at.expr.eval(s.env); err != nil {
				logf("gml: ScriptAction: %v", err)
			}
		}
	}
}

func (s *animStep) easing() core.Easing {
	for _, at := range s.n.attrs {
		if at.name == "easing.type" || at.name == "easing" {
			if v, err := at.expr.eval(s.env); err == nil {
				return easingByName(str(v))
			}
		}
	}
	// QML's default is LINEAR: `NumberAnimation { duration: 4000 }` moves
	// at a constant rate. Defaulting to InOutQuad put every un-eased
	// animation ahead of Qt's (at 62.5% of a ramp it read 71.9%), which is
	// most of what an animated page's frame-to-frame difference was.
	return core.EaseLinear
}

// loops returns the loop count: -1 for Animation.Infinite, else the number
// (default 1).
func (a *gmlAnimator) loops() int {
	for _, at := range a.node.attrs {
		if at.name != "loops" {
			continue
		}
		if v, err := at.expr.eval(a.env); err == nil {
			if f, err := toFloat64(v); err == nil {
				return int(f)
			}
		}
	}
	return 1
}

// animValue reads an animation attribute as it is written, without forcing
// it to a number (a ColorAnimation's `to` is a colour).
func animValue(n *node, env *Env, name string) (any, bool) {
	for _, a := range n.attrs {
		if a.name == name && a.expr != nil {
			if v, err := a.expr.eval(env); err == nil {
				return v, true
			}
		}
	}
	return nil, false
}

// animFloat reads a numeric animation attribute, reporting whether present.
func animFloat(n *node, env *Env, name string) (float32, bool) {
	for _, a := range n.attrs {
		if a.name == name {
			if v, err := a.expr.eval(env); err == nil {
				if f, err := toFloat32(v); err == nil {
					return f, true
				}
			}
		}
	}
	return 0, false
}

// applyEasing maps t in [0,1] through an easing curve.
func applyEasing(e core.Easing, t float32) float32 {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return e(t)
}
