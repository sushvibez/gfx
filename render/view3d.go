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
	// Post, when set, displays the scene through the post-processing
	// stack: the Renderer3D (built for FormatRGBA16Float, FrameSettings
	// Tonemap = TonemapHDR) renders into Post's HDR target at this view's
	// pixel size, and Post writes the displayed image into the view. The
	// view does not own it: release it yourself. Clear is then linear
	// radiance.
	Post *Post

	device *gpu.Device
	r2     *Renderer2D
	format gpu.TextureFormat

	color     *gpu.Texture
	colorView *gpu.TextureView
	depth     *gpu.Texture
	depthView *gpu.TextureView
	img       *Image
	w, h      int // logical size (the layout box, the units of Size)
	pw, ph    int // target size in physical pixels (PixelSize)
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

// Resize sizes the view to w×h pixels, logical and physical alike (a DPI
// scale of 1); see ResizeScaled. Idempotent for an unchanged size.
func (v *View3D) Resize(w, h int) { v.ResizeScaled(w, h, 1) }

// ResizeScaled sizes the view to a w×h box in logical pixels (the units of
// the layout, the mouse and Size) with the textures allocated at
// w·scale × h·scale physical pixels, so a display scaled 1.5x or 2x renders
// the scene at full resolution instead of stretching a logical-sized image.
// The ui.Viewport3D element calls it during layout with the 2D renderer's
// DPI scale (Renderer2D.Scale). The textures are (re)allocated only when
// the pixel size changes; scale <= 0 counts as 1.
func (v *View3D) ResizeScaled(w, h int, scale float32) {
	v.w, v.h = max(w, 1), max(h, 1)
	pw, ph := ScaledPixels(v.w, v.h, scale)
	if pw == v.pw && ph == v.ph && v.color != nil {
		return
	}
	v.release()
	v.pw, v.ph = pw, ph
	if v.device == nil {
		return // test views without a device keep only the sizes
	}
	v.color = v.device.CreateTexture2D(pw, ph, v.format, gpu.TextureRenderAttachment|gpu.TextureBinding)
	v.colorView = v.color.CreateView()
	v.depth = v.device.CreateDepthTexture(pw, ph)
	v.depthView = v.depth.CreateView()
	v.img = v.r2.ImageFromView(v.colorView, pw, ph)
}

// ScaledPixels is the physical-pixel size of a w×h logical box at a DPI
// scale (rounded, at least 1×1; scale <= 0 counts as 1) — the size
// View3D.ResizeScaled allocates.
func ScaledPixels(w, h int, scale float32) (int, int) {
	if scale <= 0 {
		scale = 1
	}
	return max(int(float32(w)*scale+0.5), 1), max(int(float32(h)*scale+0.5), 1)
}

// Size returns the view's size in LOGICAL pixels (0,0 before the first
// Resize): the laid-out box, in the units of mouse coordinates and ui
// rects. Use it for the camera aspect and for mapping viewport-local
// points to pick rays (render.ScreenRay).
func (v *View3D) Size() (int, int) { return v.w, v.h }

// PixelSize returns the render target's size in physical pixels (Size
// times the DPI scale the view was sized with; 0,0 before the first
// Resize). Use it for anything measured in texels: readback, pixel-exact
// line widths, screen-space effects.
func (v *View3D) PixelSize() (int, int) { return v.pw, v.ph }

// Render records r3's queued frame (shadow + main pass) into the target.
// Call between r3.Begin+draws and the UI pass that samples Image.
func (v *View3D) Render(enc *gpu.CommandEncoder, r3 *Renderer3D) error {
	if v.colorView == nil {
		return nil // not yet sized
	}
	if v.Post != nil {
		v.Post.Resize(v.pw, v.ph)
		keep := r3.KeepDepth
		r3.KeepDepth = true // Post reads the depth buffer
		err := r3.Render(enc, v.Post.SceneView(), v.Post.DepthView(), v.Clear)
		r3.KeepDepth = keep
		if err != nil {
			return err
		}
		v.Post.Render(enc, v.colorView)
		return nil
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
func (v *View3D) Release() { v.release(); v.w, v.h, v.pw, v.ph = 0, 0, 0, 0 }
