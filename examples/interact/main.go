//go:build linux || windows || js

// Interact demonstrates the GPUI interaction features: keyboard focus with
// Tab + a focus ring, mouse cursor shapes (hand over buttons, I-beam over
// the field), and drag-and-drop (drag the color chips into the basket).
//
//	go run ./interact
//	go run ./interact -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

const W, H = 540, 420

var basket = []uint32{}
var name = ui.NewInput("")

func chip(c uint32) ui.Element {
	return ui.Draggable(
		ui.Div().Size(48, 48).Bg(ui.RGB(c)).Rounded(10).Cursor(app.CursorPointer),
		c)
}

func button(label string) ui.Element {
	return ui.Div().Size(120, 38).Bg(ui.RGB(0x3b5bdb)).Rounded(8).Focusable().
		HoverBg(ui.RGB(0x4c6ef5)).PressBg(ui.RGB(0x2f44ad)).Cursor(app.CursorPointer).
		JustifyCenter().ItemsCenter().OnClick(func() {}).
		Children(ui.Text(label, ui.RGB(0xffffff)))
}

func view() ui.Element {
	chips := ui.Div().Row().Gap(10).Children(
		chip(0xe03131), chip(0xf08c00), chip(0x2f9e44), chip(0x1971c2), chip(0x9c36b5),
	)

	basketRow := ui.Div().Row().Gap(6).Children()
	for _, c := range basket {
		basketRow.Children(ui.Div().Size(32, 32).Bg(ui.RGB(c)).Rounded(6))
	}
	drop := ui.DropZone(
		ui.Div().H(80).P(12).Bg(ui.RGB(0x1d1d27)).Rounded(12).Border(2, ui.RGB(0x3b3b4f)).
			Children(basketRow),
		func(p any) { basket = append(basket, p.(uint32)) },
	)

	return ui.Div().Col().P(20).Gap(16).Bg(ui.RGB(0x14141c)).Children(
		ui.Text("Tab to focus · hover for cursors · drag chips → basket", ui.RGB(0xc6c6d4)),
		ui.Div().Row().Gap(10).Children(button("OK"), button("Cancel")),
		ui.Input(name, 260),
		ui.Text("drag a color into the basket:", ui.RGB(0x8f8fa3)),
		chips,
		drop,
	)
}

func main() {
	ui.SetTheme(ui.DarkTheme()) // a dark app: its controls drawn dark
	frames := flag.Int("frames", 0, "exit after N frames")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	flag.Parse()
	opts := shell.Options{Title: "graphics: interact", Width: W, Height: H, MaxFrames: *frames}
	var err error
	if *shot != "" {
		basket = []uint32{0xe03131, 0x2f9e44} // a couple pre-dropped for the still
		err = shell.ScreenshotUI(opts, view, *shot)
	} else {
		err = shell.RunUI(opts, view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "interact:", err)
		os.Exit(1)
	}
}
