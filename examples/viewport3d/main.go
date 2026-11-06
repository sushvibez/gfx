//go:build linux || windows

// viewport3d demonstrates ui.Viewport3D: a live 3D scene embedded INSIDE a
// normal 2D layout (header, side panel, footer) as an ordinary element —
// rendered to an offscreen render.View3D target and composited by the UI
// like any image. Clicking inside the viewport ray-picks the scene
// (render.ScreenRay + Renderer3D.Pick); the picked cube highlights and the
// panel reports it.
//
//	go run ./viewport3d
//	go run ./viewport3d -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/internal/orbit"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

var lightDir = core.V3(-0.5, -1, -0.4)

// cubeInfo is one pickable cube in the scene.
type cubeInfo struct {
	name  string
	pos   core.Vec3
	scale core.Vec3
	color render.RGBA
}

var cubes = []cubeInfo{
	{"red crate", core.V3(-1.6, 0.5, 0), core.V3(1, 1, 1), render.RGBA{R: 0.85, G: 0.25, B: 0.2, A: 1}},
	{"green slab", core.V3(0.4, 0.25, 0.8), core.V3(1.4, 0.5, 1), render.RGBA{R: 0.25, G: 0.7, B: 0.35, A: 1}},
	{"blue tower", core.V3(1.4, 1.0, -0.9), core.V3(0.7, 2, 0.7), render.RGBA{R: 0.25, G: 0.45, B: 0.9, A: 1}},
}

// scene draws the pickable cubes over a ground slab, highlighting selected.
func scene(r3 *render.Renderer3D, cube *render.Mesh, selected int) {
	r3.Draw(cube, core.Mat4Translate(core.V3(0, -0.15, 0)).Mul(core.Mat4Scale(core.V3(10, 0.3, 10))),
		render.RGBA{R: 0.22, G: 0.23, B: 0.26, A: 1})
	for i, c := range cubes {
		col := c.color
		if i == selected {
			col = render.RGBA{R: col.R*0.4 + 0.5, G: col.G*0.4 + 0.5, B: col.B*0.4 + 0.3, A: 1}
		}
		r3.SetPickTag(i)
		r3.DrawPBR(cube, core.Mat4Translate(c.pos).Mul(core.Mat4Scale(c.scale)), col, 0.1, 0.5, nil)
	}
	r3.SetPickTag(-1)
}

func fontBytes() []byte {
	for _, p := range []string{
		"/usr/share/fonts/truetype/ubuntu/Ubuntu-R.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	} {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	return goregular.TTF
}

// shellUI builds the 2D layout with the viewport embedded mid-page.
type shellUI struct {
	vp       *ui.Viewport3DEl
	selected core.Property[int]
	lastTap  core.Property[string]
}

func (s *shellUI) view() ui.Element {
	dark := ui.RGB(0x1d1f26)
	panel := ui.RGB(0x262933)
	fg := ui.RGB(0xe8eaf0)
	dim := ui.RGB(0x9aa0ac)
	accent := ui.RGB(0x4c8dff)

	name := "nothing — click a cube"
	if i := s.selected.Get(); i >= 0 && i < len(cubes) {
		name = cubes[i].name
	}
	side := ui.Div().Col().Gap(10).P(14).W(230).Bg(panel).Rounded(10).Children(
		ui.Text("Scene", fg).SizePx(15).Bold(),
		ui.Div().H(1).Bg(ui.RGBA{R: 1, G: 1, B: 1, A: 0.08}),
		ui.Text("The 3D view on the left is a", dim).SizePx(12),
		ui.Text("ui.Viewport3D element — an", dim).SizePx(12),
		ui.Text("offscreen target in the layout.", dim).SizePx(12),
		ui.Div().H(8),
		ui.Text("Picked:", dim).SizePx(12),
		ui.Text(name, accent).SizePx(14).Bold(),
		ui.Text(s.lastTap.Get(), dim).SizePx(11),
	)
	return ui.Div().Col().Grow().Gap(12).P(14).Bg(dark).Children(
		ui.Text("Embedded 3D viewport + ray picking", fg).SizePx(17).Bold(),
		ui.Div().Row().Grow().Gap(12).Children(s.vp, side),
		ui.Div().Row().P(8).Bg(panel).Rounded(8).Children(
			ui.Text("drag inside the view: orbit   wheel: zoom   click: pick", dim).SizePx(12)),
	)
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render one frame headlessly and write a PNG")
	flag.Parse()
	var err error
	if *shot != "" {
		err = headless(*shot)
	} else {
		err = run(*frames)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "viewport3d:", err)
		os.Exit(1)
	}
}

func run(maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "graphics: viewport3d", Width: 960, Height: 620})
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

	r3, err := render.New3D(device, queue, format)
	if err != nil {
		return err
	}
	r3.SetShadowArea(core.V3(0, 0, 0), 6)
	r2, err := render.New2D(device, queue, format)
	if err != nil {
		return err
	}
	font, err := r2.NewFont(fontBytes(), 16)
	if err != nil {
		return err
	}

	view3d := render.NewView3D(device, r2, format)
	cam := &orbit.Camera{Target: core.V3(0, 0.4, 0), Distance: 7, Yaw: 0.6, Pitch: 0.45}
	s := &shellUI{}
	s.selected.Set(-1)

	// The tap arrives in viewport-local pixels — exactly what ScreenRay wants.
	s.vp = ui.Viewport3D(view3d).Grow().OnTap(func(x, y float32) {
		vw, vh := view3d.Size()
		vp, _ := cam.ViewProj(vw, vh)
		if o, d, ok := render.ScreenRay(vp, x, y, float32(vw), float32(vh)); ok {
			if hit, ok := r3.Pick(o, d); ok && hit.Tag >= 0 {
				s.selected.Set(hit.Tag)
				s.lastTap.Set(fmt.Sprintf("hit at (%.2f, %.2f, %.2f)", hit.Point.X, hit.Point.Y, hit.Point.Z))
			} else {
				s.selected.Set(-1)
				s.lastTap.Set("")
			}
		}
	})
	u := ui.New(r2, font, s.view)
	cube := r3.CubeMesh()

	rendered := 0
	win.EachFrame(func() bool {
		events := win.MouseEvents()
		u.Mouse(events)
		// Orbit only with pointer activity inside the viewport.
		r := s.vp.Rect()
		var camEvents []app.MouseEvent
		for _, m := range events {
			if m.X >= r.X && m.X < r.X+r.W && m.Y >= r.Y && m.Y < r.Y+r.H {
				camEvents = append(camEvents, m)
			}
		}
		cam.Handle(camEvents)

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
		err := frame(device, queue, r3, r2, u, view3d, cam, cube, s, view, w, h)
		if err != nil {
			fmt.Fprintln(os.Stderr, "viewport3d:", err)
			return false
		}
		// Present BEFORE releasing the swapchain texture — releasing
		// first drops the reference presentation needs and the
		// swapchain slot leaks until acquire blocks (frozen window).
		surface.Present()
		view.Release()
		tex.Release()
		rendered++
		return maxFrames == 0 || rendered < maxFrames
	})
	return nil
}

// frame: UI layout first (sizes the offscreen target), then the 3D pass into
// it, then the UI pass sampling it — one encoder, one submit.
func frame(dev *gpu.Device, q *gpu.Queue, r3 *render.Renderer3D, r2 *render.Renderer2D,
	u *ui.UI, view3d *render.View3D, cam *orbit.Camera, cube *render.Mesh,
	s *shellUI, target *gpu.TextureView, w, h int) error {
	u.Layout(float32(w), float32(h)) // resolves the viewport rect + target size

	vw, vh := view3d.Size()
	vp, eye := cam.ViewProj(vw, vh)
	r3.Begin(vp, lightDir, eye)
	scene(r3, cube, s.selected.Get())

	enc := dev.CreateCommandEncoder()
	if err := view3d.Render(enc, r3); err != nil {
		return err
	}
	r2.Begin(w, h)
	u.Frame(float32(w), float32(h))
	pass := enc.BeginRenderPass(target, gpu.Color{R: 0.09, G: 0.10, B: 0.12, A: 1})
	if err := r2.Flush(pass); err != nil {
		return err
	}
	pass.End()
	q.Submit(enc.Finish())
	return nil
}

// headless renders one frame offscreen and writes a PNG.
func headless(path string) error {
	const W, H = 960, 620
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

	r3, err := render.New3D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		return err
	}
	r3.SetShadowArea(core.V3(0, 0, 0), 6)
	r2, err := render.New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		return err
	}
	font, err := r2.NewFont(fontBytes(), 16)
	if err != nil {
		return err
	}

	view3d := render.NewView3D(device, r2, gpu.FormatRGBA8Unorm)
	cam := &orbit.Camera{Target: core.V3(0, 0.4, 0), Distance: 7, Yaw: 0.6, Pitch: 0.45}
	s := &shellUI{}
	s.selected.Set(2) // pre-select the tower so the shot shows the highlight
	s.lastTap.Set("(preselected for screenshot)")
	s.vp = ui.Viewport3D(view3d).Grow()
	u := ui.New(r2, font, s.view)
	cube := r3.CubeMesh()

	if err := frame(device, queue, r3, r2, u, view3d, cam, cube, s, view, W, H); err != nil {
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
