package gml

import "testing"

// qsTr looks its text up in the app's catalogue (SetTranslator); the NOOP
// markers do not; .arg fills the lowest %N marker, so a translation can
// reorder them.
func TestTranslatorAndArg(t *testing.T) {
	SetTranslator(func(s string) string {
		switch s {
		case "Save":
			return "Speichern"
		case "%1 of %2":
			return "%2 — %1"
		}
		return s
	})
	defer SetTranslator(nil)
	for _, c := range []struct{ src, want string }{
		{`qsTr("Save")`, "Speichern"},
		{`qsTr("Open")`, "Open"},
		{`QT_TR_NOOP("Save")`, "Save"},
		{`qsTr("%1 of %2").arg(3).arg(9)`, "9 — 3"},
		{`"%1 and %10 and %1".arg("a")`, "a and %10 and a"},
		{`"no markers".arg("x")`, "no markers"},
	} {
		if got := evalStr(t, NewEnv(), c.src); got != c.want {
			t.Errorf("%s = %q, want %q", c.src, got, c.want)
		}
	}
}

// Display text is translated with no markup: a literal when parsed for
// display, a computed value as a whole when evaluated; a text field's text
// and the strings inside an expression (compared as data) are not.
func TestDisplayTextTranslated(t *testing.T) {
	SetTranslator(func(s string) string {
		switch s {
		case "Save":
			return "Speichern"
		case "Props (3)":
			return "Requisiten (3)"
		case "wall":
			return "WAND" // must never reach a comparison
		}
		return s
	})
	defer SetTranslator(nil)
	root, err := parseForDisplay(`Item {
    Text { text: "Save" }
    Text { text: "Props (" + n + ")" }
    Text { text: mode == "wall" ? "Save" : "other" }
    TextField { text: "Save" }
    Rectangle { ToolTip.text: "Save" }
}`)
	if err != nil {
		t.Fatal(err)
	}
	env := NewEnv()
	env.Set("n", 3)
	env.Set("mode", "wall")
	val := func(n *node, name string) string {
		for _, a := range n.attrs {
			if a.name == name {
				v, err := a.expr.eval(env)
				if err != nil {
					t.Fatal(err)
				}
				return str(v)
			}
		}
		return "<none>"
	}
	c := root.children
	for i, want := range []string{"Speichern", "Requisiten (3)", "Speichern", "Save"} {
		if got := val(c[i], "text"); got != want {
			t.Errorf("child %d text = %q, want %q", i, got, want)
		}
	}
	if got := val(c[4], "ToolTip.text"); got != "Speichern" {
		t.Errorf("tooltip = %q", got)
	}
}

// DisplayTexts finds a page's display texts as catalogue keys.
func TestDisplayTexts(t *testing.T) {
	got, err := DisplayTexts(`Item {
    Text { text: "Save" }
    Text { text: "Props (" + n + ")" }
    Text { text: mode == "wall" ? "Wall" : "Sphere" }
    TextField { text: "typed" }
    Rectangle { ToolTip.text: count + " of " + total }
    Repeater { model: [["props", "Props"], ["sounds", "Sound files"]] }
    Text { text: "·" }
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Save", "Props (%1)", "Wall", "Sphere", "%1 of %2", "Props", "Sound files"}
	if len(got) != len(want) {
		t.Fatalf("DisplayTexts = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DisplayTexts = %q, want %q", got, want)
		}
	}
}
