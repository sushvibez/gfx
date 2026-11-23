package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestNumberField(t *testing.T) {
	var v core.Property[float64]
	v.Set(2)
	st := NewNumberState()
	mods, ends := 0, 0
	var box *DivEl
	u := New(nil, nil, func() Element {
		box = NumberField(&v, st, NumberOpts{Min: 0, Max: 10, Step: 0.5, Label: "Height", Unit: "m", Decimals: 2,
			OnModified: func() { mods++ }, OnDragEnd: func() { ends++ }}).FocusID("num")
		return box
	})
	frame := func() { u.Layout(300, 60) }
	press := func(kind app.MouseKind, x, y float32, shift, ctrl bool) {
		u.Mouse([]app.MouseEvent{{Kind: kind, X: x, Y: y, Button: 1, Shift: shift, Ctrl: ctrl}})
	}
	frame()
	kids := Children(box)
	left, mid, right := Bounds(kids[0]), Bounds(kids[1]), Bounds(kids[2])
	click := func(r Rect) {
		frame()
		press(app.MouseDown, r.X+r.W/2, r.Y+r.H/2, false, false)
		press(app.MouseUp, r.X+r.W/2, r.Y+r.H/2, false, false)
		frame()
	}
	near := func(what string, want float64) {
		t.Helper()
		if d := v.Get() - want; d > 1e-9 || d < -1e-9 {
			t.Errorf("%s: %v, want %v", what, v.Get(), want)
		}
	}

	click(right)
	near("› steps up", 2.5)
	click(left)
	click(left)
	near("‹ twice", 1.5)

	// Drag right 60 px: 10 steps of 0.5 from 1.5 → 6.5; no edit afterwards.
	cx, cy := mid.X+mid.W/2, mid.Y+mid.H/2
	press(app.MouseDown, cx, cy, false, false)
	press(app.MouseMove, cx+60, cy, false, false)
	press(app.MouseUp, cx+60, cy, false, false)
	frame()
	near("drag 60 px", 6.5)
	if ends != 1 || st.Editing() {
		t.Errorf("after a drag: %d drag ends, editing %v", ends, st.Editing())
	}
	// Shift drags ten times finer; Ctrl snaps to whole steps.
	press(app.MouseDown, cx, cy, false, false)
	press(app.MouseMove, cx+60, cy, true, false)
	press(app.MouseUp, cx+60, cy, true, false)
	frame()
	near("shift drag", 7)
	press(app.MouseDown, cx, cy, false, false)
	press(app.MouseMove, cx-8, cy, false, true)
	press(app.MouseUp, cx-8, cy, false, true)
	frame()
	near("ctrl drag snaps", 6.5)
	// Far past the end clamps.
	press(app.MouseDown, cx, cy, false, false)
	press(app.MouseMove, cx+1000, cy, false, false)
	press(app.MouseUp, cx+1000, cy, false, false)
	frame()
	near("clamped at Max", 10)

	// A click types: the text is the value; Enter commits.
	click(mid)
	if !st.Editing() || st.Input.Text.Get() != "10" {
		t.Fatalf("click to type: editing %v text %q", st.Editing(), st.Input.Text.Get())
	}
	st.Input.SetText("2*1.5 m")
	u.Key(app.KeyEvent{Down: true, Key: app.KeyEnter})
	frame()
	near("typed 2*1.5", 3)
	if st.Editing() {
		t.Error("still editing after Enter")
	}
	// Escape cancels: the value stays.
	click(mid)
	st.Input.SetText("9")
	u.Key(app.KeyEvent{Down: true, Key: app.KeyEscape})
	frame()
	near("escape", 3)
	if st.Editing() {
		t.Error("still editing after Escape")
	}
	// Keys step while focused.
	if !u.FocusID("num") {
		t.Fatal("not focusable")
	}
	u.Key(app.KeyEvent{Down: true, Key: app.KeyUp})
	near("Up", 3.5)
	if mods == 0 {
		t.Error("OnModified never ran")
	}
}

func TestNumberFormatParse(t *testing.T) {
	for _, c := range []struct {
		v    float64
		dec  int
		want string
	}{{1.5, 2, "1.50"}, {1.25, -1, "1.25"}, {3, -1, "3"}, {-0.0001, 2, "0.00"}, {0.1 + 0.2, -1, "0.3"}} {
		if got := FormatNumber(c.v, c.dec); got != c.want {
			t.Errorf("FormatNumber(%v, %d) = %q, want %q", c.v, c.dec, got, c.want)
		}
	}
	for _, c := range []struct {
		in   string
		want float64
		ok   bool
	}{{"1.5", 1.5, true}, {" 2 m", 2, true}, {"90°", 90, true}, {"-3", -3, true}, {"10-0.25", 9.75, true},
		{"2*1.5", 3, true}, {"1/4", 0.25, true}, {"1e2", 100, true}, {"1/0", 0, false}, {"abc", 0, false}} {
		got, ok := ParseNumber(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseNumber(%q) = %v %v, want %v %v", c.in, got, ok, c.want, c.ok)
		}
	}
}
