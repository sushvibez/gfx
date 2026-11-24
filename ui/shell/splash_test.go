//go:build !js

package shell

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"runtime"
	"testing"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// TestDrawSplash: the splash image lands centred at its own logical size
// (twice as many pixels at scale 2) over the clear colour, and the hook
// draws over it — offscreen, no window.
func TestDrawSplash(t *testing.T) {
	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		t.Skip("no wgpu-native: ", err)
	}
	defer inst.Release()
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Skip("no GPU adapter: ", err)
	}
	defer adapter.Release()
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Skip("no GPU device: ", err)
	}
	defer device.Release()
	queue := device.Queue()
	r, err := render.New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()

	logo := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for i := range logo.Pix {
		if i%4 == 0 || i%4 == 3 {
			logo.Pix[i] = 255 // opaque red
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, logo)
	hooked := false
	o := Options{ClearHex: 0x204060, SplashImage: buf.Bytes(), Splash: func(r *render.Renderer2D, w, h float32) {
		hooked = true
		r.FillRect(0, 0, 4, 4, render.RGBA{G: 1, A: 1}) // a green mark in the corner
	}}.withDefaults()

	const W, H = 200, 120
	for _, scale := range []float32{1, 2} {
		target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
		view := target.CreateView()
		img := drawSplash(o, r, W, H, scale)
		enc := device.CreateCommandEncoder()
		pass := enc.BeginRenderPass(view, o.clearColor())
		if err := r.Flush(pass); err != nil {
			t.Fatal(err)
		}
		pass.End()
		queue.Submit(enc.Finish())
		if img != nil {
			img.Release()
		}
		pix, err := device.ReadTexture(queue, target, W, H)
		view.Release()
		target.Release()
		if err != nil {
			t.Fatal(err)
		}
		at := func(x, y int) color.RGBA {
			i := (y*W + x) * 4
			return color.RGBA{pix[i], pix[i+1], pix[i+2], pix[i+3]}
		}
		red := func(c color.RGBA) bool { return c.R > 200 && c.G < 40 && c.B < 40 }
		bg := func(c color.RGBA) bool { return c.R == 0x20 && c.G == 0x40 && c.B == 0x60 }
		// The image spans 40×20 logical pixels about the centre.
		hw, hh := int(20*scale), int(10*scale)
		for _, p := range [][2]int{{W / 2, H / 2}, {W/2 - hw + 2, H/2 - hh + 2}, {W/2 + hw - 3, H/2 + hh - 3}} {
			if c := at(p[0], p[1]); !red(c) {
				t.Errorf("scale %v: %v inside the image is %v, want red", scale, p, c)
			}
		}
		for _, p := range [][2]int{{W/2 - hw - 3, H / 2}, {W / 2, H/2 + hh + 3}, {W - 2, H - 2}} {
			if c := at(p[0], p[1]); !bg(c) {
				t.Errorf("scale %v: %v outside the image is %v, want the clear colour", scale, p, c)
			}
		}
		if c := at(1, 1); c.G < 200 || c.R > 40 {
			t.Errorf("scale %v: the hook's mark is %v, want green", scale, c)
		}
	}
	if !hooked {
		t.Error("the Splash hook was not called")
	}
}

// TestRunShowsSplashBeforeInit: Run draws the splash before Init (by hand:
// it opens a window, GFX_WINDOW_TESTS=1).
func TestRunShowsSplashBeforeInit(t *testing.T) {
	if os.Getenv("GFX_WINDOW_TESTS") == "" {
		t.Skip("set GFX_WINDOW_TESTS=1 to open a window")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	needGPU(t)
	var order []string
	hooks := Hooks{Init: func(*App) (func() ui.Element, error) {
		order = append(order, "init")
		return func() ui.Element { return ui.Div() }, nil
	}}
	o := Options{Title: "splash test", Width: 200, Height: 120, MaxFrames: 3,
		Splash: func(*render.Renderer2D, float32, float32) { order = append(order, "splash") }}
	if err := Run(o, hooks); err != nil && len(order) == 0 {
		t.Skip("no window here: ", err)
	}
	if len(order) != 2 || order[0] != "splash" || order[1] != "init" {
		t.Fatalf("order %v, want [splash init]", order)
	}
}
