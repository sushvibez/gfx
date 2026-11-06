//go:build !js

// Probe acquires a WebGPU adapter and device headlessly and prints what it
// found — the smallest possible proof that the no-cgo GPU path works on this
// machine.
//
// Run scripts/fetch-wgpu.sh first, then:
//
//	go run ./probe
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sushvibez/gfx/gpu"
)

func main() {
	paths := gpu.DefaultLibNames()
	if p := os.Getenv("GFX_WGPU_LIB"); p != "" {
		paths = []string{p}
	} else {
		// Prefer the workspace-local copy fetched by scripts/fetch-wgpu.sh,
		// searching upward from the working directory.
		for _, up := range []string{".", "..", "../.."} {
			abs, err := filepath.Abs(filepath.Join(up, "build", "wgpu", paths[0]))
			if err == nil {
				if _, err := os.Stat(abs); err == nil {
					paths = append([]string{abs}, paths...)
					break
				}
			}
		}
	}

	info, err := gpu.Probe(paths...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "probe failed:", err)
		os.Exit(1)
	}
	fmt.Println(info)
}
