package render

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render/particles"
)

// puffPixels renders a bright floor with a dark puff of smoke between it
// and the eye (none, drawn in the main pass, or soft over the finished
// picture) and returns the pixels.
func puffPixels(t *testing.T, mode int) []byte {
	t.Helper()
	const w, h = 160, 120
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = 0
	fs.Ambient = RGBA{R: 0.8, G: 0.8, B: 0.8, A: 1}
	r3.SetFrameSettings(fs)
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	depth := dev.CreateTexture2D(w, h, gpu.FormatDepth32Float, gpu.TextureRenderAttachment|gpu.TextureBinding)
	dview := depth.CreateView()
	defer func() { dview.Release(); depth.Release(); view.Release(); target.Release() }()
	eye := core.V3(0, 3, 10)
	vp := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.5, 100).Mul(core.Mat4LookAt(eye, core.V3(0, 0, 0), core.V3(0, 1, 0)))
	if mode > 0 {
		r3.SetParticles([]particles.Sprite{{Pos: particles.Vec3{X: 0, Y: 1.5, Z: 5}, Radius: 1.2,
			Color: [3]float32{0.02, 0.02, 0.02}, Alpha: 1, Seed: 0.3}})
	}
	r3.Begin(vp, core.V3(0.3, -1, 0.2), eye)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(60, 1, 60))),
		&Material{Color: RGBA{R: 0.9, G: 0.9, B: 0.9, A: 1}, Roughness: 1})
	enc := dev.CreateCommandEncoder()
	r3.DeferRain, r3.KeepDepth = mode == 2, mode == 2
	if err := r3.Render(enc, view, dview, gpu.Color{A: 1}); err != nil {
		t.Fatal(err)
	}
	if mode == 2 {
		r3.RenderRain(enc, view, dview)
	}
	q.Submit(enc.Finish())
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return pix
}

// A dark puff in front of a bright floor darkens it, drawn hard in the
// main pass or soft after it.
func TestRenderer3DParticles(t *testing.T) {
	const w, h = 160, 120
	at := func(p []byte) int { i := (h/2*w + w/2) * 4; return int(p[i]) + int(p[i+1]) + int(p[i+2]) }
	none, hard, soft := puffPixels(t, 0), puffPixels(t, 1), puffPixels(t, 2)
	if at(hard) > at(none)*2/3 || at(soft) > at(none)*2/3 {
		t.Fatalf("the puff does not darken the floor behind it: %d clear, %d hard, %d soft", at(none), at(hard), at(soft))
	}
	if testing.Verbose() {
		dumpPNG(t, soft, w, h, "particles-soft")
	}
}
