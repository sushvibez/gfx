//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

func postPipesFor(dev *gpu.Device) int {
	postPipes.Lock()
	defer postPipes.Unlock()
	n := 0
	for k := range postPipes.m {
		if k.dev == dev {
			n++
		}
	}
	return n
}

// Posts of one device share their pipelines: the second Post builds none,
// keeps working after the first is released (each holds its own
// reference), and releasing the device empties the cache.
func TestPostSharesPipelines(t *testing.T) {
	inst, err := gpu.NewInstance(wgpuLib(t))
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := adapter.RequestDevice()
	if err != nil {
		t.Fatal(err)
	}
	q := dev.Queue()
	errs := gpu.ErrorCount()

	a, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	n := postPipesFor(dev)
	if n == 0 {
		t.Fatal("the first Post cached no pipelines")
	}
	b, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	if m := postPipesFor(dev); m != n {
		t.Fatalf("the second Post built pipelines: %d cached, was %d", m, n)
	}
	a.Release() // b's references keep the pipelines alive

	const w, h = 160, 100
	r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
	if err != nil {
		t.Fatal(err)
	}
	fs := DefaultFrameSettings()
	fs.Tonemap = TonemapHDR
	r3.SetFrameSettings(fs)
	b.Settings.AutoExposure, b.Settings.Grain, b.Settings.Vignette = 0, 0, 0
	b.Resize(w, h)
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	view := target.CreateView()
	eye := core.V3(0, 1.2, 6)
	vp := core.Mat4Perspective(1.0, float32(w)/float32(h), 0.1, 100).Mul(core.Mat4LookAt(eye, core.V3(0, 1, 0), core.V3(0, 1, 0)))
	lamp := &Material{Color: RGBA{R: 1, G: 0.8, B: 0.5, A: 1}, Unlit: true, Emission: 20}
	for i := 0; i < 3; i++ {
		r3.Begin(vp, core.V3(-0.4, -1, -0.3), eye)
		r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 1, 0)).Mul(core.Mat4Scale(core.V3(0.5, 0.5, 0.5))), lamp)
		enc := dev.CreateCommandEncoder()
		if err := r3.Render(enc, b.SceneView(), b.DepthView(), gpu.Color{R: 0.01, G: 0.012, B: 0.02, A: 1}); err != nil {
			t.Fatal(err)
		}
		b.Render(enc, view)
		q.Submit(enc.Finish())
	}
	pix, err := dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	if l := lumAt(pix, w, w/2, h/2); l < 200 {
		t.Fatalf("through the second Post the lamp reads %v, want bright", l)
	}
	if e := gpu.ErrorCount(); e != errs {
		t.Fatalf("%d GPU errors after the first Post was released", e-errs)
	}
	view.Release()
	target.Release()
	r3.Release()
	b.Release()
	dev.Release()
	if m := postPipesFor(dev); m != 0 {
		t.Fatalf("%d pipelines still cached after the device was released", m)
	}
}
