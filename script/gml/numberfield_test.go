package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// A NumberField's arrow and drag reach onValueModified with the new
// `value`; the binding is one-way (the host writes it back), and a drag's
// release fires onDragFinished once.
func TestNumberFieldMarkup(t *testing.T) {
	src := `
Item {
    width: 400; height: 100
    NumberField { x: 0; y: 0; width: 200; value: h; from: 0; to: 6; stepSize: 0.5; decimals: 1
        label: "Height"; unit: "m"
        onValueModified: setH(value)
        onDragFinished: done() }
}`
	env := NewEnv()
	h, dones := 2.0, 0
	env.Set("h", h)
	env.Func("setH", func(v float64) { h = v; env.Set("h", v) })
	env.Func("done", func() { dones++ })
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, func() ui.Element { return d.View()() })
	frame := func() { u.Layout(400, 100); u.Layout(400, 100) }
	frame()
	// The › arrow is the last 18 px of the 200 px box.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 191, Y: 13, Button: 1}, {Kind: app.MouseUp, X: 191, Y: 13, Button: 1}})
	frame()
	if h != 2.5 {
		t.Fatalf("› : h = %v, want 2.5", h)
	}
	// Drag 30 px right from the middle: 5 steps of 0.5.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 100, Y: 13, Button: 1}})
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 130, Y: 13}})
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: 130, Y: 13, Button: 1}})
	frame()
	if h != 5 || dones != 1 {
		t.Fatalf("drag: h = %v, drag ends %d; want 5, 1", h, dones)
	}
}

// A constant value is only the initial number: the user's change stands,
// and id.value reads it (a dialog's OK button).
func TestNumberFieldConstantValue(t *testing.T) {
	src := `
Item {
    width: 400; height: 100
    NumberField { id: w; x: 0; y: 0; width: 200; value: 20; from: 4; to: 200 }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatal(err)
	}
	u := ui.New(nil, nil, func() ui.Element { return d.View()() })
	frame := func() { u.Layout(400, 100); u.Layout(400, 100) }
	frame()
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 191, Y: 13, Button: 1}, {Kind: app.MouseUp, X: 191, Y: 13, Button: 1}})
	frame()
	frame()
	v, err := evalString("w.value", env)
	if err != nil {
		t.Fatal(err)
	}
	if f, _ := toFloat64(v); f != 21 {
		t.Fatalf("w.value = %v after ›, want 21", v)
	}
}
