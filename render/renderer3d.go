package render

import (
	"sort"
	"unsafe"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// Renderer3D is the 3D renderer (DESIGN.md §9): depth-tested, back-culled
// forward rendering of indexed meshes with Cook-Torrance PBR shading
// (metallic/roughness), one directional light with a PCF-sampled shadow
// map, and per-draw data in an instance-indexed storage buffer. Clustered
// lighting arrives later; the mesh/draw API is the part that stays.
//
// Render records two passes: a depth-only shadow pass from the light's
// view, then the main pass sampling it.
type Renderer3D struct {
	device    *gpu.Device
	queue     *gpu.Queue
	pipeline  *gpu.RenderPipeline
	shadowPL  *gpu.RenderPipeline
	linePL    *gpu.RenderPipeline  // line-list twin of pipeline (DrawLines)
	lineBind  *gpu.BindGroup       // group 0 for linePL (auto layouts are per pipeline)
	lineWhite *gpu.BindGroup       // group 1: the white texture, for linePL
	layout    *gpu.BindGroupLayout // main group 0
	shLayout  *gpu.BindGroupLayout // shadow group 0
	texLayout *gpu.BindGroupLayout // group 1: sampler + texture
	sampler   *gpu.Sampler
	white     *Image3D // 1x1 white for untextured draws

	shadowMap     *gpu.Texture
	shadowView    *gpu.TextureView
	shadowSampler *gpu.Sampler

	bind       *gpu.BindGroup
	shadowBind *gpu.BindGroup
	frame      *gpu.Buffer // per-frame uniforms
	draws      *gpu.Buffer // per-draw array, instance-indexed
	format     gpu.TextureFormat

	// ---- GPU skinning (gpuskin.go) ----
	// Skinned twins of both pipeline families, built lazily on the first
	// skinned draw. skinFailed latches a device refusal so the model layer
	// falls back to CPU skinning instead of retrying every frame.
	skinFailed         bool
	skinPL             *gpu.RenderPipeline
	skinShadowPL       *gpu.RenderPipeline
	skinLayout         *gpu.BindGroupLayout // skinned main group 0
	skinTexLayout      *gpu.BindGroupLayout // skinned main group 1
	skinPalLayout      *gpu.BindGroupLayout // skinned main group 2 (palette)
	skinShLayout       *gpu.BindGroupLayout // skinned shadow group 0
	skinShPalLayout    *gpu.BindGroupLayout // skinned shadow group 1 (palette)
	skinBind           *gpu.BindGroup
	skinShadowBind     *gpu.BindGroup
	matSkinShadowPL    *gpu.RenderPipeline
	matSkinShLayout    *gpu.BindGroupLayout
	matSkinShPalLayout *gpu.BindGroupLayout
	matSkinShadowBind  *gpu.BindGroup
	matPalLayout       [matVariants]*gpu.BindGroupLayout // skinned variants only

	items   []drawItem
	pickTag int // stamp applied to queued draws (SetPickTag); -1 = untagged

	// Cached shared unit primitives (owned by the renderer).
	unitCube     *Mesh
	unitSphere   *Mesh
	unitCylinder *Mesh
	unitCone     *Mesh
	unitPlane    *Mesh
	data         []float32

	shadowCenter core.Vec3
	shadowRadius float32
	camPos       core.Vec3

	// ---- material pass (material.go, lights.go, matshader.go) ----
	// Four pipeline variants indexed by matVariant(blend, doubleSided);
	// each carries its own auto-derived layouts, so bind groups are built
	// per variant (an "auto" pipeline layout is only compatible with the
	// pipeline it came from).
	matPL         [matVariants]*gpu.RenderPipeline
	matLayout     [matVariants]*gpu.BindGroupLayout
	matTexLayout  [matVariants]*gpu.BindGroupLayout
	matBind       [matVariants]*gpu.BindGroup
	matShadowPL   *gpu.RenderPipeline
	matShLayout   *gpu.BindGroupLayout
	matShadowBind *gpu.BindGroup
	mdraws        *gpu.Buffer
	lightBuf      *gpu.Buffer
	mipSampler    *gpu.Sampler
	flatNormal    *Image3D // 1x1 (0.5,0.5,1) tangent-space "no bump"
	black         *Image3D // 1x1 black for the emissive fallback

	mitems   []matItem
	mdata    []float32
	settings FrameSettings
	points   []PointLight
	spots    []SpotLight
	lights   lightBlock
	order    []int
}

// matVariants is the number of material pipeline variants: opaque/blended
// crossed with back-face-culled/double-sided crossed with static/skinned.
// The four static variants are built eagerly by initMaterialPass; the four
// skinned ones (matSkinBit) are built on demand by ensureMatSkin.
const matVariants = 8

// matSkinBit marks the skinned half of the variant space.
const matSkinBit = 4

func matVariant(blend, doubleSided, skinned bool) int {
	v := 0
	if blend {
		v |= 2
	}
	if doubleSided {
		v |= 1
	}
	if skinned {
		v |= matSkinBit
	}
	return v
}

// matItem is one queued material draw: a mesh, its material, and the range
// of per-draw records it occupies in the material draw buffer (more than
// one for DrawInstanced, which issues a single instanced draw call).
type matItem struct {
	mesh    *Mesh
	mat     *Material
	base    int // first instance index
	count   int // instance count
	variant int
	depth   float32 // view depth of the first instance, for blend sorting
	model   core.Mat4
	tag     int

	// GPU skinning: the primitive's joint/weight vertex buffer (slot 1)
	// and the joint palette bound at group 2. nil = static geometry.
	sattrs *gpu.Buffer
	spal   *skinPalette
}

// Image3D is a texture usable as a 3D base-color map.
type Image3D struct {
	tex  *gpu.Texture
	view *gpu.TextureView
	bind *gpu.BindGroup
	// sbind is the same texture bound for the SKINNED legacy pipeline,
	// whose auto-derived group-1 layout is its own object. Built on demand
	// (skinImageBind), nil until a skinned draw uses the image.
	sbind *gpu.BindGroup
}

func (i *Image3D) Release() {
	i.bind.Release()
	rel(i.sbind)
	i.view.Release()
	i.tex.Release()
}

// Vertex3D is the mesh vertex layout: position + normal + texcoord +
// tangent. Tan is the tangent-space basis normal mapping needs: xyz is the
// unit tangent (the +U direction on the surface) and w the handedness
// (+1/-1) that orients the bitangent. NewMesh fills it in from the UV
// parametrisation when a mesh source leaves it zero; a zero tangent is
// legal and makes the material shader fall back to a screen-space
// derivative frame.
type Vertex3D struct {
	Pos    core.Vec3
	Normal core.Vec3
	U, V   float32
	Tan    core.Vec4
}

// Mesh is GPU-resident indexed geometry; build with NewMesh. The CPU-side
// vertices/indices are retained for ray picking (see pick.go), along with
// the model-space AABB used as the pick broad phase.
type Mesh struct {
	vbuf    *gpu.Buffer
	ibuf    *gpu.Buffer
	indices int

	verts      []Vertex3D
	idx        []uint32
	bmin, bmax core.Vec3
	lines      bool // indices are segment pairs (NewLines)
}

// Bounds returns the mesh's model-space AABB.
func (m *Mesh) Bounds() (lo, hi core.Vec3) { return m.bmin, m.bmax }

// Stats returns the vertex and triangle counts.
func (m *Mesh) Stats() (verts, tris int) { return len(m.verts), len(m.idx) / 3 }

func (m *Mesh) Release() {
	m.vbuf.Release()
	m.ibuf.Release()
}

type drawItem struct {
	mesh  *Mesh
	img   *Image3D
	model core.Mat4 // retained for ray picking
	tag   int       // pick tag (SetPickTag); -1 = untagged
	lines bool      // drawn with the line pipeline; never picked, never shadowed

	// GPU skinning: the primitive's joint/weight vertex buffer (slot 1)
	// and the joint palette bound at group 2. nil = static geometry.
	sattrs *gpu.Buffer
	spal   *skinPalette
}

// frameUniforms mirrors the WGSL Frame struct (frameWGSL in matshader.go).
// The first four members are the original 160-byte layout the legacy shader
// reads; the rest is the environment the material pass adds.
type frameUniforms struct {
	ViewProj   core.Mat4
	LightVP    core.Mat4
	LightDir   core.Vec4 // xyz normalized, w sun intensity
	CamPos     core.Vec4
	Ambient    core.Vec4 // rgb ambient radiance, w exposure
	FogColor   core.Vec4 // rgb, w density
	FogParams  core.Vec4 // height, height density, aerial perspective, enabled
	SkyTop     core.Vec4
	SkyHorizon core.Vec4
	Misc       core.Vec4 // tonemap mode, light count
}

// sunIntensity is the directional light's radiance, matching the constant
// the original PBR shader hard-coded.
const sunIntensity = 2.6

const shadowMapSize = 2048

const shader3D = frameWGSL + `
struct Draw {
    model : mat4x4f,
    color : vec4f,
    params : vec4f, // metallic, roughness, shade mode (0 pbr, 1 unlit, 2 lambert, 3 lambert unshadowed), ambient
}
@group(0) @binding(0) var<storage, read> frame : Frame;
@group(0) @binding(1) var<storage, read> draws : array<Draw>;
@group(0) @binding(2) var shadow_samp : sampler_comparison;
@group(0) @binding(3) var shadow_tex : texture_depth_2d;

@group(1) @binding(0) var samp : sampler;
@group(1) @binding(1) var tex : texture_2d<f32>;

struct VSOut {
    @builtin(position) pos : vec4f,
    @location(0) world : vec3f,
    @location(1) normal : vec3f,
    @location(2) uv : vec2f,
    @location(3) @interpolate(flat) draw_id : u32,
}

fn world_pos(pos : vec3f, iid : u32) -> vec4f {
    return draws[iid].model * vec4f(pos, 1.0);
}

@vertex
fn vs(@location(0) pos : vec3f, @location(1) normal : vec3f, @location(2) uv : vec2f,
      @builtin(instance_index) iid : u32) -> VSOut {
    let world = world_pos(pos, iid);
    var out : VSOut;
    out.pos = frame.view_proj * world;
    out.world = world.xyz;
    // Uniform-scale assumption: rotate normals by the model matrix.
    out.normal = (draws[iid].model * vec4f(normal, 0.0)).xyz;
    out.uv = uv;
    out.draw_id = iid;
    return out;
}

// Depth-only vertex stage for the shadow pass.
@vertex
fn vs_shadow(@location(0) pos : vec3f, @location(1) normal : vec3f, @location(2) uv : vec2f,
             @builtin(instance_index) iid : u32) -> @builtin(position) vec4f {
    return frame.light_vp * world_pos(pos, iid);
}

const PI = 3.14159265;

fn shadow_factor(world : vec3f, n_dot_l : f32) -> f32 {
    let sc = frame.light_vp * vec4f(world, 1.0);
    let uv = vec2f(sc.x * 0.5 + 0.5, -sc.y * 0.5 + 0.5);
    if (any(uv < vec2f(0.0)) || any(uv > vec2f(1.0))) {
        return 1.0; // outside the shadow map: lit
    }
    let bias = clamp(0.002 * (1.0 - n_dot_l) + 0.0006, 0.0005, 0.004);
    return textureSampleCompare(shadow_tex, shadow_samp, uv, sc.z - bias);
}

@fragment
fn fs(in : VSOut) -> @location(0) vec4f {
    let d = draws[in.draw_id];
    let texel = textureSample(tex, samp, in.uv);
    // Alpha cutout: the pipeline is opaque, so transparent texels (hair /
    // foliage cards) are discarded rather than blended. Texture alpha only —
    // a translucent instance color still renders (opaquely) as before.
    if (texel.a < 0.5) {
        discard;
    }
    // Base color textures are sRGB-encoded; shade in linear space.
    let albedo = d.color.rgb * pow(texel.rgb, vec3f(2.2));
    let metallic = d.params.x;
    let roughness = max(d.params.y, 0.05);

    let n = normalize(in.normal);
    let v = normalize(frame.cam_pos.xyz - in.world);
    let l = normalize(-frame.light_dir.xyz);
    let h = normalize(l + v);
    let ndl = max(dot(n, l), 0.0);
    let ndv = max(dot(n, v), 1e-4);
    let ndh = max(dot(n, h), 0.0);
    let hdv = max(dot(h, v), 0.0);

    // Cook-Torrance: GGX distribution, Smith geometry, Schlick fresnel.
    let a = roughness * roughness;
    let a2 = a * a;
    let denom = ndh * ndh * (a2 - 1.0) + 1.0;
    let ndf = a2 / (PI * denom * denom);
    let k = (roughness + 1.0) * (roughness + 1.0) / 8.0;
    let g = (ndv / (ndv * (1.0 - k) + k)) * (ndl / (ndl * (1.0 - k) + k));
    let f0 = mix(vec3f(0.04), albedo, metallic);
    let f = f0 + (vec3f(1.0) - f0) * pow(1.0 - hdv, 5.0);
    let specular = ndf * g * f / (4.0 * ndv * ndl + 1e-4);
    let kd = (vec3f(1.0) - f) * (1.0 - metallic);

    let shadow = shadow_factor(in.world, ndl);
    let light = vec3f(2.6); // white directional intensity
    let lo = (kd * albedo / PI + specular) * light * ndl * shadow;
    if (d.params.z > 0.5 && d.params.z < 1.5) {
        // Unlit (grids, gizmos, debug draw): the caller's color is already
        // display-space — skip lighting, tone map and gamma.
        return vec4f(d.color.rgb * texel.rgb, d.color.a * texel.a);
    }
    if (d.params.z >= 1.5) {
        // QtQuick3D's DefaultMaterial: diffuse only, no PBR, no tone map
        // and no gamma — the colour is scaled straight by the light term.
        // MEASURED off a Qt render: the unlit side of a white sphere under
        // an ambientColor of 0.2 reads 50/255 and the lit crown clips at
        // 255, which is clamp(0.2 + N·L) in DISPLAY space.
        // A light casts shadows only when asked to (QtQuick3D's
        // DirectionalLight.castsShadow is false by default), so mode 3
        // skips the shadow map entirely.
        var sh = 1.0;
        if (d.params.z < 2.5) { sh = shadow_factor(in.world, ndl); }
        let lam = clamp(d.params.w + ndl * sh, 0.0, 1.0);
        return vec4f(d.color.rgb * texel.rgb * lam, d.color.a * texel.a);
    }
    let ambient = albedo * (0.05 + d.params.w);
    var c = ambient + lo;
    // Reinhard tone map + gamma encode for the unorm target.
    c = c / (c + vec3f(1.0));
    c = pow(c, vec3f(1.0 / 2.2));
    return vec4f(c, d.color.a * texel.a);
}` + skinCommonWGSL + `
// Skinned twin of vs: the palette deforms position and normal in mesh
// space, then the ordinary per-draw model matrix places the result. The
// fragment stage is shared, so a skinned actor shades identically to a
// static mesh.
@vertex
fn vs_skin(@location(0) pos : vec3f, @location(1) normal : vec3f, @location(2) uv : vec2f,
           @location(4) joints : vec4<u32>, @location(5) weights : vec4f,
           @builtin(instance_index) iid : u32) -> VSOut {
    let s = skin_matrix(joints, weights);
    let sp = (s * vec4f(pos, 1.0)).xyz;
    let sn = skin_dir((s * vec4f(normal, 0.0)).xyz);
    let world = world_pos(sp, iid);
    var out : VSOut;
    out.pos = frame.view_proj * world;
    out.world = world.xyz;
    out.normal = (draws[iid].model * vec4f(sn, 0.0)).xyz;
    out.uv = uv;
    out.draw_id = iid;
    return out;
}`

// New3D builds the renderer for a target format.
func New3D(device *gpu.Device, queue *gpu.Queue, format gpu.TextureFormat) (*Renderer3D, error) {
	m, err := device.CreateShaderModuleWGSL("renderer3d", shader3D)
	if err != nil {
		return nil, err
	}
	defer m.Release()
	layouts := vertexLayout3D(3)
	pipeline, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: m, VertexEntry: "vs", FragmentEntry: "fs",
		Format: format, DepthTest: true, CullBack: true,
		VertexLayouts: layouts,
	})
	if err != nil {
		return nil, err
	}
	shadowPL, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: m, VertexEntry: "vs_shadow",
		DepthTest: true, CullBack: true,
		VertexLayouts: layouts,
	})
	if err != nil {
		return nil, err
	}
	linePL, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: m, VertexEntry: "vs", FragmentEntry: "fs",
		Format: format, DepthTest: true, Lines: true,
		VertexLayouts: layouts,
	})
	if err != nil {
		return nil, err
	}

	r := &Renderer3D{
		device:        device,
		queue:         queue,
		format:        format,
		pipeline:      pipeline,
		shadowPL:      shadowPL,
		linePL:        linePL,
		layout:        pipeline.BindGroupLayout(0),
		shLayout:      shadowPL.BindGroupLayout(0),
		texLayout:     pipeline.BindGroupLayout(1),
		sampler:       device.CreateSamplerRepeat(true), // tiling UVs; [0,1] UVs unaffected
		shadowSampler: device.CreateComparisonSampler(),
		shadowRadius:  10,
	}
	r.shadowMap = device.CreateShadowMap(shadowMapSize)
	r.shadowView = r.shadowMap.CreateView()
	r.frame = device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, int(unsafe.Sizeof(frameUniforms{})))
	r.growDraws(24 * 1024)
	r.white = r.NewImage3D(1, 1, []byte{255, 255, 255, 255})
	lineTex := linePL.BindGroupLayout(1)
	r.lineWhite = r.device.CreateBindGroup(lineTex,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: r.white.view},
	)
	lineTex.Release()
	if err := r.initMaterialPass(format); err != nil {
		r.Release()
		return nil, err
	}
	return r, nil
}

// vertexLayout3D is the full Vertex3D layout (position, normal, uv,
// tangent). The legacy pipelines declare only the first three attributes;
// the material pipelines take all four.
func vertexLayout3D(attrs int) []gpu.VertexLayout {
	all := []gpu.VertexAttr{
		{Format: gpu.VertexFloat32x3, Offset: 0, Location: 0},
		{Format: gpu.VertexFloat32x3, Offset: 12, Location: 1},
		{Format: gpu.VertexFloat32x2, Offset: 24, Location: 2},
		{Format: gpu.VertexFloat32x4, Offset: 32, Location: 3},
	}
	return []gpu.VertexLayout{{Stride: int(unsafe.Sizeof(Vertex3D{})), Attrs: all[:attrs]}}
}

// initMaterialPass compiles the material shader into its four pipeline
// variants plus a depth-only shadow pipeline, and allocates the per-draw,
// light and fallback-texture resources they need.
func (r *Renderer3D) initMaterialPass(format gpu.TextureFormat) error {
	m, err := r.device.CreateShaderModuleWGSL("renderer3d-material", shaderMaterial)
	if err != nil {
		return err
	}
	defer m.Release()
	layouts := vertexLayout3D(4)
	for v := range matSkinBit {
		pl, err := r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
			Shader: m, VertexEntry: "vs", FragmentEntry: "fs",
			Format: format, DepthTest: true,
			CullBack:      v&1 == 0,
			AlphaBlend:    v&2 != 0,
			VertexLayouts: layouts,
		})
		if err != nil {
			return err
		}
		r.matPL[v] = pl
		r.matLayout[v] = pl.BindGroupLayout(0)
		r.matTexLayout[v] = pl.BindGroupLayout(1)
	}
	r.matShadowPL, err = r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: m, VertexEntry: "vs_shadow",
		DepthTest: true, CullBack: true,
		VertexLayouts: layouts,
	})
	if err != nil {
		return err
	}
	r.matShLayout = r.matShadowPL.BindGroupLayout(0)
	r.lightBuf = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, int(unsafe.Sizeof(lightBlock{})))
	r.mipSampler = r.device.CreateSamplerMip(true, 8)
	r.flatNormal = r.NewImage3D(1, 1, []byte{128, 128, 255, 255})
	r.black = r.NewImage3D(1, 1, []byte{0, 0, 0, 255})
	r.settings = DefaultFrameSettings()
	r.growMDraws(16 * 1024)
	return nil
}

// growMDraws (re)allocates the material per-draw storage buffer and the
// bind groups that reference it.
func (r *Renderer3D) growMDraws(size int) {
	for v := range matVariants {
		if r.matBind[v] != nil {
			r.matBind[v].Release()
			r.matBind[v] = nil
		}
	}
	if r.matShadowBind != nil {
		r.matShadowBind.Release()
	}
	if r.mdraws != nil {
		r.mdraws.Release()
	}
	r.mdraws = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, size)
	for v := range matVariants {
		if r.matLayout[v] == nil {
			continue // a skinned variant no draw has asked for yet
		}
		r.matBind[v] = r.device.CreateBindGroup(r.matLayout[v],
			gpu.BindGroupEntry{Binding: 0, Buffer: r.frame},
			gpu.BindGroupEntry{Binding: 1, Buffer: r.mdraws},
			gpu.BindGroupEntry{Binding: 2, Sampler: r.shadowSampler},
			gpu.BindGroupEntry{Binding: 3, TextureView: r.shadowView},
			gpu.BindGroupEntry{Binding: 4, Buffer: r.lightBuf},
		)
	}
	r.matShadowBind = r.device.CreateBindGroup(r.matShLayout,
		gpu.BindGroupEntry{Binding: 0, Buffer: r.frame},
		gpu.BindGroupEntry{Binding: 1, Buffer: r.mdraws},
	)
	r.rebuildMatSkinShadowBind()
}

func (r *Renderer3D) growDraws(size int) {
	if r.bind != nil {
		r.bind.Release()
	}
	if r.shadowBind != nil {
		r.shadowBind.Release()
	}
	if r.draws != nil {
		r.draws.Release()
	}
	r.draws = r.device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, size)
	r.bind = r.device.CreateBindGroup(r.layout,
		gpu.BindGroupEntry{Binding: 0, Buffer: r.frame},
		gpu.BindGroupEntry{Binding: 1, Buffer: r.draws},
		gpu.BindGroupEntry{Binding: 2, Sampler: r.shadowSampler},
		gpu.BindGroupEntry{Binding: 3, TextureView: r.shadowView},
	)
	r.shadowBind = r.device.CreateBindGroup(r.shLayout,
		gpu.BindGroupEntry{Binding: 0, Buffer: r.frame},
		gpu.BindGroupEntry{Binding: 1, Buffer: r.draws},
	)
	if r.lineBind != nil {
		r.lineBind.Release()
	}
	lineLayout := r.linePL.BindGroupLayout(0)
	r.lineBind = r.device.CreateBindGroup(lineLayout,
		gpu.BindGroupEntry{Binding: 0, Buffer: r.frame},
		gpu.BindGroupEntry{Binding: 1, Buffer: r.draws},
		gpu.BindGroupEntry{Binding: 2, Sampler: r.shadowSampler},
		gpu.BindGroupEntry{Binding: 3, TextureView: r.shadowView},
	)
	lineLayout.Release()
	r.rebuildSkinBinds()
}

// NewImage3D uploads tightly packed RGBA8 pixels as a 3D texture map.
func (r *Renderer3D) NewImage3D(w, h int, rgba []byte) *Image3D {
	tex := r.device.CreateTexture2D(w, h, gpu.FormatRGBA8Unorm, gpu.TextureBinding|gpu.TextureCopyDst)
	r.queue.WriteTexture(tex, w, h, rgba)
	view := tex.CreateView()
	bind := r.device.CreateBindGroup(r.texLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: view},
	)
	return &Image3D{tex: tex, view: view, bind: bind}
}

func (r *Renderer3D) Release() {
	for _, m := range []*Mesh{r.unitCube, r.unitSphere, r.unitCylinder, r.unitCone, r.unitPlane} {
		if m != nil {
			m.Release()
		}
	}
	rel(r.white)
	rel(r.flatNormal)
	rel(r.black)
	rel(r.sampler)
	rel(r.mipSampler)
	rel(r.shadowSampler)
	rel(r.shadowView)
	rel(r.shadowMap)
	rel(r.bind)
	rel(r.lineBind)
	rel(r.lineWhite)
	rel(r.linePL)
	rel(r.shadowBind)
	rel(r.draws)
	rel(r.frame)
	rel(r.lightBuf)
	rel(r.mdraws)
	rel(r.matShadowBind)
	rel(r.matShLayout)
	rel(r.matShadowPL)
	rel(r.layout)
	rel(r.shLayout)
	rel(r.texLayout)
	rel(r.pipeline)
	rel(r.shadowPL)
	r.releaseSkin()
	for v := range matVariants {
		rel(r.matBind[v])
		rel(r.matLayout[v])
		rel(r.matTexLayout[v])
		rel(r.matPL[v])
	}
}

// releasable is a GPU resource handle: a pointer type with a Release
// method. rel releases one unless it is nil, which keeps Release usable on
// a partially constructed renderer (New3D's error path).
type releasable interface {
	comparable
	Release()
}

func rel[T releasable](x T) {
	var zero T
	if x != zero {
		x.Release()
	}
}

// NewMesh uploads indexed geometry.
func (r *Renderer3D) NewMesh(verts []Vertex3D, indices []uint32) *Mesh {
	// Normal mapping needs a tangent frame; derive one from the UVs unless
	// the mesh source already supplied it.
	if !hasTangents(verts) {
		generateTangents(verts, indices)
	}
	vb := r.device.CreateBuffer(gpu.BufferVertex|gpu.BufferCopyDst, len(verts)*int(unsafe.Sizeof(Vertex3D{})))
	r.queue.WriteBuffer(vb, 0, unsafe.Slice((*byte)(unsafe.Pointer(&verts[0])), len(verts)*int(unsafe.Sizeof(Vertex3D{}))))
	ib := r.device.CreateBuffer(gpu.BufferIndex|gpu.BufferCopyDst, len(indices)*4)
	r.queue.WriteBuffer(ib, 0, unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*4))
	m := &Mesh{vbuf: vb, ibuf: ib, indices: len(indices), verts: verts, idx: indices}
	m.bmin, m.bmax = verts[0].Pos, verts[0].Pos
	for _, v := range verts[1:] {
		m.bmin = core.V3(min(m.bmin.X, v.Pos.X), min(m.bmin.Y, v.Pos.Y), min(m.bmin.Z, v.Pos.Z))
		m.bmax = core.V3(max(m.bmax.X, v.Pos.X), max(m.bmax.Y, v.Pos.Y), max(m.bmax.Z, v.Pos.Z))
	}
	return m
}

// uploadVerts overwrites a mesh's vertex buffer (CPU skinning re-upload;
// verts must have the mesh's original count).
func (r *Renderer3D) uploadVerts(m *Mesh, verts []Vertex3D) {
	if len(verts) == 0 {
		return
	}
	r.queue.WriteBuffer(m.vbuf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&verts[0])), len(verts)*int(unsafe.Sizeof(Vertex3D{}))))
}

// SetShadowArea centers the directional light's shadow volume; everything
// within radius of center casts and receives shadows.
func (r *Renderer3D) SetShadowArea(center core.Vec3, radius float32) {
	r.shadowCenter, r.shadowRadius = center, radius
}

// Begin starts a frame with the camera and light setup.
func (r *Renderer3D) Begin(viewProj core.Mat4, lightDir, camPos core.Vec3) {
	ld := lightDir.Normalize()
	eye := r.shadowCenter.Sub(ld.Scale(2 * r.shadowRadius))
	up := core.V3(0, 1, 0)
	if ld.Y > 0.99 || ld.Y < -0.99 {
		up = core.V3(1, 0, 0)
	}
	lightView := core.Mat4LookAt(eye, r.shadowCenter, up)
	s := r.shadowRadius
	lightProj := core.Mat4Ortho(-s, s, -s, s, 0.1, 4.5*s)

	fs := r.settings
	if fs.Exposure <= 0 {
		fs.Exposure = 1
	}
	n := r.packLights(&r.lights)
	r.queue.WriteBuffer(r.lightBuf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&r.lights)), unsafe.Sizeof(r.lights)))

	fogOn := float32(0)
	if fs.Fog.Enabled {
		fogOn = 1
	}
	u := frameUniforms{
		ViewProj:   viewProj,
		LightVP:    lightProj.Mul(lightView),
		LightDir:   core.V4(ld.X, ld.Y, ld.Z, sunIntensity),
		CamPos:     core.V4(camPos.X, camPos.Y, camPos.Z, 1),
		Ambient:    core.V4(fs.Ambient.R, fs.Ambient.G, fs.Ambient.B, fs.Exposure),
		FogColor:   core.V4(fs.Fog.Color.R, fs.Fog.Color.G, fs.Fog.Color.B, fs.Fog.Density),
		FogParams:  core.V4(fs.Fog.Height, fs.Fog.HeightDensity, fs.Fog.AerialPerspective, fogOn),
		SkyTop:     core.V4(fs.SkyTop.R, fs.SkyTop.G, fs.SkyTop.B, 1),
		SkyHorizon: core.V4(fs.SkyHorizon.R, fs.SkyHorizon.G, fs.SkyHorizon.B, 1),
		Misc:       core.V4(float32(fs.Tonemap), float32(n), 0, 0),
	}
	r.queue.WriteBuffer(r.frame, 0, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u)))
	r.items = r.items[:0]
	r.data = r.data[:0]
	r.mitems = r.mitems[:0]
	r.mdata = r.mdata[:0]
	r.camPos = camPos
	r.pickTag = -1
}

// DrawMaterial queues a mesh shaded by a full Material: albedo, normal, ARM
// or roughness and emissive maps, optionally projected triplanar and/or
// hex-tiled, lit by the directional light (with its shadow) plus every
// point and spot light set for the frame, then fogged and tone mapped.
//
// A material with Alpha < 1 is drawn after all opaque geometry, sorted back
// to front.
func (r *Renderer3D) DrawMaterial(mesh *Mesh, model core.Mat4, m *Material) {
	r.DrawInstanced(mesh, []core.Mat4{model}, m)
}

// DrawInstanced queues one mesh drawn once per model matrix as a single
// instanced draw call — the path for thousands of foliage or debris
// instances. Every instance shares the material; per-instance transforms go
// into the material draw buffer, which the vertex stage indexes by
// instance_index.
func (r *Renderer3D) DrawInstanced(mesh *Mesh, models []core.Mat4, m *Material) {
	r.drawInstanced(mesh, models, m, nil, nil)
}

// drawInstanced is DrawInstanced plus the optional GPU-skinning binding a
// Model attaches to a skinned primitive (one instance only: a palette
// describes a single pose).
func (r *Renderer3D) drawInstanced(mesh *Mesh, models []core.Mat4, m *Material,
	sattrs *gpu.Buffer, spal *skinPalette) {
	if mesh == nil || len(models) == 0 {
		return
	}
	if m == nil {
		m = &Material{}
	}
	res := m.resolve()
	base := len(r.mdata) / matDrawFloats
	for _, model := range models {
		r.mdata = append(r.mdata, model[:]...)
		r.mdata = append(r.mdata, res.color.R, res.color.G, res.color.B, res.color.A)
		r.mdata = append(r.mdata, res.metallic, res.roughness, res.normalScale, res.uvScale)
		r.mdata = append(r.mdata, res.flags, res.emission, res.alpha, res.rchan)
		r.mdata = append(r.mdata, res.antiRot, 0, 0, 0)
	}
	blend := res.alpha < 1
	if spal != nil && !r.ensureMatSkin(matVariant(blend, m.DoubleSided, true)) {
		// The device refused the skinned material pipelines: draw the bind
		// pose rather than nothing (the CPU fallback is chosen per model in
		// Model.useGPUSkin, and cannot be switched mid-frame).
		sattrs, spal = nil, nil
	}
	it := matItem{
		mesh:    mesh,
		mat:     m,
		base:    base,
		count:   len(models),
		variant: matVariant(blend, m.DoubleSided, spal != nil),
		model:   models[0],
		tag:     r.pickTag,
		sattrs:  sattrs,
		spal:    spal,
	}
	if blend {
		// Sort key: distance from the camera to the first instance's
		// origin, so transparent surfaces composite back to front.
		o := core.V3(models[0][12], models[0][13], models[0][14])
		it.depth = o.Sub(r.camPos).Length()
	}
	r.mitems = append(r.mitems, it)
}

// matDrawFloats is the float count of one WGSL MDraw record: a mat4x4 plus
// four vec4s.
const matDrawFloats = 16 + 4 + 4 + 4 + 4

// Draw queues one mesh with a model transform and base color
// (non-metallic, mid roughness).
func (r *Renderer3D) Draw(mesh *Mesh, model core.Mat4, color RGBA) {
	r.DrawPBR(mesh, model, color, 0, 0.6, nil)
}

// DrawTextured queues a textured mesh (nil = plain color).
func (r *Renderer3D) DrawTextured(mesh *Mesh, model core.Mat4, color RGBA, img *Image3D) {
	r.DrawPBR(mesh, model, color, 0, 0.6, img)
}

// DrawPBR queues a mesh with full material parameters.
func (r *Renderer3D) DrawPBR(mesh *Mesh, model core.Mat4, color RGBA, metallic, roughness float32, img *Image3D) {
	r.push(mesh, model, color, metallic, roughness, 0, img)
}

// DrawUnlit queues a mesh drawn with its flat color, ignoring lighting —
// grids, gizmos, debug shapes.
func (r *Renderer3D) DrawUnlit(mesh *Mesh, model core.Mat4, color RGBA) {
	r.push(mesh, model, color, 0, 0.6, 1, r.white)
}

// DrawLambert queues a mesh shaded the way QtQuick3D's DefaultMaterial is:
// diffuse only, in display space, no tone map — colour * clamp(ambient +
// N·L). It is deliberately NOT the PBR path: a DefaultMaterial page states
// flat-ish colours and expects them back (a white sphere stays white).
func (r *Renderer3D) DrawLambert(mesh *Mesh, model core.Mat4, color RGBA, ambient float32, shadows bool) {
	mode := float32(3) // lambert, shadow map skipped
	if shadows {
		mode = 2
	}
	r.pushA(mesh, model, color, 0, 0.6, mode, ambient, nil)
}

// DrawShaded queues a mesh with a CAD-viewer look: PBR shading lifted by a
// strong ambient term so unlit faces stay readable (Shapr3D "Shaded").
// img is an optional texture (nil = plain color).
func (r *Renderer3D) DrawShaded(mesh *Mesh, model core.Mat4, color RGBA, ambient float32, img *Image3D) {
	r.pushA(mesh, model, color, 0.1, 0.55, 0, ambient, img)
}

func (r *Renderer3D) push(mesh *Mesh, model core.Mat4, color RGBA, metallic, roughness, unlit float32, img *Image3D) {
	r.pushA(mesh, model, color, metallic, roughness, unlit, 0, img)
}

// drawSkinnedPBR is DrawPBR for a GPU-skinned primitive: the same per-draw
// record, plus the joint/weight buffer and palette the skinned pipeline
// needs. Used by Model.draw; not public, because the bindings only make
// sense for geometry a Model owns.
func (r *Renderer3D) drawSkinnedPBR(mesh *Mesh, model core.Mat4, color RGBA,
	metallic, roughness float32, img *Image3D, sattrs *gpu.Buffer, spal *skinPalette) {
	r.pushSkin(mesh, model, color, metallic, roughness, 0, 0, img, sattrs, spal)
}

func (r *Renderer3D) pushA(mesh *Mesh, model core.Mat4, color RGBA, metallic, roughness, unlit, ambient float32, img *Image3D) {
	r.pushSkin(mesh, model, color, metallic, roughness, unlit, ambient, img, nil, nil)
}

func (r *Renderer3D) pushSkin(mesh *Mesh, model core.Mat4, color RGBA, metallic, roughness, unlit, ambient float32,
	img *Image3D, sattrs *gpu.Buffer, spal *skinPalette) {
	if img == nil {
		img = r.white
	}
	r.items = append(r.items, drawItem{mesh: mesh, img: img, model: model, tag: r.pickTag,
		sattrs: sattrs, spal: spal})
	r.data = append(r.data, model[:]...)
	r.data = append(r.data, color.R, color.G, color.B, color.A)
	r.data = append(r.data, metallic, roughness, unlit, ambient)
}

// Render uploads the frame's draw list and records both passes — the
// shadow pass from the light, then the lit main pass — into enc.
func (r *Renderer3D) Render(enc *gpu.CommandEncoder, color, depth *gpu.TextureView, clear gpu.Color) error {
	if len(r.items) == 0 && len(r.mitems) == 0 {
		pass := enc.BeginRenderPassDepth(color, depth, clear)
		pass.End()
		return nil
	}
	if len(r.items) > 0 {
		data := floatBytes(r.data)
		if r.draws.Size() < len(data) {
			r.growDraws(max(len(data), 2*r.draws.Size()))
		}
		r.queue.WriteBuffer(r.draws, 0, data)
	}
	if len(r.mitems) > 0 {
		data := floatBytes(r.mdata)
		if r.mdraws.Size() < len(data) {
			r.growMDraws(max(len(data), 2*r.mdraws.Size()))
		}
		r.queue.WriteBuffer(r.mdraws, 0, data)
		r.sortBlended()
	}

	// One shadow pass for both draw lists — the second BeginShadowPass
	// would clear what the first wrote.
	sp := enc.BeginShadowPass(r.shadowView)
	if len(r.items) > 0 {
		r.drawItems(sp, false)
	}
	if len(r.mitems) > 0 {
		r.drawMatItems(sp, false)
	}
	sp.End()

	mp := enc.BeginRenderPassDepth(color, depth, clear)
	if len(r.items) > 0 {
		r.drawItems(mp, true)
	}
	if len(r.mitems) > 0 {
		r.drawMatItems(mp, true)
	}
	mp.End()
	return nil
}

// sortBlended orders the material draw list: opaque items keep submission
// order, then blended items follow sorted far-to-near so alpha composites
// correctly.
func (r *Renderer3D) sortBlended() {
	r.order = r.order[:0]
	for i, it := range r.mitems {
		if it.variant&2 == 0 {
			r.order = append(r.order, i)
		}
	}
	first := len(r.order)
	for i, it := range r.mitems {
		if it.variant&2 != 0 {
			r.order = append(r.order, i)
		}
	}
	blended := r.order[first:]
	sort.SliceStable(blended, func(a, b int) bool {
		return r.mitems[blended[a]].depth > r.mitems[blended[b]].depth
	})
}

// drawItems records the legacy draw list into one pass (main = the lit
// pass, false = the depth-only shadow pass). Items carrying a joint palette
// go to the skinned pipeline, which reads the joint/weight buffer from
// vertex slot 1 and the palette from its own bind group; the pipeline is
// switched only when the kind changes, so a run of actors costs one switch.
func (r *Renderer3D) drawItems(pass *gpu.RenderPass, main bool) {
	var lastBind *gpu.BindGroup
	var lastPal *skinPalette
	kind := -1
	for i, it := range r.items {
		k := 0
		if it.spal != nil {
			k = 1
		}
		if it.lines {
			if !main {
				continue // lines cast no shadow
			}
			k = 2
		}
		if k != kind {
			kind = k
			switch {
			case main && k == 2:
				pass.SetPipeline(r.linePL)
				pass.SetBindGroup(0, r.lineBind)
			case main && k == 1:
				pass.SetPipeline(r.skinPL)
				pass.SetBindGroup(0, r.skinBind)
			case main:
				pass.SetPipeline(r.pipeline)
				pass.SetBindGroup(0, r.bind)
			case k == 1:
				pass.SetPipeline(r.skinShadowPL)
				pass.SetBindGroup(0, r.skinShadowBind)
			default:
				pass.SetPipeline(r.shadowPL)
				pass.SetBindGroup(0, r.shadowBind)
			}
			lastBind, lastPal = nil, nil
		}
		if main {
			b := it.img.bind
			if k == 1 {
				b = r.skinImageBind(it.img)
			}
			if k == 2 {
				b = r.lineWhite
			}
			if b != lastBind {
				pass.SetBindGroup(1, b)
				lastBind = b
			}
		}
		if k == 1 {
			// One palette serves every primitive of a skin, so a model's
			// run of items rebinds it once.
			if it.spal != lastPal {
				lastPal = it.spal
				// Main pass: palette at group 2 (group 1 is the texture
				// set). The shadow module numbers the palette group 1.
				if main {
					pass.SetBindGroup(2, it.spal.group(r, palLegacy, r.skinPalLayout))
				} else {
					pass.SetBindGroup(1, it.spal.group(r, palLegacyShadow, r.skinShPalLayout))
				}
			}
			pass.SetVertexBuffer(1, it.sattrs)
		}
		pass.SetVertexBuffer(0, it.mesh.vbuf)
		pass.SetIndexBuffer(it.mesh.ibuf)
		pass.DrawIndexedInstanced(it.mesh.indices, 1, i)
	}
}

// drawMatItems records the material draw list. In the main pass it walks
// r.order (opaques first, then blended back-to-front) and switches pipeline
// only when the variant changes; in the shadow pass it draws the opaque
// items with the single depth-only pipeline already bound.
func (r *Renderer3D) drawMatItems(pass *gpu.RenderPass, textures bool) {
	if !textures {
		kind := -1
		for _, it := range r.mitems {
			if it.variant&2 != 0 {
				continue // blended surfaces do not cast shadows
			}
			k := 0
			if it.spal != nil {
				k = 1
			}
			if k != kind {
				kind = k
				if k == 1 {
					pass.SetPipeline(r.matSkinShadowPL)
					pass.SetBindGroup(0, r.matSkinShadowBind)
				} else {
					pass.SetPipeline(r.matShadowPL)
					pass.SetBindGroup(0, r.matShadowBind)
				}
			}
			if k == 1 {
				pass.SetBindGroup(1, it.spal.group(r, palMatShadow, r.matSkinShPalLayout))
				pass.SetVertexBuffer(1, it.sattrs)
			}
			pass.SetVertexBuffer(0, it.mesh.vbuf)
			pass.SetIndexBuffer(it.mesh.ibuf)
			pass.DrawIndexedInstanced(it.mesh.indices, it.count, it.base)
		}
		return
	}
	variant := -1
	var lastBind *gpu.BindGroup
	for _, i := range r.order {
		it := r.mitems[i]
		if it.variant != variant {
			variant = it.variant
			pass.SetPipeline(r.matPL[variant])
			pass.SetBindGroup(0, r.matBind[variant])
			lastBind = nil
		}
		if b := r.bindGroup(it.mat, variant); b != lastBind {
			pass.SetBindGroup(1, b)
			lastBind = b
		}
		if it.spal != nil {
			pass.SetBindGroup(2, it.spal.group(r, palMat+variant, r.matPalLayout[variant]))
			pass.SetVertexBuffer(1, it.sattrs)
		}
		pass.SetVertexBuffer(0, it.mesh.vbuf)
		pass.SetIndexBuffer(it.mesh.ibuf)
		pass.DrawIndexedInstanced(it.mesh.indices, it.count, it.base)
	}
}

// CubeMesh returns the shared unit cube (cached; owned by the renderer).
func (r *Renderer3D) CubeMesh() *Mesh {
	if r.unitCube == nil {
		r.unitCube = r.buildCubeMesh()
	}
	return r.unitCube
}

func (r *Renderer3D) buildCubeMesh() *Mesh {
	type face struct {
		n          core.Vec3
		a, b, c, d core.Vec3
	}
	h := float32(0.5)
	faces := []face{
		{core.V3(0, 0, 1), core.V3(-h, -h, h), core.V3(h, -h, h), core.V3(h, h, h), core.V3(-h, h, h)},
		{core.V3(0, 0, -1), core.V3(h, -h, -h), core.V3(-h, -h, -h), core.V3(-h, h, -h), core.V3(h, h, -h)},
		{core.V3(1, 0, 0), core.V3(h, -h, h), core.V3(h, -h, -h), core.V3(h, h, -h), core.V3(h, h, h)},
		{core.V3(-1, 0, 0), core.V3(-h, -h, -h), core.V3(-h, -h, h), core.V3(-h, h, h), core.V3(-h, h, -h)},
		{core.V3(0, 1, 0), core.V3(-h, h, h), core.V3(h, h, h), core.V3(h, h, -h), core.V3(-h, h, -h)},
		{core.V3(0, -1, 0), core.V3(-h, -h, -h), core.V3(h, -h, -h), core.V3(h, -h, h), core.V3(-h, -h, h)},
	}
	var verts []Vertex3D
	var idx []uint32
	for _, f := range faces {
		base := uint32(len(verts))
		for _, p := range []core.Vec3{f.a, f.b, f.c, f.d} {
			verts = append(verts, Vertex3D{Pos: p, Normal: f.n})
		}
		idx = append(idx, base, base+1, base+2, base, base+2, base+3)
	}
	return r.NewMesh(verts, idx)
}

// NewLines uploads a line mesh: verts joined by indices in pairs. It is
// drawn with DrawLines (1px line segments), never picked and casts no
// shadow. Vertex normals/UVs are unused.
func (r *Renderer3D) NewLines(verts []Vertex3D, indices []uint32) *Mesh {
	if len(verts) == 0 || len(indices) < 2 {
		return nil
	}
	vb := r.device.CreateBuffer(gpu.BufferVertex|gpu.BufferCopyDst, len(verts)*int(unsafe.Sizeof(Vertex3D{})))
	r.queue.WriteBuffer(vb, 0, unsafe.Slice((*byte)(unsafe.Pointer(&verts[0])), len(verts)*int(unsafe.Sizeof(Vertex3D{}))))
	ib := r.device.CreateBuffer(gpu.BufferIndex|gpu.BufferCopyDst, len(indices)*4)
	r.queue.WriteBuffer(ib, 0, unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*4))
	m := &Mesh{vbuf: vb, ibuf: ib, indices: len(indices) &^ 1, verts: verts, idx: indices, lines: true}
	m.bmin, m.bmax = verts[0].Pos, verts[0].Pos
	for _, v := range verts[1:] {
		m.bmin = core.V3(min(m.bmin.X, v.Pos.X), min(m.bmin.Y, v.Pos.Y), min(m.bmin.Z, v.Pos.Z))
		m.bmax = core.V3(max(m.bmax.X, v.Pos.X), max(m.bmax.Y, v.Pos.Y), max(m.bmax.Z, v.Pos.Z))
	}
	return m
}

// NewSegments is NewLines over a flat list of segment endpoints (pairs).
func (r *Renderer3D) NewSegments(pts []core.Vec3) *Mesh {
	verts := make([]Vertex3D, len(pts))
	idx := make([]uint32, len(pts)&^1)
	for i, p := range pts {
		verts[i] = Vertex3D{Pos: p, Normal: core.V3(0, 1, 0)}
	}
	for i := range idx {
		idx[i] = uint32(i)
	}
	return r.NewLines(verts, idx)
}

// DrawLines queues a line mesh (NewLines/NewSegments) unlit in the colour.
// Width is one pixel; draw a segment twice with a small offset for a
// heavier stroke.
func (r *Renderer3D) DrawLines(mesh *Mesh, model core.Mat4, color RGBA) {
	if mesh == nil || !mesh.lines {
		return
	}
	r.items = append(r.items, drawItem{mesh: mesh, img: r.white, model: model, tag: -1, lines: true})
	r.data = append(r.data, model[:]...)
	r.data = append(r.data, color.R, color.G, color.B, color.A)
	r.data = append(r.data, 0, 0.6, 1, 0)
}
