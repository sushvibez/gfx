//go:build linux || windows || js

package shell

import (
	"fmt"
	"os"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
)

// splashOn reports whether Run puts up a splash before Hooks.Init: one was
// asked for and the window will be seen.
func (o Options) splashOn() bool {
	return !o.Hidden && (o.Splash != nil || len(o.SplashImage) > 0)
}

// presentSplash draws the splash (o.SplashImage centred over the clear
// colour, then o.Splash over it) into one swapchain image, presents it and
// shows the window, so the window comes up at its final size with the
// splash on it while Hooks.Init builds the app. It reports whether it
// showed the window; a failure only costs the splash (the window then
// appears with the first real frame, as without one).
func presentSplash(o Options, win *app.Window, surface *gpu.Surface, device *gpu.Device, queue *gpu.Queue,
	r *render.Renderer2D, msaaView *gpu.TextureView, w, h int) bool {
	tex, ok := surface.CurrentTexture()
	if !ok {
		return false
	}
	view := tex.CreateView()
	scale := win.Scale()
	if scale <= 0 {
		scale = 1
	}
	img := drawSplash(o, r, w, h, scale)
	enc := device.CreateCommandEncoder()
	var pass *gpu.RenderPass
	if msaaView != nil {
		pass = enc.BeginRenderPassResolve(msaaView, view, o.clearColor())
	} else {
		pass = enc.BeginRenderPass(view, o.clearColor())
	}
	err := r.Flush(pass)
	pass.End()
	if err == nil {
		queue.Submit(enc.Finish())
		surface.Present()
	} else {
		fmt.Fprintln(os.Stderr, "shell: splash:", err)
	}
	view.Release()
	tex.Release()
	if img != nil {
		img.Release() // the submitted frame keeps what it reads alive
	}
	if err != nil {
		return false
	}
	win.Show()
	return true
}

// drawSplash records the splash into r for a w×h target at scale: the
// image centred at its own size in logical pixels (fitted down into nine
// tenths of a smaller window), then the Splash hook. It returns the loaded
// image, to release once the frame is submitted.
func drawSplash(o Options, r *render.Renderer2D, w, h int, scale float32) *render.Image {
	lw, lh := float32(w)/scale, float32(h)/scale
	r.Begin(w, h)
	r.SetScale(scale)
	var img *render.Image
	if len(o.SplashImage) > 0 {
		var err error
		if img, err = r.LoadImage(o.SplashImage); err != nil {
			fmt.Fprintln(os.Stderr, "shell: splash image:", err)
			img = nil
		} else {
			// One image pixel to a logical pixel: the same size on a
			// high-DPI screen as the app's own layout.
			iw, ih := float32(img.W), float32(img.H)
			if k := min(lw*0.9/iw, lh*0.9/ih); k < 1 {
				iw, ih = iw*k, ih*k
			}
			r.DrawImage(img, (lw-iw)/2, (lh-ih)/2, iw, ih, render.RGBA{R: 1, G: 1, B: 1, A: 1})
		}
	}
	if o.Splash != nil {
		o.Splash(r, lw, lh)
	}
	return img
}
