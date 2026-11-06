package gltf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/core"
)

// buildTestGLB assembles a minimal valid GLB: one triangle with positions,
// normals and uint16 indices, a TRS node, and a base-color-factor material.
func buildTestGLB(t *testing.T) []byte {
	t.Helper()
	var bin bytes.Buffer
	putF32 := func(vs ...float32) {
		for _, v := range vs {
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
			bin.Write(b[:])
		}
	}
	// positions (3 x vec3) at offset 0
	putF32(0, 0, 0, 1, 0, 0, 0, 1, 0)
	// normals (3 x vec3) at offset 36
	putF32(0, 0, 1, 0, 0, 1, 0, 0, 1)
	// indices (3 x uint16 + 2 pad) at offset 72
	for _, i := range []uint16{0, 1, 2} {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], i)
		bin.Write(b[:])
	}
	bin.Write([]byte{0, 0})

	doc := fmt.Sprintf(`{
"asset":{"version":"2.0","generator":"test"},
"scene":0,
"scenes":[{"name":"main","nodes":[0],"extras":{"units":"m"}}],
"nodes":[{"name":"tri","mesh":0,"translation":[5,0,0],"scale":[2,2,2],"extras":{"type":"ROOM","hoverable":true}}],
"meshes":[{"name":"m","extras":{"lod":0},"primitives":[{"attributes":{"POSITION":0,"NORMAL":1},"indices":2,"material":0}]}],
"materials":[{"name":"red","doubleSided":true,"alphaMode":"BLEND","pbrMetallicRoughness":{"baseColorFactor":[1,0,0,1],"roughnessFactor":0.5}}],
"accessors":[
{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},
{"bufferView":1,"componentType":5126,"count":3,"type":"VEC3"},
{"bufferView":2,"componentType":5123,"count":3,"type":"SCALAR"}],
"bufferViews":[
{"buffer":0,"byteOffset":0,"byteLength":36},
{"buffer":0,"byteOffset":36,"byteLength":36},
{"buffer":0,"byteOffset":72,"byteLength":6}],
"buffers":[{"byteLength":%d}]
}`, bin.Len())
	// Pad JSON to 4 bytes.
	for len(doc)%4 != 0 {
		doc += " "
	}

	var glb bytes.Buffer
	w32 := func(v uint32) {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], v)
		glb.Write(b[:])
	}
	glb.WriteString("glTF")
	w32(2)
	w32(uint32(12 + 8 + len(doc) + 8 + bin.Len()))
	w32(uint32(len(doc)))
	glb.WriteString("JSON")
	glb.WriteString(doc)
	w32(uint32(bin.Len()))
	glb.WriteString("BIN\x00")
	glb.Write(bin.Bytes())
	return glb.Bytes()
}

func TestParseMinimalGLB(t *testing.T) {
	doc, err := Parse(buildTestGLB(t), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Meshes) != 1 || len(doc.Meshes[0].Primitives) != 1 {
		t.Fatalf("meshes = %+v", doc.Meshes)
	}
	p := doc.Meshes[0].Primitives[0]
	if len(p.Positions) != 3 || p.Positions[1].X != 1 {
		t.Fatalf("positions = %v", p.Positions)
	}
	if len(p.Normals) != 3 || p.Normals[0].Z != 1 {
		t.Fatalf("normals = %v", p.Normals)
	}
	if len(p.Indices) != 3 || p.Indices[2] != 2 {
		t.Fatalf("indices = %v", p.Indices)
	}
	if p.Material != 0 {
		t.Fatalf("material = %d", p.Material)
	}
	if doc.Materials[0].BaseColor != [4]float32{1, 0, 0, 1} || doc.Materials[0].Roughness != 0.5 {
		t.Fatalf("material = %+v", doc.Materials[0])
	}
	// TRS node: translation 5 with scale 2 — local origin maps to (5,0,0),
	// local (1,0,0) maps to (7,0,0).
	if len(doc.Roots) != 1 {
		t.Fatalf("roots = %v", doc.Roots)
	}
	n := doc.Nodes[doc.Roots[0]]
	if n.Mesh != 0 || n.Name != "tri" {
		t.Fatalf("node = %+v", n)
	}
	if got := n.Local.MulPoint(core.V3(1, 0, 0)); got.X != 7 {
		t.Fatalf("TRS compose: %v, want x=7", got)
	}
}

func TestParseDuck(t *testing.T) {
	path := filepath.Join("..", "..", "build", "assets", "Duck.glb")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skip("Duck.glb not fetched; run scripts/fetch-assets.sh")
	}
	doc, err := Parse(data, nil)
	if err != nil {
		t.Fatalf("Parse(Duck): %v", err)
	}
	if len(doc.Meshes) == 0 || len(doc.Meshes[0].Primitives) == 0 {
		t.Fatal("duck has no mesh")
	}
	p := doc.Meshes[0].Primitives[0]
	if len(p.Positions) < 1000 || len(p.Indices) < 3000 {
		t.Fatalf("duck geometry suspicious: %d verts %d indices", len(p.Positions), len(p.Indices))
	}
	if len(p.UVs) != len(p.Positions) || len(p.Normals) != len(p.Positions) {
		t.Fatalf("duck attribute counts mismatch: pos=%d uv=%d n=%d",
			len(p.Positions), len(p.UVs), len(p.Normals))
	}
	if doc.Materials[p.Material].Image == nil {
		t.Fatal("duck base color texture missing")
	}
	if len(doc.Roots) == 0 {
		t.Fatal("duck has no scene roots")
	}
}

// TestParseExtras checks application metadata survives verbatim: node, mesh,
// material and scene extras, the asset header, and the material flags.
func TestParseExtras(t *testing.T) {
	doc, err := Parse(buildTestGLB(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(doc.Nodes[0].Extras); got != `{"type":"ROOM","hoverable":true}` {
		t.Errorf("node extras = %s", got)
	}
	if got := string(doc.Meshes[0].Extras); got != `{"lod":0}` {
		t.Errorf("mesh extras = %s", got)
	}
	if got := string(doc.SceneExtras); got != `{"units":"m"}` || doc.SceneName != "main" {
		t.Errorf("scene = %q %s", doc.SceneName, got)
	}
	if doc.Asset.Generator != "test" || doc.Asset.Version != "2.0" {
		t.Errorf("asset = %+v", doc.Asset)
	}
	m := doc.Materials[0]
	if !m.DoubleSided || m.AlphaMode != "BLEND" || m.AlphaCutoff != 0.5 || m.Extras != nil {
		t.Errorf("material = %+v", m)
	}
}
