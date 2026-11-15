package gltf

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// malformedDoc is a .gltf with one 36-byte embedded buffer (three vec3s)
// around the given accessors, buffer views and extra top-level members.
func malformedDoc(accessors, views, extra string) []byte {
	buf := base64.StdEncoding.EncodeToString(make([]byte, 36))
	if extra != "" {
		extra = "," + extra
	}
	return []byte(fmt.Sprintf(`{"asset":{"version":"2.0"},
"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],
"accessors":[%s],
"bufferViews":[%s],
"buffers":[{"byteLength":36,"uri":"data:application/octet-stream;base64,%s"}]%s}`,
		accessors, views, buf, extra))
}

// An asset is input from outside: a reference that points nowhere, a range
// that runs off its buffer or a count that cannot be is an error from
// Parse, never a panic in the loader.
func TestParseRejectsMalformedReferences(t *testing.T) {
	const view = `{"buffer":0,"byteOffset":0,"byteLength":36}`
	const acc = `{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}`
	cases := []struct {
		name string
		data []byte
	}{
		{"buffer view index past the end", malformedDoc(`{"bufferView":10,"componentType":5126,"count":3,"type":"VEC3"}`, ``, "")},
		{"negative buffer view index", malformedDoc(`{"bufferView":-1,"componentType":5126,"count":3,"type":"VEC3"}`, view, "")},
		{"buffer index past the end", malformedDoc(acc, `{"buffer":3,"byteOffset":0,"byteLength":36}`, "")},
		{"negative buffer index", malformedDoc(acc, `{"buffer":-1,"byteOffset":0,"byteLength":36}`, "")},
		{"negative count", malformedDoc(`{"bufferView":0,"componentType":5126,"count":-2,"type":"VEC3"}`, view, "")},
		{"negative accessor offset", malformedDoc(`{"bufferView":0,"byteOffset":-4,"componentType":5126,"count":3,"type":"VEC3"}`, view, "")},
		{"negative view offset", malformedDoc(acc, `{"buffer":0,"byteOffset":-12,"byteLength":36}`, "")},
		{"negative stride", malformedDoc(acc, `{"buffer":0,"byteOffset":0,"byteLength":36,"byteStride":-12}`, "")},
		{"view offset past the buffer", malformedDoc(`{"bufferView":0,"componentType":5126,"count":0,"type":"VEC3"}`, `{"buffer":0,"byteOffset":400,"byteLength":0}`, "")},
		{"view longer than its buffer", malformedDoc(acc, `{"buffer":0,"byteOffset":0,"byteLength":4000}`, "")},
		{"accessor longer than its view", malformedDoc(acc, `{"buffer":0,"byteOffset":0,"byteLength":24}`, "")},
		{"count that overflows", malformedDoc(`{"bufferView":0,"componentType":5126,"count":9223372036854775807,"type":"VEC3"}`, view, "")},
		{"unknown component type", malformedDoc(`{"bufferView":0,"componentType":7,"count":3,"type":"VEC3"}`, view, "")},
		{"unknown element type", malformedDoc(`{"bufferView":0,"componentType":5126,"count":3,"type":"VEC9"}`, view, "")},
		{"animation without keys", malformedDoc(acc+`,{"bufferView":0,"componentType":5126,"count":0,"type":"SCALAR"},{"bufferView":0,"componentType":5126,"count":0,"type":"VEC3"}`, view,
			`"nodes":[{}],"animations":[{"channels":[{"sampler":0,"target":{"node":0,"path":"translation"}}],"samplers":[{"input":1,"output":2}]}]`)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse panicked: %v", r)
				}
			}()
			doc, err := Parse(c.data, nil)
			if c.name == "animation without keys" {
				// Nothing to play is not an error: the channel is dropped.
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				if n := len(doc.Animations[0].Channels); n != 0 {
					t.Fatalf("%d channels kept, want the keyless one dropped", n)
				}
				return
			}
			if err == nil {
				t.Fatal("Parse accepted it")
			}
		})
	}
}

// An accessor with no elements is legal and reads as empty.
func TestParseEmptyAccessor(t *testing.T) {
	doc, err := Parse(malformedDoc(`{"bufferView":0,"componentType":5126,"count":0,"type":"VEC3"}`,
		`{"buffer":0,"byteOffset":0,"byteLength":36,"byteStride":24}`, ""), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if n := len(doc.Meshes[0].Primitives[0].Positions); n != 0 {
		t.Fatalf("%d positions, want 0", n)
	}
}

// A texture that cannot be read leaves its material without the image
// (see document); a broken reference must take that path too.
func TestParseSurvivesMalformedImageReferences(t *testing.T) {
	const view = `{"buffer":0,"byteOffset":0,"byteLength":36}`
	const acc = `{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}`
	const mat = `"materials":[{"pbrMetallicRoughness":{"baseColorTexture":{"index":0}}}],"textures":[{"source":%s}],"images":[%s]`
	cases := []struct {
		name        string
		views       string
		src, images string
	}{
		{"image index past the end", view, "5", `{"uri":"a.png"}`},
		{"negative image index", view, "-1", `{"uri":"a.png"}`},
		{"image buffer view past the end", view, "0", `{"bufferView":9}`},
		{"image buffer view on a missing buffer", view + `,{"buffer":7,"byteOffset":0,"byteLength":4}`, "0", `{"bufferView":1}`},
		{"image bytes past the buffer", view + `,{"buffer":0,"byteOffset":30,"byteLength":400}`, "0", `{"bufferView":1}`},
		{"image bytes at a negative offset", view + `,{"buffer":0,"byteOffset":-8,"byteLength":4}`, "0", `{"bufferView":1}`},
		{"image bytes of negative length", view + `,{"buffer":0,"byteOffset":8,"byteLength":-4}`, "0", `{"bufferView":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse panicked: %v", r)
				}
			}()
			doc, err := Parse(malformedDoc(acc, c.views, fmt.Sprintf(mat, c.src, c.images)), nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if doc.Materials[0].Image != nil {
				t.Fatal("material has an image from a broken reference")
			}
		})
	}
}

// A GLB whose chunk claims more bytes than the file holds is rejected.
func TestParseRejectsTruncatedGLB(t *testing.T) {
	glb := buildTestGLB(t)
	for _, n := range []int{12, 16, 20, 40, len(glb) - 1} {
		if _, err := Parse(glb[:n], nil); err == nil {
			t.Errorf("a GLB cut to %d bytes parsed", n)
		}
	}
}

// FuzzParse feeds the loader mutated assets: any outcome but a panic is
// fine.
func FuzzParse(f *testing.F) {
	const view = `{"buffer":0,"byteOffset":0,"byteLength":36}`
	f.Add(malformedDoc(`{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}`, view, ""))
	f.Add(malformedDoc(`{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}`, view,
		`"materials":[{"pbrMetallicRoughness":{"baseColorTexture":{"index":0}}}],"textures":[{"source":0}],"images":[{"bufferView":0}]`))
	f.Add([]byte(strings.Repeat(" ", 12)))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data, nil)
	})
}
