package gml

import (
	"fmt"
	"strings"

	"github.com/sushvibez/gfx/app"
)

// QML Keys handling. An element with Keys.onPressed / Keys.onReleased receives
// raw key events while it holds focus; the handler runs with an `event` object
// bound (event.key is a Qt.Key_* code, event.text the typed character,
// event.modifiers the Qt.*Modifier bits), and a handler that sets
// event.accepted = true keeps the key; one it leaves unaccepted falls
// through to the default handling (Enter/Space activation), as in QML.

// Qt::Key codes, exposed as Qt.Key_* enums so handlers can compare
// event.key == Qt.Key_Left, matching real QML. Letters, digits and F-keys
// are filled in by init.
var qtKeyCodes = map[string]float64{
	"Key_Escape":       0x01000000,
	"Key_Tab":          0x01000001,
	"Key_Backtab":      0x01000002,
	"Key_Backspace":    0x01000003,
	"Key_Return":       0x01000004,
	"Key_Enter":        0x01000005,
	"Key_Insert":       0x01000006,
	"Key_Delete":       0x01000007,
	"Key_Home":         0x01000010,
	"Key_End":          0x01000011,
	"Key_Left":         0x01000012,
	"Key_Up":           0x01000013,
	"Key_Right":        0x01000014,
	"Key_Down":         0x01000015,
	"Key_PageUp":       0x01000016,
	"Key_PageDown":     0x01000017,
	"Key_Shift":        0x01000020,
	"Key_Control":      0x01000021,
	"Key_Meta":         0x01000022,
	"Key_Alt":          0x01000023,
	"Key_Space":        0x20,
	"Key_Exclam":       0x21,
	"Key_QuoteDbl":     0x22,
	"Key_NumberSign":   0x23,
	"Key_Dollar":       0x24,
	"Key_Percent":      0x25,
	"Key_Ampersand":    0x26,
	"Key_Apostrophe":   0x27,
	"Key_ParenLeft":    0x28,
	"Key_ParenRight":   0x29,
	"Key_Asterisk":     0x2a,
	"Key_Plus":         0x2b,
	"Key_Comma":        0x2c,
	"Key_Minus":        0x2d,
	"Key_Period":       0x2e,
	"Key_Slash":        0x2f,
	"Key_Colon":        0x3a,
	"Key_Semicolon":    0x3b,
	"Key_Less":         0x3c,
	"Key_Equal":        0x3d,
	"Key_Greater":      0x3e,
	"Key_Question":     0x3f,
	"Key_At":           0x40,
	"Key_BracketLeft":  0x5b,
	"Key_Backslash":    0x5c,
	"Key_BracketRight": 0x5d,
	"Key_Underscore":   0x5f,
	"Key_QuoteLeft":    0x60,
	"Key_BraceLeft":    0x7b,
	"Key_Bar":          0x7c,
	"Key_BraceRight":   0x7d,
	"Key_AsciiTilde":   0x7e,
}

func init() {
	for c := 'A'; c <= 'Z'; c++ {
		qtKeyCodes["Key_"+string(c)] = float64(c)
	}
	for c := '0'; c <= '9'; c++ {
		qtKeyCodes["Key_"+string(c)] = float64(c)
	}
	for i := 1; i <= 12; i++ {
		qtKeyCodes[fmt.Sprintf("Key_F%d", i)] = float64(0x01000030 + i - 1)
	}
}

// Qt::KeyboardModifier bits, for event.modifiers (exposed as
// Qt.ShiftModifier and friends).
var qtModifiers = map[string]float64{
	"NoModifier":      0,
	"ShiftModifier":   0x02000000,
	"ControlModifier": 0x04000000,
	"AltModifier":     0x08000000,
	"MetaModifier":    0x10000000,
	"KeypadModifier":  0x20000000,
}

// appKeyCodes maps the app layer's non-printable keys to Qt codes.
var appKeyCodes = map[app.Key]string{
	app.KeyEscape:    "Key_Escape",
	app.KeyTab:       "Key_Tab",
	app.KeyBackspace: "Key_Backspace",
	app.KeyEnter:     "Key_Return",
	app.KeyInsert:    "Key_Insert",
	app.KeyDelete:    "Key_Delete",
	app.KeyHome:      "Key_Home",
	app.KeyEnd:       "Key_End",
	app.KeyLeft:      "Key_Left",
	app.KeyUp:        "Key_Up",
	app.KeyRight:     "Key_Right",
	app.KeyDown:      "Key_Down",
	app.KeyPageUp:    "Key_PageUp",
	app.KeyPageDown:  "Key_PageDown",
	app.KeyF1:        "Key_F1",
	app.KeyF2:        "Key_F2",
	app.KeyF3:        "Key_F3",
	app.KeyF4:        "Key_F4",
	app.KeyF5:        "Key_F5",
	app.KeyF6:        "Key_F6",
	app.KeyF7:        "Key_F7",
	app.KeyF8:        "Key_F8",
	app.KeyF9:        "Key_F9",
	app.KeyF10:       "Key_F10",
	app.KeyF11:       "Key_F11",
	app.KeyF12:       "Key_F12",
}

// keyCode maps an app.KeyEvent to the Qt key code (for event.key).
func keyCode(ev app.KeyEvent) float64 {
	if name, ok := appKeyCodes[ev.Key]; ok {
		return qtKeyCodes[name]
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

// keyModifiers is the event's Qt.KeyboardModifiers bitmask.
func keyModifiers(ev app.KeyEvent) float64 {
	var m float64
	if ev.Shift {
		m += qtModifiers["ShiftModifier"]
	}
	if ev.Ctrl {
		m += qtModifiers["ControlModifier"]
	}
	if ev.Alt {
		m += qtModifiers["AltModifier"]
	}
	return m
}

// keyEvent builds the `event` object passed to a Keys handler. accepted
// starts false, as Qt's KeyEvent does for Keys.onPressed: a handler that
// acts on the key sets it.
func keyEvent(ev app.KeyEvent) map[string]any {
	text := ""
	if ev.Rune != 0 {
		text = string(ev.Rune)
	}
	return map[string]any{
		"key":          keyCode(ev),
		"text":         text,
		"modifiers":    keyModifiers(ev),
		"accepted":     false,
		"isAutoRepeat": false,
		"count":        float64(1),
	}
}

// keySignalCode is the Qt key a key-specific handler is for:
// "Keys.onLeftPressed" → Qt.Key_Left, "Keys.onDigit3Pressed" → Qt.Key_3.
func keySignalCode(attr string) float64 {
	name := strings.TrimSuffix(strings.TrimPrefix(attr, "Keys.on"), "Pressed")
	name = strings.TrimPrefix(name, "Digit")
	return qtKeyCodes["Key_"+name]
}

// keyHandler returns a ui key callback that runs the given press/release
// statement bodies with `event` bound and reports whether the handler
// accepted the key, or nil if both are empty.
func keyHandler(onPressed, onReleased []stmt, env *Env) func(app.KeyEvent) bool {
	return keyHandlerWith(nil, onPressed, onReleased, env)
}

// keyHandlerWith is keyHandler with key-specific press handlers
// (Keys.onLeftPressed, by Qt key code). As in Qt, such a handler starts
// with the event accepted; only if it un-accepts it does the generic
// onPressed run as well.
func keyHandlerWith(specific map[float64][]stmt, onPressed, onReleased []stmt, env *Env) func(app.KeyEvent) bool {
	if onPressed == nil && onReleased == nil && len(specific) == 0 {
		return nil
	}
	run := func(body []stmt, event map[string]any) bool {
		scope := env.child()
		scope.vars["event"] = event
		if err := runStmts(body, scope); err != nil {
			logf("gml: Keys handler: %v", err)
		}
		accepted, _ := toBool(event["accepted"])
		return accepted
	}
	return func(ev app.KeyEvent) bool {
		event := keyEvent(ev)
		if !ev.Down {
			return onReleased != nil && run(onReleased, event)
		}
		if body, ok := specific[keyCode(ev)]; ok {
			event["accepted"] = true
			if run(body, event) {
				return true
			}
		}
		return onPressed != nil && run(onPressed, event)
	}
}
