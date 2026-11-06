package render

import (
	"fmt"
	"image"
	"strings"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render/gltf"
)

// Model is a glTF document turned into GPU resources: one draw item per
// primitive. The node hierarchy is retained (a copy of the document's
// nodes), so an AnimationPlayer can re-pose it: item world transforms are
// recomputed from node locals whenever the pose changes. Skinned primitives
// deform in the vertex shader from a joint palette (gpuskin.go), or on the
// CPU when GPUSkinning is off.
type Model struct {
	items  []modelItem
	images []*Image3D
	meshes []*Mesh
	nodes  []gltf.Node // posed copy of the document hierarchy
	roots  []int
	skins  []gltf.Skin
	worlds []core.Mat4 // per-node world transforms for the current pose

	// shared marks an Instance: meshes, images and skin attribute buffers
	// belong to the base model and are not released with this one.
	shared    bool
	ownMeshes []*Mesh // CPU-path instance copies (released with the instance)

	poseDirty bool        // pose changed: palettes (or vertices) need re-upload
	palette   []core.Mat4 // scratch joint palette
	packed    []skinMat   // scratch packed palette (skinVertices)
	seen      []bool      // updateWorlds scratch (kept: the walk runs per frame)
	byName    map[string]int

	// GPU skinning (gpuskin.go): one joint palette per skin, uploaded when
	// the pose moves, instead of the whole deformed vertex buffer. gpuSkin
	// is resolved on the first draw (it needs the renderer) and latched:
	// skinReady records that the question has been asked.
	gpuSkin   bool
	skinReady bool
	palettes  []*skinPalette
	one       [1]core.Mat4 // scratch for the single-instance material path

	// Dissolve state (DrawFaded): fade is the alpha whose dissolve pattern
	// is currently resident in the GPU buffers, 1 = intact geometry.
	fade        float32
	fadeApplied bool
}

type modelItem struct {
	mesh *Mesh
	img  *Image3D // nil = untextured
	// The glTF material's other maps; an item with any of them draws
	// through the material pipeline (see draw), the base-colour-only path
	// has no slot for them.
	normal    *Image3D
	arm       *Image3D
	emissive  *Image3D
	nscale    float32
	emission  float32
	mat       *Material // cached material for the mapped path
	color     RGBA
	metallic  float32
	roughness float32
	material  int  // glTF material index, -1 if none
	double    bool // glTF doubleSided
	blend     bool // glTF alphaMode BLEND
	node      int  // owning node (world recomputed from the hierarchy)
	world     core.Mat4

	// Skinning (skin >= 0): bind-pose copy + per-vertex joint data; the
	// deformed vertices re-upload into mesh.vbuf when the pose changes.
	skin    int
	base    []Vertex3D
	joints  [][4]uint16
	weights [][4]float32
	scratch []Vertex3D

	// GPU skinning: the joint/weight vertex buffer (slot 1) and the
	// palette of the skin this primitive belongs to.
	attrs *gpu.Buffer
	pal   *skinPalette
	// fadeMat is the material DrawFaded draws through when real alpha is
	// available (see DrawFaded); its texture bind groups are cached, the
	// colour and alpha are rewritten per frame.
	fadeMat *Material

	// Dissolve scratch (DrawFaded): faded holds the punched-out copy of the
	// posed vertices, solo lists the triangles whose three vertices belong
	// to no other triangle (only those can be collapsed without dragging a
	// neighbour's corner along).
	faded    []Vertex3D
	solo     []int32
	soloDone bool
}

// BuildModel uploads a parsed glTF document.
func (r *Renderer3D) BuildModel(doc *gltf.Document) (*Model, error) {
	m := &Model{}

	upload := func(im *image.RGBA) *Image3D {
		if im == nil {
			return nil
		}
		b := im.Bounds()
		img := r.NewImage3D(b.Dx(), b.Dy(), im.Pix)
		m.images = append(m.images, img)
		return img
	}
	type matImages struct {
		albedo, normal, arm, emissive *Image3D
		normalScale, emission         float32
	}
	images := make([]matImages, len(doc.Materials))
	for i, mat := range doc.Materials {
		mi := &images[i]
		mi.albedo = upload(mat.Image)
		mi.normal = upload(mat.NormalImage)
		mi.normalScale = mat.NormalScale
		mi.arm = upload(armImage(mat.MetallicRoughnessImage, mat.OcclusionImage, mat.OcclusionStrength))
		if mat.EmissiveImage != nil {
			mi.emissive = upload(mat.EmissiveImage)
			mi.emission = max(mat.EmissiveFactor[0], mat.EmissiveFactor[1], mat.EmissiveFactor[2])
		}
	}

	// GPU mesh per primitive, interleaving the vertex streams.
	type prim struct {
		mesh      *Mesh
		color     RGBA
		metallic  float32
		roughness float32
		img       *Image3D
		normal    *Image3D
		arm       *Image3D
		emissive  *Image3D
		nscale    float32
		emission  float32
		material  int
		double    bool
		blend     bool
		verts     []Vertex3D
		joints    [][4]uint16
		weights   [][4]float32
	}
	prims := make([][]prim, len(doc.Meshes))
	for mi, mesh := range doc.Meshes {
		for _, p := range mesh.Primitives {
			verts := make([]Vertex3D, len(p.Positions))
			for vi := range verts {
				verts[vi].Pos = p.Positions[vi]
				if vi < len(p.Normals) {
					verts[vi].Normal = p.Normals[vi]
				}
				if vi < len(p.UVs) {
					verts[vi].U, verts[vi].V = p.UVs[vi].X, p.UVs[vi].Y
				}
				if vi < len(p.Tangents) {
					verts[vi].Tan = p.Tangents[vi]
				}
			}
			gm := r.NewMesh(verts, p.Indices)
			m.meshes = append(m.meshes, gm)
			pr := prim{mesh: gm, color: RGBA{1, 1, 1, 1}, roughness: 0.7, material: -1,
				joints: p.Joints, weights: p.Weights, verts: verts}
			if p.Material >= 0 && p.Material < len(doc.Materials) {
				mat := doc.Materials[p.Material]
				pr.material, pr.double, pr.blend = p.Material, mat.DoubleSided, mat.AlphaMode == "BLEND"
				pr.color = RGBA{mat.BaseColor[0], mat.BaseColor[1], mat.BaseColor[2], mat.BaseColor[3]}
				pr.metallic, pr.roughness = mat.Metallic, mat.Roughness
				mi := images[p.Material]
				pr.img, pr.normal, pr.arm, pr.emissive = mi.albedo, mi.normal, mi.arm, mi.emissive
				pr.nscale, pr.emission = mi.normalScale, mi.emission
			}
			prims[mi] = append(prims[mi], pr)
		}
	}

	// Retain the hierarchy; items record their node so a pose change can
	// re-walk. A primitive on a skinned node keeps its bind pose + joint
	// data and draws with identity (the palette carries node worlds).
	m.nodes = append(m.nodes, doc.Nodes...)
	m.roots = append(m.roots, doc.Roots...)
	m.skins = append(m.skins, doc.Skins...)
	var walkErr func(idx int) error
	walkErr = func(idx int) error {
		if idx < 0 || idx >= len(m.nodes) {
			return fmt.Errorf("render: node index %d out of range", idx)
		}
		n := m.nodes[idx]
		if n.Mesh >= 0 && n.Mesh < len(prims) {
			for _, p := range prims[n.Mesh] {
				it := modelItem{
					mesh: p.mesh, img: p.img, color: p.color,
					normal: p.normal, arm: p.arm, emissive: p.emissive,
					nscale: p.nscale, emission: p.emission,
					metallic: p.metallic, roughness: p.roughness,
					material: p.material, double: p.double, blend: p.blend,
					node: idx, skin: -1,
				}
				if n.Skin >= 0 && n.Skin < len(m.skins) &&
					len(p.joints) == len(p.verts) && len(p.weights) == len(p.verts) {
					it.skin = n.Skin
					it.base = p.verts
					it.joints = p.joints
					it.weights = p.weights
					it.scratch = make([]Vertex3D, len(p.verts))
				}
				m.items = append(m.items, it)
			}
		}
		for _, c := range n.Children {
			if err := walkErr(c); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range m.roots {
		if err := walkErr(root); err != nil {
			return nil, err
		}
	}
	if len(m.items) == 0 {
		return nil, fmt.Errorf("render: model has no drawable primitives")
	}
	m.updateWorlds()
	return m, nil
}

// updateWorlds recomputes per-node world transforms and item placements
// from the (re-posed) hierarchy — called by AnimationPlayer after sampling
// a clip. Skinned items keep identity placement (their palette holds the
// worlds) and are marked for vertex re-upload on the next Draw.
func (m *Model) updateWorlds() {
	if len(m.worlds) != len(m.nodes) {
		m.worlds = make([]core.Mat4, len(m.nodes))
		m.seen = make([]bool, len(m.nodes))
		m.poseDirty = true
	}
	seen := m.seen
	for i := range seen {
		seen[i] = false
	}
	// Track whether the pose actually moved: an unchanged pose skips the
	// re-skin and the vertex re-upload entirely (a held stance, a paused
	// player, or an actor whose clip has not ticked this frame).
	moved := false
	var walk func(idx int, parent core.Mat4)
	walk = func(idx int, parent core.Mat4) {
		if idx < 0 || idx >= len(m.nodes) {
			return
		}
		w := parent.Mul(m.nodes[idx].Local)
		if w != m.worlds[idx] {
			moved = true
		}
		m.worlds[idx] = w
		seen[idx] = true
		for _, c := range m.nodes[idx].Children {
			walk(c, w)
		}
	}
	for _, root := range m.roots {
		walk(root, core.Mat4Identity())
	}
	// Joints may live outside the default scene roots in some assets: give
	// unvisited nodes their local as world (best effort).
	for i := range m.worlds {
		if !seen[i] {
			if m.worlds[i] != m.nodes[i].Local {
				moved = true
			}
			m.worlds[i] = m.nodes[i].Local
		}
	}
	for i := range m.items {
		if s := m.items[i].skin; s >= 0 && s < len(m.skins) {
			m.items[i].world = core.Mat4Identity()
			if moved {
				m.poseDirty = true
			}
		} else {
			m.items[i].world = m.worlds[m.items[i].node]
		}
	}
}

// hasSkin reports whether any primitive is skinned.
func (m *Model) hasSkin() bool {
	for i := range m.items {
		if s := m.items[i].skin; s >= 0 && s < len(m.skins) {
			return true
		}
	}
	return false
}

// useGPUSkin reports whether this model deforms in the vertex shader,
// building its GPU skinning resources (joint/weight buffers, one palette
// per skin, the skinned pipelines) on the first call. It answers false for
// an unskinned model, when GPUSkinning is off (GFX_CPU_SKIN=1), and when
// the device refused the skinned pipelines — in each case the model falls
// back to the CPU path, which stays fully functional.
func (m *Model) useGPUSkin(r *Renderer3D) bool {
	if m.skinReady {
		return m.gpuSkin
	}
	m.skinReady = true
	if !GPUSkinning || !m.hasSkin() || !r.ensureLegacySkin() {
		return false
	}
	m.palettes = make([]*skinPalette, len(m.skins))
	for i := range m.items {
		it := &m.items[i]
		if it.skin < 0 || it.skin >= len(m.skins) {
			continue
		}
		attrs := it.attrs // an Instance shares its base model's buffer
		if attrs == nil {
			attrs = r.newSkinAttrBuffer(it.joints, it.weights)
		}
		if attrs == nil {
			continue // no joint data: this primitive stays in its bind pose
		}
		if m.palettes[it.skin] == nil {
			m.palettes[it.skin] = &skinPalette{}
		}
		it.attrs, it.pal = attrs, m.palettes[it.skin]
	}
	m.gpuSkin = true
	m.poseDirty = true
	return true
}

// applyPose re-skins dirty vertices, applies the requested dissolve level
// and uploads the result (needs the renderer's queue; called from Draw).
// Work is skipped entirely when neither the pose nor the fade changed since
// the last upload — a frozen actor costs nothing per frame.
func (m *Model) applyPose(r *Renderer3D, fade float32) {
	if m.gpuSkin {
		// GPU skinning: nothing but the joint palettes travels, and only
		// when the pose actually moved (AnimationPlayer.Advance ->
		// updateWorlds sets poseDirty). 51 joints x 64 B = 3 KB per actor,
		// against ~1 MB of vertices the CPU path re-uploads.
		if !m.poseDirty {
			return
		}
		m.poseDirty = false
		for si, p := range m.palettes {
			if p == nil {
				continue
			}
			p.scratch = jointPalette(&m.skins[si], m.worlds, p.scratch)
			p.upload(r, p.scratch)
		}
		return
	}
	if !m.fadeApplied {
		m.fade = 1
	}
	q := quantizeFade(fade)
	if !m.poseDirty && q == m.fade {
		return
	}
	m.poseDirty = false
	m.fade, m.fadeApplied = q, q < 1
	for i := range m.items {
		it := &m.items[i]
		skinned := it.skin >= 0 && it.skin < len(m.skins)
		src := it.mesh.verts
		if skinned {
			m.palette = jointPalette(&m.skins[it.skin], m.worlds, m.palette)
			m.packed = skinVertices(it.scratch, it.base, it.joints, it.weights, m.palette, m.packed)
			src = it.scratch
		} else if q >= 1 && !m.fadeApplied {
			continue // static geometry, nothing dissolved: buffer is current
		}
		if q < 1 {
			src = it.dissolve(src, q)
		}
		r.uploadVerts(it.mesh, src)
	}
}

// quantizeFade snaps an alpha to 1/64 steps so a slow corpse fade re-uploads
// 64 times over its life instead of once per frame.
func quantizeFade(a float32) float32 {
	if a >= 1 {
		return 1
	}
	if a <= 0 {
		return 0
	}
	return float32(int(a*64+0.5)) / 64
}

// dissolve copies src into the item's fade scratch with a deterministic
// fraction (1-alpha) of its collapsible triangles punched out. Only
// triangles whose three vertices are used by no other triangle can be
// collapsed — the CPU pipeline can rewrite vertices but not indices, so a
// shared corner would drag its neighbours into a sliver.
func (it *modelItem) dissolve(src []Vertex3D, alpha float32) []Vertex3D {
	it.findSolo()
	if len(it.solo) == 0 {
		return src
	}
	if len(it.faded) != len(src) {
		it.faded = make([]Vertex3D, len(src))
	}
	copy(it.faded, src)
	idx := it.mesh.idx
	for _, t := range it.solo {
		if triNoise(uint32(t)) < alpha {
			continue // survives this frame's dissolve
		}
		a, b, c := idx[int(t)*3], idx[int(t)*3+1], idx[int(t)*3+2]
		// Collapse to the centroid: a zero-area triangle rasterizes nothing.
		mid := it.faded[a].Pos.Add(it.faded[b].Pos).Add(it.faded[c].Pos).Scale(1.0 / 3)
		it.faded[a].Pos, it.faded[b].Pos, it.faded[c].Pos = mid, mid, mid
	}
	return it.faded
}

// findSolo records (once) the triangles whose vertices no other triangle
// shares. FBX/glTF character meshes split vertices per face corner for
// normals and UVs, so in practice this is most of the mesh.
func (it *modelItem) findSolo() {
	if it.soloDone {
		return
	}
	it.soloDone = true
	idx := it.mesh.idx
	if len(idx) < 3 {
		return
	}
	uses := make([]uint8, len(it.mesh.verts))
	for _, v := range idx {
		if int(v) < len(uses) && uses[v] < 2 {
			uses[v]++
		}
	}
	for t := 0; t*3+2 < len(idx); t++ {
		a, b, c := idx[t*3], idx[t*3+1], idx[t*3+2]
		if int(a) >= len(uses) || int(b) >= len(uses) || int(c) >= len(uses) {
			continue
		}
		if uses[a] == 1 && uses[b] == 1 && uses[c] == 1 {
			it.solo = append(it.solo, int32(t))
		}
	}
}

// triNoise hashes a triangle index to [0,1) — a stable dissolve pattern, so
// a fading corpse loses triangles monotonically instead of flickering.
func triNoise(t uint32) float32 {
	h := t * 2654435761
	h ^= h >> 15
	h *= 2246822519
	h ^= h >> 13
	return float32(h>>8) / float32(1<<24)
}

// Draw queues the whole model under an extra transform, re-uploading
// skinned vertices first when the pose changed.
func (m *Model) Draw(r *Renderer3D, transform core.Mat4) {
	m.draw(r, transform, RGBA{1, 1, 1, 1}, 1)
}

// DrawTinted draws the model with every material colour multiplied by tint
// — faction colours, a grey "down" state, a damage flash. The tint's alpha
// multiplies the material alpha (see DrawFaded for what alpha can do).
func (m *Model) DrawTinted(r *Renderer3D, transform core.Mat4, tint RGBA) {
	m.draw(r, transform, tint, 1)
}

// DrawFaded draws the model fading out (alpha 1 = intact, 0 = gone).
//
// This is REAL alpha: the items go through the material pipeline's blended
// variant, which the material pass already sorts back to front, so a corpse
// dissolves by going translucent rather than by losing geometry. Shading
// moves to the material shader for the faded draw (lights, fog and tone
// mapping as set by FrameSettings), which is why a full-alpha model keeps
// using the plain path.
//
// FALLBACK: with CPU skinning forced (GFX_CPU_SKIN=1 / GPUSkinning = false)
// a skinned model still deforms by re-uploading vertices, and the fade
// stays the old DISSOLVE — a stable (1-alpha) fraction of the mesh's
// collapsible triangles punched out on the CPU, the colour dimmed towards
// black. Its limits are the ones a dissolve has: triangles whose vertices
// are shared with a neighbour cannot be punched out (the CPU pipeline
// rewrites vertices, never indices), and a deep fade opens the model up so
// its interior surfaces show through.
func (m *Model) DrawFaded(r *Renderer3D, transform core.Mat4, alpha float32) {
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	dim := 0.25 + 0.75*alpha
	m.draw(r, transform, RGBA{dim, dim, dim, alpha}, alpha)
}

func (m *Model) draw(r *Renderer3D, transform core.Mat4, tint RGBA, fade float32) {
	gpuSkin := m.useGPUSkin(r)
	// Real alpha is available whenever the model does not depend on the
	// CPU dissolve: a GPU-skinned model, or any unskinned one, drawn while
	// GPU skinning is enabled. Then a fade goes through the material
	// pipeline's blended variant instead of punching out triangles.
	blend := fade < 1 && GPUSkinning && (gpuSkin || !m.hasSkin())
	cpuFade := fade
	if blend {
		cpuFade = 1 // no dissolve: the fade is real alpha
	}
	m.applyPose(r, cpuFade)
	for i := range m.items {
		it := &m.items[i]
		c := RGBA{it.color.R * tint.R, it.color.G * tint.G, it.color.B * tint.B, it.color.A * tint.A}
		world := transform.Mul(it.world)
		switch {
		case blend:
			m.one[0] = world
			r.drawInstanced(it.mesh, m.one[:], it.fadeMaterial(c, fade), it.attrs, it.pal)
		case it.mapped() && (it.pal != nil || it.skin < 0):
			// Normal / ARM / emissive maps: the material pipeline. (A
			// CPU-skinned item keeps the base path, which re-uploads
			// its deformed vertices.)
			m.one[0] = world
			r.drawInstanced(it.mesh, m.one[:], it.mappedMaterial(c), it.attrs, it.pal)
		case it.pal != nil:
			r.drawSkinnedPBR(it.mesh, world, c, it.metallic, it.roughness, it.img, it.attrs, it.pal)
		default:
			r.DrawPBR(it.mesh, world, c, it.metallic, it.roughness, it.img)
		}
	}
}

// mapped reports whether the item carries maps only the material pipeline
// can apply.
func (it *modelItem) mapped() bool {
	return it.normal != nil || it.arm != nil || it.emissive != nil
}

// material is the item's cached material for the mapped path; the colour
// is rewritten per draw (a tint), the maps and bind groups persist.
func (it *modelItem) mappedMaterial(c RGBA) *Material {
	if it.mat == nil {
		it.mat = &Material{
			Albedo: it.img, Normal: it.normal, ARM: it.arm, Emissive: it.emissive,
			Metallic: it.metallic, Roughness: it.roughness,
			NormalScale: it.nscale, Emission: it.emission, DoubleSided: it.double,
		}
	}
	f := it.mat
	f.Color = RGBA{R: c.R, G: c.G, B: c.B, A: 1}
	f.Alpha = 1
	if it.blend {
		f.Alpha = max(c.A, 1e-4)
	}
	return f
}

// armImage packs glTF's metallic-roughness (G, B) and occlusion (R)
// textures into one ARM image. Either may be missing: a missing channel
// reads 1 so the material's factors apply unchanged. When the two are the
// same image (an ORM map) it is used as it is.
func armImage(mr, occ *image.RGBA, strength float32) *image.RGBA {
	if mr == nil && occ == nil {
		return nil
	}
	if mr == occ && (strength == 0 || strength == 1) {
		return mr
	}
	src := mr
	if src == nil {
		src = occ
	}
	b := src.Bounds()
	out := image.NewRGBA(b)
	w, h := b.Dx(), b.Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := y*out.Stride + x*4
			g, bl := uint8(255), uint8(255)
			if mr != nil {
				i := y*mr.Stride + x*4
				g, bl = mr.Pix[i+1], mr.Pix[i+2]
			}
			ao := uint8(255)
			if occ != nil && occ.Bounds().Dx() == w && occ.Bounds().Dy() == h {
				i := y*occ.Stride + x*4
				// glTF: occluded = 1 + strength * (texel - 1).
				ao = uint8(255 + strength*(float32(occ.Pix[i])-255) + 0.5)
			}
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = ao, g, bl, 255
		}
	}
	return out
}

// fadeMaterial is the item's material for an alpha-blended draw, reusing
// the cached bind groups (only the numbers change frame to frame).
func (it *modelItem) fadeMaterial(c RGBA, alpha float32) *Material {
	if it.fadeMat == nil {
		it.fadeMat = &Material{}
	}
	f := it.fadeMat
	f.Albedo = it.img
	f.Color = RGBA{R: c.R, G: c.G, B: c.B, A: 1}
	f.Metallic, f.Roughness = it.metallic, it.roughness
	// Material treats Alpha 0 as "unset, opaque"; a fully faded model must
	// stay in the blended variant, so clamp just above zero.
	f.Alpha = max(alpha, 1e-4)
	return f
}

// ModelPrimitive is one drawable piece of a built model: its GPU mesh, the
// model-space transform of the node that owns it in the current pose, and
// the glTF material values BuildModel resolved. Primitives returns them for
// callers that must reissue a model's geometry themselves rather than
// through Draw — instancing thousands of copies of one tree through
// DrawInstanced, say, which Draw (one draw call per instance) cannot do.
type ModelPrimitive struct {
	Mesh      *Mesh
	Local     core.Mat4 // placement within the model
	Color     RGBA
	Metallic  float32
	Roughness float32
	Albedo    *Image3D // nil = untextured
	// Normal, ARM and Emissive are the material's other maps (nil when
	// absent): the tangent-space normal map with NormalScale, occlusion /
	// roughness / metallic packed as render.Material.ARM, and sRGB
	// emission scaled by Emission. Pass them on into a render.Material to
	// draw the primitive the way Model.Draw does.
	Normal      *Image3D
	ARM         *Image3D
	Emissive    *Image3D
	NormalScale float32
	Emission    float32
	// Node is the glTF node that owns the primitive and Material its glTF
	// material index (-1 if none) — the keys a scene inspector maps picks,
	// visibility and highlighting through.
	Node     int
	Material int
	// DoubleSided and Blend carry the glTF material's doubleSided flag and
	// alphaMode BLEND, for callers drawing through DrawMaterial.
	DoubleSided bool
	Blend       bool
	// Skinned marks a primitive deformed by a joint palette. Mesh holds
	// its BIND POSE (the GPU path never rewrites it, and the CPU path
	// writes only the GPU buffer), and the palette belongs to this model
	// alone, so an instancing caller cannot share the geometry between
	// instances: skip it, or fall back to Draw.
	Skinned bool
}

// Primitives lists the model's drawable pieces in the current pose. The
// returned meshes and images stay owned by the model — do not release them,
// and do not keep the slice across a re-pose.
func (m *Model) Primitives() []ModelPrimitive {
	out := make([]ModelPrimitive, 0, len(m.items))
	for i := range m.items {
		it := &m.items[i]
		out = append(out, ModelPrimitive{
			Mesh: it.mesh, Local: it.world, Color: it.color,
			Metallic: it.metallic, Roughness: it.roughness, Albedo: it.img,
			Normal: it.normal, ARM: it.arm, Emissive: it.emissive,
			NormalScale: it.nscale, Emission: it.emission,
			Node: it.node, Material: it.material, DoubleSided: it.double, Blend: it.blend,
			Skinned: it.skin >= 0 && it.skin < len(m.skins),
		})
	}
	return out
}

// JointWorld returns a joint's model-space matrix in the pose the last
// Advance produced — the anchor for bone attachment:
//
//	hand, _ := model.JointWorld("mixamorig:RightHand")
//	weapon.Draw(r3, modelTransform.Mul(hand).Mul(offset))
//
// The name must match a node exactly; JointBySuffix is the forgiving form.
func (m *Model) JointWorld(name string) (core.Mat4, bool) {
	if m.byName == nil {
		m.byName = make(map[string]int, len(m.nodes))
		for i := range m.nodes {
			if _, dup := m.byName[m.nodes[i].Name]; !dup {
				m.byName[m.nodes[i].Name] = i
			}
		}
	}
	i, ok := m.byName[name]
	if !ok || i >= len(m.worlds) {
		return core.Mat4Identity(), false
	}
	return m.worlds[i], true
}

// JointBySuffix finds a joint whose name ends with suffix, ignoring case
// and any ':'/'_' separators — so "_RightHand", "RightHand" and
// ":rightHand" all match "mixamorig6:RightHand", while "RightHandIndex1"
// does not. The shortest matching name wins, so "RightHand" prefers the
// hand over a child bone that happens to end the same way.
func (m *Model) JointBySuffix(suffix string) (core.Mat4, bool) {
	want := normalizeJoint(suffix)
	if want == "" {
		return core.Mat4Identity(), false
	}
	best := -1
	for i := range m.nodes {
		n := normalizeJoint(m.nodes[i].Name)
		if !strings.HasSuffix(n, want) {
			continue
		}
		if best < 0 || len(m.nodes[i].Name) < len(m.nodes[best].Name) {
			best = i
		}
	}
	if best < 0 || best >= len(m.worlds) {
		return core.Mat4Identity(), false
	}
	return m.worlds[best], true
}

// JointNames lists the model's node names (every joint is a node) — useful
// when hunting for the right attachment bone.
func (m *Model) JointNames() []string {
	out := make([]string, len(m.nodes))
	for i := range m.nodes {
		out[i] = m.nodes[i].Name
	}
	return out
}

func normalizeJoint(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == ':' || r == '_' || r == ' ' || r == '|':
			// separator: dropped
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Bounds returns the model's world-space AABB in its current pose (each
// item's local mesh box transformed by its node world). Useful for framing
// a camera on a loaded model.
func (m *Model) Bounds() (lo, hi core.Vec3) {
	first := true
	for _, it := range m.items {
		bmin, bmax := it.mesh.bmin, it.mesh.bmax
		// 8 corners of the mesh box, transformed by the item world.
		for cx := 0; cx < 2; cx++ {
			for cy := 0; cy < 2; cy++ {
				for cz := 0; cz < 2; cz++ {
					p := core.V3(pick(cx, bmin.X, bmax.X), pick(cy, bmin.Y, bmax.Y), pick(cz, bmin.Z, bmax.Z))
					w := it.world.MulPoint(p)
					if first {
						lo, hi, first = w, w, false
						continue
					}
					lo = core.V3(min(lo.X, w.X), min(lo.Y, w.Y), min(lo.Z, w.Z))
					hi = core.V3(max(hi.X, w.X), max(hi.Y, w.Y), max(hi.Z, w.Z))
				}
			}
		}
	}
	return lo, hi
}

// Height returns the model's vertical extent in its current pose — the
// scale factor for normalizing an FBX rig (usually centimetres) to metres.
func (m *Model) Height() float32 {
	lo, hi := m.Bounds()
	return hi.Y - lo.Y
}

func pick(b int, lo, hi float32) float32 {
	if b == 0 {
		return lo
	}
	return hi
}

// Release frees the model's GPU meshes, textures and skinning buffers.
func (m *Model) Release() {
	for i := range m.items {
		it := &m.items[i]
		if it.attrs != nil {
			if !m.shared {
				it.attrs.Release()
			}
			it.attrs = nil
		}
		if it.fadeMat != nil {
			it.fadeMat.Release()
			it.fadeMat = nil
		}
	}
	for _, p := range m.palettes {
		if p != nil {
			p.release()
		}
	}
	m.palettes = nil
	for _, mesh := range m.ownMeshes {
		mesh.Release()
	}
	m.ownMeshes = nil
	if m.shared {
		return
	}
	for _, mesh := range m.meshes {
		mesh.Release()
	}
	for _, img := range m.images {
		img.Release()
	}
}

// Instance returns an independently posable copy of the model that SHARES
// its GPU meshes, textures and skin attribute buffers: only the node
// hierarchy, world transforms and joint palettes (3 KB per skin) are per
// instance. Eighteen soldiers from one FBX cost one upload of the model
// instead of eighteen (which was 16 s of a 20 s scene load). Requires GPU
// skinning; with the CPU path (GFX_CPU_SKIN=1) skinned primitives get
// their own vertex buffers, since that path rewrites them per pose.
// Releasing an instance never frees the base model's resources; release
// the base after every instance.
func (m *Model) Instance(r *Renderer3D) *Model {
	c := &Model{
		shared: true,
		images: m.images, meshes: m.meshes,
		nodes: append([]gltf.Node(nil), m.nodes...),
		roots: m.roots, skins: m.skins,
		worlds:    append([]core.Mat4(nil), m.worlds...),
		seen:      make([]bool, len(m.nodes)),
		byName:    m.byName,
		poseDirty: true,
	}
	if r != nil && GPUSkinning {
		m.useGPUSkin(r) // create the shared skin attribute buffers once, on the base
	}
	c.items = make([]modelItem, len(m.items))
	for i := range m.items {
		it := m.items[i] // copy; attrs stay shared with the base
		it.pal, it.fadeMat, it.scratch = nil, nil, nil
		if it.skin >= 0 && (!GPUSkinning || r == nil) {
			// CPU skinning rewrites the vertex buffer per pose: give the
			// instance its own copy of every skinned primitive's mesh.
			if r != nil && len(it.base) > 0 {
				own := r.NewMesh(it.base, it.mesh.idx)
				it.mesh = own
				c.ownMeshes = append(c.ownMeshes, own)
			}
		}
		c.items[i] = it
	}
	return c
}
