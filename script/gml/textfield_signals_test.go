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

// The catalogue translates shown text by convention (a Text's whole
// string); qsTr stays a no-op returning its text, as in plain QML.
func TestTranslations(t *testing.T) {
	src := `Item { property string a: qsTr("Items"); property string b: qsTr("History")
    Text { id: label; text: "Items" } }`
	SetTranslations(map[string]string{"Items": "Elemente"})
	defer SetTranslations(nil)
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatal(err)
	}
	d.View()
	if a, b := str(env.vars["a"].(Ref).get()), str(env.vars["b"].(Ref).get()); a != "Items" || b != "History" {
		t.Fatalf("qsTr gave %q, %q", a, b)
	}
	var texts []string
	var walk func(ui.Element)
	walk = func(e ui.Element) {
		if s, ok := ui.TextOf(e); ok {
			texts = append(texts, s)
		}
		for _, c := range ui.Children(e) {
			walk(c)
		}
	}
	walk(d.View()())
	if len(texts) != 1 || texts[0] != "Elemente" {
		t.Fatalf("shown text %v, want [Elemente]", texts)
	}
}
