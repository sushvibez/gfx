package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

func TestTextFieldMaskAndSelection(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    id: root
    width: 400; height: 300
    property string picked: ""
    TextField { id: phone; x: 0; y: 0; width: 200; inputMask: "999-9999"; focus: true }
    TextField { id: words; x: 0; y: 60; width: 200; text: "hello world" }
    MouseArea { x: 300; y: 200; width: 100; height: 100
        onClicked: { words.select(6, 11); root.picked = words.selectedText } }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	frame := func() { u.Layout(400, 300); u.Layout(400, 300) }
	frame()
	get := func(expr string) string {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return str(v)
	}
	for _, r := range "555a1234" {
		u.Key(app.KeyEvent{Down: true, Rune: r})
		frame()
	}
	if got := get("phone.text") + "|" + get("phone.acceptableInput"); got != "555-1234|true" {
		t.Errorf("masked field: %s, want 555-1234|true (the a refused, the dash put in)", got)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 350, Y: 250}, {Kind: app.MouseDown, X: 350, Y: 250, Button: 1},
		{Kind: app.MouseUp, X: 350, Y: 250, Button: 1}})
	frame()
	if got := get("root.picked"); got != "world" {
		t.Errorf("select(6, 11) then selectedText = %q, want world", got)
	}
	if got := get("words.selectionStart") + "," + get("words.selectionEnd") + "," + get("words.cursorPosition"); got != "6,11,11" {
		t.Errorf("selectionStart,End,cursor = %s, want 6,11,11", got)
	}
	if _, err := evalString("words.selectAll()", env); err != nil {
		t.Fatal(err)
	}
	if got := get("words.selectedText"); got != "hello world" {
		t.Errorf("after selectAll(): %q", got)
	}
}
