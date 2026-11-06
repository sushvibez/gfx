# Graphics Framework — Design

A Go framework for 2D user interfaces (QML as the reference) and 3D (Godot as the
reference), rendered through WebGPU, targeting **Windows, Linux, Web and Android**
with no C toolchain required to build.

Status (2026-07-03): living design document, no longer a draft — phases 0–3 of
the roadmap (§12) are implemented and verified by `scripts/check.sh` (gofmt,
build+vet for linux/windows/js, the layering test, all tests, headless
screenshot smoke),
and the QML interlude (`script/gml`) is delivered well beyond its original
scope. Sections below carry *Status* notes where the implementation deviates
from or hasn't reached the design. The notable gaps: the `mem` module is still
a doc-only stub (§7), `PropertyRef` isn't built (§3), ECS storage is sparse-set
rather than archetypes and has no system scheduler (§4), scene has no typed
nodes/lifecycle/Tasks yet (§4.1, §6.3), the null-GPU headless variant is
missing (§8.1), Wayland and multi-window-beyond-X11 are missing (§8), and XGo
hasn't started (§6.2, Phase 4).

---

## 1. Vision & goals

- One framework, two profiles: an app profile (declarative 2D UI, low idle cost)
  and a game profile (frame loop, ECS, 3D). Both share the same core, gpu and
  platform layers, so a game can embed real UI and an app can embed a 3D viewport.
- `go build` is the whole build. No cgo, no NDK toolchain wrangling beyond
  packaging, no shader toolchain at runtime.
- WebGPU is the only graphics API we target. It is already a portable HAL
  (D3D12/Vulkan/Metal underneath natively, native WebGPU in browsers), so we do
  not build another abstraction beneath it.
- Predictable performance: the GC must be a non-issue in the frame loop by
  construction (see §7), not by tuning after the fact.
- **Web is a first-class citizen.** Like QML/Qt-for-WebAssembly and Godot web
  exports: every phase ships and is demoed as a wasm build alongside native —
  web parity is part of each phase's exit criterion, never a port done later.

Wishlist (not targeted yet, but kept cheap by the architecture): macOS/iOS —
wgpu-native's Metal backend already covers rendering there, so only the `app`
layer (window/input via purego + objc_msgSend, as Ebitengine proves) is extra
work.

Non-goals (for now): AAA-class rendering, a visual editor before the runtime
is solid.

## 2. Architecture & module map

Layered, strictly downward dependencies:

```
                ┌──────────── examples ────────────┐
   script (XGo / DSL / codegen)                    │
   ui (widgets, layout)      scene (node tree)     │
        └──────────┬──────────────┘                │
                render (render graph, 2D/3D)       │
        ecs ───────┤                               │
                  gpu (WebGPU)                     │
        app (window, input, lifecycle)             │
                  ffi (purego/syscall)             │
        core (math, properties, signals, loop)     │
        mem (arenas, pools, handles)               │
```

| Module   | Responsibility                                            |
|----------|-----------------------------------------------------------|
| `core`   | math, Property/Signal system, event loop                  |
| `mem`    | arenas, pools, generation handles                         |
| `ffi`    | shared-library loading without cgo                        |
| `gpu`    | WebGPU: wgpu-native (native) / browser (web)              |
| `app`    | windowing, input, lifecycle per platform                  |
| `ecs`    | archetype ECS with GPU-sharable component pools           |
| `scene`  | typed node tree (authoring layer) backed by ecs           |
| `render` | render graph, 2D batcher, 3D renderer                     |
| `ui`     | QML-like widget/layout/styling toolkit                    |
| `script` | XGo integration, codegen tool, future DSL                 |

One Go module (`github.com/sushvibez/gfx`) with one package directory per
layer; `examples/` is a second module so the framework carries no demo-only
dependencies. Dependency direction is enforced by `internal/layering`, a
test that fails on any import of an equal or higher layer. *(Until
2026-10-31 each layer was its own module in a `go.work` workspace and the
module graph was the fence.)*

## 3. Properties & signals — QML's machinery in Go

Qt implements properties/signals with moc, a meta-object compiler, because C++
has no reflection. Go doesn't need a moc: **generics + a tiny amount of runtime
bookkeeping** give us typed properties, typed signals, and automatic dependency
tracking (the same trick as QML bindings, Solid.js or MobX).

```go
// core
type Property[T comparable] struct { ... }

func (p *Property[T]) Get() T      // records a dependency if a binding is evaluating
func (p *Property[T]) Set(v T)     // no-op if equal; else marks dependents dirty + emits Changed
func (p *Property[T]) Changed() *Signal[T]

// width: height * 2   — re-evaluated automatically when Height changes
core.Bind(&rect.Width, func() float32 { return rect.Height.Get() * 2 })

type Signal[T any] struct { ... }
func (s *Signal[T]) Connect(fn func(T)) Connection   // slot = any func/method value
func (s *Signal[T]) Emit(v T)
```

Decisions:

- **Automatic dependency tracking.** While a binding closure runs, every
  `Property.Get` it touches registers itself as a dependency. No codegen, no
  reflection, no string names. Dependencies are re-captured on every evaluation,
  so conditional bindings (`if a.Get() { return b.Get() } ...`) track correctly.
- **Lazy, batched re-evaluation.** `Set` only marks dependents dirty; bindings
  re-evaluate once per frame in topological order before layout/paint. This
  avoids the diamond-glitch problem and redundant recomputation that eager
  propagation has.
- **Single-owner threading model**, like Qt: all properties/UI objects belong to
  the goroutine running the event loop, so `Get`/`Set` take no locks. Other
  goroutines communicate via `loop.Post(func)` — the equivalent of Qt's queued
  connections — and `Signal.EmitAsync` sugar on top of it.
- **No virtual dispatch tax**: `Property[T]` is a plain struct embedded in
  widgets; memory layout stays flat and pool-friendly.

Cost to watch: each live binding is a closure plus a dependency list. Fine for
UI-scale object counts (thousands); the ECS (§4), not the property system, is
the tool for entity-scale data (millions).

Two storage tiers share one property API:

- **Owning properties** (`Property[T]`): the value lives inside the struct.
  Used by `ui` widgets and anything not GPU-shared.
- **Column-backed properties** (`PropertyRef[T]`): the value lives in an ECS
  component column (§4); the node exposes a view with the same
  `Get`/`Set`/`Bind`/`Changed` surface. `Set` writes into the column row and
  marks the GPU dirty range. Bindings and signals work identically, so QML-style
  binding works on 3D scene nodes too — but bulk data stays contiguous and
  GPU-sharable.

*Status (2026-07-03):* the owning tier is implemented in `core` (35 tests):
automatic dependency tracking with per-evaluation recapture, lazy once-per-frame
flush (pull-based dependency order rather than a literal topological sort, which
still evaluates diamonds exactly once), binding-loop detection with QML-style
keep-previous-value, `Set`-breaks-binding semantics, and a process-wide
`Revision()` change counter that lets consumers skip idle frames. `Bind` is a
method on the property rather than the free function sketched above, and
`EmitAsync` sugar doesn't exist — use `Loop.Post`. `core` also grew a
tween/spring animation engine (`core/anim.go`) that §10's Behavior support and
the gml animation types build on. **`PropertyRef` is not built yet** — scene
nodes don't expose property views so far.

## 4. ECS — arrays of structs the GPU can read

Goal stated up front: component storage that can be bound directly as a WebGPU
storage buffer and processed by compute shaders using stride/offset, with the
CPU and GPU agreeing on layout.

- **Archetype storage.** Entities with the same component set live in the same
  table; each component is a contiguous typed slice (column). Iteration is
  linear memory. Entity = `{index uint32, generation uint32}` handle — never a
  pointer (GC-friendly, GPU-friendly, trivially serializable).
- **AoS vs SoA, per component.** A component struct stored in a column is
  AoS *within itself* (stride = sizeof(struct), field ops use offset+stride —
  exactly the access pattern you described). Hot fields that compute shaders
  hammer independently can be split into their own single-field components,
  which is SoA. The framework doesn't force either; splitting a component is a
  data-modeling decision.
- **GPU-shared pools.** A component type can be declared GPU-shared:
  - its Go struct must be POD (no pointers, no slices) and match WGSL
    std430-compatible layout — a codegen tool (§6) emits the WGSL struct and
    Go-side `unsafe.Sizeof/Offsetof` static assertions so drift is a compile
    error (the classic trap: `vec3` aligns to 16 bytes in WGSL);
  - the pool allocates from persistently-mapped staging memory with dirty-range
    tracking, so a frame uploads only the spans that changed;
  - readback (GPU→CPU) is explicit and double-buffered to avoid stalls.
- **Systems** are plain functions over typed queries. Each system declares its
  read/write component sets; the scheduler runs non-conflicting systems in
  parallel across worker goroutines, and falls back to serial on js/wasm
  (no threads there, §8).

Reference points: Bevy (archetypes, system scheduling), Flecs (queries),
our addition is the GPU-shared pool contract.

*Status (2026-07-03):* the GPU-shared pool contract is real and test-proven
(91.7% coverage): `Entity{ID, Gen}` handles with generation validation,
contiguous POD columns, `GPUColumn[T]` syncing dirty data to a storage buffer,
and `gfx gen` emitting WGSL structs + layout assertions (§6.2). Deviations:
storage is **sparse-set** (one pool per component type, dense slice +
swap-remove, `Each`/`Each2` queries), not archetype tables — the internals can
be swapped later without changing the API; dirty tracking merges to a single
span per frame and uploads via `queue.WriteBuffer` (no persistently-mapped
staging, no double-buffered readback); **systems and the parallel scheduler
don't exist yet**.

### 4.1 The node tree on top — decided

**Decision: the node tree is the public API; ECS columns are its storage.**
Users never deal with entities/archetypes directly — those are the
implementation. The two requirements this must satisfy, and how:

1. *Get/set on an individual object.* A node value is a thin handle
   `{id, generation}`. `node.Position().Set(v)` resolves the handle through the
   indirection table to `(table, row)` and writes `Transform` column\[row\],
   marking the row's span dirty. O(1), no tree walk, no allocation. The handle
   stays valid when rows move (archetype change, compaction) because only the
   indirection table updates — same trick as Godot's RID and our `mem.Handle`.
2. *Pass the struct array to GPU for render and compute passes.* A column is
   already `[]Transform` — one contiguous POD slice per table. Render/compute
   passes bind it as a storage buffer directly; per-frame work is just
   uploading dirty ranges. Instanced draws read instance data straight from
   the column; a compute pass (culling, particles, skinning) reads/writes the
   same buffer with stride/offset.

What the tree itself stores is only *structure*: parent/children, name, order
— in its own columns (parent index, depth). Parent→child transform propagation
is a per-frame topological pass over dirty subtrees (Godot 4 does the same),
not a recursive tree walk. Group-by-archetype means siblings of the same type
are adjacent in memory regardless of where they sit in the tree — tree shape
costs nothing at iteration/render time.

*Status (2026-07-03):* the minimal version that Phase 3 needed exists
(`scene.Tree`, 92.7% coverage): `Node = ecs.Entity` handles, Transform +
parent/depth columns, per-frame world-matrix propagation. The propagation pass
rescans the full column per depth level rather than walking dirty subtrees, and
typed node kinds, names/child order, lifecycle callbacks, and signals are still
missing (Phase 4 scope along with the §6.3 Task API).

## 5. GPU layer — WebGPU and nothing else

- **Native**: [wgpu-native] (the C API of Rust wgpu, the same implementation
  Firefox ships) as a prebuilt shared library per platform, loaded via `ffi`
  (§6). Bindings are generated from `webgpu.h`. Windows→D3D12/Vulkan,
  Linux→Vulkan, Android→Vulkan.
- **Web**: the browser's WebGPU through `syscall/js`. Every JS call has boundary
  cost, so the Go-side API is designed for *recording then submitting*: command
  encoding batches into as few JS crossings as possible.
- **WGSL is the only shader language.** One source of truth; a build-time
  reflection step parses WGSL and generates Go bind-group-layout code, so
  pipeline layout mismatches are compile errors, not validation errors at
  runtime.
- The `gpu` package stays a faithful, thin, Go-idiomatic mirror of WebGPU
  (descriptors as structs, errors as errors, resources with explicit `Release`).
  Convenience belongs in `render`, not here.

*Status (2026-07-03):* both backends are real — native (currently pinned to the
wgpu-native **v29.x** ABI, upgraded from the original v0.19.4.1 pin) covers
render + compute pipelines, depth/shadow, MSAA resolve, and offscreen
texture/buffer readback; the browser backend mirrors the API surface except
readback, `DualSrcBlend`, and MSAA textures. Two design points are not yet
reality: WGSL-reflection codegen for bind group layouts (pipelines use
auto-derived layouts instead; `gfx gen` covers the inverse Go→WGSL direction),
and batched command recording on web (each encoder call is one JS call today).

## 6. FFI (the cgo replacement) and scripting

### 6.1 Replacing cgo

cgo breaks the promise of trivial cross-compilation, so:

| Platform | Mechanism |
|----------|-----------|
| Linux (and later macOS) | **ebitengine/purego** — `dlopen`/`dlsym` + assembly trampolines, no C compiler involved |
| Windows | stdlib `syscall.LoadLibrary` — Go never needed cgo to call DLLs |
| Android | purego, but **with cgo enabled** — purego's bionic `dlopen` path requires CGO (verified: `GOOS=android CGO_ENABLED=0` fails to build purego v0.10). Accepted: Android builds need the NDK toolchain for APK packaging anyway, so this costs nothing extra in practice. Upstreaming a no-cgo bionic path to purego is a wishlist item. |
| Web | not applicable — no native libraries exist; `syscall/js` is the bridge |

`ffi` wraps these behind one `Lib`/`Proc` API. Known constraints we accept:
purego callbacks (C→Go) are limited and slightly costly — WebGPU's API shape
(descriptor structs, few callbacks, and wgpu-native callbacks are mostly
one-shot async completions) fits this fine. Passing large structs *by value*
through purego is the painful case; `webgpu.h` passes descriptors by pointer.

Considered and rejected: wazero (compile C to wasm, run sandboxed inside Go —
the ncruces/go-sqlite3 trick). Fully portable, but a sandboxed wasm module
cannot talk to the GPU driver. Wrong tool for this job.

Distribution: prebuilt `wgpu-native` libs are embedded per-GOOS/GOARCH via
`go:embed` and extracted to the app cache dir on first run (with a
"system-provided path" override for packagers).

*Status (2026-07-03):* the `ffi` module is implemented as designed and tested
against real libc (`Lib.Open/Sym/Bind`, package-level `Callback`; purego on
unix/android, `syscall` on Windows, compile-only stub on js). The `go:embed`
distribution story is not built — development uses `scripts/fetch-wgpu.sh`
into `build/wgpu/` with a `GFX_WGPU_LIB` override, and the Android APK packer
bundles the library itself.

*Status (2026-10-10):* **the hot path no longer goes through reflection.**
`purego.RegisterFunc` builds every binding on `reflect.MakeFunc`, which
measured 620–900 ns and 4–13 allocations per call (purego's own benchmark,
and ~700 ns for a real `wgpuDeviceGetQueue`+`Release` pair). `ffi.Call` is
a 20-line assembly trampoline per ABI (SysV amd64, AAPCS64 arm64; Windows
uses the stdlib's `syscall.SyscallN`, other arches fall back to
`purego.SyscallN`) entered through `runtime.cgocall` with a `go:noescape`
prototype, so the argument block stays on the Go stack: **39 ns, 0 allocs**
on libc `labs`, 54 ns per real wgpu call. `ffi/cmd/ffigen` generates typed
stubs over it from the package's `lib.Bind` declarations (`gpu/zffi_gen.go`).
`CallStack` (the seventh integer argument onwards: stack on amd64, X6/X7
then stack on arm64) and `CallF` (floats in the vector registers) cover
the rest, so on linux/android nothing in `gpu` goes through `RegisterFunc`
any more; Windows keeps `SetViewport` there. A by-value struct differs per
architecture — amd64 copies it onto the stack, arm64 passes a pointer —
which `gpu` handles per arch. Verified on an Adreno 740 phone and the
x86_64 emulator (2026-10-15).
This is the same shape as Rust's FFI — a static stub per symbol with the
calling convention fixed at compile time — minus what Go's runtime must
still do on every crossing (stack switch to g0, scheduler hand-off), which
is the remaining ~40 ns and is not removable without cgo-free runtime
changes.

### 6.2 Scripting — XGo

XGo (formerly Go+/goplus) is the candidate because it **compiles to Go** —
scripts become normal compiled code in release builds (zero interpreter tax),
and the project also has interpreter tooling for development.

- **Operator overloading**: XGo already supports user-defined operators
  (`func (a Vec2) + (b Vec2) Vec2`), which covers the C++-style math ergonomics
  you want for vectors/matrices without forking anything. Plain-Go consumers
  still get the method forms (`Add`, `Mul`).
- **Annotations — standard Go machinery only, nothing invented.** The codegen
  tool is driven by stock `go generate`, and the markers are the constructs Go
  already has: **embedding** marks what a type *is*, **struct tags** carry
  field metadata:

  ```go
  //go:generate gfx gen

  type Velocity struct {
      ecs.GPUShared              // embedding marks the type: GPU-sharable component
      X, Y, Z float32
  }

  type Spaceship struct {
      scene.Node3D               // embedding marks the type: a scene node
      Thrust core.PropertyRef[float32] `gfx:"animatable,min=0"`
  }
  ```

  `go generate ./...` runs `gfx gen`, which walks the AST (embedding + tags are
  visible without executing anything), and emits registration code, WGSL
  structs with layout assertions (§4), and editor/scene-format metadata. This
  is exactly the ecosystem-standard pattern (stringer, ent, mockgen) — no new
  comment dialect to teach tooling about. Forking XGo for `@Annotation` syntax
  is off the table unless tags+embedding provably fall short.

  *Status: v0 shipped (2026-06-12)* — `script/cmd/gfx` emits WGSL structs plus
  compile-time offset/stride assertions from `ecs.GPUShared` embeds, fails
  generation on Go↔WGSL misalignment with concrete padding fixes, and rejects
  vec3 with the aligns-to-16 explanation. Registration/meta codegen arrives
  with the scene format.
- **Hot reload** (dev only): run script-level code under an interpreter
  (XGo's interpreter or yaegi for plain Go) while engine modules stay compiled;
  release builds AOT-compile everything. Go plugins are not an option
  (Linux-only, fragile).
- **QML replacement**: phase 1 is a fluent Go/XGo builder API for `ui`. The
  declarative document format (a `.gml` that maps 1:1 onto that API, the way
  QML maps onto Qt properties) comes after the widget set exists, so the DSL
  serializes a proven API instead of driving its design.

*Status (2026-07-03):* XGo integration and hot reload of Go-level code have not
started (Phase 4). The scripting need is currently served by `script/gml`
instead: the `.gml` engine shipped with its own JS-subset interpreter and
dev-time hot reload, far beyond the "serialize the builder API" scope — see the
Interlude entry in §12. `gfx gen` v0 status is described above; registration/
meta codegen still arrives with the scene format.

### 6.3 Blueprint, code-first

We copy Blueprint's *programming model*, not its visual editor — you write
code with the same shape a Blueprint graph has:

- **Event-driven lifecycle**: nodes implement `Ready`, `Process(dt)`,
  `PhysicsProcess(dt)`, input handlers; everything else reacts to signals
  (collision, timer, animation-finished) connected in code.
- **Latent actions** — the thing that makes Blueprints pleasant (`Delay`,
  `Move To`, `Play And Wait`) — map naturally onto goroutines synchronized to
  the frame loop. A node script can run a coroutine that *reads* like a
  Blueprint execution wire:

  ```go
  s.Go(func(t *scene.Task) {        // frame-synced coroutine, auto-cancelled on node exit
      t.Wait(2 * time.Second)       // Delay
      t.Tween(&s.Glow, 1.0, 300*time.Millisecond) // Timeline
      s.Explode.Emit(s.Position().Get())
  })
  ```

  `Task` methods yield to the main loop (no real blocking, works on
  single-threaded wasm) and are cancelled when the node leaves the tree —
  Blueprint's latent-action semantics without a graph editor.
- **Exposed properties** via struct tags (§6.2) give the same
  "EditAnywhere/BlueprintReadWrite" metadata Unreal gets from UPROPERTY —
  consumed later by the scene format and any future editor/inspector.
- Because the model is plain code + metadata, a visual front-end could be
  *generated from and serialized to* this code later — the door stays open
  without us building a graph editor now.

*Status (2026-07-03):* not started (Phase 4). `core/anim.go` provides the
tween/spring primitives a `Task.Tween` would sit on, but the frame-synced
coroutine API, lifecycle callbacks, and node signals all wait on scene's typed
nodes (§4.1).

## 7. Memory — keeping the GC out of the loop

Honest assessment of the ideas on the table:

| Idea | Verdict |
|------|---------|
| Compile-time escape analysis | Go already does it, but it's conservative and **not contractual**. We make it contractual with CI: hot-path benchmarks assert `0 allocs/op`, and `-gcflags=-m` output is diffed on PRs so a new escape is a visible regression. |
| Context/scope-based alloc (per frame / per request) | **Yes — build it.** §7.1. This is the centerpiece. |
| Reference counting | Only where destruction must be deterministic: GPU resources (buffers, textures, pipelines). Explicit `Release()`, refcount inside the resource registry, leak detection in debug builds. General-purpose RC fights the GC and Go has no destructors to hang it on. |
| Modifying Go itself (custom GC, real arenas) | **Feasible but defer.** The runtime is Go + asm and the official arena experiment (`GOEXPERIMENT=arenas`) still exists in-tree, frozen — a fork could resurrect it. But a fork is a forever tax on every Go release, and js/wasm + Android make a patched toolchain painful to ship. Decision: build the same capability in userland (§7.1); only revisit forking if profiling proves a wall we cannot cross. TinyGo (pluggable GC) is the cheaper escape hatch for web binary size/GC issues. |

### 7.1 Userland arenas + handles

```go
arena := mem.NewArena(mem.Frame)        // lifetimes: Frame, Scene, App
verts := mem.AllocSlice[Vertex](arena, n) // carved from large []byte chunks, no per-object GC cost
...
arena.Reset()                            // end of frame/request: everything freed at once, O(1)
```

The rule that makes this safe without runtime support: **arena memory is
POD-only** — no Go pointers stored inside (the GC cannot see pointers written
through unsafe into byte-backed memory, so we simply forbid them). Cross-references
use `mem.Handle[T]` (index + generation, validated on deref). The ECS needs
exactly the same discipline, so the constraint costs nothing extra. This is also
your "free all memory allocated in one web request or game loop" model: a server
framework on top can hand each request a `mem.Arena` in its `Context`.

Plus the boring-but-decisive measures: pools for recycled objects, value types
in APIs (no `*Vec3`), pre-sized reused slices, `GOMEMLIMIT` + high `GOGC`, and
optionally triggering GC during the vsync slack when a frame finishes early.

*Status (2026-07-03):* **the `mem` module is still a doc-only stub** — no
Arena, no `Handle[T]`, no pools exist yet, and the 0-allocs-in-hot-paths CI
contract isn't set up. The ECS's POD-column discipline (§4) is the one piece
of this section that's real. Value-type APIs are followed throughout.

## 8. Platforms & WebAssembly

| Platform | Window/input | GPU | Notes |
|----------|--------------|-----|-------|
| Windows  | Win32 via `syscall` | wgpu-native (D3D12) | *implemented; verified on Windows 11 (D3D12), headless and windowed, 2026-10* |
| Linux    | X11 via purego today; native Wayland follows later | wgpu-native (Vulkan) | dev platform; the only backend with multi-window (`NewChild`) so far |
| Android  | `NativeActivity` + a small cgo glue (`app/android_glue.c`) | wgpu-native (Vulkan) | **min API 29**; *WORKING (2026-06-13)* — `scripts/build-apk.sh` packages a signed APK with no Gradle/Java, verified on the API-36 x86_64 emulator (the triangle renders through Vulkan). wgpu-native is cross-compiled from source for Android by `scripts/build-wgpu-android.sh` (upstream ships no Android prebuilt). |
| Web      | canvas + DOM events via `syscall/js` | browser WebGPU | **first-class target** — see below |
| Headless | virtual window + synthetic input | offscreen wgpu, or null GPU | no display server needed — see §8.1 |
| macOS/iOS (wishlist) | purego + objc_msgSend | wgpu-native (Metal) | rendering is already covered by WebGPU; only `app` glue is new work |

WASM specifics — web is first-class, so these are design constraints, not
porting notes:
- The deliverable is the same as QML-for-WebAssembly / Godot web export: any
  app built on the framework produces a runnable web build with
  `gfx pack -target web` (wasm + loader JS + canvas host page). Every phase's
  demo ships as a web build (§12).
- Target `GOOS=js GOARCH=wasm` first. WASI has no GPU/window story yet.
  TinyGo is **an option we keep open, not a requirement**: we avoid
  reflection and finalizers anyway (codegen philosophy), which is most of
  what TinyGo compatibility demands — so if binary size or GC pauses become
  a problem on web, switching the web build to TinyGo stays a realistic move.
- **No threads** on js/wasm — goroutines multiplex on one thread. The ECS
  scheduler and renderer must degrade to single-threaded cleanly (a scheduling
  policy, not an `#ifdef`).
- **Binary size**: plain Go wasm starts ~2–5 MB compressed. Mitigations in
  order: avoid `reflect`-heavy deps (also required for codegen-not-reflection
  philosophy), `-ldflags=-s -w`, wasm-opt, and keep the code TinyGo-compatible
  where cheap so TinyGo (with its smaller binaries and pluggable GC) stays an
  option.
- JS boundary cost shapes the `gpu` API (§5): record native-side, cross the
  boundary in batches.

### 8.1 Headless mode (Godot-style)

Headless is a real `app` backend selected at startup (`gfx run --headless` /
`app.Run(app.Headless())`), not a build variant — every app built on the
framework gets it for free. Two levels:

- **Headless + offscreen GPU**: no window or display server; wgpu renders to
  an offscreen texture (wgpu needs no surface for that). Frames can be read
  back and saved (`--screenshot out.png`, or programmatically), so CI and AI
  agents can *see* the app pixel-for-pixel as it would render on screen.
  Works on a bare Linux server with a GPU, or via lavapipe/SwiftShader
  software Vulkan where there is none.
- **Headless + null GPU**: the gpu module's null backend accepts all calls and
  renders nothing. Logic, layout, scene tree, ECS and signals all still run —
  for game servers, fast unit/integration tests, and logic-only automation.

The same backend carries **synthetic input**: an `app.Inject(events...)` API
feeds the identical event stream real platforms produce, so a test or an AI
agent can click/type/scroll a headless app and screenshot the result. Combined
with the deterministic frame loop (`app.Step()` advances exactly one frame),
this makes "drive the app, assert on pixels/state" a first-class workflow —
which is also exactly what the framework's own integration tests will use.

*Status (2026-07-03):* the offscreen-GPU level is implemented and test-proven:
`app.Headless` is a real Host with `Step`, `InjectMouse`/`InjectKey` +
`Click`/`Type` conveniences (instead of one `Inject(events...)` signature),
and offscreen render + `ReadTexture` readback; `ui/headless_test.go` drives a
real app headlessly and asserts on pixels, and `scripts/check.sh` screenshots
two example apps as its smoke test. Deviations: screenshots are per-example
`-screenshot` flags rather than a framework-level `gfx run --headless` CLI,
readback is native-only (no js), and **the null-GPU level doesn't exist yet**
— headless still requires the wgpu-native library.

## 9. Rendering

- **Render graph** (Unreal RDG / Godot 4 inspired): passes declare resource
  reads/writes; the graph orders passes, allocates and aliases transient
  attachments, and culls dead passes. WebGPU does most synchronization for us,
  so this is primarily about *structure* — making "insert a bloom pass" a
  10-line change.
- **2D**: retained draw lists from the `ui`/`scene` layer; batcher sorts by
  pipeline / atlas page / clip rect into instanced quads. Text starts as a
  glyph atlas (HarfBuzz-quality shaping deferred; start with simple shaping),
  with SDF or compute rasterization (Vello-style) as a later upgrade. Vector
  paths via CPU tessellation first.
- **3D**: clustered forward+ with PBR materials, mesh instancing from day one
  (it falls out of ECS storage), then shadow maps, then a post stack (tonemap,
  bloom, TAA much later).
- **Scene format**: text-based like `.tscn`/QML, loading into the `scene` node
  tree; binary-packed variant for release. UUID-stable resource references with
  an import pipeline (source asset → imported artifact cache), Godot-style.

*Status (2026-07-03):* render graph v0 exists (pass read/write declarations,
dead-pass culling, transient-texture pooling; RGBA8 transients and render
passes only so far). The 2D batcher is past its phase-1 scope: instanced SDF
quads, Gaussian shadows, clip stack, **linear/radial gradients** (originally
deferred), glyph-atlas text with LCD subpixel rendering, CPU path tessellation,
and SVG. 3D matches the Phase 3 exit: Cook-Torrance PBR, one directional light,
2048px PCF shadow map, glTF (GLB + .gltf, base-color/metallic-roughness).
*(2026-07-29)* Skinned animation landed: `render.AnimationPlayer` samples
clips into the retained node hierarchy, a CPU skinner re-poses vertices
(`examples/skinned`). Binary FBX loads too (`render/fbx`: pure-Go parser →
`gltf.Document`, Mixamo-style rigs; `fbx.Retarget` rebases clips shipped in
separate files onto the character's skeleton — `examples/actor` plays the
Mixamo soldier). FBX materials extract as a Phong→PBR subset: per-polygon
material splitting into primitives, PNG/JPEG diffuse textures (embedded
Content, else a gltf.Parse-style resolve callback for external files),
shininess→roughness; the PBR shader alpha-cutouts transparent texels (hair
cards). ASCII FBX remains unsupported. The 3D sampler wraps (repeat) —
glTF's default — and Scene3D solids take `texture:`/`uvScale:` (gml
`scenetex.go`: world-metric tiled UVs via per-extent cube meshes, lazy GPU
upload, soft color fallback while assets are unfetched) — the fps level's
walls/floor/stairs are textured this way.
Per-draw data lives in an instance-indexed storage buffer
but each item still draws with instance count 1 — true instanced batching,
clustered forward+, culling, and the scene format remain future work.
*(2026-09-15)* **Skinning moved to the GPU** (`render/gpuskin.go`): the
bind-pose vertex buffer is uploaded once, per-vertex joints (uint16x4) and
weights (float32x4) live in a second vertex slot, and the only per-frame
traffic is the joint palette (`array<mat4x4f>` in its own bind group, 64 B
per joint, written when the pose actually moves). Both shader families
(legacy `Draw*` and the material pass) and the shadow pass have skinned
pipeline variants, built lazily and cached. Measured on 18 Mixamo soldiers
re-posed every frame: 40.3 ms and 46.5 MB/frame of `WriteBuffer` traffic on
the CPU path against 1.40 ms and 69 KB/frame on the GPU path. The CPU
skinner stays as the fallback (`render.GPUSkinning = false`, or
`GFX_CPU_SKIN=1`) and both paths render the same pixels. With real alpha now
reachable, `Model.DrawFaded` routes through the material pipeline's blended
variant instead of the CPU triangle dissolve (which remains the fallback).
*(2026-08-29)* **Offscreen 2D layers** (`render.Layer2D`): part of a UI
renders into its own texture and the frame samples the result — QML's
`layer.enabled`, and the prerequisite for any effect that reads pixels back.
The layer pass submits its own command buffer, because the renderer refills
one set of vertex buffers per flush; separating the submissions is what lets
the same renderer (and its font atlases) draw both. On top of it,
`ui.Layer(state, child).Blur(px)` is a real gaussian: the layer renders
downscaled, is blurred once in its own texture and once while compositing,
from a 13-tap kernel whose taps are drawn over each other at their share of
the weight so far (so the result is their weighted average). That is what
frosted glass is made of, and what `layer.effect: MultiEffect { blurEnabled }`
maps to in gml.
*(2026-08-31)* **Fill rules**: a path that overlaps itself now fills by
even-odd or non-zero (`render.FillRule`, SVG's fill-rule, QML's
ShapePath.fillRule, canvas `fill("evenodd")`). A simple outline still takes
the exact ear clipper; one that nests or crosses itself takes the scanline,
whose slabs are split at every Y where two edges cross — without those
boundaries the crossings swap order within a slab and neither rule comes
out right. **Dashed strokes** land the same way — on the geometry, not in a
shader: `render.DashPath` cuts a path into its on-runs and those are stroked
normally, so caps, joins and widths keep working and the dashes are measured
along the outline (they stay put when the shape animates). That covers
canvas `setLineDash`, `ShapePath.strokeStyle`/`dashPattern` (pen-width
units) and SVG `stroke-dasharray`.
*(2026-09-14)* **3D material and light model.** `Renderer3D` grew a second
pipeline family beside the original PBR one (`render/material.go`,
`lights.go`, `matshader.go`); the legacy `Draw*` entry points are untouched
and still take the old shader, so existing scenes render exactly as before,
while `DrawMaterial(mesh, model, *Material)` and
`DrawInstanced(mesh, []Mat4, *Material)` take the new one. A `Material`
carries the texture set a Poly Haven / glTF asset actually ships — base
colour, tangent-space normal map, packed ARM (AO/roughness/metalness) or a
standalone grayscale roughness with a channel selector, and emission — plus
scalar factors, `Unlit`, `DoubleSided` and `Alpha` (a translucent material
takes a blended pipeline variant and is drawn after the opaques, sorted back
to front). Two projection modes ride along: **triplanar** world-space
projection blended by |n|^4, which needs no UVs and does not stretch on
cliffs, and **stochastic hex tiling** (Heitz & Neyret / Mikkelsen) —
three hex-lattice samples with a per-cell random offset and rotation,
combined by sharpened barycentric weights through a variance-preserving
blend against the texture mean, which is read free from the top of the mip
chain. That is the cure for the repeating-grid look of tiled level geometry;
`AntiTileRotation` scales the rotation, because an oriented texture (a brick
bond) scrambles under the full random turn that rock and sand want.
Textures upload through `NewImage3DMipmapped` (CPU box-filtered chain, so
native and wasm behave identically) and sample trilinearly at 8x anisotropy
(`gpu.CreateSamplerMip`, `CreateTexture2DMips`, `WriteTextureMip`).
`Vertex3D` gained a `Tan vec4` and `NewMesh` derives a MikkTSpace-lite
tangent frame from the UV parametrisation when a mesh source leaves it zero
(the shader falls back to a screen-space cotangent frame when even that is
degenerate). Lighting: the directional light and its shadow map stay, joined
by up to 16 shadowless point and spot lights per frame
(`SetPointLights`/`SetSpotLights`) in one storage block, with Godot-style
windowed range attenuation and a smoothstep cone. `SetFrameSettings` carries
the frame-wide look — ambient, exposure, tone map (none / filmic / ACES),
exponential height fog with aerial perspective toward a sky gradient — all
applied in the material fragment stage. Every material sample uses explicit
derivatives (`textureSampleGrad`/`SampleLevel`), because hex tiling and
triplanar run in non-uniform control flow where implicit LOD is invalid.
`examples/materials` is the diptych: the same brick wall and stone floor
tiled plainly on the left and hex-tiled on the right, under three coloured
point lights and fog, with an instanced row of triplanar blocks. Still
open: the pass is LDR (tone mapping happens at the end of the fragment
shader, not on an HDR target), local lights cast no shadows, there is no
IBL/reflection probe, no clustered light culling, and the legacy `Draw*`
path sees none of the new environment settings.

## 10. What else to steal from Qt / Godot / Unreal

| Source | Idea | Position |
|--------|------|----------|
| Godot | Node tree as the *authoring* model, even though storage is ECS | **Adopted (decided)** — see §4.1. Nodes are handles; data lives in GPU-sharable columns; users get per-object get/set, the GPU gets contiguous struct arrays. |
| Godot | Servers architecture (render thread owns its world; game thread sends commands) | Adopt at render module boundary: scene/ui produce command lists, render consumes — also exactly what wasm's single thread and WebGPU's recording model want. |
| Godot | Editor built with the framework itself | Adopt as a north star; it forces the UI toolkit to be real. Not before phase 4. |
| Godot | Import pipeline + `.import` cache, text scene files | Adopt (§9). |
| Godot | Headless mode | **Adopt early (decided)** — §8.1: offscreen-GPU and null-GPU backends + synthetic input + single-frame stepping. CI and AI agents drive and screenshot apps without a display. |
| Unreal | UHT codegen for reflection/metadata | Adopt — it's our `gfx gen` (§6.2). |
| Unreal | Render graph (RDG) | Adopt (§9). |
| Unreal | Hot reload of gameplay code | Adopt in dev via interpreter (§6.2). |
| Unreal | Blueprints | **Adopt the programming model, code-first (decided)** — see §6.3: event lifecycle, frame-synced latent actions, exposed-property metadata. The visual editor itself stays deferred; the code model keeps that door open. |
| Qt | Queued connections / single UI thread | Adopt (§3). |
| GPUI (Zed) | Hybrid immediate/retained: state lives in retained entities, the UI is a lightweight element tree rebuilt per frame by a render function | **Adopted (decided 2026-06-12)** for the ui module: state in Property-holding view objects, `Render() ui.Element` builds the tree each frame. Pairs with §3 — property reads during render can drive damage tracking later. |
| GPUI (Zed) | Tailwind-style fluent element API (`div().flex().gap_2().bg(...)`) | Adopt: `ui.Div().Row().Gap(8).Bg(...)` — Go method chaining maps 1:1. |
| GPUI (Zed) | Everything is a GPU quad: rounded corners, borders, shadows via SDF in one shader | Adopt for the 2D batcher's next iteration (the white-texture quad pipeline grows SDF params). |
| Qt | QML file hot-reload during development | **Done** — `gml.Watch` re-parses on save; build errors render as a banner instead of crashing. |
| Qt/QML | Anchors + declarative animation (`Behavior on x`) | **Done** — full anchor layout in `ui` (fill/centerIn/edge links, fixpoint iteration) and in gml markup; `Behavior`, `NumberAnimation` & friends, states/transitions over `core` tweens/springs. |

## 11. Main risks

1. **wgpu-native API churn** — the C header has historically moved; pin a
   release, wrap it once in `gpu`, regenerate bindings per upgrade.
2. **purego callback/struct-by-value edges** — prototype the wgpu adapter
   request + device callbacks *first* (phase 0 exit criterion).
   *Status: PASSED on Linux (2026-06-11)* — `gpu.Probe` loads wgpu-native
   v0.19.4.1 through purego, C→Go request callbacks fire, adapter/device/queue
   acquired headlessly with CGO_ENABLED=0.
3. **Android without cgo is the least-trodden path** — NativeActivity glue via
   purego should work (dlopen is there) but budget real time; gomobile fallback.
4. **Two profiles, one team** — UI toolkit and 3D renderer are each big. The
   layering keeps them independent so they can progress unevenly.
5. **XGo dependency health** — if it stalls, plain Go + codegen remains fully
   viable; XGo is additive, not load-bearing.

## 12. Roadmap

Web is first-class: **every phase's exit demo must also run in the browser**,
not only phase 0's.

- **Phase 0 — proof of skeleton** *(DONE 2026-06-12; Windows runtime check
  pending a real machine)*: core math + properties/signals + event loop;
  ffi; window on Linux & Windows; wgpu triangle on both; wasm triangle in
  browser. *Exit: same example runs on 3 platforms from one `go build`.*
- **Phase 1 — 2D** *(DONE 2026-06-12)*: render graph v0, 2D batcher (GPUI
  SDF quads + Gaussian shadows), text, ui widgets + layout + input + scroll
  + clip, the property system driving real UI; app.Headless host with
  Step/Inject (§8.1). *Exit met: examples/settings runs native + web and is
  driven + screenshotted headlessly (scripts/check.sh).* Deferred into later
  phases: gradients/per-corner radii (with theming), animation framework
  (with the QML slice), multi-flush renderer sub-allocation. *(Gradients and
  the animation framework have since landed — §9, §10.)*
- **Interlude — QML** *(DONE 2026-06-12, expanded through 2026-06-24+)*: the
  `.gml` declarative document format mapping onto the ui builders, with
  property bindings in markup and dev-time hot reload. *Delivered far beyond
  scope*: ~60 QML element types including Controls (buttons, inputs, ComboBox,
  SpinBox, Dial, ...), ListModel/Repeater/ListView/GridView, Loader + inline
  Component, a Canvas with an HTML5-style 2d context, States/Transitions,
  the QML animation types, Timer, Keys/focus, a floating overlay layer
  (Popup/Dialog/Menu/Drawer/ToolTip), StackView/SwipeView/TabBar navigation,
  and a JS-subset interpreter (arrow functions, template literals, `?.`/`??`,
  spread, `for...of`, object literals, user functions/closures). Proven by
  recreating 29 of the 30 feasible Zed GPUI examples in pure `.gml`
  (script/gml/check/QML-EXAMPLES.md). Known gaps: runtime ListModel mutation, `when`-based
  states, `textFormat: RichText`, Grid `rows`, two-axis Flickable.
- **Phase 2 — node tree + compute** *(DONE 2026-06-12, with deviations)*:
  node handles (§4.1), `gfx gen` v0 (`go generate`-driven WGSL layout
  codegen), GPU-shared pools. *Exit met: examples/particles integrates 1M
  particles in a compute shader from an ECS-seeded GPU column, native + web
  build.* Deviations: sparse-set columns instead of archetype tables, and no
  system scheduler yet (§4 status).
- **Phase 3 — 3D + Android** *(DONE 2026-06-13)*: forward PBR (Cook-Torrance)
  with shadow mapping, glTF loading, scene-tree-on-ECS, orbit camera, Android
  APK packaging. *Exit met: the cubes scene (PBR + shadows + hierarchy)
  renders on the API-36 Android emulator via Vulkan, the Duck renders on
  desktop, and the 3D path builds for web (on-screen web verify pending a
  non-headless browser).* Deferred: clustered forward+, GI, skinning, a
  native `.tscn`-style scene format.
- **Phase 4 — scripting & tooling** *(next)*: XGo integration, Blueprint-style
  task API (§6.3), hot reload of Go-level code, editor seeds. (The ui DSL
  originally slotted here shipped early as the QML interlude.) Also owed from
  earlier phases: the `mem` module (§7), systems/scheduler and archetype
  internals (§4), typed scene nodes (§4.1), the null-GPU headless backend
  (§8.1), and Wayland (§8).
- **Wishlist**: macOS/iOS `app` glue (rendering already works via Metal
  backend), TinyGo web builds, visual front-end over the §6.3 code model.

## 13. Decisions & open questions

Decided (2026-06-10):

1. **Authoring model**: node tree as the public API over GPU-sharable ECS
   columns (§4.1) — per-object get/set *and* struct arrays for render/compute.
2. **Annotations/codegen**: standard `go generate` + embedding markers +
   struct tags (§6.2); no invented comment dialect, no XGo fork.
3. **Blueprint**: adopt the programming model code-first (§6.3); no visual
   editor for now.
4. **Android**: minimum API 29; raise later if support improves.
5. **Web**: first-class citizen — wasm export for any app, demoed every phase;
   TinyGo kept as an option, not a requirement.
6. **macOS/iOS**: on the wishlist; WebGPU's Metal backend means only `app`
   glue is missing.
7. **Headless mode** (Godot-style): a standard `app` backend from phase 1 —
   offscreen/null GPU, synthetic input, frame stepping, screenshots — so CI
   and AI agents can drive any app without a display (§8.1).
8. **Import path**: `github.com/sushvibez/gfx/<layer>`, one module
   (2026-10-31; the per-layer modules under the original organisation path
   were collapsed for the open-source release so a single `go get` works).
9. **Open source from day one**, licensed Apache-2.0 (permissive with an
   explicit patent grant).
