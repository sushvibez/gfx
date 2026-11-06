//go:build linux || windows || js || android

// Materials is the 3D material demo: a brick wall and a stone floor built
// from a Poly Haven PBR set (base colour + OpenGL normal map + packed ARM),
// drawn twice side by side — plain tiling on the left, stochastic hex
// tiling on the right — under three coloured point lights and height fog.
//
// The left half shows what tiling always does: the same stamp repeating on
// a grid your eye locks onto immediately. The right half is the same
// texture at the same scale through Material.AntiTile, which samples three
// hex cells with per-cell offset and rotation and blends them
// variance-preservingly, so the repetition disappears while the contrast
// does not.
//
//	cd examples && go run ./materials
//	go run ./materials -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"time"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/internal/orbit"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
)

var lightDir = core.V3(-0.4, -1, -0.55)

// ---- assets -----------------------------------------------------------------

// textureDir finds a Poly Haven texture set under build/textures. Override
// with GFX_TEXTURES to point at another directory of <name>_{diff,nor_gl,arm}
// maps.
func textureDir(set string) (string, error) {
	if p := os.Getenv("GFX_TEXTURES"); p != "" {
		return filepath.Join(p, set+"_1k"), nil
	}
	for _, base := range []string{"../build/textures", "build/textures", "../../build/textures"} {
		p := filepath.Join(base, set+"_1k")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("texture set %q not found (set GFX_TEXTURES)", set)
}

// loadJPEG decodes an image into tightly packed RGBA8.
func loadJPEG(path string) (int, int, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, nil, err
	}
	defer f.Close()
	src, err := jpeg.Decode(f)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("%s: %w", path, err)
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			dst.Set(x, y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return b.Dx(), b.Dy(), dst.Pix, nil
}

// loadSet uploads one Poly Haven material set as mipmapped textures. A
// missing map is simply nil — the material falls back to a neutral 1x1.
func loadSet(r *render.Renderer3D, set string) (albedo, normal, arm *render.Image3D, err error) {
	dir, err := textureDir(set)
	if err != nil {
		return nil, nil, nil, err
	}
	load := func(suffix string) (*render.Image3D, error) {
		p := filepath.Join(dir, set+"_"+suffix+"_1k.jpg")
		w, h, pix, err := loadJPEG(p)
		if err != nil {
			return nil, err
		}
		return r.NewImage3DMipmapped(w, h, pix), nil
	}
	if albedo, err = load("diff"); err != nil {
		return nil, nil, nil, err
	}
	if normal, err = load("nor_gl"); err != nil {
		return nil, nil, nil, err
	}
	if arm, err = load("arm"); err != nil {
		return nil, nil, nil, err
	}
	return albedo, normal, arm, nil
}

// ---- scene ------------------------------------------------------------------

// quad builds a w x h rectangle with UVs running 0..1, in the XZ plane
// (floor, normal +Y) or the XY plane (wall, normal +Z), centered on origin.
func quad(r *render.Renderer3D, w, h float32, floor bool) *render.Mesh {
	hw, hh := w/2, h/2
	var verts []render.Vertex3D
	if floor {
		n := core.V3(0, 1, 0)
		verts = []render.Vertex3D{
			{Pos: core.V3(-hw, 0, hh), Normal: n, U: 0, V: 0},
			{Pos: core.V3(hw, 0, hh), Normal: n, U: 1, V: 0},
			{Pos: core.V3(hw, 0, -hh), Normal: n, U: 1, V: 1},
			{Pos: core.V3(-hw, 0, -hh), Normal: n, U: 0, V: 1},
		}
	} else {
		n := core.V3(0, 0, 1)
		verts = []render.Vertex3D{
			{Pos: core.V3(-hw, -hh, 0), Normal: n, U: 0, V: 1},
			{Pos: core.V3(hw, -hh, 0), Normal: n, U: 1, V: 1},
			{Pos: core.V3(hw, hh, 0), Normal: n, U: 1, V: 0},
			{Pos: core.V3(-hw, hh, 0), Normal: n, U: 0, V: 0},
		}
	}
	return r.NewMesh(verts, []uint32{0, 1, 2, 0, 2, 3})
}

type scene struct {
	wall, floor        *render.Mesh
	plainWall, hexWall *render.Material
	plainFloor         *render.Material
	hexFloor           *render.Material
	post               *render.Mesh
	postMat            *render.Material
	blockMat           *render.Material
	lights             []render.PointLight
}

const halfWidth = 9 // each half of the diptych is 9 units wide

func buildScene(r *render.Renderer3D) (*scene, error) {
	brickA, brickN, brickR, err := loadSet(r, "red_brick")
	if err != nil {
		return nil, err
	}
	stoneA, stoneN, stoneR, err := loadSet(r, "rocky_terrain")
	if err != nil {
		// The floor set is optional; reuse the brick on failure.
		stoneA, stoneN, stoneR = brickA, brickN, brickR
	}

	s := &scene{
		wall:  quad(r, halfWidth, 6, false),
		floor: quad(r, halfWidth, 14, true),
		post:  r.CubeMesh(),
	}
	s.plainWall = &render.Material{Albedo: brickA, Normal: brickN, ARM: brickR, UVScale: 2.5, NormalScale: 1.4}
	s.hexWall = &render.Material{Albedo: brickA, Normal: brickN, ARM: brickR, UVScale: 2.5, NormalScale: 1.4, AntiTile: true, AntiTileRotation: 0.04}
	s.plainFloor = &render.Material{Albedo: stoneA, Normal: stoneN, ARM: stoneR, UVScale: 5, NormalScale: 1.2}
	s.hexFloor = &render.Material{Albedo: stoneA, Normal: stoneN, ARM: stoneR, UVScale: 5, NormalScale: 1.2, AntiTile: true}
	// The instanced blocks carry no UVs at all, so they take the stone set
	// triplanar — projected from world space on the three axis planes.
	s.blockMat = &render.Material{Albedo: stoneA, Normal: stoneN, ARM: stoneR,
		UVScale: 0.7, NormalScale: 1.2, Triplanar: true, AntiTile: true}
	// Small emissive posts marking where each coloured lamp hangs.
	s.postMat = &render.Material{Color: render.RGBA{R: 1, G: 1, B: 1, A: 1}, Emission: 2.5, Unlit: true}

	s.lights = []render.PointLight{
		{Pos: core.V3(-5, 2.2, -5.0), Color: render.RGBA{R: 1, G: 0.22, B: 0.12, A: 1}, Energy: 55, Range: 8},
		{Pos: core.V3(0, 2.6, -4.2), Color: render.RGBA{R: 0.22, G: 1, B: 0.40, A: 1}, Energy: 45, Range: 8},
		{Pos: core.V3(5, 2.2, -5.0), Color: render.RGBA{R: 0.25, G: 0.45, B: 1, A: 1}, Energy: 60, Range: 9},
	}
	return s, nil
}

func frameSettings() render.FrameSettings {
	fs := render.DefaultFrameSettings()
	fs.Ambient = render.RGBA{R: 0.05, G: 0.06, B: 0.09, A: 1}
	fs.Exposure = 1.05
	fs.Tonemap = 2 // ACES
	fs.SkyTop = render.RGBA{R: 0.10, G: 0.16, B: 0.30, A: 1}
	fs.SkyHorizon = render.RGBA{R: 0.42, G: 0.45, B: 0.55, A: 1}
	fs.Fog.Enabled = true
	fs.Fog.Color = render.RGBA{R: 0.30, G: 0.33, B: 0.42, A: 1}
	fs.Fog.Density = 0.014
	fs.Fog.Height = 1.5
	fs.Fog.HeightDensity = 0.09
	fs.Fog.AerialPerspective = 0.45
	return fs
}

// draw queues the diptych: the left half plain-tiled, the right half hex
// tiled, floor and wall alike.
func (s *scene) draw(r *render.Renderer3D) {
	const z = -7
	left := -halfWidth / 2
	right := float32(halfWidth) / 2

	r.DrawMaterial(s.wall, core.Mat4Translate(core.V3(float32(left), 3, z)), s.plainWall)
	r.DrawMaterial(s.wall, core.Mat4Translate(core.V3(right, 3, z)), s.hexWall)
	r.DrawMaterial(s.floor, core.Mat4Translate(core.V3(float32(left), 0, z+7)), s.plainFloor)
	r.DrawMaterial(s.floor, core.Mat4Translate(core.V3(right, 0, z+7)), s.hexFloor)

	// The lamps themselves, as small emissive cubes.
	for _, l := range s.lights {
		m := core.Mat4Translate(l.Pos).Mul(core.Mat4Scale(core.V3(0.16, 0.16, 0.16)))
		s.postMat.Color = render.RGBA{R: l.Color.R, G: l.Color.G, B: l.Color.B, A: 1}
		r.DrawMaterial(s.post, m, s.postMat)
	}

	// A row of instanced blocks along the seam: one draw call, full
	// lighting — the foliage/debris path.
	var blocks []core.Mat4
	for i := range 40 {
		x := -8.5 + float32(i%20)*0.9
		zz := z + 8.5 + float32(i/20)*1.1
		blocks = append(blocks, core.Mat4Translate(core.V3(x, 0.18, zz)).
			Mul(core.Mat4Scale(core.V3(0.34, 0.36, 0.34))))
	}
	r.DrawInstanced(s.post, blocks, s.blockMat)
}

func newCamera() *orbit.Camera {
	return &orbit.Camera{Target: core.V3(0, 1.9, -3.2), Distance: 9.5, Yaw: 0.02, Pitch: 0.10}
}

func setup(r *render.Renderer3D) (*scene, error) {
	s, err := buildScene(r)
	if err != nil {
		return nil, err
	}
	r.SetFrameSettings(frameSettings())
	r.SetPointLights(s.lights)
	r.SetShadowArea(core.V3(0, 1, -4), 12)
	return s, nil
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
		fmt.Fprintln(os.Stderr, "materials:", err)
		os.Exit(1)
	}
}

func run(maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "graphics: materials", Width: 1100, Height: 700})
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
	s, err := setup(r)
	if err != nil {
		return err
	}
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
		// Drift the lamps so the coloured pools move over the brick.
		t := float32(time.Since(start).Seconds())
		lights := append([]render.PointLight(nil), s.lights...)
		for i := range lights {
			lights[i].Pos.X += 1.6 * math32.Sin(t*0.5+float32(i)*2.1)
		}
		r.SetPointLights(lights)

		tex, ok := surface.CurrentTexture()
		if !ok {
			surface.Configure(device, w, h, format)
			return true
		}
		view := tex.CreateView()
		vp, eye := cam.ViewProj(w, h)
		r.Begin(vp, lightDir, eye)
		s.draw(r)
		enc := device.CreateCommandEncoder()
		if err := r.Render(enc, view, depthView, clearColor()); err != nil {
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

// clearColor matches the fog/sky so the background and the fogged distance
// meet without a seam.
func clearColor() gpu.Color {
	fs := frameSettings()
	return gpu.Color{R: float64(fs.Fog.Color.R), G: float64(fs.Fog.Color.G), B: float64(fs.Fog.Color.B), A: 1}
}
