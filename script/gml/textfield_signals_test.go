package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// TextField runs onTextChanged as the text changes and onEditingFinished
// when it loses focus (or on Enter), with `text` bound.
func TestTextFieldChangedAndFinished(t *testing.T) {
	src := `Item {
    width: 300; height: 100
    property string changed: ""
    property string finished: ""
    TextField { id: f; x: 0; y: 0; width: 200; text: "a"
        onTextChanged: changed = text
        onEditingFinished: finished = text }
    Rectangle { x: 0; y: 60; width: 200; height: 30
        MouseArea { anchors.fill: parent; onClicked: {} } }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatal(err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 100)
	press := func(x, y float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, Button: 1, X: x, Y: y}, {Kind: app.MouseUp, Button: 1, X: x, Y: y}})
	}
	press(20, 10) // focus the field
	u.Layout(300, 100)
	u.Key(app.KeyEvent{Down: true, Rune: 'b'})
	u.Layout(300, 100)
	if got := str(env.vars["changed"].(Ref).get()); got != "ab" && got != "ba" { // where the click put the caret
		t.Fatalf("onTextChanged saw %q, want ab", got)
	}
	if got := str(env.vars["finished"].(Ref).get()); got != "" {
		t.Fatalf("finished early: %q", got)
	}
	press(20, 75) // focus leaves the field
	u.Layout(300, 100)
	u.Layout(300, 100)
	if got := str(env.vars["finished"].(Ref).get()); got != str(env.vars["changed"].(Ref).get()) {
		t.Fatalf("onEditingFinished saw %q, want ab", got)
	}
}
