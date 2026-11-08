package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestSpinBox(t *testing.T) {
	var v core.Property[int]
	v.Set(8)
	in := NewInput("")
	wrap := false
	mods := 0
	var box *DivEl
	u := New(nil, nil, func() Element {
		box = SpinBox(&v, SpinOpts{From: 0, To: 10, Step: 2, Wrap: wrap, Input: in, OnModified: func() { mods++ }}).FocusID("spin")
		return box
	})
	frame := func() { u.Layout(300, 60) }
	clickAt := func(x, y float32) {
		frame()
		u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: x, Y: y, Button: 1}, {Kind: app.MouseUp, X: x, Y: y, Button: 1}})
		frame()
	}
	frame()
	plus := func() { // the third child is the + button
		b := Bounds(Children(box)[2])
		clickAt(b.X+b.W/2, b.Y+b.H/2)
	}
	plus()
	plus() // clamped at 10
	if v.Get() != 10 || mods != 1 {
		t.Errorf("after + twice from 8: %d (mods %d), want 10 (1)", v.Get(), mods)
	}
	wrap = true
	plus() // wraps to From
	if v.Get() != 0 {
		t.Errorf("wrapping + from 10: %d, want 0", v.Get())
	}
	if !u.FocusID("spin") { // keys go to the focused box
		t.Fatal("the spin box is not focusable")
	}
	u.Key(app.KeyEvent{Down: true, Key: app.KeyUp})
	if v.Get() != 2 {
		t.Errorf("Up from 0: %d, want 2", v.Get())
	}
	frame()
	r := box.Rect()
	for i := 0; i < 10; i++ { // a notch's worth of touchpad tenths, turned away
		u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: r.X + 5, Y: r.Y + 5, ScrollY: -float32(app.WheelNotchPx) / 10}})
	}
	if v.Get() != 4 {
		t.Errorf("a notch of wheel up from 2: %d, want 4", v.Get())
	}
	in.SetText("7")
	in.Key(app.KeyEvent{Down: true, Key: app.KeyEnter}) // typed 7: clamps to 7, a step need not hold
	if v.Get() != 7 {
		t.Errorf("typed 7: %d", v.Get())
	}
	in.SetText("x")
	in.Key(app.KeyEvent{Down: true, Key: app.KeyEnter})
	if v.Get() != 7 || in.Text.Get() != "7" {
		t.Errorf("typed x: value %d field %q, want 7 kept and shown", v.Get(), in.Text.Get())
	}
}
