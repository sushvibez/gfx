package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestButtonGroupAndRadio(t *testing.T) {
	var a, b, c core.Property[bool]
	a.Set(true)
	g := NewButtonGroup()
	var rings []*DivEl
	toggles := 0
	u := New(nil, nil, func() Element {
		rings = rings[:0]
		col := Div().Col()
		for _, p := range []*core.Property[bool]{&a, &b, &c} {
			r := RadioRing(p, g, func(bool) { toggles++ })
			rings = append(rings, r)
			col.Children(r)
		}
		return col
	})
	click := func(i int) {
		u.Layout(100, 100)
		r := rings[i].Rect()
		u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: r.X + 5, Y: r.Y + 5, Button: 1}, {Kind: app.MouseUp, X: r.X + 5, Y: r.Y + 5, Button: 1}})
		u.Layout(100, 100)
	}
	click(1)
	if a.Get() || !b.Get() || c.Get() {
		t.Errorf("after clicking b: a %v b %v c %v, want only b", a.Get(), b.Get(), c.Get())
	}
	click(1) // a checked radio stays checked
	if !b.Get() || toggles != 1 {
		t.Errorf("clicking checked b: b %v, toggles %d; want true, 1", b.Get(), toggles)
	}
	if g.Checked() != &b {
		t.Error("Checked() is not b")
	}
	g.Exclusive = false
	click(2)
	if !b.Get() || !c.Get() {
		t.Error("a non-exclusive group unchecked b")
	}
}

func TestTriCheckboxCycles(t *testing.T) {
	var s core.Property[int]
	var seen []int
	var box Element
	u := New(nil, nil, func() Element { box = TriCheckbox(&s, "x", func(v int) { seen = append(seen, v) }); return box })
	for i := 0; i < 3; i++ {
		u.Layout(100, 40)
		u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 5, Y: 5, Button: 1}, {Kind: app.MouseUp, X: 5, Y: 5, Button: 1}})
	}
	if len(seen) != 3 || seen[0] != PartiallyChecked || seen[1] != Checked || seen[2] != Unchecked {
		t.Errorf("states %v, want partly, checked, unchecked", seen)
	}
}
