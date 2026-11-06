package plat

import (
	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
)

func NewSurface(inst *gpu.Instance, win *app.Window) (*gpu.Surface, error) {
	return inst.CreateSurfaceAndroid(win.AndroidWindow())
}

// LibPaths is the bundled library's soname; the APK puts it in the app's
// native library dir, which is on the dynamic loader path.
func LibPaths() []string { return []string{"libwgpu_native.so"} }
