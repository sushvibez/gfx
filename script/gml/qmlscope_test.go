package gml

import (
	"os"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestNestedFunctionDeclaration: a function DECLARED inside another
// function's body (JS's function statement) binds in that scope and is
// callable — the canvas painters in this corpus factor a repeated shape that
// way.
func TestNestedFunctionDeclaration(t *testing.T) {
	src := `
Item {
    property int total: 0
    function run() {
        var sum = 0
        function add(a, b) { return a + b }
        for (var i = 1; i <= 3; i++)
            sum = add(sum, i)
        return sum
    }
    Component.onCompleted: total = run()
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(100, 100)
	u.Layout(100, 100)
	if got, _ := toFloat64(d.doc.props["total"].get()); got != 6 {
		t.Errorf("total = %v, want 6 (the nested add ran three times)", got)
	}
}

// TestParentAttrAndParentOfID: a child reads a plain visual attribute of the
// item it sits in (`radius: parent.radius`), and a sibling reaches that item
// through another id (`someId.parent.prop`).
func TestParentAttrAndParentOfID(t *testing.T) {
	src := `
Item {
    Rectangle {
        id: card
        width: 100; height: 50; radius: 12
        property bool night: true
        Rectangle { id: inner; radius: parent.radius }
    }
    property real innerRadius: inner.parent.radius
    property bool isNight: inner.parent.night
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	if got, _ := toFloat64(d.doc.props["innerRadius"].get()); got != 12 {
		t.Errorf("inner.parent.radius = %v, want 12", got)
	}
	if d.doc.props["isNight"].get() != true {
		t.Errorf("inner.parent.night = %v, want true", d.doc.props["isNight"].get())
	}
}

// TestSwipeViewRepeaterPages: a Repeater inside a navigation container
// expands into one page per model row, and the container reports the page
// count an indicator binds to.
func TestSwipeViewRepeaterPages(t *testing.T) {
	src := `
Item {
    width: 300; height: 200
    SwipeView {
        id: view
        currentIndex: 2
        Repeater {
            model: 5
            Text { text: "page " + index }
        }
    }
    property int pages: view.count
    property int shown: view.currentIndex
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	u.Layout(300, 200)
	if got, _ := toFloat64(d.doc.props["pages"].get()); got != 5 {
		t.Errorf("view.count = %v, want 5 (the Repeater's rows are pages)", got)
	}
	if got, _ := toFloat64(d.doc.props["shown"].get()); got != 2 {
		t.Errorf("view.currentIndex = %v, want 2", got)
	}
}

// TestPageIndicatorDots: PageIndicator builds one delegate per page with
// index in scope, and an interactive dot sets the index it stands for.
func TestPageIndicatorDots(t *testing.T) {
	src := `
Item {
    width: 200; height: 40
    PageIndicator {
        id: dots
        count: 4
        interactive: true
        delegate: Rectangle { required property int index; width: 8; height: 8 }
    }
    property int at: dots.currentIndex
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 40)
	obj := d.doc.elemObjs.get(d.doc.intern("dots"))
	if obj == nil {
		t.Fatal("no elemObject for the indicator")
	}
	if got, _ := toFloat64(obj.props["count"]); got != 4 {
		t.Errorf("dots.count = %v, want 4", got)
	}
	ref, ok := obj.props["currentIndex"].(Ref)
	if !ok {
		t.Fatalf("dots.currentIndex = %T, want a Ref", obj.props["currentIndex"])
	}
	if err := ref.set(3); err != nil {
		t.Fatal(err)
	}
	u.Layout(200, 40)
	if got, _ := toFloat64(d.doc.props["at"].get()); got != 3 {
		t.Errorf("the bound index reads %v after the indicator moved to 3", got)
	}
}

// TestFlickableScrollContract: a Flickable's scroll properties are readable
// bare inside its own scope, readable through its id, and contentX/contentY
// are assignable.
func TestFlickableScrollContract(t *testing.T) {
	src := `
Item {
    width: 200; height: 200
    Flickable {
        id: flick
        width: 200; height: 100
        contentWidth: 600
        property int page: Math.round(contentX / 200)
        property bool onTheMove: movingHorizontally
        Item { width: 600; height: 100 }
    }
    property int shownPage: flick.page
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	st := d.doc.scrolls["flick"]
	if st == nil {
		t.Fatal("no scroll state for the flickable")
	}
	// contentX is assignable — QML pages a Flickable by writing it.
	ref, ok := scrollOffsetRef(st, "contentX")
	if !ok {
		t.Fatal("contentX is not assignable")
	}
	if err := ref.set(400); err != nil {
		t.Fatal(err)
	}
	u.Layout(200, 200)
	if got, _ := toFloat64(d.doc.props["page"].get()); got != 2 {
		t.Errorf("page = %v after scrolling to 400 of 200-wide pages, want 2", got)
	}
	if d.doc.props["onTheMove"].get() != false {
		t.Error("movingHorizontally should read false, not fail the binding")
	}
}

// TestQualifiedPropertyChanges: Qt 6's PropertyChanges names its target per
// property (`morphRect.width: 200`) instead of with a `target:` line, and one
// block may address several elements.
func TestQualifiedPropertyChanges(t *testing.T) {
	src := `
Item {
    width: 200; height: 200
    Rectangle {
        id: morph
        width: 100; height: 100; radius: 0
        state: "circle"
        states: [
            State {
                name: "square"
                PropertyChanges { morph.radius: 0 }
            },
            State {
                name: "circle"
                PropertyChanges {
                    morph.radius: 50
                    morph.width: 120
                }
            }
        ]
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	el := d.doc.elems.get(d.doc.intern("morph"))
	if el == nil {
		t.Fatal("morph did not build")
	}
	if w := ui.Bounds(el).W; w != 120 {
		t.Errorf("width = %v in the circle state, want 120 (the qualified override)", w)
	}
}

// TestComboBoxDerivedBeforeBuild: a ComboBox's read-only properties have
// their real values (and types) even when read before the box builds.
func TestComboBoxDerivedBeforeBuild(t *testing.T) {
	src := `
Item {
    width: 300; height: 200
    // Reads the box BELOW it: sizes and colours a preview from currentValue.
    property real size: sizeCombo.currentValue
    property string label: sizeCombo.currentText
    property int n: sizeCombo.count
    ComboBox {
        id: sizeCombo
        currentIndex: 1
        textRole: "name"
        valueRole: "px"
        model: ListModel {
            ListElement { name: "Small"; px: 40 }
            ListElement { name: "Large"; px: 120 }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	if got, _ := toFloat64(d.doc.props["size"].get()); got != 120 {
		t.Errorf("currentValue = %v, want 120 (the valueRole of row 1)", got)
	}
	if got := str(d.doc.props["label"].get()); got != "Large" {
		t.Errorf("currentText = %q, want \"Large\"", got)
	}
	if got, _ := toFloat64(d.doc.props["n"].get()); got != 2 {
		t.Errorf("count = %v, want 2", got)
	}
}

// TestNativeIDInsideComponent: a host-registered object declared inside a
// component file owns its id — the per-instance scope must not shadow it with
// a plain element object, or its methods disappear (every settings card
// declares its own `SettingsManager { id: settings }`).
func TestNativeIDInsideComponent(t *testing.T) {
	recorded := -1
	RegisterType("TestBackend", func(EmitFunc) *NativeObject {
		return &NativeObject{
			Props: map[string]any{"ready": true},
			Methods: map[string]any{
				"allKeys": func([]any) (any, error) { return []any{"a", "b"}, nil },
				"record": func(args []any) (any, error) {
					if len(args) > 0 {
						if f, err := toFloat64(args[0]); err == nil {
							recorded = int(f)
						}
					}
					return nil, nil
				},
			},
		}
	})
	dir := t.TempDir()
	card := "import QtQuick\nItem { id: root\n" +
		"  TestBackend { id: backend }\n" +
		"  Component.onCompleted: backend.record(backend.allKeys().length)\n" +
		"  Text { text: \"card\" } }"
	if err := os.WriteFile(dir+"/Card.qml", []byte(card), 0o644); err != nil {
		t.Fatal(err)
	}
	main := "import QtQuick\nItem { Card { anchors.fill: parent } }"
	if err := os.WriteFile(dir+"/Main.qml", []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Load(dir+"/Main.qml", NewEnv())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 100)
	u.Layout(200, 100)
	u.Layout(200, 100)
	if recorded != 2 {
		t.Errorf("the card recorded %d, want 2 — the backend's id was shadowed", recorded)
	}
}

// TestObjectTruthinessAndToString: JS semantics for values the engine holds
// as Go objects — an object is truthy, and everything answers toString().
func TestObjectTruthinessAndToString(t *testing.T) {
	src := `
Item {
    property bool hasItem: box
    property string s: box.radius.toString()
    Rectangle { id: box; radius: 4 }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(100, 100)
	if d.doc.props["hasItem"].get() != true {
		t.Error("an element object must be truthy, as in JS")
	}
	if got := str(d.doc.props["s"].get()); got != "4" {
		t.Errorf("radius.toString() = %q, want \"4\"", got)
	}
}

// TestQtCallLater: Qt.callLater defers a call to the next frame, which is how
// QML syncs a highlight to geometry that only exists after layout.
func TestQtCallLater(t *testing.T) {
	src := `
Item {
    property int synced: 0
    function sync() { synced = synced + 1 }
    Component.onCompleted: Qt.callLater(sync)
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(100, 100)
	u.Layout(100, 100) // onCompleted runs once the tree has geometry
	if got, _ := toFloat64(d.doc.props["synced"].get()); got != 0 {
		t.Fatalf("synced = %v on the frame that queued the call, want 0", got)
	}
	drainPosted()
	if got, _ := toFloat64(d.doc.props["synced"].get()); got != 1 {
		t.Errorf("synced = %v after the queue drained, want 1", got)
	}
}

// TestTryCatchFinally: JS exception handling, including a runtime fault
// (which is what a bad member access looks like here) reaching the catch
// clause as an Error-shaped object.
func TestTryCatchFinally(t *testing.T) {
	src := `
Item {
    property string log: ""
    function run() {
        try {
            log = log + "a"
            throw "boom"
        } catch (e) {
            log = log + "|" + e
        } finally {
            log = log + "|done"
        }
        try {
            JSON.parse("{not json")
        } catch (e) {
            log = log + "|caught:" + (e.message !== undefined)
        }
    }
    Component.onCompleted: run()
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(100, 100)
	u.Layout(100, 100)
	got := str(d.doc.props["log"].get())
	want := "a|boom|done|caught:true"
	if got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// TestPropertyOrInvokable: a C++ type exposes the same value as a property on
// one page and as an invokable on another (QSqlQueryModel's rowCount /
// columnCount()), so a no-argument call on a plain value yields the value.
func TestPropertyOrInvokable(t *testing.T) {
	src := `
Item {
    property int a: box.count
    property int b: box.count()
    Item { id: box; property int count: 7 }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(100, 100)
	av, _ := toFloat64(d.doc.props["a"].get())
	bv, _ := toFloat64(d.doc.props["b"].get())
	if av != 7 || bv != 7 {
		t.Errorf("count as property = %v, as call = %v; want 7 both ways", av, bv)
	}
}

// TestNullModelIsEmpty: `model: null` — a var property the backend has not
// filled yet — shows nothing rather than failing the view.
func TestNullModelIsEmpty(t *testing.T) {
	src := `
Item {
    width: 100; height: 100
    property var rows: null
    Repeater { model: rows; Text { text: "row" } }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(100, 100)
	// Nothing to assert beyond "it built": a nil model used to be an error
	// that skipped the whole view.
}
