package ui

import (
	"math"

	"github.com/sushvibez/gfx/core"
)

// Checkbox renders a toggle bound to a bool property; clicking the box or
// the label flips it. Optional onToggle callbacks run after the flip with
// the new value (QML's toggled() signal).
func Checkbox(checked *core.Property[bool], label string, onToggle ...func(bool)) Element {
	tm := CurrentTheme()
	box := Div().Size(20, 20).Rounded(5)
	if checked.Get() {
		box.Bg(tm.Accent).Children(
			Div().Size(8, 8).Bg(tm.OnAccent).Rounded(2),
		).JustifyCenter().ItemsCenter()
	} else {
		box.Border(2, tm.Outline)
	}
	return Div().Row().Gap(10).ItemsCenter().
		OnClick(func() {
			v := !checked.Get()
			checked.Set(v)
			for _, f := range onToggle {
				f(v)
			}
		}).
		Children(box, Text(label, tm.Label))
}

// Slider renders a horizontal slider bound to a float property in [0,1];
// Range remaps it to any [from,to]. Press or drag anywhere on it to set
// the value.
func Slider(value *core.Property[float32], w float32) *SliderEl {
	return &SliderEl{value: value, w: w, from: 0, to: 1, tm: CurrentTheme()}
}

// SliderEl is the per-frame element of a slider property.
type SliderEl struct {
	tm       Theme // the theme it was built in (it paints later, outside any scope)
	value    *core.Property[float32]
	step     float32 // quantum the value moves in (QML stepSize); 0 = continuous
	snap     bool    // ... and whether a drag snaps to it (QML SnapAlways)
	w        float32
	from, to float32 // value range the track spans (QML from/to)
	moved    func()  // fired after the USER changes the value (QML onMoved)
	bare     bool    // paint nothing: something else draws this slider
	vertical bool    // the track runs bottom-to-top (QML orientation: Qt.Vertical)
	h        float32 // stated length of a vertical track (0 = fill)
	// Look: a slider belongs to the app's palette, not to this package's.
	// Zero values keep the defaults below.
	trackC, fillC, knobC RGBA
	hasColors            bool
	trackPx, knobPx      float32
	disabled             bool
	grow                 float32
	crossFill            bool
	rect                 Rect
}

// Colors paints the slider in the app's own palette — the track behind it,
// the part left of the handle, and the handle (QML: the style's background
// rectangle, its filled child, and the handle).
func (e *SliderEl) Colors(track, fill, knob RGBA) *SliderEl {
	e.trackC, e.fillC, e.knobC, e.hasColors = track, fill, knob, true
	return e
}

// Thickness sets the track's thickness and the handle's diameter. 0 keeps
// the defaults (4px track, a 7px-radius knob).
func (e *SliderEl) Thickness(track, knob float32) *SliderEl {
	e.trackPx, e.knobPx = track, knob
	return e
}

// Disabled greys the slider out and stops it taking the pointer — QML's
// `enabled: false`, which every control page demonstrates.
func (e *SliderEl) Disabled() *SliderEl { e.disabled = true; return e }

// W sets the track's length across a horizontal slider, after the fact —
// the constructor takes one, but a slider built by a helper needs to be
// re-sized by the page that places it.
func (e *SliderEl) W(px float32) *SliderEl { e.w = px; return e }

// Grow lets the slider take the leftover width of its row — QML's
// `Layout.fillWidth: true`, which is how nearly every slider on a control
// page is laid out.
func (e *SliderEl) Grow() *SliderEl { e.grow = 1; return e }

// GrowBy is Grow with a weight; see DivEl.GrowBy.
func (e *SliderEl) GrowBy(f float32) *SliderEl { e.grow = f; return e }

func (e *SliderEl) growFactor() float32 { return e.grow }

// CrossFill stretches the slider across its parent's CROSS axis —
// `Layout.fillWidth: true` on a slider inside a ColumnLayout, which is how
// every captioned slider on a control page is written.
func (e *SliderEl) CrossFill() *SliderEl { e.crossFill = true; return e }

func (e *SliderEl) crossFills() bool { return e.crossFill }

// Vertical lays the slider out bottom-to-top — QML's
// `orientation: Qt.Vertical`, whose zero is at the BOTTOM. Without it a
// vertical slider read the pointer's x across its narrow track, so dragging
// it up did nothing and pressing it anywhere jumped the value to the middle.
func (e *SliderEl) Vertical() *SliderEl { e.vertical = true; return e }

// H sets the slider's length along a vertical track (a horizontal one takes
// its height from the knob).
func (e *SliderEl) H(px float32) *SliderEl { e.h = px; return e }

// Bare makes the slider paint nothing while still handling press and drag —
// for a control whose visuals come from elsewhere (a QML style's groove and
// handle delegates), with this element supplying the interaction.
func (e *SliderEl) Bare() *SliderEl { e.bare = true; return e }

// Step sets the quantum the value moves in (QML's stepSize), and whether a
// DRAG is held to it (QML's snapMode: Slider.SnapAlways). With snap off the
// handle moves freely — stepSize then only governs keys and the wheel,
// which is Qt's default (NoSnap).
func (e *SliderEl) Step(step float32, snap bool) *SliderEl {
	e.step, e.snap = step, snap
	return e
}

// snapTo quantises v to the slider's step, measured from `from` (QML snaps
// to from + n*stepSize, not to multiples of the step in absolute terms).
func snapTo(v, from, step float32) float32 {
	if step <= 0 {
		return v
	}
	n := float32(int((v-from)/step + 0.5))
	if v < from {
		n = float32(int((v-from)/step - 0.5))
	}
	return from + n*step
}

// OnMoved registers a callback fired after user interaction changes the
// value (not on programmatic sets) — QML Slider's moved() signal.
func (e *SliderEl) OnMoved(fn func()) *SliderEl { e.moved = fn; return e }

// Range makes the slider span [from, to]: the bound property holds ranged
// values and the track position is the normalized fraction.
func (e *SliderEl) Range(from, to float32) *SliderEl {
	if to != from {
		e.from, e.to = from, to
	}
	return e
}

const sliderKnob = 9 // knob radius

func (e *SliderEl) measure(*Context) (float32, float32) {
	if e.vertical {
		return max(e.w, 2*sliderKnob+4), e.h
	}
	return e.w, 2*sliderKnob + 4
}

func (e *SliderEl) layout(_ *Context, r Rect) { e.rect = r }

// valueAtY maps a pointer y to the 0..1 position down a vertical track.
func valueAtY(r Rect, y float32) float32 {
	t := (y - r.Y - sliderKnob) / (r.H - 2*sliderKnob)
	return min(max(t, 0), 1)
}

// valueAt maps a pointer x to the 0..1 value for this slider's rect.
func valueAt(r Rect, x float32) float32 {
	t := (x - r.X - sliderKnob) / (r.W - 2*sliderKnob)
	return min(max(t, 0), 1)
}

// paintSig: the value and the look (see damage.go).
func (e *SliderEl) paintSig() uint64 {
	h := newSig(0x511)
	if e.value != nil {
		h.f32(e.value.Get())
	}
	h.f32(e.from)
	h.f32(e.to)
	h.b(e.bare)
	h.b(e.vertical)
	h.b(e.hasColors)
	h.col(e.trackC)
	h.col(e.fillC)
	h.col(e.knobC)
	h.col(e.tm.Track) // the theme it paints in, when it has no colours of its own
	h.col(e.tm.Accent)
	h.col(e.tm.Knob)
	h.col(e.tm.KnobDisabled)
	h.f32(e.trackPx)
	h.f32(e.knobPx)
	h.b(e.disabled)
	return uint64(h)
}

func (e *SliderEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	if e.bare {
		return
	}
	tm := e.tm
	track, fill, knob := tm.Track, tm.Accent, tm.Knob
	if e.hasColors {
		track, fill, knob = e.trackC, e.fillC, e.knobC
	}
	if e.disabled {
		fill, knob = track, tm.KnobDisabled
	}
	kr := float32(sliderKnob)
	if e.knobPx > 0 {
		kr = e.knobPx / 2
	}
	th := float32(4)
	if e.trackPx > 0 {
		th = e.trackPx
	}
	v := (e.value.Get() - e.from) / (e.to - e.from)
	v = min(max(v, 0), 1)
	if e.vertical {
		// A vertical slider's zero is at the BOTTOM, so what the style
		// draws and the handle both ride visualPosition (1 - position):
		// the filled child is anchored to the track's TOP and is
		// visualPosition tall, which means a LOW value fills MORE of the
		// track (Qt's own render of the corpus' 30/60/90 sliders shows
		// exactly that).
		vp := 1 - v
		cx := e.rect.X + e.rect.W/2
		trackY, trackH := e.rect.Y+kr, e.rect.H-2*kr
		ctx.R.FillRoundRect(cx-th/2, trackY, th, trackH, th/2, track)
		ctx.R.FillRoundRect(cx-th/2, trackY, th, trackH*vp, th/2, fill)
		knobY := trackY + trackH*vp
		ctx.R.FillRoundRect(cx-kr, knobY-kr, 2*kr, 2*kr, kr, knob)
		return
	}
	cy := e.rect.Y + e.rect.H/2
	trackX, trackW := e.rect.X+kr, e.rect.W-2*kr
	ctx.R.FillRoundRect(trackX, cy-th/2, trackW, th, th/2, track)
	ctx.R.FillRoundRect(trackX, cy-th/2, trackW*v, th, th/2, fill)
	knobX := trackX + trackW*v
	ctx.R.FillRoundRect(knobX-kr, cy-kr, 2*kr, 2*kr, kr, knob)
}

func (e *SliderEl) hit(x, y float32) Element {
	if e.disabled {
		return nil
	}
	if e.rect.contains(x, y) {
		return e
	}
	return nil
}

func (e *SliderEl) onClick() func() { return func() {} }
func (e *SliderEl) bounds() Rect    { return e.rect }

func (e *SliderEl) fixedWH() (float32, float32) { return e.w, e.h }

// RangeSlider is a slider with two handles bounding a range — QML's
// RangeSlider, whose `first` and `second` each carry their own value. The
// handles cannot cross: each clamps against the other.
func RangeSlider(first, second *core.Property[float32], w float32) *RangeSliderEl {
	return &RangeSliderEl{first: first, second: second, w: w, from: 0, to: 1, tm: CurrentTheme()}
}

// RangeSliderEl is the per-frame element of a two-handled slider.
type RangeSliderEl struct {
	tm            Theme // the theme it was built in
	first, second *core.Property[float32]
	w             float32
	from, to      float32
	vertical      bool
	step          float32 // quantum each handle moves in (QML stepSize)
	snap          bool    // ... and whether a drag is held to it
	bare          bool    // paint nothing: a style's own delegates draw this
	moved         func()
	rect          Rect
}

// Range makes the slider span [from, to].
func (e *RangeSliderEl) Range(from, to float32) *RangeSliderEl {
	if to != from {
		e.from, e.to = from, to
	}
	return e
}

// Vertical lays the slider out bottom-to-top (QML orientation: Qt.Vertical).
func (e *RangeSliderEl) Vertical() *RangeSliderEl { e.vertical = true; return e }

// Step sets the quantum each handle moves in, and whether a drag is held to
// it — QML's stepSize with snapMode: RangeSlider.SnapAlways. See
// SliderEl.Step.
func (e *RangeSliderEl) Step(step float32, snap bool) *RangeSliderEl {
	e.step, e.snap = step, snap
	return e
}

// Bare makes the slider paint nothing while still handling press and drag —
// a styled RangeSlider draws through its own groove and handle delegates.
func (e *RangeSliderEl) Bare() *RangeSliderEl { e.bare = true; return e }

// OnMoved registers a callback fired after a drag changes a value.
func (e *RangeSliderEl) OnMoved(fn func()) *RangeSliderEl { e.moved = fn; return e }

func (e *RangeSliderEl) measure(*Context) (float32, float32) {
	if e.vertical {
		return 2*sliderKnob + 4, e.w
	}
	return e.w, 2*sliderKnob + 4
}

func (e *RangeSliderEl) layout(_ *Context, r Rect) { e.rect = r }

// frac normalizes a value to 0..1 on this slider's range.
func (e *RangeSliderEl) frac(v float32) float32 {
	if e.to == e.from {
		return 0
	}
	return min(max((v-e.from)/(e.to-e.from), 0), 1)
}

func (e *RangeSliderEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	if e.bare {
		return
	}
	a, b := e.frac(e.first.Get()), e.frac(e.second.Get())
	if b < a {
		a, b = b, a
	}
	tm := e.tm
	track, fill := tm.Track, tm.Accent
	knob := tm.Knob
	if e.vertical {
		cx := e.rect.X + e.rect.W/2
		trackY, trackH := e.rect.Y+sliderKnob, e.rect.H-2*sliderKnob
		ctx.R.FillRoundRect(cx-2, trackY, 4, trackH, 2, track)
		// Value grows upward: fraction 0 is the bottom of the track.
		y0 := trackY + trackH*(1-b)
		ctx.R.FillRoundRect(cx-2, y0, 4, trackH*(b-a), 2, fill)
		for _, f := range [2]float32{a, b} {
			cy := trackY + trackH*(1-f)
			ctx.R.FillRoundRect(cx-sliderKnob, cy-sliderKnob, 2*sliderKnob, 2*sliderKnob, sliderKnob, knob)
		}
		return
	}
	cy := e.rect.Y + e.rect.H/2
	trackX, trackW := e.rect.X+sliderKnob, e.rect.W-2*sliderKnob
	ctx.R.FillRoundRect(trackX, cy-2, trackW, 4, 2, track)
	ctx.R.FillRoundRect(trackX+trackW*a, cy-2, trackW*(b-a), 4, 2, fill)
	for _, f := range [2]float32{a, b} {
		kx := trackX + trackW*f
		ctx.R.FillRoundRect(kx-sliderKnob, cy-sliderKnob, 2*sliderKnob, 2*sliderKnob, sliderKnob, knob)
	}
}

// nearest picks the handle a press at (x, y) grabs: the closer of the two.
func (e *RangeSliderEl) nearest(x, y float32) *core.Property[float32] {
	t := valueAt(e.rect, x)
	if e.vertical {
		t = 1 - valueAtY(e.rect, y)
	}
	if abs32(t-e.frac(e.first.Get())) <= abs32(t-e.frac(e.second.Get())) {
		return e.first
	}
	return e.second
}

func (e *RangeSliderEl) hit(x, y float32) Element {
	if e.rect.contains(x, y) {
		return e
	}
	return nil
}

func (e *RangeSliderEl) onClick() func() { return func() {} }
func (e *RangeSliderEl) bounds() Rect    { return e.rect }

func (e *RangeSliderEl) fixedWH() (float32, float32) {
	if e.vertical {
		return 0, e.w
	}
	return e.w, 0
}

// DialEl is a rotary control's interaction: a press or drag anywhere on it
// sets the value from the pointer's ANGLE about its centre, sweeping from
// -140° to +140° through 12 o'clock, which is Qt's Dial. It paints nothing
// — a QML style's own background and handle delegates draw the dial, and
// this supplies the turning (the same division of labour as SliderEl.Bare).
type DialEl struct {
	value    *core.Property[float32]
	from, to float32
	step     float32
	snap     bool
	moved    func()
	size     float32
	rect     Rect
}

// Dial makes a rotary region for value, spanning [from, to].
func Dial(value *core.Property[float32], from, to float32) *DialEl {
	if to == from {
		to = from + 1
	}
	return &DialEl{value: value, from: from, to: to}
}

// Step sets the quantum the value turns in, and whether a turn is held to
// it (QML stepSize with snapMode: Dial.SnapAlways).
func (e *DialEl) Step(step float32, snap bool) *DialEl { e.step, e.snap = step, snap; return e }

// OnMoved registers the callback QML's moved() signal runs.
func (e *DialEl) OnMoved(fn func()) *DialEl { e.moved = fn; return e }

// Size states the region's own size (0 = fill what it is given).
func (e *DialEl) Size(px float32) *DialEl { e.size = px; return e }

// dialSweep is Qt's Dial: 280° of travel centred on 12 o'clock.
const dialStartDeg, dialSweepDeg = -140.0, 280.0

// valueAtAngle maps a pointer position to this dial's value. Outside the
// sweep (the gap at the bottom) the nearer end wins, as Qt's does.
func (e *DialEl) valueAtAngle(x, y float32) float32 {
	cx, cy := e.rect.X+e.rect.W/2, e.rect.Y+e.rect.H/2
	// Degrees clockwise from 12 o'clock.
	deg := math.Atan2(float64(x-cx), float64(cy-y)) * 180 / math.Pi
	t := (deg - dialStartDeg) / dialSweepDeg
	if t < 0 {
		// Below the sweep: the bottom gap splits at the far side.
		if t < -0.5 {
			t = 1
		} else {
			t = 0
		}
	}
	if t > 1 {
		t = 1
	}
	v := e.from + float32(t)*(e.to-e.from)
	if e.snap && e.step > 0 {
		v = snapTo(v, e.from, e.step)
	}
	return min(max(v, min(e.from, e.to)), max(e.from, e.to))
}

func (e *DialEl) measure(*Context) (float32, float32) { return e.size, e.size }
func (e *DialEl) layout(_ *Context, r Rect)           { e.rect = r }
func (e *DialEl) paint(*Context)                      {}
func (e *DialEl) bounds() Rect                        { return e.rect }
func (e *DialEl) onClick() func()                     { return nil }
func (e *DialEl) fixedWH() (float32, float32)         { return e.size, e.size }

func (e *DialEl) hit(x, y float32) Element {
	if e.rect.contains(x, y) {
		return e
	}
	return nil
}

// turnTo sets the value from a pointer position, reporting the move.
func (e *DialEl) turnTo(x, y float32) {
	if e.value == nil {
		return
	}
	e.value.Set(e.valueAtAngle(x, y))
	if e.moved != nil {
		e.moved()
	}
}
