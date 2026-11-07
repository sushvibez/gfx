package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

func TestParseKeySequence(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want KeyCombo
		ok   bool
	}{
		{"Ctrl+S", KeyCombo{Rune: 'S', Ctrl: true}, true},
		{"ctrl+shift+z", KeyCombo{Rune: 'Z', Ctrl: true, Shift: true}, true},
		{"F5", KeyCombo{Key: app.KeyF5}, true},
		{"Esc", KeyCombo{Key: app.KeyEscape}, true},
		{"Alt+Left", KeyCombo{Key: app.KeyLeft, Alt: true}, true},
		{"Ctrl++", KeyCombo{Rune: '+', Ctrl: true}, true},
		{"Ctrl+,", KeyCombo{Rune: ',', Ctrl: true}, true},
		{"Space", KeyCombo{Rune: ' '}, true},
		{"Ctrl+K, Ctrl+C", KeyCombo{}, false},
		{"Meta+X", KeyCombo{}, false},
	} {
		got, ok := ParseKeySequence(tc.in)
		if ok != tc.ok || ok && got != tc.want {
			t.Errorf("ParseKeySequence(%q) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestKeyComboMatches(t *testing.T) {
	ctrlS, _ := ParseKeySequence("Ctrl+S")
	plus, _ := ParseKeySequence("Ctrl++")
	for _, tc := range []struct {
		c    KeyCombo
		ev   app.KeyEvent
		want bool
	}{
		{ctrlS, app.KeyEvent{Down: true, Rune: 's', Ctrl: true}, true},
		{ctrlS, app.KeyEvent{Down: false, Rune: 's', Ctrl: true}, false},
		{ctrlS, app.KeyEvent{Down: true, Rune: 's', Ctrl: true, Alt: true}, false},
		{ctrlS, app.KeyEvent{Down: true, Rune: 's', Ctrl: true, Shift: true}, false},
		{plus, app.KeyEvent{Down: true, Rune: '+', Ctrl: true, Shift: true}, true}, // Shift types the +
		{KeyCombo{Key: app.KeyF5}, app.KeyEvent{Down: true, Key: app.KeyF5}, true},
	} {
		if got := tc.c.Matches(tc.ev); got != tc.want {
			t.Errorf("%+v.Matches(%+v) = %v, want %v", tc.c, tc.ev, got, tc.want)
		}
	}
	if cs := StdRedo.Combos(); len(cs) != 2 || cs[1] != (KeyCombo{Rune: 'Z', Ctrl: true, Shift: true}) {
		t.Errorf("StdRedo.Combos() = %+v", cs)
	}
}

// DivEl.Shortcut fires wherever focus is, several on one element, but not
// for a plain key a focused text field types.
func TestDivShortcut(t *testing.T) {
	saves, finds, xs := 0, 0, 0
	field := NewInput("")
	u := New(nil, nil, func() Element {
		return Div().Col().
			Shortcut(func() { saves++ }, StdSave).
			Shortcut(func() { finds++ }, "Ctrl+F", "F3").
			Shortcut(func() { xs++ }, "X").
			Children(Input(field, 100))
	})
	u.Layout(200, 100)
	u.Focus(field)
	for _, ev := range []app.KeyEvent{
		{Down: true, Rune: 's', Ctrl: true},
		{Down: true, Rune: 'f', Ctrl: true},
		{Down: true, Key: app.KeyF3},
		{Down: true, Rune: 'x'}, // typed into the field
	} {
		u.Key(ev)
		u.Layout(200, 100)
	}
	if saves != 1 || finds != 2 || xs != 0 {
		t.Errorf("saves %d, finds %d, xs %d; want 1, 2, 0", saves, finds, xs)
	}
	if field.Text.Get() != "x" {
		t.Errorf("field = %q, want the typed x", field.Text.Get())
	}
}
