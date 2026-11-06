package plat

import (
	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
)

func NewSurface(inst *gpu.Instance, win *app.Window) (*gpu.Surface, error) {
	return inst.CreateSurfaceCanvas(win.Canvas())
}

// libPaths is empty on web: the GPU comes from the browser, not a library.
func LibPaths() []string { return nil }
