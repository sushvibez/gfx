//go:build linux || windows || js

// Sink2d is the 2D kitchen sink in two styles for side-by-side comparison:
// the Go builder API (default) and the QML-style .gml document (-gml). The
// same widgets, bindings and layout, two authoring models.
//
//	go run ./sink2d                      # Go builder
//	go run ./sink2d -gml                 # QML / .gml (hot-reloads on save)
//	go run ./sink2d -screenshot a.png
//	go run ./sink2d -gml -screenshot b.png
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sushvibez/gfx/script/gml"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

const W, H = 540, 560

func gmlView() (func() ui.Element, error) {
	var path string
	for _, p := range []string{"sink2d/sink.gml", "sink.gml", "examples/sink2d/sink.gml"} {
		if abs, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				path = abs
				break
			}
		}
	}
	if path == "" {
		return nil, fmt.Errorf("sink.gml not found")
	}
	live, err := gml.Watch(path, gml.NewEnv())
	if err != nil {
		return nil, err
	}
	return live.View(), nil
}

func main() {
	ui.SetTheme(ui.DarkTheme()) // a dark app: its controls drawn dark
	gmlMode := flag.Bool("gml", false, "render the QML-style .gml version")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	frames := flag.Int("frames", 0, "exit after N frames")
	flag.Parse()

	title := "graphics: sink2d (Go builder)"
	view := newState().view
	if *gmlMode {
		v, err := gmlView()
		if err != nil {
			fmt.Fprintln(os.Stderr, "sink2d:", err)
			os.Exit(1)
		}
		view, title = v, "graphics: sink2d (.gml)"
	}

	opts := shell.Options{Title: title, Width: W, Height: H, MaxFrames: *frames}
	var err error
	if *shot != "" {
		err = shell.ScreenshotUI(opts, view, *shot)
	} else {
		err = shell.RunUI(opts, view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sink2d:", err)
		os.Exit(1)
	}
}
