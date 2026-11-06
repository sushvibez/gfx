//go:build linux || windows || js

// Triangle draws one triangle through the full no-cgo stack — the phase-0
// exit example. The same code runs natively (app: X11/Win32, gpu:
// wgpu-native), in the browser (app: canvas, gpu: navigator.gpu), and on
// Android (NativeActivity + wgpu-native via the NDK); only the per-platform
// plat/ glue and the entry point differ.
//
// Native: scripts/fetch-wgpu.sh once, then `go run ./triangle`.
// Web: scripts/build-web.sh. Android: scripts/build-apk.sh triangle.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	flag.Parse()
	if err := run(*frames); err != nil {
		fmt.Fprintln(os.Stderr, "triangle:", err)
		os.Exit(1)
	}
}
