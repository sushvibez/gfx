//go:build linux || windows || js || android

// Cubes is the first 3D demo: a parent cube spinning at the origin with
// orbiting child cubes, composed through the scene tree (nodes as ECS
// handles, transforms in columns, one-pass world-matrix propagation) and
// rendered depth-tested with directional lighting.
//
//	go run ./cubes
//	go run ./cubes -frames 300
//	go run ./cubes -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/internal/orbit"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/scene"
)

type world struct {
	tree     *scene.Tree
	parent   scene.Node
	children []scene.Node
	moons    []scene.Node
}

func buildScene() *world {
	w := &world{tree: scene.NewTree()}
	w.parent = w.tree.Spawn(scene.Node{})
	for i := range 4 {
		c := w.tree.Spawn(w.parent)
		angle := float32(i) * math32.Pi / 2
		w.tree.SetLocal(c,
			core.V3(2.2*math32.Cos(angle), 0, 2.2*math32.Sin(angle)),
			core.QuatIdentity(), core.V3(0.5, 0.5, 0.5))
		w.children = append(w.children, c)
		m := w.tree.Spawn(c)
		w.tree.SetLocal(m, core.V3(0, 1.1, 0), core.QuatIdentity(), core.V3(0.4, 0.4, 0.4))
		w.moons = append(w.moons, m)
	}
	return w
}

// animate updates local transforms for time t; world matrices follow in
// Tree.Update.
func (w *world) animate(t float32) {
	w.tree.SetLocal(w.parent, core.V3(0, 0, 0),
		core.QuatAxisAngle(core.V3(0, 1, 0), t*0.8), core.V3(1, 1, 1))
	for i, c := range w.children {
		l := w.tree.Local(c)
		l.Rot = core.QuatAxisAngle(core.V3(1, 0, 0), t*2+float32(i))
	}
	w.tree.Update()
}

var palette = []render.RGBA{
	{R: 0.95, G: 0.55, B: 0.15, A: 1},
	{R: 0.25, G: 0.65, B: 0.95, A: 1},
	{R: 0.35, G: 0.85, B: 0.45, A: 1},
	{R: 0.9, G: 0.35, B: 0.55, A: 1},
}

func (w *world) draw(r *render.Renderer3D, cube *render.Mesh) {
	r.Draw(cube, core.Mat4Scale(core.V3(16, 0.1, 16)).Mul(core.Mat4Translate(core.V3(0, -22, 0))), render.RGBA{R: 0.4, G: 0.42, B: 0.47, A: 1})
	r.DrawPBR(cube, w.tree.WorldMatrix(w.parent), render.RGBA{R: 0.85, G: 0.85, B: 0.9, A: 1}, 0.9, 0.25, nil)
	for i, c := range w.children {
		r.Draw(cube, w.tree.WorldMatrix(c), palette[i%len(palette)])
		r.Draw(cube, w.tree.WorldMatrix(w.moons[i]), render.RGBA{R: 0.7, G: 0.7, B: 0.75, A: 1})
	}
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render one frame headlessly and write a PNG")
	flag.Parse()
	var err error
	if *shot != "" {
		err = screenshot(*shot)
	} else {
		err = run(*frames)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cubes:", err)
		os.Exit(1)
	}
}

func newCamera() *orbit.Camera {
	return &orbit.Camera{Target: core.V3(0, 0, 0), Distance: 8, Yaw: 0.65, Pitch: 0.4}
}

func frameCamera(w, h int) (core.Mat4, core.Vec3) {
	return newCamera().ViewProj(w, h)
}

var lightDir = core.V3(-0.5, -1, -0.4)

func run(maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "graphics: cubes", Width: 900, Height: 640})
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

	r, err := render.New3D(device, queue, format)
	if err != nil {
		return err
	}
	cube := r.CubeMesh()
	sc := buildScene()
	r.SetShadowArea(core.V3(0, 0, 0), 6)
	cam := newCamera()

	depth := device.CreateDepthTexture(w, h)
	depthView := depth.CreateView()

	start := time.Now()
	rendered := 0
	win.EachFrame(func() bool {
		cam.Handle(win.MouseEvents())
		if nw, nh := win.Size(); nw != w || nh != h {
			w, h = nw, nh
			surface.Configure(device, w, h, format)
			depthView.Release()
			depth.Release()
			depth = device.CreateDepthTexture(w, h)
			depthView = depth.CreateView()
		}
		sc.animate(float32(time.Since(start).Seconds()))

		tex, ok := surface.CurrentTexture()
		if !ok {
			surface.Configure(device, w, h, format)
			return true
		}
		view := tex.CreateView()

		vp, eye := cam.ViewProj(w, h)
		r.Begin(vp, lightDir, eye)
		sc.draw(r, cube)

		enc := device.CreateCommandEncoder()
		if err := r.Render(enc, view, depthView, gpu.Color{R: 0.04, G: 0.045, B: 0.07, A: 1}); err != nil {
			return false
		}
		queue.Submit(enc.Finish())
		surface.Present()
		view.Release()
		tex.Release()
		rendered++
		return maxFrames == 0 || rendered < maxFrames
	})
	fmt.Printf("rendered %d frames\n", rendered)
	return nil
}
