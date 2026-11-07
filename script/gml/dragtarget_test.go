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

func TestDragHandler(t *testing.T) {
	src := `
import QtQuick 2.15
Item {
    id: root
    width: 400; height: 300
    property real panX: 0
    property real panY: 0
    property int activations: 0
    Rectangle {
        id: knob
        x: 20; y: 30; width: 60; height: 40
        DragHandler { id: dh; xAxis.maximum: 100; yAxis.enabled: false
            onActiveChanged: if (active) root.activations += 1 }
    }
    Rectangle {
        id: map
        x: 0; y: 150; width: 400; height: 150
        DragHandler { id: pan; target: null
            onTranslationChanged: (delta) => { root.panX += delta.x; root.panY += delta.y } }
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
	frame()
	get := func(expr string) string {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return str(v)
	}
	press := func(x, y float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}, {Kind: app.MouseDown, X: x, Y: y, Button: 1}})
		frame()
	}
	move := func(x, y float32) { u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}}); frame() }
	release := func(x, y float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: x, Y: y, Button: 1}})
		frame()
	}

	// The default target is the item the handler sits in; y is off, x
	// stops at 100.
	press(40, 50)
	move(70, 80)
	if got := get("dh.active"); got != "true" {
		t.Errorf("dh.active = %s mid-drag", got)
	}
	if got := get("dh.activeTranslation.x"); got != "30" {
		t.Errorf("activeTranslation.x = %s, want 30", got)
	}
	move(200, 90)
	release(200, 90)
	if got := get("knob.x") + "," + get("knob.y"); got != "100,30" {
		t.Errorf("knob at %s, want 100,30 (x capped, y off)", got)
	}
	if got := get("dh.active") + "," + get("root.activations"); got != "false,1" {
		t.Errorf("active,activations = %s, want false,1", got)
	}

	// target: null moves nothing and reports the deltas.
	press(100, 200)
	move(130, 210)
	move(150, 230)
	release(150, 230)
	press(150, 230)
	move(170, 230)
	release(170, 230)
	if got := get("map.x") + "," + get("map.y"); got != "0,150" {
		t.Errorf("map moved to %s with target: null", got)
	}
	if got := get("root.panX") + "," + get("root.panY"); got != "70,30" {
		t.Errorf("pan = %s, want 70,30 from the deltas", got)
	}
	if got := get("pan.persistentTranslation.x") + "," + get("pan.activeTranslation.x"); got != "70,20" {
		t.Errorf("persistent,active x = %s, want 70,20", got)
	}
}
