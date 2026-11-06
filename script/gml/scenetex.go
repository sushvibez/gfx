package gml

// Textured scene solids: Box { texture: "concrete.jpg"; uvScale: 2 } tiles
// the image across each face in world meters (one repeat per uvScale
// meters), so one texture serves walls of any size. The image decodes at
// build time and uploads lazily on the first SceneFrame.Draw — a missing
// file logs once and falls back to the element's flat color, keeping
// documents loadable before assets are fetched.

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
)

// sceneTex is the retained per-path texture cell.
type sceneTex struct {
	path   string
	pix    *image.RGBA // decoded at build; nil = missing/broken
	img    *render.Image3D
	err    error
	logged bool
}

// texCell loads (or returns) the cached cell for a texture path.
func (v *SceneView) texCell(source string) *sceneTex {
	if c, ok := v.texes[source]; ok {
		return c
	}
	c := &sceneTex{path: source}
	path := source
	if !filepath.IsAbs(path) && v.baseDir != "" {
		path = filepath.Join(v.baseDir, source)
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		var decoded image.Image
		decoded, _, err = image.Decode(bytes.NewReader(raw))
		if err == nil {
			b := decoded.Bounds()
			c.pix = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
			draw.Draw(c.pix, c.pix.Bounds(), decoded, b.Min, draw.Src)
		}
	}
	c.err = err
	v.texes[source] = c
	return c
}

// ensure uploads the GPU image on first use; nil = fall back to flat color.
func (c *sceneTex) ensure(r3 *render.Renderer3D) *render.Image3D {
	if c.pix == nil {
		if !c.logged {
			c.logged = true
			logf("gml: texture %q: %v", c.path, c.err)
		}
		return nil
	}
	if c.img == nil {
		b := c.pix.Bounds()
		c.img = r3.NewImage3D(b.Dx(), b.Dy(), c.pix.Pix)
	}
	return c.img
}

// boxMeshKey quantizes size/uvScale (mm) — boxes with equal face UV extents
// share one mesh.
type boxMeshKey struct{ x, y, z int32 }

// sceneBoxMesh is a unit cube whose per-face UVs bake in the box's world
// dimensions (in uvScale units), built lazily like the texture.
type sceneBoxMesh struct {
	verts []render.Vertex3D
	idx   []uint32
	mesh  *render.Mesh
}

func (v *SceneView) boxMeshCell(size core.Vec3, uvScale float32) *sceneBoxMesh {
	if uvScale <= 0 {
		uvScale = 1
	}
	u := size.Scale(1 / uvScale) // face UV extents
	key := boxMeshKey{int32(u.X * 1000), int32(u.Y * 1000), int32(u.Z * 1000)}
	if c, ok := v.boxMeshes[key]; ok {
		return c
	}
	c := &sceneBoxMesh{}
	c.verts, c.idx = buildTiledCube(u)
	v.boxMeshes[key] = c
	return c
}

func (c *sceneBoxMesh) ensure(r3 *render.Renderer3D) *render.Mesh {
	if c.mesh == nil {
		c.mesh = r3.NewMesh(c.verts, c.idx)
	}
	return c.mesh
}

// buildTiledCube is the renderer's unit cube with UVs: each face spans its
// world extent in tiles (u = size/uvScale), so the texture repeats at a
// constant world-space density on every face.
func buildTiledCube(u core.Vec3) ([]render.Vertex3D, []uint32) {
	type face struct {
		n          core.Vec3
		a, b, c, d core.Vec3
		tu, tv     float32 // UV extent across the face (a→b, a→d)
	}
	h := float32(0.5)
	faces := []face{
		{core.V3(0, 0, 1), core.V3(-h, -h, h), core.V3(h, -h, h), core.V3(h, h, h), core.V3(-h, h, h), u.X, u.Y},
		{core.V3(0, 0, -1), core.V3(h, -h, -h), core.V3(-h, -h, -h), core.V3(-h, h, -h), core.V3(h, h, -h), u.X, u.Y},
		{core.V3(1, 0, 0), core.V3(h, -h, h), core.V3(h, -h, -h), core.V3(h, h, -h), core.V3(h, h, h), u.Z, u.Y},
		{core.V3(-1, 0, 0), core.V3(-h, -h, -h), core.V3(-h, -h, h), core.V3(-h, h, h), core.V3(-h, h, -h), u.Z, u.Y},
		{core.V3(0, 1, 0), core.V3(-h, h, h), core.V3(h, h, h), core.V3(h, h, -h), core.V3(-h, h, -h), u.X, u.Z},
		{core.V3(0, -1, 0), core.V3(-h, -h, -h), core.V3(h, -h, -h), core.V3(h, -h, h), core.V3(-h, -h, h), u.X, u.Z},
	}
	var verts []render.Vertex3D
	var idx []uint32
	for _, f := range faces {
		base := uint32(len(verts))
		// UVs walk the quad a(0,tv) b(tu,tv) c(tu,0) d(0,0): V decreases
		// upward so textures stand upright on walls.
		uvs := [4][2]float32{{0, f.tv}, {f.tu, f.tv}, {f.tu, 0}, {0, 0}}
		for i, p := range []core.Vec3{f.a, f.b, f.c, f.d} {
			verts = append(verts, render.Vertex3D{Pos: p, Normal: f.n, U: uvs[i][0], V: uvs[i][1]})
		}
		idx = append(idx, base, base+1, base+2, base, base+2, base+3)
	}
	return verts, idx
}
