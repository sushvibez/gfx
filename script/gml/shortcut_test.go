package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

func TestShortcut(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    id: root
    width: 300; height: 200
    property int saves: 0
    property int undos: 0
    property int refreshes: 0
    property int plus: 0
    property int typed: 0
    property bool canRefresh: true
    Shortcut { sequence: "Ctrl+S"; onActivated: root.saves += 1 }
    Shortcut { sequence: StandardKey.Undo; onActivated: root.undos += 1 }
    Shortcut { sequences: ["F5", "Ctrl+R"]; enabled: root.canRefresh; onActivated: root.refreshes += 1 }
    Shortcut { sequence: Qt.CTRL | Qt.Key_Plus; onActivated: root.plus += 1 }
    Shortcut { sequence: "X"; onActivated: root.typed += 1 }
    TextField { width: 100; height: 30; focus: true }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	get := func(name string) string { return str(env.vars[name].(Ref).get()) }
	press := func(ev app.KeyEvent) bool {
		ev.Down = true
		ok := u.Key(ev)
		u.Layout(300, 200)
		return ok
	}

	if !press(app.KeyEvent{Rune: 's', Ctrl: true}) {
		t.Error("Ctrl+S not consumed")
	}
	press(app.KeyEvent{Rune: 'z', Ctrl: true})
	press(app.KeyEvent{Key: app.KeyF5})
	press(app.KeyEvent{Rune: 'r', Ctrl: true})
	press(app.KeyEvent{Rune: '+', Ctrl: true, Shift: true}) // Shift types the +
	if got := get("saves"); got != "1" {
		t.Errorf("saves = %s, want 1", got)
	}
	if got := get("undos"); got != "1" {
		t.Errorf("undos = %s, want 1 (StandardKey.Undo is Ctrl+Z)", got)
	}
	if got := get("refreshes"); got != "2" {
		t.Errorf("refreshes = %s, want 2 (sequences)", got)
	}
	if got := get("plus"); got != "1" {
		t.Errorf("plus = %s, want 1 (Qt.CTRL | Qt.Key_Plus)", got)
	}

	// The focused text field keeps a plain key it types.
	press(app.KeyEvent{Rune: 'x'})
	if got := get("typed"); got != "0" {
		t.Errorf("typed = %s, want 0: a plain key belongs to the focused text field", got)
	}
	// A disabled shortcut lets the key through.
	env.vars["canRefresh"].(Ref).set(false)
	u.Layout(300, 200)
	press(app.KeyEvent{Key: app.KeyF5})
	if got := get("refreshes"); got != "2" {
		t.Errorf("refreshes = %s after disabling, want 2", got)
	}
	// Modifiers must match exactly: Ctrl+Alt+S is not Ctrl+S.
	press(app.KeyEvent{Rune: 's', Ctrl: true, Alt: true})
	if got := get("saves"); got != "1" {
		t.Errorf("saves = %s after Ctrl+Alt+S, want 1", got)
	}
}

// A Shortcut usually sits on the window itself.
func TestShortcutOnWindow(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
ApplicationWindow {
    id: win
    width: 300; height: 200
    property int quits: 0
    Shortcut { sequence: StandardKey.Quit; onActivated: win.quits += 1 }
    Rectangle { anchors.fill: parent; color: "gray" }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	if !u.Key(app.KeyEvent{Down: true, Rune: 'q', Ctrl: true}) {
		t.Fatal("Ctrl+Q not consumed")
	}
	if got := str(env.vars["quits"].(Ref).get()); got != "1" {
		t.Errorf("quits = %s, want 1", got)
	}
}
