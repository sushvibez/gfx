package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

func TestComboBoxDropdownAndEditable(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    id: root
    width: 400; height: 300
    property int picked: -1
    ListModel { id: fruits; ListElement { text: "apple" } ListElement { text: "pear" } }
    ComboBox { id: cb; x: 0; y: 0; model: ["red", "green", "blue"]; onActivated: (index) => { root.picked = index } }
    ComboBox { id: ed; x: 200; y: 0; editable: true; model: fruits; textRole: "text"
        onAccepted: { if (find(editText) === -1) fruits.append({ text: editText }) } }
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
	click := func(x, y float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}, {Kind: app.MouseDown, X: x, Y: y, Button: 1},
			{Kind: app.MouseUp, X: x, Y: y, Button: 1}})
		frame()
	}
	click(40, 16)         // open
	click(40, 36+2+56+14) // the third row: blue
	if got := get("cb.currentIndex") + "," + get("cb.currentText") + "," + get("root.picked"); got != "2,blue,2" {
		t.Errorf("picked blue: currentIndex,currentText,picked = %s, want 2,blue,2", got)
	}
	if got := get(`cb.find("green")`); got != "1" {
		t.Errorf(`find("green") = %s, want 1`, got)
	}
	in := d.doc.inputFor(d.doc.key(findNodeID(d.root, "ed"))+".edit", env)
	in.SetText("plum")
	in.Key(app.KeyEvent{Down: true, Key: app.KeyEnter})
	frame()
	if got := get("ed.count") + "," + get("ed.editText"); got != "3,plum" {
		t.Errorf("accepted plum: count,editText = %s, want 3,plum (onAccepted appended it)", got)
	}
}

// findNodeID is the node with the given id under n.
func findNodeID(n *node, id string) *node {
	if nodeID(n) == id {
		return n
	}
	for _, c := range n.children {
		if f := findNodeID(c, id); f != nil {
			return f
		}
	}
	return nil
}
