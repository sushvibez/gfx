package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestComboBox(t *testing.T) {
	var cur core.Property[int]
	var open core.Property[bool]
	in := NewInput("")
	editable := false
	activated, accepted := -1, ""
	u := New(nil, nil, func() Element {
		o := ComboOpts{Items: []string{"red", "green", "blue"}, OnActivated: func(i int) { activated = i },
			OnAccepted: func(s string) { accepted = s }}
		if editable {
			o.Input = in
		}
		return Div().Children(ComboBox(&cur, &open, o).(*DivEl).FocusID("combo"))
	})
	frame := func() { u.Layout(400, 300) }
	click := func(x, y float32) {
		frame()
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}, {Kind: app.MouseDown, X: x, Y: y, Button: 1}, {Kind: app.MouseUp, X: x, Y: y, Button: 1}})
		frame()
	}
	click(40, 16) // open
	if !open.Get() {
		t.Fatal("a click did not open the list")
	}
	click(40, 36+2+28+14) // the second row
	if cur.Get() != 1 || open.Get() || activated != 1 {
		t.Errorf("picked green: current %d open %v activated %d", cur.Get(), open.Get(), activated)
	}
	click(40, 16)
	click(300, 250) // outside: closes, picks nothing
	if open.Get() || cur.Get() != 1 {
		t.Errorf("outside click: open %v current %d", open.Get(), cur.Get())
	}
	u.FocusID("combo")
	u.Key(app.KeyEvent{Down: true, Key: app.KeyDown})
	if cur.Get() != 2 {
		t.Errorf("Down from green: %d, want 2", cur.Get())
	}
	editable = true
	frame()
	in.SetText("red")
	in.Key(app.KeyEvent{Down: true, Key: app.KeyEnter})
	if cur.Get() != 0 {
		t.Errorf("typed red + Enter: current %d, want 0", cur.Get())
	}
	in.SetText("purple")
	in.Key(app.KeyEvent{Down: true, Key: app.KeyEnter})
	if accepted != "purple" || cur.Get() != 0 {
		t.Errorf("typed purple + Enter: accepted %q current %d, want purple and unchanged", accepted, cur.Get())
	}
}
