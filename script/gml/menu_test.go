package gml

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// A MenuBar title opens its menu, and picking an entry runs the action and
// closes the menu again.
func TestMenuBarOpensAndTriggers(t *testing.T) {
	src := `
Column {
    property string last: "none"
    MenuBar {
        Menu {
            title: "File"
            Action { text: "New"; onTriggered: last = "File > New" }
            MenuSeparator {}
            Action { text: "Exit"; onTriggered: last = "File > Exit" }
        }
        Menu { title: "Help" }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	render := func() *ui.UI {
		u := ui.New(nil, nil, d.View())
		u.Layout(400, 300)
		return u
	}
	// Text measures as zero-width without a font, so a title's box is only
	// its padding — click inside that.
	// Closed: no entries anywhere.
	if u := render(); u.Click(6, 60) {
		t.Fatal("a closed menu consumed a click below the bar")
	}
	// Click the "File" title, then the first entry.
	if !render().Click(6, 17) {
		t.Fatal("menu title click not consumed")
	}
	if !render().Click(6, 45) {
		t.Fatal("menu entry click not consumed")
	}
	if got := str(env.vars["last"].(Ref).get()); got != "File > New" {
		t.Fatalf("last = %q, want %q", got, "File > New")
	}
	// The pick closed the menu: that spot is empty again.
	if render().Click(6, 45) {
		t.Fatal("menu stayed open after picking an entry")
	}
}

// A MenuBar has no style file in the corpus, so Qt renders it in the
// BASIC style — and Basic's metrics are its own, not the application
// font's: measured from Qt, the bar and its items are 40 tall, a title is
// inset 12 left and 16 right, and the labels are 12px. Ours was 34 tall
// with 10/6 padding, so every menu bar on the page sat 6px short and its
// titles were spaced wrong.
func TestMenuBarBasicMetrics(t *testing.T) {
	src := `
Rectangle {
    width: 300; height: 120
    MenuBar {
        id: bar
        width: 280
        Menu { title: "File"; MenuItem { text: "New" } }
        Menu { title: "Edit"; MenuItem { text: "Copy" } }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 120)
	u.Layout(300, 120)
	el := d.doc.elems.get(d.doc.intern("bar"))
	if el == nil {
		t.Fatal("the menu bar did not build")
	}
	if got := ui.Bounds(el).H; got != basicMenuBarHeight {
		t.Errorf("bar is %v tall, want %v (Qt's Basic style)", got, basicMenuBarHeight)
	}
	// Each title's box is its text plus 28px of padding, which is what
	// decides where the next one starts and where its menu drops.
	kids := ui.Children(el)
	if len(kids) < 2 {
		t.Fatalf("%d titles built, want 2", len(kids))
	}
	for i, k := range kids[:2] {
		if got := ui.Bounds(k).H; got != basicMenuBarHeight {
			t.Errorf("title %d is %v tall, want %v", i, got, basicMenuBarHeight)
		}
	}
}

// An open menu drops UNDER the title that opened it, and closes when the
// user clicks elsewhere or presses Escape. Positioned from the title's
// offset within the bar — which an overlay reads as window coordinates —
// every menu opened at the window's top-left corner instead; and with no
// dismiss scrim, the only way to close one was to pick something.
func TestMenuBarMenuDropsUnderItsTitleAndDismisses(t *testing.T) {
	src := `
Rectangle {
    width: 400; height: 300
    property string last: "none"
    Item {
        x: 60; y: 50; width: 300; height: 200
        MenuBar {
            width: 300
            Menu {
                title: "File"
                Action { text: "New"; onTriggered: last = "File > New" }
                Action { text: "Exit"; onTriggered: last = "File > Exit" }
            }
            Menu { title: "Help" }
        }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(400, 300)
	// The bar sits at (60, 50) and is 40 tall, so its first title is there.
	if !u.Click(66, 60) {
		t.Fatal("the File title was not clickable")
	}
	u.Layout(400, 300)
	entry := ui.Rect{}
	var walk func(ui.Element)
	walk = func(e ui.Element) {
		if e == nil {
			return
		}
		if s, ok := ui.TextOf(e); ok && s == "New" {
			entry = ui.Bounds(e)
		}
		for _, ch := range ui.Children(e) {
			walk(ch)
		}
	}
	walk(tree)
	if entry == (ui.Rect{}) {
		t.Fatal("the menu did not open")
	}
	// Under the bar, and left-aligned with the title — not at the window's
	// corner.
	if entry.Y < 90 {
		t.Errorf("the menu opened at y=%v, above the bar's bottom (90)", entry.Y)
	}
	if entry.X < 60 {
		t.Errorf("the menu opened at x=%v, left of its title (60)", entry.X)
	}
	// Escape closes it (through the overlay's dismiss scrim).
	if !u.Key(app.KeyEvent{Down: true, Key: app.KeyEscape}) {
		t.Fatal("Escape not consumed by the open menu")
	}
	u.Layout(400, 300)
	entry = ui.Rect{}
	walk(tree)
	if entry != (ui.Rect{}) {
		t.Error("Escape did not close the menu")
	}
	// So does a click somewhere else.
	u.Click(66, 60)
	u.Layout(400, 300)
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 350, Y: 280, Button: 1},
		{Kind: app.MouseUp, X: 350, Y: 280, Button: 1},
	})
	u.Layout(400, 300)
	entry = ui.Rect{}
	walk(tree)
	if entry != (ui.Rect{}) {
		t.Error("a click outside did not close the menu")
	}
}

// A nested Menu opens as a submenu from the keyboard, and an Action's
// shortcut triggers it from anywhere in the window.
func TestMenuSubmenuAndShortcut(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    id: root
    width: 400; height: 300
    property string did: ""
    MenuBar { id: bar; width: 400
        Menu { title: "File"
            Action { text: "Save"; shortcut: "Ctrl+S"; onTriggered: root.did = "save" }
            Menu { title: "Export"
                Action { text: "PNG"; onTriggered: root.did = "png" } } } }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	frame := func() { u.Layout(400, 300); u.Layout(400, 300) }
	frame()
	get := func(expr string) string {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return str(v)
	}
	u.Key(app.KeyEvent{Down: true, Rune: 's', Ctrl: true})
	frame()
	if got := get("root.did"); got != "save" {
		t.Errorf("Ctrl+S did %q, want save (the Action's shortcut)", got)
	}
	// Open File (its title is the bar's first child) and walk into Export.
	el := d.doc.elems.get(d.doc.intern("bar"))
	title := ui.Bounds(ui.Children(el)[0])
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: title.X + 5, Y: title.Y + 5}, {Kind: app.MouseDown, X: title.X + 5, Y: title.Y + 5, Button: 1},
		{Kind: app.MouseUp, X: title.X + 5, Y: title.Y + 5, Button: 1}})
	frame()
	for _, k := range []app.Key{app.KeyDown, app.KeyDown, app.KeyRight, app.KeyDown, app.KeyEnter} {
		u.Key(app.KeyEvent{Down: true, Key: k})
		frame()
	}
	if got := get("root.did"); got != "png" {
		t.Errorf("File > Export > PNG by keyboard did %q, want png", got)
	}
}

// A Drawer slides in from its edge on open(), and a click on the scrim
// closes it and runs onClosed.
func TestDrawerOpenClose(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    id: root
    width: 600; height: 400
    property int closedCount: 0
    Drawer { id: nav; width: 240; edge: Qt.RightEdge
        onClosed: root.closedCount++
        Text { text: "Nav" } }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	frame := func() { u.Layout(600, 400); u.Layout(600, 400) }
	settle := func() {
		for i := 0; i < 30; i++ {
			core.TickAnimations(20 * time.Millisecond)
			frame()
		}
	}
	get := func(expr string) string {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return str(v)
	}
	frame()
	if _, err := evalString("nav.open()", env); err != nil {
		t.Fatal(err)
	}
	settle()
	if got := get("nav.position"); got != "1" {
		t.Errorf("position after open() = %s, want 1", got)
	}
	// A click on the panel itself (x 360..600) leaves it open.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 500, Y: 300, Button: 1}, {Kind: app.MouseUp, X: 500, Y: 300, Button: 1}})
	settle()
	if get("nav.opened") != "true" {
		t.Error("a click inside the drawer closed it")
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 100, Y: 200, Button: 1}, {Kind: app.MouseUp, X: 100, Y: 200, Button: 1}})
	settle()
	if get("nav.opened") != "false" || get("root.closedCount") != "1" {
		t.Errorf("after a scrim click: opened %s, onClosed ran %s times; want false, 1",
			get("nav.opened"), get("root.closedCount"))
	}
}

// A running BusyIndicator asks for frames, and stops asking when it stops
// (or when its page no longer builds it).
func TestBusyIndicatorRuns(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    id: root
    width: 200; height: 200
    property bool busy: true
    property bool shown: true
    Loader { active: root.shown; sourceComponent: BusyIndicator { running: root.busy } }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	frame := func() { u.Layout(200, 200); u.Layout(200, 200) }
	frame()
	// The indicator's own loop (other documents ask for frames too).
	running := func() bool {
		for _, a := range d.doc.activities {
			if a.Running() {
				return true
			}
		}
		return false
	}
	if !running() {
		t.Fatal("a running BusyIndicator is not running")
	}
	if _, err := evalString("root.busy = false", env); err != nil {
		t.Fatal(err)
	}
	frame()
	if running() {
		t.Error("a stopped BusyIndicator still runs")
	}
	if _, err := evalString("root.busy = true", env); err != nil {
		t.Fatal(err)
	}
	frame()
	if _, err := evalString("root.shown = false", env); err != nil {
		t.Fatal(err)
	}
	frame()
	if running() || len(d.doc.activities) != 0 {
		t.Error("a BusyIndicator its Loader dropped still runs")
	}
}
