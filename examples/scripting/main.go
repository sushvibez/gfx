//go:build linux || windows

// Scripting showcases the JS-subset engine — function declarations with
// var/for/if/return and Math builtins, driving live bindings — with hot
// reload: edit app.gml while the app is open and watch it update on save.
//
//	go run ./scripting                       # interactive, edit app.gml live
//	go run ./scripting -screenshot out.png   # headless render
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sushvibez/gfx/script/gml"
	"github.com/sushvibez/gfx/ui/shell"
)

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	file := flag.String("file", "", "gml file to run (default: app.gml next to this example)")
	flag.Parse()

	path := *file
	if path == "" {
		for _, p := range []string{"scripting/app.gml", "app.gml", "examples/scripting/app.gml"} {
			if abs, err := filepath.Abs(p); err == nil {
				if _, err := os.Stat(abs); err == nil {
					path = abs
					break
				}
			}
		}
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "scripting: cannot find app.gml (use -file)")
		os.Exit(1)
	}

	env := gml.NewEnv()
	live, err := gml.Watch(path, env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scripting:", err)
		os.Exit(1)
	}

	opts := shell.Options{Title: "graphics: gml scripting (edit app.gml live)", Width: 460, Height: 360, MaxFrames: *frames}
	if *shot != "" {
		err = shell.ScreenshotUI(opts, live.View(), *shot)
	} else {
		err = shell.RunUI(opts, live.View())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "scripting:", err)
		os.Exit(1)
	}
}
