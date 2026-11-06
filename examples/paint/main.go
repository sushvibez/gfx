//go:build linux || windows || js

// Paint shows the phase-parity 2D additions together: a gradient header, a
// Canvas drawing a filled star path, and a virtualized 10k-row list.
//
//	go run ./paint
//	go run ./paint -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"github.com/chewxy/math32"
	"os"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

const W, H = 560, 520

// A heart icon as inline SVG (filled path + viewBox), parsed once.
const heartSVG = `<svg viewBox="0 0 24 24">
  <path d="M12 21 C12 21 3 13.5 3 8.5 C3 5.5 5.5 4 7.5 4 C9.5 4 12 6 12 6
           C12 6 14.5 4 16.5 4 C18.5 4 21 5.5 21 8.5 C21 13.5 12 21 12 21 Z"
        fill="#e64980"/>
</svg>`

var heart, _ = render.ParseSVG([]byte(heartSVG))

// wave strokes a sine curve across the canvas.
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

func star(r *render.Renderer2D, rect ui.Rect) {
	cx, cy := rect.X+rect.W/2, rect.Y+rect.H/2
	rad := min(rect.W, rect.H) * 0.45
	p := render.NewPath()
	for i := range 10 {
		ang := float32(i)*math32.Pi/5 - math32.Pi/2
		rr := rad
		if i%2 == 1 {
			rr = rad * 0.42 // inner points
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

var listState ui.ListState

func view() ui.Element {
	return ui.Div().Col().Bg(ui.RGB(0x14141c)).Children(
		// Gradient header with a rich (multi-color) title.
		ui.Div().H(70).Gradient(ui.RGB(0x7048e8), ui.RGB(0x4263eb), math32.Pi/2).
			JustifyCenter().ItemsCenter().
			Children(ui.Rich(360,
				ui.Span{Text: "gradients", Color: ui.RGB(0xffe066)},
				ui.Span{Text: "·", Color: ui.RGB(0xffffff)},
				ui.Span{Text: "paths", Color: ui.RGB(0x69db7c)},
				ui.Span{Text: "·", Color: ui.RGB(0xffffff)},
				ui.Span{Text: "lists", Color: ui.RGB(0x74c0fc)},
			)),

		ui.Div().Row().P(16).Gap(16).Children(
			// Filled star, SVG heart, stroked wave.
			ui.Div().Col().Gap(8).Children(
				ui.Text("fill · svg · stroke", ui.RGB(0x8f8fa3)),
				ui.Div().Row().Gap(8).Children(
					ui.NewCanvas(90, 90, star),
					ui.SVG(heart, 90, 90),
				),
				ui.NewCanvas(188, 60, wave),
			),
			// A virtualized list of 10k rows.
			ui.Div().Col().Gap(8).Grow().Children(
				ui.Text("virtualized list (10,000 rows)", ui.RGB(0x8f8fa3)),
				ui.List(&listState, 34, 10000, func(i int) ui.Element {
					bg := uint32(0x1e1e28)
					if i%2 == 1 {
						bg = 0x24242f
					}
					return ui.Div().Row().H(34).P(8).Bg(ui.RGB(bg)).ItemsCenter().
						Children(ui.Text(fmt.Sprintf("Item #%d", i), ui.RGB(0xd4d4e0)))
				}).H(320),
			),
		),
	)
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	flag.Parse()
	opts := shell.Options{Title: "graphics: paint", Width: W, Height: H, MaxFrames: *frames}
	var err error
	if *shot != "" {
		err = shell.ScreenshotUI(opts, view, *shot)
	} else {
		err = shell.RunUI(opts, view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "paint:", err)
		os.Exit(1)
	}
}
