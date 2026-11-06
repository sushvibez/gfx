package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

// A validator REFUSES a keystroke; it does not flag the text afterwards.
// That is QML's `validator:` contract, and the reason a numeric field never
// holds "12x": the x never goes in.
func TestInputValidatorRefusesKeystrokes(t *testing.T) {
	st := NewInput("")
	digits := func(s string) bool {
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	st.Accept(digits)
	type_ := func(s string) {
		for _, r := range s {
			st.Key(app.KeyEvent{Down: true, Rune: r})
		}
	}
	type_("12x9")
	if got := st.Text.Get(); got != "129" {
		t.Fatalf("typed 12x9 into a digits-only field, got %q, want 129", got)
	}
	// A refused keystroke must not move the caret either, or the next
	// character lands in the wrong place.
	st.Key(app.KeyEvent{Down: true, Rune: 'z'})
	type_("4")
	if got := st.Text.Get(); got != "1294" {
		t.Errorf("after a refused keystroke, got %q, want 1294", got)
	}
	// Deleting is always allowed: a validator that refuses the empty
	// string must not trap the user with text they cannot clear.
	st.Accept(func(s string) bool { return len(s) >= 2 })
	for range 5 {
		st.Key(app.KeyEvent{Down: true, Key: app.KeyBackspace})
	}
	if got := st.Text.Get(); got != "" {
		t.Errorf("backspace blocked by the validator: %q", got)
	}
}

// maximumLength caps the field, and the cap applies to typing and pasting
// alike.
func TestInputMaxLen(t *testing.T) {
	st := NewInput("")
	st.MaxLen(3)
	for _, r := range "abcdef" {
		st.Key(app.KeyEvent{Down: true, Rune: r})
	}
	if got := st.Text.Get(); got != "abc" {
		t.Errorf("MaxLen(3) held %q, want abc", got)
	}
}

// echoMode: Password shows a mask, and the caret/selection maths run on
// what is DRAWN — a click in a password field has to land where the bullets
// are, not where the letters would have been.
func TestInputEchoMasksDisplayNotValue(t *testing.T) {
	st := NewInput("secret")
	in := Input(st, 200).Echo('•')
	if got := in.shown(); got != "••••••" {
		t.Errorf("password field draws %q, want six bullets", got)
	}
	if got := st.Text.Get(); got != "secret" {
		t.Errorf("the field's VALUE was masked too: %q", got)
	}
	// NoEcho draws nothing at all, and the field is still not empty.
	in.Echo(0)
	if got := in.shown(); got != "" {
		t.Errorf("NoEcho draws %q, want nothing", got)
	}
	if st.Text.Get() != "secret" {
		t.Error("NoEcho changed the value")
	}
	// A field with no echo mode draws itself.
	if got := Input(st, 200).shown(); got != "secret" {
		t.Errorf("plain field draws %q", got)
	}
}

// A single-line field scrolls its text left to keep the caret inside it,
// and back when the caret returns; text that fits never scrolls.
func TestInputScrollsToCaret(t *testing.T) {
	st := NewInput("")
	e := &InputEl{state: st, rect: Rect{0, 0, 120, 30}} // 100 px inside the padding
	e.keepCaretVisible(250, 400)                        // caret far right of a long text
	if st.scrollX != 150 {
		t.Errorf("caret at 250: scrollX = %v, want 150", st.scrollX)
	}
	e.keepCaretVisible(180, 400) // still in view: no change
	if st.scrollX != 150 {
		t.Errorf("caret at 180: scrollX = %v, want it to stay 150", st.scrollX)
	}
	e.keepCaretVisible(40, 400) // Home-ward: scroll back to the caret
	if st.scrollX != 40 {
		t.Errorf("caret at 40: scrollX = %v, want 40", st.scrollX)
	}
	e.keepCaretVisible(60, 80) // the text now fits: no scroll at all
	if st.scrollX != 0 {
		t.Errorf("text that fits: scrollX = %v, want 0", st.scrollX)
	}
}
