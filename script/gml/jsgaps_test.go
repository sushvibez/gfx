package gml

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// Gaps found while verifying docs/gml.md: spread in call arguments, sort
// with a comparator, mutators on a temporary receiver, and Qt's
// translation functions.
func TestJSDocGaps(t *testing.T) {
	env := NewEnv()
	env.Set("xs", []any{float64(3), float64(9), float64(4)})
	env.Set("words", []any{"pear", "fig", "apple"})
	for _, c := range []struct{ src, want string }{
		{`Math.max(...xs)`, "9"},
		{`Math.min(1, ...xs, 0)`, "0"},
		{`Math.max(...[2, 5], ...xs)`, "9"},
		{`[3, 1, 2].sort((a, b) => a - b).join(",")`, "1,2,3"},
		{`[3, 1, 2].sort((a, b) => b - a).join(",")`, "3,2,1"},
		{`xs.slice().sort((a, b) => a - b).join(",")`, "3,4,9"},
		{`xs.join(",")`, "3,9,4"}, // the slice was sorted, not xs
		{`words.slice().sort().join(",")`, "apple,fig,pear"},
		{`words.slice().reverse()[0]`, "apple"},
		{`[10, 9, 1].sort().join(",")`, "1,10,9"}, // no comparator: string order
		{`qsTr("Hello")`, "Hello"},
		{`qsTr("%n files", "", 3)`, "3 files"},
		{`qsTranslate("Main", "Quit")`, "Quit"},
		{`qsTrId("id-save")`, "id-save"},
		{`QT_TR_NOOP("Later")`, "Later"},
	} {
		if got := evalStr(t, env, c.src); got != c.want {
			t.Errorf("%s = %q, want %q", c.src, got, c.want)
		}
	}
	// A comparator also sorts in place through a variable.
	evalStr(t, env, `xs.sort((a, b) => a - b)`)
	if got := evalStr(t, env, `xs.join(",")`); got != "3,4,9" {
		t.Errorf("xs after an in-place sort = %q, want 3,4,9", got)
	}
}

// clickRun loads src (a Column with `property string out` and a Button
// whose onClicked is the code under test), clicks the button and returns
// out.
func clickRun(t *testing.T, handler string) string {
	t.Helper()
	src := "Column {\n    property string out: \"\"\n    Button { text: \"go\"; onClicked: {\n" + handler + "\n    } }\n}"
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	if !u.Click(50, 20) {
		t.Fatal("click not consumed")
	}
	return str(env.vars["out"].(Ref).get())
}

// The statement forms docs/gml.md lists that had no test.
func TestJSStatementsDocumented(t *testing.T) {
	for _, c := range []struct{ name, code, want string }{
		{"optional call on null", `var o = null; out = "" + (o?.f() === undefined)`, "true"},
		{"optional call on object", `var o = { f: function() { return "hi" } }; out = o?.f()`, "hi"},
		{"for of string", `for (var c of "abc") out += c + "."`, "a.b.c."},
		{"switch fall-through", `switch (2) { case 1: out += "1"; case 2: out += "2"; case 3: out += "3"; break; case 4: out += "4" }`, "23"},
		{"switch default", `switch ("x") { case "y": out = "y"; break; default: out = "d" }`, "d"},
		{"throw an object", `try { throw { code: 7, msg: "bad" } } catch (e) { out = e.msg + e.code }`, "bad7"},
		{"throw a string", `try { throw "oops" } catch (e) { out = e }`, "oops"},
		{"finally runs", `try { out = "t" } finally { out += "f" }`, "tf"},
	} {
		if got := clickRun(t, c.code); got != c.want {
			t.Errorf("%s: out = %q, want %q", c.name, got, c.want)
		}
	}
}

// Qt.callLater runs after the handler, in the order the calls were made.
func TestCallLaterOrder(t *testing.T) {
	src := `Column {
    property string out: ""
    Button { text: "go"; onClicked: {
        Qt.callLater(function() { out += "1" })
        Qt.callLater(function(s) { out += s }, "2")
        out += "0"
    } }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	u.Click(50, 20)
	if got := str(env.vars["out"].(Ref).get()); got != "0" {
		t.Errorf("before the next frame out = %q, want 0 (callLater must wait)", got)
	}
	drainPosted()
	if got := str(env.vars["out"].(Ref).get()); got != "012" {
		t.Errorf("after the next frame out = %q, want 012", got)
	}
}

// A Loader source resolves against the directory of the file it is
// written in: main loads sub/Page.qml, whose own Loader names Inner.qml,
// which is sub/Inner.qml. The main markup comes through LoadStringAt, the
// way a program embeds its root page next to the files it loads.
func TestLoaderSourceFromSubdirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sub/Page.qml", `import QtQuick
Rectangle { width: 90; height: 30; color: "#ff0000"
    Loader { y: 40; source: "Inner.qml" }
}`)
	write("sub/Inner.qml", `import QtQuick
Rectangle { width: 70; height: 25; color: "#0000ff" }`)
	main := `import QtQuick
Rectangle { width: 300; height: 200
    Loader { source: "sub/Page.qml" }
}`
	d, err := LoadStringAt(main, dir, NewEnv())
	if err != nil {
		t.Fatalf("LoadStringAt: %v", err)
	}
	ui.New(nil, nil, d.View()).Layout(300, 200)
	ui.New(nil, nil, d.View()).Layout(300, 200)
	sizes := map[[2]float32]bool{}
	for _, el := range d.doc.prevElems.v {
		if el != nil {
			b := ui.Bounds(el)
			sizes[[2]float32{b.W, b.H}] = true
		}
	}
	for _, want := range [][2]float32{{90, 30}, {70, 25}} {
		if !sizes[want] {
			t.Errorf("no %vx%v element: a Loader source did not resolve", want[0], want[1])
		}
	}
}

// Connections reach a signal declared in markup, with its arguments, in
// both the Qt 6 (`function onMoved(dx)`) and the Qt 5 (`onMoved: …`,
// which sees the signal's parameter names) forms; and the Qt 5 form works
// for an element's own signals such as a Button's onClicked.
func TestConnectionsFormsAndArguments(t *testing.T) {
	src := `Column {
    property string out: ""
    Item { id: knob; signal moved(int dx, string why); onMoved: out += "own" + dx + why + ";" }
    Connections { target: knob; function onMoved(step, reason) { out += "qt6" + step + reason + ";" } }
    Button { id: btn; text: "go"; onClicked: knob.moved(3, "x") }
    Connections { target: btn; onClicked: out += "click;" }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	if !u.Click(50, 20) {
		t.Fatal("click not consumed")
	}
	if got := str(env.vars["out"].(Ref).get()); got != "own3x;qt63x;click;" {
		t.Errorf("out = %q, want own3x;qt63x;click;", got)
	}

	// The Qt 5 form on a declared signal sees the signal's own names.
	src5 := `Column {
    property string out: ""
    Item { id: knob; signal moved(int dx) }
    Connections { target: knob; onMoved: out = "qt5:" + dx }
    Button { text: "go"; onClicked: knob.moved(7) }
}`
	env5 := NewEnv()
	d5, err := LoadString(src5, env5)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u5 := ui.New(nil, nil, d5.View())
	u5.Layout(300, 200)
	u5.Click(50, 20)
	if got := str(env5.vars["out"].(Ref).get()); got != "qt5:7" {
		t.Errorf("Qt 5 form: out = %q, want qt5:7", got)
	}
}

// Keys handlers see the fuller key table and the modifiers, and only a
// handler that sets event.accepted keeps the key.
func TestKeysEventModifiersAccepted(t *testing.T) {
	env := NewEnv()
	for _, c := range []struct{ src, want string }{
		{`Qt.Key_A`, "65"},
		{`Qt.Key_7`, "55"},
		{`Qt.Key_F5 - Qt.Key_F1`, "4"},
		{`Qt.Key_PageDown`, "16777239"},
		{`Qt.ControlModifier`, "67108864"},
	} {
		if got := evalStr(t, env, c.src); got != c.want {
			t.Errorf("%s = %s, want %s", c.src, got, c.want)
		}
	}
	var out string
	env.Func("note", func(args []any) (any, error) { out += str(args[0]) + ";"; return nil, nil })
	src := `Item { Keys.onPressed: {
        note(event.key + "/" + ((event.modifiers & Qt.ControlModifier) ? "ctrl" : "-"))
        if (event.key === Qt.Key_S) event.accepted = true
    } }`
	root, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	var body []stmt
	for _, a := range root.attrs {
		if a.name == "Keys.onPressed" {
			body = a.stmt
		}
	}
	kh := keyHandler(body, nil, env)
	if took := kh(app.KeyEvent{Down: true, Rune: 's', Ctrl: true}); !took {
		t.Error("Ctrl+S was set accepted but the handler reported it untaken")
	}
	if took := kh(app.KeyEvent{Down: true, Key: app.KeyF2}); took {
		t.Error("F2 was left unaccepted but the handler took it")
	}
	if out != "83/ctrl;16777265/-;" {
		t.Errorf("handler saw %q, want 83/ctrl;16777265/-;", out)
	}
}

// A key-specific handler starts accepted; the generic one then does not
// run, unless the specific one un-accepts the event.
func TestKeysSpecificHandlers(t *testing.T) {
	src := `Item {
    property string out: ""
    focus: true
    Keys.onLeftPressed: out += "L;"
    Keys.onRightPressed: { out += "R;"; event.accepted = false }
    Keys.onPressed: out += "any;"
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 100)
	if !u.Key(app.KeyEvent{Down: true, Key: app.KeyLeft}) {
		t.Error("Left was not consumed by Keys.onLeftPressed")
	}
	u.Key(app.KeyEvent{Down: true, Key: app.KeyRight})
	if got := str(env.vars["out"].(Ref).get()); got != "L;R;any;" {
		t.Errorf("out = %q, want L;R;any;", got)
	}
}

// Regex literals are RegExp values: test/exec, the properties, and the
// String methods that take one; a validator still reads the pattern.
func TestRegExpValues(t *testing.T) {
	env := NewEnv()
	for _, c := range []struct{ src, want string }{
		{`/ab+c/.test("xabbbcx")`, "true"},
		{`/^ab+c$/.test("xabbbcx")`, "false"},
		{`/AB/i.test("ab")`, "true"},
		{`/(\d+)-(\d+)/.exec("on 12-34 ok")[2]`, "34"},
		{`/x/.exec("abc") === null`, "true"},
		{`/a.c/g.source + "|" + /a/g.flags + "|" + /a/g.global`, "a.c|g|true"},
		{`"a1b22c333".replace(/\d+/, "#")`, "a#b22c333"},
		{`"a1b22c333".replace(/\d+/g, "#")`, "a#b#c#"},
		{`"a1b22".replaceAll(/\d/g, "")`, "ab"},
		{`"john smith".replace(/(\w+) (\w+)/, "$2, $1")`, "smith, john"},
		{`"a-b_c".replace(/[-_]/g, function(m) { return m === "-" ? "+" : "=" })`, "a+b=c"},
		{`"x1y2z3".match(/\d/g).join("")`, "123"},
		{`"x1y2".match(/(\d)/)[1]`, "1"},
		{`"hello".search(/l+/)`, "2"},
		{`"a, b,c".split(/,\s*/).join("|")`, "a|b|c"},
		{`RegExp("b+", "g").test("abbc")`, "true"},
		{`"plain".replace("a", "A")`, "plAin"}, // string patterns unchanged
		{`10 / 2 / 5`, "1"},                    // division still divides
	} {
		if got := evalStr(t, env, c.src); got != c.want {
			t.Errorf("%s = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestQtLocale(t *testing.T) {
	env := NewEnv()
	for _, c := range []struct{ src, want string }{
		{`Qt.locale().decimalPoint`, "."},
		{`Qt.locale("de_DE").decimalPoint + Qt.locale("de_DE").groupSeparator`, ",."},
		{`Qt.locale("fr-FR").currencySymbol`, "€"},
		{`Qt.locale("en_GB").currencySymbol`, "£"},
		{`Qt.locale("xx_YY").currencySymbol`, "$"},
	} {
		if got := evalStr(t, env, c.src); got != c.want {
			t.Errorf("%s = %q, want %q", c.src, got, c.want)
		}
	}
}

// orientation: ListView.Horizontal lays the delegates out side by side.
func TestListViewHorizontal(t *testing.T) {
	src := `Item {
    width: 300; height: 100
    ListView {
        width: 300; height: 60
        orientation: ListView.Horizontal
        spacing: 10
        model: 5
        delegate: Rectangle { width: 80; height: 50; color: "#336699" }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	ui.New(nil, nil, d.View()).Layout(300, 100)
	ui.New(nil, nil, d.View()).Layout(300, 100)
	var xs []float32
	for _, el := range d.doc.prevElems.v {
		if el == nil {
			continue
		}
		if b := ui.Bounds(el); b.W == 80 && b.H == 50 {
			xs = append(xs, b.X)
		}
	}
	if len(xs) != 5 {
		t.Fatalf("found %d delegates of 80x50, want 5", len(xs))
	}
	slices.Sort(xs)
	for i := 1; i < len(xs); i++ {
		if xs[i]-xs[i-1] != 90 {
			t.Errorf("delegates at %v: want a 90px pitch along x (80 wide + 10 spacing)", xs)
			break
		}
	}
	if evalStr(t, NewEnv(), `ListView.Horizontal === Qt.Horizontal`) != "true" {
		t.Error("ListView.Horizontal is not Qt.Horizontal")
	}
}

// A child given at a file component's use site, anchored to `parent`, is
// centred in the component instance (its parent in QML), not elsewhere.
func TestUseSiteChildCentredInComponent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Card.qml"), []byte(`import QtQuick
Rectangle { width: 200; height: 100; color: "#336699" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	main := `import QtQuick
Item { width: 400; height: 300
    Card { x: 50; y: 40
        Rectangle { anchors.centerIn: parent; width: 20; height: 10; color: "#ff0000" }
    }
}`
	d, err := LoadStringAt(main, dir, NewEnv())
	if err != nil {
		t.Fatalf("LoadStringAt: %v", err)
	}
	ui.New(nil, nil, d.View()).Layout(400, 300)
	ui.New(nil, nil, d.View()).Layout(400, 300)
	var card, dot ui.Rect
	for _, el := range d.doc.prevElems.v {
		if el == nil {
			continue
		}
		b := ui.Bounds(el)
		switch {
		case b.W == 200 && b.H == 100:
			card = b
		case b.W == 20 && b.H == 10:
			dot = b
		}
	}
	if card.W == 0 || dot.W == 0 {
		t.Fatalf("card %+v dot %+v: not both laid out", card, dot)
	}
	if cx, cy := dot.X+10, dot.Y+5; cx != card.X+100 || cy != card.Y+50 {
		t.Errorf("dot centre %v,%v, want the card's centre %v,%v (card %+v)", cx, cy, card.X+100, card.Y+50, card)
	}
}

// The same through a component that forwards its children into an inner
// item (default property alias): `parent` is that inner item.
func TestUseSiteChildCentredInAliasedContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Panel.qml"), []byte(`import QtQuick
Rectangle { width: 240; height: 140; color: "#222222"
    default property alias content: inner.data
    Item { id: inner; x: 20; y: 20; width: 200; height: 100 }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	main := `import QtQuick
Item { width: 400; height: 300
    Panel { x: 50; y: 40
        Rectangle { anchors.centerIn: parent; width: 20; height: 10; color: "#ff0000" }
    }
}`
	d, err := LoadStringAt(main, dir, NewEnv())
	if err != nil {
		t.Fatalf("LoadStringAt: %v", err)
	}
	ui.New(nil, nil, d.View()).Layout(400, 300)
	ui.New(nil, nil, d.View()).Layout(400, 300)
	var inner, dot ui.Rect
	for _, el := range d.doc.prevElems.v {
		if el == nil {
			continue
		}
		b := ui.Bounds(el)
		switch {
		case b.W == 200 && b.H == 100:
			inner = b
		case b.W == 20 && b.H == 10:
			dot = b
		}
	}
	if inner.W == 0 || dot.W == 0 {
		t.Fatalf("inner %+v dot %+v: not both laid out", inner, dot)
	}
	if cx, cy := dot.X+10, dot.Y+5; cx != inner.X+100 || cy != inner.Y+50 {
		t.Errorf("dot centre %v,%v, want the inner item's centre %v,%v", cx, cy, inner.X+100, inner.Y+50)
	}
}
