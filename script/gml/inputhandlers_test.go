package gml

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

func loadHandlers(t *testing.T, src string) (*ui.UI, *Env, func(), func(string) float64) {
	t.Helper()
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	frame := func() { u.Layout(400, 300) }
	frame()
	frame()
	num := func(expr string) float64 {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		f, err := toFloat64(v)
		if err != nil {
			b, _ := toBool(v)
			if b {
				return 1
			}
			return 0
		}
		return f
	}
	return u, env, frame, num
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-3 }

func TestWheelHandler(t *testing.T) {
	u, _, frame, num := loadHandlers(t, `
import QtQuick 2.15
Item {
    id: root
    width: 400; height: 300
    property real notches: 0
    Rectangle { id: zoomer; x: 0; y: 0; width: 200; height: 200
        WheelHandler { id: wh; property: "scale" } }
    Rectangle { id: mover; x: 200; y: 0; width: 200; height: 200
        WheelHandler { target: parent; property: "x"; rotationScale: 2
            onWheel: (event) => { root.notches += event.angleDelta.y / 120 } } }
}`)
	wheel := func(x, y, dy float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}, {Kind: app.MouseScroll, X: x, Y: y, ScrollY: dy}})
		frame()
		frame() // geometry (x) reads the laid-out tree, a frame behind the write
	}
	wheel(100, 100, -8*app.WheelNotchPx) // eight notches away: the scale doubles
	if v := num("zoomer.scale"); !near(v, 2) {
		t.Errorf("zoomer.scale = %v after 8 notches, want 2", v)
	}
	if v := num("wh.rotation"); !near(v, 120) {
		t.Errorf("wh.rotation = %v, want 120 degrees", v)
	}
	wheel(300, 100, -app.WheelNotchPx) // one notch: 15° × 2 px
	if v := num("mover.x"); !near(v, 230) {
		t.Errorf("mover.x = %v, want 230", v)
	}
	if v := num("root.notches"); !near(v, 1) {
		t.Errorf("onWheel saw %v notches, want 1", v)
	}
}

func TestPinchHandler(t *testing.T) {
	u, _, frame, num := loadHandlers(t, `
import QtQuick 2.15
Item {
    id: root
    width: 400; height: 300
    property int starts: 0
    property real lastDelta: 0
    Rectangle { id: photo; x: 0; y: 0; width: 200; height: 200
        PinchHandler { id: ph; maximumScale: 3
            onActiveChanged: if (active) root.starts += 1
            onScaleChanged: (delta) => { root.lastDelta = delta } } }
}`)
	// A touchpad pinch: Ctrl+wheel, two notches out.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 100, Y: 100}, {Kind: app.MouseScroll, X: 100, Y: 100, Ctrl: true, ScrollY: -2 * app.WheelNotchPx}})
	frame()
	want := ui.PinchStepPerNotch * ui.PinchStepPerNotch
	if v := num("photo.scale"); !near(v, want) {
		t.Errorf("photo.scale = %v, want %v", v, want)
	}
	if v := num("root.lastDelta"); !near(v, want) {
		t.Errorf("onScaleChanged delta = %v, want %v", v, want)
	}
	// A touchscreen pinch: spread from 40 to 160 apart (×4), capped at 3.
	u.Touch([]app.TouchEvent{{ID: 1, Phase: app.TouchBegin, X: 80, Y: 100}, {ID: 2, Phase: app.TouchBegin, X: 120, Y: 100}})
	frame()
	if v := num("ph.active"); v != 1 {
		t.Errorf("ph.active = %v during the pinch", v)
	}
	u.Touch([]app.TouchEvent{{ID: 1, Phase: app.TouchMove, X: 20, Y: 100}, {ID: 2, Phase: app.TouchMove, X: 180, Y: 100}})
	frame()
	u.Touch([]app.TouchEvent{{ID: 1, Phase: app.TouchEnd, X: 20, Y: 100}, {ID: 2, Phase: app.TouchEnd, X: 180, Y: 100}})
	frame()
	if v := num("photo.scale"); !near(v, 3) {
		t.Errorf("photo.scale = %v, want the maximum 3", v)
	}
	if v := num("ph.persistentScale"); !near(v, 3) {
		t.Errorf("persistentScale = %v, want 3", v)
	}
	if v := num("ph.active") + 10*num("root.starts"); v != 10 {
		t.Errorf("active/starts = %v, want inactive after one start", v)
	}
}

func TestPointHandler(t *testing.T) {
	u, _, frame, num := loadHandlers(t, `
import QtQuick 2.15
Item {
    id: root
    width: 400; height: 300
    property real seenX: -1
    Rectangle { x: 50; y: 40; width: 200; height: 200
        PointHandler { id: pt; onPointChanged: root.seenX = pt.point.position.x } }
}`)
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 100, Y: 90}, {Kind: app.MouseDown, X: 100, Y: 90, Button: 1}})
	frame()
	if v := num("pt.active"); v != 1 {
		t.Errorf("pt.active = %v while pressed", v)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 130, Y: 120}})
	frame()
	if x, y := num("pt.point.position.x"), num("pt.point.position.y"); x != 80 || y != 80 {
		t.Errorf("point.position = %v,%v, want 80,80 in the item", x, y)
	}
	if x := num("pt.point.pressPosition.x"); x != 50 {
		t.Errorf("pressPosition.x = %v, want 50", x)
	}
	if x := num("pt.point.scenePosition.x"); x != 130 {
		t.Errorf("scenePosition.x = %v, want 130", x)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: 130, Y: 120, Button: 1}})
	frame()
	if v := num("pt.active"); v != 0 {
		t.Errorf("pt.active = %v after release", v)
	}
	if v := num("root.seenX"); v < 50 {
		t.Errorf("onPointChanged saw x %v", v)
	}
}
