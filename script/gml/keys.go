package gml

import "github.com/sushvibez/gfx/app"

// QML Keys handling. An element with Keys.onPressed / Keys.onReleased receives
// raw key events while it holds focus; the handler runs with an `event` object
// bound (event.key is a Qt.Key_* code, event.text the typed character).

// Qt::Key codes (subset) — exposed as Qt.Key_* enums so handlers can compare
// event.key == Qt.Key_Left, matching real QML.
var qtKeyCodes = map[string]float64{
	"Key_Escape":    0x01000000,
	"Key_Tab":       0x01000001,
	"Key_Backspace": 0x01000003,
	"Key_Return":    0x01000004,
	"Key_Enter":     0x01000005,
	"Key_Delete":    0x01000007,
	"Key_Home":      0x01000010,
	"Key_End":       0x01000011,
	"Key_Left":      0x01000012,
	"Key_Up":        0x01000013,
	"Key_Right":     0x01000014,
	"Key_Down":      0x01000015,
	"Key_Space":     0x20,
}

// keyCode maps an app.KeyEvent to the Qt key code (for event.key).
func keyCode(ev app.KeyEvent) float64 {
	switch ev.Key {
	case app.KeyEscape:
		return qtKeyCodes["Key_Escape"]
	case app.KeyTab:
		return qtKeyCodes["Key_Tab"]
	case app.KeyBackspace:
		return qtKeyCodes["Key_Backspace"]
	case app.KeyEnter:
		return qtKeyCodes["Key_Return"]
	case app.KeyDelete:
		return qtKeyCodes["Key_Delete"]
	case app.KeyHome:
		return qtKeyCodes["Key_Home"]
	case app.KeyEnd:
		return qtKeyCodes["Key_End"]
	case app.KeyLeft:
		return qtKeyCodes["Key_Left"]
	case app.KeyUp:
		return qtKeyCodes["Key_Up"]
	case app.KeyRight:
		return qtKeyCodes["Key_Right"]
	case app.KeyDown:
		return qtKeyCodes["Key_Down"]
	}
	if ev.Rune != 0 {
		r := ev.Rune
		if r >= 'a' && r <= 'z' { // Qt key codes use the uppercase letter
			r -= 32
		}
		return float64(r)
	}
	return 0
}

// keyEvent builds the `event` object passed to a Keys handler.
func keyEvent(ev app.KeyEvent) map[string]any {
	text := ""
	if ev.Rune != 0 {
		text = string(ev.Rune)
	}
	return map[string]any{
		"key":          keyCode(ev),
		"text":         text,
		"modifiers":    float64(0),
		"accepted":     true,
		"isAutoRepeat": false,
	}
}

// keyHandler returns a ui key callback that runs the given press/release
// statement bodies with `event` bound, or nil if both are empty.
func keyHandler(onPressed, onReleased []stmt, env *Env) func(app.KeyEvent) {
	if onPressed == nil && onReleased == nil {
		return nil
	}
	return func(ev app.KeyEvent) {
		body := onPressed
		if !ev.Down {
			body = onReleased
		}
		if body == nil {
			return
		}
		scope := env.child()
		scope.vars["event"] = keyEvent(ev)
		if err := runStmts(body, scope); err != nil {
			logf("gml: Keys handler: %v", err)
		}
	}
}
