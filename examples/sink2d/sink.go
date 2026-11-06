//go:build linux || windows || js

package main

import (
	"fmt"
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// state is the retained model behind the Go-builder view (the QML version
// in sink.gml keeps the equivalent state as `property` declarations).
type state struct {
	count core.Property[int]
	pct   core.Property[float32]
	dark  core.Property[bool]
	name  *ui.InputState
	greet core.Property[string]
	list  ui.ListState
}

func newState() *state {
	s := &state{name: ui.NewInput("")}
	s.pct.Set(0.6)
	s.dark.Set(true)
	s.greet.Bind(func() string {
		if s.name.Text.Get() == "" {
			return "type a name above"
		}
		return "Hello, " + s.name.Text.Get() + "!"
	})
	return s
}

func card(title string, body ...ui.Element) ui.Element {
	head := ui.Text(title, ui.RGB(0xb6b6c8))
	return ui.Div().Col().P(16).Gap(10).Bg(ui.RGB(0x1d1d27)).Rounded(12).
		Border(1, ui.RGB(0x32323f)).Shadow(14, 0, 6, ui.RGBA{A: 0.5}).
		Children(append([]ui.Element{head}, body...)...)
}

func button(label string, fn func()) ui.Element {
	return ui.Div().Size(44, 36).Bg(ui.RGB(0x3b5bdb)).Rounded(8).
		HoverBg(ui.RGB(0x4c6ef5)).PressBg(ui.RGB(0x2f44ad)).
		JustifyCenter().ItemsCenter().OnClick(fn).
		Children(ui.Text(label, ui.RGB(0xffffff)))
}

func (s *state) view() ui.Element {
	val := func(t string) ui.Element { return ui.Text(t, ui.RGB(0xe8e8f0)) }
	return ui.Div().Col().Bg(ui.RGB(0x14141c)).Children(
		ui.Div().H(64).Gradient(ui.RGB(0x7048e8), ui.RGB(0x4263eb), math32.Pi/2).
			JustifyCenter().ItemsCenter().
			Children(ui.Text("2D kitchen sink — Go builder", ui.RGB(0xffffff))),

		ui.Div().Col().P(16).Gap(14).Children(
			card("Counter, slider, checkbox",
				ui.Div().Row().Gap(10).ItemsCenter().Children(
					button("-", func() { s.count.Set(s.count.Get() - 1) }),
					button("+", func() { s.count.Set(s.count.Get() + 1) }),
					val(fmt.Sprintf("count: %d", s.count.Get())),
				),
				ui.Div().Row().Gap(10).ItemsCenter().Children(
					ui.Slider(&s.pct, 200),
					val(fmt.Sprintf("%d%%", int(s.pct.Get()*100+0.5))),
				),
				ui.Checkbox(&s.dark, "Dark mode"),
			),
			card("Text input (greeting is a binding)",
				ui.Input(s.name, 240),
				ui.Text(s.greet.Get(), ui.RGB(0x91a7ff)),
			),
			card("Virtualized list (5,000 rows)",
				ui.List(&s.list, 32, 5000, func(i int) ui.Element {
					bg := uint32(0x1e1e28)
					if i%2 == 1 {
						bg = 0x24242f
					}
					return ui.Div().Row().H(32).P(8).Bg(ui.RGB(bg)).ItemsCenter().
						Children(ui.Text(fmt.Sprintf("Row %d", i), ui.RGB(0xd4d4e0)))
				}).H(160),
			),
		),
	)
}
