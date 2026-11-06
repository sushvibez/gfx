package gml

import (
	"os"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestQMLItemChildrenOverlap: in QML an Item is not a layout — its children
// all sit at (0,0) unless anchored. gml's own dialect treats a box as a flex
// container (that is what justify/items/spacing are for), so the QML rule
// applies to .qml documents only.
func TestQMLItemChildrenOverlap(t *testing.T) {
	src := `import QtQuick
Item {
    width: 200; height: 200
    Rectangle { id: a; width: 100; height: 40; color: "#ff0000" }
    Rectangle { id: b; width: 100; height: 40; color: "#00ff00" }
}`
	dir := t.TempDir()
	// Same markup, two extensions: .qml gets Qt semantics, .gml keeps ours.
	if err := os.WriteFile(dir+"/page.qml", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/page.gml", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	yOf := func(path string) (float32, float32) {
		d, err := Load(path, NewEnv())
		if err != nil {
			t.Fatalf("Load %s: %v", path, err)
		}
		u := ui.New(nil, nil, d.View())
		u.Layout(200, 200)
		u.Layout(200, 200)
		ea, eb := d.doc.elems.get(d.doc.intern("a")), d.doc.elems.get(d.doc.intern("b"))
		if ea == nil || eb == nil {
			t.Fatalf("%s: rectangles did not build", path)
		}
		return ui.Bounds(ea).Y, ui.Bounds(eb).Y
	}
	if ay, by := yOf(dir + "/page.qml"); ay != by {
		t.Errorf(".qml: children at y=%v and y=%v — QML overlaps them at 0", ay, by)
	}
	if ay, by := yOf(dir + "/page.gml"); ay == by {
		t.Errorf(".gml: children both at y=%v — the gml dialect stacks them", ay)
	}
}

// TestAttributeOverrideThroughID: QML lets a plain attribute be assigned
// through an element's id (`box.width = 40`), and the value wins over the
// markup from then on.
func TestAttributeOverrideThroughID(t *testing.T) {
	src := `
Item {
    width: 200; height: 200
    Rectangle { id: box; width: 20; height: 20; color: "#334455" }
    Component.onCompleted: box.width = 120
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	u.Layout(200, 200)
	el := d.doc.elems.get(d.doc.intern("box"))
	if el == nil {
		t.Fatal("box did not build")
	}
	if w := ui.Bounds(el).W; w != 120 {
		t.Errorf("width = %v after `box.width = 120`, want 120", w)
	}
}

// TestAliasPropertyWrite: `property alias canvasWidth: canvas.width` on a
// component, assigned at the use site, must reach the aliased element — the
// diorama page sizes every layer that way.
func TestAliasPropertyWrite(t *testing.T) {
	dir := t.TempDir()
	comp := `import QtQuick
Item {
    id: layer
    property alias boxWidth: inner.width
    width: inner.width
    Rectangle { id: inner; width: 10; height: 30; color: "#00d1a9" }
}`
	if err := os.WriteFile(dir+"/Layer.qml", []byte(comp), 0o644); err != nil {
		t.Fatal(err)
	}
	main := `import QtQuick
Item {
    width: 300; height: 100
    Layer { id: one; boxWidth: 150 }
}`
	if err := os.WriteFile(dir+"/Main.qml", []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Load(dir+"/Main.qml", NewEnv())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 100)
	u.Layout(300, 100)
	var found float32
	for ki, el := range d.doc.elems.v {
		k := keyID(ki)
		if el == nil {
			continue
		}
		if key := d.doc.keyStr(k); len(key) >= 5 && key[len(key)-5:] == "inner" {
			found = ui.Bounds(el).W
		}
	}
	if found != 150 {
		t.Errorf("the aliased rectangle is %v wide, want the use site's 150", found)
	}
}
