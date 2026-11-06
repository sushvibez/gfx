package gml

import "testing"

// TestTextFieldFollowsBinding: a TextField whose text is bound to a
// property shows the new value when the property changes (QML's text
// setter), but never overwrites what the user is typing in a focused
// field.
func TestTextFieldFollowsBinding(t *testing.T) {
	src := `
Column {
    property string v: "one"
    TextField { id: f; width: 100; text: v }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	_ = d.View()()
	if len(d.doc.inputs) != 1 {
		t.Fatalf("inputs = %d, want 1", len(d.doc.inputs))
	}
	var st = func() string {
		for _, s := range d.doc.inputs {
			return s.Text.Get()
		}
		return ""
	}
	if st() != "one" {
		t.Fatalf("initial text = %q", st())
	}
	if err := env.vars["v"].(Ref).set("two"); err != nil {
		t.Fatal(err)
	}
	_ = d.View()()
	if st() != "two" {
		t.Fatalf("text after binding change = %q, want two", st())
	}
	// Focused: the user's text wins over a binding change.
	for _, s := range d.doc.inputs {
		s.SetFocused(true)
		s.SetText("typing")
	}
	if err := env.vars["v"].(Ref).set("three"); err != nil {
		t.Fatal(err)
	}
	_ = d.View()()
	if st() != "typing" {
		t.Fatalf("focused field overwritten: %q", st())
	}
	// Unfocused again with the binding unchanged since: keeps the typed text.
	for _, s := range d.doc.inputs {
		s.SetFocused(false)
	}
	_ = d.View()()
	if st() != "typing" {
		t.Fatalf("unchanged binding re-seeded the field: %q", st())
	}
}
