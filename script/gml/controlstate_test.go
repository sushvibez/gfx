package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestControlStateReadable: a control's own state is readable as id.attr,
// whether the markup binds an external property or gives a literal — and
// it resolves even when the read comes BEFORE the control in the file
// (declare-time seeding), which is how QML label-above-slider markup reads.
func TestControlStateReadable(t *testing.T) {
	src := `
Column {
    property real ext: 4
    property string before: "" + sl.value
    Text { text: "A: " + sl.value; color: "#ffffff" }
    Slider { id: sl; from: 1; to: 7; value: 3 }
    Slider { id: bound; from: 0; to: 10; value: ext }
    CheckBox { id: cb; checked: true }
    property string after: "" + bound.value + "/" + cb.checked
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	if got := str(d.doc.props["before"].get()); got != "3" {
		t.Errorf("forward read sl.value = %q, want 3", got)
	}
	if got := str(d.doc.props["after"].get()); got != "4/true" {
		t.Errorf("bound.value/cb.checked = %q, want 4/true", got)
	}
}

// TestSliderIntProperty: `value: someIntProp` is legal QML — the Slider
// mirrors the int through its own real state instead of being dropped.
func TestSliderIntProperty(t *testing.T) {
	src := `
Column {
    property int count: 6
    Slider { from: 3; to: 12; value: count; stepSize: 1 }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
}

// TestShapePaths: a Shape with ShapePath children builds (paths are
// evaluated at build time, so a bad path element fails here, not silently
// at paint).
func TestShapePaths(t *testing.T) {
	src := `
Row {
    Shape {
        width: 60; height: 40
        ShapePath {
            strokeWidth: 2; strokeColor: "#33aadd"; fillColor: "#222244"
            startX: 0; startY: 0
            PathLine { x: 60; y: 0 }
            PathQuad { x: 0; y: 40; controlX: 30; controlY: 20 }
            PathArc { x: 0; y: 0; radiusX: 20; radiusY: 20 }
        }
    }
    Shape {
        width: 40; height: 40
        ShapePath {
            startX: 0; startY: 0
            PathSvg { path: "M 0 0 L 40 40 L 0 40 Z" }
            fillGradient: LinearGradient {
                x1: 0; y1: 0; x2: 40; y2: 40
                GradientStop { position: 0; color: "#00d1a9" }
                GradientStop { position: 1; color: "#9b59b6" }
            }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
}

// TestItemStateProperty: `state` is live item state — readable and
// writable as id.state, with the markup binding as its default.
func TestItemStateProperty(t *testing.T) {
	src := `
Column {
    Rectangle {
        id: box
        width: 40; height: 40; color: "#333333"
        states: [
            State { name: "big"; PropertyChanges { target: box; width: 80 } }
        ]
    }
    property string s: box.state
    Text { text: "s=" + box.state; color: "#ffffff" }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	if got := str(d.doc.props["s"].get()); got != "" {
		t.Errorf("initial state = %q, want empty", got)
	}
	// An imperative assignment through the exposed ref switches states.
	ref, _ := d.doc.elemObjs.get(d.doc.intern("box")).props["state"].(Ref)
	if ref == nil {
		t.Fatal("box.state not exposed")
	}
	if err := ref.set("big"); err != nil {
		t.Fatalf("set state: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view after state change")
	}
	if got := str(d.doc.props["s"].get()); got != "big" {
		t.Errorf("state after set = %q, want big", got)
	}
}

// TestRegexLiteralAndMultilineAssign: JS syntax the corpus uses — a regex
// literal in a value position and an assignment whose value starts on the
// next line.
func TestRegexLiteralAndMultilineAssign(t *testing.T) {
	src := `
Column {
    property string out: ""
    property string pat: ""
    TextField { id: f; validator: /[0-9]*/ }
    Button {
        text: "go"
        onClicked: {
            out =
                "a" + "b"
                + "c"
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
}

// TestHandlerThenAttribute: `onClicked: doThing(); ToolTip.text: "x"` —
// inside an element body a `name:` after the semicolon starts the next
// attribute, not another statement in the handler.
func TestHandlerThenAttribute(t *testing.T) {
	src := `
Column {
    property int taps: 0
    Button { text: "a"; onClicked: taps = taps + 1; ToolTip.text: "hint"; opacity: 1 }
    Text { text: "" + taps; color: "#ffffff" }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
}

// TestLayoutFillAxes: Layout.fillWidth inside a ColumnLayout must stretch
// the item ACROSS the column, not grow it along the column's main axis
// (which would split the column between the rows and squash both).
func TestLayoutFillAxes(t *testing.T) {
	src := `
ColumnLayout {
    width: 200; height: 120
    Rectangle { id: top; Layout.fillWidth: true; implicitHeight: 20; color: "#a00000" }
    Rectangle { id: bot; Layout.fillWidth: true; implicitHeight: 20; color: "#00a000" }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 120) // first frame; geometry settles over the next ones
	u.Layout(200, 120)
	u.Layout(200, 120)
	for _, id := range []string{"top", "bot"} {
		el := d.doc.elems.get(d.doc.intern(id))
		if el == nil {
			t.Fatalf("%s not registered", id)
		}
		b := ui.Bounds(el)
		if b.W != 200 {
			t.Errorf("%s width = %v, want 200 (stretched across the column)", id, b.W)
		}
		if b.H != 20 {
			t.Errorf("%s height = %v, want 20 (its own implicit height)", id, b.H)
		}
	}
}

// TestComboBoxModelValues: a ComboBox publishes the QML read-only
// properties other bindings use — currentText/displayText/count and the
// typed currentValue (its row's valueRole field).
func TestComboBoxModelValues(t *testing.T) {
	src := `
Column {
    ComboBox {
        id: cb
        textRole: "label"
        valueRole: "px"
        model: ListModel {
            ListElement { label: "Small"; px: 40 }
            ListElement { label: "Large"; px: 80 }
        }
    }
    property string shown: "" + cb.currentText
    property string val: "" + cb.currentValue
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	// Two frames: the combo publishes its state as it builds, and the
	// document's declared properties read it on the next pass.
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	if got := str(d.doc.props["shown"].get()); got != "Small" {
		t.Errorf("currentText = %q, want Small", got)
	}
	if got := str(d.doc.props["val"].get()); got != "40" {
		t.Errorf("currentValue = %q, want 40 (the valueRole field)", got)
	}
}

// TestJSParserGaps: JS the corpus writes that the parser used to reject —
// several declarators in one `var`, a property whose value starts on the
// next line, `typeof`, and QML 6 type annotations on a function.
func TestJSParserGaps(t *testing.T) {
	src := `
Column {
    readonly property string kind:
        typeof 1 === "number" ? "num" : "other"
    property string joined: pad2(7) + "-" + sum()
    function pad2(n: int) : string {
        return n < 10 ? "0" + n : "" + n
    }
    function sum() {
        var a = [1, 2], b = 3, c = a.length + b
        return "" + c
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	if got := str(d.doc.props["kind"].get()); got != "num" {
		t.Errorf("typeof branch = %q, want num", got)
	}
	if got := str(d.doc.props["joined"].get()); got != "07-5" {
		t.Errorf("joined = %q, want 07-5", got)
	}
}

// TestAssignmentExpressions: assignments and ++/-- are expressions in JS —
// `{ id: n++ }` inside an object literal, `(t -= 1.5)` inside a larger
// expression — not just statements.
func TestAssignmentExpressions(t *testing.T) {
	src := `
Column {
    property string out: build()
    function build() {
        var n = 0
        var o = { id: n++, second: n++ }
        var v = 4
        var w = 1 - (v -= 1.5) * 2
        return "" + o.id + "," + o.second + "," + n + "," + v + "," + w
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	// Postfix yields the value from before the write (0 then 1, n ends 2);
	// the compound assignment writes 2.5 and yields it, so w = 1 - 5 = -4.
	if got := str(d.doc.props["out"].get()); got != "0,1,2,2.5,-4" {
		t.Fatalf("out = %q, want 0,1,2,2.5,-4", got)
	}
}
