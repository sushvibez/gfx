// Package gpu exposes WebGPU as the framework's one and only graphics API.
// WebGPU already is a portable hardware abstraction, so no extra HAL is
// layered underneath it — higher-level concepts (render graph, materials)
// live in the render module instead.
//
// Backends (see /DESIGN.md §5):
//   - Native (Windows/Linux/Android): wgpu-native shared library loaded
//     through the ffi module; bindings hand-mirrored from webgpu.h.
//   - Web: the browser's own WebGPU via syscall/js. Each encoder call is
//     one JS call today; batching command recording to amortize the
//     boundary cost is planned. Texture/buffer readback (screenshots,
//     compute results) is native-only so far.
//
// WGSL is the single shader language. Pipelines use auto-derived bind group
// layouts (Pipeline.BindGroupLayout); the WGSL-reflection codegen from
// /DESIGN.md §5 is not built yet. The inverse direction exists: gfx gen
// (script/gen) emits WGSL structs from Go GPU-shared component types.
//
// # Lifetime
//
// Every object a Create or Request call returns has a Release, and this
// package does nothing on the caller's behalf: there is no registry, no
// finalizer and no leak check. The contract:
//
//   - Release an object exactly once, from the goroutine that uses the
//     device. Release is not repeatable and not nil-safe: on native a
//     second call drops a reference the wrapper no longer owns (wgpu
//     aborts or corrupts its own count), and the wrapper is not usable
//     after the first. Owners in the layers above (ecs.GPUColumn,
//     render.Graph, the renderers) nil their fields and make their own
//     Release repeatable; do the same when you hold one of these.
//   - Release an object only when nothing will use it again, and that
//     includes objects made from it: a bind group before the buffers,
//     views and samplers it binds, a view before its texture. Native wgpu
//     counts references (a bind group keeps its buffer alive, a submitted
//     command keeps what it draws with), so the other order happens to
//     work there; on the web Buffer.Release destroys the buffer at once
//     and a bind group left over it fails validation when used. Code that
//     must run on both follows the strict order.
//   - Everything made from a Device is released before the Device, the
//     Device before its Adapter, the Adapter before the Instance; a
//     Surface is released before the Device it was configured with. A
//     Queue is a view of its Device: Device.Queue may be called any
//     number of times, and a Queue's Release only gives back the native
//     reference that call took.
//   - Release does not wait for the GPU. A resource used by commands
//     already submitted may be released straight after Submit.
//
// On the web most Release methods are no-ops (the browser collects the
// object); Buffer.Release and Surface.Release are the ones that act.
// Calling them all, in the order above, is what keeps one program right
// on both backends.
package gpu
