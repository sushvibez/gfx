//go:build linux || windows

// Qml runs a .gml document with hot reload: edit app.gml while the app is
// open and watch it update on save. Everything in the file — properties,
// bindings, handlers, animation — lives in markup; this main only mounts it.
//
//	go run ./qml                          # interactive, edit app.gml live
//	go run ./qml -screenshot out.png      # headless render
//	go run ./qml -screenshot out.png -script @steps.txt   # scripted pass
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/script/gml"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	file := flag.String("file", "", "gml file to run (default: app.gml next to this example)")
	script := flag.String("script", "", "interaction script (text, or @file) — capture a PNG per step")
	width := flag.Int("w", 560, "window width")
	height := flag.Int("h", 560, "window height")
	flag.Parse()

	path := *file
	if path == "" {
		for _, p := range []string{"qml/app.gml", "app.gml", "examples/qml/app.gml"} {
			if abs, err := filepath.Abs(p); err == nil {
				if _, err := os.Stat(abs); err == nil {
					path = abs
					break
				}
			}
		}
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "qml: cannot find app.gml (use -file)")
		os.Exit(1)
	}

	registerNativeTypes()
	env := gml.NewEnv()
	live, err := gml.Watch(path, env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "qml:", err)
		os.Exit(1)
	}

	// The corpus' pages are written for a Dashboard that sets `fullSize`
	// when a page becomes the visible one; several gate their animations and
	// particle systems on it. Running a page on its own, we are that host.
	live.SetProperty("fullSize", true)
	// The corpus' PDF page builds file paths from appDirPath, a context
	// property its C++ main exposes. Point it at the document's own tree so
	// the library entries resolve to the PDFs shipped beside it.
	live.SetProperty("appDirPath", filepath.Dir(path))

	opts := shell.Options{Title: "graphics: gml (edit app.gml live)", Width: *width, Height: *height, MaxFrames: *frames}
	// A document with a View3D needs the 3D pass: the engine renders each
	// declared scene into its offscreen target before the 2D pass samples it.
	// Installed only when the page has one, since the hook also turns off the
	// 2D MSAA path.
	hooks := shell.Hooks{Init: func(a *shell.App) (func() ui.Element, error) {
		if live.DeclaresType("View3D") {
			gml.SetView3DGPU(a.Device, a.Queue, a.R2, a.Format)
		}
		return live.View(), nil
	}}
	if live.DeclaresType("View3D") {
		hooks.Encode3D = func(_ *shell.App, enc *gpu.CommandEncoder, _, _ *gpu.TextureView) error {
			return gml.EncodeView3D(enc)
		}
	}
	if *shot != "" && *script != "" {
		live.View()() // warm: Component.onCompleted before the first capture
		err = shell.RunScript(opts, hooks, *script, *shot)
	} else if *shot != "" {
		// Warm one frame so Component.onCompleted (which fires after the first
		// build) is reflected in the captured frame; state is shared via the doc.
		live.View()() // warm: run Component.onCompleted before the captured frame
		err = shell.Screenshot(opts, hooks, *shot)
	} else {
		err = shell.Run(opts, hooks)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "qml:", err)
		os.Exit(1)
	}
}
