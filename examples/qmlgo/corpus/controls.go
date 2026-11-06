package corpus

import (
	"strconv"
	"time"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// Button is the corpus' Button.qml, restated against the ui builder: a
// pill of palette.button, lightened on hover, darkened while pressed, and
// grey when disabled; flat drops the fill for a 10%/20% wash of the same
// colour and paints its text in it.
//
// ui has no Button of its own — a click target is a Div with an OnClick
// and hover/press fills — so every port that wants the corpus' button
// comes through here.
type Button struct {
	Text        string
	W, H        float32
	Color       ui.RGBA // palette.button (default mainColor)
	TextColor   ui.RGBA // palette.buttonText (default white)
	Highlighted bool
	Flat        bool
	Disabled    bool
	Checkable   bool
	Checked     *core.Property[bool]
	OnClick     func()
	// Label overrides the text for a state the caller tracks itself (the
	// "Pressed!"/"Hovering" buttons of the states card).
	OnHover func(bool)
	OnPress func(bool)
}

func (b Button) El() ui.Element {
	base := b.Color
	if base == (ui.RGBA{}) {
		base = Main
	}
	textC := b.TextColor
	if textC == (ui.RGBA{}) {
		textC = FontContrast
	}
	w, h := b.W, b.H
	if w == 0 {
		w = R(120)
	}
	if h == 0 {
		h = R(40)
	}
	checked := b.Checkable && b.Checked != nil && b.Checked.Get()

	d := ui.Div().W(w).H(h).Rounded(R(30)).ItemsCenter().JustifyCenter()
	switch {
	case b.Disabled:
		d = d.Bg(Inactive)
		if b.Flat {
			textC = Inactive
			d = d.Bg(ui.RGBA{})
		}
	case b.Flat:
		textC = base
		d = d.Bg(Alpha(base, 0)).HoverBg(Alpha(base, 0.1)).PressBg(Alpha(base, 0.2))
	case b.Highlighted || checked:
		d = d.Bg(Lighter(base, 1.15)).HoverBg(Lighter(base, 1.1)).PressBg(Darker(base, 1.2))
	default:
		d = d.Bg(base).HoverBg(Lighter(base, 1.1)).PressBg(Darker(base, 1.2))
	}
	label := ui.Text(b.Text, textC).SizePx(FontM)
	if b.Highlighted {
		label = label.Bold()
	}
	d = d.Children(label)
	if !b.Disabled {
		if b.OnHover != nil {
			d = d.OnHoverChanged(b.OnHover)
		}
		if b.OnPress != nil {
			d = d.OnPressChanged(b.OnPress)
		}
		d = d.OnClick(func() {
			if b.Checkable && b.Checked != nil {
				b.Checked.Set(!b.Checked.Get())
			}
			if b.OnClick != nil {
				b.OnClick()
			}
		})
	}
	return d
}

// ToolButton is ToolButton.qml: no background at all, the icon colorized
// to mainColor on the left, and the text after it in inactiveColor.
type ToolButton struct {
	Text    string
	Icon    *render.Image
	W, H    float32
	OnClick func()
}

func (t ToolButton) El() ui.Element {
	w, h := t.W, t.H
	if w == 0 {
		w = R(140)
	}
	if h == 0 {
		h = R(40)
	}
	var kids []ui.Element
	if t.Icon != nil {
		// Unsized: the style spells it `width: Style.resize(sourceSize.width)`.
		kids = append(kids, ui.Img(t.Icon, 0, 0).Colorize(Main))
	}
	if t.Text != "" {
		kids = append(kids, ui.Text(t.Text, Inactive).SizePx(FontM))
	}
	d := ui.Div().W(w).H(h).Row().ItemsCenter().Gap(R(10)).Children(kids...)
	if t.OnClick != nil {
		d = d.OnClick(t.OnClick)
	}
	return d
}

// Pulse is PulseButton.qml: a solid pill with a ring around it that
// scales 1 -> 1.3 while fading 0 -> 1 -> 0, on a 1500ms loop.
//
// ui has no animation objects — a gml page declares a SequentialAnimation
// and the engine ticks it, while a Go view is rebuilt every frame and
// reads the clock itself. core.AnimClock is that clock.
func Pulse(text string, color ui.RGBA, w, h float32, onClick func()) ui.Element {
	const period = 1500 * time.Millisecond
	// Keep the frame loop awake. A host that renders only on damage has
	// no way to know that this view reads the clock — the gml engine says
	// so through the same call for its own animators — and without it the
	// ring simply freezes at whatever phase the last damaged frame caught.
	core.SetExternalAnimation(pulseKey, true)
	t := float32(core.AnimClock()%period) / float32(period) // 0..1
	// Out over the first half, back over the second (two NumberAnimations
	// in sequence), and the ring's opacity rides the same triangle.
	tri := 2 * t
	if t > 0.5 {
		tri = 2 * (1 - t)
	}
	ring := ui.Div().W(w).H(h).Rounded(h/2).Border(R(2), color).
		Opacity(tri).ScaleBy(1 + 0.3*tri)
	face := ui.Div().W(w).H(h).Rounded(R(20)).Bg(color).
		PressBg(Darker(color, 1.2)).ItemsCenter().JustifyCenter().
		Children(ui.Text(text, FontContrast).SizePx(FontM))
	if onClick != nil {
		face = face.OnClick(onClick)
	}
	// The ring is BEHIND the face and must not push the layout around, so
	// the two are stacked in one box. A flex row would put them side by
	// side and Overlay places in WINDOW coordinates, so this is the
	// anchored container — the QML the style actually writes (both fill
	// the background Item, the ring centred in it).
	return ui.Div().W(w).H(h).AnchorMode().Children(
		ui.Anchored(ring, "ring", ui.Anchors{CenterIn: "parent"}),
		ui.Anchored(face, "face", ui.Anchors{Fill: "parent"}),
	)
}

// pulseKey identifies this package's clock-reading views to core.
var pulseKey = new(int)

// MenuEntry is one row of a Menu; an empty Text is a separator.
type MenuEntry struct {
	Text      string
	Checkable bool
	Checked   *core.Property[bool]
	OnClick   func()
}

// Menu is Menu.qml opened under its button: a dark rounded panel of
// entries, dismissed by a click anywhere else. ui has popups (a modal
// placed at a point) but no menu, so the rows are Divs.
func Menu(x, y float32, entries []MenuEntry, dismiss func()) ui.Element {
	var rows []ui.Element
	for _, e := range entries {
		if e.Text == "" {
			rows = append(rows, ui.Div().H(1).Bg(Grey).CrossFill())
			continue
		}
		mark := "   "
		if e.Checkable && e.Checked != nil && e.Checked.Get() {
			mark = " ✓ "
		}
		en := e
		rows = append(rows, ui.Div().Row().H(R(36)).PadXY(R(12), 0).ItemsCenter().
			HoverBg(Surface).Rounded(R(4)).CrossFill().
			Children(ui.Text(mark+e.Text, FontPrimary).SizePx(FontS)).
			OnClick(func() {
				if en.Checkable && en.Checked != nil {
					en.Checked.Set(!en.Checked.Get())
				}
				if en.OnClick != nil {
					en.OnClick()
				}
				dismiss()
			}))
	}
	panel := ui.Div().Col().Bg(BgDark).Rounded(R(6)).P(R(6)).MinW(R(160)).
		Border(1, Grey).Children(rows...)
	return ui.PositionedModal(panel, x, y, ui.RGBA{}, dismiss)
}

// Glow is GlowButton.qml: a SOLID pill of the glow colour with white text,
// and behind it a blurred, colorized copy of itself at glowIntensity — the
// halo. Qt spells that as a MultiEffect over the background rectangle, and
// ours is the same thing: an offscreen layer, blurred and colorized.
//
// st is that layer's retained target (one per button, held across frames);
// pass nil for a button that should not glow at all.
func Glow(st *ui.LayerState, text string, color ui.RGBA, w, h, intensity, radius float32, onClick func()) ui.Element {
	pill := func() *ui.DivEl {
		return ui.Div().W(w).H(h).Rounded(R(20)).Bg(color)
	}
	face := pill().PressBg(Darker(color, 1.2)).ItemsCenter().JustifyCenter().
		Children(ui.Text(text, FontContrast).SizePx(FontM))
	if onClick != nil {
		face = face.OnClick(onClick)
	}
	if st == nil || intensity <= 0 || radius <= 0 {
		return face
	}
	// blurMax is twice the radius in the markup, and `blur: 1.0` means all
	// of it.
	halo := ui.Layer(st, pill()).Blur(radius * 2).Colorize(color).Opacity(min(intensity, 1))
	return ui.Div().W(w).H(h).AnchorMode().Children(
		ui.Anchored(halo, "halo", ui.Anchors{Fill: "parent"}),
		ui.Anchored(face, "face", ui.Anchors{Fill: "parent"}),
	)
}

// Gradient is GradientButton.qml: a pill filled with a two-stop ramp.
func Gradient(text string, from, to ui.RGBA, w, h float32, onClick func()) ui.Element {
	d := ui.Div().W(w).H(h).Rounded(h/2).Gradient(from, to, 0).
		ItemsCenter().JustifyCenter().
		Children(ui.Text(text, FontContrast).SizePx(FontM))
	if onClick != nil {
		d = d.OnClick(onClick)
	}
	return d
}

// Neumorphic is NeumorphicButton.qml: the page's own background colour,
// raised by a light top edge and a dark bottom one.
func Neumorphic(text string, base ui.RGBA, w, h float32) ui.Element {
	return ui.Div().W(w).H(h).Rounded(h/2).Bg(base).
		BorderTop(1, Lighter(base, 1.6)).BorderBottom(1, Darker(base, 1.4)).
		ItemsCenter().JustifyCenter().
		Children(ui.Text(text, Main).SizePx(FontM)) // the style's contentItem is mainColor
}

// Slider is Slider.qml: a track of inactiveColor a quarter of the box's
// thickness, filled to the handle in mainColor, with a round mainColor
// handle half the box across.
func Slider(v *core.Property[float32], from, to float32, box float32) *ui.SliderEl {
	return ui.Slider(v, 0).Range(from, to).
		Colors(Inactive, Main, Main).Thickness(box/4, box/2)
}

// Dial is Dial.qml, which the corpus writes as a Canvas inside a styled
// T.Dial: a 280-degree track from 130 degrees, the travelled part in the
// progress colour with a soft wide copy under it, optional ticks, the
// value and its suffix in the middle, and a handle riding the arc.
//
// ui.Dial is INTERACTION ONLY (its paint draws nothing), so the visual is
// the page's to draw — which is right for a dial this customized, and the
// reason the port needs a canvas rather than a widget.
type DialSpec struct {
	Value     *core.Property[float32]
	From, To  float32
	Step      float32
	Size      float32
	Progress  ui.RGBA
	TrackW    float32
	ShowTicks bool
	TickCount int
	Suffix    string
	Decimals  int
}

func Dial(d DialSpec) ui.Element {
	if d.Size == 0 {
		d.Size = R(180)
	}
	if d.TrackW == 0 {
		d.TrackW = R(10)
	}
	if d.TickCount == 0 {
		d.TickCount = 10
	}
	if d.Progress == (ui.RGBA{}) {
		d.Progress = Main
	}
	const start, sweep = 130 * math32.Pi / 180, 280 * math32.Pi / 180
	pos := func() float32 {
		if d.To == d.From {
			return 0
		}
		return min(max((d.Value.Get()-d.From)/(d.To-d.From), 0), 1)
	}
	canvas := ui.NewCanvasFont(d.Size, d.Size, func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
		size := min(rect.W, rect.H)
		cx, cy := rect.X+rect.W/2, rect.Y+rect.H/2
		rad := size/2 - size*0.09
		p := pos()
		angle := start + p*sweep
		arc := func(a0, a1, width float32, c ui.RGBA) {
			path := render.NewPath()
			const seg = 64
			for i := 0; i <= seg; i++ {
				a := a0 + (a1-a0)*float32(i)/seg
				x, y := cx+rad*math32.Cos(a), cy+rad*math32.Sin(a)
				if i == 0 {
					path.MoveTo(x, y)
				} else {
					path.LineTo(x, y)
				}
			}
			r.StrokePathRound(path, width, c)
		}
		if p > 0.01 { // the wide soft copy under the progress arc
			arc(start, angle, d.TrackW+size*0.03, Alpha(d.Progress, 0.2))
		}
		if d.ShowTicks {
			for i := 0; i <= d.TickCount; i++ {
				ta := start + (float32(i)/float32(d.TickCount))*sweep
				major := i%5 == 0
				inner := rad - d.TrackW/2 - size*map[bool]float32{true: 0.07, false: 0.04}[major]
				outer := rad - d.TrackW/2 - size*0.01
				w := float32(1)
				a := float32(0.12)
				if major {
					w, a = max(1.5, size*0.01), 0.3
				}
				path := render.NewPath()
				path.MoveTo(cx+inner*math32.Cos(ta), cy+inner*math32.Sin(ta))
				path.LineTo(cx+outer*math32.Cos(ta), cy+outer*math32.Sin(ta))
				r.StrokePath(path, w, ui.RGBA{R: 1, G: 1, B: 1, A: a})
			}
		}
		arc(start, start+sweep, d.TrackW, ui.RGBA{R: 0.66, G: 0.66, B: 0.66, A: 0.3})
		if p > 0.005 {
			arc(start, angle, d.TrackW, d.Progress)
		}
		// The value, and its unit under it.
		if f != nil {
			val := strconv.FormatFloat(float64(d.Value.Get()), 'f', d.Decimals, 32)
			vf := f.AtSize(float64(rad * 0.38))
			w, _ := r.MeasureText(vf, val)
			r.DrawText(vf, cx-w/2, cy-vf.Height()*0.65, val, FontPrimary)
			if d.Suffix != "" {
				sf := f.AtSize(float64(rad * 0.15))
				sw, _ := r.MeasureText(sf, d.Suffix)
				r.DrawText(sf, cx-sw/2, cy+vf.Height()*0.4, d.Suffix, Inactive)
			}
		}
		// The handle on the arc: a card-coloured disc ringed in the
		// progress colour, with a dot of it in the middle.
		hr := size * 0.07
		hx, hy := cx+rad*math32.Cos(angle), cy+rad*math32.Sin(angle)
		disc := func(x, y, rr float32, c ui.RGBA) {
			r.FillRoundRect(x-rr, y-rr, 2*rr, 2*rr, rr, c)
		}
		disc(hx, hy+size*0.01, hr, ui.RGBA{A: 0.2})
		disc(hx, hy, hr, d.Progress) // the ring...
		disc(hx, hy, hr-max(2, size*0.015), Card)
		disc(hx, hy, size*0.02, d.Progress)
	})
	dial := ui.Dial(d.Value, d.From, d.To).Size(d.Size)
	if d.Step > 0 {
		dial = dial.Step(d.Step, true)
	}
	// The canvas draws, the Dial takes the drag: the interaction control
	// goes ON TOP, exactly as the style puts its handle over its canvas.
	return ui.Div().W(d.Size).H(d.Size).AnchorMode().Children(
		ui.Anchored(canvas, "face", ui.Anchors{Fill: "parent"}),
		ui.Anchored(dial, "dial", ui.Anchors{Fill: "parent"}),
	)
}
