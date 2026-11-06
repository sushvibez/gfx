//go:build !js

// Package shot renders an example's raw-GPU frames offscreen and writes
// the last one as a PNG: the -screenshot path for the examples that drive
// gpu directly instead of through ui/shell (which has its own).
package shot

import (
	"image"
	"image/png"
	"os"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
)

// Format is the target's format, the one setup must build pipelines for.
const Format = gpu.FormatRGBA8Unorm

// Render opens a headless device, lets setup build the example's GPU state
// for a w×h Format target, calls the returned frame function frames times
// (at least once) on that target, and writes the last frame to path.
// frame encodes and submits its own work.
func Render(w, h, frames int, path string,
	setup func(d *gpu.Device, q *gpu.Queue, f gpu.TextureFormat) (frame func(view *gpu.TextureView), err error)) error {
	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		return err
	}
	defer inst.Release()
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		return err
	}
	defer adapter.Release()
	device, err := adapter.RequestDevice()
	if err != nil {
		return err
	}
	defer device.Release()
	queue := device.Queue()

	target := device.CreateRenderTexture(w, h, Format)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	frame, err := setup(device, queue, Format)
	if err != nil {
		return err
	}
	for range max(frames, 1) {
		frame(view)
	}
	pix, err := device.ReadTexture(queue, target, w, h)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, &image.RGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)})
}
