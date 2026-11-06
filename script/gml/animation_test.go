package gml

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// QML runs a standalone animation by CALLING it — a Play button does
// `easingAnim.restart()` — and animates ordinary item geometry
// (`property: "x"`). Neither worked: the corpus' animations page sat
// perfectly still while Qt's ran, and no static screenshot could tell.
func TestAnimationRestartMovesItsTarget(t *testing.T) {
	src := `
Item {
    width: 300; height: 100
    Rectangle {
        id: bar
        x: 0; y: 0; width: 20; height: 20; color: "#ffffff"
        PropertyAnimation {
            id: anim
            target: bar
            property: "x"
            from: 0
            to: 200
            duration: 200
            easing.type: Easing.Linear
        }
    }
    MouseArea { y: 50; width: 300; height: 50; onClicked: anim.restart() }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(300, 100)
	barX := func() float32 {
		var x float32
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if b := ui.Bounds(e); b.W == 20 && b.H == 20 {
				x = b.X
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(tree)
		return x
	}
	if got := barX(); got != 0 {
		t.Fatalf("the bar started at x=%v, want 0", got)
	}
	// Nothing moves until the animation is told to run.
	core.TickAnimations(100 * time.Millisecond)
	u.Layout(300, 100)
	if got := barX(); got != 0 {
		t.Fatalf("the bar moved before anything started it (x=%v)", got)
	}
	if !u.Click(150, 75) {
		t.Fatal("the Play area was not clicked")
	}
	// Half way through a 200ms linear run.
	core.TickAnimations(100 * time.Millisecond)
	u.Layout(300, 100)
	mid := barX()
	if mid < 80 || mid > 120 {
		t.Errorf("half way through, x=%v, want ~100", mid)
	}
	// And it finishes on its `to`.
	core.TickAnimations(150 * time.Millisecond)
	u.Layout(300, 100)
	if got := barX(); got != 200 {
		t.Errorf("at the end x=%v, want 200", got)
	}
	// stop() holds it there, restart() plays it again from the beginning.
	if r, ok := env.vars["anim"].(*elemObject).methods["restart"].(jsFunc); ok {
		if _, err := r([]any{}); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal("the animation's id publishes no restart()")
	}
	core.TickAnimations(10 * time.Millisecond)
	u.Layout(300, 100)
	if got := barX(); got > 40 {
		t.Errorf("restart() left the bar at x=%v, want it back near the start", got)
	}
}

// A document's Timers and animations advance with the FRAME LOOP's clock,
// not the wall clock: under a headless capture the wall clock barely moves
// between frames while the loop advances animation time in big steps to
// settle what is moving. Read the wall clock alone and a screenshot shows a
// page that never started.
func TestAnimationsFollowTheFrameLoopClock(t *testing.T) {
	src := `
Item {
    width: 200; height: 60
    property real phase: 0
    NumberAnimation { target: parent; property: "phase"; from: 0; to: 1; duration: 100; running: true }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 60)
	phase := func() float64 {
		f, _ := toFloat64(d.doc.props["phase"].get())
		return f
	}
	// Frames with no wall-clock gap between them, but the frame loop
	// advancing animation time: the animation has to move.
	for i := 0; i < 3; i++ {
		core.TickAnimations(40 * time.Millisecond)
		u.Layout(200, 60)
	}
	if got := phase(); got <= 0 {
		t.Errorf("phase = %v after 120ms of frame-loop time, want it moving", got)
	}
}

// A SequentialAnimation runs its children one after another and a
// ParallelAnimation all at once — the composition QML pages are built from
// (move, then recolour, then scale). Unmodelled, a Play button moved the
// first step's property and nothing else ever happened.
func TestSequentialAndParallelAnimations(t *testing.T) {
	src := `
Item {
    width: 400; height: 200
    property real seqX: 0
    property real seqScale: 1
    property string seqColor: "#0000ff"
    property real parX: 0
    property real parScale: 1
    SequentialAnimation {
        id: seq
        NumberAnimation { target: parent; property: "seqX"; from: 0; to: 100; duration: 200
                          easing.type: Easing.Linear }
        ColorAnimation  { target: parent; property: "seqColor"; to: "#00ff00"; duration: 200 }
        NumberAnimation { target: parent; property: "seqScale"; from: 1; to: 2; duration: 200
                          easing.type: Easing.Linear }
    }
    ParallelAnimation {
        id: par
        NumberAnimation { target: parent; property: "parX"; from: 0; to: 100; duration: 200
                          easing.type: Easing.Linear }
        NumberAnimation { target: parent; property: "parScale"; from: 1; to: 2; duration: 200
                          easing.type: Easing.Linear }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(400, 200)
	num := func(name string) float64 {
		f, _ := toFloat64(d.doc.props[name].get())
		return f
	}
	col := func() string { return str(d.doc.props["seqColor"].get()) }
	call := func(id, method string) {
		obj, ok := env.vars[id].(*elemObject)
		if !ok {
			t.Fatalf("%s has no object", id)
		}
		fn, ok := obj.methods[method].(jsFunc)
		if !ok {
			t.Fatalf("%s has no %s()", id, method)
		}
		if _, err := fn([]any{}); err != nil {
			t.Fatal(err)
		}
	}
	// In slices of at most 200ms: a document ignores a frame gap longer
	// than a quarter of a second (a stall or a paused window should not
	// jump every animation forward), so a test has to tick like a frame
	// loop rather than in one leap.
	step := func(d time.Duration) {
		for left := d; left > 0; {
			slice := min(left, 200*time.Millisecond)
			core.TickAnimations(slice)
			u.Layout(400, 200)
			left -= slice
		}
	}
	// The sequence: 200ms of moving, THEN 200ms of colour, THEN 200ms of
	// scaling. Nothing after the running step has begun.
	call("seq", "restart")
	step(100 * time.Millisecond)
	if x := num("seqX"); x < 40 || x > 60 {
		t.Errorf("half way through step one, seqX = %v, want ~50", x)
	}
	if got := col(); got != "#0000ff" && got != "#ff0000ff" {
		t.Errorf("step two started early (colour %q)", got)
	}
	if s := num("seqScale"); s != 1 {
		t.Errorf("step three started early (scale %v)", s)
	}
	step(200 * time.Millisecond) // 300ms: step one done, step two half way
	if x := num("seqX"); x != 100 {
		t.Errorf("after its step, seqX = %v, want it held at 100", x)
	}
	if c, err := parseColor(col()); err != nil || c.G < 0.2 || c.G > 0.8 {
		t.Errorf("mid-colour = %q, want it part way to green", col())
	}
	if s := num("seqScale"); s != 1 {
		t.Errorf("step three started during step two (scale %v)", s)
	}
	step(300 * time.Millisecond) // 600ms: the whole sequence is done
	if s := num("seqScale"); s != 2 {
		t.Errorf("at the end, seqScale = %v, want 2", s)
	}
	if c, _ := parseColor(col()); c.G < 0.9 {
		t.Errorf("at the end the colour is %q, want green", col())
	}
	// The parallel one moves both properties together.
	call("par", "restart")
	step(100 * time.Millisecond)
	x, sc := num("parX"), num("parScale")
	if x < 40 || x > 60 {
		t.Errorf("parX = %v half way, want ~50", x)
	}
	if sc < 1.4 || sc > 1.6 {
		t.Errorf("parScale = %v half way, want ~1.5 (both at once)", sc)
	}
}

// A sequence's pauses, instant sets and script snippets are steps of their
// own: `SequentialAnimation { NumberAnimation{} PauseAnimation{}
// PropertyAction{} ScriptAction{} }` is a common shape.
func TestSequenceActionsAndPause(t *testing.T) {
	src := `
Item {
    width: 200; height: 100
    property real v: 0
    property string mark: "none"
    property bool flag: false
    SequentialAnimation {
        id: seq
        NumberAnimation { target: parent; property: "v"; from: 0; to: 10; duration: 100
                          easing.type: Easing.Linear }
        PauseAnimation { duration: 200 }
        PropertyAction { target: parent; property: "flag"; value: true }
        ScriptAction { script: mark = "ran" }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 100)
	get := func(name string) string { return str(d.doc.props[name].get()) }
	fn := env.vars["seq"].(*elemObject).methods["restart"].(jsFunc)
	if _, err := fn([]any{}); err != nil {
		t.Fatal(err)
	}
	core.TickAnimations(150 * time.Millisecond)
	u.Layout(200, 100)
	// The tween is done; the pause is holding, so nothing after it has run.
	if get("v") != "10" {
		t.Errorf("v = %q after its step, want 10", get("v"))
	}
	if get("flag") != "false" || get("mark") != "none" {
		t.Errorf("the pause did not hold the sequence (flag %q, mark %q)", get("flag"), get("mark"))
	}
	core.TickAnimations(200 * time.Millisecond)
	u.Layout(200, 100)
	if get("flag") != "true" {
		t.Errorf("the PropertyAction never ran (flag %q)", get("flag"))
	}
	if get("mark") != "ran" {
		t.Errorf("the ScriptAction never ran (mark %q)", get("mark"))
	}
}

// `SequentialAnimation on opacity { NumberAnimation { from: 1; to: 0.3 } }`
// — the blink idiom — is a container in the `on <prop>` form: its children
// state no target of their own and drive the container's property, which
// here is a BUILT-IN one of the element the animation sits in (opacity is
// layout-derived, not a property, so it is written as that element's own
// attribute).
func TestContainerOnPropertyDrivesItsOwner(t *testing.T) {
	src := `
import QtQuick
Rectangle {
    width: 300; height: 120; color: "#101318"
    Rectangle {
        id: dot
        x: 20; y: 40; width: 40; height: 40; color: "#ff4040"
        SequentialAnimation on opacity {
            loops: Animation.Infinite
            running: true
            NumberAnimation { from: 1.0; to: 0.3; duration: 300 }
            NumberAnimation { from: 0.3; to: 1.0; duration: 300 }
        }
    }
    Text { y: 0; text: "op " + dot.opacity; color: "#ffffff" }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(300, 120)
	says := func() string {
		var out string
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if s, ok := ui.TextOf(e); ok && len(s) > 3 && s[:3] == "op " {
				out = s
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(tree)
		return out
	}
	step := func() {
		core.TickAnimations(150 * time.Millisecond)
		u.Layout(300, 120)
	}
	if got := says(); got != "op 1" {
		t.Fatalf("at rest the opacity reads %q, want op 1", got)
	}
	step()
	mid := says()
	if mid == "op 1" {
		t.Fatal("the blink never started")
	}
	step() // 300ms: the end of the first step, the dimmest point
	if got := says(); got != "op 0.3" {
		t.Errorf("at the bottom of the blink the opacity reads %q, want op 0.3", got)
	}
	step()
	step() // 600ms: back up, and looping
	if got := says(); got == "op 0.3" {
		t.Error("the blink did not come back up")
	}
}

// QML's property-value-source form — `NumberAnimation on x { ... }` — RUNS
// BY DEFAULT, and a NumberAnimation's default easing is LINEAR. Both were
// wrong here: attached animations sat inert (a headless capture of an
// animated page was at t=0 where Qt's reference was at 2.5s), and the ones
// that did run used InOutQuad, which at 62.5% of a ramp reads 71.9%.
func TestAttachedAnimationRunsLinearByDefault(t *testing.T) {
	src := `
Rectangle {
    width: 900; height: 100
    property real phase: 0
    NumberAnimation on phase { from: 0; to: 800; duration: 4000 }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(900, 100)
	// A running animation must also report that the document is MOVING, or
	// a caller that renders only while state changes (the headless capture)
	// stops after two frames.
	if !d.doc.anythingMoving() {
		t.Error("a running animation did not mark the document as moving")
	}
	for i := 0; i < 20; i++ { // 20 x 125ms = 2500ms, as a capture advances
		d.doc.advanceAnimators(125)
	}
	got, _ := toFloat64(d.Property("phase"))
	if got < 495 || got > 505 {
		t.Errorf("phase = %v at 2500/4000ms, want ~500 (linear)", got)
	}
}
