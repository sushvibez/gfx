package render

// Material is the 3D renderer's surface description (DESIGN.md §9): a
// Cook-Torrance PBR material with the texture set a Poly Haven / glTF asset
// actually ships — base colour, tangent-space normal map, packed ARM
// (occlusion/roughness/metalness) or a standalone grayscale roughness map,
// and emission — plus two projection tricks that matter for level geometry:
// world-space TRIPLANAR projection (no UVs needed, no stretching on cliffs)
// and stochastic HEX TILING, which kills the visible grid a tiled texture
// otherwise stamps across a large surface.
//
// Every numeric field treats its zero value as "the sensible default", so a
// bare &Material{Albedo: img} is a white, opaque, unit-scaled surface:
// Color black-zero means white, Alpha 0 means opaque, UVScale/NormalScale 0
// mean 1, and Roughness 0 with a roughness texture bound means "take the
// texture".
//
//	r.DrawMaterial(wall, model, &render.Material{
//	        Albedo: diff, Normal: nor, ARM: arm,
//	        UVScale: 4, AntiTile: true,
//	})

import (
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// Material describes a surface for DrawMaterial / DrawInstanced.
type Material struct {
	// Texture maps. All are optional; nil falls back to a neutral 1x1.
	Albedo *Image3D // base colour, sRGB-encoded
	Normal *Image3D // tangent-space normal map (OpenGL +Y convention)
	ARM    *Image3D // packed: R = ambient occlusion, G = roughness, B = metallic
	// RoughnessMap is a standalone grayscale roughness map, the
	// alternative to a packed ARM (the scalar Roughness below keeps the
	// plain name, so the map takes the -Map suffix).
	RoughnessMap *Image3D
	Emissive     *Image3D // emission colour, sRGB-encoded

	// RoughnessChannel selects which channel of RoughnessMap to read
	// (0 = R, 1 = G, 2 = B). Ignored when ARM is set.
	RoughnessChannel uint8

	Color       RGBA    // base colour tint (zero = opaque white)
	Metallic    float32 // 0..1, multiplied with the ARM blue channel
	Roughness   float32 // 0..1, multiplied with the roughness texture
	NormalScale float32 // normal-map strength (zero = 1)
	UVScale     float32 // texture repeats per UV/world unit (zero = 1)

	// Triplanar projects the maps from world space on the three axis
	// planes, blended by |n|^4 — no UVs needed and no stretching.
	Triplanar bool
	// AntiTileRotation scales the per-cell random rotation hex tiling
	// applies, as a fraction of a full turn: zero means the technique's
	// default of 1 (fully random), which is right for rock, sand, grass
	// and other unoriented material. An ORIENTED texture — a brick bond,
	// planks, tiles — scrambles under full rotation; give it something
	// like 0.05 to break the repetition while keeping the courses
	// readable, or a value near zero for offsets only.
	AntiTileRotation float32

	// AntiTile enables stochastic hex tiling: three hex-lattice samples
	// with a per-cell random offset and rotation, blended by sharpened
	// barycentric weights through a variance-preserving mix. This is the
	// fix for the repeating-grid look of a tiled texture.
	AntiTile bool

	// Water animates the surface as water: Normal is a wave map, sampled as
	// two layers scrolling across each other in the horizontal plane
	// (FrameSettings.Time moves them; UVScale sets the wave size,
	// NormalScale their height). Pair it with a dark Color, a low
	// Roughness and FrameSettings.SkyReflection.
	Water bool

	Emission float32 // emissive multiplier (0 = no emission unless Emissive is set)
	Unlit    bool    // skip lighting entirely (still tonemapped-free, display colour)

	DoubleSided bool    // draw back faces too, with flipped normals
	Alpha       float32 // 1 (or 0) = opaque; <1 = alpha-blended, drawn after opaques back-to-front

	// Cached GPU bind groups for this texture set, one per material
	// pipeline variant (an "auto" pipeline layout is only compatible with
	// the pipeline that derived it), rebuilt when the maps change. Not
	// part of the public value.
	bind    [matVariants]*gpu.BindGroup
	bindKey [4]*Image3D
	bindOf  *Renderer3D
}

// Release frees the material's cached bind group. Textures are owned by the
// caller and are not released.
func (m *Material) Release() {
	for v := range m.bind {
		if m.bind[v] != nil {
			m.bind[v].Release()
			m.bind[v] = nil
		}
	}
	m.bindKey, m.bindOf = [4]*Image3D{}, nil
}

// material flag bits, mirrored by the WGSL fragment stage.
const (
	matTriplanar = 1 << iota
	matAntiTile
	matUnlit
	matHasNormal
	matHasARM
	matHasRough
	matHasEmissive
	matWater
)

// resolved is the material flattened to what the shader consumes, with
// every "zero means default" rule already applied on the CPU.
type resolved struct {
	color                         RGBA
	metallic, roughness           float32
	normalScale, uvScale          float32
	flags, emission, alpha, rchan float32
	antiRot                       float32
}

func (m *Material) resolve() resolved {
	r := resolved{
		color:       m.Color,
		metallic:    m.Metallic,
		roughness:   m.Roughness,
		normalScale: m.NormalScale,
		uvScale:     m.UVScale,
		alpha:       m.Alpha,
		emission:    m.Emission,
		rchan:       float32(m.RoughnessChannel),
		antiRot:     m.AntiTileRotation,
	}
	if r.antiRot == 0 {
		r.antiRot = 1
	}
	if r.color == (RGBA{}) {
		r.color = RGBA{R: 1, G: 1, B: 1, A: 1}
	}
	if r.color.A == 0 {
		r.color.A = 1
	}
	if r.normalScale == 0 {
		r.normalScale = 1
	}
	if r.uvScale == 0 {
		r.uvScale = 1
	}
	if r.alpha == 0 {
		r.alpha = 1
	}
	var flags int
	if m.Triplanar {
		flags |= matTriplanar
	}
	if m.AntiTile {
		flags |= matAntiTile
	}
	if m.Unlit {
		flags |= matUnlit
	}
	if m.Normal != nil {
		flags |= matHasNormal
	}
	switch {
	case m.ARM != nil:
		flags |= matHasARM
	case m.RoughnessMap != nil:
		flags |= matHasRough
	}
	if m.Water && m.Normal != nil {
		flags |= matWater
	}
	if m.Emissive != nil {
		flags |= matHasEmissive
		if r.emission == 0 {
			r.emission = 1
		}
	}
	// A bound roughness/metalness map with no explicit scalar means "use
	// the texture", i.e. a multiplier of 1 rather than 0.
	if flags&(matHasARM|matHasRough) != 0 && r.roughness == 0 {
		r.roughness = 1
	}
	if flags&matHasARM != 0 && r.metallic == 0 {
		r.metallic = 1
	}
	r.flags = float32(flags)
	return r
}

// bindGroup returns (building or rebuilding as needed) the group-1 bind
// group holding this material's four texture views plus the mip/aniso
// sampler, for one material pipeline variant.
func (r *Renderer3D) bindGroup(m *Material, variant int) *gpu.BindGroup {
	pick := func(img, fallback *Image3D) *Image3D {
		if img == nil {
			return fallback
		}
		return img
	}
	albedo := pick(m.Albedo, r.white)
	normal := pick(m.Normal, r.flatNormal)
	rough := m.ARM
	if rough == nil {
		rough = m.RoughnessMap
	}
	rough = pick(rough, r.white)
	emis := pick(m.Emissive, r.black)
	key := [4]*Image3D{albedo, normal, rough, emis}
	if m.bindKey != key || m.bindOf != r {
		for v := range m.bind {
			if m.bind[v] != nil {
				m.bind[v].Release()
				m.bind[v] = nil
			}
		}
		m.bindKey, m.bindOf = key, r
	}
	if m.bind[variant] == nil {
		m.bind[variant] = r.device.CreateBindGroup(r.matTexLayout[variant],
			gpu.BindGroupEntry{Binding: 0, Sampler: r.mipSampler},
			gpu.BindGroupEntry{Binding: 1, TextureView: albedo.view},
			gpu.BindGroupEntry{Binding: 2, TextureView: normal.view},
			gpu.BindGroupEntry{Binding: 3, TextureView: rough.view},
			gpu.BindGroupEntry{Binding: 4, TextureView: emis.view},
		)
	}
	return m.bind[variant]
}

// ---- mipmapped textures ----------------------------------------------------

// NewImage3DMipmapped uploads RGBA8 pixels as a 3D texture with a full mip
// chain, box-filtered on the CPU. Minified material maps alias badly
// without it — and the hex-tiling and triplanar paths rely on the smallest
// level being the texture's average colour, which is what the
// variance-preserving blend needs.
func (r *Renderer3D) NewImage3DMipmapped(w, h int, rgba []byte) *Image3D {
	levels := gpu.MipLevels(w, h)
	tex := r.device.CreateTexture2DMips(w, h, gpu.FormatRGBA8Unorm,
		gpu.TextureBinding|gpu.TextureCopyDst, levels)
	r.queue.WriteTextureMip(tex, 0, w, h, rgba)
	lw, lh, src := w, h, rgba
	for level := 1; level < levels; level++ {
		src, lw, lh = downsampleRGBA(src, lw, lh)
		r.queue.WriteTextureMip(tex, level, lw, lh, src)
	}
	view := tex.CreateView()
	bind := r.device.CreateBindGroup(r.texLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: view},
	)
	return &Image3D{tex: tex, view: view, bind: bind}
}

// downsampleRGBA box-filters one mip level to the next, halving each
// dimension (floor, minimum 1) the way WebGPU sizes a mip chain.
func downsampleRGBA(src []byte, w, h int) ([]byte, int, int) {
	nw, nh := max(w/2, 1), max(h/2, 1)
	dst := make([]byte, nw*nh*4)
	for y := range nh {
		y0 := min(2*y, h-1)
		y1 := min(2*y+1, h-1)
		for x := range nw {
			x0 := min(2*x, w-1)
			x1 := min(2*x+1, w-1)
			o := (y*nw + x) * 4
			a := (y0*w + x0) * 4
			b := (y0*w + x1) * 4
			c := (y1*w + x0) * 4
			d := (y1*w + x1) * 4
			for k := range 4 {
				dst[o+k] = byte((uint32(src[a+k]) + uint32(src[b+k]) + uint32(src[c+k]) + uint32(src[d+k]) + 2) / 4)
			}
		}
	}
	return dst, nw, nh
}

// ---- tangents --------------------------------------------------------------

// generateTangents fills in per-vertex tangents from the UV parametrisation
// (a MikkTSpace-lite: accumulate each triangle's UV-derived tangent and
// bitangent at its corners, then Gram-Schmidt against the normal and store
// the handedness in W). Normal mapping needs a tangent frame, and neither
// the primitive builders nor most mesh sources supply one. Triangles with a
// degenerate UV triangle (a cube built without texcoords, say) contribute
// nothing, leaving a zero tangent that the shader detects and replaces with
// a screen-space derivative frame.
func generateTangents(verts []Vertex3D, idx []uint32) {
	tan := make([]core.Vec3, len(verts))
	bit := make([]core.Vec3, len(verts))
	for i := 0; i+2 < len(idx); i += 3 {
		i0, i1, i2 := idx[i], idx[i+1], idx[i+2]
		if int(i0) >= len(verts) || int(i1) >= len(verts) || int(i2) >= len(verts) {
			continue
		}
		v0, v1, v2 := &verts[i0], &verts[i1], &verts[i2]
		e1 := v1.Pos.Sub(v0.Pos)
		e2 := v2.Pos.Sub(v0.Pos)
		du1, dv1 := v1.U-v0.U, v1.V-v0.V
		du2, dv2 := v2.U-v0.U, v2.V-v0.V
		det := du1*dv2 - du2*dv1
		if det > -1e-12 && det < 1e-12 {
			continue
		}
		f := 1 / det
		t := e1.Scale(dv2 * f).Sub(e2.Scale(dv1 * f))
		b := e2.Scale(du1 * f).Sub(e1.Scale(du2 * f))
		for _, j := range [3]uint32{i0, i1, i2} {
			tan[j] = tan[j].Add(t)
			bit[j] = bit[j].Add(b)
		}
	}
	for i := range verts {
		t := tan[i]
		if t.Length() < 1e-9 {
			verts[i].Tan = core.Vec4{}
			continue
		}
		n := verts[i].Normal
		if n.Length() > 1e-9 {
			n = n.Normalize()
			t = t.Sub(n.Scale(n.Dot(t))) // Gram-Schmidt
		}
		if t.Length() < 1e-9 {
			verts[i].Tan = core.Vec4{}
			continue
		}
		t = t.Normalize()
		w := float32(1)
		if n.Cross(t).Dot(bit[i]) < 0 {
			w = -1
		}
		verts[i].Tan = core.V4(t.X, t.Y, t.Z, w)
	}
}

// hasTangents reports whether any vertex already carries a tangent, so a
// mesh source that supplies its own (glTF) is left alone.
func hasTangents(verts []Vertex3D) bool {
	for i := range verts {
		if verts[i].Tan != (core.Vec4{}) {
			return true
		}
	}
	return false
}
