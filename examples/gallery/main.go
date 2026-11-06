//go:build linux || windows

// Gallery renders a small widget showcase — rounded cards, borders,
// buttons, text — and doubles as the headless-mode demo (DESIGN.md §8.1):
//
//	go run ./gallery                       # windowed
//	go run ./gallery -screenshot out.png   # no window: offscreen render + PNG
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

func view() ui.Element {
	swatch := func(c uint32) ui.Element {
		return ui.Div().Size(36, 36).Bg(ui.RGB(c)).Rounded(10)
	}
	return ui.Div().Col().P(20).Gap(14).Bg(ui.RGB(0x16161e)).Children(
		ui.Text("Widget gallery", ui.RGB(0xe8e8f0)),
		ui.Div().Col().P(16).Gap(10).Bg(ui.RGB(0x1f1f2a)).Rounded(12).
			Border(2, ui.RGB(0x3b3b4f)).Children(
			ui.Text("A card with border and radius", ui.RGB(0xb8b8c8)),
			ui.Div().Row().Gap(10).Children(
				ui.Div().Size(96, 36).Bg(ui.RGB(0x3b5bdb)).Rounded(18).
					JustifyCenter().ItemsCenter().
					Children(ui.Text("Primary", ui.RGB(0xffffff))),
				ui.Div().Size(96, 36).Rounded(18).Border(2, ui.RGB(0x3b5bdb)).
					JustifyCenter().ItemsCenter().
					Children(ui.Text("Ghost", ui.RGB(0x91a7ff))),
			),
		),
		ui.Div().Row().Gap(10).Children(
			swatch(0xe03131), swatch(0xf08c00), swatch(0x2f9e44),
			swatch(0x1971c2), swatch(0x9c36b5),
		),
	)
}

func main() {
	shot := flag.String("screenshot", "", "render headlessly and write a PNG instead of opening a window")
	frames := flag.Int("frames", 0, "windowed mode: exit after N frames")
	flag.Parse()

	opts := shell.Options{Title: "graphics: gallery", Width: 480, Height: 320, MaxFrames: *frames}
	var err error
	if *shot != "" {
		err = shell.ScreenshotUI(opts, view, *shot)
	} else {
		err = shell.RunUI(opts, view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gallery:", err)
		os.Exit(1)
	}
}
