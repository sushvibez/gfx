package gml

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// A plain Dial turns to the pointer: a press sets the value at its angle,
// onMoved runs with it, a value bound to an int property is written back,
// and a disabled Dial does not turn.
func TestDialTurns(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    id: root
    width: 300; height: 120
    property int level: 10
    property real lastMoved: -1
    Dial { id: free; x: 0; y: 0; width: 100; height: 100; from: 0; to: 100
        onMoved: root.lastMoved = value }
    Dial { id: bound; x: 100; y: 0; width: 100; height: 100; from: 0; to: 100; value: root.level; stepSize: 10; snapMode: Dial.SnapAlways }
    Dial { id: off; x: 200; y: 0; width: 100; height: 100; from: 0; to: 100; enabled: false }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	frame := func() { u.Layout(300, 120) }
	frame()
	frame()
	num := func(expr string) float64 {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		f, _ := toFloat64(v)
		return f
	}
	click := func(x, y float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}, {Kind: app.MouseDown, X: x, Y: y, Button: 1}})
		frame()
		u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: x, Y: y, Button: 1}})
		frame()
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.5 }

	click(50, 5) // 12 o'clock: the middle of the sweep
	if v := num("free.value"); !near(v, 50) {
		t.Errorf("free.value = %v at 12 o'clock, want 50", v)
	}
	if v := num("root.lastMoved"); !near(v, 50) {
		t.Errorf("onMoved saw %v, want 50", v)
	}
	click(95, 50) // 3 o'clock: 230 of 280 degrees
	if v := num("free.value"); !near(v, 100*230.0/280) {
		t.Errorf("free.value = %v at 3 o'clock, want %v", v, 100*230.0/280)
	}
	click(195, 50) // the bound dial, snapped to 10s, written back to the int
	if v := num("root.level"); v != 80 {
		t.Errorf("root.level = %v, want 80 (82.1 snapped)", v)
	}
	click(250, 5)
	if v := num("off.value"); v != 0 {
		t.Errorf("a disabled Dial turned to %v", v)
	}
}
