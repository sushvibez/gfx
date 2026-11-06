//go:build !js

package render

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render/gltf"
)

// TestModelDuckPixels loads the Khronos Duck, renders it headlessly, and
// asserts the texture actually arrives on screen: yellow-dominant pixels
// in quantity, with lighting variation.
func TestModelDuckPixels(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "build", "assets", "Duck.glb"))
	if err != nil {
		t.Skip("Duck.glb not fetched; run scripts/fetch-assets.sh")
	}
	doc, err := gltf.Parse(data, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	device, queue := headlessDevice(t)
	const W, H = 320, 320
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

	model, err := r.BuildModel(doc)
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	defer model.Release()

	// The Duck's root node scales cm to meters: it stands ~1.7 units tall.
	cam := core.V3(2.2, 1.6, 2.6)
	viewM := core.Mat4LookAt(cam, core.V3(0, 0.8, 0), core.V3(0, 1, 0))
	proj := core.Mat4Perspective(math.Pi/3, 1, 0.1, 100)
	r.Begin(proj.Mul(viewM), core.V3(-0.5, -1, -0.4), cam)
	model.Draw(r, core.Mat4Identity())

	enc := device.CreateCommandEncoder()
	if err := r.Render(enc, view, depthView, gpu.Color{R: 0.05, G: 0.05, B: 0.08, A: 1}); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}

	yellow, levels := 0, map[int]bool{}
	for i := 0; i < len(pix); i += 4 {
		r8, g8, b8 := int(pix[i]), int(pix[i+1]), int(pix[i+2])
		// Duck yellow: red and green high together, blue well below.
		if r8 > 80 && g8 > 60 && b8 < r8/2 {
			yellow++
			levels[r8/32] = true
		}
	}
	if yellow < 3000 {
		t.Fatalf("only %d yellow pixels — duck or its texture missing", yellow)
	}
	if len(levels) < 3 {
		t.Fatalf("duck shading flat: %d brightness bands", len(levels))
	}
}

// TestDissolve: DrawFaded's geometry punch-out collapses a stable,
// monotonically growing fraction of the collapsible triangles, and leaves
// meshes whose vertices are shared between triangles untouched.
func TestDissolve(t *testing.T) {
	const tris = 300
	verts := make([]Vertex3D, tris*3)
	idx := make([]uint32, tris*3)
	for i := range idx {
		idx[i] = uint32(i)
		verts[i].Pos = core.V3(float32(i), float32(i%7), 0)
	}
	it := &modelItem{mesh: &Mesh{verts: verts, idx: idx}}

	collapsed := func(v []Vertex3D) map[int]bool {
		out := map[int]bool{}
		for tri := 0; tri < tris; tri++ {
			a, b, c := v[tri*3], v[tri*3+1], v[tri*3+2]
			if a.Pos == b.Pos && b.Pos == c.Pos {
				out[tri] = true
			}
		}
		return out
	}

	half := collapsed(it.dissolve(verts, 0.5))
	if n := len(half); n < tris/3 || n > 2*tris/3 {
		t.Fatalf("dissolve(0.5) collapsed %d/%d triangles, want ~half", n, tris)
	}
	quarter := collapsed(it.dissolve(verts, 0.25))
	if len(quarter) <= len(half) {
		t.Fatalf("dissolve is not monotonic: %d at 0.25 vs %d at 0.5", len(quarter), len(half))
	}
	for tri := range half {
		if !quarter[tri] {
			t.Fatalf("triangle %d reappeared as the fade deepened", tri)
		}
	}
	// Stable across calls: the same alpha yields the same pattern.
	again := collapsed(it.dissolve(verts, 0.5))
	if len(again) != len(half) {
		t.Fatalf("dissolve pattern unstable: %d vs %d", len(again), len(half))
	}
	// Source vertices are never mutated (the fade writes to scratch).
	for i := range verts {
		if verts[i].Pos != core.V3(float32(i), float32(i%7), 0) {
			t.Fatalf("dissolve mutated the source vertices at %d", i)
		}
	}

	// A fan sharing every vertex has no collapsible triangle: unchanged.
	shared := &modelItem{mesh: &Mesh{
		verts: []Vertex3D{{}, {Pos: core.V3(1, 0, 0)}, {Pos: core.V3(0, 1, 0)}, {Pos: core.V3(1, 1, 0)}},
		idx:   []uint32{0, 1, 2, 1, 2, 3},
	}}
	if got := shared.dissolve(shared.mesh.verts, 0.1); &got[0] != &shared.mesh.verts[0] {
		t.Fatal("shared-vertex mesh was dissolved (indices cannot be rewritten)")
	}
}

// TestModelHeight: Height is the posed bounding box's vertical extent.
func TestModelHeight(t *testing.T) {
	m := &Model{items: []modelItem{{
		mesh:  &Mesh{bmin: core.V3(-1, 0, -1), bmax: core.V3(1, 180, 1)},
		world: core.Mat4Scale(core.V3(1, 1, 1)),
	}}}
	if h := m.Height(); math.Abs(float64(h)-180) > 1e-4 {
		t.Fatalf("Height = %v, want 180", h)
	}
}
