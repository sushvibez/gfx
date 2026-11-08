# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go framework for 2D UIs (QML as the reference) and 3D (Godot as the reference), rendered through WebGPU, targeting Windows, Linux, Web (wasm, first-class) and Android — with **no cgo** (the only exception is Android's `NativeActivity` glue). `go build` is the whole build.

**DESIGN.md is the authoritative architecture document** — read the relevant section before working on a layer. Phases 0–3 of its roadmap plus the QML interlude are done; DESIGN.md carries per-section *Status (date)* notes marking where implementation deviates from the design (e.g. sparse-set ECS instead of archetypes, no `mem` arenas yet, no null-GPU backend). Trust those notes over the surrounding design prose, and update them when you close a gap. **TODO.md** is the open list; when something changes state, change the file in the same commit.

## Commands

One Go module (`github.com/sushvibez/gfx`) at the repo root, plus `examples/`, a second module that replaces the framework with `../` — run example commands from inside `examples`.

```bash
scripts/check.sh                      # the full CI sweep: gofmt, build+vet for
                                      # GOOS=linux/windows/js, layering test, all tests,
                                      # headless screenshot smoke (Git Bash on Windows)

go test ./...                         # every framework test
go test ./ui/...                      # one layer
go test -run TestName ./core/...      # single test
go test ./internal/layering/          # the downward-only import rule
cd examples && go test ./...          # the examples module
```

`check.sh` fails on unformatted files and requires both modules to build and vet on **linux, windows, and js/wasm** — keep platform-specific code behind build-tagged files so all three stay green. Recurring traps: `app.Window` methods must exist on every backend (x11_linux, win32_windows, web_js, android — add error-returning stubs where a feature is X11-only, like `NewChild`/`Hide`/`Show`); GPU readback (`ReadTexture`/`ReadBuffer`, so all screenshot paths) is native-only, so js needs a stub file; a `main` package tagged `linux || windows` must tag *all* its files, or the js build sees a main package without `main`. Note `gofmt` import ordering: `github.com/sushvibez/...` sorts after `github.com/chewxy/...` and `golang.org/...` within a group.

On Windows some machines' application-control policy blocks test binaries in `%TEMP%`; build them elsewhere: `go test -c -o build/x.test.exe ./render/fbx && (cd render/fbx && ../../build/x.test.exe)`.

### Running examples (native / headless)

Native runs need wgpu-native: `scripts/fetch-wgpu.sh` puts it at `build/wgpu/libwgpu_native.so` (`wgpu_native.dll` on Windows; override with `GFX_WGPU_LIB`). Some examples need `scripts/fetch-fonts.sh` / `scripts/fetch-assets.sh` (fonts, sample images, glTF samples, textures → `build/`); `GFX_FONT` / `GFX_CJK_FONT` override font paths, `GFX_TEXTURES` the material sets, `GFX_FBX_ASSETS` the rigged-character fixtures the FBX tests and `actor`/`actorblend` use (they skip without it).

Headless mode is first-class — every example can render offscreen and screenshot without a display:

```bash
cd examples
go run ./settings -screenshot /tmp/out.png
go run ./qml -file qml/gpui/hello_world.gml -w 600 -h 400 -screenshot /tmp/out.png  # run any .gml file
```

Other builds: `scripts/build-web.sh` (wasm), `scripts/build-apk.sh` (signed APK, no Gradle; needs `scripts/build-wgpu-android.sh` first; `GFX_APK_MODULE=. GFX_APK_PKG=./md/cmd/mdview` for a program outside `examples/`).

## Architecture

Strictly layered, downward dependencies only — enforced by `internal/layering` (a test over `go list`):

```
script (gml/.gml engine, gfx codegen)   examples
md (markdown parser, layout, ui element)
ui (widgets, ui/shell)      scene (node tree)
        render (render graph, 2D batcher, 3D, text, paths, gltf/fbx, pdf)
ecs ────┤
        gpu (WebGPU: wgpu-native via ffi natively, syscall/js on web; gpu/plat surfaces)
app (window, input, lifecycle, headless backend)      physics/p3     audio
ffi (purego / syscall / asm trampolines — the cgo replacement)
core (math, Property/Signal, event loop, animation; core/settings: saved settings)    mem (arenas, pools, handles — stub)
```

Cross-cutting rules that shape most code here:

- **Properties & signals (`core`)**: `Property[T]` with automatic dependency tracking — `Get()` inside a binding closure records a dependency; `Set()` marks dependents dirty; bindings re-evaluate lazily once per frame in topological order. Single-owner threading (Qt-style): all properties/UI objects belong to the event-loop goroutine, no locks; other goroutines use `loop.Post`.
- **UI model (GPUI-style hybrid)**: state lives in retained view objects holding Properties; a `Render()` function rebuilds a lightweight element tree every frame using a fluent builder API (`ui.Div().Row().Gap(8)...`). 2D drawing is SDF-styled instanced quads.
- **`script/gml`**: the QML-compatible `.gml` markup engine — lexer/parser/JS-expression evaluator (`lex.go`/`parse.go`/`eval.go`/`js.go`) that builds ui element trees (`build.go`). The goal is running real QML files; `script/TODO.md` tracks the page-by-page parity review and `script/gml/check/QML-EXAMPLES.md` the findings log. Bindings re-evaluate per frame, mitigated by whole-document frame memoization: `Document.View()` returns the cached tree when `core.Revision()` and the geometry/scroll signatures are unchanged.
- **ECS + node tree**: the node tree is the public API; ECS columns are its storage. Entities/nodes are `{index, generation}` handles, never pointers. GPU-shared component types must be POD and match WGSL std430 layout — `script/cmd/gfx` (`go generate`) emits the WGSL structs plus compile-time offset/stride assertions.
- **GPU**: WebGPU is the only graphics API, WGSL the only shader language. The `gpu` package is a thin faithful mirror (explicit `Release`); convenience belongs in `render`. On web, batch work per JS-boundary crossing. `ffi/cmd/ffigen` (`go generate` in `gpu`) writes the typed wgpu stubs over the assembly trampoline.
- **Memory discipline**: arena memory (`mem`) is POD-only — no Go pointers inside; cross-references use `mem.Handle[T]`. Hot paths target 0 allocs/op. APIs take value types (`Vec3`, not `*Vec3`).
- **js/wasm has no threads**: anything parallel (ECS scheduler, renderer) must degrade to single-threaded as a scheduling policy, not a build variant.

`examples/internal/orbit` is the shared orbit camera; `ui/shell` is the window+renderer harness (`RunUI` for apps, `RunGame` for frame loops) and `gpu/plat` the per-platform surface glue. Per-area lists: `script/TODO.md` (the .gml engine), `md/TODO.md`, `physics/PLAN.md`, `docs/notes/sdf-ideas.md`.
