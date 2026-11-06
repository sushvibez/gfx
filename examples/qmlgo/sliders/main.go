//go:build linux || windows || js

// A hand-written Go port of the corpus' sliders page
// (~/workspace/qml-examples/examples/sliders/Main.qml) — the third render
// in scripts/qml-trio.sh.
//
//	go run ./qmlgo/sliders
package main

import (
	"fmt"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/qmlgo/corpus"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

type state struct {
	body ui.ScrollState
	// One retained layer per glowing button (see corpus.Glow).
	demoGlow, maxGlow *ui.LayerState

	horizontal, stepped, disabled core.Property[float32]
	vert1, vert2, vert3           core.Property[float32]
	glowIntensity, glowRadius     core.Property[float32]
	volume                        core.Property[float32]
	temp, vol, speed              core.Property[float32]
}

func newState() *state {
	s := &state{demoGlow: ui.NewLayerState(), maxGlow: ui.NewLayerState()}
	for p, v := range map[*core.Property[float32]]float32{
		&s.horizontal: 50, &s.stepped: 50, &s.disabled: 75,
		&s.vert1: 30, &s.vert2: 60, &s.vert3: 90,
		&s.glowIntensity: 0.6, &s.glowRadius: 20, &s.volume: 50,
		&s.temp: 22, &s.vol: 65, &s.speed: 80,
	} {
		p.Set(v)
	}
	return s
}

// labelledSlider is the card idiom of this page: a caption over a slider
// in a 40px-high box, inset 10px each side.
func labelledSlider(caption string, sl *ui.SliderEl) ui.Element {
	return ui.Div().Col().Gap(corpus.R(8)).CrossFill().Children(
		ui.Text(caption, corpus.FontSecondary).SizePx(14),
		// The markup is an Item filling the width with the Slider anchored
		// into it, 10px in on each side — a ROW here, since ui.Div is a
		// column by default and a grow along a column is a HEIGHT.
		ui.Div().Row().H(corpus.R(40)).PadXY(corpus.R(10), 0).CrossFill().ItemsCenter().
			Children(sl.Grow()),
	)
}

func (s *state) basicCard() ui.Element {
	return corpus.CardBox("Basic Sliders",
		labelledSlider(fmt.Sprintf("Horizontal Slider: %.2f", s.horizontal.Get()),
			corpus.Slider(&s.horizontal, 0, 100, corpus.R(40))),
		labelledSlider(fmt.Sprintf("Stepped Slider (step: 10): %.0f", s.stepped.Get()),
			corpus.Slider(&s.stepped, 0, 100, corpus.R(40)).Step(10, true)),
		labelledSlider("Disabled Slider",
			corpus.Slider(&s.disabled, 0, 100, corpus.R(40)).Disabled()),
	).Gap(corpus.R(20))
}

func (s *state) verticalCard() ui.Element {
	col := func(v *core.Property[float32]) ui.Element {
		return ui.Div().Col().Gap(corpus.R(10)).ItemsCenter().Children(
			corpus.Slider(v, 0, 100, corpus.R(60)).Vertical().H(corpus.R(150)).W(corpus.R(60)),
			ui.Text(fmt.Sprintf("%.0f", v.Get()), corpus.FontSecondary).SizePx(14),
		)
	}
	return corpus.CardBox("Vertical Sliders",
		ui.Div().Row().Gap(corpus.R(30)).CrossFill().Children(
			col(&s.vert1), col(&s.vert2), col(&s.vert3),
			ui.Wrapped("Vertical sliders can be created using orientation: Qt.Vertical",
				corpus.FontSecondary, 0).SizePx(12).Grow(),
		),
	)
}

func (s *state) glowCard() ui.Element {
	return corpus.CardBox("Interactive Demo - Reusable Components",
		corpus.Note("Control the GlowButton intensity with the slider below"),
		ui.Div().Row().JustifyCenter().CrossFill().Children(
			corpus.Glow(s.demoGlow, "Glow Button", ui.RGB(0x00D1A8), corpus.R(180), corpus.R(50),
				s.glowIntensity.Get(), s.glowRadius.Get(), nil),
		),
		labelledSlider(fmt.Sprintf("Glow Intensity: %.2f", s.glowIntensity.Get()),
			corpus.Slider(&s.glowIntensity, 0, 1, corpus.R(40)).Step(0.1, true)),
		labelledSlider(fmt.Sprintf("Glow Radius: %.0f", s.glowRadius.Get()),
			corpus.Slider(&s.glowRadius, 5, 50, corpus.R(40)).Step(5, true)),
		ui.Text("✓ This demonstrates component reusability - GlowButton from the Buttons example is used here!",
			corpus.Main).SizePx(12).Bold(),
	).Gap(corpus.R(20))
}

func (s *state) volumeCard() ui.Element {
	return corpus.CardBox("Custom Slider with Actions",
		labelledSlider(fmt.Sprintf("Volume: %.0f%%", s.volume.Get()),
			corpus.Slider(&s.volume, 0, 100, corpus.R(40))),
		ui.Div().Row().Gap(corpus.R(10)).CrossFill().Children(
			corpus.Gradient("Mute", ui.RGB(0xFF5900), ui.RGB(0xFF8C00),
				corpus.R(100), corpus.R(35), func() { s.volume.Set(0) }),
			corpus.Pulse("50%", corpus.Main, corpus.R(100), corpus.R(35),
				func() { s.volume.Set(50) }),
			corpus.Glow(s.maxGlow, "Max", ui.RGB(0x00D1A8), corpus.R(100), corpus.R(35),
				0.6, 20, func() { s.volume.Set(100) }),
		),
		corpus.Note("Using GradientButton, PulseButton, and GlowButton - all reusable components!"),
	)
}

func (s *state) dialCard() ui.Element {
	dial := func(label string, d corpus.DialSpec) ui.Element {
		return ui.Div().Col().Gap(corpus.R(8)).ItemsCenter().Children(
			corpus.Dial(d),
			ui.Text(label, corpus.FontSecondary).SizePx(14),
		)
	}
	// The temperature dial's arc colour follows its own position, as the
	// page's binding does.
	tempColor := ui.RGB(0x4FC3F7)
	switch p := (s.temp.Get() - 16) / 16; {
	case p >= 0.65:
		tempColor = ui.RGB(0xFF7043)
	case p >= 0.35:
		tempColor = corpus.Main
	}
	return corpus.CardBox("Dial — Styled Qt Controls",
		ui.Div().Row().Gap(corpus.R(30)).CrossFill().Children(
			dial("Temperature", corpus.DialSpec{Value: &s.temp, From: 16, To: 32, Step: 0.5,
				Decimals: 1, Suffix: "°C", Progress: tempColor, ShowTicks: true}),
			dial("Volume", corpus.DialSpec{Value: &s.vol, From: 0, To: 100, Step: 1,
				Suffix: "%", Progress: ui.RGB(0x7C4DFF), TrackW: corpus.R(12), ShowTicks: true}),
			dial("Speed", corpus.DialSpec{Value: &s.speed, From: 0, To: 200, Step: 5,
				Suffix: "km/h", Progress: ui.RGB(0xFF5900), TrackW: corpus.R(6)}),
			ui.Div().Col().Gap(corpus.R(10)).Grow().Children(
				ui.Text("Custom Properties", corpus.FontSecondary).SizePx(16).Bold(),
				corpus.Note("• progressColor — arc color\n• trackWidth — arc thickness\n"+
					"• showTicks / tickCount\n• suffix — unit label\n"+
					"• valueDecimals — precision\n• showValue — center display"),
				ui.Text("Native Dial interaction:", corpus.Main).SizePx(12).Bold(),
				ui.Text("drag, mouse wheel, arrow keys", corpus.Main).SizePx(12).Bold(),
			),
		),
	)
}

func (s *state) view() ui.Element {
	return corpus.Page(&s.body, "Slider Examples", func(availW float32) []ui.Element {
		cell := func(e ui.Element, minH float32) ui.Element {
			return ui.Div().MinH(corpus.R(minH)).CrossFill().Clip().NoImplicitSize().Children(e)
		}
		return []ui.Element{
			ui.Grid(2, 3).Gap(corpus.R(20)).W(availW).Children(
				cell(s.basicCard(), 320), cell(s.verticalCard(), 80),
				cell(s.glowCard(), 320), cell(s.volumeCard(), 220),
			),
			ui.Div().H(corpus.R(380)).CrossFill().Clip().NoImplicitSize().
				Children(s.dialCard()),
		}
	})
}

func main() {
	s := newState()
	corpus.Run("sliders", func(*shell.App) (func() ui.Element, error) { return s.view, nil })
}
