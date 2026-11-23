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

// A filtered list narrows as the filter is typed; Enter picks the first
// match and clears the filter; a long list scrolls instead of growing.
func TestComboBoxFilter(t *testing.T) {
	var cur core.Property[int]
	var open core.Property[bool]
	filter := NewInput("")
	items := []string{"brick", "Concrete", "herringbone_brick_03", "metal", "wood"}
	for i := 0; i < 20; i++ {
		items = append(items, "tile_"+string(rune('a'+i)))
	}
	u := New(nil, nil, func() Element {
		return Div().Children(ComboBox(&cur, &open, ComboOpts{Items: items, Filter: filter, Width: 220}))
	})
	frame := func() { u.Layout(400, 600) }
	frame()
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 40, Y: 16, Button: 1}, {Kind: app.MouseUp, X: 40, Y: 16, Button: 1}})
	frame()
	if !open.Get() {
		t.Fatal("not open")
	}
	filter.SetText("BRICK")
	frame()
	u.Key(app.KeyEvent{Down: true, Key: app.KeyEnter})
	frame()
	if cur.Get() != 0 || open.Get() || filter.Text.Get() != "" {
		t.Errorf("Enter on 'brick': current %d open %v filter %q, want 0 false \"\"", cur.Get(), open.Get(), filter.Text.Get())
	}
	filter.SetText("03")
	open.Set(true)
	frame()
	u.Key(app.KeyEvent{Down: true, Key: app.KeyEnter})
	frame()
	if cur.Get() != 2 {
		t.Errorf("'03' picks herringbone_brick_03: current %d", cur.Get())
	}
}
