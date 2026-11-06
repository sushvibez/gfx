//go:build !js

package render

// A custom-shader quad: what a QML ShaderEffect draws.
//
// The shaders come compiled (see qsb.go) — SPIR-V, which wgpu runs through
// naga's frontend like any other shader — so this needs no GLSL compiler.
// The quad is drawn INLINE in the 2D pass rather than into an offscreen
// texture: it breaks the batch (its own pipeline and bind group), blends
// over whatever is already there, and takes the scissor clip in force.
//
// Qt's convention is the whole contract: a std140 uniform block at binding
// 0 whose first two members are qt_Matrix and qt_Opacity, the effect's own
// properties after them, sampler2Ds at the bindings that follow, and the
// vertex inputs qt_Vertex / qt_MultiTexCoord0. The vertices are written in
// CLIP space with qt_Matrix left as the identity, so the item's transform,
// DPI scale and camera come along for free through the same toNDC the rest
// of the renderer uses.

import (
	"fmt"

	"github.com/sushvibez/gfx/gpu"
)

// ShaderFX is one compiled ShaderEffect: its pipeline, its uniform block
// and the textures it samples.
type ShaderFX struct {
	pipe    *gpu.RenderPipeline
	layout  *gpu.BindGroupLayout
	uni     *gpu.Buffer
	verts   *gpu.Buffer
	bind    *gpu.BindGroup
	uniSize int
	samples int
	format  gpu.TextureFormat
	// What the bind group currently holds, so a source texture that
	// appears (or is re-created on a resize) rebuilds it: a captured
	// layer's texture only exists after its first offscreen pass.
	splits []SamplerSplit
	texs   []ShaderTexture
	bound  []*Image
}

// NewShaderFX builds the pipeline for a vertex/fragment .qsb pair. tex are
// the images its samplers read, in binding order (nil entries fall back to
// the renderer's 1x1 white).
func (r *Renderer2D) NewShaderFX(label string, vert, frag *QSB, tex []*Image) (*ShaderFX, error) {
	if vert == nil || frag == nil {
		return nil, fmt.Errorf("render: %s: needs both a vertex and a fragment shader", label)
	}
	// A GLSL `sampler2D` compiles to a SPIR-V COMBINED image sampler, which
	// WebGPU's binding model has no place for — a texture and a sampler
	// are separate bindings there. The module is rewritten to say the same
	// thing that way (SplitCombinedSamplers), which is what lets a Qt
	// shader that samples anything run at all.
	vspv, _, err := SplitCombinedSamplers(vert.SPIRV)
	if err != nil {
		return nil, err
	}
	fspv, splits, err := SplitCombinedSamplers(frag.SPIRV)
	if err != nil {
		return nil, err
	}
	// Both stages must declare the SAME uniform block, or wgpu rejects the
	// pipeline: Qt compiles each against only the members it uses, so the
	// shorter one is padded (see PadUniformBlock).
	block := max(vert.Size, frag.Size)
	if vspv, err = PadUniformBlock(vspv, block); err != nil {
		return nil, err
	}
	if fspv, err = PadUniformBlock(fspv, block); err != nil {
		return nil, err
	}
	// wgpu reports a shader it cannot accept through the device's error
	// callback and still returns an object, so the count is read around
	// every call here: a shader we did not write is allowed to fail, and
	// using the broken pipeline would take the process down.
	errs := gpu.ErrorCount()
	vm, err := r.device.CreateShaderModuleSPIRV(label+".vert", vspv)
	if err != nil {
		return nil, err
	}
	defer vm.Release()
	fm, err := r.device.CreateShaderModuleSPIRV(label+".frag", fspv)
	if err != nil {
		return nil, err
	}
	defer fm.Release()
	size := max(frag.Size, vert.Size)
	if size <= 0 {
		size = 16
	}
	fx := &ShaderFX{uniSize: (size + 15) / 16 * 16, samples: r.samples, format: r.format}
	// Qt's scene graph blends PREMULTIPLIED alpha, and a ShaderEffect's
	// fragment output follows that convention.
	blend := &gpu.BlendDesc{
		Color: gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendOne, Dst: gpu.BlendOneMinusSrcAlpha},
		Alpha: gpu.BlendComponent{Op: gpu.BlendAdd, Src: gpu.BlendOne, Dst: gpu.BlendOneMinusSrcAlpha},
	}
	pipe, err := r.device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: vm, FragmentShader: fm,
		VertexEntry: vert.EntryPoint, FragmentEntry: frag.EntryPoint,
		Format: r.format, Blend: blend, Samples: r.samples,
		VertexLayouts: []gpu.VertexLayout{{
			Stride: 6 * 4,
			Attrs: []gpu.VertexAttr{
				{Format: gpu.VertexFloat32x4, Offset: 0, Location: 0},  // qt_Vertex
				{Format: gpu.VertexFloat32x2, Offset: 16, Location: 1}, // qt_MultiTexCoord0
			},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("render: %s: %v", label, err)
	}
	if gpu.ErrorCount() != errs {
		pipe.Release()
		return nil, fmt.Errorf("render: %s: the GPU rejected the shaders", label)
	}
	fx.pipe = pipe
	fx.layout = pipe.BindGroupLayout(0)
	fx.uni = r.device.CreateBuffer(gpu.BufferUniform|gpu.BufferCopyDst, fx.uniSize)
	fx.verts = r.device.CreateBuffer(gpu.BufferVertex|gpu.BufferCopyDst, 6*6*4)
	fx.splits, fx.texs = splits, frag.Textures
	fx.bindTextures(r, tex)
	return fx, nil
}

// bindTextures (re)builds the bind group for a set of source textures.
func (fx *ShaderFX) bindTextures(r *Renderer2D, tex []*Image) {
	entries := []gpu.BindGroupEntry{{Binding: 0, Buffer: fx.uni}}
	fx.bound = make([]*Image, len(fx.texs))
	for i, t := range fx.texs {
		img := r.white
		if i < len(tex) && tex[i] != nil && tex[i].view != nil {
			img = tex[i]
		}
		fx.bound[i] = img
		if img == nil || img.view == nil {
			continue
		}
		// The texture keeps the binding the shader declared; its sampler
		// went to the binding the rewrite reports.
		entries = append(entries, gpu.BindGroupEntry{Binding: t.Binding, TextureView: img.view})
		for _, sp := range fx.splits {
			if sp.TextureBinding == t.Binding {
				entries = append(entries,
					gpu.BindGroupEntry{Binding: sp.SamplerBinding, Sampler: r.sampler})
			}
		}
	}
	if fx.bind != nil {
		fx.bind.Release()
	}
	fx.bind = r.device.CreateBindGroup(fx.layout, entries...)
}

// SetTextures points the effect at the images its samplers should read,
// rebuilding the bind group only when they actually changed.
func (fx *ShaderFX) SetTextures(r *Renderer2D, tex []*Image) {
	if fx == nil || fx.layout == nil {
		return
	}
	same := true
	for i := range fx.texs {
		var want *Image = r.white
		if i < len(tex) && tex[i] != nil && tex[i].view != nil {
			want = tex[i]
		}
		if i >= len(fx.bound) || fx.bound[i] != want {
			same = false
			break
		}
	}
	if same {
		return
	}
	fx.bindTextures(r, tex)
}

// SetUniforms writes the uniform block. The caller lays the bytes out at
// the offsets the reflection reported.
func (fx *ShaderFX) SetUniforms(r *Renderer2D, data []byte) {
	if fx == nil || fx.uni == nil || len(data) == 0 {
		return
	}
	if len(data) > fx.uniSize {
		data = data[:fx.uniSize]
	}
	r.queue.WriteBuffer(fx.uni, 0, data)
}

// Release frees the effect's GPU objects.
func (fx *ShaderFX) Release() {
	if fx == nil {
		return
	}
	if fx.bind != nil {
		fx.bind.Release()
		fx.bind = nil
	}
	if fx.layout != nil {
		fx.layout.Release()
		fx.layout = nil
	}
	if fx.uni != nil {
		fx.uni.Release()
		fx.uni = nil
	}
	if fx.verts != nil {
		fx.verts.Release()
		fx.verts = nil
	}
	if fx.pipe != nil {
		fx.pipe.Release()
		fx.pipe = nil
	}
}

// draw records the effect's quad into a pass. Called from the flush, which
// keeps the batch order.
func (fx *ShaderFX) draw(pass *gpu.RenderPass, count int) {
	if fx == nil || fx.pipe == nil || fx.bind == nil {
		return
	}
	pass.SetPipeline(fx.pipe)
	pass.SetVertexBuffer(0, fx.verts)
	pass.SetBindGroup(0, fx.bind)
	pass.Draw(count, 1, 0, 0)
}

// Valid reports whether the effect built and still matches the renderer's
// target (a resized window rebuilds the pipelines).
func (fx *ShaderFX) Valid(r *Renderer2D) bool {
	return fx != nil && fx.pipe != nil && fx.samples == r.samples && fx.format == r.format
}

// DrawShaderFX queues the effect's quad over a pixel rectangle. The
// vertices go in clip space (see the file comment), so the transform stack
// and DPI scale apply as they do to any other primitive.
func (r *Renderer2D) DrawShaderFX(fx *ShaderFX, x, y, w, h float32) {
	if !fx.Valid(r) {
		return
	}
	tlx, tly := r.toNDC(x, y)
	trx, try := r.toNDC(x+w, y)
	brx, bry := r.toNDC(x+w, y+h)
	blx, bly := r.toNDC(x, y+h)
	verts := []float32{
		tlx, tly, 0, 1, 0, 0,
		trx, try, 0, 1, 1, 0,
		brx, bry, 0, 1, 1, 1,
		tlx, tly, 0, 1, 0, 0,
		brx, bry, 0, 1, 1, 1,
		blx, bly, 0, 1, 0, 1,
	}
	r.queue.WriteBuffer(fx.verts, 0, floatBytes(verts))
	r.noteGroupNDC(tlx, tly, brx, bry)
	// Its own pipeline, bind group and vertex buffer: always a batch of its
	// own, never merged with the one before it.
	r.batches = append(r.batches, batch{
		kind: kindShaderFX, clip: r.clip(), fx: fx, count: 6,
	})
}
