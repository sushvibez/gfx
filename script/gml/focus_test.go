package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

func TestKeyNavigationAndFocus(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    id: root
    width: 400; height: 300
    property string seen: ""
    Rectangle { id: a; x: 0; y: 0; width: 50; height: 50; focus: true
        color: activeFocus ? "red" : "gray"
        KeyNavigation.right: c; KeyNavigation.down: field
        Keys.onSpacePressed: root.seen += "a" }
    Rectangle { id: b; x: 60; y: 0; width: 50; height: 50; activeFocusOnTab: true }
    Rectangle { id: c; x: 120; y: 0; width: 50; height: 50; activeFocusOnTab: true
        KeyNavigation.left: a
        Keys.onSpacePressed: root.seen += "c" }
    TextField { id: field; x: 0; y: 100; width: 150; KeyNavigation.up: b }
    MouseArea { x: 300; y: 200; width: 100; height: 100; onClicked: c.forceActiveFocus() }
    MouseArea { x: 200; y: 200; width: 90; height: 100; onClicked: a.focus = true }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	frame := func() { u.Layout(400, 300) }
	// red reports whether a red box is drawn (a, lit by its activeFocus).
	red := func() bool {
		found := false
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if c, ok := ui.FillColor(e); ok && c.R > 0.9 && c.G < 0.1 && c.B < 0.1 {
				found = true
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(tree)
		return found
	}
	get := func(expr string) string {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return str(v)
	}
	key := func(ev app.KeyEvent) {
		ev.Down = true
		u.Key(ev)
		frame()
		frame()
	}
	click := func(x, y float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}, {Kind: app.MouseDown, X: x, Y: y, Button: 1},
			{Kind: app.MouseUp, X: x, Y: y, Button: 1}})
		frame()
		frame()
	}
	frame()
	get("a.activeFocus") // start tracking (from the frame after the first read)
	get("b.activeFocus")
	get("c.activeFocus")
	frame()
	frame()
	if got := get("a.activeFocus"); got != "true" {
		t.Fatalf("a.activeFocus = %s with focus: true", got)
	}
	if !red() {
		t.Errorf("a is not drawn red: its color binding did not see activeFocus")
	}
	key(app.KeyEvent{Key: app.KeyRight}) // a → c
	key(app.KeyEvent{Rune: ' '})
	if got := get("root.seen"); got != "c" {
		t.Errorf("seen = %q after Right then Space, want c", got)
	}
	if got := get("a.activeFocus") + get("c.activeFocus"); got != "falsetrue" {
		t.Errorf("activeFocus a,c = %s, want false,true", got)
	}
	if red() {
		t.Errorf("a is still red after focus left it")
	}
	key(app.KeyEvent{Key: app.KeyLeft}) // c → a
	key(app.KeyEvent{Key: app.KeyDown}) // a → field
	key(app.KeyEvent{Rune: 'h'})
	if got := get("field.text"); got != "h" {
		t.Errorf("field.text = %q, want the h typed after Down", got)
	}
	key(app.KeyEvent{Key: app.KeyUp}) // field → b
	if got := get("b.activeFocus"); got != "true" {
		t.Errorf("b.activeFocus = %s after Up from the field", got)
	}
	click(350, 250) // c.forceActiveFocus()
	if got := get("c.activeFocus"); got != "true" {
		t.Errorf("c.activeFocus = %s after forceActiveFocus()", got)
	}
	click(250, 250) // a.focus = true
	key(app.KeyEvent{Rune: ' '})
	if got := get("root.seen"); got != "ca" {
		t.Errorf("seen = %q, want ca (a focused by focus = true)", got)
	}
}
