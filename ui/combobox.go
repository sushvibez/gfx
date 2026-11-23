package ui

import (
	"strings"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// ComboOpts configures a ComboBox.
type ComboOpts struct {
	Items []string
	Width float32 // 0: 140
	// Input makes it editable (QML's editable): the text is typed into a
	// field over this state. Enter selects the item it names, or, when it
	// names none, reports it to OnAccepted (where a page adds it).
	Input *InputState
	// OnActivated runs after the user picks an item, with its index.
	OnActivated func(i int)
	// OnAccepted runs after Enter in an editable box, with the typed text.
	OnAccepted func(text string)
	Disabled   bool
	// Filter puts a search field at the top of the open list that narrows
	// it as you type (case-insensitive, anywhere in the item); Enter picks
	// the first match. For long lists — materials, models. nil: none.
	Filter *InputState
	// MaxRows caps the open list's height in rows (zero: 12); a longer
	// list scrolls.
	MaxRows int
}

// comboSynced is the current index each editable box's field last took.
var comboSynced = map[*InputState]int{}

// comboScroll is each box's list scroll, by its open state.
var comboScroll = map[*core.Property[bool]]*ScrollState{}

// ComboBox is a drop-down choice bound to current (QML's ComboBox): a
// click opens the list under it (open is the retained open state), a pick
// selects and closes, a click outside closes, and Up/Down step through the
// items while it holds focus.
func ComboBox(current *core.Property[int], open *core.Property[bool], o ComboOpts) Element {
	w := o.Width
	if w <= 0 {
		w = 140
	}
	cur := current.Get()
	text := ""
	if cur >= 0 && cur < len(o.Items) {
		text = o.Items[cur]
	}
	pick := func(i int) {
		open.Set(false)
		if o.Filter != nil {
			o.Filter.SetText("")
		}
		if i < 0 || i >= len(o.Items) {
			return
		}
		if o.Input != nil {
			o.Input.SetText(o.Items[i])
		}
		if i != current.Get() {
			current.Set(i)
		}
		if o.OnActivated != nil {
			o.OnActivated(i)
		}
	}
	tm := CurrentTheme()
	arrow := Text("▾", tm.Dim)
	box := Div().Size(w, 32).Bg(tm.Field).Border(1, tm.Border).Rounded(4).
		Row().ItemsCenter().PadXY(8, 0).Gap(6)
	box.Access(Access{Role: RoleComboBox, Value: text, Checked: -1, Expanded: checkState(open.Get()), Disabled: o.Disabled})
	if o.Input != nil {
		in := o.Input
		// The field takes the current item when the selection changes, and
		// keeps what was typed otherwise (Qt's editText), focused or not.
		if last, seen := comboSynced[in]; !seen || last != cur {
			in.SetText(text)
			comboSynced[in] = cur
		}
		in.OnSubmit(func(s string) {
			for i, it := range o.Items {
				if it == s {
					pick(i)
					return
				}
			}
			if o.OnAccepted != nil {
				o.OnAccepted(s)
			}
		})
		toggle := Div().Size(20, 28).JustifyCenter().ItemsCenter().Children(arrow)
		if !o.Disabled {
			toggle.OnClick(func() { open.Set(!open.Get()) })
		}
		box.Children(Input(in, w-40).Bg(tm.Field).TextColor(tm.Text).Border(0, RGBA{}), toggle)
	} else {
		// The current item truncates with an ellipsis: a long name must not
		// run under the arrow or out of the box.
		box.Children(Text(text, tm.Text).Truncate().Grow(), arrow)
		if !o.Disabled {
			box.OnClick(func() { open.Set(!open.Get()) })
		}
	}
	if o.Disabled {
		return box.Opacity(0.5)
	}
	box.OnKeyAccept(func(ev app.KeyEvent) bool {
		if !ev.Down || len(o.Items) == 0 {
			return false
		}
		switch ev.Key {
		case app.KeyUp:
			pick(max(current.Get()-1, 0))
		case app.KeyDown:
			pick(min(current.Get()+1, len(o.Items)-1))
		case app.KeyEscape:
			if !open.Get() {
				return false
			}
			open.Set(false)
		default:
			return false
		}
		return true
	})
	if !open.Get() {
		return box
	}
	const rowH = 28
	query := ""
	if o.Filter != nil {
		query = strings.ToLower(strings.TrimSpace(o.Filter.Text.Get()))
	}
	var shown []int
	for i, it := range o.Items {
		if query == "" || strings.Contains(strings.ToLower(it), query) {
			shown = append(shown, i)
		}
	}
	rows := Div().Col().W(w - 4)
	for _, i := range shown {
		i := i
		row := Div().W(w-4).H(rowH).PadXY(8, 0).Row().ItemsCenter().Rounded(3).
			HoverBg(tm.RowHover).OnClick(func() { pick(i) }).
			Children(Text(o.Items[i], tm.Text).Truncate())
		if i == cur {
			row.Bg(tm.RowSelected)
		}
		rows.Children(row)
	}
	if len(shown) == 0 {
		rows.Children(Div().W(w-4).H(rowH).PadXY(8, 0).Row().ItemsCenter().Children(Text("No match", tm.Dim)))
	}
	maxRows := o.MaxRows
	if maxRows <= 0 {
		maxRows = 12
	}
	list := Div().Col().W(w).Bg(tm.Field).Border(1, tm.Border).Rounded(4).P(2).Gap(2)
	if o.Filter != nil {
		f := o.Filter
		f.OnSubmit(func(string) {
			if len(shown) > 0 {
				pick(shown[0])
			}
		})
		f.OnCancel(func() { f.SetText(""); open.Set(false) })
		list.Children(Input(f, w-4).Bg(tm.Input).TextColor(tm.InputText).
			Placeholder("Filter", tm.Dim).RequestFocus())
	}
	if len(shown) > maxRows {
		st := comboScroll[open]
		if st == nil {
			st = &ScrollState{}
			comboScroll[open] = st
		}
		list.Children(Scroll(st).W(w - 4).H(float32(maxRows * rowH)).Children(rows))
	} else {
		list.Children(rows)
	}
	closeList := func() {
		open.Set(false)
		if o.Filter != nil {
			o.Filter.SetText("")
		}
	}
	return Div().Children(box,
		Overlay(list, 0, 0).AnchorBelow(box, 2).SnapToWindow(4).OnClickOutside(closeList))
}
