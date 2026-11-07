package ui

import (
	"strings"
	"unicode"

	"github.com/sushvibez/gfx/app"
)

// Keyboard shortcuts: a key sequence as text ("Ctrl+Shift+S", "F5",
// "Ctrl++"), the standard ones by name (StdSave is Ctrl+S here), and
// DivEl.Shortcut, which runs a function when the sequence is pressed
// anywhere in the window (see DivEl.OnShortcut for the routing).

// KeyCombo is one key with its modifiers: a named key (Key) or a
// character (Rune, upper case for a letter).
type KeyCombo struct {
	Key              app.Key
	Rune             rune
	Ctrl, Shift, Alt bool
}

// Matches reports whether ev presses c. A shifted symbol ("Ctrl++",
// "Ctrl+?") is typed with Shift on most layouts, so Shift is not asked of
// a symbol unless the sequence says so.
func (c KeyCombo) Matches(ev app.KeyEvent) bool {
	if !ev.Down || ev.Ctrl != c.Ctrl || ev.Alt != c.Alt {
		return false
	}
	if c.Key != 0 {
		if ev.Key != c.Key {
			return false
		}
	} else if c.Rune == 0 || unicode.ToUpper(ev.Rune) != c.Rune || ev.Key != 0 {
		return false
	}
	if ev.Shift == c.Shift {
		return true
	}
	symbol := c.Key == 0 && !unicode.IsLetter(c.Rune) && !unicode.IsDigit(c.Rune) && c.Rune != ' '
	return symbol && !c.Shift
}

// shortcutKeys are the named keys of a sequence, by the names Qt's
// portable key-sequence text uses.
var shortcutKeys = map[string]app.Key{
	"esc": app.KeyEscape, "escape": app.KeyEscape, "tab": app.KeyTab,
	"backspace": app.KeyBackspace, "return": app.KeyEnter, "enter": app.KeyEnter,
	"ins": app.KeyInsert, "insert": app.KeyInsert, "del": app.KeyDelete,
	"delete": app.KeyDelete, "home": app.KeyHome, "end": app.KeyEnd,
	"left": app.KeyLeft, "up": app.KeyUp, "right": app.KeyRight, "down": app.KeyDown,
	"pgup": app.KeyPageUp, "pageup": app.KeyPageUp, "pgdown": app.KeyPageDown,
	"pagedown": app.KeyPageDown,

	"f1": app.KeyF1, "f2": app.KeyF2, "f3": app.KeyF3, "f4": app.KeyF4,
	"f5": app.KeyF5, "f6": app.KeyF6, "f7": app.KeyF7, "f8": app.KeyF8,
	"f9": app.KeyF9, "f10": app.KeyF10, "f11": app.KeyF11, "f12": app.KeyF12,
}

// ParseKeySequence reads one key sequence ("Ctrl+Shift+S", "F5", "Esc",
// "Ctrl++", "Space"), modifiers in any case and order. A multi-chord
// sequence ("Ctrl+K, Ctrl+C") and the Meta modifier are not supported and
// report false.
func ParseKeySequence(s string) (KeyCombo, bool) {
	var c KeyCombo
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
			c.Ctrl = true
		case "shift":
			c.Shift = true
		case "alt":
			c.Alt = true
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
	switch r := []rune(key); {
	case lower == "space":
		c.Rune = ' '
	case shortcutKeys[lower] != 0:
		c.Key = shortcutKeys[lower]
	case len(r) == 1:
		c.Rune = unicode.ToUpper(r[0])
	default:
		return c, false
	}
	return c, true
}

// StandardKey names a platform-standard shortcut. The values are Qt's
// QKeySequence::StandardKey numbers.
type StandardKey int

const (
	StdHelpContents  StandardKey = 1
	StdWhatsThis     StandardKey = 2
	StdOpen          StandardKey = 3
	StdClose         StandardKey = 4
	StdSave          StandardKey = 5
	StdNew           StandardKey = 6
	StdDelete        StandardKey = 7
	StdCut           StandardKey = 8
	StdCopy          StandardKey = 9
	StdPaste         StandardKey = 10
	StdUndo          StandardKey = 11
	StdRedo          StandardKey = 12
	StdBack          StandardKey = 13
	StdForward       StandardKey = 14
	StdRefresh       StandardKey = 15
	StdZoomIn        StandardKey = 16
	StdZoomOut       StandardKey = 17
	StdPrint         StandardKey = 18
	StdAddTab        StandardKey = 19
	StdNextChild     StandardKey = 20
	StdPreviousChild StandardKey = 21
	StdFind          StandardKey = 22
	StdFindNext      StandardKey = 23
	StdFindPrevious  StandardKey = 24
	StdReplace       StandardKey = 25
	StdSelectAll     StandardKey = 26
	StdBold          StandardKey = 27
	StdItalic        StandardKey = 28
	StdUnderline     StandardKey = 29
	StdSaveAs        StandardKey = 63
	StdPreferences   StandardKey = 64
	StdQuit          StandardKey = 65
	StdFullScreen    StandardKey = 66
	StdDeselect      StandardKey = 67
	StdCancel        StandardKey = 70
)

// standardKeySeqs are the Windows/Linux bindings (the common part of Qt's
// KDE and Windows tables).
var standardKeySeqs = map[StandardKey][]string{
	StdHelpContents: {"F1"}, StdWhatsThis: {"Shift+F1"}, StdOpen: {"Ctrl+O"},
	StdClose: {"Ctrl+W", "Ctrl+F4"}, StdSave: {"Ctrl+S"}, StdNew: {"Ctrl+N"},
	StdDelete: {"Del"}, StdCut: {"Ctrl+X", "Shift+Del"}, StdCopy: {"Ctrl+C", "Ctrl+Ins"},
	StdPaste: {"Ctrl+V", "Shift+Ins"}, StdUndo: {"Ctrl+Z"}, StdRedo: {"Ctrl+Y", "Ctrl+Shift+Z"},
	StdBack: {"Alt+Left"}, StdForward: {"Alt+Right"}, StdRefresh: {"F5"},
	StdZoomIn: {"Ctrl++", "Ctrl+="}, StdZoomOut: {"Ctrl+-"}, StdPrint: {"Ctrl+P"},
	StdAddTab: {"Ctrl+T"}, StdNextChild: {"Ctrl+Tab"}, StdPreviousChild: {"Ctrl+Shift+Tab"},
	StdFind: {"Ctrl+F"}, StdFindNext: {"F3"}, StdFindPrevious: {"Shift+F3"},
	StdReplace: {"Ctrl+H"}, StdSelectAll: {"Ctrl+A"}, StdBold: {"Ctrl+B"},
	StdItalic: {"Ctrl+I"}, StdUnderline: {"Ctrl+U"}, StdSaveAs: {"Ctrl+Shift+S"},
	StdQuit: {"Ctrl+Q"}, StdFullScreen: {"F11"}, StdDeselect: {"Ctrl+Shift+A"},
	StdCancel: {"Esc"},
}

// Combos are k's bindings (nil for a key with none here, like
// StdPreferences).
func (k StandardKey) Combos() []KeyCombo {
	var out []KeyCombo
	for _, s := range standardKeySeqs[k] {
		if c, ok := ParseKeySequence(s); ok {
			out = append(out, c)
		}
	}
	return out
}

// Shortcut runs fn when one of the sequences is pressed, wherever focus
// is in the window (OnShortcut's routing). A sequence is a string for
// ParseKeySequence, a StandardKey or a KeyCombo; one that does not parse
// is ignored. Several Shortcut calls on one element all apply.
func (d *DivEl) Shortcut(fn func(), seqs ...any) *DivEl {
	var combos []KeyCombo
	for _, s := range seqs {
		switch s := s.(type) {
		case string:
			if c, ok := ParseKeySequence(s); ok {
				combos = append(combos, c)
			}
		case StandardKey:
			combos = append(combos, s.Combos()...)
		case KeyCombo:
			combos = append(combos, s)
		}
	}
	if len(combos) == 0 {
		return d
	}
	return d.OnShortcut(func(ev app.KeyEvent) bool {
		for _, c := range combos {
			if c.Matches(ev) {
				fn()
				return true
			}
		}
		return false
	})
}
