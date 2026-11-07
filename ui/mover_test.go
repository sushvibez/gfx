package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestMover(t *testing.T) {
	var x, y core.Property[float32]
	x.Set(20)
	y.Set(30)
	clicks, actives := 0, []bool{}
	var moved [2]float32
	m := NewMover(func() (float32, float32) { return x.Get(), y.Get() },
		func(nx, ny float32) { x.Set(nx); y.Set(ny) })
	m.MaxX = 200
	m.OnActive = func(on bool) { actives = append(actives, on) }
	m.OnMove = func(dx, dy float32) { moved[0] += dx; moved[1] += dy }
	u := New(nil, nil, func() Element {
		// The target sits at (x, y): the padding of a top-left column.
		return Div().Col().Size(400, 300).Padding(y.Get(), 0, 0, x.Get()).Children(
			Div().Size(60, 40).DragMove(m).OnClick(func() { clicks++ }))
	})
	frame := func() { u.Layout(400, 300) }
	frame()
	drag := func(x0, y0 float32, steps ...[2]float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x0, Y: y0}, {Kind: app.MouseDown, X: x0, Y: y0, Button: 1}})
		frame()
		px, py := x0, y0
		for _, s := range steps {
			px, py = s[0], s[1]
			u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: px, Y: py}})
			frame()
		}
		u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: px, Y: py, Button: 1}})
		frame()
	}

	drag(40, 50, [2]float32{60, 60}, [2]float32{90, 75})
	if x.Get() != 70 || y.Get() != 55 {
		t.Errorf("target at %v,%v, want 70,55", x.Get(), y.Get())
	}
	if len(actives) != 2 || !actives[0] || actives[1] {
		t.Errorf("OnActive calls = %v, want [true false]", actives)
	}
	if moved != [2]float32{50, 25} {
		t.Errorf("OnMove deltas sum to %v, want 50,25", moved)
	}
	if px, py := m.PersistentTranslation(); px != 50 || py != 25 {
		t.Errorf("persistent translation %v,%v", px, py)
	}
	if clicks != 0 {
		t.Errorf("a drag clicked")
	}
	// Under the threshold: a click, no movement.
	drag(100, 70, [2]float32{103, 72})
	if x.Get() != 70 || clicks != 1 {
		t.Errorf("x = %v, clicks = %d; want 70, 1", x.Get(), clicks)
	}
	// Bounds.
	drag(100, 70, [2]float32{400, 70})
	if x.Get() != 200 {
		t.Errorf("x = %v, want the maximum 200", x.Get())
	}
	// Axis, and report-only.
	m.Axis = MoveY
	drag(230, 70, [2]float32{300, 100})
	if x.Get() != 200 || y.Get() != 85 {
		t.Errorf("MoveY: target at %v,%v, want 200,85", x.Get(), y.Get())
	}
	m.SetPos = nil
	drag(230, 100, [2]float32{230, 140})
	if y.Get() != 85 {
		t.Errorf("report-only mover moved the target to y %v", y.Get())
	}
	if tx, ty := m.Translation(); tx != 0 || ty != 40 {
		t.Errorf("translation = %v,%v, want 0,40", tx, ty)
	}
}
