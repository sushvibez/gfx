package ui

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestDrawerSlideDragAndScrim(t *testing.T) {
	var st DrawerState
	closed := 0
	var dr Element
	u := New(nil, nil, func() Element {
		dr = Drawer(&st, Div().Children(Text("Nav", RGB(0))), DrawerOpts{Size: 200, OnClosed: func() { closed++ }})
		return Div().Children(dr)
	})
	frame := func() { u.Layout(800, 600) }
	settle := func() {
		for i := 0; i < 40; i++ {
			core.TickAnimations(20 * time.Millisecond)
			frame()
		}
	}
	panel := func() Rect { return Bounds(Children(dr)[0]) }
	frame()
	if len(Children(dr)) != 0 {
		t.Fatal("a closed drawer drew something")
	}
	st.Open()
	frame()
	settle()
	if r := panel(); r.X != 0 || r.W != 200 || r.H != 600 || st.Position() != 1 {
		t.Fatalf("open drawer at %+v (position %v), want 0,0 200x600", r, st.Position())
	}
	// A short drag toward the edge springs back; a long one closes it.
	u.Drag(150, 300, 100, 300)
	settle()
	if !st.IsOpen() || st.Position() != 1 || closed != 0 {
		t.Errorf("a 50 px drag: open %v at %v, closed %d; want it back open", st.IsOpen(), st.Position(), closed)
	}
	u.Drag(150, 300, 20, 300)
	settle()
	if st.IsOpen() || st.Position() != 0 || closed != 1 {
		t.Errorf("a 130 px drag: open %v at %v, closed %d; want closed once", st.IsOpen(), st.Position(), closed)
	}
	// A click on the scrim closes it.
	st.Open()
	settle()
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 600, Y: 300, Button: 1}, {Kind: app.MouseUp, X: 600, Y: 300, Button: 1}})
	settle()
	if st.IsOpen() || closed != 2 {
		t.Errorf("scrim click: open %v, closed %d; want closed", st.IsOpen(), closed)
	}
	// A right drawer sits against the right edge.
	st.SetPosition(1)
	u2 := New(nil, nil, func() Element {
		dr = Drawer(&st, nil, DrawerOpts{Edge: EdgeRight, Size: 100})
		return Div().Children(dr)
	})
	u2.Layout(800, 600)
	if r := panel(); r.X != 700 || r.W != 100 {
		t.Errorf("right drawer at %+v, want x 700, 100 wide", r)
	}
}

func TestTabBar(t *testing.T) {
	cur := 0
	var bar *DivEl
	u := New(nil, nil, func() Element {
		bar = TabBar(cur, []Tab{{Label: "A"}, {Label: "B"}, {Label: "C"}}, func(i int) { cur = i }).W(300).FocusID("tabs")
		return Div().Children(bar)
	})
	frame := func() { u.Layout(400, 300); u.Layout(400, 300) }
	frame()
	tabs := Children(bar)
	if r := Bounds(tabs[1]); r.H != TabBarHeight || r.W < 98 || r.W > 100 {
		t.Errorf("tab is %vx%v, want a third of 300 by %d", r.W, r.H, TabBarHeight)
	}
	if c, _ := FillColor(tabs[0]); c != RGB(TabCurrentColor) {
		t.Errorf("current tab %v, want the light colour", c)
	}
	r := Bounds(tabs[2])
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: r.X + 5, Y: r.Y + 5, Button: 1}, {Kind: app.MouseUp, X: r.X + 5, Y: r.Y + 5, Button: 1}})
	frame()
	if cur != 2 {
		t.Fatalf("clicked tab 2, current %d", cur)
	}
	u.FocusID("tabs")
	frame()
	u.Key(app.KeyEvent{Down: true, Key: app.KeyLeft})
	frame()
	if cur != 1 {
		t.Errorf("Left from tab 2: current %d, want 1", cur)
	}
}
