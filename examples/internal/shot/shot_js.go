//go:build js

package shot

import (
	"fmt"

	"github.com/sushvibez/gfx/gpu"
)

// Format is the target's format (see the native side).
const Format = gpu.FormatRGBA8Unorm

// Render is native-only: GPU readback does not exist on the web.
func Render(w, h, frames int, path string,
	setup func(d *gpu.Device, q *gpu.Queue, f gpu.TextureFormat) (frame func(view *gpu.TextureView), err error)) error {
	return fmt.Errorf("-screenshot is native-only; on the web just look at the canvas")
}
