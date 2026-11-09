package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// popup() opens a Popup at its trigger and it stays there on the frames
// that follow (the popup state is rebuilt every frame), here from inside a
// Repeater delegate, whose modelData the popup's rows still see.
func TestPopupAtTriggerInRepeater(t *testing.T) {
	src := `Item {
    width: 400; height: 300
    property string out: ""
    Row {
        Repeater {
            model: ["a", "b"]
            Rectangle {
                width: 50; height: 30
                MouseArea { anchors.fill: parent; onClicked: menu.popup() }
                Popup {
                    id: menu
                    padding: 0
                    Rectangle { width: 80; height: 20
                        MouseArea { anchors.fill: parent; onClicked: out += modelData } }
                }
            }
        }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatal(err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(400, 300)
	u.Click(60, 15) // the second delegate: its popup opens under it, at (50, 30)
	for range 3 {
		u.Layout(400, 300)
	}
	u.Click(80, 55) // inside the row (the panel pads it by 16)
	if got := str(env.vars["out"].(Ref).get()); got != "b" {
		t.Fatalf("out = %q, want b (the popup's row under the second delegate)", got)
	}
}

// A right click in a Repeater delegate opens a context Menu at the
// pointer: mouse.x/y are where the area was clicked, and popup(x, y)
// places the menu there in the parent's coordinates.
func TestContextMenuAtPointer(t *testing.T) {
	src := `Item {
    width: 400; height: 300
    property string out: ""
    Column {
        Repeater {
            model: ["a", "b"]
            Rectangle {
                width: 200; height: 100
                MouseArea {
                    anchors.fill: parent
                    acceptedButtons: Qt.RightButton
                    onClicked: (mouse) => menu.popup(mouse.x, mouse.y)
                }
                Menu {
                    id: menu
                    MenuItem { text: "Pick"; onTriggered: out += modelData }
                }
            }
        }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatal(err)
	}
	u := ui.New(nil, nil, d.View())
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 60, Y: 140}})
	u.Layout(400, 300)
	if !u.SecondaryClick(60, 140) { // the second delegate, 40 px down it
		t.Fatal("the right click reached nothing")
	}
	u.Layout(400, 300)
	// The menu's top-left is at the pointer.
	el := u.Hit(62, 142)
	if r := ui.Bounds(el); r.X != 60 || r.Y != 140 {
		t.Fatalf("menu at %v, want its corner at (60, 140)", r)
	}
}
