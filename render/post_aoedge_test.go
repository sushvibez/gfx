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

// aoEdgeScene renders a red pillar floating well in front of the crease
// where a floor meets a back wall, under flat ambient light, through Post
// with the given AO strength. The crease is occluded; the pillar, clear of
// everything, is not, so whatever AO changes on it bled across its
// silhouette from the crease behind.
func aoEdgeScene(t *testing.T, w, h int, ao float32) []byte {
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

	eye := core.V3(0, 3, 8)
	proj := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.1, 100)
	vp := proj.Mul(core.Mat4LookAt(eye, core.V3(0, 0.6, -4), core.V3(0, 1, 0)))
	post.Projection = proj
	grey := &Material{Color: RGBA{R: 0.7, G: 0.7, B: 0.7, A: 1}, Roughness: 1}
	red := &Material{Color: RGBA{R: 0.9, G: 0.1, B: 0.1, A: 1}, Roughness: 1}
	r3.KeepDepth = true
	r3.Begin(vp, core.V3(0, -1, 0), eye)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(40, 1, 40))), grey)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 5, -4.5)).Mul(core.Mat4Scale(core.V3(40, 10, 1))), grey)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 2.4, 3)).Mul(core.Mat4Scale(core.V3(0.5, 3.2, 0.5))), red)
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
		name := "aoedge_off.png"
		if ao > 0 {
			name = "aoedge_on.png"
		}
		if f, err := os.Create(filepath.Join(dir, name)); err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}
	return pix
}

// TestPostAOSilhouette: the half-resolution occlusion of the crease behind
// a pillar does not bleed onto the pillar's edge (the composite upsamples
// it by depth), while the crease itself stays occluded.
func TestPostAOSilhouette(t *testing.T) {
	const w, h = 480, 320
	off, on := aoEdgeScene(t, w, h, 0), aoEdgeScene(t, w, h, 1)
	red := func(pix []byte, x, y int) bool {
		i := (y*w + x) * 4
		return int(pix[i]) > int(pix[i+1])+40
	}
	// The pillar's pixels with a red neighbour on both sides and above and
	// below (its silhouette's own anti-aliased pixel is mixed in colour):
	// the largest AO change among them, and the largest anywhere.
	worst, wx, wy, crease := 0.0, 0, 0, 0.0
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			d := lumAt(off, w, x, y) - lumAt(on, w, x, y)
			crease = max(crease, d)
			if !red(off, x, y) || !red(off, x-1, y) || !red(off, x+1, y) || !red(off, x, y-1) || !red(off, x, y+1) {
				continue
			}
			if d > worst {
				worst, wx, wy = d, x, y
			}
		}
	}
	if crease < 12 {
		t.Fatalf("AO darkened nothing: the largest drop is %v", crease)
	}
	if worst > 4 {
		t.Fatalf("the pillar, clear of everything, darkened by %v at (%d, %d): the crease's occlusion bled across its silhouette", worst, wx, wy)
	}
}
