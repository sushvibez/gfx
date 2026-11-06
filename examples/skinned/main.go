//go:build linux || windows

// skinned plays a skinned, animated glTF character (Khronos CesiumMan):
// render.AnimationPlayer samples the walk clip into the node hierarchy and
// the vertex shader skins the mesh each frame (GFX_CPU_SKIN=1 for the CPU path).
//
//	scripts/fetch-assets.sh   # fetches build/models/CesiumMan.glb
//	cd examples && go run ./skinned
//	go run ./skinned -screenshot out.png   # posed mid-stride
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"time"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/internal/orbit"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/render/gltf"
	"github.com/sushvibez/gfx/ui"
)

var lightDir = core.V3(-0.5, -1, -0.3)

func modelBytes() ([]byte, error) {
	for _, p := range []string{
		"build/models/CesiumMan.glb",
		"../build/models/CesiumMan.glb",
	} {
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("CesiumMan.glb not found — run scripts/fetch-assets.sh")
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
		fmt.Fprintln(os.Stderr, "skinned:", err)
		os.Exit(1)
	}
}

// setup loads the model and builds renderer state shared by both paths.
func setup(device *gpu.Device, queue *gpu.Queue, format gpu.TextureFormat) (
	*render.Renderer3D, *render.Renderer2D, *render.Model, *render.AnimationPlayer, *render.Font, error) {
	data, err := modelBytes()
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	doc, err := gltf.Parse(data, nil)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	r3, err := render.New3D(device, queue, format)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	r3.SetShadowArea(core.V3(0, 0.8, 0), 4)
	r2, err := render.New2D(device, queue, format)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	font, err := r2.NewFont(fontBytes(), 15)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	model, err := r3.BuildModel(doc)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	player := render.NewAnimationPlayer(model, doc.Animations)
	player.PlayIndex(0) // CesiumMan's single (unnamed) walk clip
	return r3, r2, model, player, font, nil
}

func frame(dev *gpu.Device, q *gpu.Queue, r3 *render.Renderer3D, r2 *render.Renderer2D,
	u *ui.UI, model *render.Model, cam *orbit.Camera,
	view, depthView *gpu.TextureView, w, h int) error {
	vp, eye := cam.ViewProj(w, h)
	r3.Begin(vp, lightDir, eye)
	// Ground slab + the character.
	r3.Draw(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.03, 0)).Mul(core.Mat4Scale(core.V3(6, 0.06, 6))),
		render.RGBA{R: 0.20, G: 0.21, B: 0.24, A: 1})
	model.Draw(r3, core.Mat4Identity())

	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, view, depthView, gpu.Color{R: 0.09, G: 0.10, B: 0.12, A: 1}); err != nil {
		return err
	}
	r2.Begin(w, h)
	u.Frame(float32(w), float32(h))
	pass := enc.BeginRenderPassLoad(view)
	if err := r2.Flush(pass); err != nil {
		return err
	}
	pass.End()
	q.Submit(enc.Finish())
	return nil
}

func run(maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "graphics: skinned character", Width: 900, Height: 640})
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

	r3, r2, model, player, font, err := setup(device, queue, format)
	if err != nil {
		return err
	}
	status := func() ui.Element {
		return ui.Div().P(10).Bg(ui.RGBA{R: 0, G: 0, B: 0, A: 0.55}).Rounded(8).Children(
			ui.Text(fmt.Sprintf("CesiumMan — clip %.2fs / %.2fs   (drag: orbit, wheel: zoom)",
				player.Position(), player.Duration()), ui.RGB(0xe8eaf0)),
		)
	}
	u := ui.New(r2, font, func() ui.Element {
		return ui.Div().P(12).Children(status())
	})
	cam := &orbit.Camera{Target: core.V3(0, 0.8, 0), Distance: 3.2, Yaw: 0.5, Pitch: 0.25}

	depth := device.CreateDepthTexture(w, h)
	depthView := depth.CreateView()
	last := time.Now()
	rendered := 0
	win.EachFrame(func() bool {
		now := time.Now()
		dt := float32(now.Sub(last).Seconds())
		last = now
		if dt > 0.1 {
			dt = 0.1
		}
		player.Advance(dt)

		u.Mouse(win.MouseEvents())
		cam.Handle(win.MouseEvents())
		if nw, nh := win.Size(); nw != w || nh != h {
			w, h = nw, nh
			surface.Configure(device, w, h, format)
			depthView.Release()
			depth.Release()
			depth = device.CreateDepthTexture(w, h)
			depthView = depth.CreateView()
		}
		tex, ok := surface.CurrentTexture()
		if !ok {
			surface.Configure(device, w, h, format)
			return true
		}
		view := tex.CreateView()
		err := frame(device, queue, r3, r2, u, model, cam, view, depthView, w, h)
		if err != nil {
			fmt.Fprintln(os.Stderr, "skinned:", err)
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

func headless(path string) error {
	const W, H = 900, 640
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

	r3, r2, model, player, font, err := setup(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		return err
	}
	player.Seek(0.6) // mid-stride pose so the shot shows deformation
	u := ui.New(r2, font, func() ui.Element {
		return ui.Div().P(12).Children(
			ui.Div().P(10).Bg(ui.RGBA{R: 0, G: 0, B: 0, A: 0.55}).Rounded(8).Children(
				ui.Text("CesiumMan — skinned pose at t=0.6s", ui.RGB(0xe8eaf0))))
	})
	cam := &orbit.Camera{Target: core.V3(0, 0.8, 0), Distance: 3.2, Yaw: 0.5, Pitch: 0.25}

	if err := frame(device, queue, r3, r2, u, model, cam, view, depthView, W, H); err != nil {
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
