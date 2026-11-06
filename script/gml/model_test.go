package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// A ListModel filled in its OWN Component.onCompleted reaches the view.
// QML builds a model imperatively that way (`for (...) append({...})` —
// the gridview page's dynamic grid), but a model is DECLARED, never built,
// so nothing collected its lifecycle handler and the grid drew nothing.
// Inside the handler the model's own methods resolve bare.
func TestListModelCompletedFillsTheModel(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
GridView {
    id: grid
    width: 200; height: 200
    cellWidth: 50; cellHeight: 50
    model: ListModel {
        id: dyn
        Component.onCompleted: {
            var colors = ["#00d1a9", "#fea601", "#4fc3f7"]
            for (var i = 0; i < 3; i++)
                append({ itemId: i + 1, clr: colors[i] })
        }
    }
    delegate: Rectangle { width: 46; height: 46; color: "#303030" }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(200, 200) // first build: queues onCompleted
	u.Layout(200, 200) // second: the rows are there
	m, ok := env.vars["dyn"].(*listModel)
	if !ok {
		t.Fatalf("dyn is %T, want the model", env.vars["dyn"])
	}
	if len(m.rows) != 3 {
		t.Fatalf("model has %d rows, want 3", len(m.rows))
	}
	cells := 0
	var walk func(ui.Element)
	walk = func(e ui.Element) {
		if e == nil {
			return
		}
		if c, ok := ui.FillColor(e); ok && c.A > 0 {
			cells++
		}
		for _, ch := range ui.Children(e) {
			walk(ch)
		}
	}
	walk(tree)
	if cells != 3 {
		t.Errorf("the view built %d cells, want 3 (%T)", cells, tree)
	}
	// A rebuild must not undo the appends (the declarations are only a seed).
	u.Layout(200, 200)
	if len(m.rows) != 3 {
		t.Errorf("after a rebuild the model has %d rows", len(m.rows))
	}
}

// TestRepeaterRowsKeepTheirOwnPosition: inside a plain Item (an absolute
// container, not a positioner), each Repeater row is placed by the
// delegate's own x/y — per-row expressions off the model's roles. They used
// to be placed at the origin, which piled the aircraft blueprint's markers
// in one spot in the corner instead of over the plane.
func TestRepeaterRowsKeepTheirOwnPosition(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
Rectangle {
    width: 400; height: 300
    ListModel {
        id: marks
        ListElement { posX: 0.25; posY: 0.5 }
        ListElement { posX: 0.75; posY: 0.25 }
    }
    Item {
        id: field
        anchors.fill: parent
        Repeater {
            model: marks
            Rectangle {
                required property real posX
                required property real posY
                x: posX * field.width - width / 2
                y: posY * field.height - height / 2
                width: 20; height: 20; color: "#4CAF50"
            }
        }
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(400, 300)
	u.Layout(400, 300) // the row x/y read field.width, so let it settle
	var got []ui.Rect
	var walk func(ui.Element)
	walk = func(e ui.Element) {
		if e == nil {
			return
		}
		// The anchor slot and the rectangle inside it share bounds; count
		// each position once.
		if b := ui.Bounds(e); b.W == 20 && b.H == 20 {
			if len(got) == 0 || got[len(got)-1] != b {
				got = append(got, b)
			}
		}
		for _, ch := range ui.Children(e) {
			walk(ch)
		}
	}
	walk(tree)
	if len(got) != 2 {
		t.Fatalf("found %d rows, want 2", len(got))
	}
	want := []ui.Rect{{X: 90, Y: 140, W: 20, H: 20}, {X: 290, Y: 65, W: 20, H: 20}}
	for i, w := range want {
		if got[i].X != w.X || got[i].Y != w.Y {
			t.Errorf("row %d at (%v,%v), want (%v,%v)", i, got[i].X, got[i].Y, w.X, w.Y)
		}
	}
}

// TestCompletionSeesLaidOutGeometry: a Component.onCompleted handler reads
// real geometry, including through an id declared inside a file component —
// QML sizes the item tree during instantiation, so the corpus' blueprint
// viewer computes its zoom (`fitToView()`, which divides by a Flickable's
// width) from one. Our geometry exists only once a tree has been laid out,
// so the handlers wait for a laid-out tree rather than a build count.
func TestCompletionSeesLaidOutGeometry(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
import QtQuick.Layouts
Rectangle {
    id: root
    property real fitted: 0
    ColumnLayout {
        anchors.fill: parent
        Text { text: "header" }
        Flickable { id: flick; Layout.fillWidth: true; Layout.fillHeight: true }
    }
    Component.onCompleted: root.fitted = flick.width
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(400, 300)
	u.Layout(400, 300)
	got, _ := toFloat64(d.doc.props["fitted"].get())
	if got != 400 {
		t.Errorf("fitted = %v, want the Flickable's laid-out width 400", got)
	}
}
