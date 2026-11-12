//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// mirrorScene renders a glossy floor with a glowing red block standing on
// it, seen from a low camera, through Post; strength is
// FrameSettings.SceneReflection.
func mirrorScene(t *testing.T, w, h int, strength float32) (pix []byte, vp core.Mat4) {
	t.Helper()
	dev, q := headlessDevice(t)
	r3, err := New3D(dev, q, gpu.FormatRGBA16Float)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Release()
	fs := DefaultFrameSettings()
	fs.Tonemap = TonemapHDR
	fs.SkyReflection, fs.SceneReflection = 1, strength
	fs.SkyTop, fs.SkyHorizon = RGBA{R: 0.02, G: 0.02, B: 0.03, A: 1}, RGBA{R: 0.03, G: 0.03, B: 0.03, A: 1}
	fs.Sun = RGBA{R: 0.3, G: 0.3, B: 0.3, A: 1}
	fs.Ambient = RGBA{R: 0.02, G: 0.02, B: 0.02, A: 1}
	r3.SetFrameSettings(fs)
	r3.SetShadowArea(core.V3(0, 0, 0), 30)
	r3.KeepDepth = true
	post, err := NewPost(dev, q, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer post.Release()
	s := &post.Settings
	s.AutoExposure, s.Bloom, s.Grain, s.ChromaticAberration, s.Vignette, s.Sharpen, s.AO = 0, 0, 0, 0, 0, 0, 0
	s.Lift = [3]float32{}
	s.FXAA = false
	post.Resize(w, h)
	target := dev.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	tview := target.CreateView()
	defer func() { tview.Release(); target.Release() }()

	eye := core.V3(0, 1.5, 12)
	proj := core.Mat4Perspective(0.9, float32(w)/float32(h), 0.1, 300)
	view := core.Mat4LookAt(eye, core.V3(0, 1.5, 0), core.V3(0, 1, 0))
	vp = proj.Mul(view)
	post.Projection, post.View = proj, view
	post.Atmosphere = Atmosphere{SunDir: core.V3(0, -1, 0.2), Zenith: [3]float32{0.02, 0.02, 0.03}, Horizon: [3]float32{0.03, 0.03, 0.03}}
	floor := &Material{Color: RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1}, Roughness: 0.05, Metallic: 1}
	red := &Material{Color: RGBA{R: 1, G: 0.05, B: 0.05, A: 1}, Unlit: true, Emission: 2}
	r3.Begin(vp, core.V3(0, -1, 0.2), eye)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, -0.5, 0)).Mul(core.Mat4Scale(core.V3(60, 1, 60))), floor)
	r3.DrawMaterial(r3.CubeMesh(), core.Mat4Translate(core.V3(0, 2, 0)).Mul(core.Mat4Scale(core.V3(3, 4, 3))), red)
	post.UseShadow(r3)
	enc := dev.CreateCommandEncoder()
	if err := r3.Render(enc, post.SceneView(), post.DepthView(), gpu.Color{A: 1}); err != nil {
		t.Fatal(err)
	}
	post.Render(enc, tview)
	q.Submit(enc.Finish())
	pix, err = dev.ReadTexture(q, target, w, h)
	if err != nil {
		t.Fatal(err)
	}
	return pix, vp
}

// A glossy floor mirrors what stands on it: the floor in front of a red
// block shows the block upside down with FrameSettings.SceneReflection,
// and only the sky without; the floor beside the block is unchanged.
func TestSceneReflection(t *testing.T) {
	const w, h = 480, 300
	off, vp := mirrorScene(t, w, h, 0)
	on, _ := mirrorScene(t, w, h, 1)
	at := func(pix []byte, p core.Vec3) (r, g float64) {
		c := vp.MulVec4(core.V4(p.X, p.Y, p.Z, 1))
		i := (int((0.5-c.Y/c.W*0.5)*h)*w + int((c.X/c.W*0.5+0.5)*w)) * 4
		return float64(pix[i]), float64(pix[i+1])
	}
	// The floor between the camera and the block: the eye at 1.5 over a
	// point 3 in front of the block's face sees the face at height 0.75.
	front := core.V3(0, 0, 4.5)
	side := core.V3(7, 0, 4.5)
	rOff, _ := at(off, front)
	rOn, gOn := at(on, front)
	if rOn < rOff+60 || rOn < 2*gOn {
		t.Fatalf("the floor does not mirror the block: red %v with reflections (green %v), %v without", rOn, gOn, rOff)
	}
	sOff, _ := at(off, side)
	sOn, _ := at(on, side)
	if sOn > sOff+12 || sOn < sOff-12 {
		t.Fatalf("the floor beside the block changed: red %v with reflections, %v without", sOn, sOff)
	}
	if testing.Verbose() {
		dumpPNG(t, on, w, h, "ssr-on")
	}
}
