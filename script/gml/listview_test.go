package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// A horizontal ListView builds only the delegates in view, and the wheel
// scrolls it sideways.
func TestHorizontalListViewVirtualizes(t *testing.T) {
	src := `Item {
    width: 400; height: 100
    property int builds: 0
    ListView {
        id: lv
        width: 300; height: 60
        orientation: ListView.Horizontal
        model: 1000
        delegate: Rectangle { width: 50; height: 50; color: "#336699"; Component.onCompleted: builds++ }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(400, 100)
	u.Layout(400, 100)
	count := func() int {
		n := 0
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if b := ui.Bounds(e); b.W == 50 && b.H == 50 {
				n++
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(tree)
		return n
	}
	if n := count(); n == 0 || n > 10 {
		t.Errorf("%d delegates laid out for a 300 px view of 50 px ones (of 1000)", n)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 100, Y: 30}, {Kind: app.MouseScroll, X: 100, Y: 30, ScrollY: 5 * app.WheelNotchPx}})
	u.Layout(400, 100)
	var minX float32 = 1e9
	var walk func(ui.Element)
	walk = func(e ui.Element) {
		if e == nil {
			return
		}
		if b := ui.Bounds(e); b.W == 50 && b.H == 50 && b.X < minX && b.X+b.W > 0 {
			minX = b.X
		}
		for _, ch := range ui.Children(e) {
			walk(ch)
		}
	}
	walk(tree)
	if minX > 0 {
		t.Errorf("after the wheel the first visible delegate starts at x %v; the list did not scroll", minX)
	}
}

// A vertical ListView's header scrolls away with the rows.
func TestListViewHeaderScrolls(t *testing.T) {
	src := `Item {
    width: 300; height: 200
    ListView {
        id: lv
        width: 300; height: 150
        model: 30
        header: Rectangle { width: 300; height: 40; color: "#ff0000" }
        delegate: Rectangle { width: 300; height: 30; color: "#00ff00" }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(300, 200)
	u.Layout(300, 200)
	headerY := func() (float32, bool) {
		var y float32
		found := false
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if c, ok := ui.FillColor(e); ok && c.R > 0.9 && c.G < 0.1 {
				y, found = ui.Bounds(e).Y, true
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(tree)
		return y, found
	}
	if y, ok := headerY(); !ok || y != 0 {
		t.Fatalf("header at %v (found %v), want 0", y, ok)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 100, Y: 100}, {Kind: app.MouseScroll, X: 100, Y: 100, ScrollY: 25}})
	u.Layout(300, 200)
	if y, _ := headerY(); y != -25 {
		t.Errorf("header at %v after scrolling 25, want -25 (it scrolls with the rows)", y)
	}
}
