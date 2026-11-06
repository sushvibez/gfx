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
