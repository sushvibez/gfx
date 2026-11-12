//go:build !js

package render

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// postScene renders frames of one lamp (an unlit cube of the given
// radiance) over a dark lit floor through Post and returns the last
// frame's pixels (RGBA8, w×h). gain scales the whole scene's radiance.
// postSceneLampX moves postScene's lamp off the picture's centre.
var postSceneLampX float32

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
		r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(postSceneLampX, 1, 0)).Mul(core.Mat4Scale(core.V3(0.5, 0.5, 0.5))), lamp)
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

// atmosScene renders a long slab (10 wide, 44 deep along the view) floating in empty air under an overhead sun,
// seen level from the side, through Post with the given atmosphere.
func atmosScene(t *testing.T, w, h int, atm Atmosphere, yaw float32) []byte {
	t.Helper()
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = TonemapHDR
	r3.SetFrameSettings(fs)
	r3.SetShadowArea(core.V3(0, 5, 0), 30)
	r3.KeepDepth = true
	post, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer post.Release()
	s := &post.Settings
	s.AutoExposure, s.Bloom, s.Grain, s.ChromaticAberration, s.Vignette, s.Sharpen, s.AO = 0, 0, 0, 0, 0, 0, 0
	s.Lift = [3]float32{} // black stays black, so empty air reads zero
	s.FXAA = false
	post.Resize(w, h)
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	tview := target.CreateView()
	defer func() { tview.Release(); target.Release() }()

	eye := core.V3(0, 5, 40)
	at := eye.Add(core.V3(float32(math.Sin(float64(yaw))), 0, -float32(math.Cos(float64(yaw)))))
	proj := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.1, 300)
	view := core.Mat4LookAt(eye, at, core.V3(0, 1, 0))
	post.Projection, post.View, post.Atmosphere = proj, view, atm
	grey := &Material{Color: RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1}, Roughness: 1}
	r3.SetSpotLights(atmosSceneSpots)
	r3.Begin(proj.Mul(view), atm.SunDir, eye)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 10, 0)).Mul(core.Mat4Scale(core.V3(10, 0.3, 44))), grey)
	post.UseShadow(r3)
	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, post.SceneView(), post.DepthView(), gpu.Color{A: 1}); err != nil {
		t.Fatal(err)
	}
	post.Render(enc, tview)
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("POST_DUMP"); dir != "" {
		img := &image.RGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
		if f, err := os.Create(filepath.Join(dir, "atmos.png")); err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}
	return pix
}

// Light shafts: air in a slab's shadow scatters less sunlight than the lit
// air beside it, so the shadow shows as a darker column in the haze.
func TestPostLightShafts(t *testing.T) {
	const w, h = 480, 300
	atm := Atmosphere{Shafts: 0.02, Falloff: 1000, MaxDistance: 140, SunDir: core.V3(0, -1, 0), Sun: [3]float32{3, 3, 3}}
	pix := atmosScene(t, w, h, atm, 0)
	// Level with the eye: under the slab (the image's centre column) and
	// well to its side.
	y := h/2 + 20
	under, beside := lumAt(pix, w, w/2, y), lumAt(pix, w, w/2+170, y)
	if beside < 20 {
		t.Fatalf("lit air reads %v: no in-scattered light", beside)
	}
	if under > beside*0.9 {
		t.Fatalf("no shaft: the air under the slab reads %v, beside it %v", under, beside)
	}
	// Without shafts the empty air is black.
	off := atm
	off.Shafts = 0
	if l := lumAt(atmosScene(t, w, h, off, 0), w, w/2+170, y); l > 3 {
		t.Fatalf("with Shafts 0 the air reads %v, want the clear colour", l)
	}
}

// The sky: brighter toward the sun than away from it, and the zenith's
// colour overhead where the horizon's is at eye level.
func TestPostSky(t *testing.T) {
	const w, h = 480, 300
	// A low sun in front of the camera (the light travels toward +z).
	atm := Atmosphere{Sky: true, Zenith: [3]float32{0.05, 0.10, 0.40}, Horizon: [3]float32{0.6, 0.4, 0.2},
		SunDir: core.V3(0, -0.2, 1), Sun: [3]float32{4, 3, 2}}
	toward := atmosScene(t, w, h, atm, 0)
	away := atmosScene(t, w, h, atm, math.Pi)
	// Above eye level and to the side, clear of the slab overhead.
	x, y := w/2+170, h/2-30
	if a, b := lumAt(toward, w, x, y), lumAt(away, w, x, y); a < b+25 {
		t.Fatalf("the sky toward the sun reads %v, away from it %v", a, b)
	}
	// Away from the sun: blue at the top of the frame, warm at eye level.
	top, eyeLevel := (5*w+x)*4, ((h/2-4)*w+x)*4
	if !(away[top+2] > away[top]) {
		t.Fatalf("the sky overhead is not blue: rgb %v", away[top:top+3])
	}
	if !(away[eyeLevel] > away[eyeLevel+2]) {
		t.Fatalf("the horizon is not warm: rgb %v", away[eyeLevel:eyeLevel+3])
	}
}

// atmosSceneSpots are the lamps atmosScene lights its frame with.
var atmosSceneSpots []SpotLight

// Lamps light the air: a downward spot shows its cone as a brighter column
// in the haze, and not at all with Lamps at 0.
func TestPostLampBeam(t *testing.T) {
	const w, h = 480, 300
	atmosSceneSpots = []SpotLight{{Pos: core.V3(0, 9.5, 0), Dir: core.V3(0, -1, 0),
		Color: RGBA{R: 1, G: 0.7, B: 0.3, A: 1}, Energy: 400, Range: 40, Angle: 0.3}}
	defer func() { atmosSceneSpots = nil }()
	// Night: no sun to speak of, no ambient; only the lamp.
	atm := Atmosphere{Shafts: 0.02, Falloff: 1000, MaxDistance: 140, SunDir: core.V3(0, -1, 0),
		Sun: [3]float32{0.0001, 0.0001, 0.0001}, Lamps: 1}
	pix := atmosScene(t, w, h, atm, 0)
	y := h/2 + 20
	in, out := lumAt(pix, w, w/2, y), lumAt(pix, w, w/2+170, y)
	if in < out+12 {
		t.Fatalf("no beam: the air in the lamp's cone reads %v, outside it %v", in, out)
	}
	off := atm
	off.Lamps = 0
	if l := lumAt(atmosScene(t, w, h, off, 0), w, w/2, y); l > out+4 {
		t.Fatalf("with Lamps 0 the cone's air still reads %v (dark air reads %v)", l, out)
	}
}

// A shadow-casting lamp's beam is cut by what stands under it: with the
// lamp above the slab, the air below the slab holds a beam only when the
// lamp casts no shadow.
func TestPostLampBeamShadow(t *testing.T) {
	const w, h = 480, 300
	defer func() { atmosSceneSpots = nil }()
	atm := Atmosphere{Shafts: 0.02, Falloff: 1000, MaxDistance: 140, SunDir: core.V3(0, -1, 0),
		Sun: [3]float32{0.0001, 0.0001, 0.0001}, Lamps: 1}
	beam := func(shadow bool) float64 {
		atmosSceneSpots = []SpotLight{{Pos: core.V3(0, 14, 0), Dir: core.V3(0, -1, 0),
			Color: RGBA{R: 1, G: 0.7, B: 0.3, A: 1}, Energy: 900, Range: 40, Angle: 0.3, Shadow: shadow}}
		pix := atmosScene(t, w, h, atm, 0)
		y := h/2 + 20
		return lumAt(pix, w, w/2, y) - lumAt(pix, w, w/2+170, y)
	}
	through, cut := beam(false), beam(true)
	if through < 10 {
		t.Fatalf("no beam below the slab without shadows (%v over the air beside it)", through)
	}
	if cut > through*0.3 {
		t.Fatalf("the slab does not cut the beam: %v with shadows, %v without", cut, through)
	}
}

// The lens: a bright lamp left of centre throws a ghost to the mirrored
// place right of centre with LensFlare, and a streak out to its side with
// LensStreak; neither is there without them.
func TestPostLens(t *testing.T) {
	const w, h = 480, 300
	postSceneLampX = -1.6
	defer func() { postSceneLampX = 0 }()
	shot := func(flare, streak float32) []byte {
		return postScene(t, w, h, 60, 1, func(s *PostSettings) {
			s.AutoExposure, s.Bloom, s.AO = 0, 0.1, 0
			s.LensFlare, s.LensStreak = flare, streak
		}, 2)
	}
	plain, flare, streak := shot(0, 0), shot(1, 0), shot(0, 1)
	// Find the lamp: the brightest pixel of the left half.
	lx, ly := 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w/2; x++ {
			if lumAt(plain, w, x, y) > lumAt(plain, w, lx, ly) {
				lx, ly = x, y
			}
		}
	}
	// Its first ghost is the lamp mirrored through the centre.
	gx, gy := w-1-lx, h-1-ly
	if a, b := lumAt(flare, w, gx, gy), lumAt(plain, w, gx, gy); a < b+8 {
		t.Fatalf("no ghost opposite the lamp (%d,%d → %d,%d): %v with the flare, %v without", lx, ly, gx, gy, a, b)
	}
	// The streak: level with the lamp, well to its side, and only there.
	sx := lx + 80
	if a, b := lumAt(streak, w, sx, ly), lumAt(plain, w, sx, ly); a < b+8 {
		t.Fatalf("no streak beside the lamp: %v with it, %v without", a, b)
	}
	if a, b := lumAt(streak, w, sx, ly+70), lumAt(plain, w, sx, ly+70); a > b+6 {
		t.Fatalf("the streak is not a streak: %v below it, %v without", a, b)
	}
}
