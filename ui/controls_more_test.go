package ui

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestTumblerTurns(t *testing.T) {
	var st TumblerState
	cur := 5
	var wheel *DivEl
	u := New(nil, nil, func() Element {
		wheel = Tumbler(&st, cur, func(i int) { cur = i },
			TumblerOpts{Count: 12, Visible: 3, Wrap: true, W: 60, H: 90}, nil).FocusID("wheel")
		return Div().Children(wheel)
	})
	frame := func() { u.Layout(200, 200); u.Layout(200, 200) }
	frame()
	if got, _ := TextOf(Children(Children(wheel)[1])[0]); got != "5" {
		t.Fatalf("centre slot shows %q, want 5", got)
	}
	// Dragging up two rows brings later items up.
	r := Bounds(wheel)
	u.Drag(r.X+30, r.Y+60, r.X+30, r.Y)
	frame()
	if cur != 7 {
		t.Errorf("a two-row drag up: current %d, want 7", cur)
	}
	u.FocusID("wheel")
	u.Key(app.KeyEvent{Down: true, Key: app.KeyDown})
	frame()
	if cur != 8 {
		t.Errorf("Down: current %d, want 8", cur)
	}
	// A click on the slot above picks it.
	top := Bounds(Children(wheel)[0])
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: top.X + 5, Y: top.Y + 5, Button: 1}, {Kind: app.MouseUp, X: top.X + 5, Y: top.Y + 5, Button: 1}})
	frame()
	if cur != 7 {
		t.Errorf("the slot above 8: current %d, want 7", cur)
	}
}

func TestSplitViewDragClamped(t *testing.T) {
	var st SplitState
	var left, right *DivEl
	u := New(nil, nil, func() Element {
		left, right = Div(), Div()
		return SplitView(&st, []SplitPane{
			{El: left, Preferred: 200, Minimum: 100},
			{El: right, Fill: true, Minimum: 150},
		}, SplitOpts{}).Size(600, 300)
	})
	frame := func() { u.Layout(800, 400); u.Layout(800, 400) }
	frame()
	if w := left.Rect().W; w != 200 {
		t.Fatalf("left pane %v wide, want 200", w)
	}
	// The handle sits right of the left pane; drag it 50 px right.
	h := left.Rect().X + left.Rect().W + 2
	u.Drag(h, 150, h+50, 150)
	frame()
	if w := left.Rect().W; w != 250 {
		t.Errorf("after a 50 px drag the left pane is %v, want 250", w)
	}
	// Past its minimum it stops.
	h = left.Rect().X + left.Rect().W + 2
	u.Drag(h, 150, h-300, 150)
	frame()
	if w := left.Rect().W; w != 100 || st.Offset(0) != -100 {
		t.Errorf("dragged past the minimum: %v wide (offset %v), want 100", w, st.Offset(0))
	}
}

func TestDelayButtonHold(t *testing.T) {
	var st DelayState
	fired := 0
	var btn *DivEl
	u := New(nil, nil, func() Element {
		btn = DelayButton("Hold", &st, DelayOpts{Delay: 200 * time.Millisecond, OnActivated: func() { fired++ }})
		return Div().Children(btn)
	})
	frame := func() { u.Layout(300, 200) }
	frame()
	r := Bounds(Children(btn)[0])
	x, y := r.X+r.W/2, r.Y+r.H/2
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}, {Kind: app.MouseDown, X: x, Y: y, Button: 1}})
	frame()
	core.TickAnimations(100 * time.Millisecond)
	frame()
	if fired != 0 || st.Progress() < 0.4 || st.Progress() > 0.6 {
		t.Errorf("half held: fired %d, progress %v", fired, st.Progress())
	}
	core.TickAnimations(150 * time.Millisecond)
	frame()
	frame()
	if fired != 1 {
		t.Errorf("held past the delay: fired %d, want 1", fired)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: x, Y: y, Button: 1}})
	frame()
	if fired != 1 || st.Progress() != 0 {
		t.Errorf("released: fired %d, progress %v; want 1, 0", fired, st.Progress())
	}
}

func TestSmallControls(t *testing.T) {
	picked := -1
	var pi *DivEl
	var round *DivEl
	var dial Element
	var v core.Property[float32]
	v.Set(0.5)
	u := New(nil, nil, func() Element {
		pi = PageIndicator(4, 1, func(i int) { picked = i }, PageIndicatorOpts{})
		round = RoundButton("+", ButtonOpts{})
		dial = DialControl(&v, 0, 1, DialOpts{Size: 80})
		return Div().Col().Children(pi, round, dial, Separator(false), ToolBar(ToolButton("A", ButtonOpts{})),
			GroupBox("Options", Text("x", RGB(0))))
	})
	u.Layout(400, 400)
	u.Layout(400, 400)
	if n := len(Children(pi)); n != 4 {
		t.Errorf("page indicator has %d dots, want 4", n)
	}
	r := Bounds(Children(pi)[3])
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: r.X + 4, Y: r.Y + 4, Button: 1}, {Kind: app.MouseUp, X: r.X + 4, Y: r.Y + 4, Button: 1}})
	if picked != 3 {
		t.Errorf("clicked dot 3, picked %d", picked)
	}
	if b := Bounds(round); b.W != 40 || b.H != 40 {
		t.Errorf("round button %vx%v, want 40x40", b.W, b.H)
	}
	if b := Bounds(dial); b.W != 80 || b.H != 80 {
		t.Errorf("dial %vx%v, want 80x80", b.W, b.H)
	}
}
