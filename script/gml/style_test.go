package gml

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"

	"github.com/sushvibez/gfx/ui"
)

// writeStyleModule lays out the shape a real Qt app uses: a style module
// beside the document, named by qtquickcontrols2.conf, whose files
// reimplement controls through QtQuick.Templates.
func writeStyleModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	style := filepath.Join(dir, "teststyle")
	if err := os.MkdirAll(style, 0o755); err != nil {
		t.Fatal(err)
	}
	conf := "[Controls]\nStyle=teststyle\n"
	if err := os.WriteFile(filepath.Join(dir, "qtquickcontrols2.conf"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	qmldir := "module teststyle\n"
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(style, name+".qml"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		qmldir += name + " 1.0 " + name + ".qml\n"
	}
	if err := os.WriteFile(filepath.Join(style, "qmldir"), []byte(qmldir), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// loadPage writes a .qml page into dir and loads it.
func loadPage(t *testing.T, dir, src string, env *Env) *Document {
	t.Helper()
	path := filepath.Join(dir, "Main.qml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Load(path, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return d
}

// A style file's declared properties belong to the INSTANCE, not the
// document: two styled controls on a page must not share one slot, or the
// last one built decides what every one of them draws (which is how five
// dials came to paint the same arc).
func TestStyledControlPropertiesArePerInstance(t *testing.T) {
	dial := `
import QtQuick
import QtQuick.Templates as T
T.Dial {
    id: root
    implicitWidth: 100
    implicitHeight: 100
    readonly property real _frac: root.position
    background: Item {
        Text { text: root._frac.toFixed(2); color: "#ffffff" }
    }
}`
	dir := writeStyleModule(t, map[string]string{"Dial": dial})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Row {
    Dial { id: a; from: 0; to: 100; value: 20 }
    Dial { id: b; from: 0; to: 100; value: 80 }
}`, env)
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	// Each control's own scope object holds its own derived value.
	for _, tc := range []struct{ id, want string }{{"a", "0.2"}, {"b", "0.8"}} {
		obj, ok := env.vars[tc.id].(*elemObject)
		if !ok {
			t.Fatalf("%s is %T, want the control's scope object", tc.id, env.vars[tc.id])
		}
		v, _ := obj.member("position")
		if got := str(v); got != tc.want {
			t.Errorf("%s.position = %q, want %q", tc.id, got, tc.want)
		}
	}
	// And the per-instance property really is separate storage.
	if a, b := d.doc.props["tpl:Dial:a/_frac"], d.doc.props["tpl:Dial:b/_frac"]; a == nil || b == nil {
		t.Fatalf("declared style properties are not per instance: %v %v", a, b)
	} else if str(a.get()) == str(b.get()) {
		t.Errorf("both instances hold _frac = %s", str(a.get()))
	}
}

// A styled SpinBox steps its value when its up/down indicators are
// clicked: the style draws two rectangles, and the control has to make them
// buttons.
func TestStyledSpinBoxIndicatorsStep(t *testing.T) {
	spin := `
import QtQuick
import QtQuick.Templates as T
T.SpinBox {
    id: root
    implicitWidth: 120
    implicitHeight: 40
    leftPadding: 40
    rightPadding: 40
    up.indicator: Rectangle {
        x: parent.width - width
        width: 40; height: 40
        color: root.up.pressed ? "#ffffff" : "transparent"
    }
    down.indicator: Rectangle {
        x: 0
        width: 40; height: 40
        color: root.down.pressed ? "#ffffff" : "transparent"
    }
    contentItem: TextInput {
        text: root.textFromValue(root.value, root.locale)
        horizontalAlignment: Qt.AlignHCenter
        color: "#ffffff"
    }
    background: Rectangle { color: "#202020" }
}`
	dir := writeStyleModule(t, map[string]string{"SpinBox": spin})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Item {
    SpinBox { id: sp; from: 0; to: 10; value: 5; stepSize: 2 }
}`, env)
	value := func() string {
		obj := env.vars["sp"].(*elemObject)
		v, _ := obj.member("value")
		return str(v)
	}
	render := func() *ui.UI {
		u := ui.New(nil, nil, d.View())
		u.Layout(300, 100)
		return u
	}
	if got := value(); got != "5" {
		t.Fatalf("initial value = %q, want 5", got)
	}
	// The up indicator is the right 40px of the 120px control.
	if !render().Click(100, 20) {
		t.Fatal("up indicator click not consumed")
	}
	if got := value(); got != "7" {
		t.Errorf("after up click value = %q, want 7 (stepSize 2)", got)
	}
	// The down indicator is the left 40px.
	if !render().Click(10, 20) {
		t.Fatal("down indicator click not consumed")
	}
	if got := value(); got != "5" {
		t.Errorf("after down click value = %q, want 5", got)
	}
	// Stepping stops at the range's ends.
	for i := 0; i < 5; i++ {
		render().Click(100, 20)
	}
	if got := value(); got != "10" {
		t.Errorf("value = %q, want it clamped to 10", got)
	}
}

// A styled Tumbler spins the control's own currentIndex — the one the page
// binds and writes — and renders the delegate the PAGE supplied, in the
// page's scope (both files call their root `root`, so the wrong scope shows
// up immediately).
func TestStyledTumblerUsesPageDelegateAndIndex(t *testing.T) {
	tumbler := `
import QtQuick
import QtQuick.Templates as T
T.Tumbler {
    id: control
    implicitWidth: 80
    implicitHeight: 200
    readonly property real __delegateHeight: availableHeight / visibleItemCount
    delegate: Text { text: "style-" + modelData; color: "#ffffff" }
    background: Rectangle { implicitWidth: 80; implicitHeight: 200; color: "#202020" }
}`
	dir := writeStyleModule(t, map[string]string{"Tumbler": tumbler})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Item {
    id: root
    function label(n) { return "page-" + n }
    Tumbler {
        id: wheel
        model: 12
        visibleItemCount: 3
        currentIndex: 4
        delegate: Text { required property int modelData; text: root.label(modelData); color: "#ffffff" }
    }
}`, env)
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	obj, ok := env.vars["wheel"].(*elemObject)
	if !ok {
		t.Fatalf("wheel is %T, want an element object", env.vars["wheel"])
	}
	v, _ := obj.member("currentIndex")
	if got := str(v); got != "4" {
		t.Errorf("currentIndex = %q, want 4", got)
	}
	// The page's delegate wins over the style's, and runs in the page's
	// scope (`root.label`, where the style's own root is called `control`).
	ref, _ := obj.props["currentIndex"].(Ref)
	if ref == nil {
		t.Fatal("the control's currentIndex is not a writable ref")
	}
	if err := ref.set(7); err != nil {
		t.Fatalf("set: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view after turning the wheel")
	}
	v, _ = obj.member("currentIndex")
	if got := str(v); got != "7" {
		t.Errorf("after set, currentIndex = %q, want 7", got)
	}
}

// new Date(...) takes JS's argument forms — the multi-argument one most of
// all, since `new Date(y, m + 1, 0).getDate()` is how a page counts the days
// in a month.
func TestJSDateConstructorForms(t *testing.T) {
	env := NewEnv()
	if _, err := LoadString(`
Item {
    property string days:  "" + new Date(2026, 9, 0).getDate()
    property string feb:   "" + new Date(2026, 2, 0).getDate()
    property string leap:  "" + new Date(2024, 2, 0).getDate()
    property string month: "" + new Date(2026, 8, 15).getMonth()
    property string year:  "" + new Date(2026, 8, 15).getFullYear()
    property string epoch: "" + new Date(0).getFullYear()
    property string str:   "" + new Date("2026-09-01").getDate()
}`, env); err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	for _, tc := range []struct{ name, want string }{
		{"days", "30"}, // day 0 of October is 30 September
		{"feb", "28"},  // and of March, 28 February
		{"leap", "29"}, // 2024 is a leap year
		{"month", "8"}, // months stay 0-based
		{"year", "2026"},
		{"epoch", "1970"},
		{"str", "1"},
	} {
		if got := str(env.vars[tc.name].(Ref).get()); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Dragging a tumbler spins it: the wheel follows the pointer a row at a
// time from wherever it stood when the press landed, and letting go leaves
// it snapped to an item.
func TestStyledTumblerDragSpins(t *testing.T) {
	tumbler := `
import QtQuick
import QtQuick.Templates as T
T.Tumbler {
    id: control
    implicitWidth: 80
    implicitHeight: 200
    delegate: Text { text: "" + modelData; color: "#ffffff" }
    background: Rectangle { implicitWidth: 80; implicitHeight: 200; color: "#202020" }
}`
	dir := writeStyleModule(t, map[string]string{"Tumbler": tumbler})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Item {
    Tumbler { id: wheel; model: 20; visibleItemCount: 5; wrap: false; currentIndex: 10 }
}`, env)
	obj := func() *elemObject { return env.vars["wheel"].(*elemObject) }
	index := func() string {
		v, _ := obj().member("currentIndex")
		return str(v)
	}
	render := func() *ui.UI {
		u := ui.New(nil, nil, d.View())
		u.Layout(300, 300)
		return u
	}
	render() // settle the geometry the row height is measured from
	if got := index(); got != "10" {
		t.Fatalf("initial currentIndex = %q, want 10", got)
	}
	// The wheel is 200px tall over 5 items: 40px a row. Dragging DOWN two
	// rows brings earlier items into view.
	if !render().Drag(40, 100, 40, 180) {
		t.Fatal("the wheel did not take the drag")
	}
	if got := index(); got != "8" {
		t.Errorf("after dragging down 2 rows, currentIndex = %q, want 8", got)
	}
	// And up three, from there.
	if !render().Drag(40, 180, 40, 60) {
		t.Fatal("the wheel did not take the second drag")
	}
	if got := index(); got != "11" {
		t.Errorf("after dragging up 3 rows, currentIndex = %q, want 11", got)
	}
	// Half a row is not a step: the wheel stays put rather than drifting.
	if !render().Drag(40, 100, 40, 118) {
		t.Fatal("the wheel did not take the third drag")
	}
	if got := index(); got != "11" {
		t.Errorf("after a half-row drag, currentIndex = %q, want 11", got)
	}
	// A wheel that does not wrap stops at its last item, however far the
	// pointer travels.
	if !render().Drag(40, 190, 40, -400) {
		t.Fatal("the wheel did not take the fourth drag")
	}
	if got := index(); got != "19" {
		t.Errorf("after dragging past the end, currentIndex = %q, want 19", got)
	}
}

// Letting go of a spinning wheel carries it on: the flick's velocity spins
// the tumbler well past where the pointer stopped, and it settles on an
// item rather than between two.
func TestStyledTumblerFlingCarriesOn(t *testing.T) {
	tumbler := `
import QtQuick
import QtQuick.Templates as T
T.Tumbler {
    id: control
    implicitWidth: 80
    implicitHeight: 200
    delegate: Text { text: "" + modelData; color: "#ffffff" }
    background: Rectangle { implicitWidth: 80; implicitHeight: 200; color: "#202020" }
}`
	dir := writeStyleModule(t, map[string]string{"Tumbler": tumbler})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Item {
    Tumbler { id: wheel; model: 60; visibleItemCount: 5; wrap: false; currentIndex: 30 }
}`, env)
	index := func() int {
		v, _ := env.vars["wheel"].(*elemObject).member("currentIndex")
		f, err := toFloat32(v)
		if err != nil {
			t.Fatalf("currentIndex is %v", v)
		}
		return int(f)
	}
	render := func(at time.Duration) *ui.UI {
		u := ui.New(nil, nil, d.View())
		u.SetAnimTime(time.Unix(0, 0).Add(at))
		u.Layout(300, 300)
		return u
	}
	render(0)
	// A one-row drag upward, released at 900px/s. 900²/(2*1500) = 270px of
	// coasting on top, which is nearly 7 more rows at 40px each.
	if !render(0).Fling(40, 100, 40, 60, 0, -900) {
		t.Fatal("the wheel did not take the flick")
	}
	if got := index(); got != 31 {
		t.Fatalf("the drag itself moved to %d, want 31", got)
	}
	// Run the clock out: the flick's animation spins the wheel on.
	for at := 10 * time.Millisecond; at < 2*time.Second; at += 10 * time.Millisecond {
		core.TickAnimations(10 * time.Millisecond)
		render(at)
		if !core.AnimationsRunning() {
			break
		}
	}
	if got := index(); got != 38 {
		t.Errorf("the flick settled on %d, want 38 (1 dragged + about 7 coasted)", got)
	}
	if core.AnimationsRunning() {
		t.Error("the wheel is still spinning after two seconds")
	}
}

// Qt.lighter/Qt.darker return a colour QML can read back: an 8-digit hex is
// #AARRGGBB, alpha FIRST. Writing the channels the other way round made
// every lightened colour transparent — a teal's zero red channel became its
// alpha, which is what hid the background of every hovered, pressed and
// highlighted button.
func TestQtLighterKeepsTheColourOpaque(t *testing.T) {
	env := NewEnv()
	if _, err := LoadString(`
Item {
    property string light: "" + Qt.lighter("#00e5b0", 1.15)
    property string dark:  "" + Qt.darker("#00e5b0", 1.2)
    property string half:  "" + Qt.lighter(Qt.rgba(0, 0.9, 0.7, 0.5), 1.1)
}`, env); err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	for _, tc := range []struct {
		name  string
		wantA float32
	}{
		{"light", 1}, {"dark", 1}, {"half", 0.5},
	} {
		got := str(env.vars[tc.name].(Ref).get())
		c, err := parseColor(got)
		if err != nil {
			t.Errorf("%s = %q: %v", tc.name, got, err)
			continue
		}
		if d := c.A - tc.wantA; d > 0.01 || d < -0.01 {
			t.Errorf("%s = %q has alpha %.2f, want %.2f", tc.name, got, c.A, tc.wantA)
		}
		if c.G < 0.3 {
			t.Errorf("%s = %q lost its green channel (%.2f)", tc.name, got, c.G)
		}
	}
}

// Layout.preferredWidth is the size the item gets, not a suggestion the row
// may squeeze: a crowded row overflows, the way Qt's does, rather than
// cutting the labels out of the buttons.
func TestLayoutRowKeepsThePreferredWidth(t *testing.T) {
	btn := `
import QtQuick
import QtQuick.Templates as T
T.Button {
    id: control
    implicitWidth: 120
    implicitHeight: 40
    background: Rectangle { color: "#00e5b0"; radius: 20 }
    contentItem: Text { text: control.text; color: "#ffffff" }
}`
	dir := writeStyleModule(t, map[string]string{"Button": btn})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
Item {
    width: 300; height: 100
    RowLayout {
        width: 300
        spacing: 10
        Button { id: a; text: "One";   Layout.preferredWidth: 130 }
        Button { id: b; text: "Two";   Layout.preferredWidth: 130 }
        Button { id: c; text: "Three"; Layout.preferredWidth: 130 }
    }
}`, env)
	ui.New(nil, nil, d.View()).Layout(400, 200)
	ui.New(nil, nil, d.View()).Layout(400, 200) // geometry is read back a frame later
	for _, id := range []string{"a", "b", "c"} {
		obj, ok := env.vars[id].(*elemObject)
		if !ok {
			t.Fatalf("%s is %T", id, env.vars[id])
		}
		if obj.el == nil {
			t.Fatalf("%s never made it into the tree", id)
		}
		if w := ui.Bounds(obj.el).W; w != 130 {
			t.Errorf("%s is %.0fpx wide, want its preferred 130", id, w)
		}
	}
}

// A styled RangeSlider renders through the style's groove and its two
// handle delegates, each handle carrying its own live value that the page
// reads through the control's id.
func TestStyledRangeSliderHandles(t *testing.T) {
	rs := `
import QtQuick
import QtQuick.Templates as T
T.RangeSlider {
    id: control
    implicitWidth: 200
    implicitHeight: 20
    background: Rectangle {
        color: "#404040"
        Rectangle {
            objectName: "fill"
            x: control.first.visualPosition * parent.width
            width: (control.second.visualPosition - control.first.visualPosition) * parent.width
            height: parent.height
            color: "#00e5b0"
        }
    }
    first.handle: Rectangle { objectName: "lo"; x: control.first.visualPosition * (control.width - 20); width: 20; height: 20; color: "#00e5b0" }
    second.handle: Rectangle { objectName: "hi"; x: control.second.visualPosition * (control.width - 20); width: 20; height: 20; color: "#00e5b0" }
}`
	dir := writeStyleModule(t, map[string]string{"RangeSlider": rs})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Item {
    property string label: slider.first.value.toFixed(0) + "-" + slider.second.value.toFixed(0)
    RangeSlider { id: slider; width: 200; from: 0; to: 100; first.value: 20; second.value: 80 }
}`, env)
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	// The page reads the handles through its own id, from the first build.
	if got := str(env.vars["label"].(Ref).get()); got != "20-80" {
		t.Errorf("label = %q, want \"20-80\"", got)
	}
	obj, ok := env.vars["slider"].(*elemObject)
	if !ok {
		t.Fatalf("slider is %T", env.vars["slider"])
	}
	// And the style sees each handle's position on the control's range.
	for _, tc := range []struct {
		side string
		want float64
	}{{"first", 0.2}, {"second", 0.8}} {
		v, _ := obj.member(tc.side)
		part, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%s is %T, want the handle sub-object", tc.side, v)
		}
		got, _ := toFloat64(part["visualPosition"])
		if got < tc.want-0.01 || got > tc.want+0.01 {
			t.Errorf("%s.visualPosition = %v, want %v", tc.side, got, tc.want)
		}
	}
	// The handle values are writable state, so a drag sticks.
	part, _ := obj.props["first"].(map[string]any)
	ref, _ := part["value"].(Ref)
	if ref == nil {
		t.Fatal("first.value is not a writable ref")
	}
	if err := ref.set(35); err != nil {
		t.Fatalf("set: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view after moving a handle")
	}
	if got := str(env.vars["label"].(Ref).get()); got != "35-80" {
		t.Errorf("after moving the low handle, label = %q, want \"35-80\"", got)
	}
}

// A control's interaction state is ONE state: the page binds against
// `btn.down` through the object its own id names, and the styled control
// has to move that ref rather than one of its own. Forked, a button held
// down never reported itself pressed — the corpus' button whose label
// reads `pressBtn.down ? "Pressed!" : "Press Me"` stayed on "Press Me"
// however long it was held, and no static screenshot could show it.
func TestStyledButtonReportsItsPressedState(t *testing.T) {
	button := `
import QtQuick
import QtQuick.Templates as T
T.Button {
    id: root
    implicitWidth: 120; implicitHeight: 40
    background: Rectangle { color: root.down ? "#ff0000" : "#00ff00" }
    contentItem: Text { text: root.text; color: "#ffffff" }
}`
	dir := writeStyleModule(t, map[string]string{"Button": button})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Item {
    width: 300; height: 100
    Button { id: pressBtn; x: 0; y: 0; text: pressBtn.down ? "Pressed!" : "Press Me" }
    Text { id: state; y: 60; text: "down: " + pressBtn.down; color: "#ffffff" }
}`, env)
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(300, 100)
	says := func(want string) bool {
		var found bool
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if s, ok := ui.TextOf(e); ok && s == want {
				found = true
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(tree)
		return found
	}
	if !says("Press Me") || !says("down: false") {
		t.Fatal("the button did not build in its resting state")
	}
	// Hold it: the label and the sibling binding both follow.
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseMove, X: 40, Y: 20},
		{Kind: app.MouseDown, X: 40, Y: 20, Button: 1},
	})
	u.Layout(300, 100) // the press state is reported as the tree lays out
	u.Layout(300, 100)
	if !says("Pressed!") {
		t.Error("a held button did not report itself pressed to its own label")
	}
	if !says("down: true") {
		t.Error("a held button did not report itself pressed to a sibling binding")
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: 40, Y: 20, Button: 1}})
	u.Layout(300, 100)
	u.Layout(300, 100)
	if !says("Press Me") {
		t.Error("the button stayed pressed after the release")
	}
}

// An app with its own Controls style draws its DIALOGS through it too: the
// style states the panel (background, header, footer) and the page states
// the content and the buttons. Unshadowed, every dialog in the corpus drew
// our own chrome — a light card and a Basic button box — over the app's
// dark page, which is a third of the window wrong.
func TestStyledDialogUsesItsStylePanel(t *testing.T) {
	dialog := `
import QtQuick
import QtQuick.Controls
import QtQuick.Templates as T
T.Dialog {
    id: root
    implicitWidth: 400
    padding: 20
    topPadding: 10
    modal: true
    background: Rectangle { color: "#2a2d35"; radius: 12 }
    header: Rectangle {
        color: "transparent"
        implicitHeight: 48
        Label { anchors.verticalCenter: parent.verticalCenter; anchors.left: parent.left
                anchors.leftMargin: 20; text: root.title; color: "#00d1a9"; font.bold: true }
    }
    footer: DialogButtonBox { visible: count > 0 }
    Overlay.modal: Rectangle { color: "#66000000" }
}`
	dir := writeStyleModule(t, map[string]string{"Dialog": dialog})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Item {
    width: 700; height: 500
    property string outcome: "none"
    Button { id: b; x: 10; y: 10; text: "Open"; onClicked: dlg.open() }
    Dialog {
        id: dlg
        title: "Confirm Action"
        standardButtons: Dialog.Yes | Dialog.No
        Label { text: "Are you sure?"; color: "#c8c8d0" }
        onAccepted: outcome = "accepted"
        onRejected: outcome = "rejected"
    }
}`, env)
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(700, 500)
	find := func() (panel ui.Rect, fills map[uint32]bool, texts map[string]ui.Rect) {
		fills, texts = map[uint32]bool{}, map[string]ui.Rect{}
		var walk func(ui.Element, bool)
		walk = func(e ui.Element, inModal bool) {
			if e == nil {
				return
			}
			if _, ok := e.(*ui.ModalEl); ok {
				inModal = true
			}
			if inModal {
				if c, ok := ui.FillColor(e); ok && c.A > 0 {
					hex := uint32(c.R*255+0.5)<<16 | uint32(c.G*255+0.5)<<8 | uint32(c.B*255+0.5)
					fills[hex] = true
					if hex == 0x2a2d35 {
						panel = ui.Bounds(e)
					}
				}
				if s, ok := ui.TextOf(e); ok {
					texts[s] = ui.Bounds(e)
				}
			}
			for _, ch := range ui.Children(e) {
				walk(ch, inModal)
			}
		}
		walk(tree, false)
		return
	}
	if _, _, texts := find(); len(texts) != 0 {
		t.Fatalf("the dialog is showing before anything opened it: %v", texts)
	}
	if !u.Click(20, 20) {
		t.Fatal("the Open button was not clicked")
	}
	u.Layout(700, 500)
	panel, fills, texts := find()
	// The STYLE's card, at the width the style states.
	if !fills[0x2a2d35] {
		t.Errorf("the style's background never drew (fills seen: %v)", fills)
	}
	if panel.W != 400 {
		t.Errorf("the dialog is %v wide, want the style's 400", panel.W)
	}
	// Its own header (the title, in the style's accent) and the page's
	// content and buttons.
	for _, want := range []string{"Confirm Action", "Are you sure?", "Yes", "No"} {
		if _, ok := texts[want]; !ok {
			t.Errorf("the open dialog does not show %q (has %v)", want, texts)
		}
	}
	if title, content := texts["Confirm Action"], texts["Are you sure?"]; title.Y >= content.Y {
		t.Error("the header is not above the content")
	}
	if content, yes := texts["Are you sure?"], texts["Yes"]; content.Y >= yes.Y {
		t.Error("the button box is not below the content")
	}
	// The buttons trail, as Qt's DialogButtonBox does (alignment: AlignRight).
	if yes, no := texts["Yes"], texts["No"]; yes.X >= no.X || no.X < panel.X+panel.W/2 {
		t.Errorf("buttons at %v and %v, want them trailing in the box", yes.X, no.X)
	}
	// And a button still reports what the user chose.
	yes := texts["Yes"]
	if !u.Click(yes.X, yes.Y) {
		t.Fatalf("the Yes button at %v was not clickable", yes)
	}
	u.Layout(700, 500)
	if got := str(d.doc.props["outcome"].get()); got != "accepted" {
		t.Errorf("outcome = %q after clicking Yes, want accepted", got)
	}
}

// Menus draw through the app's style too: the Menu style states the panel,
// the MenuItem style states each row (its own background, label and
// checkable indicator), and choosing a row runs its handler and closes the
// menu. A Menu opened with popup() and no coordinates opens at its
// trigger.
func TestStyledMenuUsesItsStyle(t *testing.T) {
	menu := `
import QtQuick
import QtQuick.Templates as T
T.Menu {
    id: root
    padding: 4
    background: Rectangle { implicitWidth: 180; color: "#1b1e24"; radius: 8 }
}`
	item := `
import QtQuick
import QtQuick.Templates as T
T.MenuItem {
    id: root
    implicitWidth: 180
    implicitHeight: 36
    padding: 8
    background: Rectangle { color: root.highlighted ? "#2a2d35" : "transparent" }
    contentItem: Text { text: root.text; color: "#d8d8e0"; font.pixelSize: 14 }
}`
	dir := writeStyleModule(t, map[string]string{"Menu": menu, "MenuItem": item})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Item {
    width: 400; height: 300
    property string picked: "none"
    Button { id: trigger; x: 40; y: 40; width: 120; height: 36; text: "Open"
             onClicked: viewMenu.popup() }
    Menu {
        id: viewMenu
        MenuItem { text: "First"; onTriggered: picked = "first" }
        MenuItem { text: "Second"; onTriggered: picked = "second" }
    }
}`, env)
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(400, 300)
	rows := func() map[string]ui.Rect {
		out := map[string]ui.Rect{}
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if s, ok := ui.TextOf(e); ok {
				out[s] = ui.Bounds(e)
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(tree)
		return out
	}
	if _, open := rows()["First"]; open {
		t.Fatal("the menu is showing before anything opened it")
	}
	if !u.Click(60, 55) {
		t.Fatal("the trigger was not clicked")
	}
	u.Layout(400, 300)
	items := rows()
	first, ok := items["First"]
	if !ok {
		t.Fatalf("popup() opened no menu (texts: %v)", items)
	}
	if _, ok := items["Second"]; !ok {
		t.Error("the menu is missing its second item")
	}
	// At its trigger: below the button that opened it, not centred in the
	// window and not at the origin.
	if first.Y < 70 || first.X < 30 {
		t.Errorf("the menu opened at %v,%v, want it under the trigger (40,76)", first.X, first.Y)
	}
	// Choosing a row runs its handler AND closes the menu.
	if !u.Click(first.X+4, first.Y+2) {
		t.Fatalf("the first item at %v was not clickable", first)
	}
	u.Layout(400, 300)
	if got := str(d.doc.props["picked"].get()); got != "first" {
		t.Errorf("picked = %q, want first", got)
	}
	if _, open := rows()["First"]; open {
		t.Error("the menu stayed open after a choice")
	}
}

// A use site's implicitWidth beats the STYLE's: a page that states
// `implicitWidth: 34` on a transport button gets 34, where the style file's
// own 140 would draw it as a pill. An explicit `width` still wins over
// both, as in QML.
func TestUseSiteImplicitWidthBeatsTheStyle(t *testing.T) {
	button := `
import QtQuick
import QtQuick.Templates as T
T.Button {
    id: root
    implicitWidth: 140
    implicitHeight: 40
    background: Rectangle { color: "#303030" }
}`
	dir := writeStyleModule(t, map[string]string{"Button": button})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Row {
    Button { id: styled; text: "A" }
    Button { id: narrow; text: "B"; implicitWidth: 34 }
    Button { id: fixed;  text: "C"; implicitWidth: 34; width: 90 }
}`, env)
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(600, 200)
	kids := ui.Children(tree)
	if len(kids) != 3 {
		t.Fatalf("built %d children, want 3", len(kids))
	}
	for i, want := range []float32{140, 34, 90} {
		if got := ui.Bounds(kids[i]).W; got != want {
			t.Errorf("button %d width = %v, want %v", i, got, want)
		}
	}
}

// A use-site attribute may read the control's OWN state: `highlighted:
// checked` on a checkable Button is the QML idiom for "look active while I
// am checked" (item scope, where a bare name resolves against the item's
// own properties first). With nothing bound for `checked` there, every such
// button drew unhighlighted — the particles page's three affector buttons
// all looked selected because none of them looked active.
func TestUseSiteAttrReadsTheControlsOwnState(t *testing.T) {
	button := `
import QtQuick
import QtQuick.Templates as T
T.Button {
    id: root
    implicitWidth: 100
    implicitHeight: 30
    background: Rectangle {
        color: root.highlighted ? "#00d1a9" : "#303030"
    }
}`
	dir := writeStyleModule(t, map[string]string{"Button": button})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Row {
    Button { id: on;  text: "A"; checkable: true; checked: true;  highlighted: checked }
    Button { id: off; text: "B"; checkable: true; checked: false; highlighted: checked }
}`, env)
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(400, 100)
	fills := map[string]ui.RGBA{}
	for i, kid := range ui.Children(tree) {
		var found ui.RGBA
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if c, ok := ui.FillColor(e); ok && c.A > 0 && found.A == 0 {
				found = c
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(kid)
		fills[[]string{"on", "off"}[i]] = found
	}
	if got := fills["on"]; got.G < 0.5 || got.R > 0.2 {
		t.Errorf("the checked button's background is %+v, want the highlighted teal", got)
	}
	if got := fills["off"]; got.G > 0.4 {
		t.Errorf("the unchecked button's background is %+v, want the plain grey", got)
	}
}

// TestContentPaddingIsPerSide: a styled control's contentItem carries its
// own padding per side — a ComboBox's text is `leftPadding: 12;
// rightPadding: 30` (room for the arrow). Taking the larger of the two for
// both sides indented the text by 30 where Qt starts it at 12, which is
// exactly what the graphics3d panel showed.
func TestContentPaddingIsPerSide(t *testing.T) {
	dir := writeStyleModule(t, map[string]string{"ComboBox": `
import QtQuick
import QtQuick.Templates as T
T.ComboBox {
    id: root
    implicitWidth: 200
    implicitHeight: 40
    contentItem: Text {
        leftPadding: 12
        rightPadding: 30
        text: root.displayText
        color: "#ffffff"
        verticalAlignment: Text.AlignVCenter
    }
    background: Rectangle { color: "#202020"; radius: 8 }
}`})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
Rectangle {
    width: 300; height: 100
    ComboBox {
        id: combo
        x: 0; y: 0
        model: ["#Cube", "#Sphere"]
    }
}`, env)
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(300, 100)
	u.Layout(300, 100)
	// The text sits 12px in from the control's left edge, not 30.
	var text ui.Element
	var walk func(ui.Element)
	walk = func(e ui.Element) {
		if e == nil || text != nil {
			return
		}
		if _, ok := e.(*ui.TextEl); ok {
			text = e
			return
		}
		for _, c := range ui.Children(e) {
			walk(c)
		}
	}
	walk(tree)
	if text == nil {
		t.Fatal("the combo's text did not build")
	}
	if x := ui.Bounds(text).X; x < 8 || x > 18 {
		t.Errorf("the content starts at x=%v, want ~12 (its leftPadding)", x)
	}
}
