package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

func TestMouseAreaDragTarget(t *testing.T) {
	src := `
import QtQuick 2.15
Item {
    id: root
    width: 400; height: 300
    property int clicks: 0
    Rectangle {
        id: card
        x: 20; y: 30; width: 60; height: 40
        MouseArea {
            id: ma
            anchors.fill: parent
            drag.target: parent
            drag.minimumX: 0; drag.maximumX: 200
            onClicked: root.clicks += 1
        }
    }
    Rectangle {
        id: slider
        x: 10; y: 200; width: 20; height: 20
        MouseArea { anchors.fill: parent; drag.target: slider; drag.axis: Drag.XAxis }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	frame := func() { u.Layout(400, 300) }
	frame()
	frame() // parent.x reads the laid-out geometry, there from the second frame
	get := func(expr string) string {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return str(v)
	}
	drag := func(x0, y0 float32, steps ...[2]float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x0, Y: y0}, {Kind: app.MouseDown, X: x0, Y: y0, Button: 1}})
		frame()
		x, y := x0, y0
		for _, s := range steps {
			x, y = s[0], s[1]
			u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}})
			frame()
		}
		u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: x, Y: y, Button: 1}})
		frame()
	}

	// drag.active is true while the target follows the pointer.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 40, Y: 50}, {Kind: app.MouseDown, X: 40, Y: 50, Button: 1}})
	frame()
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 60, Y: 60}})
	frame()
	if got := get("ma.drag.active"); got != "true" {
		t.Errorf("drag.active = %s mid-drag, want true", got)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 90, Y: 75}})
	frame()
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: 90, Y: 75, Button: 1}})
	frame()
	if got := get("card.x") + "," + get("card.y"); got != "70,55" {
		t.Errorf("card at %s after the drag, want 70,55", got)
	}
	// ... and back, through the drag helper.
	drag(100, 70, [2]float32{50, 45})
	if got := get("card.x") + "," + get("card.y"); got != "20,30" {
		t.Errorf("card at %s after dragging back, want 20,30", got)
	}
	// Drag the card 50 right, 25 down.
	drag(40, 50, [2]float32{60, 60}, [2]float32{90, 75})
	if got := get("card.x") + "," + get("card.y"); got != "70,55" {
		t.Errorf("card at %s after the drag, want 70,55", got)
	}
	if got := get("root.clicks"); got != "0" {
		t.Errorf("clicks = %s: a drag is not a click", got)
	}
	if got := get("ma.drag.active"); got != "false" {
		t.Errorf("drag.active = %s after release, want false", got)
	}

	// A press that does not pass the threshold is a click; the card stays.
	drag(100, 70, [2]float32{103, 72})
	if got := get("card.x") + "," + get("card.y"); got != "70,55" {
		t.Errorf("card moved to %s under the threshold", got)
	}
	if got := get("root.clicks"); got != "1" {
		t.Errorf("clicks = %s, want 1", got)
	}

	// drag.maximumX holds it.
	drag(100, 70, [2]float32{400, 70})
	if got := get("card.x"); got != "200" {
		t.Errorf("card.x = %s, want the maximum 200", got)
	}

	// drag.axis: Drag.XAxis leaves y alone.
	drag(20, 210, [2]float32{80, 260})
	if got := get("slider.x") + "," + get("slider.y"); got != "70,200" {
		t.Errorf("slider at %s, want 70,200 (x only)", got)
	}
}
