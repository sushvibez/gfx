//go:build linux || windows || js

// Kitchensink exercises (nearly) everything the ui toolkit can do, all on
// one scrolling page: reactive properties and bindings, flex layout,
// gradients, shadows, rounded/bordered cards, hover/press buttons, text
// input with focus + caret, checkbox, slider, a virtualized 10k-row list, an
// inner scroll area, rich + wrapped text, canvas fill/stroke paths, an SVG
// icon, opacity, a z-layered dropdown overlay, drag-and-drop, and three live
// animations (a retargeting spring, a ping-pong loop, and an eased bar).
//
//	go run ./kitchensink                       # interactive, native
//	go run ./kitchensink -frames 120           # deterministic exit
//	go run ./kitchensink -screenshot out.png   # headless render (native)
//	scripts/build-web.sh kitchensink           # browser build
package main

import (
	"flag"
	"fmt"
	"github.com/chewxy/math32"
	"os"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

const W, H = 940, 760

// A heart icon as inline SVG (filled path + viewBox), parsed once.
const heartSVG = `<svg viewBox="0 0 24 24">
  <path d="M12 21 C12 21 3 13.5 3 8.5 C3 5.5 5.5 4 7.5 4 C9.5 4 12 6 12 6
           C12 6 14.5 4 16.5 4 C18.5 4 21 5.5 21 8.5 C21 13.5 12 21 12 21 Z"
        fill="#e64980"/>
</svg>`

var heart, _ = render.ParseSVG([]byte(heartSVG))

const lorem = "This paragraph wraps to a fixed width, breaking at word " +
	"boundaries and hard-breaking any word too long to fit on a line."

// state is the retained side of the GPUI hybrid: properties and widget state
// live here; the view function below re-derives the element tree from it
// every frame.
type state struct {
	// reactive counter
	count   core.Property[int]
	doubled core.Property[int] // bound: always 2*count

	// text input + bound greeting
	name     *ui.InputState
	greeting core.Property[string]

	// widgets
	enabled core.Property[bool]
	volume  core.Property[float32]

	// scrolling
	list  ui.ListState
	inner ui.ScrollState
	page  ui.ScrollState

	// dropdown overlay
	open   core.Property[bool]
	picked core.Property[string]

	// drag-and-drop
	basket []uint32

	// animations
	knob     core.Property[float32] // spring target 0/1
	knobLeft bool
	pulse    core.Property[float32] // ping-pong 0..1, started in newState
	bar      core.Property[float32] // eased 0..1 via buttons
}

func newState() *state {
	s := &state{name: ui.NewInput("gopher")}
	s.doubled.Bind(func() int { return s.count.Get() * 2 })
	s.greeting.Bind(func() string { return "Hello, " + s.name.Text.Get() + "!" })
	s.enabled.Set(true)
	s.volume.Set(0.6)
	s.picked.Set("")
	s.basket = []uint32{0xe03131, 0x2f9e44}
	// A perpetual ping-pong pulse drives the "loop" animation card.
	core.TweenLoop(&s.pulse, 1, 900*time.Millisecond, core.EaseInOutQuad, core.PingPong)
	return s
}

// --- canvas painters -------------------------------------------------------

func star(r *render.Renderer2D, rect ui.Rect) {
	cx, cy := rect.X+rect.W/2, rect.Y+rect.H/2
	rad := min(rect.W, rect.H) * 0.45
	p := render.NewPath()
	for i := range 10 {
		ang := float32(i)*math32.Pi/5 - math32.Pi/2
		rr := rad
		if i%2 == 1 {
			rr = rad * 0.42
		}
		x := cx + rr*math32.Cos(ang)
		y := cy + rr*math32.Sin(ang)
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	p.Close()
	r.FillPath(p, ui.RGB(0xffd43b))
}

func wave(r *render.Renderer2D, rect ui.Rect) {
	p := render.NewPath()
	for i := 0; i <= 40; i++ {
		t := float32(i) / 40
		x := rect.X + t*rect.W
		y := rect.Y + rect.H/2 + math32.Sin(t*4*math32.Pi)*rect.H*0.35
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	r.StrokePath(p, 4, ui.RGB(0x4dabf7))
}

// --- view ------------------------------------------------------------------

func (s *state) view() ui.Element {
	label := func(t string) ui.Element { return ui.Text(t, ui.RGB(0x8f8fa3)) }
	value := func(t string) ui.Element { return ui.Text(t, ui.RGB(0xe8e8f0)) }

	button := func(t string, fn func()) ui.Element {
		return ui.Div().Size(86, 38).Bg(ui.RGB(0x3b5bdb)).Rounded(10).Focusable().
			HoverBg(ui.RGB(0x4c6ef5)).PressBg(ui.RGB(0x2f44ad)).Cursor(app.CursorPointer).
			Shadow(8, 0, 3, ui.RGBA{A: 0.45}).
			JustifyCenter().ItemsCenter().OnClick(fn).
			Children(ui.Text(t, ui.RGB(0xffffff)))
	}
	card := func(title string, children ...ui.Element) ui.Element {
		return ui.Div().Col().P(16).Gap(10).Bg(ui.RGB(0x1d1d27)).Rounded(14).
			Border(1, ui.RGB(0x32323f)).Shadow(14, 0, 6, ui.RGBA{A: 0.5}).
			Children(append([]ui.Element{ui.Text(title, ui.RGB(0xb6b6c8))}, children...)...)
	}

	// --- column 1 ----------------------------------------------------------

	counter := card("Reactive counter (doubled is a binding)",
		ui.Div().Row().Gap(10).ItemsCenter().Children(
			button("-", func() { s.count.Set(s.count.Get() - 1) }),
			button("+", func() { s.count.Set(s.count.Get() + 1) }),
			ui.Div().W(12),
			label("count:"), value(fmt.Sprintf("%d", s.count.Get())),
			label("doubled:"), value(fmt.Sprintf("%d", s.doubled.Get())),
		),
	)

	input := card("Text input — click to focus, type; greeting is bound",
		ui.Input(s.name, 300),
		value(s.greeting.Get()),
	)

	widgets := card("Checkbox + slider (both bound to properties)",
		ui.Checkbox(&s.enabled, "Enabled"),
		ui.Div().Row().Gap(12).ItemsCenter().Children(
			label("volume"),
			ui.Slider(&s.volume, 220),
			value(fmt.Sprintf("%d%%", int(s.volume.Get()*100))),
		),
	)

	visuals := card("Canvas fill · stroke · SVG",
		ui.Div().Row().Gap(12).ItemsCenter().Children(
			ui.NewCanvas(80, 80, star),
			ui.SVG(heart, 80, 80),
			ui.NewCanvas(160, 70, wave),
		),
	)

	text := card("Rich + wrapped text",
		ui.Rich(360,
			ui.Span{Text: "red ", Color: ui.RGB(0xff6b6b)},
			ui.Span{Text: "green ", Color: ui.RGB(0x69db7c)},
			ui.Span{Text: "blue", Color: ui.RGB(0x74c0fc)},
		),
		ui.Wrapped(lorem, ui.RGB(0xc6c6d4), 360),
	)

	col1 := ui.Div().Col().Gap(14).GrowBy(1).Children(
		counter, input, widgets, visuals, text,
	)

	// --- column 2 ----------------------------------------------------------

	lists := card("Virtualized list (10,000 rows) + inner scroll",
		ui.Div().Row().Gap(12).Children(
			ui.List(&s.list, 32, 10000, func(i int) ui.Element {
				bg := uint32(0x1e1e28)
				if i%2 == 1 {
					bg = 0x24242f
				}
				return ui.Div().Row().H(32).P(8).Bg(ui.RGB(bg)).ItemsCenter().
					Children(ui.Text(fmt.Sprintf("Item #%d", i), ui.RGB(0xd4d4e0)))
			}).W(180).H(180),
			ui.Scroll(&s.inner).W(150).H(180).P(8).Gap(6).Children(
				lines(12)...,
			),
		),
	)

	// gradient + opacity demo strip
	gradients := card("Gradient + opacity",
		ui.Div().H(48).Rounded(10).Gradient(ui.RGB(0x7048e8), ui.RGB(0x4263eb), math32.Pi/2).
			JustifyCenter().ItemsCenter().
			Children(ui.Text("linear gradient", ui.RGB(0xffffff))),
		ui.Div().Row().Gap(0).Children(
			ui.Div().Size(70, 44).Bg(ui.RGB(0xe03131)).Opacity(1.0),
			ui.Div().Size(70, 44).Bg(ui.RGB(0xe03131)).Opacity(0.6),
			ui.Div().Size(70, 44).Bg(ui.RGB(0xe03131)).Opacity(0.3),
		),
	)

	dropTrigger := ui.Div().Size(180, 38).Bg(ui.RGB(0x14141c)).Rounded(8).
		Border(1, ui.RGB(0x3b3b4f)).ItemsCenter().P(8).Cursor(app.CursorPointer).
		OnClick(func() { s.open.Set(!s.open.Get()) }).
		Children(ui.Text(s.pickedLabel(), ui.RGB(0xe8e8f0)))

	overlay := card("Dropdown overlay (z-layered, hit-tests first)", dropTrigger)

	// drag-and-drop
	chips := ui.Div().Row().Gap(8).Children()
	for _, c := range []uint32{0xe03131, 0xf08c00, 0x2f9e44, 0x1971c2, 0x9c36b5} {
		chips.Children(ui.Draggable(
			ui.Div().Size(40, 40).Bg(ui.RGB(c)).Rounded(8).Cursor(app.CursorPointer), c))
	}
	basketRow := ui.Div().Row().Gap(6).Children()
	for _, c := range s.basket {
		basketRow.Children(ui.Div().Size(28, 28).Bg(ui.RGB(c)).Rounded(6))
	}
	dnd := card("Drag chips into the basket",
		chips,
		ui.DropZone(
			ui.Div().H(56).P(10).Bg(ui.RGB(0x14141c)).Rounded(10).Border(2, ui.RGB(0x3b3b4f)).
				ItemsCenter().Children(basketRow),
			func(p any) { s.basket = append(s.basket, p.(uint32)) },
		),
	)

	col2 := ui.Div().Col().Gap(14).GrowBy(1).Children(
		lists, gradients, overlay, dnd, s.animCard(button, label),
	)

	// --- page --------------------------------------------------------------

	page := ui.Div().Col().P(20).Gap(16).Children(
		ui.Text("Kitchen sink — everything the toolkit can do", ui.RGB(0xf1f1f7)),
		ui.Div().Row().Gap(16).Children(col1, col2),
	)

	root := ui.Div().Col().Bg(ui.RGB(0x131318)).Children(
		ui.Scroll(&s.page).Children(page).H(H),
	)

	// The dropdown menu floats above everything as an overlay.
	if s.open.Get() {
		menu := ui.Div().Col().Bg(ui.RGB(0x24242f)).Rounded(8).Border(1, ui.RGB(0x3b3b4f)).
			Shadow(18, 0, 8, ui.RGBA{A: 0.6}).
			Children(
				s.dropdownItem("Apple"),
				s.dropdownItem("Banana"),
				s.dropdownItem("Cherry"),
			)
		// Positioned under the trigger in column 2.
		root.Children(ui.Overlay(menu, 545, 470))
	}
	return root
}

// animCard shows the three animation modes side by side.
func (s *state) animCard(button func(string, func()) ui.Element, label func(string) ui.Element) ui.Element {
	// Spring: a dot that springs left/right when retargeted.
	spring := ui.Div().Row().H(28).ItemsCenter().Children(
		ui.Div().W(s.knob.Get()*180),
		ui.Div().Size(20, 20).Bg(ui.RGB(0x12b886)).Rounded(10),
	)
	// Ping-pong: a bar that breathes between 20 and 200 px wide, forever.
	pulse := ui.Div().H(20).W(20 + s.pulse.Get()*180).Bg(ui.RGB(0xf06595)).Rounded(10)
	// Eased: a fill bar driven 0/1 by buttons.
	eased := ui.Div().H(12).W(max(2, s.bar.Get()*240)).Bg(ui.RGB(0x4dabf7)).Rounded(6)

	return ui.Div().Col().P(16).Gap(10).Bg(ui.RGB(0x1d1d27)).Rounded(14).
		Border(1, ui.RGB(0x32323f)).Shadow(14, 0, 6, ui.RGBA{A: 0.5}).Children(
		ui.Text("Animations: spring · ping-pong loop · eased", ui.RGB(0xb6b6c8)),
		label("spring (retargets, keeps velocity):"),
		spring,
		button("toggle", func() {
			s.knobLeft = !s.knobLeft
			if s.knobLeft {
				core.SpringTo(&s.knob, 1)
			} else {
				core.SpringTo(&s.knob, 0)
			}
		}),
		label("ping-pong loop (runs forever):"),
		pulse,
		label("eased tween:"),
		eased,
		ui.Div().Row().Gap(8).Children(
			button("fill", func() { core.Tween(&s.bar, 1, 500*time.Millisecond, core.EaseOutCubic) }),
			button("empty", func() { core.Tween(&s.bar, 0, 500*time.Millisecond, core.EaseOutCubic) }),
		),
	)
}

func (s *state) pickedLabel() string {
	if p := s.picked.Get(); p != "" {
		return p
	}
	return "choose…"
}

func (s *state) dropdownItem(label string) ui.Element {
	return ui.Div().W(180).H(34).P(8).HoverBg(ui.RGB(0x3b5bdb)).Cursor(app.CursorPointer).
		OnClick(func() { s.picked.Set(label); s.open.Set(false) }).
		Children(ui.Text(label, ui.RGB(0xe8e8f0)))
}

// lines builds n filler rows for the inner scroll area.
func lines(n int) []ui.Element {
	out := make([]ui.Element, n)
	for i := range out {
		out[i] = ui.Div().H(24).P(4).Bg(ui.RGB(0x24242f)).Rounded(4).
			Children(ui.Text(fmt.Sprintf("row %d", i+1), ui.RGB(0xc6c6d4)))
	}
	return out
}

func main() {
	ui.SetTheme(ui.DarkTheme()) // a dark app: its controls drawn dark
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	flag.Parse()

	s := newState()
	opts := shell.Options{Title: "graphics: kitchen sink", Width: W, Height: H, MaxFrames: *frames}

	var err error
	if *shot != "" {
		s.count.Set(3)
		s.open.Set(true) // show the dropdown open in the still
		err = screenshot(opts, s, *shot)
	} else {
		err = shell.RunUI(opts, s.view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kitchensink:", err)
		os.Exit(1)
	}
}
