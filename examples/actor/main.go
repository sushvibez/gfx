//go:build linux || windows

// actor plays a binary-FBX character with a separately-shipped FBX clip —
// the render/fbx pipeline end to end: fbx.Parse + fbx.ToGLTF for both files,
// fbx.Retarget to rebase the clip onto the character's skeleton (Mixamo
// exports mesh and animations as separate files with mismatched rig
// namespaces), then the ordinary render.BuildModel + AnimationPlayer path.
//
// Pass -mesh (a skinned FBX character) and optionally -anim (a clip to
// retarget onto it), e.g. a Mixamo export.
//
//	cd examples && go run ./actor
//	go run ./actor -screenshot out.png        # posed mid-clip
//	go run ./actor -anim ""                   # clips from the mesh file itself
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/internal/orbit"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/render/fbx"
	"github.com/sushvibez/gfx/render/gltf"
	"github.com/sushvibez/gfx/ui"
)

var (
	meshPath = flag.String("mesh", "", "FBX file with the character mesh + skeleton")
	animPath = flag.String("anim", "", "FBX file with the clip to retarget (empty: use the mesh file's own clips)")
)

var lightDir = core.V3(-0.5, -1, -0.3)

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

func loadFBX(path string) (*gltf.Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := fbx.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// External (non-embedded) textures resolve relative to the FBX's dir.
	dir := filepath.Dir(path)
	doc, err := fbx.ToGLTF(d, func(uri string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, filepath.FromSlash(uri)))
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
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
		fmt.Fprintln(os.Stderr, "actor:", err)
		os.Exit(1)
	}
}

// setup loads mesh + clip, retargets, and builds renderer state shared by
// both paths. place normalizes the model (FBX rigs are usually centimeters)
// to ~1.8 units tall, feet at the origin.
func setup(device *gpu.Device, queue *gpu.Queue, format gpu.TextureFormat) (
	*render.Renderer3D, *render.Renderer2D, *render.Model, *render.AnimationPlayer,
	core.Mat4, *render.Font, error) {
	fail := func(err error) (*render.Renderer3D, *render.Renderer2D, *render.Model,
		*render.AnimationPlayer, core.Mat4, *render.Font, error) {
		return nil, nil, nil, nil, core.Mat4Identity(), nil, err
	}
	if *meshPath == "" {
		return fail(fmt.Errorf("pass -mesh <file.fbx>: a skinned FBX character (and -anim <clip.fbx> for a separate clip)"))
	}
	meshDoc, err := loadFBX(*meshPath)
	if err != nil {
		return fail(err)
	}
	clips := meshDoc.Animations
	if *animPath != "" {
		animDoc, err := loadFBX(*animPath)
		if err != nil {
			return fail(err)
		}
		fbx.Retarget(meshDoc, animDoc)
		clips = animDoc.Animations
	}
	if len(clips) == 0 {
		return fail(fmt.Errorf("no animation clips in %s", *animPath))
	}

	r3, err := render.New3D(device, queue, format)
	if err != nil {
		return fail(err)
	}
	r3.SetShadowArea(core.V3(0, 0.9, 0), 4)
	r2, err := render.New2D(device, queue, format)
	if err != nil {
		return fail(err)
	}
	font, err := r2.NewFont(fontBytes(), 15)
	if err != nil {
		return fail(err)
	}
	model, err := r3.BuildModel(meshDoc)
	if err != nil {
		return fail(err)
	}

	// Frame on the bind pose: scale to ~1.8 units tall, feet on the ground.
	lo, hi := model.Bounds()
	s := float32(1)
	if h := hi.Y - lo.Y; h > 0 {
		s = 1.8 / h
	}
	c := lo.Add(hi).Scale(0.5)
	place := core.Mat4Translate(core.V3(-c.X*s, -lo.Y*s, -c.Z*s)).
		Mul(core.Mat4Scale(core.V3(s, s, s)))

	player := render.NewAnimationPlayer(model, clips)
	player.Loop = true
	player.PlayIndex(0)
	return r3, r2, model, player, place, font, nil
}

// eyeLevelCamera starts at roughly the actor's eye height looking at the
// chest — a person seen standing across from you, not from a drone.
func eyeLevelCamera() *orbit.Camera {
	return &orbit.Camera{Target: core.V3(0, 1.0, 0), Distance: 3.0, Yaw: 0.5, Pitch: 0.06}
}

func caption(player *render.AnimationPlayer) string {
	name := strings.TrimSuffix(filepath.Base(*animPath), filepath.Ext(*animPath))
	if *animPath == "" {
		name = player.Clips()[0]
	}
	return fmt.Sprintf("%s — %s  %.2fs / %.2fs",
		strings.TrimSuffix(filepath.Base(*meshPath), filepath.Ext(*meshPath)),
		name, player.Position(), player.Duration())
}

func frame(dev *gpu.Device, q *gpu.Queue, r3 *render.Renderer3D, r2 *render.Renderer2D,
	u *ui.UI, model *render.Model, place core.Mat4, cam *orbit.Camera,
	view, depthView *gpu.TextureView, w, h int) error {
	vp, eye := cam.ViewProj(w, h)
	r3.Begin(vp, lightDir, eye)
	r3.Draw(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.03, 0)).Mul(core.Mat4Scale(core.V3(6, 0.06, 6))),
		render.RGBA{R: 0.20, G: 0.21, B: 0.24, A: 1})
	model.Draw(r3, place)

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
	win, err := app.NewWindow(app.Options{Title: "graphics: FBX actor", Width: 900, Height: 640})
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

	r3, r2, model, player, place, font, err := setup(device, queue, format)
	if err != nil {
		return err
	}
	u := ui.New(r2, font, func() ui.Element {
		return ui.Div().P(12).Children(
			ui.Div().P(10).Bg(ui.RGBA{R: 0, G: 0, B: 0, A: 0.55}).Rounded(8).Children(
				ui.Text(caption(player)+"   (drag: orbit, wheel: zoom)", ui.RGB(0xe8eaf0))))
	})
	cam := eyeLevelCamera()

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
		err := frame(device, queue, r3, r2, u, model, place, cam, view, depthView, w, h)
		if err != nil {
			fmt.Fprintln(os.Stderr, "actor:", err)
			return false
		}
		// Present BEFORE releasing the swapchain texture (see skinned).
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

	r3, r2, model, player, place, font, err := setup(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		return err
	}
	player.Seek(player.Duration() * 0.35) // mid-clip pose
	u := ui.New(r2, font, func() ui.Element {
		return ui.Div().P(12).Children(
			ui.Div().P(10).Bg(ui.RGBA{R: 0, G: 0, B: 0, A: 0.55}).Rounded(8).Children(
				ui.Text(caption(player), ui.RGB(0xe8eaf0))))
	})
	cam := eyeLevelCamera()

	if err := frame(device, queue, r3, r2, u, model, place, cam, view, depthView, W, H); err != nil {
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
