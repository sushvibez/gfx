package ui

import (
	"strconv"
	"strings"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// SpinOpts configures a SpinBox. The zero value is a 0..99 box stepping by
// one, clamped at the ends, showing the number.
type SpinOpts struct {
	From, To, Step int
	// Wrap: stepping past one end continues from the other (QML's wrap).
	Wrap bool
	// Format shows a value (QML's textFromValue); nil shows the number.
	Format func(int) string
	// Parse reads typed text back (valueFromText); nil reads a number.
	Parse func(string) (int, bool)
	// Input makes the value editable (QML's editable): a field over this
	// state, committed on Enter. nil shows the value as a label.
	Input *InputState
	// OnModified runs after the user changes the value (valueModified).
	OnModified func()
	// Disabled: shown dimmed, and nothing steps or edits it.
	Disabled bool
	// TextColor and Width style the value box (zero: the defaults).
	TextColor RGBA
	Width     float32
}

// spinWheel accumulates sub-notch wheel deltas per spin box value.
var spinWheel = map[*core.Property[int]]float32{}

// SpinBox is a number with − and + buttons, bound to value (QML's
// SpinBox): the buttons, the Up/Down keys (PageUp/PageDown ten steps) and
// the wheel step it within From..To — clamped, or wrapping round with
// Wrap — and with an Input it can be typed into, committed on Enter
// (refused text puts the value back).
func SpinBox(value *core.Property[int], o SpinOpts) *DivEl {
	if o.Step == 0 {
		o.Step = 1
	}
	if o.From == 0 && o.To == 0 {
		o.To = 99
	}
	lo, hi := min(o.From, o.To), max(o.From, o.To)
	format := o.Format
	if format == nil {
		format = strconv.Itoa
	}
	parse := o.Parse
	if parse == nil {
		parse = func(s string) (int, bool) {
			v, err := strconv.Atoi(strings.TrimSpace(s))
			return v, err == nil
		}
	}
	tm := CurrentTheme()
	textC := o.TextColor
	if textC == (RGBA{}) {
		textC = tm.Text
	}
	set := func(v int) {
		switch {
		case o.Wrap && v > hi:
			v = lo
		case o.Wrap && v < lo:
			v = hi
		default:
			v = min(max(v, lo), hi)
		}
		if v != value.Get() {
			value.Set(v)
			if o.OnModified != nil {
				o.OnModified()
			}
		}
	}
	step := func(n int) {
		if n != 0 {
			set(value.Get() + n*o.Step)
		}
	}
	cur := value.Get()
	btn := func(s string, n int, enabled bool) *DivEl {
		enabled = enabled && !o.Disabled
		b := Div().Size(32, 32).Bg(tm.FieldButton).Rounded(4).JustifyCenter().ItemsCenter()
		c := textC
		if !enabled {
			c.A *= 0.35
		}
		b.Children(Text(s, c))
		if enabled {
			b.OnClick(func() { step(n) })
		}
		return b
	}
	w := o.Width
	if w <= 0 {
		w = 56
	}
	var val Element
	if o.Input != nil && !o.Disabled {
		in := o.Input
		if !in.HasFocus() && in.Text.Get() != format(cur) {
			in.SetText(format(cur)) // the field shows the value while not being typed in
		}
		in.OnSubmit(func(s string) {
			if v, ok := parse(s); ok {
				set(v)
			}
			in.SetText(format(value.Get()))
		})
		val = Input(in, w).Bg(tm.Field).TextColor(textC)
	} else {
		val = Div().Size(w, 32).Bg(tm.Field).Border(1, tm.Border).
			JustifyCenter().ItemsCenter().Children(Text(format(cur), textC))
	}
	box := Div().Row().ItemsCenter().Gap(4).Children(
		btn("−", -1, o.Wrap || cur > lo), val, btn("+", 1, o.Wrap || cur < hi))
	if o.Disabled {
		return box.Opacity(0.5)
	}
	box.OnKeyAccept(func(ev app.KeyEvent) bool {
		if !ev.Down {
			return false
		}
		switch ev.Key {
		case app.KeyUp:
			step(1)
		case app.KeyDown:
			step(-1)
		case app.KeyPageUp:
			step(10)
		case app.KeyPageDown:
			step(-10)
		default:
			return false
		}
		return true
	})
	box.OnWheel(func(_, dy float32) bool {
		// The wheel turned away (negative dy) steps up, a notch a step;
		// a touchpad's fractions add up first.
		acc := spinWheel[value] - dy
		n := int(acc / app.WheelNotchPx)
		spinWheel[value] = acc - float32(n)*app.WheelNotchPx
		step(n)
		return true
	})
	return box
}
