//go:build linux || windows || js

// Settings is the phase-1 exit demo (DESIGN.md §12): a settings panel with
// toggles, sliders, text fields and a scrollable body — reactive properties
// throughout, running native, web, and fully headless.
//
//	go run ./settings
//	go run ./settings -frames 120
//	go run ./settings -screenshot out.png   # native: no window needed
//	scripts/build-web.sh settings           # browser
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

const W, H = 520, 480

type state struct {
	username  *ui.InputState
	dark      core.Property[bool]
	telemetry core.Property[bool]
	autosave  core.Property[bool]
	volume    core.Property[float32]
	scale     core.Property[float32]
	body      ui.ScrollState
	summary   core.Property[string]
}

func newState() *state {
	s := &state{username: ui.NewInput("gopher")}
	s.dark.Set(true)
	s.autosave.Set(true)
	s.volume.Set(0.7)
	s.scale.Set(0.5)
	s.summary.Bind(func() string {
		mode := "light"
		if s.dark.Get() {
			mode = "dark"
		}
		return fmt.Sprintf("%s · %s mode · volume %d%%",
			s.username.Text.Get(), mode, int(s.volume.Get()*100+0.5))
	})
	return s
}

func (s *state) view() ui.Element {
	label := func(t string) ui.Element { return ui.Text(t, ui.RGB(0x8f8fa3)) }
	section := func(title string, children ...ui.Element) ui.Element {
		return ui.Div().Col().P(16).Gap(12).Bg(ui.RGB(0x1d1d27)).Rounded(12).
			Border(1, ui.RGB(0x30303d)).
			Children(append([]ui.Element{ui.Text(title, ui.RGB(0xc6c6d4))}, children...)...)
	}
	sliderRow := func(name string, v *core.Property[float32]) ui.Element {
		return ui.Div().Row().Gap(12).ItemsCenter().Children(
			ui.Div().W(80).Children(label(name)),
			ui.Slider(v, 220),
			ui.Text(fmt.Sprintf("%3d%%", int(v.Get()*100+0.5)), ui.RGB(0xe8e8f0)),
		)
	}

	return ui.Div().Col().P(18).Gap(12).Bg(ui.RGB(0x131318)).Children(
		ui.Text("Settings", ui.RGB(0xf1f1f7)),
		ui.Scroll(&s.body).H(360).Gap(12).Children(
			section("Profile",
				ui.Div().Row().Gap(12).ItemsCenter().Children(
					ui.Div().W(80).Children(label("username")),
					ui.Input(s.username, 220),
				),
			),
			section("Appearance",
				ui.Checkbox(&s.dark, "Dark mode"),
				sliderRow("ui scale", &s.scale),
			),
			section("Audio",
				sliderRow("volume", &s.volume),
			),
			section("Privacy",
				ui.Checkbox(&s.telemetry, "Send anonymous usage data"),
				ui.Checkbox(&s.autosave, "Autosave documents"),
			),
		),
		ui.Div().Row().P(10).Bg(ui.RGB(0x1a1a22)).Rounded(8).Children(
			ui.Text(s.summary.Get(), ui.RGB(0x91a7ff)),
		),
	)
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	flag.Parse()

	s := newState()
	opts := shell.Options{Title: "graphics: settings", Width: W, Height: H, MaxFrames: *frames}
	var err error
	if *shot != "" {
		err = screenshot(opts, s, *shot)
	} else {
		err = shell.RunUI(opts, s.view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "settings:", err)
		os.Exit(1)
	}
}
