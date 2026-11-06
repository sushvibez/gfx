//go:build linux || windows

// markdown is the Obsidian-parity editor subproject (see PLAN.md). Today it
// renders the reference chrome faithfully (screenshots/obsidian.png): icon
// ribbon, file explorer, tab strip, and a Canvas view with cards — the
// visual target the editor grows into.
//
//	cd markdown && go run .
//	go run . -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
	uishell "github.com/sushvibez/gfx/ui/shell"
)

func fontBytes() []byte {
	for _, p := range []string{
		"build/fonts/Inter-Regular.ttf",
		"../build/fonts/Inter-Regular.ttf",
		"/usr/share/fonts/truetype/ubuntu/Ubuntu-R.ttf",
	} {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	return goregular.TTF
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render one frame headlessly and write a PNG")
	flag.Parse()
	if err := loadIcons(); err != nil {
		fmt.Fprintln(os.Stderr, "markdown:", err)
		os.Exit(1)
	}
	var err error
	if *shot != "" {
		err = headless(*shot)
	} else {
		err = run(*frames)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "markdown:", err)
		os.Exit(1)
	}
}

func frame(dev *gpu.Device, q *gpu.Queue, r2 *render.Renderer2D, u *ui.UI,
	view *gpu.TextureView, w, h int) error {
	r2.Begin(w, h)
	u.Layout(float32(w), float32(h))
	u.Frame(float32(w), float32(h))
	enc := dev.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{R: 1, G: 1, B: 1, A: 1})
	if err := r2.Flush(pass); err != nil {
		return err
	}
	pass.End()
	q.Submit(enc.Finish())
	return nil
}

func run(maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "markdown: obsidian", Width: 1434, Height: 846, Undecorated: true})
	if err != nil {
		return err
	}
	defer win.Destroy()
	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		return err
	}
	surface, err := plat.NewSurface(inst, win)
	if err != nil {
		return err
	}
	adapter, err := inst.RequestAdapter(surface)
	if err != nil {
		return err
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		return err
	}
	queue := device.Queue()
	format := surface.PreferredFormat(adapter)
	w, h := win.Size()
	surface.Configure(device, w, h, format)

	r2, err := render.New2D(device, queue, format)
	if err != nil {
		return err
	}
	font, err := r2.NewFont(fontBytes(), 16)
	if err != nil {
		return err
	}
	s := newShell()
	s.onClose = func() { win.Close() }
	s.onMin = func() { win.Minimize() }
	s.onMax = func() { win.ToggleMaximize() }
	u := ui.New(r2, font, s.view)

	rendered := 0
	var winDrag uishell.WindowDrag
	win.EachFrame(func() bool {
		s.winW, s.winH = float32(w), float32(h)
		events := win.MouseEvents()
		u.Mouse(events)
		// Undecorated window: border-band presses resize, tab-strip-gap
		// presses move (app-driven via root-pointer deltas).
		winDrag.Handle(win, s.winW, s.winH, events, 5, func(x, y float32) bool {
			dr := s.titleDragRect()
			return x >= dr.X && x < dr.X+dr.W && y < dr.H
		})
		if nw, nh := win.Size(); nw != w || nh != h {
			w, h = nw, nh
			surface.Configure(device, w, h, format)
		}
		tex, ok := surface.CurrentTexture()
		if !ok {
			surface.Configure(device, w, h, format)
			return true
		}
		view := tex.CreateView()
		if err := frame(device, queue, r2, u, view, w, h); err != nil {
			fmt.Fprintln(os.Stderr, "markdown:", err)
			return false
		}
		// Present BEFORE releasing the swapchain texture — releasing
		// first leaks the swapchain slot until acquire blocks.
		surface.Present()
		view.Release()
		tex.Release()
		rendered++
		return maxFrames == 0 || rendered < maxFrames
	})
	return nil
}

func headless(path string) error {
	const W, H = 1434, 846
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

	r2, err := render.New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		return err
	}
	font, err := r2.NewFont(fontBytes(), 16)
	if err != nil {
		return err
	}
	s := newShell()
	s.winW, s.winH = W, H
	u := ui.New(r2, font, s.view)

	if err := frame(device, queue, r2, u, view, W, H); err != nil {
		return err
	}
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
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}
