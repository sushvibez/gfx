package plat

import (
	"os"
	"path/filepath"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
)

func NewSurface(inst *gpu.Instance, win *app.Window) (*gpu.Surface, error) {
	return inst.CreateSurfaceWindows(win.HInstance(), win.HWND())
}

// libPaths prefers $GFX_WGPU_LIB, then the workspace-local DLL fetched by
// scripts/fetch-wgpu.sh, then the system search path.
func LibPaths() []string {
	if p := os.Getenv("GFX_WGPU_LIB"); p != "" {
		return []string{p}
	}
	paths := gpu.DefaultLibNames()
	// A packaged app keeps the DLL beside the executable (or in wgpu/ there).
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
