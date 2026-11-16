package gml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// The order Component.onCompleted handlers run in. Qt documents it as
// undefined, but it is stable and pages are written against it; this is
// what Qt 6.10.1's qml runtime prints for these two files (run offscreen,
// 2026-11-16): a view's delegates first, then each component's root before
// what it holds, and siblings LAST to first.
func TestCompletionOrder(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Inner.qml", `import QtQuick

Item {
    property string tag: ""
    Component.onCompleted: root.note(tag + ".self")
    Item { Component.onCompleted: root.note(tag + ".child") }
}
`)
	write("Main.qml", `import QtQuick

Item {
    id: root
    width: 100; height: 100
    property string log: ""
    function note(s) { log += (log === "" ? "" : ",") + s }
    Component.onCompleted: note("root")
    Item {
        Component.onCompleted: root.note("a")
        Item { Component.onCompleted: root.note("a1") }
        Item { Component.onCompleted: root.note("a2") }
    }
    Inner { tag: "b" }
    Item {
        Component.onCompleted: root.note("c")
        Inner { tag: "c1" }
    }
    Repeater {
        model: 2
        Item { Component.onCompleted: root.note("r" + index) }
    }
    Item { Component.onCompleted: root.note("d") }
}
`)
	if got, qt := completionLog(t, dir, "Main.qml"), "r0,r1,root,d,c,c1.self,c1.child,b.self,b.child,a,a2,a1"; got != qt {
		t.Errorf("handlers ran in the order\n  %s\nQt runs them in the order\n  %s", got, qt)
	}
	// A handler on a component's use site as well as on its root, and
	// delegates that hold handlers of their own.
	write("Main2.qml", `import QtQuick

Item {
    id: root
    width: 100; height: 100
    property string log: ""
    function note(s) { log += (log === "" ? "" : ",") + s }
    Component.onCompleted: note("root")
    Inner { tag: "b"; Component.onCompleted: root.note("b.use") }
    Repeater {
        model: 2
        Item {
            Component.onCompleted: root.note("r" + index)
            Item { Component.onCompleted: root.note("r" + index + ".k1") }
            Item { Component.onCompleted: root.note("r" + index + ".k2") }
        }
    }
    Item {
        Repeater {
            model: 2
            Item { Component.onCompleted: root.note("s" + index) }
        }
        Component.onCompleted: root.note("list")
    }
    Item { Component.onCompleted: root.note("d") }
}
`)
	if got, qt := completionLog(t, dir, "Main2.qml"), "s0,s1,r0,r0.k2,r0.k1,r1,r1.k2,r1.k1,root,d,list,b.self,b.use,b.child"; got != qt {
		t.Errorf("handlers ran in the order\n  %s\nQt runs them in the order\n  %s", got, qt)
	}
	// A root with no handler, components with and without one at the use
	// site, an element that draws nothing, and a handler two levels down.
	write("Main3.qml", `import QtQuick

Item {
    id: root
    width: 100; height: 100
    property string log: ""
    function note(s) { log += (log === "" ? "" : ",") + s }
    Item { Component.onCompleted: root.note("x") }
    Inner { tag: "b" }
    Item { Component.onCompleted: root.note("y") }
    Inner { tag: "c"; Component.onCompleted: root.note("c.use") }
    Timer { Component.onCompleted: root.note("timer") }
    Item {
        Item { Component.onCompleted: root.note("deep") }
    }
}
`)
	if got, qt := completionLog(t, dir, "Main3.qml"), "deep,timer,c.self,c.use,c.child,y,b.self,b.child,x"; got != qt {
		t.Errorf("handlers ran in the order\n  %s\nQt runs them in the order\n  %s", got, qt)
	}
}

// completionLog loads a page whose handlers append to its `log` property
// and returns the log once the page has completed.
func completionLog(t *testing.T, dir, name string) string {
	t.Helper()
	d, err := Load(filepath.Join(dir, name), NewEnv())
	if err != nil {
		t.Fatal(err)
	}
	view := d.View()
	u := ui.New(nil, nil, func() ui.Element { return view() })
	for i := 0; i < 6; i++ {
		u.Layout(300, 200)
	}
	return str(d.Property("log"))
}
