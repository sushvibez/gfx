//go:build !js

package main

import (
	"image"
	"image/png"
	"os"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
)

func screenshot(assetPath, outPath string) error {
	const W, H = 900, 700
	doc, err := loadDoc(assetPath)
	if err != nil {
		return err
	}
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
	model, err := r.BuildModel(doc)
	if err != nil {
		return err
	}
	ground := r.CubeMesh()
	r.SetShadowArea(core.V3(0, 0.8, 0), 4)

	vp, cam := frameCamera(W, H, 0.9)
	r.Begin(vp, lightDir, cam)
	r.Draw(ground, core.Mat4Scale(core.V3(10, 0.1, 10)).Mul(core.Mat4Translate(core.V3(0, -0.5, 0))), render.RGBA{R: 0.45, G: 0.47, B: 0.52, A: 1})
	model.Draw(r, core.Mat4Identity())

	enc := device.CreateCommandEncoder()
	if err := r.Render(enc, view, depthView, gpu.Color{R: 0.05, G: 0.055, B: 0.08, A: 1}); err != nil {
		return err
	}
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		return err
	}
	img := &image.RGBA{Pix: pix, Stride: W * 4, Rect: image.Rect(0, 0, W, H)}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
