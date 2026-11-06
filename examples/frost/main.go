//go:build linux || windows || js

// Frost shows offscreen layers: a group of items renders into its own
// texture (ui.Layer) and is composited blurred, which is what "frosted
// glass" is made of — a card over a blurred copy of what sits behind it.
// The slider changes the blur radius live.
//
//	go run ./frost
//	go run ./frost -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

const W, H = 560, 420

// The layer's texture outlives the per-frame element tree, so its state is
// kept here rather than rebuilt with the view.
var backdrop = ui.NewLayerState()

var blur = core.Property[float32]{}

func blobs() ui.Element {
	circle := func(size float32, c ui.RGBA, x, y float32) ui.Element {
		return ui.Anchored(ui.Div().Size(size, size).Rounded(size/2).Bg(c), "",
			ui.Anchors{X: x, Y: y, HasX: true, HasY: true})
	}
	return ui.Div().AnchorMode().Children(
		circle(200, ui.RGB(0xff2d8a), 20, 30),
		circle(180, ui.RGB(0x9b30ff), 260, 90),
		circle(120, ui.RGB(0x1fd6a5), 150, 190),
	)
}

func view() ui.Element {
	card := ui.Div().Size(320, 150).Rounded(18).
		Bg(ui.RGBA{R: 1, G: 1, B: 1, A: 0.10}).
		Border(1, ui.RGBA{R: 1, G: 1, B: 1, A: 0.28}).
		Col().P(18).Gap(6).Children(
		ui.Text("Frosted glass", ui.RGB(0xffffff)),
		ui.Text("ui.Layer(state, item).Blur(px)", ui.RGBA{R: 1, G: 1, B: 1, A: 0.7}),
	)
	return ui.Div().Col().Bg(ui.RGB(0x0a0d1a)).Children(
		ui.Div().AnchorMode().Grow().Children(
			ui.Anchored(ui.Layer(backdrop, blobs()).Blur(blur.Get()), "",
				ui.Anchors{Fill: "parent"}),
			ui.Anchored(card, "", ui.Anchors{X: 120, Y: 130, HasX: true, HasY: true}),
		),
		ui.Div().Row().ItemsCenter().Gap(12).P(16).Children(
			ui.Text("blur", ui.RGB(0x9aa0b4)),
			ui.Slider(&blur, 300).Range(0, 48),
		),
	)
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	flag.Parse()
	blur.Set(28)
	opts := shell.Options{Title: "graphics: frost", Width: W, Height: H, MaxFrames: *frames}
	var err error
	if *shot != "" {
		err = shell.ScreenshotUI(opts, view, *shot)
	} else {
		err = shell.RunUI(opts, view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "frost:", err)
		os.Exit(1)
	}
}
