//go:build js

package gpu

import (
	"fmt"
	"syscall/js"
)

// Browser WebGPU backend: the same public API as the native backend, mapped
// onto navigator.gpu via syscall/js. Command recording is sync JS calls;
// only adapter/device acquisition awaits promises (DESIGN.md §5, §8).

// SetPresentOverride is accepted for API parity; the browser owns canvas
// presentation timing, so there is no present mode to force.
func SetPresentOverride(mode string) {}

// TextureFormat is a WebGPU canvas/texture format ("bgra8unorm", ...).
// Opaque to callers; obtained from Surface.PreferredFormat.
type TextureFormat string

// Color is a render-pass clear color (linear, 0..1).
type Color struct{ R, G, B, A float64 }

// awaitPromise blocks the calling goroutine until the promise settles.
// Safe on js/wasm as long as the caller is not the JS event loop itself —
// which is true for code running under app.Window.EachFrame or main.
func awaitPromise(p js.Value) (js.Value, error) {
	done := make(chan struct{})
	var result js.Value
	var rejected js.Value
	ok := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			result = args[0]
		}
		close(done)
		return nil
	})
	fail := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			rejected = args[0]
		} else {
			rejected = js.ValueOf("promise rejected")
		}
		close(done)
		return nil
	})
	defer ok.Release()
	defer fail.Release()
	p.Call("then", ok, fail)
	<-done
	if rejected.Truthy() {
		msg := rejected
		if m := rejected.Get("message"); m.Truthy() {
			msg = m
		}
		return js.Value{}, fmt.Errorf("gpu: %s", msg.String())
	}
	return result, nil
}

// Instance is the WebGPU entry point (navigator.gpu).
type Instance struct{ gpu js.Value }

// NewInstance returns the browser's WebGPU instance. libPaths is ignored on
// web (there is no native library).
func NewInstance(libPaths ...string) (*Instance, error) {
	nav := js.Global().Get("navigator")
	if !nav.Truthy() || !nav.Get("gpu").Truthy() {
		return nil, fmt.Errorf("gpu: WebGPU not available in this browser")
	}
	return &Instance{gpu: nav.Get("gpu")}, nil
}

func (i *Instance) Release() {}

// CreateSurfaceCanvas wraps a <canvas> element in a surface.
func (i *Instance) CreateSurfaceCanvas(canvas js.Value) (*Surface, error) {
	ctx := canvas.Call("getContext", "webgpu")
	if !ctx.Truthy() {
		return nil, fmt.Errorf("gpu: canvas.getContext(\"webgpu\") failed")
	}
	return &Surface{gpu: i.gpu, canvas: canvas, ctx: ctx}, nil
}

// RequestAdapter picks an adapter. The compatible surface is implicit in
// the browser (the canvas context), so the argument is unused here.
func (i *Instance) RequestAdapter(_ *Surface) (*Adapter, error) {
	v, err := awaitPromise(i.gpu.Call("requestAdapter"))
	if err != nil {
		return nil, err
	}
	if !v.Truthy() {
		return nil, fmt.Errorf("gpu: requestAdapter returned null (WebGPU disabled?)")
	}
	return &Adapter{v: v}, nil
}

// Adapter is a browser GPUAdapter.
type Adapter struct{ v js.Value }

func (a *Adapter) Release() {}

// Name returns whatever identity the browser exposes (GPUAdapterInfo).
func (a *Adapter) Name() string {
	info := a.v.Get("info")
	if !info.Truthy() {
		return "browser webgpu adapter"
	}
	desc := info.Get("description").String()
	vendor := info.Get("vendor").String()
	if desc == "" {
		desc = info.Get("architecture").String()
	}
	return fmt.Sprintf("%s (%s, webgpu)", desc, vendor)
}

// RequestDevice creates the logical device and wires uncaptured GPU errors
// to the console.
func (a *Adapter) RequestDevice() (*Device, error) {
	// Ask for the adapter's own buffer-size limits (WebGPU's defaults are
	// 256 MB / 128 MB; browsers allow far more), like the native backend.
	lim := Limits{MaxBufferSize: MaxBufferSize, MaxStorageBindingSize: MaxStorageBindingSize}
	desc := js.Global().Get("Object").New()
	if al := a.v.Get("limits"); al.Truthy() {
		req := js.Global().Get("Object").New()
		if v := al.Get("maxBufferSize"); v.Truthy() {
			lim.MaxBufferSize = int64(v.Float())
			req.Set("maxBufferSize", v)
		}
		if v := al.Get("maxStorageBufferBindingSize"); v.Truthy() {
			lim.MaxStorageBindingSize = int64(v.Float())
			req.Set("maxStorageBufferBindingSize", v)
		}
		desc.Set("requiredLimits", req)
	}
	v, err := awaitPromise(a.v.Call("requestDevice", desc))
	if err != nil {
		// A browser that refuses the raised limits still gives the defaults.
		lim = Limits{MaxBufferSize: MaxBufferSize, MaxStorageBindingSize: MaxStorageBindingSize}
		if v, err = awaitPromise(a.v.Call("requestDevice")); err != nil {
			return nil, err
		}
	}
	onErr := js.FuncOf(func(_ js.Value, args []js.Value) any {
		js.Global().Get("console").Call("error", "gpu: uncaptured error:", args[0].Get("error"))
		return nil
	})
	v.Call("addEventListener", "uncapturederror", onErr) // intentionally never released
	return &Device{v: v, limits: lim}, nil
}

// Device is a browser GPUDevice.
type Device struct {
	v      js.Value
	limits Limits
}

// Limits reports the buffer-size limits the device was created with.
func (d *Device) Limits() Limits { return d.limits }

// DualSourceBlending reports whether subpixel text's blend mode is
// available; WebGPU has no dual-source blending yet.
func (d *Device) DualSourceBlending() bool { return false }

func (d *Device) Release() {}

func (d *Device) Queue() *Queue { return &Queue{d.v.Get("queue")} }

// Flush is a no-op on web: the browser submits queued writes itself.
func (q *Queue) Flush() {}

func (d *Device) CreateShaderModuleWGSL(label, src string) (*ShaderModule, error) {
	m := d.v.Call("createShaderModule", map[string]any{"label": label, "code": src})
	return &ShaderModule{m}, nil
}

// ErrorCount is 0 on the web: the browser reports validation failures
// through promises rather than an uncaptured-error counter, and nothing
// here needs the recovery path the native backend offers.
func ErrorCount() uint64 { return 0 }

// CreateShaderModuleSPIRV always fails on the web: WebGPU takes WGSL only,
// and no browser accepts SPIR-V. A caller that can carry compiled shaders
// (a QML ShaderEffect's .qsb) has to fall back to whatever it draws without
// them.
func (d *Device) CreateShaderModuleSPIRV(label string, code []byte) (*ShaderModule, error) {
	return nil, fmt.Errorf("gpu: SPIR-V shader modules are native-only (WebGPU takes WGSL)")
}

// BufferUsage is a GPUBufferUsage flag set (same values as native).
type BufferUsage uint32

const (
	BufferMapRead BufferUsage = 0x01
	BufferCopySrc BufferUsage = 0x04
	BufferCopyDst BufferUsage = 0x08
	BufferIndex   BufferUsage = 0x10
	BufferVertex  BufferUsage = 0x20
	BufferUniform BufferUsage = 0x40
	BufferStorage BufferUsage = 0x80
)

// VertexFormat names a vertex attribute layout (browser spelling).
type VertexFormat string

const (
	// Uint16x4 is the skinning joint-index format (four bone indices per
	// vertex, read as vec4<u32> in WGSL).
	VertexUint16x4  VertexFormat = "uint16x4"
	VertexFloat32x2 VertexFormat = "float32x2"
	VertexFloat32x3 VertexFormat = "float32x3"
	VertexFloat32x4 VertexFormat = "float32x4"
)

// FormatRGBA8Unorm is the offscreen/readback-friendly texture format.
const FormatRGBA8Unorm TextureFormat = "rgba8unorm"

// FormatBGRA8Unorm is the format most native surfaces prefer.
const FormatBGRA8Unorm TextureFormat = "bgra8unorm"

// FormatR8Unorm is one byte a texel — see the native side.
const FormatR8Unorm TextureFormat = "r8unorm"

// FormatRGBA16Float: see the native backend.
const FormatRGBA16Float TextureFormat = "rgba16float"

// VertexAttr describes one vertex attribute within a buffer layout.
type VertexAttr struct {
	Format   VertexFormat
	Offset   int
	Location int
}

// VertexLayout describes one vertex buffer's stride and attributes.
type VertexLayout struct {
	Stride int
	Attrs  []VertexAttr
	// StepMode advances the buffer per vertex (the default) or per
	// instance, for a layout whose records describe a whole primitive
	// rather than a corner of one.
	StepMode VertexStepMode
}

// RenderPipelineDesc is the pipeline shape the 2D renderer needs: triangle
// list, optional vertex buffers, optional standard alpha blending, no
// depth, single color target.
type RenderPipelineDesc struct {
	Shader *ShaderModule
	// FragmentShader: see the native backend. WebGPU takes a module per
	// stage too, so this maps straight through.
	FragmentShader *ShaderModule
	VertexEntry    string
	FragmentEntry  string
	Format         TextureFormat
	VertexLayouts  []VertexLayout
	AlphaBlend     bool
	DualSrcBlend   bool       // dual-source (subpixel text); not supported on WebGPU/js yet
	Blend          *BlendDesc // an explicit blend equation; wins over AlphaBlend
	DepthTest      bool       // Depth32Float attachment, less-compare, write
	DepthNoWrite   bool       // with DepthTest: test against the depth buffer but leave it as it is (particles, overlays)
	CullBack       bool
	Samples        int // MSAA sample count (0/1 = none); must match the pass target
	// Lines draws the index list as line segments (pairs) instead of
	// triangles: 1px lines for grids, axes and edge overlays.
	Lines bool
	// NoColorTarget runs the fragment stage with zero colour attachments:
	// a depth-only pass (shadow map) whose fragment shader only discards
	// (cut-out leaves, glyphs). FragmentEntry must then return nothing.
	NoColorTarget bool
}

// BlendFactor is a GPUBlendFactor: what a colour-target blend multiplies
// the source or destination by.
type BlendFactor string

const (
	BlendZero             BlendFactor = "zero"
	BlendOne              BlendFactor = "one"
	BlendSrc              BlendFactor = "src"
	BlendOneMinusSrc      BlendFactor = "one-minus-src"
	BlendSrcAlpha         BlendFactor = "src-alpha"
	BlendOneMinusSrcAlpha BlendFactor = "one-minus-src-alpha"
	BlendDst              BlendFactor = "dst"
	BlendOneMinusDst      BlendFactor = "one-minus-dst"
	BlendDstAlpha         BlendFactor = "dst-alpha"
	BlendOneMinusDstAlpha BlendFactor = "one-minus-dst-alpha"
)

// BlendOp is a GPUBlendOperation: how the two weighted terms combine.
type BlendOp string

const (
	BlendAdd         BlendOp = "add"
	BlendSubtract    BlendOp = "subtract"
	BlendRevSubtract BlendOp = "reverse-subtract"
	BlendMin         BlendOp = "min"
	BlendMax         BlendOp = "max"
)

// BlendComponent is one channel group's blend equation.
type BlendComponent struct {
	Op       BlendOp
	Src, Dst BlendFactor
}

// BlendDesc is a colour target's full blend state. Pass it on a pipeline
// to get an equation other than the standard alpha blend — the separable
// blend modes (multiply, screen, darken, lighten) are exactly this.
type BlendDesc struct {
	Color, Alpha BlendComponent
}

// FormatDepth32Float is the depth texture format 3D passes use.
const FormatDepth32Float TextureFormat = "depth32float"

func (d *Device) CreateRenderPipeline(rp *RenderPipelineDesc) (*RenderPipeline, error) {
	target := map[string]any{"format": string(rp.Format)}
	if b := rp.Blend; b != nil {
		comp := func(c BlendComponent) map[string]any {
			return map[string]any{
				"operation": string(c.Op),
				"srcFactor": string(c.Src),
				"dstFactor": string(c.Dst),
			}
		}
		target["blend"] = map[string]any{"color": comp(b.Color), "alpha": comp(b.Alpha)}
	} else if rp.AlphaBlend {
		target["blend"] = map[string]any{
			"color": map[string]any{"operation": "add", "srcFactor": "src-alpha", "dstFactor": "one-minus-src-alpha"},
			"alpha": map[string]any{"operation": "add", "srcFactor": "one", "dstFactor": "one-minus-src-alpha"},
		}
	}
	vertex := map[string]any{
		"module":     rp.Shader.v,
		"entryPoint": rp.VertexEntry,
	}
	if len(rp.VertexLayouts) > 0 {
		buffers := make([]any, len(rp.VertexLayouts))
		for i, vl := range rp.VertexLayouts {
			attrs := make([]any, len(vl.Attrs))
			for j, a := range vl.Attrs {
				attrs[j] = map[string]any{
					"format":         string(a.Format),
					"offset":         a.Offset,
					"shaderLocation": a.Location,
				}
			}
			b := map[string]any{"arrayStride": vl.Stride, "attributes": attrs}
			if vl.StepMode == StepInstance {
				b["stepMode"] = "instance"
			}
			buffers[i] = b
		}
		vertex["buffers"] = buffers
	}
	primitive := map[string]any{"topology": "triangle-list"}
	if rp.Lines {
		primitive["topology"] = "line-list"
	}
	if rp.CullBack {
		primitive["cullMode"] = "back"
	}
	desc := map[string]any{
		"layout":    "auto",
		"vertex":    vertex,
		"primitive": primitive,
	}
	// Empty FragmentEntry = depth-only pipeline (shadow passes).
	if rp.FragmentEntry != "" {
		fragMod := rp.Shader
		if rp.FragmentShader != nil {
			fragMod = rp.FragmentShader
		}
		targets := []any{target}
		if rp.NoColorTarget {
			targets = []any{}
		}
		desc["fragment"] = map[string]any{
			"module":     fragMod.v,
			"entryPoint": rp.FragmentEntry,
			"targets":    targets,
		}
	}
	if rp.DepthTest {
		desc["depthStencil"] = map[string]any{
			"format":            string(FormatDepth32Float),
			"depthWriteEnabled": !rp.DepthNoWrite,
			"depthCompare":      "less",
		}
	}
	if rp.Samples > 1 {
		desc["multisample"] = map[string]any{"count": rp.Samples}
	}
	p := d.v.Call("createRenderPipeline", desc)
	return &RenderPipeline{p}, nil
}

// CreateMSAATexture allocates a multisampled render attachment for use with
// BeginRenderPassResolve. samples must match the pipelines drawn into it.
func (d *Device) CreateMSAATexture(width, height int, format TextureFormat, samples int) *Texture {
	if samples < 1 {
		samples = 1
	}
	v := d.v.Call("createTexture", map[string]any{
		"size":        []any{width, height},
		"format":      string(format),
		"usage":       0x10, // RENDER_ATTACHMENT
		"sampleCount": samples,
	})
	return &Texture{v: v, bpt: BytesPerTexel(format)}
}

// CreateDepthTexture allocates a Depth32Float render attachment.
func (d *Device) CreateDepthTexture(width, height int) *Texture {
	return d.CreateTexture2D(width, height, FormatDepth32Float, TextureRenderAttachment)
}

// CreateShadowMap allocates a depth texture that shaders can sample
// (shadow mapping).
func (d *Device) CreateShadowMap(size int) *Texture {
	return d.CreateTexture2D(size, size, FormatDepth32Float, TextureRenderAttachment|TextureBinding)
}

// CreateComparisonSampler returns a linear-filtered less-equal comparison
// sampler for shadow lookups (hardware 2x2 PCF).
func (d *Device) CreateComparisonSampler() *Sampler {
	v := d.v.Call("createSampler", map[string]any{
		"addressModeU": "clamp-to-edge",
		"addressModeV": "clamp-to-edge",
		"magFilter":    "linear",
		"minFilter":    "linear",
		"compare":      "less-equal",
	})
	return &Sampler{v}
}

// BeginShadowPass starts a depth-only pass writing into a shadow map
// (cleared to 1, stored for later sampling).
func (e *CommandEncoder) BeginShadowPass(depth *TextureView) *RenderPass {
	pass := e.v.Call("beginRenderPass", map[string]any{
		"colorAttachments": []any{},
		"depthStencilAttachment": map[string]any{
			"view":            depth.v,
			"depthLoadOp":     "clear",
			"depthStoreOp":    "store",
			"depthClearValue": 1,
		},
	})
	return &RenderPass{pass}
}

// SetIndexBuffer binds a uint32 index buffer.
func (r *RenderPass) SetIndexBuffer(b *Buffer) {
	r.v.Call("setIndexBuffer", b.v, "uint32")
}

// DrawIndexed draws using the bound index buffer.
func (r *RenderPass) DrawIndexed(indexCount, instanceCount int) {
	r.v.Call("drawIndexed", indexCount, instanceCount, 0, 0, 0)
}

// DrawIndexedInstanced draws with an explicit first instance, so shaders
// can index per-draw data with instance_index.
func (r *RenderPass) DrawIndexedInstanced(indexCount, instanceCount, firstInstance int) {
	r.v.Call("drawIndexed", indexCount, instanceCount, 0, 0, firstInstance)
}

// BeginRenderPassDepth starts a pass with a color attachment (cleared to
// clear) and a depth attachment (cleared to 1.0).
func (e *CommandEncoder) BeginRenderPassDepth(view, depth *TextureView, clear Color) *RenderPass {
	pass := e.v.Call("beginRenderPass", map[string]any{
		"colorAttachments": []any{map[string]any{
			"view":    view.v,
			"loadOp":  "clear",
			"storeOp": "store",
			"clearValue": map[string]any{
				"r": clear.R, "g": clear.G, "b": clear.B, "a": clear.A,
			},
		}},
		"depthStencilAttachment": map[string]any{
			"view":            depth.v,
			"depthLoadOp":     "clear",
			"depthStoreOp":    "discard",
			"depthClearValue": 1,
		},
	})
	return &RenderPass{pass}
}

// BeginRenderPassDepthStore is BeginRenderPassDepth keeping the depth
// buffer after the pass, for passes that read it (ambient occlusion,
// volumetric light): the depth texture needs TextureBinding.
func (e *CommandEncoder) BeginRenderPassDepthStore(view, depth *TextureView, clear Color) *RenderPass {
	pass := e.v.Call("beginRenderPass", map[string]any{
		"colorAttachments": []any{map[string]any{
			"view":    view.v,
			"loadOp":  "clear",
			"storeOp": "store",
			"clearValue": map[string]any{
				"r": clear.R, "g": clear.G, "b": clear.B, "a": clear.A,
			},
		}},
		"depthStencilAttachment": map[string]any{
			"view":            depth.v,
			"depthLoadOp":     "clear",
			"depthStoreOp":    "store",
			"depthClearValue": 1,
		},
	})
	return &RenderPass{pass}
}

// BeginRenderPassDepthKeep starts a pass that keeps whatever colour is
// already in the target and still clears the depth buffer — what a split
// screen needs, since each pane has its own camera and so its own depth,
// while the panes already drawn must survive.
func (e *CommandEncoder) BeginRenderPassDepthKeep(view, depth *TextureView) *RenderPass {
	pass := e.v.Call("beginRenderPass", map[string]any{
		"colorAttachments": []any{map[string]any{
			"view":    view.v,
			"loadOp":  "load",
			"storeOp": "store",
		}},
		"depthStencilAttachment": map[string]any{
			"view":            depth.v,
			"depthLoadOp":     "clear",
			"depthStoreOp":    "discard",
			"depthClearValue": 1,
		},
	})
	return &RenderPass{pass}
}

// Buffer is GPU-resident memory.
type Buffer struct {
	v    js.Value
	size int
}

func (b *Buffer) Release() { b.v.Call("destroy") }

// Size returns the buffer's capacity in bytes.
func (b *Buffer) Size() int { return b.size }

// CreateBuffer allocates a GPU buffer of size bytes.
func (d *Device) CreateBuffer(usage BufferUsage, size int) *Buffer {
	v := d.v.Call("createBuffer", map[string]any{"size": size, "usage": int(usage)})
	return &Buffer{v: v, size: size}
}

// CreateRenderTexture allocates a 2D texture usable as a render target and
// copy source.
func (d *Device) CreateRenderTexture(width, height int, format TextureFormat) *Texture {
	v := d.v.Call("createTexture", map[string]any{
		"size":   []any{width, height},
		"format": string(format),
		"usage":  0x10 | 0x01, // RENDER_ATTACHMENT | COPY_SRC
	})
	return &Texture{v: v, bpt: BytesPerTexel(format)}
}

func (d *Device) CreateCommandEncoder() *CommandEncoder {
	return &CommandEncoder{d.v.Call("createCommandEncoder")}
}

type ShaderModule struct{ v js.Value }

func (m *ShaderModule) Release() {}

type RenderPipeline struct{ v js.Value }

func (p *RenderPipeline) Release() {}

// Surface is a canvas's WebGPU context.
type Surface struct {
	gpu    js.Value
	canvas js.Value
	ctx    js.Value
}

func (s *Surface) Release() { s.ctx.Call("unconfigure") }

// SetTransparent is accepted for API parity; canvas compositing already
// honors alpha ("premultiplied" is the WebGPU canvas default we configure).
func (s *Surface) SetTransparent(_ *Adapter) {}

func (s *Surface) PreferredFormat(_ *Adapter) TextureFormat {
	return TextureFormat(s.gpu.Call("getPreferredCanvasFormat").String())
}

// Configure sizes the canvas backing store and configures the context.
func (s *Surface) Configure(d *Device, width, height int, format TextureFormat) {
	s.canvas.Set("width", width)
	s.canvas.Set("height", height)
	s.ctx.Call("configure", map[string]any{
		"device":    d.v,
		"format":    string(format),
		"alphaMode": "opaque",
	})
}

// CurrentTexture acquires this frame's texture. In the browser this cannot
// go stale the way a native swap chain can, so ok is always true.
func (s *Surface) CurrentTexture() (*Texture, bool) {
	return &Texture{v: s.ctx.Call("getCurrentTexture"), bpt: 4}, true
}

// Present is a no-op in the browser: returning to the event loop presents.
func (s *Surface) Present() {}

type Texture struct {
	v js.Value
	// bpt is the texture's bytes per texel — see the native side.
	bpt int
}

func (t *Texture) Release() {}

func (t *Texture) CreateView() *TextureView {
	return &TextureView{t.v.Call("createView")}
}

type TextureView struct{ v js.Value }

func (v *TextureView) Release() {}

// ComputePipeline is a compiled compute shader entry point.
type ComputePipeline struct{ v js.Value }

func (p *ComputePipeline) Release() {}

// BindGroupLayout returns the pipeline's auto-derived layout for a group.
func (p *ComputePipeline) BindGroupLayout(index int) *BindGroupLayout {
	return &BindGroupLayout{p.v.Call("getBindGroupLayout", index)}
}

// CreateComputePipeline compiles a compute pipeline from a WGSL module's
// entry point, with an auto-derived layout.
func (d *Device) CreateComputePipeline(m *ShaderModule, entry string) (*ComputePipeline, error) {
	p := d.v.Call("createComputePipeline", map[string]any{
		"layout":  "auto",
		"compute": map[string]any{"module": m.v, "entryPoint": entry},
	})
	return &ComputePipeline{p}, nil
}

// ComputePass records compute work.
type ComputePass struct{ v js.Value }

func (c *ComputePass) SetPipeline(p *ComputePipeline) { c.v.Call("setPipeline", p.v) }

func (c *ComputePass) SetBindGroup(index int, g *BindGroup) {
	c.v.Call("setBindGroup", index, g.v)
}

// Dispatch launches x*y*z workgroups.
func (c *ComputePass) Dispatch(x, y, z int) { c.v.Call("dispatchWorkgroups", x, y, z) }

// End closes the pass encoder.
func (c *ComputePass) End() { c.v.Call("end") }

type CommandEncoder struct{ v js.Value }

// BeginComputePass starts recording compute commands.
func (e *CommandEncoder) BeginComputePass() *ComputePass {
	return &ComputePass{e.v.Call("beginComputePass")}
}

// BeginRenderPass starts a single-target pass that clears view to clear and
// stores the result.
func (e *CommandEncoder) BeginRenderPass(view *TextureView, clear Color) *RenderPass {
	pass := e.v.Call("beginRenderPass", map[string]any{
		"colorAttachments": []any{map[string]any{
			"view":    view.v,
			"loadOp":  "clear",
			"storeOp": "store",
			"clearValue": map[string]any{
				"r": clear.R, "g": clear.G, "b": clear.B, "a": clear.A,
			},
		}},
	})
	return &RenderPass{pass}
}

// BeginRenderPassResolve starts a pass that renders into the multisampled
// msaaView and resolves the result into view (MSAA anti-aliasing).
func (e *CommandEncoder) BeginRenderPassResolve(msaaView, view *TextureView, clear Color) *RenderPass {
	pass := e.v.Call("beginRenderPass", map[string]any{
		"colorAttachments": []any{map[string]any{
			"view":          msaaView.v,
			"resolveTarget": view.v,
			"loadOp":        "clear",
			"storeOp":       "discard", // only the resolved output is kept
			"clearValue": map[string]any{
				"r": clear.R, "g": clear.G, "b": clear.B, "a": clear.A,
			},
		}},
	})
	return &RenderPass{pass}
}

// BeginRenderPassResolveLoad loads the multisampled attachment instead of
// clearing it (see the native version). The browser's MSAA target is only
// persistent when stored, so this pass stores it.
func (e *CommandEncoder) BeginRenderPassResolveLoad(msaaView, view *TextureView) *RenderPass {
	pass := e.v.Call("beginRenderPass", map[string]any{
		"colorAttachments": []any{map[string]any{
			"view":          msaaView.v,
			"resolveTarget": view.v,
			"loadOp":        "load",
			"storeOp":       "store",
		}},
	})
	return &RenderPass{pass}
}

// BeginRenderPassLoad starts a single-target pass that LOADS the existing
// contents of view instead of clearing — for compositing a second pass (e.g.
// UI) over an earlier one (e.g. a 3D scene) in the same frame.
func (e *CommandEncoder) BeginRenderPassLoad(view *TextureView) *RenderPass {
	pass := e.v.Call("beginRenderPass", map[string]any{
		"colorAttachments": []any{map[string]any{
			"view":    view.v,
			"loadOp":  "load",
			"storeOp": "store",
		}},
	})
	return &RenderPass{pass}
}

// Finish ends recording and returns the command buffer.
func (e *CommandEncoder) Finish() *CommandBuffer {
	return &CommandBuffer{e.v.Call("finish")}
}

type RenderPass struct{ v js.Value }

func (r *RenderPass) SetPipeline(p *RenderPipeline) { r.v.Call("setPipeline", p.v) }

// SetVertexBuffer binds b to a vertex buffer slot for subsequent draws.
func (r *RenderPass) SetVertexBuffer(slot int, b *Buffer) {
	r.v.Call("setVertexBuffer", slot, b.v)
}

// SetBindGroup binds g at a group index for subsequent draws.
func (r *RenderPass) SetBindGroup(index int, g *BindGroup) {
	r.v.Call("setBindGroup", index, g.v)
}

// SetScissor restricts subsequent draws to a pixel rectangle, which must
// lie within the render target.
func (r *RenderPass) SetScissor(x, y, w, h int) {
	r.v.Call("setScissorRect", x, y, w, h)
}

// SetViewport maps clip space onto a pixel rectangle of the render
// target, so one pass can draw several views into one image — the split
// screen. Pair it with SetScissor: the viewport transforms, and only the
// scissor actually clips.
func (r *RenderPass) SetViewport(x, y, w, h float32) {
	r.v.Call("setViewport", x, y, w, h, 0, 1)
}

func (r *RenderPass) Draw(vertexCount, instanceCount, firstVertex, firstInstance int) {
	r.v.Call("draw", vertexCount, instanceCount, firstVertex, firstInstance)
}

func (r *RenderPass) End() { r.v.Call("end") }

type CommandBuffer struct{ v js.Value }

type Queue struct{ v js.Value }

func (q *Queue) Release() {}

// Submit sends command buffers to the GPU.
func (q *Queue) Submit(bufs ...*CommandBuffer) {
	arr := make([]any, len(bufs))
	for i, b := range bufs {
		arr[i] = b.v
	}
	q.v.Call("submit", arr)
}

// WriteBuffer schedules a copy of data into b at offset.
func (q *Queue) WriteBuffer(b *Buffer, offset int, data []byte) {
	if len(data) == 0 {
		return
	}
	noteBufferUpload(len(data))
	u8 := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(u8, data)
	q.v.Call("writeBuffer", b.v, offset, u8)
}

// TextureUsage is a GPUTextureUsage flag set (same values as native).
type TextureUsage uint32

const (
	TextureCopySrc          TextureUsage = 0x01
	TextureCopyDst          TextureUsage = 0x02
	TextureBinding          TextureUsage = 0x04
	TextureRenderAttachment TextureUsage = 0x10
)

// CreateTexture2D allocates a 2D texture with explicit usage flags.
func (d *Device) CreateTexture2D(width, height int, format TextureFormat, usage TextureUsage) *Texture {
	v := d.v.Call("createTexture", map[string]any{
		"size":   []any{width, height},
		"format": string(format),
		"usage":  int(usage),
	})
	return &Texture{v: v, bpt: BytesPerTexel(format)}
}

// WriteTexture uploads tightly packed RGBA8 pixels (width*height*4 bytes)
// into t.
func (q *Queue) WriteTexture(t *Texture, width, height int, data []byte) {
	q.WriteTextureRows(t, width, 0, height, data)
}

// WriteTextureRows uploads a horizontal BAND of a texture; see the native
// implementation for why an atlas is uploaded a band at a time.
func (q *Queue) WriteTextureRows(t *Texture, width, y, height int, data []byte) {
	if len(data) == 0 || height <= 0 {
		return
	}
	noteTextureUpload(len(data))
	u8 := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(u8, data)
	q.v.Call("writeTexture",
		map[string]any{"texture": t.v, "origin": []any{0, y, 0}},
		u8,
		map[string]any{"bytesPerRow": width * max(1, t.bpt), "rowsPerImage": height},
		[]any{width, height})
}

// Sampler describes texture filtering and addressing.
type Sampler struct{ v js.Value }

func (s *Sampler) Release() {}

// CreateSampler returns a clamp-to-edge sampler, linear- or
// nearest-filtered.
func (d *Device) CreateSampler(linear bool) *Sampler {
	filter := "nearest"
	if linear {
		filter = "linear"
	}
	v := d.v.Call("createSampler", map[string]any{
		"addressModeU": "clamp-to-edge",
		"addressModeV": "clamp-to-edge",
		"magFilter":    filter,
		"minFilter":    filter,
	})
	return &Sampler{v}
}

// CreateSamplerRepeat is CreateSampler with wrapping (tiling) addressing —
// what world-space tiled UVs (and glTF's default wrap mode) want.
func (d *Device) CreateSamplerRepeat(linear bool) *Sampler {
	filter := "nearest"
	if linear {
		filter = "linear"
	}
	v := d.v.Call("createSampler", map[string]any{
		"addressModeU": "repeat",
		"addressModeV": "repeat",
		"addressModeW": "repeat",
		"magFilter":    filter,
		"minFilter":    filter,
	})
	return &Sampler{v}
}

// BindGroupLayout is a pipeline-derived layout ("auto" layout group).
type BindGroupLayout struct{ v js.Value }

func (l *BindGroupLayout) Release() {}

// BindGroupLayout returns the pipeline's automatically derived layout for a
// group index.
func (p *RenderPipeline) BindGroupLayout(index int) *BindGroupLayout {
	return &BindGroupLayout{p.v.Call("getBindGroupLayout", index)}
}

// BindGroupEntry binds one resource; set exactly one of Sampler,
// TextureView or Buffer.
type BindGroupEntry struct {
	Binding     int
	Sampler     *Sampler
	TextureView *TextureView
	Buffer      *Buffer
}

// BindGroup is a bound resource set.
type BindGroup struct{ v js.Value }

func (g *BindGroup) Release() {}

func (d *Device) CreateBindGroup(layout *BindGroupLayout, entries ...BindGroupEntry) *BindGroup {
	raw := make([]any, len(entries))
	for i, e := range entries {
		var resource any
		switch {
		case e.Sampler != nil:
			resource = e.Sampler.v
		case e.TextureView != nil:
			resource = e.TextureView.v
		case e.Buffer != nil:
			resource = map[string]any{"buffer": e.Buffer.v}
		}
		raw[i] = map[string]any{"binding": e.Binding, "resource": resource}
	}
	v := d.v.Call("createBindGroup", map[string]any{"layout": layout.v, "entries": raw})
	return &BindGroup{v}
}

// Tick is a no-op on web: the browser retires GPU work between frames.
func (d *Device) Tick() {}

// WaitIdle is a no-op on web (no synchronous device wait exists).
func (d *Device) WaitIdle() {}
