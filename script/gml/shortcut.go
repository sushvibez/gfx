package gml

import (
	"strings"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// QML's Shortcut: a key sequence that runs onActivated wherever focus is
// in the window. The Shortcut children of a box are hoisted onto it as
// one ui.DivEl.OnShortcut handler; their sequence, enabled and handler are
// read when a key arrives, so bindings on them stay live.

// keyCombo is one key with its modifiers, in Qt key codes.
type keyCombo struct {
	key              float64
	ctrl, shift, alt bool
}

// matches reports whether ev presses c. A shifted symbol ("Ctrl++",
// "Ctrl+?") is typed with Shift on most layouts, so Shift is not asked of
// a key that is not a letter or a named key unless the sequence says so.
func (c keyCombo) matches(ev app.KeyEvent) bool {
	if !ev.Down || keyCode(ev) != c.key || ev.Ctrl != c.ctrl || ev.Alt != c.alt {
		return false
	}
	if ev.Shift == c.shift {
		return true
	}
	symbol := c.key < 0x01000000 && !(c.key >= 'A' && c.key <= 'Z') && !(c.key >= '0' && c.key <= '9') && c.key != ' '
	return symbol && !c.shift
}

// qtStandardKeys numbers QKeySequence::StandardKey (StandardKey.Save is 5
// in Qt too).
var qtStandardKeys = map[string]float64{
	"UnknownKey": 0, "HelpContents": 1, "WhatsThis": 2, "Open": 3, "Close": 4,
	"Save": 5, "New": 6, "Delete": 7, "Cut": 8, "Copy": 9, "Paste": 10,
	"Undo": 11, "Redo": 12, "Back": 13, "Forward": 14, "Refresh": 15,
	"ZoomIn": 16, "ZoomOut": 17, "Print": 18, "AddTab": 19, "NextChild": 20,
	"PreviousChild": 21, "Find": 22, "FindNext": 23, "FindPrevious": 24,
	"Replace": 25, "SelectAll": 26, "Bold": 27, "Italic": 28, "Underline": 29,
	"SaveAs": 63, "Preferences": 64, "Quit": 65, "FullScreen": 66,
	"Deselect": 67, "Cancel": 70,
}

// standardKeyBindings are the Windows/Linux bindings of the standard keys
// a Shortcut is likely to name (Qt's KDE/Windows tables, the common part).
var standardKeyBindings = map[float64][]string{
	1: {"F1"}, 2: {"Shift+F1"}, 3: {"Ctrl+O"}, 4: {"Ctrl+W", "Ctrl+F4"},
	5: {"Ctrl+S"}, 6: {"Ctrl+N"}, 7: {"Del"}, 8: {"Ctrl+X", "Shift+Del"},
	9: {"Ctrl+C", "Ctrl+Ins"}, 10: {"Ctrl+V", "Shift+Ins"}, 11: {"Ctrl+Z"},
	12: {"Ctrl+Y", "Ctrl+Shift+Z"}, 13: {"Alt+Left"}, 14: {"Alt+Right"},
	15: {"F5"}, 16: {"Ctrl++", "Ctrl+="}, 17: {"Ctrl+-"}, 18: {"Ctrl+P"},
	19: {"Ctrl+T"}, 20: {"Ctrl+Tab"}, 21: {"Ctrl+Shift+Tab"}, 22: {"Ctrl+F"},
	23: {"F3"}, 24: {"Shift+F3"}, 25: {"Ctrl+H"}, 26: {"Ctrl+A"},
	27: {"Ctrl+B"}, 28: {"Ctrl+I"}, 29: {"Ctrl+U"}, 63: {"Ctrl+Shift+S"},
	65: {"Ctrl+Q"}, 66: {"F11"}, 67: {"Ctrl+Shift+A"}, 70: {"Esc"},
}

// shortcutKeyNames are the named keys of a sequence string, by the names
// QKeySequence's portable text accepts.
var shortcutKeyNames = map[string]string{
	"esc": "Key_Escape", "escape": "Key_Escape", "tab": "Key_Tab",
	"backspace": "Key_Backspace", "return": "Key_Return", "enter": "Key_Return",
	"ins": "Key_Insert", "insert": "Key_Insert", "del": "Key_Delete",
	"delete": "Key_Delete", "home": "Key_Home", "end": "Key_End",
	"left": "Key_Left", "up": "Key_Up", "right": "Key_Right", "down": "Key_Down",
	"pgup": "Key_PageUp", "pageup": "Key_PageUp", "pgdown": "Key_PageDown",
	"pagedown": "Key_PageDown",
}

// parseKeySequence reads one QKeySequence string ("Ctrl+Shift+S", "F5",
// "Ctrl++"). A multi-chord sequence ("Ctrl+K, Ctrl+C") and a Meta
// modifier are not supported and report false.
func parseKeySequence(s string) (keyCombo, bool) {
	var c keyCombo
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ","); s == "" || i >= 0 && i < len(s)-1 {
		return c, false
	}
	for {
		i := strings.Index(s, "+")
		if i <= 0 || i == len(s)-1 { // no modifier left, or a trailing "+" key
			break
		}
		switch strings.ToLower(strings.TrimSpace(s[:i])) {
		case "ctrl", "control":
			c.ctrl = true
		case "shift":
			c.shift = true
		case "alt":
			c.alt = true
		default:
			return c, false
		}
		s = s[i+1:]
	}
	key := strings.TrimSpace(s)
	if key == "" {
		key = s // " " is Space
	}
	lower := strings.ToLower(key)
	switch {
	case lower == "space":
		c.key = ' '
	case shortcutKeyNames[lower] != "":
		c.key = qtKeyCodes[shortcutKeyNames[lower]]
	case len(lower) >= 2 && lower[0] == 'f' && qtKeyCodes["Key_F"+lower[1:]] != 0:
		c.key = qtKeyCodes["Key_F"+lower[1:]]
	case len([]rune(key)) == 1:
		r := []rune(key)[0]
		if r >= 'a' && r <= 'z' {
			r -= 32
		}
		c.key = float64(r)
	default:
		return c, false
	}
	return c, true
}

// keyCombosOf turns a sequence value into combos: a string, a
// StandardKey (its bindings), or a key code with Qt modifier bits
// (Qt.CTRL | Qt.Key_S). A number 1..70 is read as a StandardKey, as the
// enum is what a QML Shortcut is given as a number in practice.
func keyCombosOf(v any) []keyCombo {
	switch v := v.(type) {
	case string:
		if c, ok := parseKeySequence(v); ok {
			return []keyCombo{c}
		}
		logf("gml: Shortcut: unsupported sequence %q", v)
	case float64:
		if b, ok := standardKeyBindings[v]; ok && v <= 70 {
			var out []keyCombo
			for _, s := range b {
				out = append(out, keyCombosOf(s)...)
			}
			return out
		}
		n := int64(v)
		c := keyCombo{
			key:   float64(n & 0x01ffffff),
			ctrl:  n&int64(qtModifiers["ControlModifier"]) != 0,
			shift: n&int64(qtModifiers["ShiftModifier"]) != 0,
			alt:   n&int64(qtModifiers["AltModifier"]) != 0,
		}
		if c.key != 0 {
			return []keyCombo{c}
		}
	case []any:
		var out []keyCombo
		for _, e := range v {
			out = append(out, keyCombosOf(e)...)
		}
		return out
	}
	return nil
}

// wireShortcuts hoists n's Shortcut children onto d.
func (doc *docState) wireShortcuts(n *node, d *ui.DivEl, env *Env) {
	var cuts []*node
	for _, c := range n.children {
		if c.typeName == "Shortcut" {
			cuts = append(cuts, c)
		}
	}
	if len(cuts) == 0 {
		return
	}
	run := doc.handlerRunner(env)
	type wired struct {
		n         *node
		connected func()
	}
	ws := make([]wired, len(cuts))
	for i, c := range cuts {
		ws[i] = wired{c, doc.connectedHandler(nodeID(c), "onActivated", env)}
	}
	d.OnShortcut(func(ev app.KeyEvent) bool {
		for _, w := range ws {
			c := doc.withAttrOverrides(w.n)
			enabled := true
			var combos []keyCombo
			var onActivated []stmt
			for _, a := range c.attrs {
				switch a.name {
				case "enabled":
					if v, err := attrBool(a, env); err == nil {
						enabled = v
					}
				case "sequence", "sequences":
					if a.expr != nil {
						if v, err := a.expr.eval(env); err == nil {
							combos = append(combos, keyCombosOf(v)...)
						}
					}
				case "onActivated":
					onActivated = a.stmt
				}
			}
			if !enabled {
				continue
			}
			for _, k := range combos {
				if !k.matches(ev) {
					continue
				}
				if onActivated != nil {
					run(onActivated, "onActivated")
				}
				if w.connected != nil {
					w.connected()
				}
				return true
			}
		}
		return false
	})
}
