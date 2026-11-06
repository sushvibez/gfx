package gltf

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

// TestParseSkin parses a minimal skinned asset: a 3-vertex triangle with
// u8 JOINTS_0 / float WEIGHTS_0 and a 2-joint skin with inverse binds.
func TestParseSkin(t *testing.T) {
	f32 := func(vals ...float32) []byte {
		b := make([]byte, len(vals)*4)
		for i, v := range vals {
			binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
		}
		return b
	}
	pos := f32(0, 0, 0, 1, 0, 0, 0, 1, 0)
	jointsB := []byte{0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0} // u8 vec4 x3
	weightsB := f32(1, 0, 0, 0, 0.6, 0.6, 0, 0 /*renormalizes*/, 0.5, 0.5, 0, 0)
	// invBind: identity + translate(-2): column-major, translation in col 3.
	ib := f32(
		1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1,
		1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, -2, 0, 0, 1)
	buf := append(append(append(append([]byte{}, pos...), jointsB...), weightsB...), ib...)
	o1, o2, o3 := len(pos), len(pos)+len(jointsB), len(pos)+len(jointsB)+len(weightsB)

	doc := fmt.Sprintf(`{
  "asset": {"version": "2.0"},
  "scene": 0,
  "scenes": [{"nodes": [0, 1, 2]}],
  "nodes": [
    {"name": "skinned", "mesh": 0, "skin": 0},
    {"name": "j0"},
    {"name": "j1", "translation": [2, 0, 0]}
  ],
  "skins": [{"joints": [1, 2], "inverseBindMatrices": 3}],
  "meshes": [{"primitives": [{"attributes": {"POSITION": 0, "JOINTS_0": 1, "WEIGHTS_0": 2}}]}],
  "accessors": [
    {"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"},
    {"bufferView": 1, "componentType": 5121, "count": 3, "type": "VEC4"},
    {"bufferView": 2, "componentType": 5126, "count": 3, "type": "VEC4"},
    {"bufferView": 3, "componentType": 5126, "count": 2, "type": "MAT4"}
  ],
  "bufferViews": [
    {"buffer": 0, "byteOffset": 0, "byteLength": %d},
    {"buffer": 0, "byteOffset": %d, "byteLength": %d},
    {"buffer": 0, "byteOffset": %d, "byteLength": %d},
    {"buffer": 0, "byteOffset": %d, "byteLength": %d}
  ],
  "buffers": [{"byteLength": %d, "uri": "data:application/octet-stream;base64,%s"}]
}`, len(pos), o1, len(jointsB), o2, len(weightsB), o3, len(ib),
		len(buf), base64.StdEncoding.EncodeToString(buf))

	d, err := Parse([]byte(doc), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Skins) != 1 || len(d.Skins[0].Joints) != 2 {
		t.Fatalf("skins wrong: %+v", d.Skins)
	}
	if d.Skins[0].InverseBind[1][12] != -2 {
		t.Fatalf("invBind translation = %v, want -2", d.Skins[0].InverseBind[1][12])
	}
	if d.Nodes[0].Skin != 0 || d.Nodes[1].Skin != -1 {
		t.Fatalf("node skin indices wrong: %d %d", d.Nodes[0].Skin, d.Nodes[1].Skin)
	}
	p := d.Meshes[0].Primitives[0]
	if len(p.Joints) != 3 || len(p.Weights) != 3 {
		t.Fatalf("joint/weight counts: %d/%d, want 3/3", len(p.Joints), len(p.Weights))
	}
	if p.Joints[1] != [4]uint16{1, 0, 0, 0} {
		t.Fatalf("joints[1] = %v", p.Joints[1])
	}
	// 0.6/0.6 renormalizes to 0.5/0.5.
	if w := p.Weights[1]; mathAbs(w[0]-0.5) > 1e-5 || mathAbs(w[1]-0.5) > 1e-5 {
		t.Fatalf("weights[1] = %v, want renormalized 0.5/0.5", w)
	}
}

func mathAbs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
