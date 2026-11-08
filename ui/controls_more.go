package ui

import (
	"math"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
)

// More of the kit: a round button, a press-and-hold button, a painted dial,
// a page indicator, separators, a tool bar and a group box.

// RoundButton is a circular button (QML's RoundButton): 40 across unless
// the options size it, the label (often a glyph) centred.
func RoundButton(label string, o ButtonOpts) *DivEl {
	if o.Width == 0 {
		o.Width = 40
	}
	if o.Height == 0 {
		o.Height = o.Width
	}
	if o.Radius == 0 {
		o.Radius = min(o.Width, o.Height) / 2
	}
	return Button(label, o)
}

// DelayState is a DelayButton's retained state: how far the hold has got
// (0..1) and whether this hold has fired. Keep one per button.
type DelayState struct {
	prog    core.Property[float32]
	pressed bool
	fired   bool
}

// Progress is how far the current hold has got, 0..1.
func (s *DelayState) Progress() float32 { return s.prog.Get() }

// DelayOpts configures a DelayButton.
type DelayOpts struct {
	ButtonOpts
	// Delay is how long it must be held; 0 is Qt's 300 ms.
	Delay time.Duration
	// OnActivated runs once the hold reaches the end.
	OnActivated func()
}

// DelayButton is a button that has to be held (QML's DelayButton): a bar
// under it fills while it is pressed and OnActivated runs when it is full;
// letting go early drains it again.
func DelayButton(label string, st *DelayState, o DelayOpts) *DivEl {
	delay := o.Delay
	if delay <= 0 {
		delay = 300 * time.Millisecond
	}
	tm := themeOr(o.Theme)
	btn := Button(label, o.ButtonOpts)
	if !o.Disabled {
		btn.OnPressChanged(func(p bool) {
			if p != st.pressed {
				st.pressed = p
				left := 1 - st.prog.Get()
				if p {
					st.fired = false
					core.Tween(&st.prog, 1, time.Duration(float32(delay)*left), core.EaseLinear)
				} else if st.prog.Get() < 1 {
					core.Tween(&st.prog, 0, time.Duration(float32(150*time.Millisecond)*(1-left)), core.EaseOutCubic)
				} else {
					st.prog.Set(0)
				}
			}
			if p && !st.fired && st.prog.Get() >= 1 {
				st.fired = true
				if o.OnActivated != nil {
					o.OnActivated()
				}
			}
		})
	}
	w := o.Width
	if w == 0 {
		w = 96
	}
	bar := Div().Size(w, 4).Rounded(2).Bg(tm.Border).Row().Justify(AlignStart).
		Children(Div().Size(w*st.prog.Get(), 4).Rounded(2).Bg(tm.Accent))
	return Div().Col().Gap(3).Children(btn, bar)
}

// DialOpts configures a DialControl.
type DialOpts struct {
	Size float32 // across; 0 is 64
	// Step and Snap quantise the value (QML's stepSize and SnapAlways).
	Step    float32
	Snap    bool
	OnMoved func() // after the user turns it
	// Disabled draws it without taking input.
	Disabled bool
	Theme    *Theme
}

// DialControl is a rotary knob (QML's Dial) bound to value in [from, to]:
// a track sweeping 280° from lower left to lower right, the value as an
// accent arc with a handle on its end; dragging round it turns it.
func DialControl(value *core.Property[float32], from, to float32, o DialOpts) *DivEl {
	size := o.Size
	if size <= 0 {
		size = 64
	}
	pos := float32(0)
	if to != from {
		pos = (value.Get() - from) / (to - from)
	}
	face := DialFace(min(max(pos, 0), 1), size, themeOr(o.Theme))
	box := Div().Size(size, size).AnchorMode().Children(Anchored(face, "", Anchors{Fill: "parent"}))
	if o.Disabled {
		return box.Opacity(0.5)
	}
	dl := Dial(value, from, to).Step(o.Step, o.Snap)
	if o.OnMoved != nil {
		dl.OnMoved(o.OnMoved)
	}
	return box.Children(Anchored(dl, "", Anchors{Fill: "parent"}))
}

// DialFace draws a dial at pos (0..1) without taking input — what a
// disabled dial, or one a host turns itself, shows.
func DialFace(pos, size float32, tm Theme) Element {
	return NewCanvas(size, size, func(r *render.Renderer2D, rect Rect) {
		d := min(rect.W, rect.H)
		if d <= 0 {
			return
		}
		cx, cy := rect.X+rect.W/2, rect.Y+rect.H/2
		lw := max(d*0.09, 2)
		rad := (d - lw) / 2
		const start, sweep = -140.0, 280.0 // degrees, 0 = 12 o'clock
		arc := func(fromDeg, toDeg float64) *render.Path {
			p := render.NewPath()
			steps := 48
			for i := 0; i <= steps; i++ {
				a := (fromDeg + (toDeg-fromDeg)*float64(i)/float64(steps) - 90) * math.Pi / 180
				p.LineTo(cx+rad*float32(math.Cos(a)), cy+rad*float32(math.Sin(a)))
			}
			return p
		}
		r.StrokePathRound(arc(start, start+sweep), lw, tm.Ring)
		if pos > 0 {
			r.StrokePathRound(arc(start, start+sweep*float64(pos)), lw, tm.Accent)
		}
		// The handle: a dot on the track at the value.
		ha := (start + sweep*float64(pos) - 90) * math.Pi / 180
		hx, hy := cx+rad*float32(math.Cos(ha)), cy+rad*float32(math.Sin(ha))
		hp := render.NewPath()
		for i := 0; i <= 24; i++ {
			a := 2 * math.Pi * float64(i) / 24
			hp.LineTo(hx+lw*0.75*float32(math.Cos(a)), hy+lw*0.75*float32(math.Sin(a)))
		}
		r.FillPath(hp.Close(), tm.OnAccent)
	})
}

// PageIndicatorOpts configures a PageIndicator.
type PageIndicatorOpts struct {
	Spacing float32 // between dots; 0 is 6
	// Dot draws one page's mark; nil is an 8 px dot, faded unless current.
	Dot func(i int, current bool) Element
}

// PageIndicator is a row of dots, one per page, the current one solid
// (QML's PageIndicator). With onSelect, clicking a dot picks its page.
func PageIndicator(count, current int, onSelect func(int), o PageIndicatorOpts) *DivEl {
	gap := o.Spacing
	if gap == 0 {
		gap = 6
	}
	tm := CurrentTheme()
	row := Div().Row().ItemsCenter().Gap(gap)
	for i := 0; i < count; i++ {
		var dot Element
		if o.Dot != nil {
			dot = o.Dot(i, i == current)
		} else {
			c := tm.Indicator
			if i != current {
				c.A *= 0.45
			}
			dot = Div().W(8).H(8).Rounded(4).Bg(c)
		}
		if onSelect != nil {
			pick := i
			dot = Div().ItemsCenter().JustifyCenter().OnClick(func() { onSelect(pick) }).Children(dot)
		}
		row.Children(dot)
	}
	return row
}

// Separator is a hairline in the theme's border colour (QML's
// MenuSeparator and ToolSeparator): across its container, or a 24 px
// upright one between the buttons of a tool bar.
func Separator(vertical bool) *DivEl {
	c := CurrentTheme().Border
	if vertical {
		return Div().Size(1, 24).Bg(c)
	}
	return Div().H(1).Grow().Bg(c)
}

// ToolBar is a 48 px band holding a row of tool buttons (QML's ToolBar),
// sugar for a Div.
func ToolBar(children ...Element) *DivEl {
	return Div().Row().ItemsCenter().H(48).PadXY(6, 0).Gap(4).Bg(CurrentTheme().Band).
		Children(children...)
}

// GroupBox is a titled Frame (QML's GroupBox): the title above, the
// content in the frame.
func GroupBox(title string, content ...Element) *DivEl {
	tm := CurrentTheme()
	return Div().Col().Gap(6).Children(
		Text(title, tm.Text).Bold(),
		Frame().Col().Gap(8).Children(content...))
}
