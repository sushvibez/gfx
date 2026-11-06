package gltf

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

// pngOf encodes a 1x1 PNG of the colour.
func pngOf(c color.RGBA) []byte {
	im := image.NewRGBA(image.Rect(0, 0, 1, 1))
	im.Set(0, 0, c)
	var b bytes.Buffer
	png.Encode(&b, im)
	return b.Bytes()
}

// TestParseMaterialMaps loads a one-triangle .gltf whose material carries
// every map glTF defines, plus TANGENT, and checks each lands on the
// Material / Primitive with its factor.
func TestParseMaterialMaps(t *testing.T) {
	// 3 positions, 3 normals, 3 uvs, 3 tangents (vec4), 3 indices (u16).
	var bin bytes.Buffer
	put := func(fs ...float32) {
		for _, f := range fs {
			var b [4]byte
			u := math.Float32bits(f)
			b[0], b[1], b[2], b[3] = byte(u), byte(u>>8), byte(u>>16), byte(u>>24)
			bin.Write(b[:])
		}
	}
	put(0, 0, 0, 1, 0, 0, 0, 1, 0)            // positions  (36 bytes @0)
	put(0, 0, 1, 0, 0, 1, 0, 0, 1)            // normals    (36 @36)
	put(0, 0, 1, 0, 0, 1)                     // uvs        (24 @72)
	put(1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, -1)  // tangents   (48 @96)
	bin.Write([]byte{0, 0, 1, 0, 2, 0, 0, 0}) // indices    (8 @144, padded)
	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin.Bytes())
	doc := `{
	 "asset":{"version":"2.0"},
	 "scene":0,"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],
	 "buffers":[{"byteLength":152,"uri":"` + uri + `"}],
	 "bufferViews":[
	  {"buffer":0,"byteOffset":0,"byteLength":36},
	  {"buffer":0,"byteOffset":36,"byteLength":36},
	  {"buffer":0,"byteOffset":72,"byteLength":24},
	  {"buffer":0,"byteOffset":96,"byteLength":48},
	  {"buffer":0,"byteOffset":144,"byteLength":6}],
	 "accessors":[
	  {"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},
	  {"bufferView":1,"componentType":5126,"count":3,"type":"VEC3"},
	  {"bufferView":2,"componentType":5126,"count":3,"type":"VEC2"},
	  {"bufferView":3,"componentType":5126,"count":3,"type":"VEC4"},
	  {"bufferView":4,"componentType":5123,"count":3,"type":"SCALAR"}],
	 "meshes":[{"primitives":[{"attributes":{"POSITION":0,"NORMAL":1,"TEXCOORD_0":2,"TANGENT":3},"indices":4,"material":0}]}],
	 "images":[{"uri":"albedo.png"},{"uri":"normal.png"},{"uri":"mr.png"},{"uri":"emissive.png"}],
	 "textures":[{"source":0},{"source":1},{"source":2},{"source":3}],
	 "materials":[{"name":"m",
	  "pbrMetallicRoughness":{"baseColorTexture":{"index":0},"metallicRoughnessTexture":{"index":2},"metallicFactor":0.25,"roughnessFactor":0.5},
	  "normalTexture":{"index":1,"scale":0.8},
	  "occlusionTexture":{"index":2,"strength":0.6},
	  "emissiveTexture":{"index":3},"emissiveFactor":[1,0.5,0]}]
	}`
	files := map[string][]byte{
		"albedo.png":   pngOf(color.RGBA{200, 10, 10, 255}),
		"normal.png":   pngOf(color.RGBA{128, 128, 255, 255}),
		"mr.png":       pngOf(color.RGBA{90, 100, 110, 255}),
		"emissive.png": pngOf(color.RGBA{0, 0, 255, 255}),
	}
	d, err := Parse([]byte(doc), func(uri string) ([]byte, error) { return files[uri], nil })
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	m := d.Materials[0]
	if m.Image == nil || m.NormalImage == nil || m.MetallicRoughnessImage == nil || m.OcclusionImage == nil || m.EmissiveImage == nil {
		t.Fatalf("maps missing: albedo=%v normal=%v mr=%v occ=%v emissive=%v",
			m.Image != nil, m.NormalImage != nil, m.MetallicRoughnessImage != nil, m.OcclusionImage != nil, m.EmissiveImage != nil)
	}
	if m.NormalImage.Pix[2] != 255 {
		t.Errorf("normal map not the decoded image: %v", m.NormalImage.Pix[:4])
	}
	if m.NormalScale != 0.8 || m.OcclusionStrength != 0.6 || m.EmissiveFactor != [3]float32{1, 0.5, 0} {
		t.Errorf("factors: normalScale=%v occlusion=%v emissive=%v", m.NormalScale, m.OcclusionStrength, m.EmissiveFactor)
	}
	if m.Metallic != 0.25 || m.Roughness != 0.5 {
		t.Errorf("metallic=%v roughness=%v", m.Metallic, m.Roughness)
	}
	p := d.Meshes[0].Primitives[0]
	if len(p.Tangents) != 3 || p.Tangents[2].W != -1 || p.Tangents[0].X != 1 {
		t.Errorf("tangents: %+v", p.Tangents)
	}
}
