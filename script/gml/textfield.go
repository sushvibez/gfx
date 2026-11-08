package gml

import "github.com/sushvibez/gfx/ui"

// inputMember answers a TextField/TextInput member from its live state:
// text and length, focus, acceptableInput (the input mask filled, see
// ui.InputState.SetMask), the selection — selectedText, selectionStart,
// selectionEnd, cursorPosition — and the methods selectAll(),
// select(start, end), deselect(), clear() and forceActiveFocus().
func inputMember(in *ui.InputState, name string) (any, bool) {
	switch name {
	case "text", "displayText":
		return in.Text.Get(), true
	case "length":
		return float64(len([]rune(in.Text.Get()))), true
	case "activeFocus", "focus":
		return in.HasFocus(), true
	case "acceptableInput":
		return in.Acceptable(), true
	case "selectedText":
		return in.SelectedText(), true
	case "selectionStart", "selectionEnd":
		a, b := in.Selection()
		if name == "selectionStart" {
			return float64(a), true
		}
		return float64(b), true
	case "cursorPosition":
		return float64(in.Cursor()), true
	case "selectAll":
		return jsFunc(func([]any) (any, error) { in.SelectAll(); return nil, nil }), true
	case "deselect":
		return jsFunc(func([]any) (any, error) { in.Deselect(); return nil, nil }), true
	case "clear":
		return jsFunc(func([]any) (any, error) { in.SetText(""); return nil, nil }), true
	case "select":
		return jsFunc(func(args []any) (any, error) {
			if len(args) >= 2 {
				a, _ := toInt(args[0])
				b, _ := toInt(args[1])
				in.Select(a, b)
			}
			return nil, nil
		}), true
	case "forceActiveFocus":
		return jsFunc(func([]any) (any, error) { in.RequestFocus(); return nil, nil }), true
	}
	return nil, false
}
