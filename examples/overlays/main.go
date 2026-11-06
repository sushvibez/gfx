//go:build linux || windows || js

// Overlays shows the GPUI-parity additions: wrapped paragraph text, an
// opacity-faded panel, and a dropdown rendered as a z-layered overlay
// (open it with the button; it paints on top and hit-tests first).
//
//	go run ./overlays
//	go run ./overlays -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

const W, H = 520, 420
const lorem = "GPUI parity: this paragraph wraps to a fixed width, breaking " +
	"at word boundaries and hard-breaking any word too long to fit. " +
	"Below, an opacity-faded panel and a dropdown overlay."

var open core.Property[bool]
var picked core.Property[string]

func dropdownItem(label string) ui.Element {
	return ui.Div().W(160).H(34).P(8).HoverBg(ui.RGB(0x3b5bdb)).
		OnClick(func() { picked.Set(label); open.Set(false) }).
		Children(ui.Text(label, ui.RGB(0xe8e8f0)))
}

func view() ui.Element {
	pickedText := picked.Get()
	if pickedText == "" {
		pickedText = "choose…"
	}
	root := ui.Div().Col().P(20).Gap(16).Bg(ui.RGB(0x14141c)).Children(
		ui.Wrapped(lorem, ui.RGB(0xc6c6d4), 480),

		// Opacity demo: three stacked translucent panels.
		ui.Div().Row().Gap(0).Children(
			ui.Div().Size(80, 60).Bg(ui.RGB(0xe03131)).Opacity(1.0),
			ui.Div().Size(80, 60).Bg(ui.RGB(0xe03131)).Opacity(0.6),
			ui.Div().Size(80, 60).Bg(ui.RGB(0xe03131)).Opacity(0.3),
		),

		// Dropdown trigger.
		ui.Div().Size(160, 38).Bg(ui.RGB(0x1d1d27)).Rounded(8).Border(1, ui.RGB(0x3b3b4f)).
			ItemsCenter().P(8).OnClick(func() { open.Set(!open.Get()) }).
			Children(ui.Text(pickedText, ui.RGB(0xe8e8f0))),
	)

	if open.Get() {
		menu := ui.Div().Col().Bg(ui.RGB(0x24242f)).Rounded(8).Border(1, ui.RGB(0x3b3b4f)).
			Shadow(18, 0, 8, ui.RGBA{A: 0.6}).
			Children(dropdownItem("Apple"), dropdownItem("Banana"), dropdownItem("Cherry"))
		root.Children(ui.Overlay(menu, 20, 252))
	}
	return root
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	flag.Parse()
	opts := shell.Options{Title: "graphics: overlays", Width: W, Height: H, MaxFrames: *frames}
	var err error
	if *shot != "" {
		open.Set(true) // show the dropdown open in the screenshot
		err = shell.ScreenshotUI(opts, view, *shot)
	} else {
		err = shell.RunUI(opts, view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "overlays:", err)
		os.Exit(1)
	}
}
