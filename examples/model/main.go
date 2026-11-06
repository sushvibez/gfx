//go:build linux || windows || js

// Model loads and spins a glTF asset — real geometry, textures and node
// transforms through the whole no-cgo stack.
//
//	go run ./model                          # Duck.glb (scripts/fetch-assets.sh)
//	go run ./model -file path/to/asset.glb
//	go run ./model -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/internal/orbit"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/render/gltf"
)

func findAsset(file string) (string, error) {
	if file != "" {
		return file, nil
	}
	for _, up := range []string{".", "..", "../.."} {
		p, err := filepath.Abs(filepath.Join(up, "build", "assets", "Duck.glb"))
		if err == nil {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("no asset found; run scripts/fetch-assets.sh or pass -file")
}

func loadDoc(path string) (*gltf.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	return gltf.Parse(data, func(uri string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, uri))
	})
}

// newCamera frames the asset; drag to orbit, wheel to zoom.
func newCamera() *orbit.Camera {
	return &orbit.Camera{Target: core.V3(0, 0.8, 0), Distance: 3.1, Yaw: 0.9, Pitch: 0.35}
}

func frameCamera(w, h int, t float32) (core.Mat4, core.Vec3) {
	c := newCamera()
	c.Yaw += t * 0.5
	return c.ViewProj(w, h)
}

var lightDir = core.V3(-0.5, -1, -0.4)

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render one frame headlessly and write a PNG")
	file := flag.String("file", "", "glTF/GLB file (default: Duck.glb from build/assets)")
	flag.Parse()

	path, err := findAsset(*file)
	if err == nil {
		if *shot != "" {
			err = screenshot(path, *shot)
		} else {
			err = run(path, *frames)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "model:", err)
		os.Exit(1)
	}
}

func run(path string, maxFrames int) error {
	doc, err := loadDoc(path)
	if err != nil {
		return err
	}
	win, err := app.NewWindow(app.Options{Title: "graphics: " + filepath.Base(path), Width: 900, Height: 700})
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
	model, err := r.BuildModel(doc)
	if err != nil {
		return err
	}
	ground := r.CubeMesh()
	r.SetShadowArea(core.V3(0, 0.8, 0), 4)
	cam := newCamera()

	depth := device.CreateDepthTexture(w, h)
	depthView := depth.CreateView()
	_ = time.Now
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
		tex, ok := surface.CurrentTexture()
		if !ok {
			surface.Configure(device, w, h, format)
			return true
		}
		view := tex.CreateView()

		vp, eye := cam.ViewProj(w, h)
		r.Begin(vp, lightDir, eye)
		r.Draw(ground, core.Mat4Scale(core.V3(10, 0.1, 10)).Mul(core.Mat4Translate(core.V3(0, -0.5, 0))), render.RGBA{R: 0.45, G: 0.47, B: 0.52, A: 1})
		model.Draw(r, core.Mat4Identity())

		enc := device.CreateCommandEncoder()
		if err := r.Render(enc, view, depthView, gpu.Color{R: 0.05, G: 0.055, B: 0.08, A: 1}); err != nil {
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
