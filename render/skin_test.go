package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render/fbx"
	"github.com/sushvibez/gfx/render/gltf"
)

// TestSkinning bends the classic two-joint elbow: joint 0 fixed at the
// origin, joint 1 pivoting at x=2 rotated 90° about Z. Vertices bound to
// joint 1 swing around the pivot; a 50/50 vertex blends both.
func TestSkinning(t *testing.T) {
	skin := &gltf.Skin{
		Joints: []int{0, 1},
		InverseBind: []core.Mat4{
			core.Mat4Identity(),
			core.Mat4Translate(core.V3(-2, 0, 0)), // joint 1 binds at x=2
		},
	}
	worlds := []core.Mat4{
		core.Mat4Identity(),
		core.Mat4Translate(core.V3(2, 0, 0)).Mul(core.Mat4RotateZ(math32.Pi / 2)),
	}
	palette := jointPalette(skin, worlds, nil)
	if len(palette) != 2 {
		t.Fatalf("palette size = %d, want 2", len(palette))
	}

	base := []Vertex3D{
		{Pos: core.V3(3, 0, 0), Normal: core.V3(0, 1, 0)}, // fully joint 1
		{Pos: core.V3(1, 0, 0), Normal: core.V3(0, 1, 0)}, // 50/50 blend
		{Pos: core.V3(0, 0, 0), Normal: core.V3(0, 1, 0)}, // fully joint 0
	}
	joints := [][4]uint16{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 0, 0}}
	weights := [][4]float32{{1, 0, 0, 0}, {0.5, 0.5, 0, 0}, {1, 0, 0, 0}}
	dst := make([]Vertex3D, len(base))
	skinVertices(dst, base, joints, weights, palette, nil)

	near := func(got core.Vec3, x, y, z float32, what string) {
		t.Helper()
		if math32.Abs(got.X-x) > 1e-4 || math32.Abs(got.Y-y) > 1e-4 || math32.Abs(got.Z-z) > 1e-4 {
			t.Fatalf("%s = (%v,%v,%v), want (%v,%v,%v)", what, got.X, got.Y, got.Z, x, y, z)
		}
	}
	// (3,0,0) around the x=2 pivot by 90° → (2,1,0).
	near(dst[0].Pos, 2, 1, 0, "joint-1 vertex")
	// Blend of (1,0,0) [joint 0] and (2,-1,0) [joint 1] → (1.5,-0.5,0).
	near(dst[1].Pos, 1.5, -0.5, 0, "blended vertex")
	// Fully static.
	near(dst[2].Pos, 0, 0, 0, "joint-0 vertex")
	// The rotated vertex's normal turned with the joint: +Y → -X.
	near(dst[0].Normal, -1, 0, 0, "rotated normal")
	// UVs/material fields carried through.
	if dst[0].U != base[0].U {
		t.Fatal("skinning dropped vertex attributes")
	}
}

// TestSkinnedPoseFlow drives Model.updateWorlds → applyPose bookkeeping
// without a GPU: a skinned item goes identity-placed and pose-dirty.
func TestSkinnedPoseFlow(t *testing.T) {
	m := &Model{
		nodes: []gltf.Node{
			{Mesh: 0, Skin: 0, Local: core.Mat4Translate(core.V3(5, 0, 0)),
				T: core.V3(5, 0, 0), R: core.QuatIdentity(), S: core.V3(1, 1, 1), HasTRS: true},
		},
		roots: []int{0},
		skins: []gltf.Skin{{Joints: []int{0}, InverseBind: []core.Mat4{core.Mat4Identity()}}},
		items: []modelItem{{
			node: 0, skin: 0,
			base:    []Vertex3D{{Pos: core.V3(1, 0, 0)}},
			joints:  [][4]uint16{{0, 0, 0, 0}},
			weights: [][4]float32{{1, 0, 0, 0}},
			scratch: make([]Vertex3D, 1),
		}},
	}
	m.updateWorlds()
	// Skinned items ignore their node placement (palette carries it).
	if m.items[0].world != core.Mat4Identity() {
		t.Fatal("skinned item world is not identity")
	}
	if !m.poseDirty {
		t.Fatal("pose not marked dirty")
	}
	// The palette applies the node world: vertex (1,0,0) → (6,0,0).
	m.palette = jointPalette(&m.skins[0], m.worlds, m.palette)
	skinVertices(m.items[0].scratch, m.items[0].base, m.items[0].joints, m.items[0].weights, m.palette, nil)
	if got := m.items[0].scratch[0].Pos; math32.Abs(got.X-6) > 1e-4 {
		t.Fatalf("skinned x = %v, want 6", got.X)
	}
}

// cqbSoldier loads the 51-joint Mixamo soldier from GFX_FBX_ASSETS as the
// realistic skinning workload. Skipped when the assets are absent.
func cqbSoldier(tb testing.TB) (base []Vertex3D, joints [][4]uint16, weights [][4]float32, skin *gltf.Skin) {
	tb.Helper()
	var doc *gltf.Document
	for _, path := range []string{filepath.Join(os.Getenv("GFX_FBX_ASSETS"), "models", "hostile.fbx")} {
		if os.Getenv("GFX_FBX_ASSETS") == "" {
			break
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		d, err := fbx.Parse(b)
		if err != nil {
			tb.Fatalf("parse %s: %v", path, err)
		}
		doc, err = fbx.ToGLTF(d, func(string) ([]byte, error) { return nil, os.ErrNotExist })
		if err != nil {
			tb.Fatalf("convert %s: %v", path, err)
		}
		break
	}
	if doc == nil {
		tb.Skip("GFX_FBX_ASSETS unset or absent")
	}
	if len(doc.Skins) == 0 {
		tb.Fatal("no skin in the soldier rig")
	}
	var prim *gltf.Primitive
	for mi := range doc.Meshes {
		for pi := range doc.Meshes[mi].Primitives {
			p := &doc.Meshes[mi].Primitives[pi]
			if len(p.Joints) == len(p.Positions) && len(p.Positions) > 0 {
				prim = p
			}
		}
	}
	if prim == nil {
		tb.Fatal("no skinned primitive")
	}
	base = make([]Vertex3D, len(prim.Positions))
	for i := range base {
		base[i].Pos = prim.Positions[i]
		if i < len(prim.Normals) {
			base[i].Normal = prim.Normals[i]
		}
	}
	return base, prim.Joints, prim.Weights, &doc.Skins[0]
}

// BenchmarkSkinCQBSoldier measures the per-actor CPU skinning cost of the
// real 51-joint soldier: the budget question is 18 of them at 60 fps
// (16.6 ms/frame), so the reported ns/op is the per-actor slice.
func BenchmarkSkinCQBSoldier(b *testing.B) {
	base, joints, weights, skin := cqbSoldier(b)
	worlds := make([]core.Mat4, 0, len(skin.Joints))
	for j := range skin.Joints {
		worlds = append(worlds, core.Mat4RotateY(float32(j)*0.01))
	}
	// Joint node indices index into worlds; remap onto a dense array.
	dense := &gltf.Skin{Joints: make([]int, len(skin.Joints)), InverseBind: skin.InverseBind}
	for j := range dense.Joints {
		dense.Joints[j] = j
	}
	palette := jointPalette(dense, worlds, nil)
	dst := make([]Vertex3D, len(base))
	pack := packPalette(nil, palette)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pack = skinVertices(dst, base, joints, weights, palette, pack)
	}
	b.ReportMetric(float64(len(base)), "verts")
	b.ReportMetric(float64(len(skin.Joints)), "joints")
}

// BenchmarkJointPaletteCQB isolates the palette build (51 joints).
func BenchmarkJointPaletteCQB(b *testing.B) {
	_, _, _, skin := cqbSoldier(b)
	worlds := make([]core.Mat4, 0, len(skin.Joints))
	dense := &gltf.Skin{Joints: make([]int, len(skin.Joints)), InverseBind: skin.InverseBind}
	for j := range skin.Joints {
		worlds = append(worlds, core.Mat4RotateY(float32(j)*0.01))
		dense.Joints[j] = j
	}
	var palette []core.Mat4
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		palette = jointPalette(dense, worlds, palette)
	}
}

// BenchmarkSkinScene18 is the target scene: 18 animated soldiers
// re-skinned in one frame. ns/op is the whole frame's CPU skinning cost —
// it must stay well under 16.6 ms for 60 fps.
func BenchmarkSkinScene18(b *testing.B) {
	const actors = 18
	base, joints, weights, skin := cqbSoldier(b)
	worlds := make([]core.Mat4, 0, len(skin.Joints))
	dense := &gltf.Skin{Joints: make([]int, len(skin.Joints)), InverseBind: skin.InverseBind}
	for j := range skin.Joints {
		worlds = append(worlds, core.Mat4RotateY(float32(j)*0.01))
		dense.Joints[j] = j
	}
	dst := make([]Vertex3D, len(base))
	var palette []core.Mat4
	var pack []skinMat
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for range actors {
			palette = jointPalette(dense, worlds, palette)
			pack = skinVertices(dst, base, joints, weights, palette, pack)
		}
	}
	b.ReportMetric(float64(actors), "actors")
}
