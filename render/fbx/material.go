package fbx

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/jpeg" // embedded texture payloads
	_ "image/png"
	"math"
	"strings"

	"github.com/sushvibez/gfx/render/gltf"
)

// Materials resolve through the object graph: a Model's materials are its
// OO-connected Material children (connection order = the slot order that
// LayerElementMaterial indices address); a material's base-color texture is
// the Texture OP-connected on "DiffuseColor"; the texture's pixels are the
// Video OO-connected to it — the embedded Content blob (PNG/JPEG) when
// present, otherwise the caller's resolve callback is tried with each path
// the Texture/Video records carry (relative, absolute, bare basename).

// matCache dedups gltf materials (and decoded texture images) across the
// models of one document — actors often share textures between materials.
type matCache struct {
	matIdx  map[int64]int         // Material object id -> doc.Materials index
	images  map[int64]*image.RGBA // Texture object id -> decoded pixels
	resolve func(uri string) ([]byte, error)
}

func newMatCache(resolve func(uri string) ([]byte, error)) *matCache {
	return &matCache{matIdx: map[int64]int{}, images: map[int64]*image.RGBA{}, resolve: resolve}
}

// slots returns the gltf material index per slot for one Model object,
// appending newly-seen materials to doc.Materials.
func (mc *matCache) slots(g *graph, modelID int64, doc *gltf.Document) []int {
	var out []int
	for _, cid := range g.parent[modelID] {
		o := g.objs[cid]
		if o == nil || o.node.Name != "Material" {
			continue
		}
		idx, ok := mc.matIdx[cid]
		if !ok {
			idx = len(doc.Materials)
			doc.Materials = append(doc.Materials, mc.build(g, o))
			mc.matIdx[cid] = idx
		}
		out = append(out, idx)
	}
	return out
}

// build converts one FBX Material (Phong subset) to the engine's PBR
// base-color material.
func (mc *matCache) build(g *graph, o *object) gltf.Material {
	m := gltf.Material{Name: o.name, BaseColor: [4]float32{1, 1, 1, 1}, Roughness: 0.8}
	p := prop70(o.node)
	if s, ok := p["ShininessExponent"]; ok {
		m.Roughness = shininessToRoughness(s[0])
	} else if s, ok := p["Shininess"]; ok {
		m.Roughness = shininessToRoughness(s[0])
	}
	if m.Image = mc.diffuseImage(g, o.id); m.Image != nil {
		// The shader multiplies BaseColor into the texture — leave it white.
		return m
	}
	if c, ok := p["DiffuseColor"]; ok {
		f := 1.0
		if fv, ok := p["DiffuseFactor"]; ok {
			f = fv[0]
		}
		m.BaseColor = [4]float32{
			float32(c[0] * f), float32(c[1] * f), float32(c[2] * f), 1}
	}
	return m
}

// shininessToRoughness maps a Blinn-Phong exponent to GGX roughness
// (the standard sqrt(2/(s+2)) fit).
func shininessToRoughness(s float64) float32 {
	if s < 0 {
		s = 0
	}
	return float32(math.Min(1, math.Sqrt(2/(s+2))))
}

// diffuseImage finds the material's DiffuseColor texture and decodes its
// embedded Video content. Nil when absent or external-only.
func (mc *matCache) diffuseImage(g *graph, matID int64) *image.RGBA {
	for _, l := range g.opChildren[matID] {
		t := g.objs[l.parent] // l.parent = the OP child (the texture)
		if t == nil || t.node.Name != "Texture" || l.prop != "DiffuseColor" {
			continue
		}
		if img, ok := mc.images[t.id]; ok {
			return img
		}
		img := mc.textureImage(g, t)
		mc.images[t.id] = img // cache misses too: don't re-decode failures
		return img
	}
	return nil
}

// textureImage decodes a texture's pixels: the embedded Content blob of its
// OO-connected Video when present, else the referenced file via resolve.
func (mc *matCache) textureImage(g *graph, tex *object) *image.RGBA {
	var files []string // recorded paths, in preference order
	addFiles := func(n *Node) {
		for _, name := range []string{"RelativeFilename", "Filename", "FileName"} {
			if c := child(n, name); c != nil && len(c.Props) > 0 && c.Props[0].Kind == 'S' {
				files = append(files, c.Props[0].S)
			}
		}
	}
	for _, cid := range g.parent[tex.id] {
		v := g.objs[cid]
		if v == nil || v.node.Name != "Video" {
			continue
		}
		if cn := child(v.node, "Content"); cn != nil {
			for _, p := range cn.Props {
				if p.Kind == 'R' && len(p.Raw) > 0 {
					if img := decodeImage(p.Raw); img != nil {
						return img
					}
				}
			}
		}
		addFiles(v.node)
	}
	addFiles(tex.node)
	if mc.resolve == nil {
		return nil
	}
	// The recorded paths, then their bare basenames (exports usually point
	// into the exporting machine's directories; the basename in the model's
	// own directory is the common survivor).
	for _, f := range files {
		files = append(files, baseName(f))
	}
	tried := map[string]bool{}
	for _, f := range files {
		if f == "" || tried[f] {
			continue
		}
		tried[f] = true
		if raw, err := mc.resolve(f); err == nil {
			if img := decodeImage(raw); img != nil {
				return img
			}
		}
	}
	return nil
}

// baseName strips directories from an FBX-recorded path (either separator:
// exports come from Windows and Unix machines alike).
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func decodeImage(raw []byte) *image.RGBA {
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	b := decoded.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), decoded, b.Min, draw.Src)
	return rgba
}
