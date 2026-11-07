package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// A SplitView's handle is there to be DRAGGED: the pane before it grows by
// what the pointer moved and the pane after it gives that back, and neither
// goes under its stated minimum. Laid out but not draggable, the corpus'
// splits reported the same widths however hard they were pulled.
func TestSplitViewHandleResizesPanes(t *testing.T) {
	src := `
import QtQuick
import QtQuick.Controls
Item {
    width: 400; height: 200
    SplitView {
        anchors.fill: parent
        Rectangle {
            id: sidebar
            color: "#223344"
            SplitView.preferredWidth: 120
            SplitView.minimumWidth: 80
            Text { text: "side " + sidebar.width; color: "#ffffff" }
        }
        Rectangle {
            id: main
            color: "#334455"
            SplitView.fillWidth: true
            SplitView.minimumWidth: 100
            Text { text: "main " + main.width; color: "#ffffff" }
        }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(400, 200)
	sideWidth := func() float32 {
		var w float32
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if c, ok := ui.FillColor(e); ok &&
				uint32(c.R*255+0.5)<<16|uint32(c.G*255+0.5)<<8|uint32(c.B*255+0.5) == 0x223344 {
				w = ui.Bounds(e).W
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(tree)
		return w
	}
	if got := sideWidth(); got != 120 {
		t.Fatalf("the sidebar started at %v, want its preferredWidth 120", got)
	}
	// The handle sits just past the sidebar; drag it 60px right.
	if !u.Drag(122, 100, 182, 100) {
		t.Fatal("the handle took no drag")
	}
	u.Layout(400, 200)
	if got := sideWidth(); got < 175 || got > 185 {
		t.Errorf("after dragging the handle 60px right the sidebar is %v, want ~180", got)
	}
	// Dragging back past its minimum stops at the minimum.
	u.Drag(182, 100, 20, 100)
	u.Layout(400, 200)
	if got := sideWidth(); got != 80 {
		t.Errorf("dragged past its minimumWidth the sidebar is %v, want 80", got)
	}
}

// A handle delegate reads SplitHandle.hovered and SplitHandle.pressed: hover
// over it, press and drag (pressed holds wherever the pointer goes), release.
func TestSplitHandleHoveredPressed(t *testing.T) {
	src := `
import QtQuick
import QtQuick.Controls
Item {
    width: 400; height: 200
    SplitView {
        anchors.fill: parent
        handle: Rectangle {
            implicitWidth: 6
            color: SplitHandle.pressed ? "#ff0000" : SplitHandle.hovered ? "#0000ff" : "#808080"
        }
        Rectangle { color: "#223344"; SplitView.preferredWidth: 120 }
        Rectangle { color: "#334455"; SplitView.fillWidth: true }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	frame := func() { u.Layout(400, 200) }
	frame()
	handle := func() uint32 {
		var got uint32
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if c, ok := ui.FillColor(e); ok {
				rgb := uint32(c.R*255+0.5)<<16 | uint32(c.G*255+0.5)<<8 | uint32(c.B*255+0.5)
				switch rgb {
				case 0x808080, 0x0000ff, 0xff0000:
					got = rgb
				}
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(tree)
		return got
	}
	if got := handle(); got != 0x808080 {
		t.Fatalf("handle at rest is %06x, want 808080", got)
	}
	move := func(x float32) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: 100}})
		frame()
		frame()
	}
	move(123)
	if got := handle(); got != 0x0000ff {
		t.Errorf("hovered handle is %06x, want 0000ff", got)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 123, Y: 100, Button: 1}})
	frame()
	move(160)
	move(300) // far off the handle: still pressed
	if got := handle(); got != 0xff0000 {
		t.Errorf("pressed handle is %06x, want ff0000", got)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: 300, Y: 100, Button: 1}})
	frame()
	move(20) // off the handle
	if got := handle(); got != 0x808080 {
		t.Errorf("released, unhovered handle is %06x, want 808080", got)
	}
}
