package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

func typeInto(s *InputState, text string) {
	for _, r := range text {
		s.Key(app.KeyEvent{Down: true, Rune: r})
	}
}

func TestInputMask(t *testing.T) {
	s := NewInput("")
	s.SetMask("999-999;_")
	typeInto(s, "12")
	if got := s.Text.Get(); got != "12" {
		t.Errorf("after 12: %q", got)
	}
	typeInto(s, "3")
	if got := s.Text.Get(); got != "123-" {
		t.Errorf("after 123: %q, want the literal put in", got)
	}
	typeInto(s, "x4") // x is refused
	if got := s.Text.Get(); got != "123-4" {
		t.Errorf("after x4: %q, want x refused", got)
	}
	if s.Acceptable() {
		t.Error("a half-filled mask reports acceptable")
	}
	s.Key(app.KeyEvent{Down: true, Key: app.KeyBackspace})
	s.Key(app.KeyEvent{Down: true, Key: app.KeyBackspace})
	if got := s.Text.Get(); got != "123" {
		t.Errorf("backspace twice: %q, want 123 (the literal deletes too)", got)
	}
	typeInto(s, "456789") // only three more fit
	if got := s.Text.Get(); got != "123-456" || !s.Acceptable() {
		t.Errorf("filled: %q acceptable %v", got, s.Acceptable())
	}
	u := NewInput("")
	u.SetMask(">AA:99")
	typeInto(u, "ab12")
	if got := u.Text.Get(); got != "AB:12" {
		t.Errorf("upper-case mask: %q, want AB:12", got)
	}
}

func TestInputSelection(t *testing.T) {
	s := NewInput("hello world")
	s.Select(6, 11)
	if a, b := s.Selection(); a != 6 || b != 11 || s.SelectedText() != "world" {
		t.Errorf("selection %d..%d %q", a, b, s.SelectedText())
	}
	s.Deselect()
	if s.SelectedText() != "" || s.Cursor() != 11 {
		t.Errorf("after Deselect: %q cursor %d", s.SelectedText(), s.Cursor())
	}
	s.SetCursor(99)
	if s.Cursor() != 11 {
		t.Errorf("SetCursor past the end: %d", s.Cursor())
	}
}
