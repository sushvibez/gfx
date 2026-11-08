package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestButtonsAndSwitch(t *testing.T) {
	clicks, offClicks := 0, 0
	var checked, sw core.Property[bool]
	var b, off, chk *DivEl
	var swEl Element
	u := New(nil, nil, func() Element {
		b = Button("Go", ButtonOpts{OnClick: func() { clicks++ }}).FocusID("go")
		off = Button("No", ButtonOpts{OnClick: func() { offClicks++ }, Disabled: true})
		chk = ToolButton("Bold", ButtonOpts{Checked: &checked})
		swEl = Switch(&sw, "")
		return Div().Col().Children(b, off, chk, swEl)
	})
	frame := func() { u.Layout(300, 300) }
	frame() // the elements exist from the first layout
	click := func(el Element) {
		frame()
		r := Bounds(el)
		u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: r.X + r.W/2, Y: r.Y + r.H/2, Button: 1}, {Kind: app.MouseUp, X: r.X + r.W/2, Y: r.Y + r.H/2, Button: 1}})
		frame()
	}
	click(b)
	click(off)
	if clicks != 1 || offClicks != 0 {
		t.Errorf("clicks %d, disabled clicks %d; want 1, 0", clicks, offClicks)
	}
	u.FocusID("go")
	u.Key(app.KeyEvent{Down: true, Key: app.KeyEnter})
	if clicks != 2 {
		t.Errorf("Enter on the focused button: clicks %d, want 2", clicks)
	}
	click(chk)
	if !checked.Get() {
		t.Error("a checkable tool button did not check")
	}
	click(swEl)
	if !sw.Get() {
		t.Error("the switch did not flip")
	}
}

func TestPageSugar(t *testing.T) {
	head, body, foot := Div().H(30), Div(), Div().H(20)
	u := New(nil, nil, func() Element { return Div().Children(Page(head, body, foot).Size(200, 150)) })
	u.Layout(300, 300)
	if r := body.Rect(); r.Y != 30 || r.H != 100 {
		t.Errorf("page content at %+v, want y 30, 100 tall", r)
	}
}
