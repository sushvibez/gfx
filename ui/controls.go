package ui

import "github.com/sushvibez/gfx/core"

// The controls kit: buttons, switches, menus, dialogs, drawers, tabs, tool
// tips and progress — QML's Quick Controls as ui elements, which gml's
// controls are built on. Each draws in the kit's look (an accent-blue
// button with white text, a light card for popups) and takes overrides in
// its options. Pane, Frame and Page are plain boxes with a name, sugar for
// Div.

// The kit's accent, the colour of a button, a checked switch, a
// highlighted row, a progress bar.
var (
	Accent      = RGB(0x3b5bdb)
	AccentHover = RGB(0x4c6ef5)
	AccentPress = RGB(0x2f44ad)
)

// ButtonOpts configures a Button.
type ButtonOpts struct {
	OnClick func()
	// Width and Height: 0 is the kit's 96×38 (a ToolButton sizes to its
	// label).
	Width, Height float32
	// Color and TextColor override the accent and white (zero: the kit's).
	Color, TextColor RGBA
	Radius           float32 // 0: 9
	// Flat: no background at rest, the colour is the text's and a tint
	// shows on hover (QML's flat). Highlighted: the brighter accent.
	Flat, Highlighted bool
	Disabled          bool
	// Icon sits left of the label.
	Icon Element
	// Checked makes it checkable: a click toggles it, and a checked button
	// shows pressed (QML's checkable/checked).
	Checked *core.Property[bool]
}

// Button is a push button (QML's Button): hover lighter, press darker,
// Enter or Space activates it while it holds focus, and a label wider than
// the button elides.
func Button(label string, o ButtonOpts) *DivEl {
	w, h := o.Width, o.Height
	if w == 0 {
		w = 96
	}
	if h == 0 {
		h = 38
	}
	d := Div().Size(w, h).JustifyCenter().ItemsCenter()
	styleButton(d, label, o)
	return d
}

// ToolButton is a flat button sized to its label (QML's ToolButton).
func ToolButton(label string, o ButtonOpts) *DivEl {
	o.Flat = true
	h := o.Height
	if h == 0 {
		h = 32
	}
	d := Div().H(h).PadXY(10, 0).JustifyCenter().ItemsCenter()
	if o.Width > 0 {
		d.W(o.Width)
	}
	if o.Radius == 0 {
		o.Radius = 4
	}
	styleButton(d, label, o)
	return d
}

func styleButton(d *DivEl, label string, o ButtonOpts) {
	r := o.Radius
	if r == 0 {
		r = 9
	}
	d.Rounded(r).Focusable()
	accent := Accent
	if o.Color != (RGBA{}) {
		accent = o.Color
	}
	text := RGB(0xffffff)
	checked := o.Checked != nil && o.Checked.Get()
	switch {
	case o.Flat:
		if o.TextColor == (RGBA{}) {
			text = accent
		}
		hov, prs := accent, accent
		hov.A, prs.A = 0.15, 0.3
		d.HoverBg(hov).PressBg(prs)
		if checked {
			d.Bg(prs)
		}
	case o.Color != (RGBA{}):
		d.Bg(accent).HoverBg(tint(accent, 1, 0.15)).PressBg(tint(accent, 0, 0.2))
		if checked {
			d.Bg(tint(accent, 0, 0.2))
		}
	case o.Highlighted:
		d.Bg(AccentHover).HoverBg(RGB(0x5c7cfa)).PressBg(Accent)
	default:
		d.Bg(Accent).HoverBg(AccentHover).PressBg(AccentPress)
		if checked {
			d.Bg(AccentPress)
		}
	}
	if o.TextColor != (RGBA{}) {
		text = o.TextColor
	}
	if o.Disabled {
		text.A *= 0.4
		d.Opacity(0.6)
	} else if o.OnClick != nil || o.Checked != nil {
		d.OnClick(func() {
			if o.Checked != nil {
				o.Checked.Set(!o.Checked.Get())
			}
			if o.OnClick != nil {
				o.OnClick()
			}
		})
	}
	var kids []Element
	if o.Icon != nil {
		kids = append(kids, o.Icon)
	}
	if label != "" || len(kids) == 0 {
		kids = append(kids, Text(label, text).Truncate())
	}
	if len(kids) > 1 {
		d.Row().Gap(6)
	}
	d.Clip().Children(kids...)
}

// tint moves c a fraction amt toward white (to 1) or black (to 0).
func tint(c RGBA, to, amt float32) RGBA {
	return RGBA{R: c.R + (to-c.R)*amt, G: c.G + (to-c.G)*amt, B: c.B + (to-c.B)*amt, A: c.A}
}

// Switch is an on/off toggle bound to checked (QML's Switch): a pill with a
// knob that moves across; a click (or Enter/Space with focus) flips it.
func Switch(checked *core.Property[bool], label string, onToggle ...func(bool)) Element {
	return SwitchOpts{}.build(checked, label, onToggle)
}

// SwitchOpts configures a switch; Disabled shows it dimmed and inert.
type SwitchOpts struct {
	Disabled bool
}

// Build is Switch with options.
func (o SwitchOpts) Build(checked *core.Property[bool], label string, onToggle ...func(bool)) Element {
	return o.build(checked, label, onToggle)
}

func (o SwitchOpts) build(checked *core.Property[bool], label string, onToggle []func(bool)) Element {
	track := Div().Size(40, 22).Rounded(11).ItemsCenter().P(2).Focusable()
	if checked.Get() {
		track.Bg(Accent).Justify(AlignEnd)
	} else {
		track.Bg(RGB(0xb0b4bf)).Justify(AlignStart)
	}
	track.Children(Div().Size(18, 18).Rounded(9).Bg(RGB(0xffffff)))
	if o.Disabled {
		track.Opacity(0.5)
	} else {
		track.OnClick(func() {
			v := !checked.Get()
			checked.Set(v)
			for _, f := range onToggle {
				f(v)
			}
		})
	}
	if label == "" {
		return track
	}
	return Div().Row().ItemsCenter().Gap(8).Children(track, Text(label, RGB(0x111111)))
}

// Pane, Frame and Page are boxes with a name — sugar for Div — so a view
// reads like the QML it mirrors. A Frame has the kit's thin border; a Page
// stacks an optional header and footer around content that fills the rest.
func Pane() *DivEl  { return Div().P(12) }
func Frame() *DivEl { return Div().P(12).Border(1, RGB(0xced4da)).Rounded(4) }

// Page lays header, content and footer out top to bottom, the content
// growing into what they leave; header or footer may be nil.
func Page(header, content, footer Element) *DivEl {
	d := Div().Col()
	if header != nil {
		d.Children(header)
	}
	if c, ok := content.(*DivEl); ok {
		c.Grow()
	}
	d.Children(content)
	if footer != nil {
		d.Children(footer)
	}
	return d
}
