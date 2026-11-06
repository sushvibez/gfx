package ui

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/app"
)

// fakeClock makes the gesture clock step by hand.
func fakeClock(t *testing.T) *time.Time {
	now := time.Unix(1000, 0)
	prev := nowFn
	nowFn = func() time.Time { return now }
	t.Cleanup(func() { nowFn = prev })
	return &now
}

func click(u *UI, x, y float32) {
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, Button: 1, X: x, Y: y}})
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, Button: 1, X: x, Y: y}})
}

func TestDoubleClick(t *testing.T) {
	now := fakeClock(t)
	clicks, doubles := 0, 0
	u := New(nil, nil, func() Element {
		return Div().Size(100, 100).OnClick(func() { clicks++ }).OnDoubleClick(func() { doubles++ })
	})
	u.Layout(200, 200)
	click(u, 50, 50)
	*now = now.Add(200 * time.Millisecond)
	click(u, 50, 50)
	if clicks != 2 || doubles != 1 {
		t.Fatalf("two quick clicks: %d clicks, %d doubles; want 2 and 1", clicks, doubles)
	}
	*now = now.Add(time.Second)
	click(u, 50, 50)
	*now = now.Add(time.Second)
	click(u, 50, 50)
	if doubles != 1 {
		t.Errorf("two slow clicks made a double click (%d)", doubles)
	}
}

func TestPressAndHold(t *testing.T) {
	now := fakeClock(t)
	clicks, holds := 0, 0
	u := New(nil, nil, func() Element {
		return Div().Size(100, 100).OnClick(func() { clicks++ }).OnPressAndHold(func() { holds++ })
	})
	u.Layout(200, 200)
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, Button: 1, X: 50, Y: 50}})
	if !u.WantsFrame() {
		t.Error("a held press does not keep frames coming")
	}
	*now = now.Add(500 * time.Millisecond)
	u.Mouse(nil)
	if holds != 0 {
		t.Fatal("held fired before its delay")
	}
	*now = now.Add(400 * time.Millisecond)
	u.Mouse(nil)
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, Button: 1, X: 50, Y: 50}})
	if holds != 1 || clicks != 0 {
		t.Errorf("held 900ms: %d holds, %d clicks; want 1 and 0", holds, clicks)
	}
	click(u, 50, 50) // a quick press is a click again
	if clicks != 1 || holds != 1 {
		t.Errorf("a quick click after: %d clicks, %d holds; want 1 and 1", clicks, holds)
	}
}

func TestWheelAndHoverMove(t *testing.T) {
	var gotDY, hx, hy float32
	u := New(nil, nil, func() Element {
		return Div().Size(100, 100).
			OnWheel(func(dx, dy float32) bool { gotDY += dy; return true }).
			OnHoverMove(func(x, y float32) { hx, hy = x, y })
	})
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 30, Y: 40}})
	u.Layout(200, 200)
	if hx != 30 || hy != 40 {
		t.Errorf("hover at %v,%v, want 30,40", hx, hy)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 30, Y: 40, ScrollY: 36}})
	if gotDY != 36 {
		t.Errorf("wheel dy = %v, want 36", gotDY)
	}
}
