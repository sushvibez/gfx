# gfx

A Go framework for 2D user interfaces and 3D, rendered through WebGPU,
built without cgo: `go build` is the whole build, cross-compilation
included.

- **2D**: QML-inspired. Declarative properties with automatic dependency
  tracking, signals, an animation engine, and a real widget toolkit:
  flexbox and QML-style anchors, focus and keyboard handling, overlays,
  virtualised lists, text editing, SDF-styled drawing, MSDF text. A
  QML-compatible `.gml` markup engine runs QML-style documents with live
  bindings, a JavaScript-subset evaluator and hot reload.
- **3D**: Godot-inspired. A node tree over ECS storage the GPU can read,
  forward PBR with shadow mapping, glTF and FBX loading, skinning and
  animation blending, compute shaders over ECS columns, a kinematic
  physics engine, and 3D viewports embedded in the 2D UI.
- **Platforms**: Windows, Linux (X11), Web (WebAssembly, first-class) and
  Android (API 29+), plus a headless mode that drives and screenshots
  any program without a display, for CI and for agents.
- **No cgo**: native libraries load through purego and syscall, the
  browser through `syscall/js`. The only cgo in the tree is Android's
  `NativeActivity` glue.

## Quick start

```bash
git clone https://github.com/sushvibez/gfx && cd gfx
scripts/fetch-wgpu.sh      # wgpu-native into build/wgpu (Linux, Windows via Git Bash)
scripts/fetch-fonts.sh     # fonts and sample images into build/
cd examples
go run ./kitchensink                         # a window
go run ./kitchensink -screenshot out.png     # the same, headless
go run ./qml -file qml/gpui/hello_world.gml  # run any .gml file, hot reload on save
```

[docs/getting-started.md](docs/getting-started.md) has the full walk
through, the environment variables, and a catalogue of every example.

## Use it in your project

```bash
go get github.com/sushvibez/gfx
```

```go
import (
    "github.com/sushvibez/gfx/core"
    "github.com/sushvibez/gfx/ui"
    "github.com/sushvibez/gfx/ui/shell"
)
```

The framework is one Go module. The examples are a second module
(`examples/`) so the framework's `go.mod` carries no demo-only
dependencies.

## Layout

Strictly layered; every package depends only on the layers below it, and
`internal/layering` is the test that enforces it.

| Package | Responsibility |
|---------|----------------|
| [`core`](core) | math, `Property[T]` with dependency tracking, signals, event loop, animation |
| [`mem`](mem) | arenas, pools, generation handles (design only so far) |
| [`ffi`](ffi) | shared-library loading and calling without cgo |
| [`app`](app) | windowing, input, lifecycle, the headless backend |
| [`gpu`](gpu) | WebGPU: wgpu-native natively, the browser's WebGPU on the web |
| [`ecs`](ecs) | sparse-set ECS with GPU-sharable columns |
| [`render`](render) | render graph, 2D batcher, 3D renderer, glTF/FBX, text, paths, SVG, PDF |
| [`scene`](scene) | node tree over `ecs` |
| [`ui`](ui) | the widget toolkit and `ui/shell`, the window and frame-loop helpers |
| [`physics`](physics) | `physics/p3`: shapes, queries, a kinematic character, rigid dynamics |
| [`audio`](audio) | decoding and playback: ALSA, WinMM, AAudio, Web Audio |
| [`md`](md) | CommonMark + GFM parser, layout and a `ui` element |
| [`script`](script) | `script/gml`, the `.gml` engine; `script/cmd/gfx`, the WGSL layout codegen |
| [`markdown`](markdown) | the reference chrome of a future Markdown editor |
| [`examples`](examples) | runnable samples, one directory each |

## Documentation

- [docs/getting-started.md](docs/getting-started.md): install, run, first program, examples.
- [docs/core.md](docs/core.md): properties, signals, the event loop, animation, math.
- [docs/ui.md](docs/ui.md): the widget toolkit and the element builder.
- [docs/gml.md](docs/gml.md): the `.gml` language reference.
- [docs/3d.md](docs/3d.md): the 3D renderer, assets, animation, physics.
- [docs/gpu.md](docs/gpu.md): the WebGPU layer, `ffi`, `ecs` and codegen.
- [docs/platforms.md](docs/platforms.md): Linux, Windows, Web, Android.
- [docs/headless-and-ci.md](docs/headless-and-ci.md): driving programs without a display.
- [DESIGN.md](DESIGN.md): the architecture and roadmap, with status notes.
- [TODO.md](TODO.md): what is open, and [CONTRIBUTING.md](CONTRIBUTING.md).

## Status

Phases 0 to 3 of the roadmap are implemented and verified by
`scripts/check.sh` (gofmt, build and vet for linux, windows and js/wasm,
the layering test, every test, a headless screenshot smoke). The 2D
toolkit, the `.gml` engine, GPU compute over ECS columns, forward PBR with
shadows, glTF and FBX, skinning, the Android pipeline and headless mode are
real and used. Still open, and tracked in [TODO.md](TODO.md): the `mem`
arena module, ECS systems and archetypes, typed scene nodes, Wayland,
macOS, XGo scripting, and the QML parity review of the `.gml` engine.

## License

[Apache-2.0](LICENSE). Third-party attributions are in [NOTICE](NOTICE).
Contributions are accepted under the terms in
[CONTRIBUTING.md](CONTRIBUTING.md#licence): Apache-2.0, a grant that lets
the project move to another open-source licence later, and a signed-off
commit (`git commit -s`).

### Maintainers: enforcing the sign-off

GitHub does not check sign-offs itself; the free DCO app does:

1. Open <https://github.com/apps/dco>, choose **Install** (or
   **Configure** if it is already on the account), pick the account that
   owns the repository and select only this repository.
2. The app now adds a **DCO** check to every pull request. It fails when a
   commit has no `Signed-off-by:` line matching the commit's author, and
   its details page tells the contributor how to fix it
   (`git commit --amend -s` for the last commit,
   `git rebase --signoff main` for several).
3. Make the check required so an unsigned pull request cannot be merged:
   **Settings → Rules → Rulesets → New branch ruleset**, target the
   default branch, enable **Require status checks to pass** and add
   **DCO**. (The check appears in that list once the app has run on a
   first pull request; open a test one if the list is empty.)

Pushes made straight to the default branch by maintainers are not pull
requests, so the check does not run on them. Optionally, a
`.github/dco.yml` file configures the app, for example to let a
contributor sign off past commits with one remediation commit instead of
rewriting history:

```yaml
allowRemediationCommits:
  individual: true
```
