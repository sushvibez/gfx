package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// MouseArea's double-click, wheel and position handlers run with their
// event objects, and mouseX/mouseY follow the pointer.
func TestMouseAreaGestures(t *testing.T) {
	src := `Item {
    width: 200; height: 200
    property string out: ""
    property real at: ma.mouseX
    MouseArea {
        id: ma
        anchors.fill: parent
        hoverEnabled: true
        onClicked: out += "c;"
        onDoubleClicked: (mouse) => { out += "d" + Math.round(mouse.x) + ";" }
        onWheel: { out += "w" + wheel.angleDelta.y + ";" }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 40, Y: 50}})
	u.Layout(200, 200)
	for range 2 {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, Button: 1, X: 40, Y: 50}})
		u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, Button: 1, X: 40, Y: 50}})
	}
	// One notch toward the user is Qt's angleDelta.y of -120.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 40, Y: 50, ScrollY: app.WheelNotchPx}})
	if got := str(env.vars["out"].(Ref).get()); got != "c;c;d40;w-120;" {
		t.Errorf("out = %q, want c;c;d40;w-120;", got)
	}
	u.Layout(200, 200) // the binding reading ma.mouseX rebuilds
	if got := str(env.vars["at"].(Ref).get()); got != "40" {
		t.Errorf("ma.mouseX = %s, want 40", got)
	}
}

// acceptedButtons routes the right button to onClicked, which reads
// mouse.button; a right-only area ignores the left button.
func TestMouseAreaAcceptedButtons(t *testing.T) {
	src := `Item {
    width: 200; height: 200
    property string out: ""
    MouseArea {
        x: 0; y: 0; width: 100; height: 200
        acceptedButtons: Qt.LeftButton | Qt.RightButton
        onClicked: (mouse) => { out += (mouse.button === Qt.RightButton ? "R" : "L") + ";" }
    }
    MouseArea {
        x: 100; y: 0; width: 100; height: 200
        acceptedButtons: Qt.RightButton
        onClicked: out += "r;"
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	click := func(button int, x float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, Button: button, X: x, Y: 50}})
		u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, Button: button, X: x, Y: 50}})
		u.Layout(200, 200)
	}
	click(1, 40)
	click(3, 40)
	click(1, 150)
	click(3, 150)
	// A right press on one area released on the other is no click.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, Button: 3, X: 40, Y: 50}})
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, Button: 3, X: 150, Y: 50}})
	if got := str(env.vars["out"].(Ref).get()); got != "L;R;r;" {
		t.Errorf("out = %q, want L;R;r;", got)
	}
}
