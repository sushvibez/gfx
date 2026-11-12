package render

// GPU skeletal skinning (DESIGN.md §9).
//
// The CPU path (skin.go) deforms every vertex of a skinned mesh on the CPU
// and re-uploads the whole vertex buffer each time the pose moves: ~19k
// Vertex3D, 0.9-2.7 MB per skinned item per FRAME. Eighteen soldiers
// cost 49 MB/frame of Queue.WriteBuffer traffic, which is the single
// largest cost in that scene.
//
// GPU skinning moves the deformation into the vertex stage. What travels
// per frame is then the JOINT PALETTE alone — 51 joints x 64 B = 3 KB per
// actor — and the vertex buffers are uploaded once, at load.
//
// The shape of it:
//
//   - The bind-pose Vertex3D buffer is untouched and still lives in vertex
//     slot 0, so static meshes and every existing pipeline are unaffected.
//   - Per-vertex joint indices (uint16x4) and weights (float32x4) go into a
//     SECOND vertex buffer (slot 1, locations 4 and 5), built once per
//     skinned primitive.
//   - The palette (array<mat4x4f>, one matrix per joint) is a storage
//     buffer in its OWN bind group (group 2), so it can be rebound per
//     draw while group 0 (frame) and group 1 (textures) stay put.
//   - Each shader family gains skinned vertex entry points; the pipelines
//     are built lazily on first use and cached, like the material variants.
//
// The shadow pass gets its own small modules (skinShadowWGSL) rather than a
// third entry point in the big ones: a depth-only pipeline has no fragment
// stage, so the texture group would be an empty hole in its auto-derived
// pipeline layout. A standalone module numbers the palette group 1 and
// leaves no gap.
//
// Package switch: GPUSkinning (default on). GFX_CPU_SKIN=1 forces the old
// CPU path for A/B comparison.

import (
	"os"
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// GPUSkinning selects the skinning path. True (the default) skins in the
// vertex shader and uploads only joint palettes; false deforms vertices on
// the CPU and re-uploads them, which is what every release up to now did.
// Set GFX_CPU_SKIN=1 in the environment to force the CPU path without
// touching code — the A/B switch for comparing the two.
//
// A Model reads it the first time it is drawn and keeps that answer for
// its lifetime (the two paths own different GPU resources), so flip it
// before building the models it should apply to.
var GPUSkinning = os.Getenv("GFX_CPU_SKIN") == ""

// skinAttr is one vertex's skinning data — the whole of vertex slot 1.
// 24 bytes: four joint indices then four weights.
type skinAttr struct {
	J [4]uint16
	W [4]float32
}

// skinAttrLayout is slot 1's vertex layout. Locations 4 and 5 sit above
// the highest Vertex3D location (3, the tangent), so the same numbering
// works for both shader families.
func skinAttrLayout() gpu.VertexLayout {
	return gpu.VertexLayout{
		Stride: int(unsafe.Sizeof(skinAttr{})),
		Attrs: []gpu.VertexAttr{
			{Format: gpu.VertexUint16x4, Offset: 0, Location: 4},
			{Format: gpu.VertexFloat32x4, Offset: 8, Location: 5},
		},
	}
}

// skinVertexLayouts is a skinned pipeline's vertex state: the ordinary
// Vertex3D buffer in slot 0 plus the joint/weight buffer in slot 1.
func skinVertexLayouts(attrs int) []gpu.VertexLayout {
	return append(vertexLayout3D(attrs), skinAttrLayout())
}

// newSkinAttrBuffer uploads a primitive's joint/weight streams as the
// slot-1 vertex buffer. Weights are passed through EXACTLY as the CPU path
// reads them (no renormalisation), so the two paths agree pixel for pixel
// on any asset, well-formed or not.
func (r *Renderer3D) newSkinAttrBuffer(joints [][4]uint16, weights [][4]float32) *gpu.Buffer {
	n := len(joints)
	if n == 0 || len(weights) < n {
		return nil
	}
	a := make([]skinAttr, n)
	for i := range a {
		a[i] = skinAttr{J: joints[i], W: weights[i]}
	}
	size := n * int(unsafe.Sizeof(skinAttr{}))
	b := r.device.CreateBuffer(gpu.BufferVertex|gpu.BufferCopyDst, size)
	r.queue.WriteBuffer(b, 0, unsafe.Slice((*byte)(unsafe.Pointer(&a[0])), size))
	return b
}

// ---- joint palettes --------------------------------------------------------

// palette slots: which pipeline family a group-2 (or group-1, in the
// shadow modules) bind group was derived from. Bind groups are not shared
// between pipeline families — an auto-derived layout is only guaranteed
// compatible with the pipeline it came from — so one palette buffer keeps a
// small array of bind groups, built on demand.
const (
	palLegacy       = iota // legacy Draw*/DrawTinted main pass
	palLegacyShadow        // legacy shadow pass
	palMatShadow           // material shadow pass
	palMat                 // material main pass: palMat + variant
	palSlots        = palMat + matVariants
)

// skinPalette is one skin's joint palette on the GPU: a storage buffer of
// array<mat4x4f>, rewritten only when the pose actually moves, plus the
// bind groups that expose it to each pipeline family that has drawn it.
type skinPalette struct {
	buf     *gpu.Buffer
	joints  int
	bind    [palSlots]*gpu.BindGroup
	scratch []core.Mat4
}

// upload writes the palette, allocating (or growing) the buffer first.
func (p *skinPalette) upload(r *Renderer3D, palette []core.Mat4) {
	if len(palette) == 0 {
		return
	}
	size := len(palette) * int(unsafe.Sizeof(core.Mat4{}))
	if p.buf == nil || p.joints < len(palette) {
		p.release()
		p.buf = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, size)
		p.joints = len(palette)
	}
	r.queue.WriteBuffer(p.buf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&palette[0])), size))
}

// group returns the bind group exposing this palette to one pipeline
// family, building it the first time that family draws the palette.
func (p *skinPalette) group(r *Renderer3D, slot int, layout *gpu.BindGroupLayout) *gpu.BindGroup {
	if p.buf == nil || layout == nil {
		return nil
	}
	if p.bind[slot] == nil {
		p.bind[slot] = r.device.CreateBindGroup(layout,
			gpu.BindGroupEntry{Binding: 0, Buffer: p.buf})
	}
	return p.bind[slot]
}

func (p *skinPalette) release() {
	for i := range p.bind {
		if p.bind[i] != nil {
			p.bind[i].Release()
			p.bind[i] = nil
		}
	}
	if p.buf != nil {
		p.buf.Release()
		p.buf = nil
	}
	p.joints = 0
}

// ---- WGSL ------------------------------------------------------------------

// skinCommonWGSL is the palette binding and the blend itself, shared by
// both main-pass shader families (group 2, so groups 0 and 1 keep their
// meaning). Linear blend skinning: sum the four joint MATRICES by weight
// and apply the result once — exactly what skinVertices does on the CPU,
// including its behaviour on a degenerate all-zero weight quad (a zero
// matrix, i.e. the vertex collapses to the origin).
const skinCommonWGSL = `
@group(2) @binding(0) var<storage, read> palette : array<mat4x4f>;

fn skin_matrix(j : vec4<u32>, w : vec4f) -> mat4x4f {
    return palette[j.x] * w.x + palette[j.y] * w.y +
           palette[j.z] * w.z + palette[j.w] * w.w;
}

// Match the CPU path, which normalizes the deformed normal per vertex
// (before interpolation) and leaves a zero vector alone.
fn skin_dir(v : vec3f) -> vec3f {
    let l = length(v);
    if (l > 0.0) {
        return v / l;
    }
    return v;
}
`

// skinShadowWGSL builds the standalone depth-only skinning module for one
// shader family. draws is the WGSL for that family's per-draw struct (only
// its leading model matrix is read, but the array stride must match).
func skinShadowWGSL(draws string) string {
	return frameWGSL + draws + `
@group(0) @binding(0) var<storage, read> frame : Frame;
@group(0) @binding(1) var<storage, read> draws : array<Draw>;
@group(1) @binding(0) var<storage, read> palette : array<mat4x4f>;

fn skin_matrix(j : vec4<u32>, w : vec4f) -> mat4x4f {
    return palette[j.x] * w.x + palette[j.y] * w.y +
           palette[j.z] * w.z + palette[j.w] * w.w;
}

@vertex
fn vs_shadow_skin(@location(0) pos : vec3f,
                  @location(4) joints : vec4<u32>, @location(5) weights : vec4f,
                  @builtin(instance_index) iid : u32) -> @builtin(position) vec4f {
    let s = skin_matrix(joints, weights);
    let local = (s * vec4f(pos, 1.0)).xyz;
    return frame.light_vp * (draws[iid].model * vec4f(local, 1.0));
}
`
}

// The two per-draw record shapes, as the shadow modules see them. Only the
// model matrix is read; the rest is there so the array stride is right.
const (
	legacyDrawWGSL = `
struct Draw {
    model : mat4x4f,
    color : vec4f,
    params : vec4f,
}
`
	matDrawWGSL = `
struct Draw {
    model : mat4x4f,
    color : vec4f,
    p0 : vec4f,
    p1 : vec4f,
    p2 : vec4f,
}
`
)

// skinShadowVertexLayouts: the depth-only skinned pipelines read position
// from slot 0 and the joint data from slot 1. Slot 0 still declares the
// full Vertex3D stride — only the attributes it uses are listed.
func skinShadowVertexLayouts() []gpu.VertexLayout {
	return []gpu.VertexLayout{
		{Stride: int(unsafe.Sizeof(Vertex3D{})), Attrs: []gpu.VertexAttr{
			{Format: gpu.VertexFloat32x3, Offset: 0, Location: 0},
		}},
		skinAttrLayout(),
	}
}

// ---- lazy pipelines --------------------------------------------------------

// ensureLegacySkin builds (once) the skinned twins of the legacy PBR
// pipeline and its shadow pipeline. Returns false when the device refuses
// them, which makes the caller fall back to CPU skinning.
func (r *Renderer3D) ensureLegacySkin() bool {
	if r.skinFailed {
		return false
	}
	if r.skinPL != nil {
		return true
	}
	mod, err := r.device.CreateShaderModuleWGSL("renderer3d-skin", shader3D)
	if err != nil {
		r.skinFailed = true
		return false
	}
	defer mod.Release()
	pl, err := r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: mod, VertexEntry: "vs_skin", FragmentEntry: "fs",
		Format: r.format, DepthTest: true, CullBack: true,
		VertexLayouts: skinVertexLayouts(3),
	})
	if err != nil {
		r.skinFailed = true
		return false
	}
	smod, err := r.device.CreateShaderModuleWGSL("renderer3d-skin-shadow", skinShadowWGSL(legacyDrawWGSL))
	if err != nil {
		pl.Release()
		r.skinFailed = true
		return false
	}
	defer smod.Release()
	spl, err := r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: smod, VertexEntry: "vs_shadow_skin",
		DepthTest: true, CullBack: true,
		VertexLayouts: skinShadowVertexLayouts(),
	})
	if err != nil {
		pl.Release()
		r.skinFailed = true
		return false
	}
	r.skinPL, r.skinShadowPL = pl, spl
	r.skinLayout = pl.BindGroupLayout(0)
	r.skinTexLayout = pl.BindGroupLayout(1)
	r.skinPalLayout = pl.BindGroupLayout(2)
	r.skinShLayout = spl.BindGroupLayout(0)
	r.skinShPalLayout = spl.BindGroupLayout(1)
	r.rebuildSkinBinds()
	return true
}

// rebuildSkinBinds (re)creates the skinned pipelines' group-0 bind groups.
// Called when they are built and whenever the draw buffer they point at is
// reallocated.
func (r *Renderer3D) rebuildSkinBinds() {
	if r.skinPL == nil {
		return
	}
	rel(r.skinBind)
	rel(r.skinShadowBind)
	r.skinBind = r.device.CreateBindGroup(r.skinLayout,
		gpu.BindGroupEntry{Binding: 0, Buffer: r.frame},
		gpu.BindGroupEntry{Binding: 1, Buffer: r.draws},
		gpu.BindGroupEntry{Binding: 2, Sampler: r.shadowSampler},
		gpu.BindGroupEntry{Binding: 3, TextureView: r.shadowView},
	)
	r.skinShadowBind = r.device.CreateBindGroup(r.skinShLayout,
		gpu.BindGroupEntry{Binding: 0, Buffer: r.frame},
		gpu.BindGroupEntry{Binding: 1, Buffer: r.draws},
	)
}

// skinImageBind is an Image3D's group-1 bind group for the skinned legacy
// pipeline (whose auto-derived layout is its own object). Built on demand
// and cached on the image.
func (r *Renderer3D) skinImageBind(img *Image3D) *gpu.BindGroup {
	if img.sbind == nil {
		img.sbind = r.device.CreateBindGroup(r.skinTexLayout,
			gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
			gpu.BindGroupEntry{Binding: 1, TextureView: img.view},
		)
	}
	return img.sbind
}

// ensureMatSkin builds (once) the skinned material pipeline for one
// blend/double-sided variant, plus the shared material shadow-skin
// pipeline. Returns false on failure (caller falls back to CPU skinning).
func (r *Renderer3D) ensureMatSkin(variant int) bool {
	if r.skinFailed {
		return false
	}
	if r.matPL[variant] != nil && r.matSkinShadowPL != nil {
		return true
	}
	if r.matPL[variant] == nil {
		mod, err := r.device.CreateShaderModuleWGSL("renderer3d-material-skin", shaderMaterial)
		if err != nil {
			r.skinFailed = true
			return false
		}
		pl, err := r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
			Shader: mod, VertexEntry: "vs_skin", FragmentEntry: "fs",
			Format: r.format, DepthTest: true,
			CullBack:      variant&1 == 0,
			AlphaBlend:    variant&2 != 0,
			VertexLayouts: skinVertexLayouts(4),
		})
		mod.Release()
		if err != nil {
			r.skinFailed = true
			return false
		}
		r.matPL[variant] = pl
		r.matLayout[variant] = pl.BindGroupLayout(0)
		r.matTexLayout[variant] = pl.BindGroupLayout(1)
		r.matPalLayout[variant] = pl.BindGroupLayout(2)
		r.matBind[variant] = r.device.CreateBindGroup(r.matLayout[variant], r.matFrameEntries()...)
	}
	if r.matSkinShadowPL == nil {
		mod, err := r.device.CreateShaderModuleWGSL("renderer3d-material-skin-shadow", skinShadowWGSL(matDrawWGSL))
		if err != nil {
			r.skinFailed = true
			return false
		}
		pl, err := r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
			Shader: mod, VertexEntry: "vs_shadow_skin",
			DepthTest: true, CullBack: true,
			VertexLayouts: skinShadowVertexLayouts(),
		})
		mod.Release()
		if err != nil {
			r.skinFailed = true
			return false
		}
		r.matSkinShadowPL = pl
		r.matSkinShLayout = pl.BindGroupLayout(0)
		r.matSkinShPalLayout = pl.BindGroupLayout(1)
		r.rebuildMatSkinShadowBind()
	}
	return true
}

func (r *Renderer3D) rebuildMatSkinShadowBind() {
	if r.matSkinShadowPL == nil {
		return
	}
	rel(r.matSkinShadowBind)
	r.matSkinShadowBind = r.device.CreateBindGroup(r.matSkinShLayout,
		gpu.BindGroupEntry{Binding: 0, Buffer: r.frame},
		gpu.BindGroupEntry{Binding: 1, Buffer: r.mdraws},
	)
}

// releaseSkin frees everything the skinned pipelines own.
func (r *Renderer3D) releaseSkin() {
	rel(r.skinBind)
	rel(r.skinShadowBind)
	rel(r.skinLayout)
	rel(r.skinTexLayout)
	rel(r.skinPalLayout)
	rel(r.skinShLayout)
	rel(r.skinShPalLayout)
	rel(r.skinPL)
	rel(r.skinShadowPL)
	rel(r.matSkinShadowBind)
	rel(r.matSkinShLayout)
	rel(r.matSkinShPalLayout)
	rel(r.matSkinShadowPL)
	for v := range matVariants {
		rel(r.matPalLayout[v])
	}
}
