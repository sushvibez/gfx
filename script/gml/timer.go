package gml

import (
	"fmt"
	"math"
	"time"

	"github.com/sushvibez/gfx/core"
)

// QML Timer element. It renders nothing; it accumulates frame time and fires
// onTriggered when interval elapses. interval/running/repeat are live (read
// each tick), so they can be bound. Defaults follow QML: running=false,
// repeat=true, interval=1000ms, triggeredOnStart=false.
//
// Frame time comes from the per-frame View() rebuild (wall clock between
// rebuilds) — no separate scheduler. A non-repeat timer fires once per
// running period; toggling running off→on rearms it.
type gmlTimer struct {
	node       *node
	env        *Env
	accum      float64 // ms toward the next trigger
	wasRunning bool
	stopped    bool // one-shot already fired this running period
}

// refreshTimer registers or updates a Timer node's retained state during the
// build pass (config is re-read each tick from the node, so the live node +
// env are all it keeps).
func (doc *docState) refreshTimer(n *node, env *Env) {
	key := nodeID(n)
	if key == "" {
		key = fmt.Sprintf("__timer%d", n.line)
	}
	t := doc.timers[key]
	if t == nil {
		t = &gmlTimer{}
		doc.timers[key] = t
	}
	// Snapshot the scope: the handler runs on a later tick, after any
	// enclosing file-component or delegate scope has been restored, so the
	// live chain would resolve document-global objects instead of this
	// instance's (see Env.snapshot).
	t.node, t.env = n, doc.scopeSnapshot(env, "timer:"+key)
}

// tickTimers advances all timers by the time elapsed since the last frame,
// clamped to ignore startup/paused gaps.
//
// "Elapsed" is whichever clock has moved further: the wall clock, or the
// frame loop's own animation clock (core.AnimClock). Under a headless
// capture the wall clock barely moves between frames while the loop
// advances animation time in big steps to settle what is moving — read the
// wall clock alone and every Timer and animation in the document stands
// still, so a screenshot showed a page that never started.
func (doc *docState) tickTimers(now time.Time) {
	clock := core.AnimClock()
	if doc.lastView.IsZero() {
		doc.lastView, doc.lastClock = now, clock
		// Under the virtual clock the FIRST slice must not be lost: the
		// clock starts at zero and the document's first frame already sits
		// one slice in, so a capture ended up 125ms short of the time it
		// meant to sample (a 4s rotation was 20 degrees behind Qt's).
		if !core.VirtualClock() {
			return
		}
		doc.lastClock = 0
	}
	dt := now.Sub(doc.lastView).Seconds() * 1000
	virtual := (clock - doc.lastClock).Seconds() * 1000
	switch {
	case core.VirtualClock():
		// A deterministic capture: the animation clock is the only clock.
		dt = virtual
	case virtual > dt:
		dt = virtual
	}
	doc.lastView, doc.lastClock = now, clock
	switch {
	case dt < 0:
		dt = 0
	case dt > 250:
		dt = 250
	}
	// Step in FRAME-sized slices. A page that accumulates per frame — a
	// FrameAnimation adding `frameTime` to a shader's clock, a Timer
	// nudging a rotation — must see as many frames as a 60Hz run would,
	// or a capture that advances 125ms at a time lands at a fraction of
	// the phase (the portal's shader clock reached 1.0s where Qt's was at
	// 2.5s, because `Math.min(frameTime, 0.05)` capped every slice).
	const frame = 1000.0 / 60
	for left := dt; left > 0; {
		step := math.Min(left, frame)
		left -= step
		doc.advanceTimers(step)
		doc.advanceAnimators(step)
		doc.advanceFrameAnims(step)
		doc.advanceParticles(step)
	}
	// A document with anything MOVING has to say so: an animator writes
	// attribute overrides rather than core Properties, so nothing bumped
	// the revision and a caller that renders only while state changes
	// stopped after two frames. That is exactly what the headless capture
	// does — every animated page was screenshotted at t≈0 (a linear ramp
	// probe read 0ms where Qt's reference was at 2510ms).
	// A document with anything MOVING also has to keep the FRAME LOOP
	// awake. A host that renders only on damage asks core whether
	// anything is animating, and a gml animator is invisible to it: the
	// revision bump above happens while rendering, so by the time the
	// host compares revisions it has already seen it, and the next frame
	// is skipped — which stops the document's clock, which stops the
	// bumps. Every animated page froze a frame or two after it opened
	// (the buttons page's pulsing ring, live, never pulsed).
	moving := doc.anythingMoving()
	core.SetExternalAnimation(doc, moving)
	if moving {
		core.BumpRevision()
	}
}

// anythingMoving reports whether a timer, an animator or a particle system
// still has work — the document's own "wants another frame".
func (doc *docState) anythingMoving() bool {
	for _, t := range doc.timers {
		if !t.stopped && timerBool(t.node, t.env, "running", false) {
			return true
		}
	}
	for _, a := range doc.animators {
		if a.running() && !a.done {
			return true
		}
	}
	for _, s := range doc.particles {
		if s != nil {
			return true
		}
	}
	return len(doc.frameAnims) > 0
}

// advanceTimers steps every registered timer by dtMs (separated from the wall
// clock so tests can drive it deterministically).
func (doc *docState) advanceTimers(dtMs float64) {
	for _, t := range doc.timers {
		t.advance(dtMs)
	}
}

func (t *gmlTimer) advance(dtMs float64) {
	running := timerBool(t.node, t.env, "running", false)
	if running && !t.wasRunning { // (re)start
		t.accum = 0
		t.stopped = false
		if timerBool(t.node, t.env, "triggeredOnStart", false) {
			t.fire()
		}
	}
	t.wasRunning = running
	if !running || t.stopped {
		return
	}
	interval := timerFloat(t.node, t.env, "interval", 1000)
	if interval <= 0 {
		return
	}
	repeat := timerBool(t.node, t.env, "repeat", true)
	t.accum += dtMs
	for t.accum >= interval {
		t.accum -= interval
		t.fire()
		if !repeat {
			t.stopped = true
			return
		}
	}
}

func (t *gmlTimer) fire() {
	for _, a := range t.node.attrs {
		if a.name == "onTriggered" {
			if err := runStmts(a.stmt, t.env); err != nil {
				logf("gml: Timer.onTriggered: %v", err)
			}
			return
		}
	}
}

// timerFloat / timerBool read a Timer attribute live, with a default.
func timerFloat(n *node, env *Env, name string, def float64) float64 {
	for _, a := range n.attrs {
		if a.name == name {
			if v, err := a.expr.eval(env); err == nil {
				if f, err := toFloat64(v); err == nil {
					return f
				}
			}
		}
	}
	return def
}

func timerBool(n *node, env *Env, name string, def bool) bool {
	for _, a := range n.attrs {
		if a.name == name {
			if v, err := a.expr.eval(env); err == nil {
				if b, err := toBool(v); err == nil {
					return b
				}
			}
		}
	}
	return def
}
