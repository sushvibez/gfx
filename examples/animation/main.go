//go:build linux || windows || js

// Animation shows the core animation engine driving the UI: a spring
// toggle (the knob springs between ends, retargeting mid-flight if you
// click fast), a continuously ping-ponging pulse, and an eased progress
// bar. The shell ticks core.TickAnimations every frame.
//
//	go run ./animation
//	go run ./animation -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

const W, H = 480, 360

var (
	on    bool
	knob  core.Property[float32] // 0..1 spring position of the toggle
	pulse core.Property[float32] // 0..1 ping-pong
	bar   core.Property[float32] // 0..1 eased progress
)

func init() {
	core.TweenLoop(&pulse, 1, 900*time.Millisecond, core.EaseInOutQuad, core.PingPong)
	bar.Set(0.25)
}

func flip() {
	on = !on
	to := float32(0)
	if on {
		to = 1
	}
	core.SpringTo(&knob, to) // springs toward the new end, keeping velocity
}

func toggleSwitch() ui.Element {
	track := uint32(0x3b3b4f)
	if knob.Get() > 0.5 {
		track = 0x2f9e44
	}
	return ui.Div().Size(64, 32).Bg(ui.RGB(track)).Rounded(16).ItemsCenter().
		Cursor(app.CursorPointer).OnClick(flip).Children(
		ui.Div().W(4+knob.Get()*30), // leading spacer pushes the knob
		ui.Div().Size(24, 24).Bg(ui.RGB(0xffffff)).Rounded(12),
	)
}

func animBtn(label string, fn func()) ui.Element {
	return ui.Div().Size(72, 34).Bg(ui.RGB(0x3b5bdb)).Rounded(8).
		HoverBg(ui.RGB(0x4c6ef5)).JustifyCenter().ItemsCenter().
		Cursor(app.CursorPointer).OnClick(fn).
		Children(ui.Text(label, ui.RGB(0xffffff)))
}

func view() ui.Element {
	dot := ui.Div().Size(40, 40).Bg(ui.RGB(0x4dabf7)).Rounded(20).Opacity(0.35 + 0.65*pulse.Get())

	barW := float32(300)
	track := ui.Div().Size(barW, 14).Bg(ui.RGB(0x1d1d27)).Rounded(7).Children(
		ui.Div().Size(barW*bar.Get(), 14).Bg(ui.RGB(0x3b5bdb)).Rounded(7),
	)

	return ui.Div().Col().P(24).Gap(20).Bg(ui.RGB(0x14141c)).Children(
		ui.Text("spring toggle · ping-pong pulse · eased bar", ui.RGB(0xc6c6d4)),
		ui.Div().Row().Gap(16).ItemsCenter().Children(toggleSwitch(), ui.Text("spring toggle", ui.RGB(0x8f8fa3))),
		ui.Div().Row().Gap(16).ItemsCenter().Children(dot, ui.Text("pulse (loops)", ui.RGB(0x8f8fa3))),
		track,
		ui.Div().Row().Gap(10).Children(
			animBtn("0%", func() { core.Tween(&bar, 0, 400*time.Millisecond, core.EaseOutCubic) }),
			animBtn("60%", func() { core.Tween(&bar, 0.6, 400*time.Millisecond, core.EaseOutCubic) }),
			animBtn("100%", func() { core.Tween(&bar, 1, 400*time.Millisecond, core.EaseOutCubic) }),
		),
	)
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	flag.Parse()
	opts := shell.Options{Title: "graphics: animation", Width: W, Height: H, MaxFrames: *frames}
	var err error
	if *shot != "" {
		knob.Set(1) // representative still: toggle on, mid-pulse, partial bar
		pulse.Set(0.7)
		bar.Set(0.6)
		err = shell.ScreenshotUI(opts, view, *shot)
	} else {
		err = shell.RunUI(opts, view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "animation:", err)
		os.Exit(1)
	}
}
