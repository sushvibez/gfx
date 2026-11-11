//go:build !js

package render

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// postScene renders frames of one lamp (an unlit cube of the given
// radiance) over a dark lit floor through Post and returns the last
// frame's pixels (RGBA8, w×h). gain scales the whole scene's radiance.
func postScene(t *testing.T, w, h int, radiance, gain float32, tune func(*PostSettings), frames int) []byte {
	t.Helper()
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = TonemapHDR
	fs.Exposure = gain // in HDR mode this scales the radiance written
	r3.SetFrameSettings(fs)
	post, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer post.Release()
	post.Settings.Grain, post.Settings.ChromaticAberration, post.Settings.Vignette = 0, 0, 0
	if tune != nil {
		tune(&post.Settings)
	}
	post.Resize(w, h)
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	defer func() { view.Release(); target.Release() }()

	eye := core.V3(0, 1.2, 6)
	vp := core.Mat4Perspective(1.0, float32(w)/float32(h), 0.1, 100).Mul(core.Mat4LookAt(eye, core.V3(0, 1, 0), core.V3(0, 1, 0)))
	lamp := &Material{Color: RGBA{R: 1, G: 0.8, B: 0.5, A: 1}, Unlit: true, Emission: radiance}
	floor := &Material{Color: RGBA{R: 0.3, G: 0.3, B: 0.3, A: 1}, Roughness: 0.9}
	for i := 0; i < frames; i++ {
		r3.Begin(vp, core.V3(-0.4, -1, -0.3), eye)
		r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 1, 0)).Mul(core.Mat4Scale(core.V3(0.5, 0.5, 0.5))), lamp)
		r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(30, 1, 30))), floor)
		enc := dev.CreateCommandEncoder()
		if err := r3.Render(enc, post.SceneView(), post.DepthView(), gpu.Color{R: 0.01, G: 0.012, B: 0.02, A: 1}); err != nil {
			t.Fatal(err)
		}
		post.Render(enc, view)
		q.Submit(enc.Finish())
	}
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return pix
}

func lumAt(pix []byte, w, x, y int) float64 {
	i := (y*w + x) * 4
	return 0.2126*float64(pix[i]) + 0.7152*float64(pix[i+1]) + 0.0722*float64(pix[i+2])
}

// Post displays an HDR scene: the shaders compile and run on a real
// device, the lamp reads bright and the sky dark, and bloom spreads the
// lamp's light past its silhouette.
func TestPostDisplaysHDR(t *testing.T) {
	const w, h = 320, 200
	fixed := func(s *PostSettings) { s.AutoExposure = 0 }
	sharp := postScene(t, w, h, 20, 1, func(s *PostSettings) { fixed(s); s.Bloom = 0 }, 3)
	glow := postScene(t, w, h, 20, 1, func(s *PostSettings) { fixed(s); s.Bloom = 0.25 }, 3)
	cx, cy := w/2, h/2
	if l := lumAt(sharp, w, cx, cy); l < 200 {
		t.Fatalf("the lamp's centre reads %v, want bright", l)
	}
	if l := lumAt(sharp, w, 10, 10); l > 60 {
		t.Fatalf("the sky reads %v, want dark", l)
	}
	// Beside the lamp, over the dark sky: only bloom lights it.
	hx, hy := cx+18, cy-14
	a, b := lumAt(sharp, w, hx, hy), lumAt(glow, w, hx, hy)
	if b < a+6 {
		t.Fatalf("bloom did not spread: beside the lamp %v without, %v with", a, b)
	}
}

// Auto exposure adapts: the same scene at eight times the radiance is
// displayed much brighter at a fixed exposure, and about the same once
// the eye has adapted.
func TestPostAutoExposure(t *testing.T) {
	const w, h = 320, 200
	mean := func(pix []byte) float64 {
		var s float64
		for y := 0; y < h; y += 4 {
			for x := 0; x < w; x += 4 {
				s += lumAt(pix, w, x, y)
			}
		}
		return s / float64((w/4)*(h/4))
	}
	run := func(gain, auto float32) float64 {
		return mean(postScene(t, w, h, 3, gain, func(s *PostSettings) {
			s.AutoExposure, s.Bloom = auto, 0
			s.MinExposure, s.MaxExposure = 0.01, 100
		}, 8))
	}
	fixed1, fixed8 := run(0.25, 0), run(2, 0)
	if fixed8 < fixed1*1.6 {
		t.Fatalf("fixed exposure: 8× the radiance reads %v against %v", fixed8, fixed1)
	}
	auto1, auto8 := run(0.25, 1), run(2, 1)
	if r := auto8 / auto1; r < 0.8 || r > 1.25 {
		t.Fatalf("auto exposure: 8× the radiance reads %v against %v (ratio %.2f), want about the same", auto8, auto1, r)
	}
}

// POST_DUMP=dir writes the test scene with and without bloom.
func TestPostDump(t *testing.T) {
	dir := os.Getenv("POST_DUMP")
	if dir == "" {
		t.Skip("POST_DUMP not set")
	}
	const w, h = 640, 400
	for name, bloom := range map[string]float32{"post_sharp.png": 0, "post_glow.png": 0.25} {
		pix := postScene(t, w, h, 20, 1, func(s *PostSettings) { s.AutoExposure = 0; s.Bloom = bloom }, 3)
		img := &image.RGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, img)
		f.Close()
	}
}

// aoScene renders a box standing on a floor under flat ambient light (no
// sun to speak of), through Post with the given AO strength.
func aoScene(t *testing.T, w, h int, ao float32) []byte {
	t.Helper()
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = TonemapHDR
	fs.Sun = RGBA{R: 0.001, G: 0.001, B: 0.001, A: 1}
	fs.Ambient = RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1}
	r3.SetFrameSettings(fs)
	post, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer post.Release()
	s := &post.Settings
	*s = DefaultPostSettings()
	s.AutoExposure, s.Bloom, s.Grain, s.ChromaticAberration, s.Vignette, s.Sharpen = 0, 0, 0, 0, 0, 0
	s.FXAA = false
	s.AO, s.AORadius = ao, 1.5
	post.Resize(w, h)
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	defer func() { view.Release(); target.Release() }()

	eye := core.V3(0, 5, 9)
	proj := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.1, 100)
	vp := proj.Mul(core.Mat4LookAt(eye, core.V3(0, 0.5, 0), core.V3(0, 1, 0)))
	post.Projection = proj
	grey := &Material{Color: RGBA{R: 0.7, G: 0.7, B: 0.7, A: 1}, Roughness: 1}
	r3.KeepDepth = true // Post reads the depth buffer
	r3.Begin(vp, core.V3(0, -1, 0), eye)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(40, 1, 40))), grey)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 1.5, 0)).Mul(core.Mat4Scale(core.V3(3, 3, 3))), grey)
	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, post.SceneView(), post.DepthView(), gpu.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}); err != nil {
		t.Fatal(err)
	}
	post.Render(enc, view)
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("POST_DUMP"); dir != "" {
		img := &image.RGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
		name := "ao_off.png"
		if ao > 0 {
			name = "ao_on.png"
		}
		if f, err := os.Create(filepath.Join(dir, name)); err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}
	return pix
}

// Ambient occlusion darkens the crease where a box meets the floor and
// leaves open floor alone.
func TestPostAmbientOcclusion(t *testing.T) {
	const w, h = 480, 320
	off, on := aoScene(t, w, h, 0), aoScene(t, w, h, 1)
	// The darkest AO change in the image should be on the floor by the
	// box's foot; find the largest drop and where open floor stays put.
	maxDrop, mx, my := 0.0, 0, 0
	for y := 0; y < h; y += 2 {
		for x := 0; x < w; x += 2 {
			if d := lumAt(off, w, x, y) - lumAt(on, w, x, y); d > maxDrop {
				maxDrop, mx, my = d, x, y
			}
		}
	}
	if maxDrop < 12 {
		t.Fatalf("AO darkened nothing: the largest drop is %v", maxDrop)
	}
	// Open floor, near the bottom corners of the image (close to the
	// camera, far from the box).
	for _, p := range [][2]int{{30, h - 20}, {w - 30, h - 20}} {
		if d := lumAt(off, w, p[0], p[1]) - lumAt(on, w, p[0], p[1]); d > 4 || d < -4 {
			t.Fatalf("open floor at %v changed by %v with AO", p, d)
		}
	}
	// Open floor seen at a grazing angle must stay even row to row: a depth
	// lookup that lands on texel corners bands it.
	for y := h * 7 / 10; y < h-4; y++ {
		if d := lumAt(off, w, 40, y) - lumAt(on, w, 40, y); d > 4 || d < -4 {
			t.Fatalf("open floor banded: row %d changed by %v with AO", y, d)
		}
	}
	// The drop is around the box (the middle band of the image), not at
	// the horizon or the frame's edge.
	if mx < w/5 || mx > 4*w/5 || my < h/4 {
		t.Fatalf("the darkest occlusion is at (%d, %d), not by the box", mx, my)
	}
}
