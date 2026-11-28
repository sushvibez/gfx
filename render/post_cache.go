package render

import (
	"sync"

	"github.com/sushvibez/gfx/gpu"
)

// postPipes shares Post's pipelines between the Posts of one device: they
// are the same for every Post of a display format, and building them is
// most of what a Post costs (seconds with a slow shader compiler), so a
// second camera view — a preview beside the game's, a thumbnail, the next
// mission — gets them at once.
//
// The cache keeps one reference to each pipeline and hands every Post its
// own (RenderPipeline.Share), which that Post releases as before; the
// cache's references go when the device is released (Device.OnRelease).
var postPipes struct {
	sync.Mutex
	m       map[postPipeKey]*gpu.RenderPipeline
	devices map[*gpu.Device]bool // devices with a release hook
}

type postPipeKey struct {
	dev    *gpu.Device
	src    string // the whole WGSL source (label alone could hide a variant)
	entry  string
	format gpu.TextureFormat
}

// postPipeline is device's pipeline for (src, entry, format) — built once
// per device and shared; the caller owns (and releases) what it gets.
func postPipeline(device *gpu.Device, label, src, entry string, format gpu.TextureFormat) (*gpu.RenderPipeline, error) {
	k := postPipeKey{device, src, entry, format}
	postPipes.Lock()
	defer postPipes.Unlock()
	if pl, ok := postPipes.m[k]; ok {
		return pl.Share(), nil
	}
	m, err := device.CreateShaderModuleWGSL(label, src)
	if err != nil {
		return nil, err
	}
	defer m.Release()
	pl, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: m, VertexEntry: "vs", FragmentEntry: entry, Format: format,
	})
	if err != nil {
		return nil, err
	}
	if postPipes.m == nil {
		postPipes.m = map[postPipeKey]*gpu.RenderPipeline{}
		postPipes.devices = map[*gpu.Device]bool{}
	}
	if !postPipes.devices[device] {
		postPipes.devices[device] = true
		device.OnRelease(func() { releasePostPipes(device) })
	}
	postPipes.m[k] = pl
	return pl.Share(), nil
}

// releasePostPipes drops the cache's references to device's pipelines.
func releasePostPipes(device *gpu.Device) {
	postPipes.Lock()
	defer postPipes.Unlock()
	for k, pl := range postPipes.m {
		if k.dev == device {
			pl.Release()
			delete(postPipes.m, k)
		}
	}
	delete(postPipes.devices, device)
}
