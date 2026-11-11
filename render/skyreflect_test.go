//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// floorScene renders a dark floor seen at a grazing angle under a bright
// sky setting, and returns the frame.
func floorScene(t *testing.T, mat *Material, tune func(*FrameSettings)) []byte {
	t.Helper()
	const w, h = 256, 160
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = 0
	fs.Sun = RGBA{R: 0.001, G: 0.001, B: 0.001, A: 1}
	fs.Ambient = RGBA{R: 0.02, G: 0.02, B: 0.02, A: 1}
	fs.SkyTop = RGBA{R: 0.2, G: 0.4, B: 0.9, A: 1}
	fs.SkyHorizon = RGBA{R: 0.9, G: 0.8, B: 0.6, A: 1}
	if tune != nil {
		tune(&fs)
	}
	r3.SetFrameSettings(fs)
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	depth := dev.CreateDepthTexture(w, h)
	dview := depth.CreateView()
	defer func() { dview.Release(); depth.Release(); view.Release(); target.Release() }()
	eye := core.V3(0, 1.2, 10)
	vp := core.Mat4Perspective(0.8, float32(w)/float32(h), 0.1, 500).Mul(core.Mat4LookAt(eye, core.V3(0, 0, -20), core.V3(0, 1, 0)))
	r3.Begin(vp, core.V3(0, -1, 0), eye)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(400, 1, 400))), mat)
	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, view, dview, gpu.Color{A: 1}); err != nil {
		t.Fatal(err)
	}
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return pix
}

func meanLum(pix []byte, w, x0, y0, x1, y1 int) float64 {
	var s float64
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			s += lumAt(pix, w, x, y)
			n++
		}
	}
	return s / float64(n)
}

// SkyReflection: a glossy floor at a grazing angle mirrors the bright
// horizon; a matte one barely does; and without the setting nothing
// changes.
func TestSkyReflection(t *testing.T) {
	const w, h = 256, 160
	dark := RGBA{R: 0.05, G: 0.05, B: 0.05, A: 1}
	gloss := &Material{Color: dark, Roughness: 0.08}
	matte := &Material{Color: dark, Roughness: 1}
	on := func(fs *FrameSettings) { fs.SkyReflection = 1 }
	// The far floor, just under the horizon: the grazing part.
	far := func(pix []byte) float64 { return meanLum(pix, w, 40, h/2+6, w-40, h/2+16) }
	g0, g1 := far(floorScene(t, gloss, nil)), far(floorScene(t, gloss, on))
	m0, m1 := far(floorScene(t, matte, nil)), far(floorScene(t, matte, on))
	if g1 < g0+40 {
		t.Fatalf("a glossy floor reads %v without and %v with the sky reflected", g0, g1)
	}
	if (m1 - m0) > (g1-g0)*0.5 {
		t.Fatalf("a matte floor gained %v from the sky, a glossy one %v", m1-m0, g1-g0)
	}
}

// Material.Water: the surface moves with FrameSettings.Time, and holds
// still when time does.
func TestWaterMoves(t *testing.T) {
	const w, h = 256, 160
	// A wave map: a diagonal ripple.
	const n = 64
	pix := make([]byte, n*n*4)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			i := (y*n + x) * 4
			v := byte(56)
			if (x+y)/8%2 == 0 {
				v = 200
			}
			pix[i], pix[i+1], pix[i+2], pix[i+3] = v, 255-v, 220, 255
		}
	}
	frame := func(time float32) []byte {
		return floorSceneWater(t, pix, n, time)
	}
	a, b, c := frame(1), frame(1), frame(9)
	same, moved := 0, 0
	for i := range a {
		if a[i] != b[i] {
			same++
		}
		if a[i] != c[i] {
			moved++
		}
	}
	if same != 0 {
		t.Fatalf("the water changed with time standing still (%d bytes)", same)
	}
	if moved < w*h/20 {
		t.Fatalf("the water did not move with time (%d bytes changed)", moved)
	}
}

// floorSceneWater is floorScene with a water material built on r3's own
// device (the wave map must be uploaded by the renderer that draws it).
func floorSceneWater(t *testing.T, wave []byte, n int, time float32) []byte {
	t.Helper()
	const w, h = 256, 160
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = 0
	fs.SkyTop = RGBA{R: 0.2, G: 0.4, B: 0.9, A: 1}
	fs.SkyHorizon = RGBA{R: 0.9, G: 0.8, B: 0.6, A: 1}
	fs.SkyReflection, fs.Time = 1, time
	r3.SetFrameSettings(fs)
	img := r3.NewImage3DMipmapped(n, n, wave)
	defer img.Release()
	mat := &Material{Normal: img, Water: true, Color: RGBA{R: 0.02, G: 0.05, B: 0.06, A: 1}, Roughness: 0.07, UVScale: 0.2}
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	depth := dev.CreateDepthTexture(w, h)
	dview := depth.CreateView()
	defer func() { dview.Release(); depth.Release(); view.Release(); target.Release() }()
	eye := core.V3(0, 2, 10)
	vp := core.Mat4Perspective(0.8, float32(w)/float32(h), 0.1, 500).Mul(core.Mat4LookAt(eye, core.V3(0, 0, -10), core.V3(0, 1, 0)))
	r3.Begin(vp, core.V3(0.2, -0.4, -1), eye)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(400, 1, 400))), mat)
	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, view, dview, gpu.Color{A: 1}); err != nil {
		t.Fatal(err)
	}
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return pix
}
