package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

// Backspace and Delete are edits: OnEdit hears them, down to the empty
// field; a key that changes nothing (Home) is not one.
func TestDeletionIsAnEdit(t *testing.T) {
	s := NewInput("ab")
	var edits []string
	s.OnEdit(func(t string) { edits = append(edits, t) })
	s.Key(app.KeyEvent{Down: true, Key: app.KeyBackspace})
	s.Key(app.KeyEvent{Down: true, Key: app.KeyBackspace})
	s.Key(app.KeyEvent{Down: true, Key: app.KeyHome})
	if len(edits) != 2 || edits[0] != "a" || edits[1] != "" {
		t.Errorf("edits %q, want [a \"\"]", edits)
	}
}
