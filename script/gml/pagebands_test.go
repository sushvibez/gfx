package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// A window's menuBar, header and footer band its content, which fills
// what they leave; a Page takes a header given as a component.
func TestPageBands(t *testing.T) {
	h := newStates(t, `
import QtQuick 2.15
import QtQuick.Controls 2.15
ApplicationWindow {
    width: 400; height: 300
    menuBar: Rectangle { height: 20; color: "#0000ff" }
    header: Rectangle { height: 30; color: "#ff0000" }
    footer: Rectangle { height: 25; color: "#00ff00" }
    Rectangle { anchors.fill: parent; color: "#333333" }
}`)
	for _, tc := range []struct {
		name string
		rgb  uint32
		want ui.Rect
	}{
		{"menuBar", 0x0000ff, ui.Rect{X: 0, Y: 0, W: 400, H: 20}},
		{"header", 0xff0000, ui.Rect{X: 0, Y: 20, W: 400, H: 30}},
		{"content", 0x333333, ui.Rect{X: 0, Y: 50, W: 400, H: 225}},
		{"footer", 0x00ff00, ui.Rect{X: 0, Y: 275, W: 400, H: 25}},
	} {
		if got := h.rect(tc.rgb); got != tc.want {
			t.Errorf("%s at %+v, want %+v", tc.name, got, tc.want)
		}
	}

	p := newStates(t, `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    width: 400; height: 300
    Component { id: bar; Rectangle { height: 40; color: "#ff0000" } }
    Page { x: 0; y: 0; width: 200; height: 200; header: bar
        Rectangle { anchors.fill: parent; color: "#333333" } }
}`)
	if got := p.rect(0xff0000); got.H != 40 || got.W != 200 || got.Y != 0 {
		t.Errorf("page header at %+v, want 200x40 at the top", got)
	}
	if got := p.rect(0x333333); got.Y != 40 || got.H != 160 {
		t.Errorf("page content at %+v, want y 40, 160 tall", got)
	}
}
