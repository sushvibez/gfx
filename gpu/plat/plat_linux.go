//go:build !android

// GOOS=android implies the linux tag; exclude this X11 glue from Android.

package plat

import (
	"os"
	"path/filepath"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
)

func NewSurface(inst *gpu.Instance, win *app.Window) (*gpu.Surface, error) {
	win.RetainDisplay() // the ICD hooks the connection; see app.Window.RetainDisplay
	return inst.CreateSurfaceXlib(win.XlibDisplay(), win.XlibWindow())
}

// libPaths prefers $GFX_WGPU_LIB, then the workspace-local library fetched
// by scripts/fetch-wgpu.sh, then system paths.
func LibPaths() []string {
	if p := os.Getenv("GFX_WGPU_LIB"); p != "" {
		return []string{p}
	}
	paths := gpu.DefaultLibNames()
	// A packaged app keeps the library beside the executable (or in wgpu/
	// there), as on Windows — but dlopen, unlike LoadLibrary, does not
	// look in the executable's directory by itself, so it has to be named.
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		for _, c := range []string{filepath.Join(d, paths[0]), filepath.Join(d, "wgpu", paths[0])} {
			if _, err := os.Stat(c); err == nil {
				return append([]string{c}, paths...)
			}
		}
	}
	if p := findBuildLib(paths[0]); p != "" {
		return append([]string{p}, paths...)
	}
	return paths
}
