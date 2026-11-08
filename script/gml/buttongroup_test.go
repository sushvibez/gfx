package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

func TestButtonGroupsAndTristate(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    id: root
    width: 400; height: 300
    property string last: ""
    Column { x: 0; y: 0
        RadioButton { id: r1; checked: true }
        RadioButton { id: r2 }
        RadioButton { id: r3 }
    }
    ButtonGroup { id: picks; buttons: [c1, c2]; onClicked: (button) => { root.last = button === c2 ? "c2" : "c1" } }
    Column { x: 200; y: 0
        CheckBox { id: c1; checked: true }
        CheckBox { id: c2 }
        CheckBox { id: tri; tristate: true }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	frame := func() { u.Layout(400, 300); u.Layout(400, 300) }
	frame()
	get := func(expr string) string {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return str(v)
	}
	click := func(x, y float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}, {Kind: app.MouseDown, X: x, Y: y, Button: 1},
			{Kind: app.MouseUp, X: x, Y: y, Button: 1}})
		frame()
	}
	// Each control is 20 px tall in its column.
	click(8, 28) // r2
	if got := get("r1.checked") + get("r2.checked") + get("r3.checked"); got != "falsetruefalse" {
		t.Errorf("radios after clicking r2: %s, want only r2 (autoExclusive)", got)
	}
	click(8, 28) // r2 again: stays checked
	if got := get("r2.checked"); got != "true" {
		t.Errorf("a checked radio unchecked itself: %s", got)
	}
	click(208, 28) // c2: the ButtonGroup makes the boxes exclusive
	if got := get("c1.checked") + get("c2.checked"); got != "falsetrue" {
		t.Errorf("boxes after clicking c2: %s, want only c2", got)
	}
	if got := get("picks.checkedButton === c2") + get("root.last"); got != "truec2" {
		t.Errorf("checkedButton/onClicked: %s, want true and c2", got)
	}
	click(208, 28) // c2 again: an exclusive group's checked member stays checked
	if got := get("c2.checked"); got != "true" {
		t.Errorf("c2 unchecked itself in an exclusive group: %s", got)
	}
	for i, want := range []string{"1false", "2true", "0false"} {
		click(208, 48) // tri: partly, checked, unchecked
		if got := get("tri.checkState") + get("tri.checked"); got != want {
			t.Errorf("tristate click %d: checkState,checked = %s, want %s", i+1, got, want)
		}
	}
	if got := get("c2.checkState"); got != "2" {
		t.Errorf("a two-state box's checkState = %s, want 2 (Qt.Checked)", got)
	}
}
