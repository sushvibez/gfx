package gml

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/physics/p3"
)

// writeQuadGLTF writes a minimal .gltf: a unit quad (two triangles) in the
// XY plane at z=0, spanning x,y ∈ [-1, 1].
func writeQuadGLTF(t *testing.T, dir string) string {
	t.Helper()
	f32 := func(vals ...float32) []byte {
		b := make([]byte, len(vals)*4)
		for i, v := range vals {
			binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
		}
		return b
	}
	pos := f32(-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0)
	idx := make([]byte, 12)
	for i, v := range []uint16{0, 1, 2, 0, 2, 3} {
		binary.LittleEndian.PutUint16(idx[i*2:], v)
	}
	buf := append(append([]byte{}, pos...), idx...)
	doc := fmt.Sprintf(`{
  "asset": {"version": "2.0"},
  "scene": 0,
  "scenes": [{"nodes": [0]}],
  "nodes": [{"mesh": 0}],
  "meshes": [{"primitives": [{"attributes": {"POSITION": 0}, "indices": 1}]}],
  "accessors": [
    {"bufferView": 0, "componentType": 5126, "count": 4, "type": "VEC3"},
    {"bufferView": 1, "componentType": 5123, "count": 6, "type": "SCALAR"}
  ],
  "bufferViews": [
    {"buffer": 0, "byteOffset": 0, "byteLength": %d},
    {"buffer": 0, "byteOffset": %d, "byteLength": %d}
  ],
  "buffers": [{"byteLength": %d, "uri": "data:application/octet-stream;base64,%s"}]
}`, len(pos), len(pos), len(idx), len(buf), base64.StdEncoding.EncodeToString(buf))
	path := filepath.Join(dir, "quad.gltf")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMeshInstanceCollidersAndTransforms(t *testing.T) {
	dir := t.TempDir()
	writeQuadGLTF(t, dir)
	scene := `
Scene3D {
    MeshInstance { id: wall; source: "quad.gltf"; position: [0, 1, 5]; collider: "mesh"; scale: 2 }
    MeshInstance { id: crate; source: "quad.gltf"; position: [6, 1, 5]; rotationY: 90; collider: "box" }
    MeshInstance { source: "quad.gltf"; position: [12, 0, 0] }
}`
	path := filepath.Join(dir, "scene.gml")
	if err := os.WriteFile(path, []byte(scene), 0o644); err != nil {
		t.Fatal(err)
	}
	env := NewEnv()
	v, err := LoadSceneFile(path, env)
	if err != nil {
		t.Fatalf("LoadSceneFile: %v", err)
	}
	f, err := v.Frame()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Models) != 3 {
		t.Fatalf("models = %d, want 3", len(f.Models))
	}
	// Scaled instance transform: origin at (0,1,5), unit X scaled to 2.
	p := f.Models[0].Transform.MulPoint(core.V3(1, 0, 0))
	if math32.Abs(p.X-2) > 1e-4 || math32.Abs(p.Y-1) > 1e-4 || math32.Abs(p.Z-5) > 1e-4 {
		t.Fatalf("scaled transform wrong: (%v,%v,%v)", p.X, p.Y, p.Z)
	}

	// Mesh collider: the quad (scaled x,y ∈ [-2,2]) at z=5 blocks a +Z ray.
	h, ok := v.World().RayCast(core.V3(1.5, 1.5, 0), core.V3(0, 0, 1), 100, p3.LayerAll)
	if !ok {
		t.Fatal("mesh collider not hit")
	}
	if math32.Abs(h.T-5) > 1e-3 {
		t.Fatalf("mesh collider T = %v, want 5", h.T)
	}
	// Outside the scaled quad: misses.
	if _, ok := v.World().RayCast(core.V3(2.5, 1, 0), core.V3(0, 0, 1), 100, p3.LayerAll); ok {
		t.Fatal("ray outside the quad hit")
	}

	// Box collider on the rotated instance: the quad's AABB rotated 90°
	// about Y becomes a thin wall along Z at x=6.
	if _, ok := v.World().RayCast(core.V3(0, 1, 5), core.V3(1, 0, 0), 100, p3.LayerAll); !ok {
		t.Fatal("box collider not hit")
	}
	if _, ok := v.Body("wall"); !ok {
		t.Fatal("wall body not resolvable")
	}
	// The collider-less instance has no body.
	if _, ok := v.World().RayCast(core.V3(12, 0, -5), core.V3(0, 0, 1), 100, p3.LayerAll); ok {
		t.Fatal("collider-less model has a body")
	}
}

func TestMeshInstanceMissingSourceIsSoft(t *testing.T) {
	env := NewEnv()
	v, err := LoadScene(`Scene3D {
        MeshInstance { source: "does-not-exist.glb" }
        Box { size: [2, 2, 2]; position: [0, 1, 0]; color: "#fff" }
    }`, env)
	if err != nil {
		t.Fatalf("missing model source must not fail the scene: %v", err)
	}
	f, _ := v.Frame()
	if len(f.Models) != 1 || len(f.Draws) != 1 {
		t.Fatalf("frame = %d models / %d draws, want 1/1", len(f.Models), len(f.Draws))
	}
	v.Advance(0.016) // no player: must not panic
}
