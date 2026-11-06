//go:build !js

package main

import (
	"image"
	"image/png"
	"os"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
)

// screenshot renders one frame of the scene fully headlessly.
func screenshot(path string) error {
	const W, H = 1100, 700
	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		return err
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		return err
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		return err
	}
	queue := device.Queue()

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	depth := device.CreateDepthTexture(W, H)
	depthView := depth.CreateView()

	r, err := render.New3D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		return err
	}
	s, err := setup(r)
	if err != nil {
		return err
	}
	vp, eye := newCamera().ViewProj(W, H)
	r.Begin(vp, lightDir, eye)
	s.draw(r)

	enc := device.CreateCommandEncoder()
	if err := r.Render(enc, view, depthView, clearColor()); err != nil {
		return err
	}
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		return err
	}
	img := &image.RGBA{Pix: pix, Stride: W * 4, Rect: image.Rect(0, 0, W, H)}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
