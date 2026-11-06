package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TextArea and TextEdit are multi-line: a host-registered state behind
// one becomes a text area's, and a TextField stays a single line.
func TestTextAreaIsMultiline(t *testing.T) {
	env := NewEnv()
	notes, title := ui.NewInput(""), ui.NewInput("")
	env.Input("notes", notes)
	env.Input("title", title)
	src := `
Column {
    TextField { id: title; width: 200 }
    TextArea { id: notes; width: 300; height: 120; text: "one" }
}`
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	if !notes.IsMultiline() {
		t.Error("a TextArea's state is not multi-line")
	}
	if title.IsMultiline() {
		t.Error("a TextField's state became multi-line")
	}
}
