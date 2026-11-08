package gml

import (
	"strings"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestTableViewObjectModel: a TableView renders from a Go-registered object
// exposing the table contract (props "table" + "headers"), not just the
// declarative TableModel.
func TestTableViewObjectModel(t *testing.T) {
	RegisterType("MiniTableModel", func(emit EmitFunc) *NativeObject {
		return &NativeObject{
			Props: map[string]any{
				"headers": []any{"ID", "Name"},
				"table": []any{
					[]any{float64(1), "Ada"},
					[]any{float64(2), "Grace"},
				},
			},
		}
	})
	src := `
Column {
    MiniTableModel { id: mini }
    TableView {
        id: tv
        model: mini
        delegate: Rectangle {
            width: 100; height: 20
            Text { text: display; color: "#ffffff" }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	view := d.View()
	if el := view(); el == nil {
		t.Fatal("nil view")
	}
	// The view id exposes live dims read from the object.
	obj := d.doc.elemObjs.get(d.doc.intern("tv"))
	if obj == nil {
		t.Fatal("tv not registered")
	}
	rows, _ := obj.member("rows")
	cols, _ := obj.member("columns")
	if rows != float64(2) || cols != float64(2) {
		t.Fatalf("dims = %v x %v, want 2 x 2", rows, cols)
	}
}

// TestDelegateChooser: DelegateChoice column criteria select per-cell
// delegates; unmatched cells fall to a criterion-less choice.
func TestDelegateChooser(t *testing.T) {
	src := `
Column {
    import QtQuick
    TableModel {
        id: tm
        TableModelColumn { display: "a" }
        TableModelColumn { display: "b" }
        rows: [ { a: "A0", b: "B0" } ]
    }
    TableView {
        id: tv
        model: tm
        delegate: DelegateChooser {
            DelegateChoice {
                column: 0
                delegate: Rectangle { width: 50; height: 20; Text { text: "col0:" + display; color: "#fff" } }
            }
            DelegateChoice {
                delegate: Rectangle { width: 50; height: 20; Text { text: "rest:" + display; color: "#fff" } }
            }
        }
    }
}`
	// imports live at file top normally; strip the misplaced one above.
	src = strings.Replace(src, "    import QtQuick\n", "", 1)
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
}

// TestTableViewRowSelection: a selectionModel enables tap-to-select-row;
// delegates see selected, the ItemSelectionModel reflects it, and
// clearSelection resets.
func TestTableViewRowSelection(t *testing.T) {
	src := `
Column {
    ItemSelectionModel { id: sel }
    TableModel {
        id: tm
        TableModelColumn { display: "a" }
        rows: [ { a: "r0" }, { a: "r1" }, { a: "r2" } ]
    }
    TableView {
        id: tv
        model: tm
        selectionModel: sel
        delegate: Rectangle { width: 80; height: 20; Text { text: display; color: "#fff" } }
    }
    Text { id: status; text: sel.hasSelection ? "sel" : "none"; color: "#fff" }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	view := d.View()
	view()
	sel := d.doc.elemObjs.get(d.doc.intern("sel"))
	if sel == nil {
		t.Fatal("sel not registered")
	}
	if hs, _ := sel.member("hasSelection"); hs != false {
		t.Fatalf("initial hasSelection = %v", hs)
	}
	// Select row 1 through the table's state (what a cell tap does).
	if st := d.doc.tables["tv"]; st != nil {
		st.Select(1)
	} else {
		t.Fatal("the table keeps no ui.TableState")
	}
	view()
	if hs, _ := sel.member("hasSelection"); hs != true {
		t.Fatal("hasSelection stayed false after select")
	}
	idx, _ := sel.member("selectedIndexes")
	arr, _ := idx.([]any)
	if len(arr) != 1 {
		t.Fatalf("selectedIndexes = %v", idx)
	}
	if row := arr[0].(map[string]any)["row"]; row != float64(1) {
		t.Fatalf("selected row = %v, want 1", row)
	}
}

// TestListViewCurrentItem: ListView.isCurrentItem is live against
// currentIndex, and the highlight component renders behind the current row.
func TestListViewCurrentItem(t *testing.T) {
	src := `
Column {
    ListView {
        id: lv
        height: 120; rowHeight: 24
        model: 3
        currentIndex: 1
        highlight: Rectangle { color: "#334455"; radius: 4 }
        delegate: Rectangle {
            id: dl
            width: 100; height: 24
            color: dl.ListView.isCurrentItem ? "#ff0000" : "#222222"
            MouseArea { anchors.fill: parent; onClicked: lv.currentIndex = index }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	view := d.View()
	el := view()
	if el == nil {
		t.Fatal("nil view")
	}
	// currentIndex seeded from the attr.
	r, ok := d.doc.synthRefs["lv|currentIndex"]
	if !ok {
		t.Fatal("currentIndex ref missing")
	}
	if v, _ := toInt(r.get()); v != 1 {
		t.Fatalf("currentIndex = %v, want 1 (attr seed)", v)
	}
	_ = el
	// Rows build lazily during layout; lay out to instantiate them, then
	// check the per-row attached state.
	u := ui.New(nil, nil, view)
	u.Layout(200, 120)
	cur := func(row string) any {
		obj := d.doc.elemObjs.get(d.doc.intern("lv#" + row + "/dl"))
		if obj == nil {
			t.Fatalf("row %s instance missing", row)
		}
		// Resolved on READ (rowAttachedRef), so that a binding reading
		// it records the dependency on the view's currentIndex.
		v, _ := obj.member("ListView")
		att, _ := v.(map[string]any)
		return att["isCurrentItem"]
	}
	if cur("1") != true || cur("0") != false {
		t.Fatalf("isCurrentItem: row1=%v row0=%v, want true/false", cur("1"), cur("0"))
	}
	// Flip currentIndex; the next frame's layout re-derives row state.
	_ = r.set(2)
	u.Layout(200, 120)
	if cur("2") != true || cur("1") != false {
		t.Fatalf("after set(2): row2=%v row1=%v, want true/false", cur("2"), cur("1"))
	}
}

// TestBareSelfGeometry: `radius: width / 2` resolves width against the
// element's own (previous-frame) bounds — QML unqualified-name scoping.
func TestBareSelfGeometry(t *testing.T) {
	src := `
Column {
    Rectangle {
        id: circ
        width: 60; height: 60
        radius: width / 2
        color: "#ff0000"
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	view := d.View()
	u := ui.New(nil, nil, view)
	// Frame 1 lays out (radius 0 — no bounds yet); the geometry signature
	// then forces a settle rebuild where width resolves to 60.
	u.Layout(200, 200)
	u.Layout(200, 200)
	u.Layout(200, 200)
	el := d.doc.elems.get(d.doc.intern("circ"))
	if el == nil {
		t.Fatal("circ not registered")
	}
	if b := ui.Bounds(el); b.W != 60 {
		t.Fatalf("width = %v, want 60", b.W)
	}
	// The radius landed on the div: reach it via the exported probe.
	if r := ui.CornerRadius(el); r != 30 {
		t.Fatalf("radius = %v, want 30 (width/2)", r)
	}
}
