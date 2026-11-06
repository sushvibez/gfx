//go:build !js

package render

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render/fbx"
	"github.com/sushvibez/gfx/render/gltf"
)

// ---- a synthetic skinned model ---------------------------------------------

// skinnedBarDoc builds a two-joint bar standing on the origin: a square
// tube from y=0 to y=2, bound to joint A (the lower half) and joint B
// (pivoting at y=1), with a smooth blend across the seam. It is the
// smallest asset that exercises everything the skinned pipelines do —
// palette indexing, weight blending and normal deformation — without a
// binary fixture.
func skinnedBarDoc() *gltf.Document {
	const rings, half = 9, float32(0.25)
	corners := []core.Vec2{{X: -half, Y: -half}, {X: half, Y: -half}, {X: half, Y: half}, {X: -half, Y: half}}
	var p gltf.Primitive
	p.Material = -1
	for ri := range rings {
		y := float32(ri) * 0.25
		// Joint B takes over across y in [0.75, 1.25].
		wb := (y - 0.75) / 0.5
		wb = math32.Max(0, math32.Min(1, wb))
		for ci, c := range corners {
			n := core.V3(c.X, 0, c.Y).Normalize()
			p.Positions = append(p.Positions, core.V3(c.X, y, c.Y))
			p.Normals = append(p.Normals, n)
			p.UVs = append(p.UVs, core.Vec2{X: float32(ci) / 4, Y: y / 2})
			p.Joints = append(p.Joints, [4]uint16{0, 1, 0, 0})
			p.Weights = append(p.Weights, [4]float32{1 - wb, wb, 0, 0})
		}
	}
	for ri := 0; ri+1 < rings; ri++ {
		for ci := range 4 {
			a := uint32(ri*4 + ci)
			b := uint32(ri*4 + (ci+1)%4)
			p.Indices = append(p.Indices, a, b, a+4, b, b+4, a+4)
		}
	}
	doc := &gltf.Document{
		Meshes: []gltf.Mesh{{Name: "bar", Primitives: []gltf.Primitive{p}}},
		Nodes: []gltf.Node{
			{Name: "barMesh", Mesh: 0, Skin: 0, Local: core.Mat4Identity()},
			{Name: "jointA", Mesh: -1, Skin: -1, Local: core.Mat4Identity(), Children: []int{2}},
			{Name: "jointB", Mesh: -1, Skin: -1, Local: core.Mat4Translate(core.V3(0, 1, 0))},
		},
		Roots: []int{0, 1},
		Skins: []gltf.Skin{{
			Joints: []int{1, 2},
			InverseBind: []core.Mat4{
				core.Mat4Identity(),
				core.Mat4Translate(core.V3(0, -1, 0)),
			},
		}},
	}
	return doc
}

// bend rotates the bar's upper joint by angle about Z and re-poses.
func bend(m *Model, angle float32) {
	m.nodes[2].Local = core.Mat4Translate(core.V3(0, 1, 0)).Mul(core.Mat4RotateZ(angle))
	m.updateWorlds()
}

// skinScene renders the bar at one bend angle and returns the pixels.
// gpuPath selects the skinning path; everything else is identical, so the
// two outputs are directly comparable.
func skinScene(t *testing.T, gpuPath bool, angle float32) []byte {
	return skinSceneFade(t, gpuPath, angle, 1)
}

// skinSceneFade is skinScene with an alpha: below 1 the model is drawn with
// DrawFaded, which on the GPU-skinning path means the material pipeline's
// blended variant.
func skinSceneFade(t *testing.T, gpuPath bool, angle, alpha float32) []byte {
	t.Helper()
	matLibPath = wgpuLib(t)
	device, queue := matDevice()
	if device == nil {
		t.Skip("no headless device")
	}
	defer swapGPUSkinning(gpuPath)()

	const W, H = testW, testW
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	depthTex := device.CreateDepthTexture(W, H)
	defer depthTex.Release()
	depthView := depthTex.CreateView()
	defer depthView.Release()

	r, err := New3D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New3D: %v", err)
	}
	defer r.Release()

	model, err := r.BuildModel(skinnedBarDoc())
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	defer model.Release()
	bend(model, angle)

	cam := core.V3(0, 1.2, 4)
	vp := core.Mat4Perspective(math32.Pi/3, 1, 0.1, 100).Mul(core.Mat4LookAt(cam, core.V3(0, 1, 0), core.V3(0, 1, 0)))
	r.SetShadowArea(core.V3(0, 1, 0), 4)
	r.Begin(vp, core.V3(-0.35, -1, -0.4), cam)
	if alpha < 1 {
		model.DrawFaded(r, core.Mat4Identity(), alpha)
	} else {
		model.Draw(r, core.Mat4Identity())
	}

	enc := device.CreateCommandEncoder()
	if err := r.Render(enc, view, depthView, gpu.Color{R: 0.1, G: 0.1, B: 0.1, A: 1}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	queue.Submit(enc.Finish())
	out, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	if gpuPath && !model.gpuSkin {
		t.Fatal("GPU skinning requested but the model fell back to the CPU path")
	}
	if !gpuPath && model.gpuSkin {
		t.Fatal("CPU skinning requested but the model used the GPU path")
	}
	return out
}

// swapGPUSkinning sets the package switch and returns a restore func.
func swapGPUSkinning(on bool) func() {
	old := GPUSkinning
	GPUSkinning = on
	return func() { GPUSkinning = old }
}

// pixelDiff reports how many of a and b's pixels differ by more than tol in
// any channel, and the largest difference seen.
func pixelDiff(a, b []byte, tol int) (n, maxDiff int) {
	for i := 0; i+3 < len(a) && i+3 < len(b); i += 4 {
		d := 0
		for c := range 3 {
			if v := int(a[i+c]) - int(b[i+c]); v > d {
				d = v
			} else if -v > d {
				d = -v
			}
		}
		if d > maxDiff {
			maxDiff = d
		}
		if d > tol {
			n++
		}
	}
	return n, maxDiff
}

// The whole point of the change: the vertex shader must land the skinned
// mesh where the CPU put it. Both paths render the same bent bar and the
// images must match within rounding.
func TestGPUSkinMatchesCPU(t *testing.T) {
	const angle = math32.Pi / 5
	cpu := skinScene(t, false, angle)
	gpuPix := skinScene(t, true, angle)
	n, maxDiff := pixelDiff(cpu, gpuPix, 8)
	total := len(cpu) / 4
	if n*100 > total { // 1% of pixels may differ (edge rasterisation)
		t.Errorf("GPU vs CPU skinning: %d/%d pixels differ by >8 (max %d)", n, total, maxDiff)
	}
	if maxDiff > 64 {
		t.Errorf("GPU vs CPU skinning: max channel difference %d, want <= 64", maxDiff)
	}
}

// A moving joint must move pixels — a palette that never reaches the
// shader would still render a perfectly plausible (bind-pose) image.
func TestGPUSkinJointMovesPixels(t *testing.T) {
	straight := skinScene(t, true, 0)
	bent := skinScene(t, true, math32.Pi/4)
	n, _ := pixelDiff(straight, bent, 8)
	total := len(straight) / 4
	if n*100 < total*2 {
		t.Errorf("bending a joint changed only %d/%d pixels; the pose is not reaching the GPU", n, total)
	}
	// And the CPU path must agree that it moved (same scene, same pose).
	cpuBent := skinScene(t, false, math32.Pi/4)
	if n2, _ := pixelDiff(cpuBent, bent, 8); n2*100 > total {
		t.Errorf("bent pose differs between paths in %d/%d pixels", n2, total)
	}
}

// DrawFaded on the GPU-skinning path is real alpha through the material
// pipeline's blended variant, not the CPU dissolve: the faded model must
// still cover its silhouette (a dissolve punches holes) while reading
// visibly differently from the solid draw.
func TestGPUSkinFadedUsesAlpha(t *testing.T) {
	const angle = math32.Pi / 6
	solid := skinScene(t, true, angle)
	faded := skinSceneFade(t, true, angle, 0.5)
	bare := skinSceneFade(t, true, angle, 0) // fully transparent = background

	changed, _ := pixelDiff(solid, faded, 8)
	if changed == 0 {
		t.Error("DrawFaded(0.5) rendered identically to the solid model")
	}
	// Half alpha must land between the solid model and the empty scene
	// almost everywhere the model covers.
	between := 0
	covered := 0
	for i := 0; i+3 < len(solid); i += 4 {
		lo, hi := int(bare[i]), int(solid[i])
		if lo > hi {
			lo, hi = hi, lo
		}
		if hi-lo < 12 {
			continue // background: nothing to say
		}
		covered++
		if v := int(faded[i]); v >= lo-12 && v <= hi+12 {
			between++
		}
	}
	if covered == 0 {
		t.Fatal("the faded model covers no pixels at all")
	}
	if between*10 < covered*9 {
		t.Errorf("only %d/%d covered pixels blend between background and solid", between, covered)
	}
}

// ---- benchmark: a squad of soldiers ------------------------------------

// hostileDoc loads the Mixamo soldier rig from GFX_FBX_ASSETS, or skips.
func hostileDoc(b *testing.B) *gltf.Document {
	b.Helper()
	if os.Getenv("GFX_FBX_ASSETS") == "" {
		b.Skip("GFX_FBX_ASSETS unset")
	}
	path := filepath.Join(os.Getenv("GFX_FBX_ASSETS"), "models", "hostile.fbx")
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Skipf("%s not present: %v", path, err)
	}
	d, err := fbx.Parse(raw)
	if err != nil {
		b.Fatalf("fbx.Parse: %v", err)
	}
	doc, err := fbx.ToGLTF(d, func(uri string) ([]byte, error) {
		return os.ReadFile(filepath.Join(filepath.Dir(path), filepath.FromSlash(uri)))
	})
	if err != nil {
		b.Fatalf("fbx.ToGLTF: %v", err)
	}
	return doc
}

// BenchmarkSquad renders 18 animated soldiers a frame at a time — the
// scene that motivated GPU skinning — and reports the per-frame upload
// traffic alongside the wall clock. Run both paths to compare:
//
//	go test -run xxx -bench Squad ./...
func BenchmarkSquad(b *testing.B) {
	for _, gpuPath := range []bool{true, false} {
		name := "gpu"
		if !gpuPath {
			name = "cpu"
		}
		b.Run(name, func(b *testing.B) { benchSquad(b, gpuPath) })
	}
}

const squadSize = 18

func benchSquad(b *testing.B, gpuPath bool) {
	if p := os.Getenv("GFX_WGPU_LIB"); p != "" {
		matLibPath = p
	} else {
		p, err := filepath.Abs(filepath.Join("..", "build", "wgpu", gpu.DefaultLibNames()[0]))
		if err != nil {
			b.Skip("wgpu-native not found")
		}
		if _, err := os.Stat(p); err != nil {
			b.Skip("wgpu-native not found; run scripts/fetch-wgpu.sh")
		}
		matLibPath = p
	}
	doc := hostileDoc(b)
	device, queue := matDevice()
	if device == nil {
		b.Skip("no headless device")
	}
	defer swapGPUSkinning(gpuPath)()

	const W, H = 640, 480
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	depthTex := device.CreateDepthTexture(W, H)
	defer depthTex.Release()
	depthView := depthTex.CreateView()
	defer depthView.Release()

	r, err := New3D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		b.Fatalf("New3D: %v", err)
	}
	defer r.Release()

	models := make([]*Model, squadSize)
	for i := range models {
		m, err := r.BuildModel(doc)
		if err != nil {
			b.Fatalf("BuildModel: %v", err)
		}
		defer m.Release()
		models[i] = m
	}
	scale := float32(1)
	if h := models[0].Height(); h > 0 {
		scale = 1.8 / h
	}
	// The joint the benchmark waggles, so every actor re-poses each frame.
	spine := 0
	for i := range models[0].nodes {
		if len(models[0].nodes[i].Children) > 0 && models[0].nodes[i].Mesh < 0 {
			spine = i
			break
		}
	}
	rest := models[0].nodes[spine].Local

	cam := core.V3(0, 2, 14)
	vp := core.Mat4Perspective(math32.Pi/3, float32(W)/H, 0.1, 200).
		Mul(core.Mat4LookAt(cam, core.V3(0, 1, 0), core.V3(0, 1, 0)))
	r.SetShadowArea(core.V3(0, 1, 0), 12)

	frame := func(t float32) {
		r.Begin(vp, core.V3(-0.35, -1, -0.4), cam)
		for i, m := range models {
			m.nodes[spine].Local = rest.Mul(core.Mat4RotateZ(0.2 * math32.Sin(t+float32(i))))
			m.updateWorlds()
			x := float32(i%6)*1.5 - 3.75
			z := float32(i/6) * 1.5
			m.Draw(r, core.Mat4Translate(core.V3(x, 0, z)).Mul(core.Mat4Scale(core.V3(scale, scale, scale))))
		}
		enc := device.CreateCommandEncoder()
		if err := r.Render(enc, view, depthView, gpu.Color{R: 0.1, G: 0.1, B: 0.1, A: 1}); err != nil {
			b.Fatalf("Render: %v", err)
		}
		queue.Submit(enc.Finish())
		device.WaitIdle()
	}
	frame(0) // warm up: pipelines, buffers, first upload

	gpu.ResetUploadStats()
	b.ResetTimer()
	start := time.Now()
	for i := 0; b.Loop(); i++ {
		frame(float32(i) * 0.05)
	}
	elapsed := time.Since(start)
	b.StopTimer()
	bc, bb, _, _ := gpu.UploadStats()
	n := float64(b.N)
	if n == 0 {
		n = 1
	}
	b.ReportMetric(float64(elapsed.Nanoseconds())/n/1e6, "ms/frame")
	b.ReportMetric(float64(bb)/n/1024, "KB-uploaded/frame")
	b.ReportMetric(float64(bc)/n, "writes/frame")
}
