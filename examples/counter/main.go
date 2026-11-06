//go:build linux || windows || js

// Counter is the smallest real UI demo: one reactive property driving a
// view, two buttons mutating it. Native and web from the same main.
//
//	go run ./counter              # click the buttons; close to exit
//	go run ./counter -frames 120  # deterministic exit for CI/agents
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

func button(label string, onClick func()) ui.Element {
	return ui.Div().Size(72, 40).Bg(ui.RGB(0x3b5bdb)).Rounded(8).
		HoverBg(ui.RGB(0x4c6ef5)).PressBg(ui.RGB(0x2f44ad)).
		JustifyCenter().ItemsCenter().OnClick(onClick).
		Children(ui.Text(label, ui.RGB(0xffffff)))
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	flag.Parse()

	var count core.Property[int]
	view := func() ui.Element {
		return ui.Div().Col().P(24).Gap(16).Bg(ui.RGB(0x16161e)).Children(
			ui.Text(fmt.Sprintf("count: %d", count.Get()), ui.RGB(0xe8e8f0)),
			ui.Div().Row().Gap(12).Children(
				button("-", func() { count.Set(count.Get() - 1) }),
				button("+", func() { count.Set(count.Get() + 1) }),
			),
		)
	}

	err := shell.RunUI(shell.Options{
		Title: "graphics: counter", Width: 420, Height: 240,
		FontSize: 20, MaxFrames: *frames,
	}, view)
	if err != nil {
		fmt.Fprintln(os.Stderr, "counter:", err)
		os.Exit(1)
	}
}
