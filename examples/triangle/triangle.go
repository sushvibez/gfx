//go:build linux || windows || js || android

package main

import (
	"fmt"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
)

const shader = `
@vertex
fn vs(@builtin(vertex_index) i : u32) -> @builtin(position) vec4f {
    var p = array<vec2f, 3>(
        vec2f( 0.0,  0.6),
        vec2f(-0.6, -0.6),
        vec2f( 0.6, -0.6),
    );
    return vec4f(p[i], 0.0, 1.0);
}

@fragment
fn fs() -> @location(0) vec4f {
    return vec4f(1.0, 0.5, 0.1, 1.0);
}
`

func run(maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "graphics: triangle", Width: 800, Height: 600})
	if err != nil {
		return err
	}
	defer win.Destroy()

	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		return err
	}
	defer inst.Release()

	surface, err := plat.NewSurface(inst, win)
	if err != nil {
		return err
	}
	defer surface.Release()

	adapter, err := inst.RequestAdapter(surface)
	if err != nil {
		return err
	}
	defer adapter.Release()
	fmt.Println("adapter:", adapter.Name())

	device, err := adapter.RequestDevice()
	if err != nil {
		return err
	}
	defer device.Release()
	queue := device.Queue()
	defer queue.Release()

	format := surface.PreferredFormat(adapter)
	w, h := win.Size()
	surface.Configure(device, w, h, format)

	pipeline, err := newPipeline(device, format)
	if err != nil {
		return err
	}
	defer pipeline.Release()

	rendered := 0
	win.EachFrame(func() bool {
		if nw, nh := win.Size(); nw != w || nh != h {
			w, h = nw, nh
			surface.Configure(device, w, h, format)
		}

		tex, ok := surface.CurrentTexture()
		if !ok {
			surface.Configure(device, w, h, format) // outdated swap chain
			return true
		}
		view := tex.CreateView()
		draw(device, queue, pipeline, view)
		surface.Present()

		view.Release()
		tex.Release()

		rendered++
		return maxFrames == 0 || rendered < maxFrames
	})
	fmt.Printf("rendered %d frames at %dx%d\n", rendered, w, h)
	return nil
}

// newPipeline builds the triangle's pipeline for a target format.
func newPipeline(device *gpu.Device, format gpu.TextureFormat) (*gpu.RenderPipeline, error) {
	module, err := device.CreateShaderModuleWGSL("triangle", shader)
	if err != nil {
		return nil, err
	}
	defer module.Release()
	return device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader:        module,
		VertexEntry:   "vs",
		FragmentEntry: "fs",
		Format:        format,
	})
}

// draw clears the target and draws the triangle into it.
func draw(device *gpu.Device, queue *gpu.Queue, pipeline *gpu.RenderPipeline, view *gpu.TextureView) {
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{R: 0.05, G: 0.05, B: 0.08, A: 1})
	pass.SetPipeline(pipeline)
	pass.Draw(3, 1, 0, 0)
	pass.End()
	queue.Submit(enc.Finish())
}
