package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// When a Repeater's model changes, a row's click handler sees the row's
// new modelData, not what that row showed before (an outliner whose
// category opened: row 1 is now the category's first item).
func TestRepeaterClickSeesNewModel(t *testing.T) {
	src := `
Item {
    width: 300; height: 300
    Column {
        Repeater { model: rows
            Rectangle { width: 300; height: 20
                Text { text: modelData.label }
                MouseArea { x: 0; y: 0; width: 300; height: 20; onClicked: pick(modelData.label) } } }
    }
}`
	env := NewEnv()
	set := func(labels ...string) {
		var rows []any
		for _, l := range labels {
			rows = append(rows, map[string]any{"label": l})
		}
		env.Set("rows", rows)
	}
	set("Walls", "Props", "Hostiles")
	got := ""
	env.Func("pick", func(s string) { got = s })
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatal(err)
	}
	u := ui.New(nil, nil, func() ui.Element { return d.View()() })
	frame := func() { u.Layout(300, 300); u.Layout(300, 300) }
	frame()
	click := func(y float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 50, Y: y}})
		u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 50, Y: y, Button: 1}, {Kind: app.MouseUp, X: 50, Y: y, Button: 1}})
		frame()
	}
	click(30)
	if got != "Props" {
		t.Fatalf("row 1 before: %q, want Props", got)
	}
	set("Walls", "Wall 1", "Wall 2", "Props", "Hostiles")
	frame()
	click(30)
	if got != "Wall 1" {
		t.Fatalf("row 1 after the model changed: %q, want Wall 1", got)
	}
}

// A control's own event value wins over a delegate row's role of the same
// name: a NumberField in a row whose model has a "value" field hands its
// handler the NEW number, and a TextField its new text (an inspector row:
// {key, value} with a field bound to row.value).
func TestRowRoleDoesNotShadowEventValue(t *testing.T) {
	src := `
Item {
    width: 300; height: 300
    Column {
        Repeater { model: rows
            Item { width: 300; height: 30
                NumberField { x: 0; y: 0; width: 200; value: modelData.value; from: 0; to: 10; stepSize: 0.5; decimals: 1
                    onValueModified: edit(modelData.key, value) } } } }
}`
	env := NewEnv()
	env.Set("rows", []any{map[string]any{"key": "height", "value": 2.0}})
	var gotKey string
	var got float64
	env.Func("edit", func(k string, v float64) { gotKey, got = k, v })
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatal(err)
	}
	u := ui.New(nil, nil, func() ui.Element { return d.View()() })
	frame := func() { u.Layout(300, 300); u.Layout(300, 300) }
	frame()
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 191, Y: 13, Button: 1}, {Kind: app.MouseUp, X: 191, Y: 13, Button: 1}})
	frame()
	if gotKey != "height" || got != 2.5 {
		t.Fatalf("edit(%q, %v), want edit(\"height\", 2.5)", gotKey, got)
	}
}
