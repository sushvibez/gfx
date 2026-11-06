//go:build linux || windows || js

// Triangle draws one triangle through the full no-cgo stack — the phase-0
// exit example. The same code runs natively (app: X11/Win32, gpu:
// wgpu-native), in the browser (app: canvas, gpu: navigator.gpu), and on
// Android (NativeActivity + wgpu-native via the NDK); only the per-platform
// plat/ glue and the entry point differ.
//
// Native: scripts/fetch-wgpu.sh once, then `go run ./triangle`
// (`-screenshot out.png` renders headlessly).
// Web: scripts/build-web.sh. Android: scripts/build-apk.sh triangle.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sushvibez/gfx/examples/internal/shot"
	"github.com/sushvibez/gfx/gpu"
)

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	screenshot := flag.String("screenshot", "", "render one frame headlessly and write a PNG")
	flag.Parse()
	err := error(nil)
	if *screenshot != "" {
		err = shot.Render(800, 600, 1, *screenshot, func(d *gpu.Device, q *gpu.Queue, f gpu.TextureFormat) (func(*gpu.TextureView), error) {
			p, err := newPipeline(d, f)
			if err != nil {
				return nil, err
			}
			return func(v *gpu.TextureView) { draw(d, q, p, v) }, nil
		})
	} else {
		err = run(*frames)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "triangle:", err)
		os.Exit(1)
	}
}
