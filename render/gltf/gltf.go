// Package gltf loads glTF 2.0 assets (.glb binary and .gltf JSON) into
// renderer-agnostic CPU data: indexed triangle primitives, materials with
// decoded base-color images, and the node hierarchy with local transforms.
// The render package turns a Document into GPU meshes; this package only
// parses.
//
// v0 scope: triangle primitives, position/normal/texcoord attributes,
// uint16/uint32 indices, PBR base color (factor + texture), PNG/JPEG
// images, node TRS or matrix transforms. Skins, animations and the rest
// of the spec come later.
package gltf

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"

	"github.com/sushvibez/gfx/core"
)

// Document is a parsed asset.
type Document struct {
	Meshes     []Mesh
	Materials  []Material
	Nodes      []Node
	Roots      []int // root node indices of the default scene
	Animations []Animation
	Skins      []Skin

	Asset Asset
	// SceneName and SceneExtras describe the default scene.
	SceneName   string
	SceneExtras json.RawMessage
	// Extras is the document's top-level extras.
	Extras json.RawMessage
}

// Asset is the glTF asset header.
type Asset struct {
	Generator string
	Version   string
	Copyright string
	Extras    json.RawMessage
}

// Mesh is a named group of primitives.
type Mesh struct {
	Name       string
	Primitives []Primitive
	Extras     json.RawMessage // the mesh's extras, verbatim (nil if none)
}

// Primitive is one indexed triangle list.
type Primitive struct {
	Positions []core.Vec3
	Normals   []core.Vec3  // empty if the asset has none
	UVs       []core.Vec2  // empty if the asset has none
	Tangents  []core.Vec4  // TANGENT (xyz + handedness in w), empty if the asset has none
	Joints    [][4]uint16  // JOINTS_0, empty when unskinned
	Weights   [][4]float32 // WEIGHTS_0, normalized quads
	Indices   []uint32
	Material  int // -1 if none
}

// Material is the glTF metallic-roughness material: the factors and the
// decoded textures. Every image is nil when the material has none (or the
// texture could not be decoded).
type Material struct {
	Name      string
	BaseColor [4]float32  // factor (defaults to 1,1,1,1)
	Image     *image.RGBA // decoded base color texture, nil if none
	Metallic  float32
	Roughness float32

	// NormalImage is the tangent-space normal map (glTF: OpenGL +Y
	// convention, the one render.Material expects) and NormalScale its
	// strength (default 1).
	NormalImage *image.RGBA
	NormalScale float32
	// MetallicRoughnessImage packs roughness in G and metallic in B, the
	// same layout as render.Material's ARM. OcclusionImage carries
	// ambient occlusion in R — often the very same image (an ORM map).
	MetallicRoughnessImage *image.RGBA
	OcclusionImage         *image.RGBA
	OcclusionStrength      float32 // default 1
	// EmissiveImage is sRGB-encoded emission; EmissiveFactor scales it
	// (glTF's default is 0,0,0: a texture without a factor emits nothing).
	EmissiveImage  *image.RGBA
	EmissiveFactor [3]float32

	DoubleSided bool
	AlphaMode   string  // "OPAQUE" (default), "MASK" or "BLEND"
	AlphaCutoff float32 // MASK threshold (default 0.5)
	Extras      json.RawMessage
}

// Node mirrors the glTF node hierarchy. Local is the composed matrix; the
// decomposed pose (T/R/S, HasTRS) is retained for animation — channels write
// it and RecomputeLocal refreshes Local. Matrix-only nodes have HasTRS false
// until an animation adopts them.
type Node struct {
	Name     string
	Mesh     int // -1 if none
	Skin     int // -1 if none (index into Document.Skins)
	Children []int
	Local    core.Mat4
	// Extras is the node's application metadata (glTF "extras", which
	// Blender fills from custom properties), verbatim JSON; nil if none.
	Extras json.RawMessage

	T      core.Vec3
	R      core.Quat
	S      core.Vec3
	HasTRS bool
}

// RecomputeLocal rebuilds Local from the decomposed pose.
func (n *Node) RecomputeLocal() {
	if !n.HasTRS {
		return
	}
	m := core.Mat4Scale(n.S)
	m = n.R.Mat4().Mul(m)
	n.Local = core.Mat4Translate(n.T).Mul(m)
}

// ---- raw JSON schema (the subset we read) ----

type jsonDoc struct {
	Scene *int `json:"scene"`
	Asset struct {
		Generator string          `json:"generator"`
		Version   string          `json:"version"`
		Copyright string          `json:"copyright"`
		Extras    json.RawMessage `json:"extras"`
	} `json:"asset"`
	Extras json.RawMessage `json:"extras"`
	Scenes []struct {
		Name   string          `json:"name"`
		Nodes  []int           `json:"nodes"`
		Extras json.RawMessage `json:"extras"`
	} `json:"scenes"`
	Nodes []struct {
		Name        string          `json:"name"`
		Extras      json.RawMessage `json:"extras"`
		Mesh        *int            `json:"mesh"`
		Skin        *int            `json:"skin"`
		Children    []int           `json:"children"`
		Matrix      *[16]float32    `json:"matrix"`
		Translation *[3]float32     `json:"translation"`
		Rotation    *[4]float32     `json:"rotation"`
		Scale       *[3]float32     `json:"scale"`
	} `json:"nodes"`
	Animations []struct {
		Name     string `json:"name"`
		Channels []struct {
			Sampler int `json:"sampler"`
			Target  struct {
				Node int    `json:"node"`
				Path string `json:"path"`
			} `json:"target"`
		} `json:"channels"`
		Samplers []struct {
			Input         int    `json:"input"`
			Output        int    `json:"output"`
			Interpolation string `json:"interpolation"`
		} `json:"samplers"`
	} `json:"animations"`
	Skins []struct {
		Joints              []int  `json:"joints"`
		InverseBindMatrices *int   `json:"inverseBindMatrices"`
		Skeleton            *int   `json:"skeleton"`
		Name                string `json:"name"`
	} `json:"skins"`
	Meshes []struct {
		Name       string          `json:"name"`
		Extras     json.RawMessage `json:"extras"`
		Primitives []struct {
			Attributes map[string]int `json:"attributes"`
			Indices    *int           `json:"indices"`
			Material   *int           `json:"material"`
			Mode       *int           `json:"mode"`
		} `json:"primitives"`
	} `json:"meshes"`
	Accessors []struct {
		BufferView    *int   `json:"bufferView"`
		ByteOffset    int    `json:"byteOffset"`
		ComponentType int    `json:"componentType"`
		Count         int    `json:"count"`
		Type          string `json:"type"`
	} `json:"accessors"`
	BufferViews []struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
		ByteStride int `json:"byteStride"`
	} `json:"bufferViews"`
	Buffers []struct {
		ByteLength int    `json:"byteLength"`
		URI        string `json:"uri"`
	} `json:"buffers"`
	Materials []struct {
		Name        string          `json:"name"`
		Extras      json.RawMessage `json:"extras"`
		DoubleSided bool            `json:"doubleSided"`
		AlphaMode   string          `json:"alphaMode"`
		AlphaCutoff *float32        `json:"alphaCutoff"`
		PBR         *struct {
			BaseColorFactor  *[4]float32 `json:"baseColorFactor"`
			BaseColorTexture *struct {
				Index int `json:"index"`
			} `json:"baseColorTexture"`
			MetallicFactor           *float32 `json:"metallicFactor"`
			RoughnessFactor          *float32 `json:"roughnessFactor"`
			MetallicRoughnessTexture *struct {
				Index int `json:"index"`
			} `json:"metallicRoughnessTexture"`
		} `json:"pbrMetallicRoughness"`
		NormalTexture *struct {
			Index int      `json:"index"`
			Scale *float32 `json:"scale"`
		} `json:"normalTexture"`
		OcclusionTexture *struct {
			Index    int      `json:"index"`
			Strength *float32 `json:"strength"`
		} `json:"occlusionTexture"`
		EmissiveTexture *struct {
			Index int `json:"index"`
		} `json:"emissiveTexture"`
		EmissiveFactor *[3]float32 `json:"emissiveFactor"`
		// KHR_materials_pbrSpecularGlossiness (deprecated but common in
		// older Sketchfab exports): its diffuse stands in for base colour.
		Extensions *struct {
			SpecGloss *struct {
				DiffuseFactor  *[4]float32 `json:"diffuseFactor"`
				DiffuseTexture *struct {
					Index int `json:"index"`
				} `json:"diffuseTexture"`
			} `json:"KHR_materials_pbrSpecularGlossiness"`
		} `json:"extensions"`
	} `json:"materials"`
	Textures []struct {
		Source *int `json:"source"`
	} `json:"textures"`
	Images []struct {
		URI        string `json:"uri"`
		BufferView *int   `json:"bufferView"`
	} `json:"images"`
}

const (
	compUint8  = 5121
	compUint16 = 5123
	compUint32 = 5125
	compFloat  = 5126
)

// Parse loads a .glb or .gltf asset. resolve fetches external resources
// (.bin buffers, image files) referenced by URI; pass nil for
// self-contained .glb files.
func Parse(data []byte, resolve func(uri string) ([]byte, error)) (*Document, error) {
	var jsonBytes, bin []byte
	if len(data) >= 12 && string(data[:4]) == "glTF" {
		var err error
		jsonBytes, bin, err = parseGLB(data)
		if err != nil {
			return nil, err
		}
	} else {
		jsonBytes = data
	}

	var j jsonDoc
	if err := json.Unmarshal(jsonBytes, &j); err != nil {
		return nil, fmt.Errorf("gltf: %w", err)
	}

	// Load buffers: index 0 may be the GLB BIN chunk; others via resolve.
	buffers := make([][]byte, len(j.Buffers))
	for i, b := range j.Buffers {
		switch {
		case b.URI == "" && i == 0 && bin != nil:
			buffers[i] = bin
		case strings.HasPrefix(b.URI, "data:"):
			// Embedded base64 buffer ("data:application/octet-stream;base64,…"),
			// the form .gltf exporters use for self-contained files.
			comma := strings.IndexByte(b.URI, ',')
			if comma < 0 || !strings.Contains(b.URI[:comma], "base64") {
				return nil, fmt.Errorf("gltf: buffer %d: unsupported data: URI encoding", i)
			}
			d, err := base64.StdEncoding.DecodeString(b.URI[comma+1:])
			if err != nil {
				return nil, fmt.Errorf("gltf: buffer %d: %w", i, err)
			}
			buffers[i] = d
		case b.URI != "":
			if resolve == nil {
				return nil, fmt.Errorf("gltf: buffer %d needs external %q but no resolver given", i, b.URI)
			}
			d, err := resolve(b.URI)
			if err != nil {
				return nil, fmt.Errorf("gltf: buffer %q: %w", b.URI, err)
			}
			buffers[i] = d
		default:
			return nil, fmt.Errorf("gltf: buffer %d has no source", i)
		}
	}

	p := &parser{j: &j, buffers: buffers, resolve: resolve}
	return p.document()
}

func parseGLB(data []byte) (jsonChunk, binChunk []byte, err error) {
	if binary.LittleEndian.Uint32(data[4:]) != 2 {
		return nil, nil, fmt.Errorf("gltf: unsupported GLB version")
	}
	off := 12
	for off+8 <= len(data) {
		clen := int(binary.LittleEndian.Uint32(data[off:]))
		ctype := string(data[off+4 : off+8])
		off += 8
		if off+clen > len(data) {
			return nil, nil, fmt.Errorf("gltf: truncated GLB chunk")
		}
		switch ctype {
		case "JSON":
			jsonChunk = data[off : off+clen]
		case "BIN\x00":
			binChunk = data[off : off+clen]
		}
		off += clen
	}
	if jsonChunk == nil {
		return nil, nil, fmt.Errorf("gltf: GLB has no JSON chunk")
	}
	return jsonChunk, binChunk, nil
}

type parser struct {
	j       *jsonDoc
	buffers [][]byte
	resolve func(string) ([]byte, error)
}

// view returns the raw bytes of accessor i plus its element stride.
func (p *parser) accessor(i int) (data []byte, stride, count, compType int, typ string, err error) {
	if i < 0 || i >= len(p.j.Accessors) {
		return nil, 0, 0, 0, "", fmt.Errorf("gltf: accessor %d out of range", i)
	}
	a := p.j.Accessors[i]
	if a.BufferView == nil {
		return nil, 0, 0, 0, "", fmt.Errorf("gltf: sparse accessors not supported")
	}
	bv := p.j.BufferViews[*a.BufferView]
	buf := p.buffers[bv.Buffer]
	elem := componentSize(a.ComponentType) * typeComponents(a.Type)
	stride = bv.ByteStride
	if stride == 0 {
		stride = elem
	}
	start := bv.ByteOffset + a.ByteOffset
	end := start + (a.Count-1)*stride + elem
	if end > len(buf) {
		return nil, 0, 0, 0, "", fmt.Errorf("gltf: accessor %d overruns buffer", i)
	}
	return buf[start:end], stride, a.Count, a.ComponentType, a.Type, nil
}

func componentSize(t int) int {
	switch t {
	case compUint16:
		return 2
	case compUint32, compFloat:
		return 4
	}
	return 1
}

func typeComponents(t string) int {
	switch t {
	case "SCALAR":
		return 1
	case "VEC2":
		return 2
	case "VEC3":
		return 3
	case "VEC4":
		return 4
	case "MAT4":
		return 16
	}
	return 1
}

func (p *parser) floats(i, comps int) ([]float32, error) {
	data, stride, count, ct, typ, err := p.accessor(i)
	if err != nil {
		return nil, err
	}
	if ct != compFloat || typeComponents(typ) != comps {
		return nil, fmt.Errorf("gltf: accessor %d: want %d floats, got type %s/%d", i, comps, typ, ct)
	}
	out := make([]float32, count*comps)
	for e := range count {
		base := e * stride
		for c := range comps {
			out[e*comps+c] = math.Float32frombits(binary.LittleEndian.Uint32(data[base+c*4:]))
		}
	}
	return out, nil
}

func (p *parser) indices(i int) ([]uint32, error) {
	data, stride, count, ct, _, err := p.accessor(i)
	if err != nil {
		return nil, err
	}
	out := make([]uint32, count)
	for e := range count {
		base := e * stride
		switch ct {
		case compUint16:
			out[e] = uint32(binary.LittleEndian.Uint16(data[base:]))
		case compUint32:
			out[e] = binary.LittleEndian.Uint32(data[base:])
		default:
			return nil, fmt.Errorf("gltf: unsupported index component type %d", ct)
		}
	}
	return out, nil
}

func (p *parser) document() (*Document, error) {
	doc := &Document{}

	for mi, m := range p.j.Meshes {
		mesh := Mesh{Name: m.Name, Extras: m.Extras}
		for pi, prim := range m.Primitives {
			if prim.Mode != nil && *prim.Mode != 4 {
				return nil, fmt.Errorf("gltf: mesh %d primitive %d: only triangles supported", mi, pi)
			}
			out := Primitive{Material: -1}
			if prim.Material != nil {
				out.Material = *prim.Material
			}
			posIdx, ok := prim.Attributes["POSITION"]
			if !ok {
				return nil, fmt.Errorf("gltf: mesh %d primitive %d has no POSITION", mi, pi)
			}
			pos, err := p.floats(posIdx, 3)
			if err != nil {
				return nil, err
			}
			out.Positions = vec3s(pos)
			if ni, ok := prim.Attributes["NORMAL"]; ok {
				n, err := p.floats(ni, 3)
				if err != nil {
					return nil, err
				}
				out.Normals = vec3s(n)
			}
			if ti, ok := prim.Attributes["TEXCOORD_0"]; ok {
				uv, err := p.floats(ti, 2)
				if err != nil {
					return nil, err
				}
				out.UVs = make([]core.Vec2, len(uv)/2)
				for k := range out.UVs {
					out.UVs[k] = core.V2(uv[k*2], uv[k*2+1])
				}
			}
			if ti, ok := prim.Attributes["TANGENT"]; ok {
				tn, err := p.floats(ti, 4)
				if err != nil {
					return nil, err
				}
				out.Tangents = make([]core.Vec4, len(tn)/4)
				for k := range out.Tangents {
					out.Tangents[k] = core.Vec4{X: tn[k*4], Y: tn[k*4+1], Z: tn[k*4+2], W: tn[k*4+3]}
				}
			}
			if ji, ok := prim.Attributes["JOINTS_0"]; ok {
				j, err := p.joints4(ji)
				if err != nil {
					return nil, err
				}
				out.Joints = j
			}
			if wi, ok := prim.Attributes["WEIGHTS_0"]; ok {
				w, err := p.weights4(wi)
				if err != nil {
					return nil, err
				}
				out.Weights = w
			}
			if prim.Indices != nil {
				idx, err := p.indices(*prim.Indices)
				if err != nil {
					return nil, err
				}
				out.Indices = idx
			} else {
				out.Indices = make([]uint32, len(out.Positions))
				for k := range out.Indices {
					out.Indices[k] = uint32(k)
				}
			}
			mesh.Primitives = append(mesh.Primitives, out)
		}
		doc.Meshes = append(doc.Meshes, mesh)
	}

	for _, m := range p.j.Materials {
		mat := Material{Name: m.Name, BaseColor: [4]float32{1, 1, 1, 1}, Metallic: 1, Roughness: 1, NormalScale: 1, OcclusionStrength: 1,
			DoubleSided: m.DoubleSided, AlphaMode: m.AlphaMode, AlphaCutoff: 0.5, Extras: m.Extras}
		if mat.AlphaMode == "" {
			mat.AlphaMode = "OPAQUE"
		}
		if m.AlphaCutoff != nil {
			mat.AlphaCutoff = *m.AlphaCutoff
		}
		if m.PBR != nil {
			if m.PBR.BaseColorFactor != nil {
				mat.BaseColor = *m.PBR.BaseColorFactor
			}
			if m.PBR.MetallicFactor != nil {
				mat.Metallic = *m.PBR.MetallicFactor
			}
			if m.PBR.RoughnessFactor != nil {
				mat.Roughness = *m.PBR.RoughnessFactor
			}
			if m.PBR.BaseColorTexture != nil {
				// A texture we cannot decode (a source only in an extension
				// such as EXT_texture_webp, or an unsupported codec) leaves
				// the material with its base-colour factor rather than
				// failing the whole asset.
				if img, err := p.textureImage(m.PBR.BaseColorTexture.Index); err == nil {
					mat.Image = img
				}
			}
			if m.PBR.MetallicRoughnessTexture != nil {
				if img, err := p.textureImage(m.PBR.MetallicRoughnessTexture.Index); err == nil {
					mat.MetallicRoughnessImage = img
				}
			}
		}
		if m.NormalTexture != nil {
			if img, err := p.textureImage(m.NormalTexture.Index); err == nil {
				mat.NormalImage = img
			}
			if m.NormalTexture.Scale != nil {
				mat.NormalScale = *m.NormalTexture.Scale
			}
		}
		if m.OcclusionTexture != nil {
			if img, err := p.textureImage(m.OcclusionTexture.Index); err == nil {
				mat.OcclusionImage = img
			}
			if m.OcclusionTexture.Strength != nil {
				mat.OcclusionStrength = *m.OcclusionTexture.Strength
			}
		}
		if m.EmissiveTexture != nil {
			if img, err := p.textureImage(m.EmissiveTexture.Index); err == nil {
				mat.EmissiveImage = img
			}
		}
		if m.EmissiveFactor != nil {
			mat.EmissiveFactor = *m.EmissiveFactor
		}
		if m.PBR == nil && m.Extensions != nil && m.Extensions.SpecGloss != nil {
			sg := m.Extensions.SpecGloss
			if sg.DiffuseFactor != nil {
				mat.BaseColor = *sg.DiffuseFactor
			}
			if sg.DiffuseTexture != nil {
				if img, err := p.textureImage(sg.DiffuseTexture.Index); err == nil {
					mat.Image = img
				}
			}
			mat.Metallic, mat.Roughness = 0, 0.6
		}
		doc.Materials = append(doc.Materials, mat)
	}

	for _, n := range p.j.Nodes {
		node := Node{Name: n.Name, Mesh: -1, Skin: -1, Children: n.Children, Local: nodeMatrix(n.Matrix, n.Translation, n.Rotation, n.Scale), Extras: n.Extras}
		if n.Mesh != nil {
			node.Mesh = *n.Mesh
		}
		if n.Skin != nil {
			node.Skin = *n.Skin
		}
		// Retain the decomposed pose for animation (matrix-only nodes stay
		// HasTRS=false until a channel adopts them).
		if n.Matrix == nil {
			node.T, node.R, node.S, node.HasTRS = core.Vec3{}, core.QuatIdentity(), core.V3(1, 1, 1), true
			if n.Translation != nil {
				node.T = core.V3(n.Translation[0], n.Translation[1], n.Translation[2])
			}
			if n.Rotation != nil {
				node.R = core.Quat{X: n.Rotation[0], Y: n.Rotation[1], Z: n.Rotation[2], W: n.Rotation[3]}
			}
			if n.Scale != nil {
				node.S = core.V3(n.Scale[0], n.Scale[1], n.Scale[2])
			}
		}
		doc.Nodes = append(doc.Nodes, node)
	}

	sceneIdx := 0
	if p.j.Scene != nil {
		sceneIdx = *p.j.Scene
	}
	if sceneIdx < len(p.j.Scenes) {
		sc := p.j.Scenes[sceneIdx]
		doc.Roots = sc.Nodes
		doc.SceneName, doc.SceneExtras = sc.Name, sc.Extras
	}
	a := p.j.Asset
	doc.Asset = Asset{Generator: a.Generator, Version: a.Version, Copyright: a.Copyright, Extras: a.Extras}
	doc.Extras = p.j.Extras
	anims, err := p.parseAnimations()
	if err != nil {
		return nil, err
	}
	doc.Animations = anims
	skins, err := p.parseSkins()
	if err != nil {
		return nil, err
	}
	doc.Skins = skins
	return doc, nil
}

func vec3s(f []float32) []core.Vec3 {
	out := make([]core.Vec3, len(f)/3)
	for i := range out {
		out[i] = core.V3(f[i*3], f[i*3+1], f[i*3+2])
	}
	return out
}

func nodeMatrix(m *[16]float32, t *[3]float32, r *[4]float32, s *[3]float32) core.Mat4 {
	if m != nil {
		return core.Mat4(*m) // glTF matrices are column-major, like ours
	}
	out := core.Mat4Identity()
	if s != nil {
		out = core.Mat4Scale(core.V3(s[0], s[1], s[2]))
	}
	if r != nil {
		q := core.Quat{X: r[0], Y: r[1], Z: r[2], W: r[3]}
		out = q.Mat4().Mul(out)
	}
	if t != nil {
		out = core.Mat4Translate(core.V3(t[0], t[1], t[2])).Mul(out)
	}
	return out
}

func (p *parser) textureImage(texIdx int) (*image.RGBA, error) {
	if texIdx < 0 || texIdx >= len(p.j.Textures) || p.j.Textures[texIdx].Source == nil {
		return nil, fmt.Errorf("gltf: texture %d has no source", texIdx)
	}
	img := p.j.Images[*p.j.Textures[texIdx].Source]
	var raw []byte
	switch {
	case img.BufferView != nil:
		bv := p.j.BufferViews[*img.BufferView]
		raw = p.buffers[bv.Buffer][bv.ByteOffset : bv.ByteOffset+bv.ByteLength]
	case img.URI != "":
		if p.resolve == nil {
			return nil, fmt.Errorf("gltf: image %q needs a resolver", img.URI)
		}
		d, err := p.resolve(img.URI)
		if err != nil {
			return nil, err
		}
		raw = d
	default:
		return nil, fmt.Errorf("gltf: image has no source")
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("gltf: decoding image: %w", err)
	}
	rgba := image.NewRGBA(decoded.Bounds())
	draw.Draw(rgba, rgba.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	return rgba, nil
}
