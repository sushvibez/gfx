package gml

import (
	"testing"

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
