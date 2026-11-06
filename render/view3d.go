package render

// View3D is an offscreen 3D render target a ui.Viewport3D element can embed
// anywhere in a 2D layout (Godot's SubViewport, Qt Quick's scene-in-item):
// the 3D frame renders into a private color+depth texture pair, and the 2D
// renderer samples it as an ordinary image — so it clips, scrolls, overlaps
// and stacks like any other element, and several viewports can coexist.
//
//	v := render.NewView3D(device, r2, format)
//	// per frame, before the UI pass:
//	r3.Begin(cam.ViewProj(v.Size()), light, eye)
//	drawScene(r3)
//	v.Render(enc, r3)
//	// then the ui.Viewport3D element paints v.Image()
//
// One Renderer3D can serve one View3D per queue submission (its draw buffer
// is written per Render); give each simultaneous viewport its own
// Renderer3D, or submit between renders.

import "github.com/sushvibez/gfx/gpu"

type View3D struct {
	// Clear is the viewport background (defaults to opaque dark).
	Clear gpu.Color

	device *gpu.Device
	r2     *Renderer2D
	format gpu.TextureFormat

	color     *gpu.Texture
	colorView *gpu.TextureView
	depth     *gpu.Texture
	depthView *gpu.TextureView
	img       *Image
	w, h      int
}

// NewView3D creates an (initially empty) offscreen target. format must match
// the format the Renderer3D pipelines were built for (normally the surface
// format). Size is set by Resize — the ui element does it during layout.
func NewView3D(device *gpu.Device, r2 *Renderer2D, format gpu.TextureFormat) *View3D {
	return &View3D{
		Clear:  gpu.Color{R: 0.075, G: 0.08, B: 0.10, A: 1},
		device: device,
		r2:     r2,
		format: format,
	}
}

// Resize (re)allocates the target textures; idempotent for an unchanged size.
func (v *View3D) Resize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w == v.w && h == v.h {
		return
	}
	v.release()
	v.w, v.h = w, h
	v.color = v.device.CreateTexture2D(w, h, v.format, gpu.TextureRenderAttachment|gpu.TextureBinding)
	v.colorView = v.color.CreateView()
	v.depth = v.device.CreateDepthTexture(w, h)
	v.depthView = v.depth.CreateView()
	v.img = v.r2.ImageFromView(v.colorView, w, h)
}

// Size returns the current target size in pixels (0,0 before first Resize).
func (v *View3D) Size() (int, int) { return v.w, v.h }

// Render records r3's queued frame (shadow + main pass) into the target.
// Call between r3.Begin+draws and the UI pass that samples Image.
func (v *View3D) Render(enc *gpu.CommandEncoder, r3 *Renderer3D) error {
	if v.colorView == nil {
		return nil // not yet sized
	}
	return r3.Render(enc, v.colorView, v.depthView, v.Clear)
}

// Image returns the sampleable color target (nil before first Resize).
func (v *View3D) Image() *Image { return v.img }

func (v *View3D) release() {
	if v.img != nil {
		v.img.Release()
		v.img = nil
	}
	if v.depthView != nil {
		v.depthView.Release()
		v.depthView = nil
	}
	if v.depth != nil {
		v.depth.Release()
		v.depth = nil
	}
	if v.colorView != nil {
		v.colorView.Release()
		v.colorView = nil
	}
	if v.color != nil {
		v.color.Release()
		v.color = nil
	}
}

// Release frees the target textures.
func (v *View3D) Release() { v.release(); v.w, v.h = 0, 0 }
