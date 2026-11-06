# GPU, FFI, ECS and memory

This page covers the four packages under the renderers: `gpu` (the WebGPU
mirror), `ffi` (how native libraries are called without cgo), `ecs` (the
component store whose columns the GPU reads) and `mem` (the arena module,
which is still a stub). DESIGN.md §4–§7 are the design documents; the
*Status* notes there record where the implementation differs, and this
page describes what exists today.

## The gpu package

`gpu` is a thin, faithful Go mirror of WebGPU. Descriptors are structs,
failures are errors, and every resource has an explicit `Release`. There
is no layer underneath it (WebGPU already is the hardware abstraction) and
no convenience on top of it — materials, meshes and the render graph live
in `render`. WGSL is the only shader language.

Two backends share one API:

| Backend | Build | How |
|---------|-------|-----|
| Native (Windows, Linux, Android) | `!js` | wgpu-native loaded through `ffi`; bindings hand-mirrored from `webgpu.h`, pinned to the wgpu-native **v29.0.0.0** ABI (`scripts/fetch-wgpu.sh`) |
| Web | `js` | the browser's `navigator.gpu` through `syscall/js`; each encoder call is one JS call today |

What the web backend does not have: `Load`, `Probe`, `DefaultLibNames`,
`ReadBuffer` (readback is native-only; `ReadTexture` exists on js and
returns an error, so a screenshot path builds for the browser), dual-source blending (`Device.DualSourceBlending`
reports false) and MSAA textures. The design's WGSL-reflection codegen for
bind-group layouts is not built; pipelines use auto-derived layouts
(`Pipeline.BindGroupLayout(i)`).

### Loading the library

Native code first opens wgpu-native. `gpu.NewInstance` does it on demand;
`gpu.Load` is the explicit form. Both take the library paths to try, in
order, and fall back to `gpu.DefaultLibNames()` (`wgpu_native.dll`,
`libwgpu_native.so`, `libwgpu_native.dylib`). The first `Load` wins and
later calls return its result.

`gpu/plat` holds the per-platform glue applications use instead of calling
the surface constructors directly:

```go
inst, err := gpu.NewInstance(plat.LibPaths()...)   // $GFX_WGPU_LIB, beside the exe, build/wgpu/, system path
surface, err := plat.NewSurface(inst, win)          // app.Window -> gpu.Surface (X11, Win32, Android, canvas)
```

`plat.LibPaths()` prefers `GFX_WGPU_LIB`, then the library beside the
executable (or in `wgpu/` next to it), then `build/wgpu/` searched upward
from the working directory, then the system search path. On the web it
returns nil.

Environment knobs read by the native backend:

| Variable | Effect |
|----------|--------|
| `GFX_WGPU_LIB` | path of the wgpu-native library |
| `GFX_WGPU_LOG=error,warn,info,debug,trace` | route wgpu-native's log at that level to stderr |
| `GFX_WGPU_BACKEND=vulkan,dx12,gl,metal,all` | backend mask (Windows defaults to DX12, everything else to all) |
| `GFX_WGPU_PRESENT=immediate,mailbox,fifo` | present mode (default FIFO; `SetPresentOverride` wins over it) |
| `GFX_WGPU_POWER=low,high` | adapter power preference (default high-performance, so hybrid laptops do not land on the software adapter) |
| `GFX_WGPU_DX12_SWAPCHAIN=hwnd,visual` | DX12 swapchain kind (default: DirectComposition visual) |
| `GFX_UPLOAD_TRACE=1`, `GFX_UPLOAD_TIMING=1`, `GFX_CALLS=1` | upload and call-count diagnostics (see Statistics) |

Each variable takes one of the listed values.

### Probe

`gpu.Probe` is the smallest proof that the no-cgo path works on a machine:
it loads the library, acquires an adapter, device and queue with no window
and no surface, releases them and reports what it found.

```go
info, err := gpu.Probe(paths...)   // *gpu.ProbeInfo
fmt.Println(info)                  // "wgpu 29.0.0.0: <adapter> (discrete-gpu, vulkan, <vendor>, vendor=0x.... device=0x...., driver ...)"
```

`ProbeInfo` carries `WGPUVersion`, `Adapter`, `Vendor`, `Driver`, `Backend`,
`AdapterType`, `VendorID` and `DeviceID`. `examples/probe` is exactly this
call with the library-path search in front of it:

```sh
scripts/fetch-wgpu.sh
cd examples && go run ./probe
```

### Instance, adapter, device

```go
inst, err := gpu.NewInstance(plat.LibPaths()...)
surface, err := plat.NewSurface(inst, win)
adapter, err := inst.RequestAdapter(surface)   // nil surface for headless work
device, err := adapter.RequestDevice()
queue := device.Queue()
```

`RequestAdapter` takes the surface the adapter must be compatible with, or
nil. `RequestDevice` asks for the adapter's own `maxBufferSize` and
`maxStorageBufferBindingSize` (so a large asset can use the memory the GPU
has instead of WebGPU's 256 MB / 128 MB defaults), requests
`DualSourceBlending` where the adapter offers it, and routes uncaptured
device errors to stderr. `device.Limits()` reports the buffer limits the
device was created with; the package constants `gpu.MaxBufferSize` and
`gpu.MaxStorageBindingSize` are the WebGPU minimums a device falls back
to. `CreateBuffer` prints a stack trace when a request exceeds the limit,
because wgpu itself only logs a validation error and hands back an invalid
handle that aborts at the next draw.

`gpu.ErrorCount()` counts uncaptured device errors. Code that builds an
optional pipeline from shaders it did not write reads the count around the
calls and treats a change as failure, since wgpu still returns a non-nil
object after a validation error.

### Surfaces and frames

```go
format := surface.PreferredFormat(adapter)
w, h := win.Size()
surface.Configure(device, w, h, format)

win.EachFrame(func() bool {
    tex, ok := surface.CurrentTexture()
    if !ok {
        surface.Configure(device, w, h, format) // outdated swap chain
        return true
    }
    view := tex.CreateView()
    enc := device.CreateCommandEncoder()
    pass := enc.BeginRenderPass(view, gpu.Color{R: 0.05, G: 0.05, B: 0.08, A: 1})
    pass.SetPipeline(pipeline)
    pass.Draw(3, 1, 0, 0)
    pass.End()
    queue.Submit(enc.Finish())
    surface.Present()
    view.Release()
    tex.Release()
    return true
})
```

Present before releasing the swapchain texture: releasing first drops the
reference presentation needs. `Surface.SetTransparent(adapter)` (call
before `Configure`) picks a compositor alpha mode that honours the
framebuffer alpha where the surface supports one.

### Resources

Every resource is a handle with `Release`. `Queue.Submit` releases the
command buffers it submits. The catalogue:

| Resource | Create | Notes |
|----------|--------|-------|
| `ShaderModule` | `CreateShaderModuleWGSL(label, src)`, `CreateShaderModuleSPIRV(label, code)` | SPIR-V is for shaders that ship compiled; WGSL is the language |
| `Buffer` | `CreateBuffer(usage, size)` | usage flags `BufferMapRead`, `BufferCopySrc`, `BufferCopyDst`, `BufferIndex`, `BufferVertex`, `BufferUniform`, `BufferStorage`; `Size()` |
| `Texture` | `CreateTexture2D(w, h, format, usage)`, `CreateTexture2DMips(…, mips)`, `CreateTexture2DArray(w, h, layers, …)`, `CreateRenderTexture(w, h, format)`, `CreateMSAATexture(…, samples)`, `CreateDepthTexture(w, h)`, `CreateShadowMap(size)` | usage flags `TextureCopySrc`, `TextureCopyDst`, `TextureBinding`, `TextureRenderAttachment`; `CreateView()`, `CreateViewArray(format, layers)` |
| `Sampler` | `CreateSampler(linear)`, `CreateSamplerRepeat(linear)`, `CreateSamplerMip(repeat, maxAnisotropy)`, `CreateComparisonSampler()` | the comparison sampler is less-equal with hardware 2x2 PCF, for shadow maps |
| `RenderPipeline` | `CreateRenderPipeline(*RenderPipelineDesc)` | `BindGroupLayout(i)` returns the auto-derived layout |
| `ComputePipeline` | `CreateComputePipeline(module, entry)` | `BindGroupLayout(i)` |
| `BindGroup` | `CreateBindGroup(layout, entries...)` | each `BindGroupEntry{Binding, Sampler/TextureView/Buffer}` sets exactly one resource |

Texture formats are named constants: `FormatRGBA8Unorm` (offscreen and
readback), `FormatBGRA8Unorm` (what most native surfaces prefer; a readback
of it is blue-first), `FormatR8Unorm` (coverage masks), `FormatRGBA16Float`
(HDR targets, not readable with `ReadTexture`) and `FormatDepth32Float`.
`TextureFormat` is a `uint32` natively and a `string` on the web, so code
must use the constants, never literals. `BytesPerTexel(format)` gives the
upload row stride; `MipLevels(w, h)` the length of a full mip chain.

Uploads go through the queue: `WriteBuffer(buf, offset, data)`,
`WriteTexture(tex, w, h, data)`, `WriteTextureRows(tex, w, y, h, data)`,
`WriteTextureMip(tex, mip, w, h, data)`, `WriteTextureLayer` and
`WriteTextureRegion` for arrays. `WriteBuffer` splits writes into 1 MiB
chunks (wgpu-native serves those from its staging belt; larger writes get
a dedicated allocation and cost 25–200 ms) and the queue flushes itself
with an empty submit once 16 MiB have been staged since the last one.
`Queue.Flush()` does that on demand; `Device.Tick()` polls the device so
wgpu can reclaim staging memory between submits and `Device.WaitIdle()`
blocks until every submitted command buffer has finished.

### Render pipelines

`RenderPipelineDesc` is the pipeline shape the renderers need rather than
the whole WebGPU descriptor:

| Field | Meaning |
|-------|---------|
| `Shader`, `VertexEntry`, `FragmentEntry` | one WGSL module with both stages |
| `FragmentShader` | a separate module for the fragment stage (SPIR-V shipped per stage) |
| `Format` | the colour target format |
| `VertexLayouts` | `[]VertexLayout{Stride, Attrs, StepMode}`; `VertexAttr{Format, Offset, Location}`; formats `VertexFloat32x2/3/4`, `VertexUint16x4`; `StepVertex` (default) or `StepInstance` |
| `AlphaBlend` | standard source-over blending |
| `Blend` | an explicit `BlendDesc{Color, Alpha BlendComponent{Op, Src, Dst}}`, wins over `AlphaBlend` (multiply, screen, darken, lighten are just equations) |
| `DualSrcBlend` | dual-source blending for subpixel text; `CreateRenderPipeline` returns an error when the device lacks the feature so callers can fall back |
| `DepthTest` | Depth32Float attachment, less-compare, write |
| `CullBack`, `Samples`, `Lines`, `NoColorTarget` | back-face culling; MSAA sample count (must match the pass target); line-list topology; a depth-only pass whose fragment stage only discards |

Passes come from the command encoder: `BeginRenderPass(view, clear)`,
`BeginRenderPassLoad(view)` (keeps the contents — how the 2D UI draws over a
3D frame), `BeginRenderPassResolve`/`BeginRenderPassResolveLoad` (MSAA
into a resolve target), `BeginRenderPassDepth(view, depth, clear)`,
`BeginRenderPassDepthKeep(view, depth)` (clears depth, keeps colour — split
screens), `BeginShadowPass(depth)` (depth only, cleared to 1) and
`BeginComputePass()`. A `RenderPass` has `SetPipeline`, `SetBindGroup`,
`SetVertexBuffer(slot, buf)`, `SetIndexBuffer(buf)` (32-bit indices),
`Draw(vertexCount, instanceCount, firstVertex, firstInstance)`,
`DrawIndexed(indexCount, instanceCount)`,
`DrawIndexedInstanced(indexCount, instanceCount, firstInstance)`,
`SetScissor`, `SetViewport` and `End`.

`examples/triangle` is the whole stack in one file: window, instance,
surface, adapter, device, a WGSL module with a vertex array baked into the
shader, one pipeline, one draw per frame. It builds for native, web and
Android; only the entry point differs.

### Compute

```go
module, err := device.CreateShaderModuleWGSL("double", `
@group(0) @binding(0) var<storage, read_write> values : array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) id : vec3u) {
    if (id.x < arrayLength(&values)) { values[id.x] = values[id.x] * 2.0; }
}`)
pipeline, err := device.CreateComputePipeline(module, "main")
bind := device.CreateBindGroup(pipeline.BindGroupLayout(0), gpu.BindGroupEntry{Binding: 0, Buffer: buf})

enc := device.CreateCommandEncoder()
pass := enc.BeginComputePass()
pass.SetPipeline(pipeline)
pass.SetBindGroup(0, bind)
pass.Dispatch((n+63)/64, 1, 1)
pass.End()
queue.Submit(enc.Finish())

out, err := device.ReadBuffer(queue, buf, len(data)) // buf needs BufferCopySrc
```

That is `gpu/compute_test.go`, which runs headlessly against the fetched
library (`GFX_WGPU_LIB` or `build/wgpu/`; the test skips when neither is
present). Dispatch is capped at 65535 workgroups per dimension, so a large
one-dimensional problem is laid out as a 2D grid — `examples/particles`
does this with an `xstride` parameter.

### Headless rendering and readback

Rendering without a window is the same code with a render texture in place
of the surface, and is how CI and the examples' `-screenshot` flags work:

```go
adapter, _ := inst.RequestAdapter(nil)
device, _ := adapter.RequestDevice()
target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
view := target.CreateView()
// ... record and submit a frame into view ...
pix, err := device.ReadTexture(queue, target, W, H) // tightly packed RGBA, W*H*4 bytes
```

`ReadTexture` and `ReadBuffer` block until the GPU is done; they are for
tests, screenshots and tools, not per-frame use. `app.Headless` is the
matching display-less host for stepping frames and injecting input.

### Statistics

The package counts what a frame hands to the driver, two atomic adds per
call: `UploadStats()` returns buffer and texture upload calls and bytes,
`CallStats()` the draws, bind-group sets, pipeline sets, scissors,
vertex-buffer sets, encoders and passes since `ResetUploadStats()`.
`UploadNanos()` times the uploads when `GFX_UPLOAD_TIMING=1` is set,
`GFX_UPLOAD_TRACE=1` logs individual slow writes, and `GFX_CALLS=1` prints
per-submit averages once a second for frame loops that have no overlay of
their own.

## The ffi package

`ffi` is the cgo replacement: it loads shared libraries and resolves
symbols without a C toolchain, so cross-compilation stays `GOOS=… go
build`. Per platform:

| Platform | Mechanism |
|----------|-----------|
| Linux, Android, macOS, FreeBSD | `ebitengine/purego` `Dlopen`/`Dlsym` |
| Windows | `syscall.LoadLibrary`/`GetProcAddress` from the standard library |
| js/wasm | a stub whose every call returns an "unsupported" error; GPU and platform access go through `syscall/js` |

```go
lib, err := ffi.Open("libwgpu_native.so", "wgpu_native.dll") // first name that opens
addr, err := lib.Sym("wgpuGetVersion")                         // or lib.MustSym
var getVersion func() uint32
lib.Bind(&getVersion, "wgpuGetVersion")                        // reflection-based, any signature
cb := ffi.Callback(func(status, handle, a, b, _, _ uintptr) uintptr { return 0 })
```

`Lib.Bind` is `purego.RegisterFunc`: it accepts integer, float, pointer
and string signatures, panics on a missing symbol (a binding error is a
programming error), and goes through `reflect.MakeFunc` — roughly 700 ns
and several allocations per call. `Callback` wraps a Go function as a C
function pointer; callbacks are never freed, so create them once.

### The fast path: Call, CallStack, CallF

`ffi.Call` is a 20-line assembly trampoline per ABI, entered through
`runtime.cgocall` with a `go:noescape` prototype so the argument block
stays on the Go stack:

```go
func Call(fn, a1, a2, a3, a4, a5, a6 uintptr) uintptr
func CallStack(fn, a1, a2, a3, a4, a5, a6 uintptr, stack []uintptr) uintptr   // up to 8 more words
func CallF(fn, a1, a2, a3, a4, a5, a6 uintptr, floats []uint64) uintptr       // up to 8 floats in XMM0-7 / D0-7
```

`Call` takes up to six integer-sized arguments (the register set on every
64-bit ABI targeted) and returns one integer. `CallStack` adds the seventh
argument onwards — on the stack on amd64, X6/X7 then the stack on arm64 —
which is also how SysV passes a by-value struct larger than two words.
`CallF` puts floating-point arguments in the vector registers, given as
`ffi.F32(v)` / `ffi.F64(v)` words in the order the signature lists them.

| Platform | `Call`/`CallStack` | `CallF` |
|----------|--------------------|---------|
| linux, android, darwin, freebsd on amd64 / arm64 | assembly trampoline, 0 allocs | yes |
| windows | `syscall.SyscallN` (already allocation-free) | no; floats stay on `Bind` (`SetViewport` is the one case in `gpu`) |
| other | `purego.SyscallN`, one allocation per call | no |

Measured (DESIGN.md §6.1): 39 ns and 0 allocations for libc `labs` through
`Call` against 620–900 ns and 4–13 allocations through `RegisterFunc`; a
real wgpu call costs about 54 ns, of which roughly 40 ns is the Go runtime's
own stack switch and scheduler hand-off. `ffi/call_test.go` has
`BenchmarkCall` with the three variants side by side and `gpu/ffi_bench_test.go`
times a real wgpu pair.

Pointers passed through `Call` must stay alive and unmoved for the call:
convert `unsafe.Pointer(&x)` to `uintptr` in the call expression itself
(the `uintptrescapes` rule) or hold the value with `runtime.KeepAlive`.

```go
strlen, _ := lib.Sym("strlen")
s := []byte("hello\x00")
n := ffi.Call(strlen, uintptr(unsafe.Pointer(&s[0])), 0, 0, 0, 0, 0) // 5
```

Struct-by-value arguments differ per architecture: amd64 copies the struct
onto the stack as `CallStack` words, arm64 passes a pointer to it in the
next integer register, Win64 passes any struct larger than 8 bytes by
pointer. `gpu` handles the three wgpu request functions that take a
40-byte callback-info struct per arch (`gpu/wgpu_abi_*.go`).

### ffigen

Hand-written code never spells `ffi.Call` out. `ffi/cmd/ffigen` reads a
package, finds every `lib.Bind(&name, "symbol")` whose variable is declared
as a func type, and for each signature it can express — up to six
integer-sized arguments, at most one integer-sized, pointer or bool
result — emits a Go function of the same name over `ffi.Call`. The
variable and the `Bind` line are removed from the hand-written files and a
generated `bindGenerated(lib)` resolves the addresses with `MustSym`.
Signatures it cannot express (floats, by-value structs, more than six
arguments, multiple results, a name declared in more than one build-tagged
file) are reported and left on `Bind`.

`gpu/wgpu_native.go` carries the directive:

```go
//go:generate go run ../ffi/cmd/ffigen -dir .
```

The workflow for a new wgpu symbol is: declare the func variable, bind it
with `lib.Bind` as usual, run `go generate` in `gpu`, and the binding moves
into `gpu/zffi_gen.go`. A rerun merges: stubs from the previous run are
recovered from the generated file, so the hand-written files never declare
them again. Flags: `-dir`, `-out` (default `zffi_gen.go`), `-tag` (build
constraint of the generated file, default `!js`) and `-dry`.

## The ecs package

`ecs` is the component store whose arrays the GPU can read. DESIGN.md §4
describes archetype tables; what is built is a **sparse set** — one
`Component[T]` pool per component type, a dense contiguous slice plus an
entity-to-row index, swap-remove on delete — which already gives the GPU
contract its property (each component is one dense slice bindable as a
storage buffer). Archetype grouping is a locality optimisation for
multi-component iteration that can replace the internals later without
changing this API. Systems and the parallel scheduler from the design do
not exist yet.

```go
type Entity struct{ ID, Gen uint32 }   // a handle, never a pointer; the zero Entity is invalid

world := ecs.NewWorld()
e := world.Spawn()
world.Alive(e)     // generation-checked
world.Despawn(e)   // removes e from every component; stale handles become invalid
```

`Component[T]` is created against a world and holds values per entity:

| Method | Meaning |
|--------|---------|
| `NewComponent[T](w)` | register the pool |
| `Add(e, v)` | set e's value (creates or overwrites) |
| `Get(e) *T` | pointer into the column, valid until the next Add/Remove (rows move) |
| `Has(e)`, `Remove(e)`, `Len()` | |
| `Column() []T` | the dense column: the fast path for systems and what the GPU binds |
| `EntityAt(i)` | which entity owns row i |
| `Each(fn func(Entity, *T))` | column-order iteration |
| `ecs.Each2(ca, cb, fn)` | entities holding both components, walking the smaller column |
| `MarkDirty()`, `MarkDirtyRow(i)` | flag rows mutated through `Column`/`Get` for re-upload |

Dirty tracking is one merged row range per component; `Add` and
swap-removes mark it automatically.

### GPU-shared columns

`GPUColumn[T]` mirrors a component column into a storage buffer. `T` must
be POD with a WGSL-compatible layout — the codegen below enforces it.

```go
col := ecs.NewGPUColumn(device, parts, 0)  // usage = Storage|CopyDst|extra (pass gpu.BufferCopySrc to read back)
col.Sync(queue)                            // uploads the dirty range; grows (and re-uploads fully) when the column outgrew the buffer
col.Buffer()                               // *gpu.Buffer, valid after the first Sync
col.Stride()                               // sizeof(T)
col.Rows()                                 // rows the GPU currently holds
col.LastUpload()                           // (offset, bytes) of the last Sync, for tests and profiling
col.Release()
```

`Sync` reports whether an upload happened and uploads through
`queue.WriteBuffer`; the design's persistently mapped staging memory and
double-buffered readback are not built.

### gfx gen: WGSL structs from Go types

A GPU-shared component embeds the zero-sized marker `ecs.GPUShared`, and
`script/cmd/gfx` generates its WGSL declaration:

```go
//go:generate go run github.com/sushvibez/gfx/script/cmd/gfx gen

type Particle struct {
    ecs.GPUShared
    Pos core.Vec2
    Vel core.Vec2
}
```

`go generate` writes `gfx_gen.go` next to it:

```go
const ParticleWGSL = `struct Particle {
    pos : vec2f,
    vel : vec2f,
}`

const (
    _ = uint(unsafe.Offsetof(Particle{}.Pos) - 0)
    _ = uint(0 - unsafe.Offsetof(Particle{}.Pos))
    _ = uint(unsafe.Offsetof(Particle{}.Vel) - 8)
    _ = uint(8 - unsafe.Offsetof(Particle{}.Vel))
    _ = uint(unsafe.Sizeof(Particle{}) - 16)
    _ = uint(16 - unsafe.Sizeof(Particle{}))
)
```

The constants go negative — a compile error — if the Go layout ever drifts
from the WGSL layout, and the shader source is built by concatenation
(`ParticleWGSL + "..."`), so there is one declaration. The generator
itself computes both layouts and refuses a struct whose Go packing and WGSL
alignment disagree, with the fix in the message (insert N bytes of
explicit padding, e.g. `_ [2]float32`, before the field, or trailing
padding when the stride rounds up).

| Go field type | WGSL | size | WGSL align |
|---------------|------|------|------------|
| `float32`, `int32`, `uint32` | `f32`, `i32`, `u32` | 4 | 4 |
| `core.Vec2` | `vec2f` | 8 | 8 |
| `core.Vec4` | `vec4f` | 16 | 16 |
| `core.Mat4` | `mat4x4f` | 64 | 16 |

`core.Vec3` is rejected outright (WGSL aligns `vec3` to 16 bytes, Go packs
it to 12 — use a `Vec4` or a `Vec2` plus a `float32`), as are arrays,
pointers, nested structs and embedded fields other than the marker. Field
names are lower-camel-cased for WGSL. All marked types in one package land
in one `gfx_gen.go`; marked types spread across packages in one directory
are an error. Tests for the generator live in `script/gen`.

### One million particles

`examples/particles` is the ECS and compute path end to end: particles are
seeded through the ECS, the column uploads once, a compute shader
integrates them on the GPU every frame, and a render pipeline draws
instanced quads from the same storage buffer. The CPU touches no particle
data after the first `Sync`.

```go
world := ecs.NewWorld()
parts := ecs.NewComponent[Particle](world)
for range n {
    e := world.Spawn()
    parts.Add(e, Particle{Pos: core.V2(rng.Float32()*float32(w), rng.Float32()*float32(h)),
        Vel: core.V2((rng.Float32()-0.5)*60, (rng.Float32()-0.5)*60)})
}
col := ecs.NewGPUColumn(device, parts, 0)
defer col.Release()
col.Sync(queue)

cm, _ := device.CreateShaderModuleWGSL("particles-step", computeWGSL) // ParticleWGSL + the kernel
step, _ := device.CreateComputePipeline(cm, "step")
stepBind := device.CreateBindGroup(step.BindGroupLayout(0),
    gpu.BindGroupEntry{Binding: 0, Buffer: col.Buffer()},
    gpu.BindGroupEntry{Binding: 1, Buffer: pbuf})

// per frame
enc := device.CreateCommandEncoder()
cp := enc.BeginComputePass()
cp.SetPipeline(step)
cp.SetBindGroup(0, stepBind)
cp.Dispatch(gx, gy, 1)        // 2D grid: a dimension maxes out at 65535 workgroups
cp.End()
rp := enc.BeginRenderPass(view, gpu.Color{R: 0.02, G: 0.02, B: 0.04, A: 1})
rp.SetPipeline(draw)
rp.SetBindGroup(0, drawBind)
rp.Draw(6, n, 0, 0)           // six vertices per quad, one instance per particle
rp.End()
queue.Submit(enc.Finish())
```

The WGSL side declares `var<storage, read_write> parts : array<Particle>`
with the generated struct and reads `parts[instance_index]` in the vertex
shader. Run it with `go run ./particles` (`-n 2000000 -frames 600` for a
timed run); the mouse attracts, holding the button repels.

## The mem package

`mem` is a documentation-only stub. DESIGN.md §7 specifies chunked arenas
with `Frame`/`Scene`/`App` lifetimes, `mem.AllocSlice[T]`, typed pools and
generation-checked `mem.Handle[T]`; none of it is implemented, and the
0-allocs-per-op CI contract is not set up. What is real is the discipline
the design rests on: POD-only columns in `ecs`, `{index, generation}`
handles instead of pointers (`ecs.Entity`, `physics/p3.BodyID`), value
types in APIs, and explicit `Release` for GPU resources.
