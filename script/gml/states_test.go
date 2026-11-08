package gml

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// statesDoc loads src and returns helpers: frame lays out twice (settling
// geometry), set runs an assignment, get evaluates, rect finds the element
// filled with rgb.
type statesHarness struct {
	t    *testing.T
	env  *Env
	u    *ui.UI
	tree ui.Element
	doc  *docState
}

func newStates(t *testing.T, src string) *statesHarness {
	t.Helper()
	h := &statesHarness{t: t, env: NewEnv()}
	d, err := LoadString(src, h.env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	h.doc = d.doc
	h.u = ui.New(nil, nil, func() ui.Element { h.tree = d.View()(); return h.tree })
	h.frame()
	return h
}

func (h *statesHarness) frame() { h.u.Layout(400, 300); h.u.Layout(400, 300) }

func (h *statesHarness) set(stmt string) {
	h.t.Helper()
	if _, err := evalString(stmt, h.env); err != nil {
		h.t.Fatalf("%s: %v", stmt, err)
	}
	h.frame()
}

func (h *statesHarness) get(expr string) string {
	h.t.Helper()
	v, err := evalString(expr, h.env)
	if err != nil {
		h.t.Fatalf("%s: %v", expr, err)
	}
	return str(v)
}

func (h *statesHarness) rect(rgb uint32) ui.Rect {
	var r ui.Rect
	var walk func(ui.Element)
	walk = func(e ui.Element) {
		if e == nil {
			return
		}
		if c, ok := ui.FillColor(e); ok &&
			uint32(c.R*255+0.5)<<16|uint32(c.G*255+0.5)<<8|uint32(c.B*255+0.5) == rgb {
			r = ui.Bounds(e)
		}
		for _, ch := range ui.Children(e) {
			walk(ch)
		}
	}
	walk(h.tree)
	return r
}

func TestStateWhenExtendAnchorsAndScripts(t *testing.T) {
	h := newStates(t, `
import QtQuick 2.15
Item {
    id: root
    width: 400; height: 300
    property bool open: false
    property bool wide: false
    property int entered: 0
    states: [
        State { name: "open"; when: open && !wide
            AnchorChanges { target: box; anchors.left: undefined; anchors.right: root.right }
            PropertyChanges { target: tag; x: 150 }
            StateChangeScript { script: root.entered += 1 }
        },
        State { name: "wide"; extend: "open"; when: open && wide
            PropertyChanges { target: box; width: 200 }
        }
    ]
    Rectangle { id: box; color: "#ff0000"; width: 50; height: 50; anchors.left: root.left }
    Rectangle { id: tag; color: "#00ff00"; x: 10; y: 100; width: 20; height: 20 }
}`)
	if got := h.get("root.state"); got != "" {
		t.Fatalf("state = %q with no when holding, want the base state", got)
	}
	if r := h.rect(0xff0000); r.X != 0 {
		t.Fatalf("box at x %v in the base state, want 0 (anchored left)", r.X)
	}
	h.set("root.open = true")
	if got := h.get("root.state"); got != "open" {
		t.Errorf("state = %q once open, want open (its when)", got)
	}
	if r := h.rect(0xff0000); r.X != 350 {
		t.Errorf("box at x %v in open, want 350 (AnchorChanges: right, left released)", r.X)
	}
	if r := h.rect(0x00ff00); r.X != 150 {
		t.Errorf("tag at x %v in open, want 150 (PropertyChanges on an anchored child)", r.X)
	}
	if got := h.get("root.entered"); got != "1" {
		t.Errorf("StateChangeScript ran %s times, want 1", got)
	}
	h.frame()
	if got := h.get("root.entered"); got != "1" {
		t.Errorf("StateChangeScript ran again without a change: %s", got)
	}
	// "wide" extends "open": its own width change plus open's anchors.
	h.set("root.wide = true")
	if got := h.get("root.state"); got != "wide" {
		t.Errorf("state = %q, want wide", got)
	}
	if r := h.rect(0xff0000); r.W != 200 || r.X != 200 {
		t.Errorf("box %+v in wide, want 200 wide and still anchored right (x 200)", r)
	}
	h.set("root.open = false")
	if r := h.rect(0xff0000); r.X != 0 || r.W != 50 {
		t.Errorf("box %+v back in the base state, want x 0, 50 wide", r)
	}
}

// The transition that runs is the one whose from/to match the change;
// reversible covers the way back; nested animations count, and targets
// limit them to the items they name.
func TestTransitionFromToReversibleTargets(t *testing.T) {
	h := newStates(t, `
import QtQuick 2.15
Item {
    id: root
    width: 400; height: 300
    property string mode: ""
    state: mode
    states: [
        State { name: "a"; PropertyChanges { target: one; width: 300 }; PropertyChanges { target: two; width: 300 } },
        State { name: "b"; PropertyChanges { target: one; width: 100 } }
    ]
    transitions: [
        Transition { from: ""; to: "a"; reversible: true
            ParallelAnimation { NumberAnimation { targets: [one]; properties: "width"; duration: 1000 } } },
        Transition { from: "*"; to: "b"; NumberAnimation { properties: "width"; duration: 100 } }
    ]
    Rectangle { id: one; color: "#ff0000"; x: 0; y: 0; width: 50; height: 20 }
    Rectangle { id: two; color: "#0000ff"; x: 0; y: 50; width: 50; height: 20 }
}`)
	settle := func() {
		for i := 0; i < 100 && core.AnimationsRunning(); i++ {
			core.TickAnimations(50 * time.Millisecond)
			h.frame()
		}
	}
	h.set(`root.mode = "a"`)
	if w := h.rect(0xff0000).W; w >= 300 || w < 50 {
		t.Errorf("one jumped to %v: the \"\"→a transition (nested, 1 s) should tween it", w)
	}
	if w := h.rect(0x0000ff).W; w != 300 {
		t.Errorf("two at %v: the animation targets only one, so two snaps to 300", w)
	}
	core.TickAnimations(500 * time.Millisecond)
	h.frame()
	if w := h.rect(0xff0000).W; w <= 100 || w >= 300 {
		t.Errorf("one at %v half way through a 1 s tween from 50 to 300", w)
	}
	settle()
	if w := h.rect(0xff0000).W; w != 300 {
		t.Errorf("one settled at %v, want 300", w)
	}
	// a→b matches "*"→b: 100 ms, so 50 ms in it is half way.
	h.set(`root.mode = "b"`)
	core.TickAnimations(50 * time.Millisecond)
	h.frame()
	if w := h.rect(0xff0000).W; w <= 100 || w >= 300 {
		t.Errorf("one at %v 50 ms into a 100 ms tween from 300 to 100", w)
	}
	settle()
	// b→"" matches nothing ("" →a reversible covers a→"" only): a snap.
	h.set(`root.mode = ""`)
	if w := h.rect(0xff0000).W; w != 50 {
		t.Errorf("one at %v after b→\"\" with no matching transition, want the snap to 50", w)
	}
	// ""→a then a→"" through reversible: tweened back.
	h.set(`root.mode = "a"`)
	settle()
	h.set(`root.mode = ""`)
	core.TickAnimations(100 * time.Millisecond)
	h.frame()
	if w := h.rect(0xff0000).W; w <= 50 || w >= 300 {
		t.Errorf("one at %v on a→\"\": the reversible transition should tween it", w)
	}
	settle()
}

// ParentChange moves its target under another item while the state is
// active, positioned there by its own x/y; leaving the state puts it back.
func TestParentChange(t *testing.T) {
	h := newStates(t, `
import QtQuick 2.15
Item {
    id: root
    width: 400; height: 300
    property bool moved: false
    states: State { name: "moved"; when: moved
        ParentChange { target: chip; parent: right; x: 10; y: 20 } }
    Rectangle { id: left; color: "#222222"; x: 0; y: 0; width: 150; height: 150
        Rectangle { id: chip; color: "#ff0000"; x: 5; y: 5; width: 30; height: 30 } }
    Rectangle { id: right; color: "#444444"; x: 200; y: 100; width: 150; height: 150 }
}`)
	if r := h.rect(0xff0000); r.X != 5 || r.Y != 5 {
		t.Fatalf("chip at %v,%v under left, want 5,5", r.X, r.Y)
	}
	h.set("root.moved = true")
	if r := h.rect(0xff0000); r.X != 210 || r.Y != 120 {
		t.Errorf("chip at %v,%v after ParentChange, want 210,120 (right's 200,100 + 10,20)", r.X, r.Y)
	}
	h.set("root.moved = false")
	if r := h.rect(0xff0000); r.X != 5 || r.Y != 5 {
		t.Errorf("chip at %v,%v back in the base state, want 5,5", r.X, r.Y)
	}
}

// A SequentialAnimation's parts run one after another: a later part holds
// its property until the earlier parts (and any pause) are done.
func TestSequentialTransition(t *testing.T) {
	h := newStates(t, `
import QtQuick 2.15
Item {
    id: root
    width: 400; height: 300
    property bool big: false
    states: State { name: "big"; when: big; PropertyChanges { target: box; width: 300; height: 200 } }
    transitions: Transition {
        SequentialAnimation {
            NumberAnimation { properties: "width"; duration: 200 }
            PauseAnimation { duration: 100 }
            NumberAnimation { properties: "height"; duration: 200 }
        }
    }
    Rectangle { id: box; color: "#ff0000"; x: 0; y: 0; width: 100; height: 50 }
}`)
	h.set("root.big = true")
	at := func(ms int) ui.Rect {
		core.TickAnimations(time.Duration(ms) * time.Millisecond)
		h.frame()
		return h.rect(0xff0000)
	}
	if r := at(100); r.W <= 100 || r.W >= 300 || r.H != 50 {
		t.Errorf("at 100 ms %vx%v: want the width moving and the height held at 50", r.W, r.H)
	}
	if r := at(150); r.W != 300 || r.H != 50 { // 250 ms: width done, in the pause
		t.Errorf("at 250 ms %vx%v: want 300 wide, the height still 50 (pause)", r.W, r.H)
	}
	if r := at(150); r.H <= 50 || r.H >= 200 { // 400 ms: 100 ms into the height
		t.Errorf("at 400 ms height %v: want it moving", r.H)
	}
	if r := at(200); r.W != 300 || r.H != 200 {
		t.Errorf("at the end %vx%v, want 300x200", r.W, r.H)
	}
}
