//go:build linux || windows

// Gpui ports the feasible examples from zed-industries/zed
// crates/gpui/examples for side-by-side visual comparison with GPUI.
//
//	go run ./gpui -example hello_world
//	go run ./gpui -example shadow -screenshot shadow.png
//	go run ./gpui -list
//
// Two flavours per example:
//   - hello_world, shadow, animation, input, scrollable, opacity — our own
//     interactive takes that exercise our widgets (Slider, tween, focus...).
//   - <name>_zed — faithful 1:1 ports of Zed's GPUI examples (light theme,
//     same window size, layout and colours) for apples-to-apples comparison.
//     Text uses GPUI's actual Linux fallback font (Ubuntu) so screenshots
//     line up; see internal/shell/fonts.go and GFX_FONT.
//
// Every feasible GPUI example now has a _zed port (run -list to see them).
// A handful are documented N/A in check/NOTES.md because they are platform or
// gpui-internal and have no meaningful headless visual: layer_shell (Wayland),
// set_menus (native menu bar), window_positioning / move_entity_between_windows
// (multi-window), on_window_close_quit (lifecycle), mouse_pressure (stylus),
// testing / ownership_post (gpui-internal). Approximations in the ports
// (radial gradients, font weights, GIF/animation, async/network images, true
// CSS-grid spans) are noted per-function and in check/NOTES.md.
package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

type example struct {
	view func() ui.Element
	w, h int
}

func examples() map[string]example {
	return map[string]example{
		"hello_world": {helloWorld(), 500, 500},
		// Our own takes (different logic — interactive demos of our widgets).
		"shadow":     {shadowGallery(), 620, 500},
		"animation":  {animation(), 520, 240},
		"input":      {inputExample(), 460, 280},
		"scrollable": {scrollable(), 420, 380},
		"opacity":    {opacity(), 520, 300},
		// Faithful ports that mirror Zed's GPUI examples 1:1 (light theme,
		// same layout/colors) for apples-to-apples visual parity.
		"shadow_zed":     {shadowZed(), 1000, 800},
		"animation_zed":  {animationZed(), 300, 300},
		"input_zed":      {inputZed(), 300, 300},
		"scrollable_zed": {scrollableZed(), 500, 500},
		"opacity_zed":    {opacityZed(), 500, 500},
		// Group A batch — more 1:1 Zed ports (resting-state visuals).
		"drag_drop_zed":     {dragDropZed(), 800, 600},
		"focus_visible_zed": {focusVisibleZed(), 800, 600},
		"tab_stop_zed":      {tabStopZed(), 800, 600},
		"popover_zed":       {popoverZed(), 600, 450},
		"anchor_zed":        {anchorZed(), 750, 600},
		"painting_zed":      {paintingZed(), 920, 560},
		"text_wrapper_zed":  {textWrapperZed(), 800, 600},
		"list_example_zed":  {listExampleZed(), 400, 500},
		"uniform_list_zed":  {uniformListZed(), 300, 300},
		"grid_layout_zed":   {gridLayoutZed(), 500, 500},
		"image_loading_zed": {imageLoadingZed(), 300, 300},
		// Groups B + C — remaining feasible ports (completeness). Platform /
		// non-visual examples (layer_shell, set_menus, window_positioning,
		// move_entity_between_windows, on_window_close_quit, mouse_pressure,
		// testing, ownership_post) are documented N/A in check/NOTES.md.
		"gradient_zed":         {gradientZed(), 900, 500},
		"text_zed":             {textZed(), 1536, 1000},
		"text_layout_zed":      {textLayoutZed(), 800, 600},
		"data_table_zed":       {dataTableZed(), 900, 600},
		"tree_zed":             {treeZed(), 300, 300},
		"pattern_zed":          {patternZed(), 600, 600},
		"a11y_zed":             {a11yZed(), 500, 400},
		"window_zed":           {windowZed(), 800, 600},
		"window_shadow_zed":    {windowShadowZed(), 600, 600},
		"active_state_bug_zed": {activeStateBugZed(), 200, 60},
		"gif_viewer_zed":       {gifViewerZed(), 600, 500},
		"paths_bench_zed":      {pathsBenchZed(), 1024, 768},
		"image_gallery_zed":    {imageGalleryZed(), 600, 600},
	}
}

// helloWorld mirrors hello_world.rs: the window IS the gray panel (no outer
// margin, like gpui's Bounds::centered(500,500)), with text_xl white text, a
// blue border, and six dash-bordered color swatches (red/green/blue/yellow/
// black/white, the white one black-bordered), matching GPUI's border_dashed.
func helloWorld() func() ui.Element {
	swatch := func(c, border uint32) ui.Element {
		return ui.Div().Size(32, 32).Bg(ui.RGB(c)).Rounded(6).
			Border(1, ui.RGB(border)).BorderDashed()
	}
	const white, black = 0xffffff, 0x000000
	return func() ui.Element {
		// Grow fills the 500x500 window, so the panel == the window — no dark
		// frame around it.
		return ui.Div().Col().Grow().Gap(12).Bg(ui.RGB(0x505050)).
			JustifyCenter().ItemsCenter().
			Border(1, ui.RGB(0x0000ff)).
			Shadow(16, 0, 8, ui.RGBA{A: 0.55}).
			Children(
				ui.Text("Hello, World!", ui.RGB(0xffffff)).SizePx(20),
				ui.Div().Row().Gap(8).JustifyCenter().Children(
					// gpui's named colors: green() is HSL(120,100%,25%) = 0x008000.
					swatch(0xff0000, white), swatch(0x008000, white),
					swatch(0x0000ff, white), swatch(0xffff00, white),
					swatch(0x000000, white), swatch(0xffffff, black),
				),
			)
	}
}

// shadowGallery mirrors shadow.rs: white tiles with varying corner radii
// and shadow sizes, labeled.
func shadowGallery() func() ui.Element {
	tile := func(radius, blur float32, label string) ui.Element {
		return ui.Div().Col().Gap(8).ItemsCenter().Children(
			ui.Div().Size(64, 64).Bg(ui.RGB(0xffffff)).Rounded(radius).
				Border(1, ui.RGBA{A: 0.1}).
				Shadow(blur, 0, blur/2, ui.RGBA{A: 0.5}),
			ui.Text(label, ui.RGB(0x9a9aa8)),
		)
	}
	return func() ui.Element {
		row := func(blur float32, name string) ui.Element {
			return ui.Div().Row().Gap(24).ItemsCenter().Children(
				ui.Div().W(90).Children(ui.Text(name, ui.RGB(0xc6c6d4))),
				tile(0, blur, "square"),
				tile(4, blur, "sm"),
				tile(8, blur, "md"),
				tile(12, blur, "lg"),
				tile(32, blur, "full"),
			)
		}
		return ui.Div().Col().P(24).Gap(20).Bg(ui.RGB(0x2a2a32)).Children(
			row(2, "shadow sm"),
			row(6, "shadow md"),
			row(12, "shadow lg"),
			row(24, "shadow xl"),
		)
	}
}

// animation mirrors animation.rs in spirit: an element glides between the
// edges, eased, retriggered by the button (and auto-bounces while running
// windowed).
func animation() func() ui.Element {
	var x core.Property[float32]
	var dir core.Property[bool]
	bounce := func() {
		to := float32(380)
		if dir.Get() {
			to = 0
		}
		dir.Set(!dir.Get())
		core.Tween(&x, to, 700*time.Millisecond, core.EaseInOutQuad)
	}
	return func() ui.Element {
		if !core.AnimationsRunning() && x.Get() != 0 || (!core.AnimationsRunning() && dir.Get()) {
			bounce() // keep bouncing while visible
		}
		return ui.Div().Col().P(20).Gap(16).Bg(ui.RGB(0x131318)).Children(
			ui.Text("animation: eased tween, retarget mid-flight", ui.RGB(0xc6c6d4)),
			ui.Div().H(80).Bg(ui.RGB(0x1d1d27)).Rounded(10).Children(
				ui.Div().Row().P(8).Children(
					ui.Div().W(x.Get()),
					ui.Div().Size(64, 64).Bg(ui.RGB(0x3b5bdb)).Rounded(14).
						Shadow(10, 0, 4, ui.RGBA{A: 0.5}),
				),
			),
			ui.Div().Row().Gap(10).Children(
				ui.Div().Size(110, 36).Bg(ui.RGB(0x3b5bdb)).Rounded(8).
					HoverBg(ui.RGB(0x4c6ef5)).JustifyCenter().ItemsCenter().
					OnClick(bounce).
					Children(ui.Text("bounce", ui.RGB(0xffffff))),
			),
		)
	}
}

// inputExample mirrors input.rs: labeled text fields with focus.
func inputExample() func() ui.Element {
	first := ui.NewInput("Hello")
	last := ui.NewInput("World")
	return func() ui.Element {
		row := func(label string, s *ui.InputState) ui.Element {
			return ui.Div().Row().Gap(12).ItemsCenter().Children(
				ui.Div().W(90).Children(ui.Text(label, ui.RGB(0x9a9aa8))),
				ui.Input(s, 240),
			)
		}
		return ui.Div().Col().P(20).Gap(14).Bg(ui.RGB(0x131318)).Children(
			ui.Text("input: click to focus, type, arrows/home/end", ui.RGB(0xc6c6d4)),
			row("first", first),
			row("last", last),
			ui.Text(first.Text.Get()+" "+last.Text.Get(), ui.RGB(0x91a7ff)),
		)
	}
}

// scrollable mirrors scrollable.rs: many rows in a wheel-scrolled clipped
// viewport.
func scrollable() func() ui.Element {
	var st ui.ScrollState
	return func() ui.Element {
		s := ui.Scroll(&st).H(300).Gap(6).P(10)
		for i := range 40 {
			bg := uint32(0x1d1d27)
			if i%2 == 1 {
				bg = 0x232330
			}
			s.Children(ui.Div().Row().P(10).Bg(ui.RGB(bg)).Rounded(6).Children(
				ui.Text(fmt.Sprintf("Item %d of 40", i+1), ui.RGB(0xd4d4e0)),
			))
		}
		return ui.Div().Col().P(16).Gap(10).Bg(ui.RGB(0x131318)).Children(
			ui.Text("scrollable: wheel to scroll", ui.RGB(0xc6c6d4)),
			s,
		)
	}
}

// opacity mirrors opacity.rs: layered translucency, slider-controlled.
func opacity() func() ui.Element {
	var alpha core.Property[float32]
	alpha.Set(0.5)
	return func() ui.Element {
		a := alpha.Get()
		// GPUI overlaps its layers via absolute positioning, which we lack
		// (tracked gap); adjacent translucent tiles over the panel show the
		// same alpha behavior.
		layered := ui.Div().H(140).Bg(ui.RGB(0x1d1d27)).Rounded(10).Children(
			ui.Div().Row().P(16).Gap(12).Children(
				ui.Div().Size(110, 110).Bg(ui.RGBA{R: 0.9, G: 0.2, B: 0.2, A: a}).Rounded(12),
				ui.Div().Size(110, 110).Bg(ui.RGBA{R: 0.2, G: 0.5, B: 0.9, A: a}).Rounded(12),
				ui.Div().Size(110, 110).Bg(ui.RGBA{R: 1, G: 1, B: 1, A: a}).Rounded(12),
			),
		)
		return ui.Div().Col().P(20).Gap(14).Bg(ui.RGB(0x131318)).Children(
			ui.Text(fmt.Sprintf("opacity: %.2f (drag the slider)", a), ui.RGB(0xc6c6d4)),
			layered,
			ui.Slider(&alpha, 240),
		)
	}
}

func main() {
	name := flag.String("example", "hello_world", "which example to run")
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	width := flag.Int("w", 0, "override window/screenshot width")
	height := flag.Int("h", 0, "override window/screenshot height")
	list := flag.Bool("list", false, "list examples")
	flag.Parse()

	all := examples()
	if *list {
		names := make([]string, 0, len(all))
		for n := range all {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Println(n)
		}
		return
	}
	ex, ok := all[*name]
	if !ok {
		fmt.Fprintf(os.Stderr, "gpui: unknown example %q (use -list)\n", *name)
		os.Exit(1)
	}
	if *width > 0 {
		ex.w = *width
	}
	if *height > 0 {
		ex.h = *height
	}
	// FontSize 16 = gpui's default text size (1 rem, window.rs rem_size);
	// unsized ui.Text then matches Zed's unsized labels exactly.
	// LineHeight phi = gpui's default line box (style.rs); FontSize 16 = 1rem.
	opts := shell.Options{Title: "gpui: " + *name, Width: ex.w, Height: ex.h,
		MaxFrames: *frames, FontSize: 16, LineHeight: 1.618034}
	if f := exampleOpts[*name]; f != nil {
		f(&opts)
	}
	var err error
	if *shot != "" {
		err = shell.Screenshot(opts, shell.Hooks{
			Init: func(a *shell.App) (func() ui.Element, error) {
				if f := appInits[*name]; f != nil {
					f(a)
				}
				return ex.view, nil
			},
		}, *shot)
	} else {
		hooks := shell.Hooks{
			Init: func(a *shell.App) (func() ui.Element, error) {
				if f := appInits[*name]; f != nil {
					f(a)
				}
				return ex.view, nil
			},
			DragRegion: dragRegions[*name],
		}
		// Window-level input observer (gpui observe_keystrokes/on_key_down,
		// canvas mouse handlers); the FrameStart tap is the only hook that
		// sees events a focused element consumes. Observers here are
		// event-driven, so the window keeps present-on-damage idle skipping
		// (paths_bench opts back into continuous frames — it is a repaint
		// benchmark, like request_animation_frame).
		if kobs, fobs := keyObservers[*name], frameObservers[*name]; kobs != nil || fobs != nil {
			opts.EventDriven = *name != "paths_bench_zed"
			hooks.FrameStart = func(_ *shell.App, _ float32, mouse []app.MouseEvent, keys []app.KeyEvent) {
				if kobs != nil {
					kobs(keys)
				}
				if fobs != nil {
					fobs(mouse, keys)
				}
			}
		}
		err = shell.Run(opts, hooks)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gpui:", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Faithful Zed GPUI ports (light theme, 1:1 layout) — see *_zed entries above.
// ---------------------------------------------------------------------------

// arrow_circle.svg from gpui/examples/image; stroke-width injected because our
// parser leaves an absent stroke-width at 0 (SVG spec default is 1).
// rustLogoSVG is lyon_extra's generated Rust logo path (lyon_extra-1.0.3
// src/rust_logo.rs, MIT/Apache-2.0), converted to SVG path data — the same
// source painting.rs builds its logo from.
const rustLogoSVG = `<svg viewBox="0 0 144 144"><path d="M122.631 69.716l-4.394 -2.72c-0.037 -0.428 -0.079 -0.855 -0.125 -1.28l3.776 -3.522c0.384 -0.358 0.556 -0.888 0.452 -1.401c-0.101 -0.515 -0.462 -0.939 -0.953 -1.122l-4.827 -1.805c-0.121 -0.418 -0.248 -0.833 -0.378 -1.246l3.011 -4.182c0.307 -0.425 0.37 -0.978 0.17 -1.463c-0.2 -0.483 -0.637 -0.829 -1.154 -0.914l-5.09 -0.828c-0.198 -0.386 -0.404 -0.766 -0.612 -1.143l2.139 -4.695c0.219 -0.478 0.174 -1.034 -0.118 -1.468c-0.291 -0.436 -0.784 -0.691 -1.31 -0.671l-5.166 0.18c-0.267 -0.334 -0.539 -0.665 -0.816 -0.99l1.187 -5.032c0.12 -0.511 -0.031 -1.046 -0.403 -1.417c-0.369 -0.37 -0.905 -0.523 -1.416 -0.403l-5.031 1.186c-0.326 -0.276 -0.657 -0.549 -0.992 -0.816l0.181 -5.166c0.02 -0.523 -0.235 -1.02 -0.671 -1.31c-0.437 -0.292 -0.99 -0.336 -1.467 -0.119l-4.694 2.14c-0.379 -0.208 -0.759 -0.414 -1.143 -0.613l-0.83 -5.091c-0.084 -0.516 -0.43 -0.954 -0.914 -1.154c-0.483 -0.201 -1.037 -0.136 -1.462 0.17l-4.185 3.011c-0.412 -0.131 -0.826 -0.257 -1.244 -0.377l-1.805 -4.828c-0.183 -0.492 -0.607 -0.853 -1.122 -0.955c-0.514 -0.101 -1.043 0.07 -1.4 0.452l-3.522 3.779c-0.425 -0.047 -0.853 -0.09 -1.28 -0.125l-2.72 -4.395c-0.275 -0.445 -0.762 -0.716 -1.286 -0.716s-1.011 0.271 -1.285 0.716l-2.72 4.395c-0.428 0.035 -0.856 0.078 -1.281 0.125l-3.523 -3.779c-0.357 -0.382 -0.887 -0.553 -1.4 -0.452c-0.515 0.103 -0.939 0.463 -1.122 0.955l-1.805 4.828c-0.418 0.12 -0.832 0.247 -1.245 0.377l-4.184 -3.011c-0.425 -0.307 -0.979 -0.372 -1.463 -0.17c-0.483 0.2 -0.83 0.638 -0.914 1.154l-0.83 5.091c-0.384 0.199 -0.764 0.404 -1.143 0.613l-4.694 -2.14c-0.477 -0.218 -1.033 -0.173 -1.467 0.119c-0.436 0.29 -0.691 0.787 -0.671 1.31l0.18 5.166c-0.334 0.267 -0.665 0.54 -0.992 0.816l-5.031 -1.186c-0.511 -0.119 -1.047 0.033 -1.417 0.403c-0.372 0.371 -0.523 0.906 -0.403 1.417l1.185 5.032c-0.275 0.326 -0.547 0.656 -0.814 0.99l-5.166 -0.18c-0.521 -0.015 -1.019 0.235 -1.31 0.671c-0.292 0.434 -0.336 0.99 -0.119 1.468l2.14 4.695c-0.208 0.377 -0.414 0.757 -0.613 1.143l-5.09 0.828c-0.517 0.084 -0.953 0.43 -1.154 0.914c-0.2 0.485 -0.135 1.038 0.17 1.463l3.011 4.182c-0.131 0.413 -0.258 0.828 -0.378 1.246l-4.828 1.805c-0.49 0.183 -0.851 0.607 -0.953 1.122c-0.102 0.514 0.069 1.043 0.452 1.401l3.777 3.522c-0.047 0.425 -0.089 0.853 -0.125 1.28l-4.394 2.72c-0.445 0.275 -0.716 0.761 -0.716 1.286s0.271 1.011 0.716 1.285l4.394 2.72c0.036 0.428 0.078 0.855 0.125 1.28l-3.777 3.523c-0.383 0.357 -0.554 0.887 -0.452 1.4c0.102 0.515 0.463 0.938 0.953 1.122l4.828 1.805c0.12 0.418 0.247 0.833 0.378 1.246l-3.011 4.183c-0.306 0.426 -0.371 0.979 -0.17 1.462c0.201 0.485 0.638 0.831 1.155 0.914l5.089 0.828c0.199 0.386 0.403 0.766 0.613 1.145l-2.14 4.693c-0.218 0.477 -0.173 1.032 0.119 1.468c0.292 0.437 0.789 0.692 1.31 0.671l5.164 -0.181c0.269 0.336 0.54 0.665 0.816 0.992l-1.185 5.033c-0.12 0.51 0.031 1.043 0.403 1.414c0.369 0.373 0.906 0.522 1.417 0.402l5.031 -1.185c0.327 0.278 0.658 0.548 0.992 0.814l-0.18 5.167c-0.02 0.523 0.235 1.019 0.671 1.311c0.434 0.291 0.99 0.335 1.467 0.117l4.694 -2.139c0.378 0.21 0.758 0.414 1.143 0.613l0.83 5.088c0.084 0.518 0.43 0.956 0.914 1.155c0.483 0.201 1.038 0.136 1.463 -0.169l4.182 -3.013c0.413 0.131 0.828 0.259 1.246 0.379l1.805 4.826c0.183 0.49 0.607 0.853 1.122 0.953c0.514 0.104 1.043 -0.068 1.4 -0.452l3.523 -3.777c0.425 0.049 0.853 0.09 1.281 0.128l2.72 4.394c0.274 0.443 0.761 0.716 1.285 0.716s1.011 -0.272 1.286 -0.716l2.72 -4.394c0.428 -0.038 0.855 -0.079 1.28 -0.128l3.522 3.777c0.357 0.384 0.887 0.556 1.4 0.452c0.515 -0.101 0.939 -0.463 1.122 -0.953l1.805 -4.826c0.418 -0.12 0.833 -0.248 1.246 -0.379l4.183 3.013c0.425 0.305 0.979 0.37 1.462 0.169c0.484 -0.199 0.83 -0.638 0.914 -1.155l0.83 -5.088c0.384 -0.199 0.764 -0.406 1.143 -0.613l4.694 2.139c0.477 0.218 1.032 0.174 1.467 -0.117c0.436 -0.292 0.69 -0.787 0.671 -1.311l-0.18 -5.167c0.334 -0.267 0.665 -0.536 0.991 -0.814l5.031 1.185c0.511 0.12 1.047 -0.029 1.416 -0.402c0.372 -0.371 0.523 -0.904 0.403 -1.414l-1.185 -5.033c0.276 -0.327 0.548 -0.656 0.814 -0.992l5.166 0.181c0.521 0.021 1.019 -0.234 1.31 -0.671c0.292 -0.436 0.337 -0.991 0.118 -1.468l-2.139 -4.693c0.209 -0.379 0.414 -0.759 0.612 -1.145l5.09 -0.828c0.518 -0.083 0.954 -0.429 1.154 -0.914c0.2 -0.483 0.137 -1.036 -0.17 -1.462l-3.011 -4.183c0.13 -0.413 0.257 -0.828 0.378 -1.246l4.827 -1.805c0.491 -0.184 0.853 -0.607 0.953 -1.122c0.104 -0.514 -0.068 -1.043 -0.452 -1.4l-3.776 -3.523c0.046 -0.425 0.088 -0.853 0.125 -1.28l4.394 -2.72c0.445 -0.274 0.716 -0.761 0.716 -1.285S123.076 69.991 122.631 69.716ZM93.222 106.167c-1.678 -0.362 -2.745 -2.016 -2.385 -3.699c0.359 -1.681 2.012 -2.751 3.689 -2.389c1.678 0.359 2.747 2.016 2.387 3.696S94.899 106.526 93.222 106.167ZM91.729 96.069c-1.531 -0.328 -3.037 0.646 -3.365 2.18l-1.56 7.28c-4.814 2.185 -10.16 3.399 -15.79 3.399c-5.759 0.0 -11.221 -1.274 -16.121 -3.552l-1.559 -7.28c-0.328 -1.532 -1.834 -2.508 -3.364 -2.179l-6.427 1.38c-1.193 -1.228 -2.303 -2.536 -3.323 -3.917h31.272c0.354 0.0 0.59 -0.064 0.59 -0.386V81.932c0.0 -0.322 -0.236 -0.386 -0.59 -0.386h-9.146v-7.012h9.892c0.903 0.0 4.828 0.258 6.083 5.275c0.393 1.543 1.256 6.562 1.846 8.169c0.588 1.802 2.982 5.402 5.533 5.402h15.583c0.177 0.0 0.366 -0.02 0.565 -0.056c-1.081 1.469 -2.267 2.859 -3.544 4.158L91.729 96.069ZM48.477 106.015c-1.678 0.362 -3.33 -0.708 -3.691 -2.389c-0.359 -1.684 0.708 -3.337 2.386 -3.699c1.678 -0.359 3.331 0.711 3.691 2.392C51.222 103.999 50.154 105.655 48.477 106.015ZM36.614 57.91c0.696 1.571 -0.012 3.412 -1.581 4.107c-1.569 0.697 -3.405 -0.012 -4.101 -1.584c-0.696 -1.572 0.012 -3.41 1.581 -4.107C34.083 55.63 35.918 56.338 36.614 57.91ZM32.968 66.553l6.695 -2.975c1.43 -0.635 2.076 -2.311 1.441 -3.744l-1.379 -3.118h5.423V81.16H34.207c-0.949 -3.336 -1.458 -6.857 -1.458 -10.496C32.749 69.275 32.824 67.902 32.968 66.553ZM62.348 64.179v-7.205h12.914c0.667 0.0 4.71 0.771 4.71 3.794c0.0 2.51 -3.101 3.41 -5.651 3.41H62.348ZM109.28 70.664c0.0 0.956 -0.035 1.902 -0.105 2.841h-3.926c-0.393 0.0 -0.551 0.258 -0.551 0.643v1.803c0.0 4.244 -2.393 5.167 -4.49 5.402c-1.997 0.225 -4.211 -0.836 -4.484 -2.058c-1.178 -6.626 -3.141 -8.041 -6.241 -10.486c3.847 -2.443 7.85 -6.047 7.85 -10.871c0.0 -5.209 -3.571 -8.49 -6.005 -10.099c-3.415 -2.251 -7.196 -2.702 -8.216 -2.702H42.509c5.506 -6.145 12.968 -10.498 21.408 -12.082l4.786 5.021c1.082 1.133 2.874 1.175 4.006 0.092l5.355 -5.122c11.221 2.089 20.721 9.074 26.196 18.657l-3.666 8.28c-0.633 1.433 0.013 3.109 1.442 3.744l7.058 3.135C109.216 68.115 109.28 69.381 109.28 70.664ZM68.705 28.784c1.24 -1.188 3.207 -1.141 4.394 0.101c1.185 1.245 1.14 3.214 -0.103 4.401c-1.24 1.188 -3.207 1.142 -4.394 -0.102C67.418 31.941 67.463 29.972 68.705 28.784ZM105.085 58.061c0.695 -1.571 2.531 -2.28 4.1 -1.583c1.569 0.696 2.277 2.536 1.581 4.107c-0.695 1.572 -2.531 2.281 -4.101 1.584C105.098 61.473 104.39 59.634 105.085 58.061Z" fill="#000000"/></svg>`

const arrowCircleSVG = `<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke-width="1">
<path d="M3 8C3 6.67392 3.52678 5.40215 4.46446 4.46447C5.40214 3.52679 6.67391 3.00001 7.99999 3.00001C9.39779 3.00527 10.7394 3.55069 11.7444 4.52223L13 5.77778" stroke="black" stroke-linecap="round" stroke-linejoin="round"/>
<path d="M13 3.00001V5.77778H10.2222" stroke="black" stroke-linecap="round" stroke-linejoin="round"/>
<path d="M13 8C13 9.32608 12.4732 10.5978 11.5355 11.5355C10.5978 12.4732 9.32607 13 7.99999 13C6.60219 12.9947 5.26054 12.4493 4.25555 11.4778L3 10.2222" stroke="black" stroke-linecap="round" stroke-linejoin="round"/>
<path d="M5.77777 10.2222H3V13" stroke="black" stroke-linecap="round" stroke-linejoin="round"/>
</svg>`

// shadowZed mirrors shadow.rs: white page, pink drop shadows
// (hsla(0,0.5,0.5,0.3) = rgb(191,64,64)@0.3), labelled cells in rows for the
// rounded variants, the shadow-size scale, the blur scale and the spread scale.
// Cells use real per-side borders (right divider + label top rule, GPUI
// border_r_1/border_t_1) and rows a bottom rule (border_b_1).
func shadowZed() func() ui.Element {
	var theApp *shell.App
	appInits["shadow_zed"] = func(a *shell.App) { theApp = a }
	// Row 1 uses a pink shadow (hsla(0,0.5,0.5,0.3)); the size/blur/spread
	// rows use a neutral grey shadow (hsla(0,0,0,0.3)), exactly like shadow.rs.
	pink := ui.RGBA{R: 0.749, G: 0.251, B: 0.251, A: 0.3}
	grey := ui.RGBA{A: 0.3}
	black := ui.RGBA{A: 1}
	// base is shadow.rs's Shadow::base()/square()/rounded_*: a 64px white box;
	// rounded_full means radius = half the size, a true circle.
	base := func(radius float32) *ui.DivEl {
		return ui.Div().Size(64, 64).Bg(ui.RGB(0xffffff)).Rounded(radius).
			Border(1, ui.RGBA{A: 0.1})
	}
	box := func(radius, blur float32, sc ui.RGBA) ui.Element {
		return base(radius).Shadow(blur, 0, 8, sc)
	}
	spreadBox := func(spread float32) ui.Element {
		return base(32).ShadowSpread(8, 0, 8, spread, grey)
	}
	// elev* are gpui's Tailwind shadow_2xs..shadow_2xl presets, verbatim from
	// gpui_macros/src/styles.rs (sm..xl are two-shadow stacks, negative spread).
	a05, a10 := ui.RGBA{A: 0.05}, ui.RGBA{A: 0.1}
	elev := map[string]func() ui.Element{
		"None":        func() ui.Element { return base(32) },
		"2X Small":    func() ui.Element { return base(32).Shadow(0, 0, 1, a05) },
		"Extra Small": func() ui.Element { return base(32).Shadow(2, 0, 1, a05) },
		"Small": func() ui.Element {
			return base(32).Shadow(3, 0, 1, a10).ShadowSpread(2, 0, 1, -1, a10)
		},
		"Medium": func() ui.Element {
			return base(32).ShadowSpread(6, 0, 4, -1, a10).ShadowSpread(4, 0, 2, -2, a10)
		},
		"Large": func() ui.Element {
			return base(32).ShadowSpread(15, 0, 10, -3, a10).ShadowSpread(6, 0, 4, -4, a10)
		},
		"Extra Large": func() ui.Element {
			return base(32).ShadowSpread(25, 0, 20, -5, a10).ShadowSpread(10, 0, 8, -6, a10)
		},
		"2X Large": func() ui.Element {
			return base(32).ShadowSpread(50, 0, 25, -12, ui.RGBA{A: 0.25})
		},
	}
	// shadow.rs cells are w(relative(1/6)) of the 1000px window: five-cell
	// rows leave the last sixth empty; the eight-cell elevation row overflows
	// and flex-shrink squeezes its cells to 125px (measured from Zed).
	cellw := func(label string, e ui.Element, w float32) ui.Element {
		return ui.Div().Col().W(w).BorderRight(1, black).Children(
			ui.Div().H(130).JustifyCenter().ItemsCenter().Children(e),
			ui.Div().Grow().P(4).BorderTop(1, black).Children( // p_1
				ui.Text(label, ui.RGB(0x000000)).SizePx(12)),
		)
	}
	winW := func() float32 {
		if theApp != nil {
			return float32(theApp.W)
		}
		return 1000
	}
	cell := func(label string, e ui.Element) ui.Element { return cellw(label, e, winW()/6) }
	row := func(cells ...ui.Element) ui.Element {
		return ui.Div().Row().BorderBottom(1, black).Children(cells...)
	}
	elevCell := func(name string) ui.Element { return cellw(name, elev[name](), winW()/8) }
	return func() ui.Element {
		return ui.Div().Col().Grow().Bg(ui.RGB(0xffffff)).Children(
			row(
				cell("Square", box(0, 8, pink)),
				cell("Rounded 4", box(4, 8, pink)),
				cell("Rounded 8", box(8, 8, pink)),
				cell("Rounded 16", box(16, 8, pink)),
				cell("Circle", box(32, 8, pink)),
			),
			row(
				elevCell("None"),
				elevCell("2X Small"),
				elevCell("Extra Small"),
				elevCell("Small"),
				elevCell("Medium"),
				elevCell("Large"),
				elevCell("Extra Large"),
				elevCell("2X Large"),
			),
			row(
				cell("Blur 0", box(32, 0, grey)),
				cell("Blur 2", box(32, 2, grey)),
				cell("Blur 4", box(32, 4, grey)),
				cell("Blur 8", box(32, 8, grey)),
				cell("Blur 16", box(32, 16, grey)),
			),
			row(
				cell("Spread 0", spreadBox(0)),
				cell("Spread 2", spreadBox(2)),
				cell("Spread 4", spreadBox(4)),
				cell("Spread 8", spreadBox(8)),
				cell("Spread 16", spreadBox(16)),
			),
		)
	}
}

// animationZed mirrors animation.rs: white page, "Hello Animation" over the
// rotating arrow-circle glyph, and an "Other Panel" footer with a top rule.
// The glyph spins exactly like Zed's: a 2s repeating full turn eased with
// bounce(ease_in_out) (gpui/src/elements/animation.rs) — one eased second
// forward, one eased second back. Driven by a core tween (not wall clock) so
// the shell's damage-driven loop knows frames are needed.
func animationZed() func() ui.Element {
	arrow, _ := render.ParseSVG([]byte(arrowCircleSVG))
	var t core.Property[float32]
	return func() ui.Element {
		if !core.AnimationsRunning() {
			to := float32(1)
			if t.Get() > 0.5 {
				to = 0
			}
			core.Tween(&t, to, time.Second, core.EaseInOutQuad)
		}
		angle := t.Get() * 2 * math.Pi
		content := ui.Div().Col().Grow().JustifyCenter().ItemsCenter().Gap(16).Children(
			ui.Text("Hello Animation", ui.RGB(0x000000)).SizePx(20),
		)
		if arrow != nil {
			content.Children(ui.SVG(arrow, 80, 80).Tint(ui.RGB(0x000000)).Rotate(angle))
		}
		footer := ui.Div().Row().H(64).P(8).JustifyCenter().ItemsCenter().
			Bg(ui.RGBA{A: 0.05}).Children(
			ui.Text("Other Panel", ui.RGB(0x000000)),
		)
		return ui.Div().Col().Grow().Bg(ui.RGB(0xffffff)).Children(
			content,
			ui.Div().W(300).H(1).Bg(ui.RGBA{A: 0.1}),
			footer,
		)
	}
}

// frameObservers taps the frame's raw input for examples that need a
// window-level listener (gpui observe_keystrokes / on_key_down / canvas
// mouse handlers): each example registers under its name; main wires the
// running one as a FrameStart hook. Events reach the observer even when a
// focused element consumes them.
var frameObservers = map[string]func(mouse []app.MouseEvent, keys []app.KeyEvent){}

// keyObservers is the keys-only convenience over frameObservers.
var keyObservers = map[string]func(keys []app.KeyEvent){}

// appInits receive the shell App at startup, for examples that need live
// window metrics (a.W/a.H) or other app services in their view function.
var appInits = map[string]func(a *shell.App){}

// exampleOpts adjust the shell Options before the window opens (transparent
// or undecorated windows, resize bands).
var exampleOpts = map[string]func(o *shell.Options){}

// dragRegions provide the Hooks.DragRegion for undecorated examples.
var dragRegions = map[string]func(a *shell.App, x, y float32) bool{}

// keystrokeLabel formats one keystroke like input.rs shows them:
// `a -> "a"` for character keys, plain names for the rest.
func keystrokeLabel(ev app.KeyEvent) string {
	if ev.Rune != 0 {
		s := string(ev.Rune)
		if ev.Ctrl {
			return "ctrl-" + s
		}
		return fmt.Sprintf("%s -> %q", s, s)
	}
	names := map[app.Key]string{
		app.KeyBackspace: "backspace", app.KeyDelete: "delete",
		app.KeyEnter: "enter", app.KeyTab: "tab", app.KeyEscape: "escape",
		app.KeyLeft: "left", app.KeyRight: "right",
		app.KeyUp: "up", app.KeyDown: "down",
		app.KeyHome: "home", app.KeyEnd: "end",
	}
	n, ok := names[ev.Key]
	if !ok {
		return ""
	}
	if ev.Shift {
		n = "shift-" + n
	}
	if ev.Ctrl {
		n = "ctrl-" + n
	}
	return n
}

// inputZed mirrors input.rs: grey page, white header bar ("Keyboard <layout>"
// + yellow bordered Reset that clears field and history), an edge-to-edge
// white text field, and the recent keystrokes listed under it, newest first.
func inputZed() func() ui.Element {
	var theApp *shell.App
	appInits["input_zed"] = func(a *shell.App) { theApp = a }
	field := ui.NewInput("")
	var strokes []string
	keyObservers["input_zed"] = func(keys []app.KeyEvent) {
		for _, k := range keys {
			if !k.Down {
				continue
			}
			if l := keystrokeLabel(k); l != "" {
				strokes = append(strokes, l)
			}
		}
	}
	reset := func() { field.SetText(""); strokes = nil }
	yellow, black := ui.RGB(0xffff00), ui.RGB(0x000000)
	return func() ui.Element {
		header := ui.Div().Row().ItemsCenter().Bg(ui.RGB(0xffffff)).
			BorderBottom(1, ui.RGBA{A: 1}).Children(
			ui.Div().P(4).Children(ui.Text("Keyboard English (US)", black)),
			ui.Div().Grow(),
			// Reset: yellow, black border, hover blends toward grey
			// (yellow.blend(opaque_grey(0.5,0.5)) = 0.75,0.75,0.25); px_2,
			// height from the phi line box.
			ui.Div().PadXY(8, 0).Bg(yellow).Border(1, black).
				HoverBg(ui.RGBA{R: 0.75, G: 0.75, B: 0.25, A: 1}).
				Cursor(app.CursorPointer).OnClick(reset).
				Children(ui.Text("Reset", black)),
		)
		// TextInput: full width, no margin. input.rs puts its p_4 INSIDE the
		// white box (which fully covers the 0xeeeeee strip beneath) — our
		// InputEl pads its text internally, so the field spans edge to edge.
		fieldW := float32(300)
		if theApp != nil {
			fieldW = float32(theApp.W)
		}
		light := ui.Input(field, fieldW).
			Bg(ui.RGB(0xffffff)).Border(0, ui.RGBA{}).
			Accent(ui.RGB(0x3b5bdb)).TextColor(black).Rounded(0).
			SizePx(24) // input.rs text_size(px(24)), line_height 30 + p_4
		page := ui.Div().Col().Grow().Bg(ui.RGB(0xaaaaaa)).Children(
			header,
			light,
		)
		for i := len(strokes) - 1; i >= 0; i-- {
			page.Children(ui.Div().PadXY(4, 0).Children(ui.Text(strokes[i], black)))
		}
		return page
	}
}

// scrollableZed mirrors scrollable.rs: a white vertical scroll page holding a
// tall blue-bordered region with a wide green "Scroll Horizontal" box and a
// "Scroll Vertical" caption. (Inner horizontal scrolling is clipped statically
// — we expose vertical Scroll only.)
func scrollableZed() func() ui.Element {
	var stV, stH ui.ScrollState
	var theApp *shell.App
	appInits["scrollable_zed"] = func(a *shell.App) { theApp = a }
	blue := ui.RGBA{R: 0, G: 0, B: 1, A: 1}
	green := ui.RGBA{R: 0, G: 0.5, B: 0, A: 1}
	return func() ui.Element {
		// The 2000px green box lives in its own horizontal-only scroll region
		// nested inside the vertical page scroll, exactly like scrollable.rs
		// ("horizontal" inside "vertical"). Shift+wheel over it scrolls it
		// sideways; plain wheel bubbles out to the page. Hover darkens the
		// green (0.1 → 0.2), gpui's .hover style.
		greenBox := ui.Div().Size(2000, 150).
			Bg(ui.RGBA{R: 0, G: 0.5, B: 0, A: 0.1}).
			HoverBg(ui.RGBA{R: 0, G: 0.5, B: 0, A: 0.2}).
			Border(1, green).P(16).Children(
			ui.Text("Scroll Horizontal", ui.RGB(0x000000)),
		)
		winW, winH := float32(500), float32(500)
		if theApp != nil {
			winW, winH = float32(theApp.W), float32(theApp.H)
		}
		// Viewport width = window - outer p_4 (16) - blue border+p_4 (17)
		// per side.
		hscroll := ui.Scroll(&stH).W(winW - 66).H(150).Children(greenBox)
		inner := ui.Div().Col().H(5000).Gap(20). // mb_5 under the h-scroll
								Border(1, blue).Bg(ui.RGBA{R: 0, G: 0, B: 1, A: 0.05}).P(16).Children(
			hscroll,
			ui.Text("Scroll Vertical", ui.RGB(0x000000)),
		)
		return ui.Div().Col().Grow().Bg(ui.RGB(0xffffff)).Children(
			ui.Scroll(&stV).W(winW).H(winH).P(16).Children(
				ui.Text("Example for test 2 way scroll in nested layout", ui.RGB(0x000000)),
				inner,
			),
		)
	}
}

// opacityZed mirrors opacity.rs: blue "background text" on a light-gray page
// with a red-bordered panel (faded to opacity 0.5, as mid-animation) floated
// over it via Overlay, holding a blue inner box with a shadow and labels.
// (The app-icon image and emoji row are stand-ins — no PNG/emoji decode here.)
func opacityZed() func() ui.Element {
	var theApp *shell.App
	appInits["opacity_zed"] = func(a *shell.App) { theApp = a }
	icon := &ui.ImageCell{}
	icon.Data = readAsset("build/images/app-icon.png")
	cat := &ui.ImageCell{}
	cat.Data = readAsset("build/images/cat.gif")
	arrow, _ := render.ParseSVG([]byte(arrowCircleSVG))
	var op core.Property[float32]
	op.Set(0.5)
	// Click fades the panel back in from 0, like opacity.rs start_animation.
	// Zed's +0.005/frame is refresh-rate dependent — 200 frames ≈ 0.83s on
	// this 240Hz display.
	fade := func() {
		op.Set(0.001) // 0 would mean "inherit" to DivEl.Opacity
		core.Tween(&op, 1, 830*time.Millisecond, core.EaseLinear)
	}
	hovered := make([]bool, 6)
	emoji := []string{"🎊", "✈️", "🎉", "🎈", "🎁", "🎂"}
	return func() ui.Element {
		o := op.Get()
		// The page sets text_xl (20px), so every unsized child inherits it.
		bg := ui.Div().Grow().JustifyCenter().ItemsCenter().Children(
			ui.Text("This is background text.", ui.RGB(0x0000ff)).SizePx(20),
		)
		emojiRow := ui.Div().Row()
		for i, e := range emoji {
			d := ui.Div().Children(ui.Text(e, ui.RGB(0xffffff)).SizePx(20))
			if hovered[i] {
				d.Opacity(0.5)
			}
			i := i
			d.OnHoverChanged(func(h bool) {
				if hovered[i] != h {
					hovered[i] = h
					core.BumpRevision() // repaint with the new hover opacity
				}
			})
			emojiRow.Children(d)
		}
		inner := ui.Div().Col().Gap(8).JustifyCenter().ItemsCenter().Size(300, 300).
			Bg(ui.RGB(0x0000ff)).Border(3, ui.RGB(0x000000)).
			ShadowSpread(1, 10, 10, 5, ui.RGBA{A: 0.5}).Children(
			ui.LazyImage(icon, 32, 32),
			ui.Text("Opacity Panel (Click to test)", ui.RGB(0xffff00)).SizePx(20),
			ui.Div().P(16).Bg(ui.RGB(0x000000)).Children(
				ui.Text(fmt.Sprintf("opacity: %.1f", o), ui.RGB(0xffffff)).SizePx(20).
					UnderlineWavy().UnderlineColor(ui.RGB(0xff0000)).UnderlineThickness(2),
			),
			ui.SVG(arrow, 32, 32).Tint(ui.RGB(0x000000)),
			emojiRow,
			ui.LazyImage(cat, 48, 48),
		)
		winW, winH := float32(500), float32(500)
		if theApp != nil {
			winW, winH = float32(theApp.W), float32(theApp.H)
		}
		// absolute inset 8 (32px) on all sides: the panel tracks the window.
		panel := ui.Div().Size(winW-64, winH-64).JustifyCenter().ItemsCenter().
			Bg(ui.RGB(0xffffff)).Border(3, ui.RGB(0xff0000)).Opacity(o).
			OnClick(fade).Children(inner)
		return ui.Div().Col().Grow().Bg(ui.RGB(0xe0e0e0)).Children(
			bg,
			ui.Overlay(panel, 32, 32),
		)
	}
}

// readAsset loads a file fetched by scripts/fetch-assets.sh, tolerating
// being launched from the repo root or from examples/ (go run).
func readAsset(rel string) []byte {
	for _, p := range []string{rel, filepath.Join("..", rel)} {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	return nil
}

// ===========================================================================
// Group A ports — resting-state 1:1 mirrors of more Zed GPUI examples.
// ===========================================================================

// dragDropZed mirrors drag_drop.rs: a "Drop & Drop" title, a row of three
// draggable colour chips, and a drop zone.
func dragDropZed() func() ui.Element {
	type dragInfo struct {
		ix    int
		color uint32
	}
	var droppedOn *dragInfo // drag_drop.rs's drop_on: colors the zone after a drop
	chip := func(ix int, c uint32) ui.Element {
		return ui.Draggable(
			ui.Div().Size(120, 120).JustifyCenter().ItemsCenter().Bg(ui.RGB(0xffffff)).Rounded(4).
				Border(2, ui.RGB(c)).Children(ui.Text(fmt.Sprintf("Item (%d)", ix), ui.RGB(c))),
			dragInfo{ix, c})
	}
	return func() ui.Element {
		zoneBorder, zoneBg := ui.RGB(0x000000), ui.RGB(0xffffff)
		if d := droppedOn; d != nil {
			zoneBorder = ui.RGB(d.color)
			zoneBg = ui.RGB(d.color)
			zoneBg.A = 0.5
		}
		drop := ui.DropZone(
			ui.Div().Size(440, 130).JustifyCenter().ItemsCenter().Bg(zoneBg).
				Border(2, zoneBorder).Children(ui.Text("Drop items here", ui.RGB(0x555555))),
			func(p any) {
				if d, ok := p.(dragInfo); ok {
					droppedOn = &d
					core.BumpRevision() // repaint the zone in the drop color
				}
			})
		return ui.Div().Col().Grow().Gap(24).JustifyCenter().ItemsCenter().P(24).Bg(ui.RGB(0xffffff)).Children(
			ui.Text("Drop & Drop", ui.RGB(0x000000)).SizePx(20),
			ui.Div().Row().Gap(16).JustifyCenter().Children(chip(0, 0x0000ff), chip(1, 0xff0000), chip(2, 0x008000)),
			drop,
		)
	}
}

// focusVisibleZed mirrors focus_visible.rs: light page, bold title, an info
// message box, and blue action buttons (one drawn with a focus ring).
func focusVisibleZed() func() ui.Element {
	var msg core.Property[string]
	msg.Set("Try clicking vs tabbing! Click shows no border, Tab shows border.")
	keyObservers["focus_visible_zed"] = func(keys []app.KeyEvent) {
		for _, k := range keys {
			if k.Down && k.Key == app.KeyTab {
				if k.Shift {
					msg.Set("Pressed Shift-Tab - focus-visible border should appear!")
				} else {
					msg.Set("Pressed Tab - focus-visible border should appear!")
				}
			}
		}
	}
	var theApp *shell.App
	appInits["focus_visible_zed"] = func(a *shell.App) { theApp = a }
	const focusAmber, visGreen = 0xfbbf24, 0x10b981
	label := func(s string) ui.Element {
		return ui.Text(s, ui.RGB(0x374151)).SizePx(13).Bold()
	}
	section := func(lbl, btnLabel, clickMsg string, style func(*ui.DivEl)) ui.Element {
		wrapW := float32(720)
		if theApp != nil {
			wrapW = float32(theApp.W) - 80 // page p_32 + button padding
		}
		b := ui.Div().H(64).Grow().JustifyCenter().ItemsCenter().Bg(ui.RGB(0x2563eb)).
			Rounded(6).OnClick(func() { msg.Set(clickMsg) }).
			Children(ui.Wrapped(btnLabel, ui.RGB(0xffffff), wrapW).Center())
		style(b)
		return ui.Div().Col().Gap(8).Children(label(lbl), b)
	}
	return func() ui.Element {
		msgWrapW := float32(704)
		if theApp != nil {
			msgWrapW = float32(theApp.W) - 96 // page p_32 + banner p_16
		}
		return ui.Div().Col().Grow().Gap(24).P(32).Bg(ui.RGB(0xf3f4f6)).Children(
			ui.Text("CSS focus-visible Demo", ui.RGB(0x111827)).SizePx(24).Bold(),
			ui.Div().P(16).Rounded(6).Bg(ui.RGB(0xdbeafe)).Children(
				ui.Wrapped(msg.Get(), ui.RGB(0x1e3a8a), msgWrapW)),
			ui.Div().Col().Gap(16).Children(
				section("1. Regular .focus() - always visible:",
					"Button with .focus() - always shows border when focused",
					"Clicked button 1 - focus border is visible!",
					func(d *ui.DivEl) { d.FocusBorder(4, ui.RGB(focusAmber)) }),
				section("2. New .focus_visible() - only keyboard:",
					"Button with .focus_visible() - only shows border with keyboard",
					"Clicked button 2 - no border! Try Tab instead.",
					func(d *ui.DivEl) { d.FocusVisibleBorder(4, ui.RGB(visGreen)) }),
				section("3. Both .focus() (yellow) and .focus_visible() (green):",
					"Button with both .focus() and .focus_visible()",
					"Clicked button 3 - yellow border. Tab shows green!",
					func(d *ui.DivEl) {
						d.FocusBorder(4, ui.RGB(focusAmber)).FocusVisibleBorder(4, ui.RGB(visGreen))
					}),
			),
		)
	}
}

// tabStopZed mirrors tab_stop.rs: white page, a status line, and a column of
// black buttons — the focused one outlined with a blue 3px ring.
// tabStopZed mirrors tab_stop.rs with live focus: Tab/Shift-Tab move a blue
// 3px focus border through the stops in tab_index order (so the second
// "tab_index: 2" row focuses before "tab_index: 3", exactly like gpui); the
// dim "tab_stop: false" row is skipped.
func tabStopZed() func() ui.Element {
	var msg core.Property[string]
	msg.Set("Press `Tab`, `Shift-Tab` to switch focus.")
	keyObservers["tab_stop_zed"] = func(keys []app.KeyEvent) {
		for _, k := range keys {
			if k.Down && k.Key == app.KeyTab {
				if k.Shift {
					msg.Set("You have pressed `Shift-Tab`.")
				} else {
					msg.Set("You have pressed `Tab`.")
				}
			}
		}
	}
	blue := ui.RGB(0x0000ff)
	idxRow := func(label string, tab int) ui.Element {
		return ui.Div().H(40).JustifyCenter().ItemsCenter().Border(1, ui.RGB(0x000000)).
			FocusBorder(3, blue).HoverBg(ui.RGBA{A: 0.1}).TabIndex(tab).
			Children(ui.Text(label, ui.RGB(0x000000)))
	}
	blackBtn := func(label string, tab int, onClick func()) *ui.DivEl {
		b := ui.Div().H(40).Grow().JustifyCenter().ItemsCenter().Bg(ui.RGB(0x000000)).
			Border(1, ui.RGB(0x000000)).FocusBorder(3, blue).Shadow(3, 0, 1, ui.RGBA{A: 0.1}).
			TabIndex(tab).Children(ui.Text(label, ui.RGB(0xffffff)))
		if onClick != nil {
			b.OnClick(onClick)
		}
		return b
	}
	// gpui's tab groups (group tab_index 6/7, inner 1..3) flatten to the
	// group's rank; layout order breaks the tie exactly like the original.
	group := func(prefix string, tab int) ui.Element {
		return ui.Div().Col().Children(
			blackBtn(prefix+", 1]", tab, nil),
			blackBtn(prefix+", 2]", tab, nil),
			blackBtn(prefix+", 3]", tab, nil),
		)
	}
	return func() ui.Element {
		dim := ui.Div().H(40).JustifyCenter().ItemsCenter().Border(1, ui.RGB(0x000000)).
			Opacity(0.4).Children(ui.Text("tab_stop: false", ui.RGB(0x000000)))
		return ui.Div().Col().Grow().Gap(12).P(16).Bg(ui.RGB(0xffffff)).Children(
			ui.Text(msg.Get(), ui.RGB(0x000000)),
			idxRow("tab_index: 1", 1),
			idxRow("tab_index: 2", 2),
			idxRow("tab_index: 3", 3),
			dim,
			idxRow("tab_index: 2", 2),
			ui.Div().Row().Gap(12).Children(
				blackBtn("Button 1", 4, func() { msg.Set("You have clicked Button 1.") }),
				blackBtn("Button 2", 5, func() { msg.Set("You have clicked Button 2.") }),
			),
			group("Tab index [6", 6),
			group("Tab index [7", 7),
		)
	}
}

// popoverZed mirrors popover.rs: a trigger button with an open popover panel
// floated beneath it (white, blue-bordered), built with Overlay.
// popoverZed mirrors popover.rs interactively: an always-open popover
// anchored to its trigger, a click-to-open popover with click-outside
// dismissal, and a nested second-level popover (blue border) opened from a
// button inside the first — each anchored to its trigger, snapped to the
// window with an 8px margin, so they track resizes.
func popoverZed() func() ui.Element {
	var open, secOpen bool
	return func() ui.Element {
		darkBtn := func(s string, onClick func()) *ui.DivEl {
			b := ui.Div().PadXY(12, 4).Bg(ui.RGB(0x000000)).
				Children(ui.Text(s, ui.RGB(0xffffff)).SizePx(14))
			if onClick != nil {
				b.OnClick(onClick)
			}
			return b
		}
		// popover.rs popover(): white, rounded, shadow_lg, hairline border.
		popover := func(w float32, children ...ui.Element) *ui.DivEl {
			return ui.Div().Col().W(w).P(12).Gap(12).ItemsCenter().JustifyCenter().
				Rounded(6).Bg(ui.RGB(0xffffff)).Border(1, ui.RGBA{A: 0.1}).
				ShadowSpread(15, 0, 10, -3, ui.RGBA{A: 0.1}).
				ShadowSpread(6, 0, 4, -4, ui.RGBA{A: 0.1}).
				Children(children...)
		}
		// The popover centers its children as blocks, but wrapped text lines
		// stay left-aligned (gpui has no text-align here).
		wrapText := func(s string, w float32) ui.Element {
			return ui.Wrapped(s, ui.RGB(0x000000), w).SizePx(14)
		}

		btn0 := darkBtn("Opened Popover", nil)
		pop0 := popover(384, wrapText("This is a default opened Popover, we can use deferred to render it in a floating layer.", 360))
		ov0 := ui.Overlay(pop0, 0, 0).AnchorBelow(btn0, 4).SnapToWindow(8)

		btn1 := darkBtn("Open Popover", func() { open = true; core.BumpRevision() })
		trigger := ui.Div().Row().Gap(16).Children(btn0, btn1)

		body := ui.Div().Col().Grow().Gap(12).JustifyCenter().ItemsCenter().Bg(ui.RGB(0xffffff)).Children(
			trigger,
			ui.Wrapped("Here is an example text rendered, to ensure the Popover will float above this contents.",
				ui.RGB(0x000000), 480).SizePx(14),
			ui.Div().Size(480, 8).Bg(ui.RGBA{R: 1, A: 0.25}),
			ui.Div().Size(480, 8).Bg(ui.RGBA{R: 1, G: 1, A: 0.25}),
			ui.Div().Size(480, 8).Bg(ui.RGBA{B: 1, A: 0.25}),
			ui.Div().Size(480, 8).Bg(ui.RGBA{G: 0.5, A: 0.25}),
		)
		root := ui.Div().Grow().Bg(ui.RGB(0xffffff)).Children(body, ov0)

		if open {
			secBtn := darkBtn("Child Popover", func() { secOpen = true; core.BumpRevision() })
			pop1Kids := []ui.Element{
				wrapText("This is first level Popover, we can use deferred to render it in a floating layer.\nClick outside to close.", 360),
				secBtn,
			}
			pop1 := popover(384, pop1Kids...)
			ov1 := ui.Overlay(pop1, 0, 0).AnchorBelow(btn1, 4).SnapToWindow(8)
			if !secOpen {
				ov1.OnClickOutside(func() { open = false; core.BumpRevision() })
			}
			if secOpen {
				pop2 := popover(300, wrapText("This is second level Popover with nested deferred!", 276)).
					Border(1, ui.RGB(0x0000ff))
				pop1.Children(ui.Overlay(pop2, 0, 0).AnchorBelow(secBtn, 4).SnapToWindow(8).
					OnClickOutside(func() { secOpen = false; core.BumpRevision() }))
			}
			root.Children(ov1)
		}
		return root
	}
}

// anchorZed mirrors anchor.rs: "Popover with Anchor" over a 3x3 grid of
// outlined buttons (corners hoverable in the original).
// anchorZed mirrors anchor.rs interactively: hovering a grid button shows a
// dark tooltip pinned by that button's Anchor variant — Top* pin below the
// button, Bottom* open upward from its top edge, Left/RightCenter beside it —
// snapped to the window like gpui's anchored().snap_to_window().
func anchorZed() func() ui.Element {
	type spec struct {
		label  string
		pin    ui.Anchor
		dx, dy float32 // anchor point, relative to the button's top-left
	}
	const bw, bh = 120, 65
	specs := [9]*spec{
		{"TopLeft", ui.AnchorTopLeft, 0, bh},
		{"TopCenter", ui.AnchorTopCenter, bw / 2, bh},
		{"TopRight", ui.AnchorTopRight, bw, bh},
		{"LeftCenter", ui.AnchorLeftCenter, bw, bh / 2},
		nil, // center cell: empty, not hoverable
		{"RightCenter", ui.AnchorRightCenter, 0, bh / 2},
		{"BottomLeft", ui.AnchorBottomLeft, 0, 0},
		{"BottomCenter", ui.AnchorBottomCenter, bw / 2, 0},
		{"BottomRight", ui.AnchorBottomRight, bw, 0},
	}
	hovered := -1
	return func() ui.Element {
		// anchor.rs: size_128 (512px) grid, 3 equal tracks, gap_6 (24px);
		// the 120x65 buttons sit at each track's top-left.
		const track = (512 - 2*24) / 3.0
		var cells [9]*ui.DivEl
		var rows []ui.Element
		for r := range 3 {
			var rowCells []ui.Element
			for c := range 3 {
				i := r*3 + c
				cell := ui.Div().Size(bw, bh).JustifyCenter().ItemsCenter().Bg(ui.RGB(0xffffff))
				if s := specs[i]; s != nil {
					fg, bg := ui.RGB(0x000000), ui.RGB(0xffffff)
					if hovered == i {
						fg, bg = ui.RGB(0xffffff), ui.RGB(0x000000)
					}
					cell.Bg(bg).Rounded(8).Border(1, ui.RGB(0x000000)).
						Children(ui.Text(s.label, fg).SizePx(13))
					cell.OnHoverChanged(func(h bool) {
						if h && hovered != i {
							hovered = i
							core.BumpRevision()
						} else if !h && hovered == i {
							hovered = -1
							core.BumpRevision()
						}
					})
				}
				cells[i] = cell
				rowCells = append(rowCells, ui.Div().Size(track, track).Children(cell))
			}
			rows = append(rows, ui.Div().Row().Gap(24).Children(rowCells...))
		}
		grid := ui.Div().Col().Gap(24).Children(rows...)
		root := ui.Div().Col().Grow().Gap(16).JustifyCenter().ItemsCenter().Bg(ui.RGB(0xffffff)).Children(
			ui.Text("Popover with Anchor", ui.RGB(0x000000)),
			grid,
		)
		if hovered >= 0 && specs[hovered] != nil {
			s := specs[hovered]
			tip := ui.Div().PadXY(8, 2).Bg(ui.RGBA{A: 0.75}).Rounded(4).
				Shadow(3, 0, 1, ui.RGBA{A: 0.1}).
				Children(ui.Text(s.label, ui.RGB(0xffffff)).SizePx(14))
			root.Children(ui.Overlay(tip, 0, 0).
				AnchorTo(cells[hovered], s.dx, s.dy).Pin(s.pin).SnapToWindow(0))
		}
		return root
	}
}

// paintingZed mirrors painting.rs 1:1: black quads under 50%-alpha squares
// (reading as thick-bordered colour tiles), two overlapping translucent red
// circles, a Rust-logo stand-in (lyon's generated logo path isn't ported —
// black disc + R), the blue lightning bolt, the gradient star and ribbon
// (2-stop sRGB approximations of gpui's positioned Oklab stops), the
// 5-segment pie chart, the green 1px sine polyline — plus live mouse drawing
// with Dashed/Solid and Clear, like the original. (Shift-for-straight-lines
// is not ported: MouseEvents carry no modifier state.)
func paintingZed() func() ui.Element {
	type pt struct{ x, y float32 }
	var drawn [][]pt
	var start pt
	painting := false
	dashed := false
	var canvasEl *ui.Canvas // last frame's canvas, for mouse->local coords
	rustLogo, _ := render.ParseSVG([]byte(rustLogoSVG))

	frameObservers["painting_zed"] = func(mouse []app.MouseEvent, _ []app.KeyEvent) {
		if canvasEl == nil {
			return
		}
		b := canvasEl.Bounds()
		for _, m := range mouse {
			switch {
			case m.Kind == app.MouseDown && m.Button == 1 &&
				m.X >= b.X && m.X < b.X+b.W && m.Y >= b.Y && m.Y < b.Y+b.H:
				painting = true
				start = pt{m.X - b.X, m.Y - b.Y}
				drawn = append(drawn, []pt{start})
				core.BumpRevision()
			case m.Kind == app.MouseMove && painting:
				if n := len(drawn); n > 0 {
					p := pt{m.X - b.X, m.Y - b.Y}
					if m.Shift {
						// painting.rs: shift snaps the point to the stroke
						// start's dominant axis (straight line).
						if dx, dy := p.x-start.x, p.y-start.y; dx*dx > dy*dy {
							p.y = start.y
						} else {
							p.x = start.x
						}
					}
					// Skip sub-2px steps: slow mouse movement otherwise
					// stacks near-coincident segments whose join artifacts
					// clutter the 1px stroke.
					if last := drawn[n-1][len(drawn[n-1])-1]; (p.x-last.x)*(p.x-last.x)+(p.y-last.y)*(p.y-last.y) < 4 {
						continue
					}
					drawn[n-1] = append(drawn[n-1], p)
					core.BumpRevision()
				}
			case m.Kind == app.MouseUp:
				painting = false
			}
		}
	}

	// circle appends a full circle to a fresh path via 4 cubic arcs.
	circle := func(cx, cy, r float32) *render.Path {
		const k = 0.5523 // cubic circle constant
		p := render.NewPath()
		p.MoveTo(cx+r, cy)
		p.CubicTo(cx+r, cy+k*r, cx+k*r, cy+r, cx, cy+r)
		p.CubicTo(cx-k*r, cy+r, cx-r, cy+k*r, cx-r, cy)
		p.CubicTo(cx-r, cy-k*r, cx-k*r, cy-r, cx, cy-r)
		p.CubicTo(cx+k*r, cy-r, cx+r, cy-k*r, cx+r, cy)
		p.Close()
		return p
	}

	return func() ui.Element {
		btn := func(label string, onClick func()) ui.Element {
			return ui.Div().PadXY(12, 4).Bg(ui.RGB(0x000000)).OnClick(onClick).
				PressBg(ui.RGBA{A: 0.8}).
				Children(ui.Text(label, ui.RGB(0xffffff)).SizePx(14))
		}
		canvas := ui.NewCanvasFont(888, 470, func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
			ox, oy := rect.X, rect.Y
			at := func(x, y float32) (float32, float32) { return ox + x, oy + y }

			// Row 1: black quads beneath 50%-alpha red/blue/green/black
			// squares — the offset black quad reads as a thick dark border.
			for _, q := range [][4]float32{{70, 70, 40, 40}, {170, 70, 40, 40}, {270, 70, 40, 40}, {370, 70, 40, 40}, {450, 50, 80, 80}} {
				x, y := at(q[0], q[1])
				r.FillRect(x, y, q[2], q[3], ui.RGBA{A: 1})
			}
			half := func(c ui.RGBA) ui.RGBA { c.A = 0.5; return c }
			sq := []struct {
				x float32
				c ui.RGBA
			}{
				{50, half(ui.RGBA{R: 1, A: 1})},
				{150, half(ui.RGBA{B: 1, A: 1})},
				{250, half(ui.RGBA{G: 1, A: 1})},
				{350, half(ui.RGBA{A: 1})},
			}
			for _, s := range sq {
				x, y := at(s.x, 50)
				r.FillRect(x, y, 80, 80, s.c)
			}
			// Two overlapping 50% red circles — the lens is darker red.
			for _, cx := range []float32{530, 570} {
				x, y := at(cx, 85)
				r.FillPath(circle(x, y, 30), half(ui.RGBA{R: 1, A: 1}))
			}

			// Row 2: the Rust logo, from the same lyon-generated path
			// painting.rs uses (translate 10,200 scale 0.9).
			if rustLogo != nil {
				lx, ly := at(10, 200)
				r.DrawSVG(rustLogo, lx, ly, 0.9, ui.RGBA{A: 1})
			}

			// Lightning bolt: painting.rs's polygon self-intersects at
			// (200,250); lyon's nonzero winding fills both loops — two
			// skinny triangles — which our simple-polygon ear clipper
			// can't, so fill the two loops explicitly.
			for _, tri := range [][6]float32{
				{150, 300, 200, 225, 200, 250},
				{200, 250, 200, 275, 250, 200},
			} {
				p := render.NewPath()
				x0, y0 := at(tri[0], tri[1])
				x1, y1 := at(tri[2], tri[3])
				x2, y2 := at(tri[4], tri[5])
				p.MoveTo(x0, y0)
				p.LineTo(x1, y1)
				p.LineTo(x2, y2)
				p.Close()
				r.FillPath(p, ui.RGB(0x1d4ed8))
			}

			// Star with a vertical yellow->orange gradient (gpui: stops at
			// 0.7/1.0 in Oklab; ours is a 2-stop sRGB path gradient).
			star := render.NewPath()
			pts := [][2]float32{{350, 200}, {370, 260}, {430, 260}, {380, 300}, {400, 360}, {350, 320}, {300, 360}, {320, 300}, {270, 260}, {330, 260}}
			for i, q := range pts {
				x, y := at(q[0], q[1])
				if i == 0 {
					star.MoveTo(x, y)
				} else {
					star.LineTo(x, y)
				}
			}
			star.Close()
			// gpui: linear_color_stop(yellow, 0.7) → (orange, 1.0): solid
			// yellow until 70% down, then blend to orange.
			r.FillPathGradientStops(star, ui.RGB(0xFACC15), ui.RGB(0xD56D0C), math.Pi/2, 0.7, 1)

			// Gradient ribbon: quadratic-flag shape, blue->red top-down.
			rib := render.NewPath()
			q2c := func(px0, py0, cx, cy, x1, y1 float32) {
				rib.CubicTo(px0+2*(cx-px0)/3, py0+2*(cy-py0)/3, x1+2*(cx-x1)/3, y1+2*(cy-y1)/3, x1, y1)
			}
			r0x, r0y := at(450, 280) // bottom-left
			rib.MoveTo(r0x, r0y)
			c1x, c1y := at(450, 230)
			t1x, t1y := at(530, 230)
			q2c(r0x, r0y, c1x, c1y, t1x, t1y)
			t2x, t2y := at(570, 230)
			rib.LineTo(t2x, t2y)
			c2x, c2y := at(650, 230)
			e2x, e2y := at(650, 280)
			q2c(t2x, t2y, c2x, c2y, e2x, e2y)
			rib.LineTo(r0x, r0y)
			rib.Close()
			// gpui: blue holds until 40% down, then blends to red — the
			// transition band reads darker (purple) mid-ribbon.
			r.FillPathGradientStops(rib, ui.RGBA{B: 1, A: 1}, ui.RGBA{R: 1, A: 1}, math.Pi/2, 0.4, 1)

			// Pie chart: 5 wedges around (775,255), r 96 (painting.rs's
			// segment endpoints), arcs stepped as short polyline fans.
			pcx, pcy := at(775, 255)
			segs := []struct {
				sx, sy, ex, ey float32
				c              uint32
			}{
				{871, 255, 747, 163, 0x1374e9},
				{747, 163, 679, 263, 0xe13527},
				{679, 263, 754, 349, 0x0751ce},
				{754, 349, 854, 310, 0x209742},
				{854, 310, 871, 255, 0xfbc10a},
			}
			for _, s := range segs {
				sxx, syy := at(s.sx, s.sy)
				exx, eyy := at(s.ex, s.ey)
				a0 := math.Atan2(float64(syy-pcy), float64(sxx-pcx))
				a1 := math.Atan2(float64(eyy-pcy), float64(exx-pcx))
				d := a1 - a0
				for d > math.Pi {
					d -= 2 * math.Pi
				}
				for d < -math.Pi {
					d += 2 * math.Pi
				}
				// Interpolate the radius and land exactly on the given end
				// point so adjacent wedges share their radial edge with no
				// sliver seams.
				rad0 := float32(math.Hypot(float64(sxx-pcx), float64(syy-pcy)))
				rad1 := float32(math.Hypot(float64(exx-pcx), float64(eyy-pcy)))
				p := render.NewPath()
				p.MoveTo(sxx, syy)
				for t := 1; t < 16; t++ {
					f := float32(t) / 16
					a := a0 + d*float64(f)
					rad := rad0 + (rad1-rad0)*f
					p.LineTo(pcx+rad*float32(math.Cos(a)), pcy+rad*float32(math.Sin(a)))
				}
				p.LineTo(exx, eyy)
				p.LineTo(pcx, pcy)
				p.Close()
				r.FillPath(p, ui.RGB(s.c))
			}

			// Green 1px sine polyline ("jagged" 10px steps, like the original).
			wave := render.NewPath()
			wx, wy := at(40, 420)
			wave.MoveTo(wx, wy)
			for i := 1; i < 50; i++ {
				x, y := at(40+float32(i)*10, 420+float32(math.Sin(float64(i)*10))*40)
				wave.LineTo(x, y)
			}
			r.StrokePath(wave, 1, ui.RGBA{G: 0.5, A: 1})

			// User-drawn lines: 1px black polylines, optionally dashed (4/2).
			for _, line := range drawn {
				if len(line) < 2 {
					continue
				}
				if !dashed {
					p := render.NewPath()
					p.MoveTo(ox+line[0].x, oy+line[0].y)
					for _, q := range line[1:] {
						p.LineTo(ox+q.x, oy+q.y)
					}
					r.StrokePath(p, 1, ui.RGBA{A: 1})
					continue
				}
				// Dash by walking the polyline: 4px on, 2px off.
				const dash, gap = 4, 2
				on, rem := true, float32(dash)
				var p *render.Path
				for i := 1; i < len(line); i++ {
					x0, y0 := line[i-1].x, line[i-1].y
					x1, y1 := line[i].x, line[i].y
					segLen := float32(math.Hypot(float64(x1-x0), float64(y1-y0)))
					for segLen > 0 {
						step := min(rem, segLen)
						t := step / segLen
						nx, ny := x0+(x1-x0)*t, y0+(y1-y0)*t
						if on {
							if p == nil {
								p = render.NewPath()
								p.MoveTo(ox+x0, oy+y0)
							}
							p.LineTo(ox+nx, oy+ny)
						}
						segLen -= step
						rem -= step
						x0, y0 = nx, ny
						if rem <= 0 {
							if on && p != nil {
								r.StrokePath(p, 1, ui.RGBA{A: 1})
								p = nil
							}
							on = !on
							rem = dash
							if !on {
								rem = gap
							}
						}
					}
				}
				if p != nil {
					r.StrokePath(p, 1, ui.RGBA{A: 1})
				}
			}
		})
		canvasEl = canvas
		toggleLabel := "Dashed"
		if dashed {
			toggleLabel = "Solid"
		}
		return ui.Div().Col().Grow().P(16).Gap(8).Bg(ui.RGB(0xffffff)).Children(
			ui.Div().Row().Gap(8).ItemsCenter().Children(
				ui.Div().Grow().Children(ui.Text("Mouse down any point and drag to draw lines (Hold on shift key to draw straight lines)", ui.RGB(0x000000)).SizePx(14)),
				btn(toggleLabel, func() { dashed = !dashed; core.BumpRevision() }),
				btn("Clear", func() { drawn = nil; core.BumpRevision() }),
			),
			canvas,
		)
	}
}

// textWrapperZed mirrors text_wrapper.rs: a row of red-bordered ellipsis boxes
// over a wrapped long paragraph — including the CJK run, which renders via the
// font fallback chain (Noto Sans CJK behind the Latin UI font).
// textWrapperZed mirrors text_wrapper.rs: the first-row boxes hug their
// content (only the third stretches and ellipsizes), the blue rows clamp
// with "…", the green rows clamp with a clean cut, NOWRAP clips one line —
// all at the live window width, so resizing reflows like Zed.
func textWrapperZed() func() ui.Element {
	const para = "The longest word 你好世界这段是中文，こんにちはこの段落は日本語です in any of " +
		"the major English language dictionaries is pneumonoultramicroscopicsilicovolcanoconiosis, " +
		"a word that refers to a lung disease contracted from the inhalation of very fine silica " +
		"particles, a url https://github.com/zed-industries/zed/pull/35724?query=foo&bar=2, " +
		"specifically from a volcano; medically, it is the same as silicosis."
	var theApp *shell.App
	appInits["text_wrapper_zed"] = func(a *shell.App) { theApp = a }
	black := ui.RGB(0x000000)
	return func() ui.Element {
		red := ui.RGB(0xff0000)
		blue := ui.RGB(0x0000ff)
		green := ui.RGB(0x008000)
		W := float32(784)
		if theApp != nil {
			W = float32(theApp.W) - 16 // page p_2 padding
		}
		// Row 1: the first two boxes size to their text; the third takes the
		// rest of the row and ellipsizes to whatever width it gets.
		hugBox := func(s string) ui.Element {
			return ui.Div().P(4).Border(1, red).Children(ui.Text(s, black))
		}
		growBox := ui.Div().Grow().P(4).Border(1, red).Children(
			ui.Text("A short text in normal div", black).Truncate())
		clamp := func(prefix string, lines int, bc ui.RGBA, ellipsis bool) ui.Element {
			p := ui.Wrapped(prefix+para, black, W-8).SizePx(20).Lines(lines)
			if !ellipsis {
				p.NoEllipsis()
			}
			return ui.Div().W(W).P(3).Border(1, bc).Children(p)
		}
		nowrap := ui.Div().W(W).P(3).Border(1, black).Children(
			ui.Text("NOWRAP: "+para, black).SizePx(20).TruncateClip())
		return ui.Div().Col().Grow().P(8).Gap(8).Bg(ui.RGB(0xffffff)).Children(
			ui.Div().Row().Gap(8).Children(
				hugBox("longer text in flex 1"), hugBox("short flex"), growBox),
			clamp("ELLIPSIS: ", 1, blue, true),
			clamp("ELLIPSIS 2 lines: ", 2, blue, true),
			clamp("TRUNCATE: ", 1, green, false),
			clamp("TRUNCATE 3 lines: ", 3, green, false),
			nowrap,
			ui.Wrapped(para, black, W).SizePx(20),
		)
	}
}

// listExampleZed mirrors list_example.rs: a debug status line over a scrolled
// list of striped rows.
// listExampleZed mirrors list_example.rs: a scroll-metrics readout (offset /
// max / fraction + OK/BUG line) over a bottom-aligned list of 40 variable-
// height striped rows beside a hand-drawn scrollbar track+thumb. gpui's demo
// exists to expose an unclamped-thumb bug; our scroll offsets clamp, so the
// fraction can't exceed 1 and the status stays green.
func listExampleZed() func() ui.Element {
	var st ui.ScrollState
	var theApp *shell.App
	appInits["list_example_zed"] = func(a *shell.App) { theApp = a }
	st.Y = 1e9 // ListAlignment::Bottom — first layout clamps to the end
	var trackEl *ui.DivEl
	dragging := false
	// Thumb dragging (our addition — the Rust example doesn't implement it):
	// press in the track jumps/starts a drag; the thumb center follows y.
	jump := func(y float32) {
		b := trackEl.Bounds()
		viewH, maxOff := st.ViewH(), st.MaxY()
		if maxOff <= 0 || viewH <= 0 {
			return
		}
		thumbH := max(viewH*viewH/(viewH+maxOff), 30)
		if track := viewH - thumbH; track > 0 {
			fr := (y - b.Y - thumbH/2) / track
			st.Y = min(max(fr, 0), 1) * maxOff
			core.BumpRevision()
		}
	}
	frameObservers["list_example_zed"] = func(mouse []app.MouseEvent, _ []app.KeyEvent) {
		if trackEl == nil {
			return
		}
		b := trackEl.Bounds()
		for _, m := range mouse {
			switch {
			case m.Kind == app.MouseDown && m.Button == 1 &&
				m.X >= b.X && m.X < b.X+b.W && m.Y >= b.Y && m.Y < b.Y+b.H:
				dragging = true
				jump(m.Y)
			case m.Kind == app.MouseMove && dragging:
				jump(m.Y)
			case m.Kind == app.MouseUp:
				dragging = false
			}
		}
	}
	return func() ui.Element {
		// Metrics from the previous frame's layout (Zed reads its ListState
		// the same way).
		offset, maxOff, viewH := st.Y, st.MaxY(), st.ViewH()
		fraction := float32(0)
		if maxOff > 0 {
			fraction = offset / maxOff
		}
		statusW := float32(368)
		if theApp != nil {
			statusW = float32(theApp.W) - 32 // page p_4
		}
		status := ui.Div().Col().Gap(4).Children(
			ui.Wrapped(fmt.Sprintf("offset: %.0f / max: %.0f | fraction: %.3f", offset, maxOff, fraction),
				ui.RGB(0x000000), statusW).SizePx(14),
			ui.Wrapped("OK: fraction <= 1.0 — thumb is within track.", ui.RGB(0x008800), statusW).SizePx(14),
		)
		// The list: 40 rows, 30 + (i%5)*10 tall, striped, hairline separators.
		s := ui.Scroll(&st).Grow().BarColor(ui.RGBA{}) // flex_1; custom bar below
		for i := range 40 {
			h := 30 + float32(i%5)*10
			bg := ui.RGB(0xffffff)
			if i%2 == 0 {
				bg = ui.RGB(0xfafafa)
			}
			s.Children(ui.Div().Row().H(h).PadXY(12, 0).ItemsCenter().Bg(bg).
				BorderBottom(1, ui.RGB(0xeeeeee)).Children(
				ui.Text(fmt.Sprintf("Item %d", i), ui.RGB(0x000000)).SizePx(14)))
		}
		// Hand-drawn scrollbar (list_example.rs draws its own track+thumb).
		thumbH := float32(30)
		if total := viewH + maxOff; total > 0 && viewH > 0 {
			thumbH = max(viewH*viewH/total, 30)
		}
		thumbTop := (viewH - thumbH) * fraction
		track := ui.Div().Col().W(12).Bg(ui.RGB(0xe0e0e0)).Children(
			ui.Div().H(thumbTop),
			ui.Div().W(12).H(thumbH).Bg(ui.RGB(0x888888)).Rounded(4), // rounded_sm thumb
		)
		trackEl = track
		// P(1) insets the content past the 1px border (CSS borders take
		// layout space; our SDF border paints under children otherwise).
		body := ui.Div().Row().Grow().P(1).Border(1, ui.RGB(0xcccccc)).Rounded(2).Children(s, track)
		return ui.Div().Col().Grow().P(16).Gap(8).Bg(ui.RGB(0xffffff)).Children(status, body)
	}
}

// uniformListZed mirrors uniform_list.rs: a virtualized list of 50 rows,
// px_2 padding, pointer cursor, and clicks logged to stdout like the
// original's println!.
func uniformListZed() func() ui.Element {
	var st ui.ListState
	return func() ui.Element {
		list := ui.List(&st, 26, 50, func(i int) ui.Element {
			item := i + 1
			return ui.Div().Row().H(26).PadXY(8, 0).ItemsCenter().
				Cursor(app.CursorPointer).
				OnClick(func() { fmt.Printf("clicked Item %d\n", item) }).
				Children(ui.Text(fmt.Sprintf("Item %d", item), ui.RGB(0x000000)))
		}).Grow()
		return ui.Div().Col().Grow().Bg(ui.RGB(0xffffff)).Children(list)
	}
}

// gridLayoutZed approximates grid_layout.rs: a 5x5-ish CAD layout of dashed
// white-bordered colour blocks on a grey page. (We lack CSS grid spans, so the
// spans are laid out manually with rows/cols.)
func gridLayoutZed() func() ui.Element {
	var theApp *shell.App
	appInits["grid_layout_zed"] = func(a *shell.App) { theApp = a }
	// items_center only, like grid_layout.rs: content horizontally centered,
	// top-aligned. Long labels wrap to the cell (grid children size to their
	// track, so text wraps at the track width like CSS).
	block := func(c uint32, label string, tc uint32, wrapW float32) ui.Element {
		var text ui.Element = ui.Text(label, ui.RGB(tc))
		if wrapW > 0 {
			text = ui.Wrapped(label, ui.RGB(tc), wrapW).Center()
		}
		return ui.Div().Grow().Bg(ui.RGB(c)).Rounded(6).Border(1, ui.RGB(0xffffff)).BorderDashed().
			ItemsCenter().Children(text)
	}
	return func() ui.Element {
		// gpui named colours: red()=0xff0000, green()=0x008000, blue()=0x0000ff.
		// A real 5×5 grid with col/row spans (GPUI col_span_full / row_span);
		// gap_1 = 4px, no container padding, default-black labels except the
		// AD and Footer blocks (text_color(white) in grid_layout.rs).
		winW := float32(500)
		if theApp != nil {
			winW = float32(theApp.W)
		}
		track := (winW - 4*4) / 5 // 5 tracks, 4px gaps
		grid := ui.Grid(5, 5).Gap(4).Grow().
			Cell(block(0xffffff, "Header", 0x000000, 0), 5, 1).                  // col_span_full
			Cell(block(0xff0000, "Table of contents", 0x000000, track-6), 1, 2). // h_56: stops short
			Cell(block(0x008000, "Content", 0x000000, 0), 3, 3).
			Cell(block(0x0000ff, "AD :(", 0xffffff, 0), 1, 3).
			Cell(block(0x000000, "Footer", 0xffffff, 0), 5, 1)
		return ui.Div().Col().Grow().Bg(ui.RGB(0x505050)).
			Shadow(16, 0, 8, ui.RGBA{A: 0.5}).Border(1, ui.RGB(0xffffff)).Children(grid)
	}
}

// imageLoadingZed mirrors image_loading.rs resting state: red-bordered image
// slots showing the fallback element (the async load/network image isn't wired
// into the example harness).
// imageLoadingZed mirrors image_loading.rs live: four 48px red-bordered image
// slots — (1) loads just under gpui's 200ms LOADING_DELAY so the loader never
// shows, (2) shows the pulsating loading box for 5s then the icon, (3) pulses
// 5s then fails to the "?" fallback, (4) falls back immediately (bad path).
// Clicking a slot evicts and reloads it, restarting its cycle.
func imageLoadingZed() func() ui.Element {
	logo := &ui.ImageCell{}
	logo.Data = readAsset("build/images/app-icon.png")
	starts := [4]time.Time{}
	for i := range starts {
		starts[i] = time.Now()
	}
	// The loading box "breathes" black between 0.04 and 0.24 opacity
	// (pulsating_between), driven by a ping-pong tween so the damage loop
	// keeps frames flowing while anything is loading. Started lazily on the
	// first frame — every example constructor runs at startup, and an eager
	// perpetual tween would keep ALL example windows rendering forever.
	var pulse core.Property[float32]
	pulse.Set(0.04)
	var pulseOnce sync.Once
	startPulse := func() {
		pulseOnce.Do(func() {
			core.TweenLoop(&pulse, 0.24, 1500*time.Millisecond, core.EaseInOutQuad, core.PingPong)
		})
	}
	fallback := func() ui.Element {
		c := ui.RGBA{A: 0.5}
		return ui.Div().Size(42, 42).Rounded(2).Border(1, c).
			JustifyCenter().ItemsCenter().Children(ui.Text("?", c).SizePx(14))
	}
	loading := func() ui.Element {
		return ui.Div().Size(42, 42).Rounded(2).Bg(ui.RGBA{A: pulse.Get()})
	}
	return func() ui.Element {
		startPulse()
		slot := func(i int, delay time.Duration, fail bool) ui.Element {
			elapsed := time.Since(starts[i])
			var content ui.Element
			switch {
			case elapsed < delay && delay >= 200*time.Millisecond:
				content = loading()
			case elapsed < delay:
				content = ui.Div() // under LOADING_DELAY: no loader shown
			case fail:
				content = fallback()
			default:
				content = ui.LazyImage(logo, 46, 46)
			}
			return ui.Div().Size(48, 48).Border(1, ui.RGB(0xff0000)).
				JustifyCenter().ItemsCenter().Cursor(app.CursorPointer).
				OnClick(func() { starts[i] = time.Now(); core.BumpRevision() }).
				Children(content)
		}
		// The four img elements are adjacent children of one centered flex
		// box (no gap) — they render as a joined strip.
		return ui.Div().Col().Grow().JustifyCenter().Bg(ui.RGB(0xffffff)).Children(
			ui.Div().Row().JustifyCenter().Children(
				slot(0, 175*time.Millisecond, false),
				slot(1, 5*time.Second, false),
				slot(2, 5*time.Second, true),
				slot(3, 0, true),
			),
		)
	}
}

// ===========================================================================
// Groups B + C ports (completeness). Resting-state mirrors; some approximate
// features we lack (radial gradients, font weights, GIF/animation, async/net
// image load, true CSS-grid spans) — see check/NOTES.md.
// ===========================================================================

// gradientZed mirrors gradient.rs: a header with a colour-space toggle over a
// row of linear-gradient swatches. (We have linear gradients; oklab/srgb
// interpolation and the colour-space toggle aren't modelled.)
// gradientZed mirrors gradient.rs, including the live color-space toggle:
// the header button flips every gradient between sRGB and Oklab blending
// (Oklab passes through perceptually-even midpoints — blue→red avoids sRGB's
// bright purple).
func gradientZed() func() ui.Element {
	var theApp *shell.App
	appInits["gradient_zed"] = func(a *shell.App) { theApp = a }
	oklab := false // ColorSpace::default() = Srgb
	return func() ui.Element {
		space := render.SpaceSRGB
		spaceLabel := "sRGB"
		if oklab {
			space = render.SpaceOklab
			spaceLabel = "Oklab"
		}
		solid := func(c uint32, label string) ui.Element {
			return ui.Div().Grow().Rounded(10).Bg(ui.RGB(c)).JustifyCenter().ItemsCenter().
				Children(ui.Text(label, ui.RGB(0xffffff)).SizePx(13))
		}
		hdr := ui.Div().Row().JustifyCenter().ItemsCenter().Children(
			ui.Div().Grow().Children(ui.Text("Gradient Examples", ui.RGB(0x000000))),
			ui.Div().PadXY(12, 4).Bg(ui.RGB(0x000000)).Cursor(app.CursorPointer).
				OnClick(func() { oklab = !oklab; core.BumpRevision() }).
				Children(ui.Text(spaceLabel, ui.RGB(0xffffff)).SizePx(13)),
		)
		row := func(els ...ui.Element) ui.Element { return ui.Div().Row().Grow().Gap(12).Children(els...) }
		// Hard stop: both stops at 0.5 — a sharp blue|red split (Zed's
		// linear_gradient with coincident stops).
		// Gradient on a custom path: Zed's bottom trapezoid.
		// gpui angles are CSS degrees (0 = to-top, 90 = to-right, clockwise);
		// ours are radians with 0 = to-right — offset by -90°.
		rad := func(cssDeg float64) float32 { return float32((cssDeg - 90) * math.Pi / 180) }
		lin2 := func(c0, c1 uint32, cssDeg float64, s0, s1 float32) ui.Element {
			ramp := render.Ramp{Colors: [4]ui.RGBA{ui.RGB(c0), ui.RGB(c1)},
				Stops: [4]float32{s0, s1}, Count: 2, Space: space}
			return ui.Div().Grow().Rounded(10).GradientRamp(ramp, rad(cssDeg))
		}
		trapW := float32(720)
		if theApp != nil {
			trapW = 0.8 * float32(theApp.W)
		}
		trap := ui.NewCanvas(trapW, 80, func(r *render.Renderer2D, rect ui.Rect) {
			// canvas: 80%-width trapezoid, red→blue top-down (180°).
			p := render.NewPath()
			x, y := rect.X, rect.Y
			p.MoveTo(x, y+rect.H).LineTo(x+80, y+30).
				LineTo(x+rect.W-80, y+30).LineTo(x+rect.W, y+rect.H).Close()
			r.FillPathGradientStops(p, ui.RGB(0xff0000), ui.RGB(0x0000ff), math.Pi/2, 0, 1)
		})
		const red, green, blue, yellow, white = 0xff0000, 0x008000, 0x0000ff, 0xffff00, 0xffffff
		return ui.Div().Col().Grow().P(16).Gap(12).Bg(ui.RGB(0xffffff)).Children(
			hdr,
			row(solid(red, "Solid Color"), solid(blue, "Solid Color")),
			row(lin2(red, blue, 45, 0, 1), lin2(red, green, 135, 0, 1),
				lin2(green, blue, 225, 0, 1), lin2(green, yellow, 315, 0, 1)),
			row(lin2(red, white, 0, 0, 1), lin2(blue, white, 90, 0, 1),
				lin2(green, white, 180, 0, 1), lin2(yellow, white, 360, 0, 1)),
			lin2(green, yellow, 0, 0.05, 0.95),
			lin2(blue, red, 90, 0.05, 0.95),
			row(lin2(blue, red, 90, 0.5, 0.5), lin2(green, blue, 180, 0, 0.5)),
			ui.Div().Row().JustifyCenter().Children(trap),
		)
	}
}

// textZed mirrors text.rs ("GPUI Typography"): a header with a font-family
// cycling button over a scrollable page holding the 20-column character grid
// (62px cells, 24px glyphs) and the type-scale specimen ladder — "The quick
// brown fox…" at each scale, normal then inverted (white on black) — with the
// 240px container-colored sidebar. Zed's mono families map to our registered
// "monospace"; .SystemUIFont is our default face.
func textZed() func() ui.Element {
	families := []string{".ZedMono", ".SystemUIFont", "Menlo", "Monaco", "Courier New"}
	// Distinct installed faces per entry (see shell namedFamilyFiles):
	// cycling visibly changes the font like on macOS.
	famMap := map[string]string{".ZedMono": "monospace", "Menlo": "menlo",
		"Monaco": "monaco", "Courier New": "courier new"}
	famIdx := 0
	var st ui.ScrollState
	var theApp *shell.App
	appInits["text_zed"] = func(a *shell.App) { theApp = a }
	chars := []string{
		"1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "A", "B", "C", "D", "E", "F", "G",
		"H", "I", "J", "K", "L", "M", "N", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y",
		"Z", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "p", "q",
		"r", "s", "t", "u", "v", "w", "x", "y", "z", "ẞ", "ſ", "ß", "ð", "Þ", "þ", "α", "β",
		"Γ", "γ", "Δ", "δ", "η", "θ", "ι", "κ", "Λ", "λ", "μ", "ν", "ξ", "π", "τ", "υ", "φ",
		"χ", "ψ", "∂", "а", "в", "Ж", "ж", "З", "з", "К", "к", "л", "м", "Н", "н", "Р", "р",
		"У", "у", "ф", "ч", "ь", "ы", "Э", "э", "Я", "я", "ij", "öẋ", ".,", "⣝⣑", "~", "*",
		"_", "^", "`", "'", "(", "{", "«", "#", "&", "@", "$", "¢", "%", "|", "?", "¶", "µ",
		"❮", "<=", "!=", "==", "--", "++", "=>", "->", "🏀", "🎊", "😍", "❤️", "👍", "👎",
	}
	return func() ui.Element {
		fam := famMap[families[famIdx]]
		mkText := func(s string, size float64, c ui.RGBA) *ui.TextEl {
			t := ui.Text(s, c).SizePx(size)
			if fam != "" {
				t.FontFamily(fam)
			}
			return t
		}
		btn := ui.Div().PadXY(12, 4).Bg(ui.RGB(0x000000)).Cursor(app.CursorPointer).
			PressBg(ui.RGBA{A: 0.8}).
			OnClick(func() { famIdx = (famIdx + 1) % len(families); core.BumpRevision() }).
			Children(ui.Text(families[famIdx], ui.RGB(0xffffff)).SizePx(14))
		header := ui.Div().Row().P(12).Bg(ui.RGB(0xffffff)).BorderBottom(1, ui.RGBA{A: 1}).Children(btn)

		// Cells flex to the row (Zed's w_full flex rows shrink/spread the
		// 62px cells with the window); incomplete rows pad with fillers so
		// columns stay aligned.
		grid := ui.Div().Col().P(16).Gap(8)
		for start := 0; start < len(chars); start += 20 {
			row := ui.Div().Row()
			batch := chars[start:min(start+20, len(chars))]
			for _, s := range batch {
				// text.rs grid cells set line_height(relative(1.0)).
				row.Children(ui.Div().H(62).Grow().ItemsCenter().JustifyCenter().
					Children(mkText(s, 24, ui.RGB(0x000000)).LineHeight(1)))
			}
			for i := len(batch); i < 20; i++ {
				row.Children(ui.Div().Grow())
			}
			grid.Children(row)
		}

		// Type-scale ladder: 16px base, ratio 1.33; each scale rendered
		// normal then inverted, both padded 10px (Specimen p(10)).
		specimens := ui.Div().Col()
		const base, ratio = 16.0, 1.33
		scales := []float64{1 / (ratio * ratio), 1 / ratio, 1,
			ratio, ratio * ratio, ratio * ratio * ratio,
			ratio * ratio * ratio * ratio, ratio * ratio * ratio * ratio * ratio,
			ratio * ratio * ratio * ratio * ratio * ratio}
		// Specimens wrap at the content width, like Zed's flowing text.
		wrapW := float32(1516)
		if theApp != nil {
			wrapW = float32(theApp.W) - 20
		}
		for _, sc := range scales {
			for _, inv := range []bool{false, true} {
				fg, bg := ui.RGB(0x000000), ui.RGB(0xffffff)
				if inv {
					fg, bg = bg, fg
				}
				// Specimens use text.rs's TextContext line_height 1.3.
				p := ui.Wrapped("The quick brown fox jumps over the lazy dog", fg, wrapW).
					SizePx(base * sc).LineHeight(1.3)
				if fam != "" {
					p.FontFamily(fam)
				}
				specimens.Children(ui.Div().P(10).Bg(bg).Children(p))
			}
		}

		// (text.rs also draws a 240px container-colored sidebar on the right;
		// deliberately omitted here — it only proves the container color.)
		content := ui.Scroll(&st).Grow().Children(grid, specimens)
		return ui.Div().Col().Grow().Bg(ui.RGB(0xffffff)).Children(
			header,
			ui.Div().Row().Grow().Children(content),
		)
	}
}

// textLayoutZed mirrors text_layout.rs: nine full-width lines (left/center/
// right, plain/underlined/line-through), a justify-between row of two blue
// nowrap boxes, and the styled "ABCD" run (A extra-bold, C italic).
func textLayoutZed() func() ui.Element {
	return func() ui.Element {
		black := ui.RGB(0x000000)
		blue := ui.RGB(0x0000ff)
		line := func(s string, a ui.Align, deco func(*ui.TextEl) *ui.TextEl) ui.Element {
			t := ui.Text(s, black).HAlign(a)
			if deco != nil {
				t = deco(t)
			}
			return t
		}
		und := func(t *ui.TextEl) *ui.TextEl { return t.Underline().UnderlineThickness(1) }
		str := func(t *ui.TextEl) *ui.TextEl { return t.StrikeThrough().UnderlineThickness(1) }
		return ui.Div().Col().Grow().P(16).Gap(8).Bg(ui.RGB(0xffffff)).Children(
			line("Text left", ui.AlignStart, nil),
			line("Text center", ui.AlignCenter, nil),
			line("Text right", ui.AlignEnd, nil),
			line("Text left (underline)", ui.AlignStart, und),
			line("Text center (underline)", ui.AlignCenter, und),
			line("Text right (underline)", ui.AlignEnd, und),
			line("Text left (line_through)", ui.AlignStart, str),
			line("Text center (line_through)", ui.AlignCenter, str),
			line("Text right (line_through)", ui.AlignEnd, str),
			ui.Div().Row().Gap(8).Children(
				// overflow_hidden flex items in gpui shrink below their set
				// width when the window narrows (flex-shrink 1, min-width 0).
				ui.Div().W(400).Shrink().Clip().Border(1, blue).P(4).Children(
					ui.Text("A long non-wrapping text align center", black).
						HAlign(ui.AlignCenter).TruncateClip()),
				ui.Div().Grow(), // justify_between
				ui.Div().W(128).Shrink().Clip().Border(1, blue).P(4).Children(
					ui.Text("100%", black).HAlign(ui.AlignEnd).TruncateClip()),
			),
			// StyledText "ABCD": A extra-bold, C italic (per-run highlights).
			ui.Div().Row().Children(
				ui.Text("A", black).Bold(),
				ui.Text("B", black),
				ui.Text("C", black).Italic(),
				ui.Text("D", black),
			),
		)
	}
}

// dataTableZed mirrors data_table.rs: a "Total N items, visible range"
// status line over a bordered virtualized table of 10,000 quote rows and all
// 24 fixed-width columns (clipped at the window edge, like Zed's
// overflow_hidden), striped rows, red/green price columns, and Zed's custom
// fixed-height draggable scrollbar thumb on the right edge.
func dataTableZed() func() ui.Element {
	type field struct {
		key string
		w   float32
	}
	fields := []field{
		{"id", 64}, {"symbol", 64}, {"name", 180}, {"last_done", 80},
		{"prev_close", 80}, {"open", 80}, {"low", 80}, {"high", 80},
		{"ttm", 50}, {"market_cap", 96}, {"float_cap", 96}, {"turnover", 120},
		{"volume", 100}, {"turnover_ratio", 96}, {"pe", 64}, {"pb", 64},
		{"eps", 64}, {"shares", 96}, {"dividend", 64}, {"yield", 64},
		{"dividend_per_share", 64}, {"dividend_date", 96},
		{"dividend_payment", 64}, {"timestamp", 120},
	}
	const totalItems = 10000
	const rowH = 22
	var st ui.ListState
	var listEl *ui.ListEl
	var listBounds ui.Rect // previous frame's list rect (layout runs post-build)
	dragOff := float32(-1) // pointer offset inside the thumb while dragging
	// Zed's scrollbar: fixed 100px thumb, 8px wide, 4px in from the right.
	thumbRect := func() (x, y, w, h float32, ok bool) {
		b := listBounds
		if b.H <= 0 || st.MaxY() <= 0 {
			return 0, 0, 0, 0, false
		}
		fr := st.Offset() / st.MaxY()
		top := b.Y + min(max(b.H*fr*((b.H-108)/b.H), 4), b.H-104)
		return b.X + b.W - 12, top, 8, 100, true
	}
	frameObservers["data_table_zed"] = func(mouse []app.MouseEvent, _ []app.KeyEvent) {
		for _, m := range mouse {
			switch m.Kind {
			case app.MouseDown:
				if tx, ty, tw, th, ok := thumbRect(); ok && m.Button == 1 &&
					m.X >= tx && m.X < tx+tw && m.Y >= ty && m.Y < ty+th {
					dragOff = m.Y - ty
				}
			case app.MouseMove:
				if dragOff >= 0 {
					b := listBounds
					if track := b.H - 108; track > 0 {
						fr := (m.Y - dragOff - b.Y - 4) / track
						st.SetOffset(min(max(fr, 0), 1) * st.MaxY())
						core.BumpRevision()
					}
				}
			case app.MouseUp:
				dragOff = -1
			}
		}
	}
	// Deterministic per-row quote (data_table.rs uses rand; we seed per index
	// so scrolling is stable frame to frame).
	cellText := func(ix int, key string) (string, bool) {
		rng := rand.New(rand.NewSource(int64(ix)*7919 + 17))
		prevClose := 100 + rng.Float64()*100
		change := -5 + rng.Float64()*10
		lastDone := prevClose + change
		open := prevClose - 3 + rng.Float64()*6
		high := max(prevClose+rng.Float64()*10, open)
		low := min(prevClose-rng.Float64()*10, open)
		volume := 1_000_000 + rng.Int63n(99_000_000)
		turnover := lastDone * float64(volume)
		symbol := fmt.Sprintf("%c%c.%c", 'A'+rng.Intn(26), 'A'+rng.Intn(26), 'A'+rng.Intn(26))
		marketCap := 1_000_000 + rng.Float64()*9_000_000
		shares := 100 + rng.Float64()*900
		colored := true
		switch key {
		case "id":
			return fmt.Sprintf("%d", ix), false
		case "symbol":
			return symbol, false
		case "name":
			return fmt.Sprintf("%s %d - #%d", symbol, 1+rng.Intn(99), 10000+rng.Intn(90000)), false
		case "last_done":
			return fmt.Sprintf("%.3f", lastDone), colored
		case "prev_close":
			return fmt.Sprintf("%.3f", prevClose), colored
		case "change":
			return fmt.Sprintf("%.2f%%", change/prevClose*100), colored
		case "timestamp":
			return fmt.Sprintf("%d", rng.Intn(86400)), colored
		case "open":
			return fmt.Sprintf("%.2f", open), colored
		case "low":
			return fmt.Sprintf("%.2f", low), colored
		case "high":
			return fmt.Sprintf("%.2f", high), colored
		case "ttm":
			return fmt.Sprintf("%.2f", rng.Float64()*10), colored
		case "eps":
			return fmt.Sprintf("%.2f", marketCap/shares), colored
		case "market_cap":
			return fmt.Sprintf("%.2f M", marketCap/1e6), false
		case "float_cap":
			return fmt.Sprintf("%.2f M", (marketCap+1000+rng.Float64()*9000)/1e6), false
		case "turnover":
			return fmt.Sprintf("%.2f M", turnover/1e6), false
		case "volume":
			return fmt.Sprintf("%.2f M", float64(volume)/1e6), false
		case "turnover_ratio":
			return fmt.Sprintf("%.2f%%", float64(volume)/turnover*100), false
		case "pe", "pb":
			return fmt.Sprintf("%.2f", marketCap/shares), false
		case "shares":
			return fmt.Sprintf("%.2f", shares), false
		case "dividend", "dividend_per_share", "dividend_payment":
			return fmt.Sprintf("%.2f", rng.Float64()*10), false
		case "yield":
			return fmt.Sprintf("%.2f%%", rng.Float64()*10), false
		case "dividend_date":
			return fmt.Sprintf("%d-%d-%d", 2000+rng.Intn(23), 1+rng.Intn(11), 1+rng.Intn(27)), false
		}
		return "--", false
	}
	rowChangeUp := func(ix int) bool {
		rng := rand.New(rand.NewSource(int64(ix)*7919 + 17))
		rng.Float64()
		return -5+rng.Float64()*10 > 0
	}
	return func() ui.Element {
		grey := ui.RGB(0xe0e0e0)
		buildRow := func(ix int) ui.Element {
			bg := ui.RGB(0xffffff)
			if ix%2 == 1 {
				bg = ui.RGB(0xfafafa)
			}
			color := ui.RGB(0xff0000)
			if rowChangeUp(ix) {
				color = ui.RGB(0x008000)
			}
			row := ui.Div().Row().H(rowH).PadXY(8, 2).ItemsCenter().Bg(bg).BorderBottom(1, grey)
			for _, f := range fields {
				s, colored := cellText(ix, f.key)
				c := ui.RGB(0x000000)
				if colored {
					c = color
				}
				row.Children(ui.Div().W(f.w).PadXY(4, 0).Children(
					ui.Text(s, c).SizePx(14).LineHeight(1).TruncateClip()))
			}
			return row
		}
		header := ui.Div().Row().PadXY(8, 4).ItemsCenter().Bg(ui.RGB(0xf0f0f0)).BorderBottom(1, grey).Clip()
		for _, f := range fields {
			header.Children(ui.Div().W(f.w).PadXY(4, 0).Children(
				ui.Text(f.key, ui.RGB(0x555555)).SizePx(14).TruncateClip()))
		}
		list := ui.List(&st, rowH, totalItems, buildRow).Grow().BarColor(ui.RGBA{})
		table := ui.Div().Col().Grow().P(1).Border(1, grey).Rounded(2).Children(header, list)
		// The scrollbar overlay positions from the PREVIOUS frame's list
		// bounds (the scroll area under the header) — the just-built list
		// hasn't been laid out yet.
		if listEl != nil {
			listBounds = listEl.Bounds()
		}
		listEl = list
		first := int(st.Offset() / rowH)
		last := min(first+int(st.ViewH()/rowH)+1, totalItems)
		page := ui.Div().Col().Grow().P(16).Gap(8).Bg(ui.RGB(0xffffff)).Children(
			ui.Text(fmt.Sprintf("Total %d items, visible range: %d..%d", totalItems, first, last),
				ui.RGB(0x000000)).SizePx(14),
			table,
		)
		if tx, ty, tw, th, ok := thumbRect(); ok {
			page.Children(ui.Overlay(
				ui.Div().Size(tw, th).Rounded(8).Bg(ui.RGB(0xc0c0c0)).HoverBg(ui.RGB(0xa0a0a0)),
				tx, ty))
		}
		return page
	}
}

// treeZed mirrors tree.rs: concentric padded boxes cycling red/blue/green/yellow.
func treeZed() func() ui.Element {
	return func() ui.Element {
		// tree.rs: 50 nested divs, each p_0p5 (2px), colors cycling
		// red/blue/green/yellow from the innermost out; the root is
		// content-sized (~204px) at the window's top-left on white.
		cols := []uint32{0xff0000, 0x0000ff, 0x008000, 0xffff00}
		var node ui.Element = ui.Div().P(2).Bg(ui.RGB(cols[0]))
		for d := 1; d <= 50; d++ {
			node = ui.Div().P(2).Bg(ui.RGB(cols[d%4])).Children(node)
		}
		// The nest stretches to the window width (gpui's root fills the
		// window; height stays content-sized).
		return ui.Div().Grow().Bg(ui.RGB(0xffffff)).Children(node)
	}
}

// patternZed mirrors pattern.rs: "Pattern Example" over boxes filled with a
// diagonal red slash pattern (drawn on a canvas — we have no native pattern fill).
func patternZed() func() ui.Element {
	// pattern_slash(color, thickness, interval): 45° stripes `thick` wide
	// separated by `interval` (both measured perpendicular). Full-length
	// lines clipped to the box — trimming centerlines instead would drop the
	// stripes whose centers fall outside the corners.
	slash := func(w, h, thick, interval float32, c ui.RGBA) ui.Element {
		return ui.NewCanvas(w, h, func(r *render.Renderer2D, rect ui.Rect) {
			// gpui's pattern shader: perpendicular stripe width is
			// width*sin45° and the horizontal repeat is exactly
			// width+interval. Lines overshoot the box so their end caps
			// fall outside the clip instead of cutting stripes at edges.
			r.PushClip(rect.X, rect.Y, rect.W, rect.H)
			strokeW := thick * float32(math.Sqrt2) / 2
			e := thick
			for x := -h - thick - interval; x < w+thick+interval; x += thick + interval {
				p := render.NewPath()
				p.MoveTo(rect.X+x-e, rect.Y+h+e)
				p.LineTo(rect.X+x+h+e, rect.Y-e)
				r.StrokePath(p, strokeW, c)
			}
			r.PopClip()
		})
	}
	red := ui.RGBA{R: 1, A: 1}
	green := ui.RGBA{G: 0.5, A: 1}
	blue := ui.RGBA{B: 1, A: 1}
	yellow := ui.RGBA{R: 1, G: 1, A: 1}
	return func() ui.Element {
		box1 := ui.Div().Col().W(56).P(1).Border(1, ui.RGB(0x0000ff)).Children(
			slash(54, 18, 4.5, 4.5, red), slash(54, 18, 4.5, 4.5, red),
			slash(54, 18, 4.5, 4.5, red), slash(54, 18, 4.5, 9, red))
		// box2 sizes to its caption width; slashes are 256 so the green tint
		// shows to their right. Last row is shorter (h_26), like pattern.rs.
		box2 := ui.Div().Col().P(1).Border(1, ui.RGB(0x0000ff)).Bg(ui.RGBA{G: 0.5, A: 0.16}).Children(
			ui.Text("Elements the same height should align", ui.RGB(0x000000)).SizePx(20),
			slash(256, 56, 56.0/6, 56.0/6, red), slash(256, 56, 56.0/6, 56.0/6, green),
			slash(256, 56, 56.0/6, 56.0/6, blue), slash(256, 26, 56.0/6, 56.0/6, yellow))
		bar := func(fill func(*ui.DivEl) *ui.DivEl) ui.Element {
			d := ui.Div().Size(240, 40).Border(1, ui.RGB(0x0000ff))
			return fill(d)
		}
		solidBar := bar(func(d *ui.DivEl) *ui.DivEl { return d.Bg(red) })
		// CSS 45° = to top-right = our -45°.
		gradBar := bar(func(d *ui.DivEl) *ui.DivEl { return d.Gradient(red, blue, -0.785) })
		// pattern.rs: a fixed 600x600 white box with the shadow — it does
		// NOT track the window size.
		return ui.Div().Children(
			ui.Div().Col().Size(600, 600).Gap(12).JustifyCenter().ItemsCenter().Bg(ui.RGB(0xffffff)).
				Shadow(16, 0, 8, ui.RGBA{A: 0.4}).Children(
				ui.Text("Pattern Example", ui.RGB(0x000000)).SizePx(20),
				box1, box2, solidBar, gradBar,
			))
	}
}

// a11yZed mirrors a11y.rs resting visual: dark page, bold heading, a counter
// row and action buttons. (ARIA roles aren't modelled; bold approximated.)
// a11yZed mirrors a11y.rs interactively: the Count pill increments on click,
// Reset zeroes it, the "Enable feature" switch toggles (0x585b70 off /
// 0x89b4fa on, knob sliding), and everything is a Tab stop. (ARIA roles and
// screen-reader actions have no visual — not modelled.)
func a11yZed() func() ui.Element {
	count := 0
	enabled := false
	return func() ui.Element {
		pill := func(s string, bg, fg uint32, onClick func()) ui.Element {
			return ui.Div().PadXY(12, 4).Bg(ui.RGB(bg)).Rounded(6).Focusable().
				Cursor(app.CursorPointer).OnClick(onClick).
				Children(ui.Text(s, ui.RGB(fg)).SizePx(14))
		}
		knobPos := float32(3)
		trackBg := uint32(0x585b70)
		if enabled {
			knobPos = 23
			trackBg = 0x89b4fa
		}
		toggle := ui.Div().Row().ItemsCenter().Size(44, 24).Rounded(12).
			Bg(ui.RGB(trackBg)).Focusable().Cursor(app.CursorPointer).
			OnClick(func() { enabled = !enabled; core.BumpRevision() }).Children(
			ui.Div().W(knobPos),
			ui.Div().Size(18, 18).Rounded(9).Bg(ui.RGB(0xffffff)))
		item := func(s string) ui.Element {
			return ui.Text(s, ui.RGB(0xcdd6f4)).SizePx(15)
		}
		return ui.Div().Col().Grow().Gap(16).P(16).Bg(ui.RGB(0x1e1e2e)).Children(
			ui.Text("Accessibility Demo", ui.RGB(0xcdd6f4)).SizePx(20).Bold(),
			ui.Div().Row().Gap(12).ItemsCenter().Children(
				pill(fmt.Sprintf("Count: %d", count), 0x89b4fa, 0x1e1e2e,
					func() { count++; core.BumpRevision() }),
				pill("Reset", 0x45475a, 0xcdd6f4,
					func() { count = 0; core.BumpRevision() })),
			ui.Div().Row().Gap(12).ItemsCenter().Children(
				toggle, ui.Text("Enable feature", ui.RGB(0xcdd6f4)).SizePx(15)),
			ui.Div().Col().Gap(10).Children(
				item("1. Write code"), item("2. Run tests"), item("3. Ship it")),
		)
	}
}

// windowZed mirrors window.rs default state: a white window with a wrapped row
// of window-option buttons (no custom titlebar until toggled).
// windowZed mirrors window.rs with REAL child windows (App.OpenChild): the
// kind buttons open 250x200 SubWindows (window kinds all map to plain X11
// children — Popup/Floating/Dialog hints are not modelled), each SubWindow
// can open nested dialogs and close itself, "Prompt" opens an OK/Cancel
// dialog, and "Resize" grows/shrinks the main window like Zed's.
func windowZed() func() ui.Element {
	var theApp *shell.App
	appInits["window_zed"] = func(a *shell.App) { theApp = a }
	grew := false
	btnEl := func(s string, onClick func()) ui.Element {
		b := ui.Div().PadXY(10, 4).Rounded(4).Border(1, ui.RGBA{A: 0.25}).
			Cursor(app.CursorPointer).HoverBg(ui.RGBA{A: 0.06}).
			Children(ui.Text(s, ui.RGB(0x111111)).SizePx(13))
		if onClick != nil {
			b.OnClick(onClick)
		}
		return b
	}
	// dialogModals renders `*count` stacked modal dialogs (Zed's Dialog kind
	// and nested dialogs are modal within their parent window). Each level's
	// buttons push/pop the stack; lower levels are blocked by upper scrims.
	dialogModals := func(count *int) []ui.Element {
		var ms []ui.Element
		for i := 0; i < *count; i++ {
			panel := ui.Div().Col().P(16).Gap(8).Rounded(8).Bg(ui.RGB(0xffffff)).
				ShadowSpread(15, 0, 10, -3, ui.RGBA{A: 0.2}).Children(
				ui.Text(fmt.Sprintf("Dialog %d", i+1), ui.RGB(0x000000)),
				btnEl("Open Nested Dialog", func() { *count++; core.BumpRevision() }),
				btnEl("Close", func() { *count--; core.BumpRevision() }),
			)
			ms = append(ms, ui.Modal(panel, ui.RGBA{A: 0.35}, nil))
		}
		return ms
	}
	mainDialogs := 0
	var openSub func(title string, customBar bool)
	var openSubOpts func(title string, customBar bool, mod func(*shell.Options))
	// A custom titlebar implies hiding the OS one (gpui's hide_title_bar
	// when the window supplies its own): otherwise both bars stack.
	openSub = func(title string, customBar bool) {
		openSubOpts(title, customBar, func(o *shell.Options) { o.Undecorated = customBar })
	}
	openSubOpts = func(title string, customBar bool, mod func(*shell.Options)) {
		if theApp == nil {
			return
		}
		var handle *shell.ChildWindow
		subDialogs := 0
		view := func() ui.Element {
			root := ui.Div().Col().Grow().Bg(ui.RGB(0xffffff))
			if customBar {
				root.Children(ui.Div().Row().H(32).PadXY(8, 0).ItemsCenter().
					Bg(ui.RGB(0x3b5bdb)).Children(
					ui.Text("Custom Titlebar", ui.RGB(0xffffff)).SizePx(13)))
			}
			root.Children(ui.Div().Col().Grow().P(12).Gap(8).Children(
				ui.Text("SubWindow", ui.RGB(0x000000)),
				btnEl("Open Nested Dialog", func() { subDialogs++; core.BumpRevision() }),
				btnEl("Close", func() {
					if handle != nil {
						handle.Close()
					}
				}),
			))
			root.Children(dialogModals(&subDialogs)...)
			return root
		}
		o := shell.Options{Title: title, Width: 250, Height: 200}
		if mod != nil {
			mod(&o)
		}
		h, err := theApp.OpenChild(o, view)
		if err != nil {
			fmt.Fprintln(os.Stderr, "window_zed:", err)
			return
		}
		if customBar {
			// The custom titlebar (top 32px) drag-moves the window, like
			// gpui windows with their own chrome.
			h.SetDragRegion(func(x, y float32) bool { return y < 32 })
		}
		handle = h
	}
	promptOpen := false
	promptButtons := []string{"OK", "Cancel"}
	openPrompt := func(buttons []string) func() {
		return func() { promptOpen = true; promptButtons = buttons; core.BumpRevision() }
	}
	closePrompt := func(label string) func() {
		return func() {
			promptOpen = false
			fmt.Printf("You have clicked %s\n", label)
			core.BumpRevision()
		}
	}
	return func() ui.Element {
		sub := func(label string, customBar bool) ui.Element {
			return btnEl(label, func() { openSub(label, customBar) })
		}
		row1 := ui.Div().Row().Gap(8).Children(
			sub("Normal", false),
			// Popup/PopUp windows are undecorated and unmovable in Zed.
			btnEl("Popup", func() {
				openSubOpts("Popup", false, func(o *shell.Options) { o.Undecorated, o.Unmovable = true, true })
			}),
			sub("Floating", false),
			btnEl("Dialog", func() { mainDialogs++; core.BumpRevision() }),
			sub("Custom Titlebar", true), sub("Invisible", false),
			btnEl("Unmovable", func() {
				openSubOpts("Unmovable", false, func(o *shell.Options) { o.Unmovable = true })
			}),
			btnEl("Unresizable", func() {
				openSubOpts("Unresizable", false, func(o *shell.Options) { o.Unresizable = true })
			}),
		)
		row2 := ui.Div().Row().Gap(8).Children(
			btnEl("Unminimizable", func() {
				openSubOpts("Unminimizable", false, func(o *shell.Options) { o.Unminimizable = true })
			}),
			btnEl("Hide Application", nil), // no per-app hide on X11
			btnEl("Resize", func() {
				if theApp == nil {
					return
				}
				x, y, w, h := theApp.Win.Geometry()
				d := 100
				if grew {
					d = -100
				}
				grew = !grew
				theApp.Win.SetGeometry(x, y, w+d, h+d)
			}),
			btnEl("Prompt", openPrompt([]string{"OK", "Cancel"})),
			btnEl("Prompt (non-English)", openPrompt([]string{"确定", "取消"})),
		)
		// Zed centres the rows vertically but left-aligns them horizontally.
		root := ui.Div().Col().Grow().JustifyCenter().P(16).Gap(8).Bg(ui.RGB(0xffffff)).Children(
			row1, row2,
		)
		root.Children(dialogModals(&mainDialogs)...)
		if promptOpen {
			// Prompts are modal: centered over the parent with a dim scrim;
			// gpui's prompt renderer stacks the buttons one per row.
			buttons := ui.Div().Col().Gap(8)
			for _, b := range promptButtons {
				buttons.Children(ui.Div().W(200).JustifyCenter().PadXY(10, 4).Rounded(4).
					Border(1, ui.RGBA{A: 0.25}).Cursor(app.CursorPointer).
					HoverBg(ui.RGBA{A: 0.06}).OnClick(closePrompt(b)).
					Children(ui.Text(b, ui.RGB(0x111111)).SizePx(13)))
			}
			panel := ui.Div().Col().P(16).Gap(12).ItemsCenter().Rounded(8).Bg(ui.RGB(0xffffff)).
				ShadowSpread(15, 0, 10, -3, ui.RGBA{A: 0.2}).Children(
				ui.Text("Are you sure?", ui.RGB(0x000000)),
				buttons,
			)
			// nil dismiss: prompts require an explicit button — clicking the
			// scrim does nothing (pass closePrompt instead for click-away).
			root.Children(ui.Modal(panel, ui.RGBA{A: 0.35}, nil))
		}
		return root
	}
}

// windowShadowZed mirrors window_shadow.rs: a rounded client window with a drop
// shadow over a transparent backdrop (we render the backdrop light grey).
// windowShadowZed mirrors window_shadow.rs with REAL client-side decorations:
// a transparent undecorated window (X11 ARGB visual), a 10px shadow inset
// around the rounded lavender client (grey 1px border, black@0.4 blur-5
// shadow — the desktop shows through around it), draggable anywhere and
// resizable from the shadow band, exactly Zed's CSD behavior.
func windowShadowZed() func() ui.Element {
	exampleOpts["window_shadow_zed"] = func(o *shell.Options) {
		o.Transparent = true
		o.Undecorated = true
		o.ResizeBand = 10 // Zed's resize_edge zone = the shadow inset
	}
	// Anywhere inside the client that isn't an interactive child moves the
	// window (window_shadow.rs starts a move on any non-edge mouse down).
	dragRegions["window_shadow_zed"] = func(_ *shell.App, _, _ float32) bool { return true }
	return func() ui.Element {
		inner := ui.Div().Size(300, 300).JustifyCenter().ItemsCenter().Bg(ui.RGB(0xffffff)).
			Border(1, ui.RGB(0x0000ff)).
			ShadowSpread(15, 0, 10, -3, ui.RGBA{A: 0.1}). // shadow_lg
			ShadowSpread(6, 0, 4, -4, ui.RGBA{A: 0.1}).
			Children(
				ui.Div().Size(200, 100).JustifyCenter().ItemsCenter().Bg(ui.RGB(0x008000)).
					Shadow(20, 0, 0, ui.RGBA{A: 1}). // black glow, blur 20
					Children(ui.Text("this is the custom titlebar", ui.RGB(0x000000)).SizePx(13)),
			)
		client := ui.Div().Grow().JustifyCenter().ItemsCenter().Rounded(10).
			Bg(ui.RGB(0xccccff)).Border(1, ui.RGB(0x808080)).
			Shadow(5, 0, 0, ui.RGBA{A: 0.4}). // Zed: blur shadow_size/2, no offset
			Children(inner)
		// The 10px transparent padding is the shadow/resize inset; the
		// desktop is visible through it.
		return ui.Div().Col().Grow().P(10).Children(client)
	}
}

// activeStateBugZed mirrors active_state_bug.rs: a small "Click me" button on a
// dark page (hsla(215,0.12,0.15) bg, hsla(221,0.11,0.86) text).
func activeStateBugZed() func() ui.Element {
	return func() ui.Element {
		// hover hsla(225,11.8%,26.7%) ≈ 0x3c404c; active hsla(220,11.8%,20%)
		// ≈ 0x2d3139 — hold the mouse down to see the darker active state.
		return ui.Div().Grow().P(4).Bg(ui.RGB(0x22252b)).Children(
			ui.Div().PadXY(8, 2).Rounded(6).
				HoverBg(ui.RGB(0x3c404c)).PressBg(ui.RGB(0x2d3139)).
				OnClick(func() {}).
				Children(ui.Text("Click me", ui.RGB(0xd7dadf)).SizePx(14)),
		)
	}
}

// gifViewerZed mirrors gif_viewer.rs: a single decoded image frame. image.Decode
// (image/gif registered) yields the GIF's first frame; live frame animation is
// a runtime concern (a frame driver), so the screenshot shows one frame.
func gifViewerZed() func() ui.Element {
	cell := &ui.ImageCell{Data: readAsset("build/images/cat.gif")}
	return func() ui.Element {
		// img().size_full().object_fit(Contain): the GIF aspect-fits the
		// window on the default white page; playback runs off frame delays.
		return ui.Div().Grow().Bg(ui.RGB(0xffffff)).Children(
			ui.LazyImage(cell, 400, 400).Grow(),
		)
	}
}

// pathsBenchZed mirrors paths_bench.rs: a filled star path (Zed animates it
// pathsBenchZed mirrors paths_bench.rs: the exact 10-point star path from
// the source, filled with the yellow→orange gradient (stops 0.7/1.0; sRGB
// approximation of gpui's Oklab), painted 2000 times EVERY frame — a
// tessellation/fill-rate benchmark. A no-op frame observer keeps the loop
// rendering continuously, like Zed's request_animation_frame.
func pathsBenchZed() func() ui.Element {
	frameObservers["paths_bench_zed"] = func([]app.MouseEvent, []app.KeyEvent) {}
	// paths_bench.rs paints no page background — the window keeps gpui's
	// transparent-black clear (opaque drivers render it black).
	exampleOpts["paths_bench_zed"] = func(o *shell.Options) { o.Transparent = true }
	star := render.NewPath()
	pts := [][2]float32{{350, 100}, {370, 160}, {430, 160}, {380, 200}, {400, 260},
		{350, 220}, {300, 260}, {320, 200}, {270, 160}, {330, 160}}
	for i, q := range pts {
		if i == 0 {
			star.MoveTo(q[0], q[1])
		} else {
			star.LineTo(q[0], q[1])
		}
	}
	star.Close()
	return func() ui.Element {
		canvas := ui.NewCanvas(1024, 768, func(r *render.Renderer2D, rect ui.Rect) {
			for i := 0; i < 2000; i++ {
				r.FillPathGradientStops(star, ui.RGB(0xFACC15), ui.RGB(0xD56D0C), math.Pi/2, 0.7, 1)
			}
		})
		return ui.Div().Grow().Children(canvas)
	}
}

// imageGalleryZed mirrors image_gallery.rs with REAL network images: two
// grey galleries (gpui's manual and LRU image-cache sections) of 30
// picsum.photos thumbnails fetched asynchronously — placeholders paint until
// each response lands (handed to the UI thread via the frame observer) —
// plus the rendered-count line and the "Next Photos" button that fetches a
// fresh cache-busted batch.
func imageGalleryZed() func() ui.Element {
	const nImages = 30
	var mu sync.Mutex
	arrived := map[*ui.ImageCell][]byte{}
	cells := make([]*ui.ImageCell, nImages)
	totalCount := 0
	batchKey := 0
	var theApp *shell.App
	appInits["image_gallery_zed"] = func(a *shell.App) { theApp = a }
	fetch := func() {
		batchKey++
		totalCount += nImages
		// Drop the old batch like gpui's cache.clear(): release its GPU
		// textures and forget any responses still in flight for it.
		mu.Lock()
		clear(arrived)
		mu.Unlock()
		for i := range cells {
			if old := cells[i]; old != nil {
				old.Release()
			}
			c := &ui.ImageCell{}
			cells[i] = c
			url := fmt.Sprintf("https://picsum.photos/400/200?t=%d-%d", batchKey, i)
			go func(c *ui.ImageCell, url string) {
				resp, err := http.Get(url)
				if err != nil {
					return
				}
				b, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					return
				}
				mu.Lock()
				arrived[c] = b
				mu.Unlock()
			}(c, url)
		}
	}
	// Responses land on the UI thread here; SetData re-decodes the cell.
	frameObservers["image_gallery_zed"] = func([]app.MouseEvent, []app.KeyEvent) {
		mu.Lock()
		for c, b := range arrived {
			c.SetData(b)
			delete(arrived, c)
			core.BumpRevision()
		}
		mu.Unlock()
	}
	var first sync.Once
	var st1, st2 ui.ScrollState
	return func() ui.Element {
		first.Do(fetch)
		winW := float32(600)
		if theApp != nil {
			winW = float32(theApp.W)
		}
		gallery := func() ui.Element {
			// flex_wrap gap_x_4 gap_y_2 of size_20 (80px) thumbnails,
			// hand-chunked (our flex has no wrap).
			perRow := max(int((winW-32+16)/(80+16)), 1)
			g := ui.Div().Col().Gap(8).ItemsCenter()
			for i := 0; i < nImages; i += perRow {
				row := ui.Div().Row().Gap(16)
				for _, c := range cells[i:min(i+perRow, nImages)] {
					row.Children(ui.LazyImage(c, 80, 80))
				}
				g.Children(row)
			}
			return g
		}
		status := ui.Div().Row().Children(
			ui.Text(fmt.Sprintf("Example to show images and test memory usage (Rendered: %d images).", totalCount),
				ui.RGB(0x000000)).SizePx(14),
			ui.Div().Grow(),
			ui.Div().W(160).PadXY(16, 4).Bg(ui.RGB(0x000000)).Cursor(app.CursorPointer).
				PressBg(ui.RGBA{A: 0.8}).JustifyCenter().ItemsCenter().
				OnClick(func() { fetch(); core.BumpRevision() }).
				Children(ui.Text("Next Photos", ui.RGB(0xffffff)).SizePx(14)),
		)
		section := func(st *ui.ScrollState, top ...ui.Element) ui.Element {
			s := ui.Scroll(st).Grow().BarColor(ui.RGBA{A: 0.3})
			box := ui.Div().Col().Grow().P(16).Gap(8).Bg(ui.RGB(0xe9e9e9))
			box.Children(append(top, gallery())...)
			s.Children(box)
			return s
		}
		return ui.Div().Col().Grow().Gap(4).Children(
			ui.Text("Manually managed image cache:", ui.RGB(0xffffff)),
			section(&st1, status),
			ui.Text("Automatically managed image cache:", ui.RGB(0xffffff)),
			section(&st2),
		)
	}
}
