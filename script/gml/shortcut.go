package gml

import (
	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// QML's Shortcut: a key sequence that runs onActivated wherever focus is
// in the window. The Shortcut children of a box are hoisted onto it as
// one ui.DivEl.OnShortcut handler; their sequence, enabled and handler are
// read when a key arrives, so bindings on them stay live. The sequences
// themselves are ui's (ui.ParseKeySequence, ui.StandardKey); this file
// adds the QML forms: StandardKey names and Qt key codes with modifier
// bits.

// qtStandardKeys are QKeySequence::StandardKey's names, for the
// StandardKey namespace (ui.StandardKey shares Qt's numbers).
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

// qtCodeKeys maps a Qt key code back to the app layer's named key.
var qtCodeKeys = func() map[float64]app.Key {
	m := map[float64]app.Key{}
	for k, name := range appKeyCodes {
		m[qtKeyCodes[name]] = k
	}
	return m
}()

// keyCombosOf turns a sequence value into combos: a string, a
// StandardKey (its bindings), or a key code with Qt modifier bits
// (Qt.CTRL | Qt.Key_S). A number 1..70 is read as a StandardKey, as the
// enum is what a QML Shortcut is given as a number in practice.
func keyCombosOf(v any) []ui.KeyCombo {
	switch v := v.(type) {
	case string:
		if c, ok := ui.ParseKeySequence(v); ok {
			return []ui.KeyCombo{c}
		}
		logf("gml: Shortcut: unsupported sequence %q", v)
	case float64:
		if v >= 1 && v <= 70 {
			if cs := ui.StandardKey(v).Combos(); cs != nil {
				return cs
			}
		}
		n := int64(v)
		code := float64(n & 0x01ffffff)
		c := ui.KeyCombo{
			Ctrl:  n&int64(qtModifiers["ControlModifier"]) != 0,
			Shift: n&int64(qtModifiers["ShiftModifier"]) != 0,
			Alt:   n&int64(qtModifiers["AltModifier"]) != 0,
		}
		if k, ok := qtCodeKeys[code]; ok {
			c.Key = k
		} else if code > 0 && code < 0x01000000 {
			c.Rune = rune(code) // printable codes are the (upper-case) character
		} else {
			return nil
		}
		return []ui.KeyCombo{c}
	case []any:
		var out []ui.KeyCombo
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
			var combos []ui.KeyCombo
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
				if !k.Matches(ev) {
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
