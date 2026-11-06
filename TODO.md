# TODO — the engine

The framework's own list: `core`, `mem`, `ffi`, `gpu`, `app`, `ecs`,
`scene`, `render`, `ui`, `physics`, `audio`, `md`, `script`. Kept
current by hand — when something here changes state, this file changes
in the same commit.

**One or two lines an item.** Detail lives where the work is; this page
says what is open, what it is waiting on, and where to look. Dates are
when the item was last touched.

Layers big enough to have their own story keep their own list:

| list | what it covers |
| --- | --- |
| [`script/TODO.md`](script/TODO.md) | the .gml engine: the QML parity review (all 65 pages listed) and frame cost |
| [`md/TODO.md`](md/TODO.md) | Markdown: parser done, what the renderer and ui element still lack |
| [`physics/PLAN.md`](physics/PLAN.md) | P0–P2 implemented (2026-08-14); P3+ planned |
| [`markdown/PLAN.md`](markdown/PLAN.md) | the Markdown editor: Obsidian-parity plan, chrome only so far |
| [`docs/notes/sdf-ideas.md`](docs/notes/sdf-ideas.md) | signed distance fields: the MSDF glyph atlas is done, the rest is an idea list |

## Now — the open-source release (2026-10-31)

### Continuous integration
No `.github/workflows` yet. Wanted: one workflow running
`scripts/check.sh` on ubuntu (gofmt, build+vet for linux/windows/js, the
layering test, all tests) plus the headless screenshot smoke, which needs
wgpu-native (`scripts/fetch-wgpu.sh`) and a software Vulkan adapter
(Mesa lavapipe) on the runner. A Windows job for `go test ./...` would
have caught the two Windows-only failures fixed at release time.

### Licence audit before tagging v0.1.0
`NOTICE` lists what was adapted (GPUI's SDF renderer and shadows,
Microsoft Terminal's text gamma, Evan Wallace's shadow technique, the GFM
spec text, the QML-Examples-Collection corpus). Confirm the corpus
repository's licence covers the re-authored `.gml` pages and the parity
screenshots under `script/gml/check/qml-examples`, and that
`examples/images/assets/logo.png` is ours.

### Versioning
Tag `v0.1.0` once CI is green. The framework is one module; `examples/`
replaces it with `../`, so a tagged release needs nothing more. Decide
whether `markdown/` (the editor chrome) ships in the module or moves out
before v0.1.0 — it is a `main` package with no tests.

### Sample assets
`scripts/fetch-fonts.sh` downloads the images the `.gml` gallery pages
reference (`build/images/img1..6.jpg`, `anim.gif`); the pages reach them
by a relative path from `examples/qml/gpui/`. A fetched sample set for
the FBX pipeline is missing: `render/fbx`, the `render` skinning
benchmarks, `examples/actor` and `examples/actorblend` want a rigged
Mixamo-style character under `GFX_FBX_ASSETS` and skip without one. Find
a CC0/CC-BY rigged FBX and add it to `scripts/fetch-assets.sh`.

## Next

### Android: recreate the surface when the window comes back (2026-10-15)
A locked screen destroys the activity's window; the GPU surface belongs
to it and nothing recreates one, so the backend exits the process on
`GFX_EV_WINDOW_DESTROYED` rather than leave a black activity. The fix is
a surface-lost signal from `app.Window` and `plat.NewSurface` again in
every frame loop (`ui/shell.RunGame`, the examples), with the program's
state kept.

### gml frame cost (2026-10-10)
509 → 147 µs on the buttons page; pages built out of delegate rows moved
much further. What still rebuilds is animating content; the next
measured step is skipping a cached row's scope declaration, and buttons
needs a layered scope to go further. → [`script/TODO.md`](script/TODO.md).

### QML parity review — 1 of 65 pages signed off (2026-09-14)
Every corpus page opened three times at once (`scripts/qml-trio.sh
<page>`: real Qt, our .gml engine, a Go port) and reviewed by eye,
interaction included. → [`script/TODO.md`](script/TODO.md) has the page
list and what is open on each.

### Verify on devices and in a browser
Android touch, Android audio and clipboard, and web audio and clipboard
are compile-checked (NDK clang, js vet) and the Android pieces ran on
one phone (2026-10-15); the web pieces have not been exercised in a
real browser. Win32 `WM_TOUCH` is not handled.

## Missing features (DESIGN.md Phase 4 and the status notes)

- **`mem`** (§7): still a doc-only stub. Arena allocators with Frame /
  Scene / App scopes, typed pools, `Handle[T]`, and the 0 allocs/op
  benchmarks that guard hot paths — none of it exists yet.
- **`core`** (§3): `PropertyRef[T]` (the Property API over an ECS column
  row) is not built.
- **`ecs`** (§4): sparse-set columns rather than archetype tables; no
  system scheduler (and the js/wasm single-thread policy it must carry);
  no change detection beyond the GPU-shared pools.
- **`scene`** (§4.1, §6.3): no typed nodes, no lifecycle (`Ready`,
  `Process`), no Blueprint-style task API, no `.tscn`-like scene file.
- **`app`** (§8): Wayland; multi-window beyond X11 (`NewChild`, `Hide`,
  `Show` are X11-only); the null-GPU headless backend (§8.1 — today
  headless still needs wgpu-native); macOS/iOS `app` glue (rendering
  already works through wgpu's Metal backend).
- **`gpu`/`render` perf headroom**: render bundles, indirect draws with
  GPU culling, a pipeline cache, threaded encoding, a compute pre-pass
  for skinning that writes deformed vertices once for many draws.
  Measured 2026-10-15: a 1080p scene makes ~200 ffi crossings (~11 µs)
  in a 1.2 ms frame, so nothing here pays until a scene draws thousands
  of items.
- **`render` 3D**: clustered forward+, global illumination, a
  user-facing shader API (we have WGSL pipelines internally; Ebiten
  exposes Kage), and adaptive flattening by default for pixel-space
  paths (`render.Path.CubicTo` without `Flatness` can put 160 points on
  every segment and ask for hundreds of MB of vertices).
- **`ui`**: `TextArea` has no scrollbar, rich text or undo; no theming
  layer beyond per-element styling; the QML `when`-based states,
  `textFormat: RichText`, Grid `rows` and two-axis Flickable gaps live
  in the gml engine's list.
- **`script`** (§6.2, Phase 4): XGo integration, hot reload of Go-level
  code, editor seeds. The `.gml` engine is the DSL that shipped instead;
  full QML compatibility is staged in `script/TODO.md`.
- **`audio`**: Web Audio and AAudio verified only once; no mixer, no
  spatialisation, no streaming decode.
- **`physics`**: P3+ of `physics/PLAN.md` (joints, continuous collision,
  a 2D variant).
- **Markdown editor**: `markdown/PLAN.md` is the plan; only the reference
  chrome exists. Needs the multiline editor the toolkit lacks.
- **Showcase demos**: geometric algebra, an anime.js-style landing page
  clone, a procedural-generation demo once such a library lands.

## Missing tests

- `mem` has nothing to test yet; `gpu/plat`, `ui/icons`,
  `ffi/cmd/ffigen`, `script/cmd/gfx` (a golden test over the generated
  WGSL would do) and `markdown` (a headless screenshot test) have no
  tests.
- The examples are smoke-tested only through three screenshots in
  `scripts/check.sh` (settings, kitchensink, frost). A table-driven
  headless screenshot test over every example that supports
  `-screenshot` would catch regressions per example.
- No js/wasm tests run anywhere: `go vet` is the only web gate. A wasm
  test runner (`go test` with `GOOS=js` and a Node exec wrapper) over
  `core`, `ui` and `md` would cover the platform-independent layers.
- The FBX real-asset tests and skinning benchmarks need `GFX_FBX_ASSETS`
  and skip without it; see "Sample assets" above.
- No alloc-count guards run in CI although hot paths target 0 allocs/op;
  add `testing.AllocsPerRun` checks for the 2D batcher, property
  re-evaluation and the frame loop.
- Android and device checks (`examples/devcheck`) are manual.
- `core`: no tests for `TrackReads`/`ReadSet` (which carries the gml
  subtree cache), none for `geom.go` (`RayAABB`, `RayTriangle`,
  `RaySphere`, `ClosestOnSegment`, `ClosestOnTriangle`); `Vec2` and
  `Vec4` have no direct tests.
- `render/fbx`, `gpuskin_test.go` and `skin_test.go` skip entirely in
  CI: no self-contained FBX fixture, so `Parse`/`ToGLTF`/`Retarget` are
  never exercised there.

## Found while writing the docs (2026-10-31)

Small inconsistencies the documentation pass turned up, each one or two
lines of work; fix them as they are touched.

- **`core` animation**: `Tween` stops only an earlier tween, not a live
  spring on the same property (`TweenLoop` and `SpringConfig` stop
  both), so a tween and a spring can fight over `Set`. `Tween` with
  `d <= 0` sets immediately while `TweenLoop` clamps to 1 ms. Integer
  tweens truncate instead of rounding (an `int` tween 0→10 sits at 0 for
  the first tenth of its duration). `Loop.Step` does not call
  `TickAnimations`; only `ui/shell` does, so a bare `core.Loop` user gets
  no animation unless they know to call it.
- **`core` math**: `Vec2` lacks `Neg`; `Vec4` lacks `Mul`/`Lerp`/
  `Normalize`; no TRS compose helper and no `Mat4.MulDir` (w = 0).
- **`ui/shell`**: `RunGame`/`RunGame2` ignore `Options.MaxFPS`,
  `LineHeight`, `MSDFText`, `Undecorated` and `Transparent` (only
  `RunUI`/`Run` apply them) and the `Options` comment does not say which
  runner reads which field. `RunGame` opens the window before the first
  frame while `Run` opens it hidden and shows after the first present.
  `Unmovable`/`Unresizable`/`Unminimizable` are Windows-only and
  `OpenChild` X11-only without saying so at the `app.Window` level.
- **`ui`**: `Input` does not scroll long text horizontally (it clips);
  `TextArea` has no vertical scrollbar. `List.Heights` silently ignores
  a slice of the wrong length. `GridEl` has no padding and `ScrollEl` no
  background/rounding (callers wrap a Div). `ui/icons` has 35 glyphs but
  no exported `Names()` and `icons.SVG` is re-parsed by every caller.
  `LayerState.Release` is manual, so dynamically created layers leak GPU
  textures unless the app tracks them.
- **`gpu`**: `ReadTexture` assumes 4 bytes per texel (`RGBA16Float` and
  `R8` are unreadable; `BytesPerTexel` knows only R8). `TextureFormat`
  is `uint32` natively and `string` on js, so a literal breaks one
  build. No WGSL-reflection codegen for bind-group layouts; the web
  backend issues one JS call per encoder call; web lacks MSAA textures
  and dual-source blending. `ffi.CallF` has no Windows implementation;
  the `go:embed` distribution of wgpu-native (DESIGN §6.1) is not built.
- **`ecs` / `gfx gen`**: one merged dirty span per frame uploaded via
  `WriteBuffer` (no persistently mapped staging, no double-buffered
  readback); `gfx gen` supports only `float32/int32/uint32/Vec2/Vec4/
  Mat4` (no arrays, nested structs, `Vec3` padding helpers), writes
  `gfx_gen.go` only, and is used by exactly one package.
- **`render` 3D**: render graph v0 is RGBA8 transients and colour-only
  passes with no depth attachments or aliasing, and `Renderer3D` and the
  2D UI bypass it. Every legacy draw is instance count 1; the legacy
  `Draw*` family ignores `FrameSettings` and local lights; local lights
  cast no shadows; LDR with tone mapping in the fragment shader; no IBL.
  Picking tests only the first transform of an instanced draw and picks
  skinned geometry in bind pose.
- **glTF / FBX**: `AlphaMode == "MASK"` and `AlphaCutoff` are parsed but
  the shaders use a fixed 0.5 cutout; only
  `KHR_materials_pbrSpecularGlossiness` is handled (no
  `KHR_texture_transform`, emissive strength, Draco); morph targets,
  cameras and lights are not loaded. ASCII FBX is unsupported; FBX
  materials are diffuse + shininess only.
- **`scene`**: `Tree.Update` rescans the whole column per depth level
  rather than dirty subtrees.
- **`physics/p3`**: dynamic bodies are spheres and boxes only (no GJK/EPA,
  dynamic capsules or hulls); the BVH rebuilds on every mutation instead
  of refitting; tower stacking wanders laterally (no block solver or
  persistent manifolds).
- **Examples and scripts**: `counter`, `triangle`, `particles` and
  `devcheck` have no `-screenshot` flag (`-frames` only). Several
  `linux || windows` examples (`skinned`, `actor`, `actorblend`, `fps`,
  `partviewer`, `viewport3d`) have no js stub. `scripts/build-web.sh`
  writes no `index.html`; only `counter`, `cubes`, `kitchensink`,
  `settings` and `triangle` ship one. `build-apk.sh` and
  `build-wgpu-android.sh` hard-code the NDK's `linux-x86_64` prebuilt
  path, so Android packaging is Linux-host only. `GFX_CALLS=1` reports
  once per second from `Submit`, so a sub-second headless run prints
  nothing; a one-shot dump at exit would suit `-screenshot` runs. No
  example demonstrates `app.Headless` with input injection on the 3D
  path.
- **`script/gml`** (found while verifying `docs/gml.md`): call-argument
  spread (`Math.max(...xs)`) is not parsed (spread works only in array
  literals); array mutators need an assignable receiver
  (`items.slice().sort()` fails) and `sort(cmp)` ignores its comparator;
  `qsTr` is not a global; a use-site child with `anchors.centerIn:
  parent` inside a file component renders off-centre (`parent` resolves
  to the wrong element; no test covers use-site children with anchors);
  `ListView.Horizontal` is accepted but renders vertically, `header`/
  `footer` are rejected on strict `.gml`; `Connections` honours only the
  Qt 6 `function onX()` form and passes no signal arguments; `Keys`
  events have no `modifiers`/`accepted` and a 13-code key table;
  `RegularExpressionValidator` never matches and regex literals cannot
  be used; silent no-ops: `Settings`, `Shortcut`, `SystemPalette`,
  `AnimatedImage` (frame 1), `BorderImage` (not 9-patch), tiling fill
  modes, `MouseArea` drag/wheel/double-click/long-press/`mouseX`, pointer
  handlers beyond tap/hover, `Dial` drag, `SplitHandle.pressed/hovered`;
  `LoadString` has no base directory (no imports, singletons or file
  components; relative sources resolve against the cwd) — add
  `LoadStringAt(src, baseDir)`; lenient mode is keyed on the `.qml`
  extension only; Timers advance on the wall clock between `View()`
  calls so `-screenshot` output of a page with a running Timer is
  non-deterministic; `Qt.locale(name)` is always en-US. Missing tests:
  `a?.b()`, `for…of` over a string, `switch` fall-through, `try/catch`
  with a thrown object, `Qt.callLater` ordering, `Loader { source:
  "sub/File.qml" }` resolution from a subdirectory.

## Later (deliberately deferred — build when asked)

### HVM2-style parallel reduction of the scene graph (2026-10-10)
Can the per-frame evaluation — property bindings, the element tree
rebuild, measure/layout over the scene graph — be expressed as
interaction-net reduction the way HVM2 (Bend's runtime) does it, so it
parallelises by construction? The dependency graph `core` already
tracks is the natural net; the question is whether the rewrite rules
stay local enough that independent subtrees reduce on different cores
without locks, and what that does to the single-owner threading rule.
Must degrade to one thread on js/wasm. Open: read the HVM2 paper,
prototype on the `core` dependency graph first.

## Recently closed

Kept as one line each so the next person does not reopen them.

- **Open-source split** (2026-10-31). The framework moved to its own
  repository and one Go module (`github.com/sushvibez/gfx`); the
  applications built on it live in their own repositories. The layering
  rule moved from the module graph to `internal/layering`. Two
  Windows-only test failures fixed on the way: a `.gml` string literal
  swallowed the backslashes of a temp path (`script/gml/pdf_test.go`),
  and the FBX test binary needs building outside `%TEMP%` on machines
  with an application-control policy.
- **X11 teardown crash** (2026-10-15). Every example that returned from
  its frame loop died in the deferred `win.Destroy`: the Vulkan ICD hooks
  the X connection a surface is made from, releasing the wgpu instance
  unloads the ICD, and `XCloseDisplay` then jumped into unmapped code.
  `plat.NewSurface` now marks the window (`RetainDisplay`) so Destroy
  leaves that connection to the process.
- **Nothing left on reflection (SysV)** (2026-10-15). `ffi.CallF` passes
  floats in the vector registers, so `SetViewport` joins the trampoline
  on linux/android amd64+arm64; Windows and other architectures keep it
  on `purego.RegisterFunc`. With `CallStack` for the by-value request
  structs, every wgpu call on a SysV platform is a generated or
  hand-written stub over the trampoline.
- **Android verified on a phone** (2026-10-15, Adreno 740, Android 16).
  `devcheck` passes every line: window, surface, device, JNI clipboard,
  AAudio, 600 frames with zero device errors. AAPCS64's eight integer
  argument registers and by-reference structs over 16 bytes are handled
  per arch in `ffi`; Android 16 recreates the activity's first window
  during its initial layout, so `AndroidWindow` waits for a live one.
  The emulator's gfxstream Vulkan driver dies on the first queue submit
  and SwiftShader fails at `vkCreateDevice` — both emulator-side.
- **Clipboard on Android and the web** (2026-10-10). Android reads and
  writes the ClipboardManager over JNI from the render thread; the web
  takes the `paste` event's text synchronously and writes through
  `writeText`.
- **2D batches merge across disjoint draws** (2026-10-15). A HUD
  alternating panels and labels made 32 draws and 30 pipeline switches a
  frame; `mergeBatches` joins a batch back into the nearest earlier
  compatible one when everything between is bounded and disjoint (2 px
  AA margin): 12 draws, 6 switches, frame time unchanged.
- **Device buffer limits follow the adapter** (2026-10-15).
  `RequestDevice` asks for the adapter's own `maxBufferSize` and
  `maxStorageBufferBindingSize` on both backends and `Device.Limits()`
  reports them.
- **FFI without reflection** (2026-10-10). `ffi.Call` is an assembly
  trampoline (amd64, arm64; `syscall.SyscallN` on Windows): 39 ns /
  0 allocs against 300–900 ns / 4–13 allocs for `purego.RegisterFunc`.
  `ffi/cmd/ffigen` writes typed stubs for 63 of the 67 wgpu symbols.
- **Markdown, the `md` package, and `ui.NewCanvasFlow`** (2026-10-07). A
  CommonMark + GitHub-extensions parser, layout/drawing on Renderer2D, a
  ui element and a headless viewer; `.gml` `Text.MarkdownText` renders
  real Markdown.
- **`ui.TextArea`, multi-line editing** (2026-10-05). Word wrap, Enter =
  new line, Ctrl+Enter submits, caret movement by wrapped line, click and
  drag, scroll to caret; `.gml` `TextArea`/`TextEdit` build it.
- **Frame loop floor and damage-driven rendering** (2026-09-25). A static
  `RunUI` window builds no swapchain image, no command buffer and no
  present, and measures 0 CPU ticks over 12 seconds; `RunGame` gets the
  same skip through `Frame.Unchanged()`, plus `Options.MaxFPS`.
- **The 2D path against Zed's "We rebuilt the UI like a videogame"**
  (2026-09-23). Three gaps, all in text, all closed — a shaped-run cache,
  alpha-only glyph sheets and instanced glyph draws took a sixty-line
  page from 707 to 195 µs. →
  [`docs/notes/2d-text-and-batching.md`](docs/notes/2d-text-and-batching.md).
- **The MSDF glyph atlas** (2026-09-25), including small text, the
  ui/gml wiring, and the field chosen per draw for transformed text.
- **gml dirty tracking**: per-subtree read-sets, 2 rebuilds a frame
  against 201. AST→closure compilation was dropped — evaluation is ~3 %
  of a frame.

## Rules that apply to everything here
- Bounded runs only: `scripts/bounded.sh`, `scripts/qml-perf.sh`,
  foreground, short `-frames`. Two unfenced runs took a desktop down.
- Gates before a commit: `scripts/check.sh` (linux/windows/js build+vet,
  the layering test, tests, smoke), the warning sweep prints 0, and the
  corpus renders unchanged.
