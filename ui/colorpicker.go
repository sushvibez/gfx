package ui

import (
	"fmt"
	"strings"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
)

// ColorOpts configures a ColorField.
type ColorOpts struct {
	Width float32 // the field's; 0: 140
	// Swatches are the preset colours under the picker (nil: a default
	// set of greys and hues; empty: none).
	Swatches []RGBA
	// OnChange runs with each colour the user picks: continuously while a
	// handle is dragged, on a swatch, and on Enter in the hex field.
	OnChange func(c RGBA)
	// OnDone runs once a pick is finished: a drag released, a swatch
	// clicked, Enter in the hex field — where a page commits one undo step.
	OnDone   func(c RGBA)
	Disabled bool
}

// ColorState is a ColorField's retained state: whether the picker is open,
// the hue, saturation and value it shows (kept apart from the colour so a
// grey keeps the hue it was dragged from) and the hex field.
type ColorState struct {
	Open core.Property[bool]
	Hex  *InputState

	h, s, v float32
	synced  RGBA
	have    bool
	rev     core.Property[int] // bumped when h/s/v move, to repaint
	// sent are the colours this picker has handed the page since it
	// opened: the page's value can come back a frame or an event late,
	// and one of our own on its way back is not a change to re-seed from.
	sent map[string]bool
}

// NewColorState makes a ColorField's state.
func NewColorState() *ColorState { return &ColorState{Hex: NewInput("")} }

// DefaultSwatches are a ColorField's presets: greys, then twelve hues.
var DefaultSwatches = []RGBA{
	RGB(0xffffff), RGB(0xd9d9d9), RGB(0xa6a6a6), RGB(0x737373), RGB(0x404040), RGB(0x000000),
	RGB(0xfff1cc), RGB(0xffd9a0), RGB(0xffb070), RGB(0xa8c8ff), RGB(0x7f9cbf), RGB(0x5b6b7a),
	RGB(0xe53935), RGB(0xfb8c00), RGB(0xfdd835), RGB(0x7cb342), RGB(0x43a047), RGB(0x00897b),
	RGB(0x00acc1), RGB(0x1e88e5), RGB(0x3949ab), RGB(0x8e24aa), RGB(0xd81b60), RGB(0x6d4c41),
}

// ColorHex is c as "#rrggbb".
func ColorHex(c RGBA) string {
	b := func(v float32) int { return int(math32.Round(max(0, min(1, v)) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", b(c.R), b(c.G), b(c.B))
}

// ColorField is a colour well bound to c (QML has none built in; this is
// a ColorDialog in a drop-down): a swatch and the hex code, which a click
// opens into a picker under it — a saturation × value square, a hue bar,
// the hex field and preset swatches. Esc or a click outside closes it.
func ColorField(c RGBA, st *ColorState, o ColorOpts) Element {
	w := o.Width
	if w <= 0 {
		w = 140
	}
	tm := CurrentTheme()
	c.A = 1
	// A colour the page changed (or the first one) re-seeds the picker; one
	// the picker made itself keeps its hue.
	if !st.Open.Get() {
		st.sent = nil
	}
	if !st.have || c != st.synced && !st.sent[ColorHex(c)] {
		h, s, v := rgbToHSV(c)
		if s > 0 && v > 0 || !st.have {
			st.h = h
		}
		if v > 0 || !st.have {
			st.s = s
		}
		st.v, st.synced, st.have = v, c, true
		if !st.Hex.HasFocus() {
			st.Hex.SetText(ColorHex(c))
		}
	}
	emit := func(nc RGBA, done bool) {
		nc.A = 1
		st.synced = nc
		if st.sent == nil {
			st.sent = map[string]bool{}
		}
		st.sent[ColorHex(nc)] = true
		if !st.Hex.HasFocus() {
			st.Hex.SetText(ColorHex(nc))
		}
		st.rev.Set(st.rev.Get() + 1)
		if o.OnChange != nil {
			o.OnChange(nc)
		}
		if done && o.OnDone != nil {
			o.OnDone(nc)
		}
	}
	fromHSV := func(done bool) { emit(hsvToRGB(st.h, st.s, st.v), done) }
	// set takes a colour from a swatch or the hex field; a grey keeps the
	// hue the picker had, so dragging out of it again starts there.
	set := func(nc RGBA) {
		h, s, v := rgbToHSV(nc)
		if s > 0 && v > 0 {
			st.h = h
		}
		st.s, st.v = s, v
		emit(nc, true)
	}

	shown := c
	if st.Open.Get() {
		shown = st.synced // the picker's own, ahead of a page that lags
	}
	well := Div().Size(18, 18).Rounded(3).Bg(shown).Border(1, tm.Border)
	box := Div().Size(w, 28).Bg(tm.Field).Border(1, tm.Border).Rounded(4).
		Row().ItemsCenter().PadXY(5, 0).Gap(8).
		Children(well, Text(ColorHex(shown), tm.Text).Truncate().Grow())
	box.Access(Access{Role: RoleButton, Name: "Colour", Value: ColorHex(c), Checked: -1,
		Expanded: checkState(st.Open.Get()), Disabled: o.Disabled})
	if o.Disabled {
		return box.Opacity(0.5)
	}
	box.Cursor(app.CursorPointer).OnClick(func() { st.Open.Set(!st.Open.Get()) })
	if !st.Open.Get() {
		return box
	}
	_ = st.rev.Get() // repaint when the handles move

	const pw, sqH, barH = 216, 150, 14
	inner := float32(pw - 16)

	// Saturation (→) by value (↑) at the hue.
	sq := Div().Size(inner, sqH).Rounded(4).Cursor(app.CursorPointer)
	sq.Children(NewCanvas(inner, sqH, func(r *render.Renderer2D, rc Rect) {
		hue := hsvToRGB(st.h, 1, 1)
		rect := func() *render.Path {
			return render.NewPath().MoveTo(rc.X, rc.Y).LineTo(rc.X+rc.W, rc.Y).
				LineTo(rc.X+rc.W, rc.Y+rc.H).LineTo(rc.X, rc.Y+rc.H).Close()
		}
		r.FillRect(rc.X, rc.Y, rc.W, rc.H, render.RGBA(hue))
		r.FillPathGradient(rect(), render.RGBA{R: 1, G: 1, B: 1, A: 1}, render.RGBA{R: 1, G: 1, B: 1, A: 0}, 0)
		r.FillPathGradient(rect(), render.RGBA{}, render.RGBA{A: 1}, math32.Pi/2)
		x, y := rc.X+st.s*rc.W, rc.Y+(1-st.v)*rc.H
		ring := render.NewPath()
		for k := 0; k <= 24; k++ {
			a := float32(k) / 24 * 2 * math32.Pi
			if k == 0 {
				ring.MoveTo(x+6*math32.Cos(a), y+6*math32.Sin(a))
			} else {
				ring.LineTo(x+6*math32.Cos(a), y+6*math32.Sin(a))
			}
		}
		ring.Close()
		r.StrokePathRound(ring, 3, render.RGBA{A: 0.55})
		r.StrokePathRound(ring, 1.5, render.RGBA{R: 1, G: 1, B: 1, A: 1})
	}))
	sq.OnPointer(func(x, y float32, phase DragPhase) {
		rc := sq.rect
		if rc.W <= 0 || rc.H <= 0 {
			return
		}
		st.s = max(0, min(1, (x-rc.X)/rc.W))
		st.v = 1 - max(0, min(1, (y-rc.Y)/rc.H))
		fromHSV(phase == DragEnd)
	})

	// Hue, left to right.
	bar := Div().Size(inner, barH).Cursor(app.CursorPointer)
	bar.Children(NewCanvas(inner, barH, func(r *render.Renderer2D, rc Rect) {
		for i := range 6 {
			x0 := rc.X + rc.W*float32(i)/6
			x1 := rc.X + rc.W*float32(i+1)/6
			p := render.NewPath().MoveTo(x0, rc.Y).LineTo(x1, rc.Y).LineTo(x1, rc.Y+rc.H).LineTo(x0, rc.Y+rc.H).Close()
			r.FillPathGradient(p, render.RGBA(hsvToRGB(float32(i)/6, 1, 1)), render.RGBA(hsvToRGB(float32(i+1)/6, 1, 1)), 0)
		}
		x := rc.X + st.h*rc.W
		r.FillRoundRect(x-3, rc.Y-2, 6, rc.H+4, 2, render.RGBA{A: 0.55})
		r.FillRoundRect(x-2, rc.Y-1, 4, rc.H+2, 1.5, render.RGBA{R: 1, G: 1, B: 1, A: 1})
	}))
	bar.OnPointer(func(x, _ float32, phase DragPhase) {
		rc := bar.rect
		if rc.W <= 0 {
			return
		}
		st.h = max(0, min(1, (x-rc.X)/rc.W))
		if st.s == 0 && st.v > 0 { // a grey takes the hue it is dragged to
			st.s = 1
		}
		fromHSV(phase == DragEnd)
	})

	// The hex field and the colour as it stands.
	st.Hex.OnSubmit(func(s string) {
		pc, err := ParseColor(strings.TrimSpace(s))
		if err != nil {
			st.Hex.SetText(ColorHex(st.synced))
			return
		}
		set(pc)
	})
	st.Hex.OnCancel(func() { st.Open.Set(false) })
	hexRow := Div().Row().ItemsCenter().Gap(8).Children(
		Div().Size(28, 28).Rounded(4).Bg(st.synced).Border(1, tm.Border),
		Input(st.Hex, inner-36).Bg(tm.Input).TextColor(tm.InputText).Border(1, tm.InputBorder).Rounded(4),
	)

	panel := Div().Col().W(pw).P(8).Gap(10).Bg(tm.Menu).Border(1, tm.MenuBorder).Rounded(6).
		Shadow(12, 0, 4, RGBA{A: 0.25}).Children(sq, bar, hexRow)

	sw := o.Swatches
	if sw == nil {
		sw = DefaultSwatches
	}
	if len(sw) > 0 {
		const cell, gap = 16, 4
		perRow := int((inner + gap) / (cell + gap))
		grid := Div().Col().Gap(gap)
		var row *DivEl
		for i, c := range sw {
			if i%perRow == 0 {
				row = Div().Row().Gap(gap)
				grid.Children(row)
			}
			c := c
			cellEl := Div().Size(cell, cell).Rounded(3).Bg(c).Border(1, tm.Border).
				Cursor(app.CursorPointer).OnClick(func() {
				set(c)
			})
			if ColorHex(c) == ColorHex(st.synced) {
				cellEl.Border(2, tm.Accent)
			}
			row.Children(cellEl)
		}
		panel.Children(grid)
	}
	panel.OnKeyAccept(func(ev app.KeyEvent) bool {
		if ev.Down && ev.Key == app.KeyEscape {
			st.Open.Set(false)
			return true
		}
		return false
	})
	return Div().Children(box,
		Overlay(panel, 0, 0).AnchorBelow(box, 2).SnapToWindow(4).OnClickOutside(func() { st.Open.Set(false) }))
}

// rgbToHSV is c as hue, saturation and value, each 0…1.
func rgbToHSV(c RGBA) (h, s, v float32) {
	mx := max(c.R, c.G, c.B)
	mn := min(c.R, c.G, c.B)
	v = mx
	d := mx - mn
	if mx > 0 {
		s = d / mx
	}
	if d == 0 {
		return 0, s, v
	}
	switch mx {
	case c.R:
		h = (c.G - c.B) / d
		if h < 0 {
			h += 6
		}
	case c.G:
		h = (c.B-c.R)/d + 2
	default:
		h = (c.R-c.G)/d + 4
	}
	return h / 6, s, v
}

// hsvToRGB is the opaque colour at hue, saturation and value (0…1; a hue
// of 1 is red again).
func hsvToRGB(h, s, v float32) RGBA {
	h = (h - math32.Floor(h)) * 6
	i := int(h) % 6
	f := h - math32.Floor(h)
	p, q, t := v*(1-s), v*(1-s*f), v*(1-s*(1-f))
	switch i {
	case 0:
		return RGBA{R: v, G: t, B: p, A: 1}
	case 1:
		return RGBA{R: q, G: v, B: p, A: 1}
	case 2:
		return RGBA{R: p, G: v, B: t, A: 1}
	case 3:
		return RGBA{R: p, G: q, B: v, A: 1}
	case 4:
		return RGBA{R: t, G: p, B: v, A: 1}
	}
	return RGBA{R: v, G: p, B: q, A: 1}
}
