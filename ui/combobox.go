package ui

import (
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
}

// comboSynced is the current index each editable box's field last took.
var comboSynced = map[*InputState]int{}

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
	arrow := Text("▾", RGB(0x868e96))
	box := Div().Size(w, 32).Bg(RGB(0xffffff)).Border(1, RGB(0xced4da)).Rounded(4).
		Row().ItemsCenter().PadXY(8, 0).Gap(6)
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
		box.Children(Input(in, w-40).Bg(RGB(0xffffff)).TextColor(RGB(0x111111)).Border(0, RGBA{}), toggle)
	} else {
		box.Children(Text(text, RGB(0x111111)), Div().Grow(), arrow)
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
	list := Div().Col().W(w).Bg(RGB(0xffffff)).Border(1, RGB(0xced4da)).Rounded(4).P(2)
	for i, it := range o.Items {
		i := i
		row := Div().W(w-4).H(28).PadXY(8, 0).Row().ItemsCenter().Rounded(3).
			HoverBg(RGB(0xe7f0ff)).OnClick(func() { pick(i) }).
			Children(Text(it, RGB(0x111111)))
		if i == cur {
			row.Bg(RGB(0xd0e2ff))
		}
		list.Children(row)
	}
	return Div().Children(box,
		Overlay(list, 0, 0).AnchorBelow(box, 2).SnapToWindow(4).OnClickOutside(func() { open.Set(false) }))
}
