package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// A Canvas is a layout item: `Layout.fillWidth` + `Layout.preferredHeight`
// reach it, where before it sat at the 300x150 an HTML canvas defaults to
// (the states page's easing plot came out half as wide and twice as tall as
// Qt's). And a LAYOUT inside a layout fills by default when its own items
// can use the room — Qt's fill hints default to false for an item and true
// for a layout, which is how the toasts page's tile strip grows past the
// `Layout.preferredHeight: 72` it states.
func TestCanvasAndNestedLayoutAreLayoutItems(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
import QtQuick.Layouts
Rectangle {
    width: 500; height: 400
    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 20
        spacing: 10
        Text { text: "Header" }
        RowLayout {
            id: strip
            Layout.fillWidth: true
            Layout.preferredHeight: 72
            Repeater {
                model: 3
                Rectangle { Layout.fillWidth: true; Layout.fillHeight: true; color: "#303030" }
            }
        }
        Canvas {
            id: plot
            Layout.fillWidth: true
            Layout.preferredHeight: 60
        }
        Item { Layout.fillHeight: true }
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(500, 400)
	var canvas, strip ui.Element
	var walk func(ui.Element)
	walk = func(e ui.Element) {
		if e == nil {
			return
		}
		if _, ok := e.(*ui.Canvas); ok && canvas == nil {
			canvas = e
		}
		for _, ch := range ui.Children(e) {
			walk(ch)
		}
	}
	walk(tree)
	if canvas == nil {
		t.Fatal("no canvas in the tree")
	}
	cv := ui.Bounds(canvas)
	if cv.H != 60 {
		t.Errorf("canvas height = %v, want its stated 60", cv.H)
	}
	if cv.W != 460 {
		t.Errorf("canvas width = %v, want the column's 460", cv.W)
	}
	// The strip grew past its preferred height: its own tiles fill, so it
	// is not pinned at 72 the way a plain item would be. The column is the
	// root's only child; the strip is its second item.
	// Walk down to the column: an anchor-filled child is wrapped, so find
	// the first element with four items.
	var items []ui.Element
	var find func(ui.Element)
	find = func(e ui.Element) {
		if e == nil || items != nil {
			return
		}
		kids := ui.Children(e)
		if len(kids) == 4 {
			items = kids
			return
		}
		for _, k := range kids {
			find(k)
		}
	}
	find(tree)
	if items == nil {
		t.Fatal("the column's four items are not in the tree")
	}
	strip = items[1]
	if h := ui.Bounds(strip).H; h < 100 {
		t.Errorf("the nested RowLayout is %v tall: it took no share of the column's leftover", h)
	}
}

// TestScrollViewContentIsWhatParentMeans: a child of a ScrollView means the
// CONTENT ITEM by `parent`, not the enclosing box — its width is the
// viewport's availableWidth. graphics3d's control panel sizes its whole
// column that way (`ColumnLayout { width: parent.width }`), and reading the
// panel instead made every control in it as wide as the PANEL, so each one
// overflowed the clip and the slider handles sat too far right.
func TestScrollViewContentIsWhatParentMeans(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
Rectangle {
    width: 500; height: 400
    Rectangle {
        id: panel
        width: 320
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        ScrollView {
            anchors.fill: parent
            anchors.margins: 25
            contentWidth: availableWidth
            ColumnLayout {
                width: parent.width
                Rectangle { id: row; Layout.fillWidth: true; height: 20; color: "#303030" }
            }
        }
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(500, 400)
	u.Layout(500, 400) // the width binding reads the previous frame's bounds
	el := d.doc.elems.get(d.doc.intern("row"))
	if el == nil {
		t.Fatal("the row did not build")
	}
	if w := ui.Bounds(el).W; w != 270 {
		t.Errorf("row width = %v, want the viewport's 270 (320 - 2*25)", w)
	}
}

// TestLayoutSkipsInvisibleItems: a layout gives an item with `visible:
// false` no slot AND no spacing — the diorama's collapsed maths panel cost
// its scene 12px of height when an empty element still ate the gap.
// Outside a layout the invisible item stays in the tree with geometry: the
// shaders page hides the square its three effects take as their source and
// anchor to.
func TestLayoutSkipsInvisibleItems(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
import QtQuick.Layouts
Rectangle {
    width: 300; height: 200
    ColumnLayout {
        anchors.fill: parent
        spacing: 10
        Rectangle { id: head; Layout.fillWidth: true; height: 20; color: "#404040" }
        Rectangle { id: hidden; visible: false; Layout.fillWidth: true; height: 40; color: "#ff0000" }
        Rectangle { id: body; Layout.fillWidth: true; Layout.fillHeight: true; color: "#202020" }
    }
    Item {
        anchors.fill: parent
        Rectangle { id: source; visible: false; x: 30; y: 40; width: 50; height: 60; color: "#00ff00" }
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	body := d.doc.elems.get(d.doc.intern("body"))
	if body == nil {
		t.Fatal("the body did not build")
	}
	// 200 - 20 (head) - 10 (one gap) = 170: the hidden row costs nothing,
	// neither its 40px nor a second gap.
	if b := ui.Bounds(body); b.H != 170 || b.Y != 30 {
		t.Errorf("body = %v, want y 30 h 170", b)
	}
	if src := d.doc.elems.get(d.doc.intern("source")); src == nil {
		t.Error("the hidden source item is not in the tree: an effect anchored to it has nothing to read")
	} else if b := ui.Bounds(src); b.W != 50 || b.H != 60 {
		t.Errorf("hidden source bounds = %v, want 50x60 (invisible, but it still HAS geometry)", b)
	}
}

// TestLayoutSharesSurplusByPreferredSize: Qt's layout engine divides the
// space among the items that FILL in proportion to their preferred sizes —
// the stretch factor defaults to the size hint. The toasts page is the
// case: a column holding a `Layout.preferredHeight: 72` tile strip and a
// bare `Item { Layout.fillHeight: true }` gives the strip nearly the whole
// column, where an equal split (ours) left it at the stated 72 with the
// panel empty beneath it. Probed against Qt three ways: 72+0 -> 608/7,
// 72+24 -> 461/154 (exactly 3:1), no preferences -> 308/307.
func TestLayoutSharesSurplusByPreferredSize(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
import QtQuick.Layouts
Rectangle {
    width: 300; height: 400
    ColumnLayout {
        anchors.fill: parent
        spacing: 0
        Rectangle { id: strip; Layout.fillWidth: true; Layout.fillHeight: true; Layout.preferredHeight: 72; color: "#303030" }
        Rectangle { id: other; Layout.fillWidth: true; Layout.fillHeight: true; Layout.preferredHeight: 24; color: "#404040" }
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 400)
	strip, other := d.doc.elems.get(d.doc.intern("strip")), d.doc.elems.get(d.doc.intern("other"))
	if strip == nil || other == nil {
		t.Fatal("the two items did not build")
	}
	sh, oh := ui.Bounds(strip).H, ui.Bounds(other).H
	if sh+oh != 400 {
		t.Fatalf("heights %v + %v do not fill the column's 400", sh, oh)
	}
	// 72:24 is 3:1 — 300 and 100 of the 400.
	if sh < 295 || sh > 305 {
		t.Errorf("the 72-preference item is %v tall, want ~300 (three quarters)", sh)
	}
}

// TestRowSharesWidthByContent: a row of filling items with no stated
// preference divides its width in proportion to what each item's own
// CONTENT wants — Qt's stretch factor defaults to the size hint. Probed
// against Qt with four labelled columns whose implicit widths were
// 45.5/22.6/79.4/32.7: they came out 210/105/365/150, the same ratios.
// (Heights are left alone: a column's preferred height is height-for-width,
// and our first pass measures a card before its width settles.)
func TestRowSharesWidthByContent(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
import QtQuick.Layouts
Rectangle {
    width: 400; height: 100
    RowLayout {
        anchors.fill: parent
        spacing: 0
        Rectangle { id: wide; Layout.fillWidth: true; implicitWidth: 120; color: "#303030" }
        Rectangle { id: narrow; Layout.fillWidth: true; implicitWidth: 20; color: "#404040" }
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(400, 100)
	wide, narrow := d.doc.elems.get(d.doc.intern("wide")), d.doc.elems.get(d.doc.intern("narrow"))
	if wide == nil || narrow == nil {
		t.Fatal("the two items did not build")
	}
	ww, nw := ui.Bounds(wide).W, ui.Bounds(narrow).W
	if ww+nw < 396 || ww+nw > 404 {
		t.Fatalf("widths %v + %v do not fill the row's 400", ww, nw)
	}
	// Six times the implicit width, so the wide one takes about six times
	// the width; an equal split would give each 200.
	if ww < 3*nw {
		t.Errorf("widths %v and %v: the row is not sharing by content", ww, nw)
	}
}

// TestListViewFillsItsSlot: a ListView is a layout item like any other —
// its `Layout.fillHeight` used to reach nothing at all, so the navigation
// display's flight plan sat at the 200px a bare list defaults to, centred
// in a slot twice that: its first row landed 80px below Qt's and its last
// row fell off the bottom.
func TestListViewFillsItsSlot(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
import QtQuick.Layouts
Rectangle {
    width: 300; height: 400
    ColumnLayout {
        anchors.fill: parent
        spacing: 0
        Rectangle { id: head; Layout.fillWidth: true; Layout.preferredHeight: 40; color: "#303030" }
        ListView {
            id: list
            Layout.fillWidth: true
            Layout.fillHeight: true
            model: 20
            delegate: Rectangle { width: 300; height: 36; color: "#202020" }
        }
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 400)
	u.Layout(300, 400)
	el := d.doc.elems.get(d.doc.intern("list"))
	if el == nil {
		t.Fatal("the list did not build")
	}
	b := ui.Bounds(el)
	if b.Y != 40 {
		t.Errorf("the list starts at y=%v, want 40 (right under the header)", b.Y)
	}
	if b.H != 360 {
		t.Errorf("the list is %v tall, want the column's leftover 360", b.H)
	}
}
