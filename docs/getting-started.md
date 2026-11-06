# Getting started

gfx is a Go framework for 2D UI (QML-style properties and bindings) and 3D,
rendered through WebGPU, with no cgo. This page goes from a clean machine to
a running example and a first program of your own. Per-platform notes are in
[platforms.md](platforms.md); offscreen rendering and the CI sweep are in
[headless-and-ci.md](headless-and-ci.md).

## Prerequisites

| Need | Why |
|---|---|
| Go 1.26 or newer | `go.mod` declares `go 1.26.4`. |
| No C toolchain | Nothing uses cgo on Linux, Windows or the web: wgpu-native is loaded at runtime through `ffi` (purego / syscall). Android packaging is the one exception (see platforms.md). |
| bash, curl, unzip | For `scripts/*.sh`. On Windows use Git Bash. |
| A GPU driver wgpu-native can use | Vulkan on Linux, D3D12 on Windows. A software adapter is enough for headless runs (lavapipe on Linux; the D3D12 software adapter on Windows). |
| A browser with WebGPU | Only for web builds. |

Module layout: `github.com/sushvibez/gfx` is one Go module. `examples/` is
a second module (`github.com/sushvibez/gfx/examples`) that `replace`s the
root with `../`, so the framework carries no demo-only dependencies. Run `go`
commands from the repository root for the framework and from `examples/` for
the examples.

```bash
git clone https://github.com/sushvibez/gfx
cd gfx
scripts/fetch-wgpu.sh      # the WebGPU implementation (required for anything native)
scripts/fetch-fonts.sh     # optional: CJK/emoji/UI fonts and sample images
scripts/fetch-assets.sh    # optional: glTF models and textures for the 3D examples
cd examples && go run ./kitchensink
```

## wgpu-native: `scripts/fetch-wgpu.sh`

Native builds render through [wgpu-native](https://github.com/gfx-rs/wgpu-native),
a shared library the `gpu` package loads at runtime. The script downloads the
pinned release (`v29.0.0.0`, the only version the bindings are written
against) for the host OS and architecture, unzips it and flattens the
archive's `lib/` so the library sits at the top of `build/wgpu/`:

| Host | File |
|---|---|
| Linux | `build/wgpu/libwgpu_native.so` |
| Windows | `build/wgpu/wgpu_native.dll` (the MSVC build of the release) |
| macOS | `build/wgpu/libwgpu_native.dylib` (fetched, but there is no macOS `app` backend yet) |

`build/` is gitignored. `WGPU_OS` / `WGPU_ARCH` fetch for another target
(packaging a Windows build on Linux, say); a cross fetch lands in
`build/wgpu-<os>-<arch>/` so it cannot overwrite the host's copy.

How the library is found at runtime (`LibPaths()` in
`gpu/plat/plat_linux.go` and `gpu/plat/plat_windows.go`), first hit wins:

1. `GFX_WGPU_LIB`, if set: the full path to the library, nothing else is tried.
2. Beside the executable, or in a `wgpu/` directory beside it (what a packaged app ships).
3. `build/wgpu/<name>` relative to the working directory, then `../build/wgpu`, then `../../build/wgpu`, so `go run` from `examples/` finds the repository's copy.
4. The bare library name, left to the system loader.

`go run ./probe` (from `examples/`) acquires an adapter headlessly and prints
what it found, the quickest check that the library loads:

```
wgpu 29.0.0.0: Intel(R) Graphics (cpu, d3d12, 32.0.101.8356, vendor=0x8086 device=0xb0a0, driver )
```

## Fonts and sample assets

Neither script is required: text renders with the bundled Go Regular face when
nothing else is found, and the 3D examples that need a model say so when it
is missing.

`scripts/fetch-fonts.sh` downloads into `build/fonts/` and `build/images/`,
skipping files already present:

| File | Used for |
|---|---|
| `build/fonts/NotoSansCJKsc-Regular.otf` | CJK fallback (the `text_wrapper` GPUI port's 你好 / こんにちは run). A single-face OTF, because `x/image/opentype` cannot open `.ttc` collections. |
| `build/images/img1.jpg` … `img6.jpg`, `build/images/anim.gif` | The `image_gallery`, `gif_viewer` and `opacity` `.gml` ports. |
| `build/fonts/NotoColorEmoji.ttf` | Colour emoji, only fetched when no system Noto Color Emoji exists. |
| `build/fonts/Inter-{Regular,Medium,SemiBold}.ttf` | UI font of the markdown app chrome. |
| `build/fonts/JetBrainsMono-{Regular,Medium}.ttf` | Fixed-pitch digits for HUDs. |

`scripts/fetch-assets.sh`:

| File | Used by |
|---|---|
| `build/assets/Duck.glb`, `build/assets/BoxTextured.glb` | `model` (Khronos glTF-Sample-Assets) and the render tests, which skip when the files are absent. |
| `build/textures/<name>_diff_1k.jpg` for `damaged_plaster`, `cracked_concrete_wall`, `metal_plate`, `brick_pavement_02` | Poly Haven CC0 textures. Note: the `materials` example expects `build/textures/<name>_1k/<name>_{diff,nor_gl,arm}_1k.jpg`, which this script does not produce; fetch the three maps of a set by hand into that layout, or point `GFX_TEXTURES` at a directory that has it. |
| `build/models/CesiumMan.glb` | `skinned`. |

Default font resolution (`ui/shell/fonts.go`): `GFX_FONT` if set; otherwise
the system UI font (Segoe UI, then Arial on Windows; Ubuntu Sans, Ubuntu,
Cantarell, Noto Sans, DejaVu Sans on Linux); otherwise the bundled Go Regular,
which always works, including on wasm. The CJK, emoji and symbol fallbacks are
looked up in `build/fonts/` (and `../build/fonts/` when launched from a module
directory), then in the usual Linux system font paths.

## Environment variables

Every `GFX_*` variable in the tree, grouped by layer. "Default" is what
happens when the variable is unset. "Any value" means the variable only has
to be non-empty (`GFX_FPS=1`).

GPU (`gpu/`):

| Variable | Meaning | Read in | Default |
|---|---|---|---|
| `GFX_WGPU_LIB` | Path to the wgpu-native library; replaces the whole search list. | `gpu/plat/plat_linux.go`, `gpu/plat/plat_windows.go` (also the GPU tests, `examples/probe`, `scripts/check.sh`) | The search order above |
| `GFX_WGPU_BACKEND` | `vulkan`, `dx12`, `gl`, `metal` or `all`: which wgpu backends the instance may use. | `gpu/api_native.go`; `app/win32_windows.go` reads it for the window's redirection-bitmap flag | `dx12` on Windows, `all` elsewhere |
| `GFX_WGPU_PRESENT` | `immediate`, `mailbox` or `fifo`: swapchain present mode. | `gpu/api_native.go` | `fifo` (vsync) |
| `GFX_WGPU_POWER` | `low` or `high`: adapter power preference (`low` is the iGPU on a hybrid laptop). | `gpu/api_native.go` | `high` |
| `GFX_WGPU_DX12_SWAPCHAIN` | `hwnd` or `visual`: a plain-HWND DXGI swapchain or a DirectComposition visual (DX12 only). | `gpu/api_native.go`, `app/win32_windows.go` | `visual` |
| `GFX_WGPU_LOG` | `error`, `warn`, `info`, `debug` or `trace`: route wgpu-native's log to stderr at that level. | `gpu/wgpu_native.go` | off |
| `GFX_CALLS` | Any value: print per-submit encoding and upload counts to stderr once a second, and once more when the device is released (see headless-and-ci.md). | `gpu/stats.go` | off |
| `GFX_UPLOAD_TIMING` | Any value: also time buffer and texture uploads (`gpu.UploadNanos`). | `gpu/stats.go` | off |
| `GFX_UPLOAD_TRACE` | Any value: log individual slow buffer writes. | `gpu/resources_native.go` | off |

Windowing (`app/`):

| Variable | Meaning | Read in | Default |
|---|---|---|---|
| `GFX_SCALE` | Force the DPI scale (physical pixels per logical pixel), `0 < x <= 8`. | `app/scale.go`, consulted by `app/x11_linux.go` and `app/web_js.go` | Platform detection (`Xft.dpi` on X11, `devicePixelRatio` on the web) |
| `GFX_DATA_DIR` | Set by the framework, not read by it: on Android `app.NewWindow` exports the activity's private data directory here for packages that resolve paths without importing `app`. | `app/android.go` | unset off Android |

UI shell (`ui/shell`, the harness every example runs through):

| Variable | Meaning | Read in | Default |
|---|---|---|---|
| `GFX_FONT` | Path to the UI font file. | `ui/shell/fonts.go` | System font, then the bundled Go Regular |
| `GFX_CJK_FONT` | Path to the CJK fallback font. | `ui/shell/fonts.go` | `build/fonts/NotoSansCJKsc-Regular.otf`, then system Noto |
| `GFX_EMOJI_FONT` | Path to a colour-emoji font (CBDT). | `ui/shell/fonts.go` | `build/fonts/NotoColorEmoji.ttf`, then system Noto |
| `GFX_SYMBOL_FONT` | A symbol font placed in front of the symbol fallback chain (▸ ⌂ ↻ …). | `ui/shell/fonts.go` | System Noto Symbols / DejaVu faces |
| `GFX_TEXT_GAMMA`, `GFX_TEXT_CONTRAST` | Floats: subpixel-text weight tuning. | `ui/shell/fonts.go` | Unset (Windows applies 2.0 / 0.4) |
| `GFX_MSDF` | `1` or `0`: force distance-field text on or off, for an A/B against the raster path. | `ui/shell/fonts.go` | The app's `shell.Options.MSDFText` |
| `GFX_FPS` | Any value: print presented frames, loop passes, damage and upload counts once a second; with `MaxFrames` set, the process CPU time at exit. | `ui/shell/shell.go` (fed by `ui/damage.go`, `ui/ui.go`, `gpu/stats.go`) | off |
| `GFX_MAX_FPS` | Integer cap on animation-driven frames per second; `0` removes the cap. | `ui/shell/shell.go` | 60 |
| `GFX_NO_DAMAGE` | Any value: disable present-on-damage idle skipping (always redraw). | `ui/shell/shell.go` | off |
| `GFX_CPUPROFILE` | Path: write a pprof CPU profile of the run (also flushed on SIGTERM/SIGINT). | `ui/shell/shell.go`, `ui/shell/screenshot.go` | off |
| `GFX_CPUPROFILE_SKIP` | Integer: frames to skip before the CPU profile starts (past font-atlas and pipeline start-up). | `ui/shell/shell.go` | 0 |
| `GFX_MEMPROFILE` | Path: write a heap profile when the window closes. | `ui/shell/shell.go` | off |
| `GFX_SETTLE_MS` | Integer: how much animation time a headless screenshot may advance before capturing. | `ui/shell/screenshot.go` | 2500 |
| `GFX_PROBE` | Any value: in an interaction script a `click` step instead prints a hit-test grid of the window (a debugging aid). | `ui/shell/script.go` | off |

Rendering and properties:

| Variable | Meaning | Read in | Default |
|---|---|---|---|
| `GFX_CPU_SKIN` | Any value: skin animated models on the CPU instead of in the vertex shader. | `render/gpuskin.go` (`render.GPUSkinning`), used by `render/model.go`, `render/skin.go` | GPU skinning |
| `GFX_UNTRACKED_TRACE` | Any value: count the call sites of untracked property reads (what keeps a gml subtree cache from hitting); read back with `core.UntrackedSites`. | `core/property.go`, `script/gml/eval.go` | off |

`.gml` engine (`script/gml`, debugging knobs for the document cache):

| Variable | Meaning | Read in | Default |
|---|---|---|---|
| `GFX_BUILD_STATS` | Any value: report per-frame subtree cache hits and miss reasons. | `script/gml/doc.go`, `build.go`, `eval.go` | off |
| `GFX_BUILD_TRACE` | Any value: name every subtree built or served per frame. | `script/gml/doc.go` | off |
| `GFX_CACHE_SKIP` | Substring: force every subtree whose key contains it to rebuild. | `script/gml/doc.go` | unset |
| `GFX_NO_SUBTREE_CACHE` | Any value: rebuild every subtree every frame (the ground truth for a stale-cache bug). | `script/gml/doc.go` | off |
| `GFX_NO_ROW_CACHE` | Any value: disable delegate-row caching. | `script/gml/build.go` | off |
| `GFX_NO_PATCH` | Any value: disable subtree patching (every stale ancestor rebuilds). | `script/gml/build.go` | off |
| `GFX_PATCH_VERIFY` | `1`: check each patched tree against a full rebuild; `all`: check every frame. | `script/gml/patchverify.go` | off |
| `GFX_QSB_PATH` | Path list: extra directories searched for the compiled Qt `.qsb` shaders a document's ShaderEffect names. | `script/gml/shaderfx.go` | Beside the document only |

Tests and benchmarks only:

| Variable | Meaning | Read in |
|---|---|---|
| `GFX_FBX_ASSETS` | Directory holding a Mixamo export (`models/hostile.fbx`, `animations/Idle.fbx`); the real-asset FBX tests skip without it. | `render/fbx/convert_test.go`, `render/fbx/retarget_test.go`, `render/skin_test.go`, `render/gpuskin_test.go` |
| `GFX_QML_CORPUS` | Root of a QML example corpus for the gml benchmarks; they skip without it. | `script/gml/bench_corpus_test.go` |
| `GFX_PAGE_STATS` | `1`: enable `TestPageRebuildCounts`, the per-page rebuild report. | `script/gml/bench_pages_test.go` |
| `GFX_CLIP_MODE`, `GFX_CLIP_TEXT` | Internal to the X11 clipboard test's helper process. | `app/clipboard_x11_linux_test.go` |

Android packaging (`scripts/build-apk.sh`, see platforms.md): `GFX_APK_MODULE`,
`GFX_APK_PKG`, `GFX_APK_ORIENTATION`, `GFX_APK_ASSETS`, `GFX_LDFLAGS`. The QML
comparison scripts (`scripts/qml-compare.sh`, `qml-perf.sh`, `qml-trio.sh`)
read `GFX_REF_DISPLAY` (the X display for the Qt reference, default `:1`) and
`GFX_REF_GPU`; they need a Qt installation and a QML corpus checkout and are
development tools, not part of the build. The `GFX_EV_*` names in
`app/android.go` are C event constants from `android_glue.h`, not environment
variables.

## Running the examples

Everything runs from `examples/`:

```bash
cd examples
go run ./kitchensink                       # a window; close it to exit
go run ./kitchensink -frames 120           # exit after 120 frames (deterministic runs)
go run ./kitchensink -screenshot out.png   # no window: render offscreen, write a PNG
```

`-screenshot` renders offscreen and needs no display server, only the
wgpu-native library; it is native-only (the browser has no GPU readback). Most
examples take it (see the catalogue); `counter`, `triangle`, `particles` and
`devcheck` only take `-frames`.

Any `.gml` (QML-syntax) document runs through the `qml` example, with hot
reload when windowed:

```bash
go run ./qml                                      # examples/qml/app.gml, re-read on save
go run ./qml -file qml/gpui/hello_world.gml -w 600 -h 400 -screenshot out.png
go run ./qml -file qml/gpui/popover.gml -screenshot out.png -script @steps.txt   # a PNG per scripted step
```

`examples/qml/gpui/` holds 32 ports of Zed's GPUI examples as pure `.gml`;
`go run ./gpui -list` lists the same examples written against the Go API.

## Your first program

The smallest real program is `examples/counter`: one `core.Property[int]`
read inside a view function and two buttons that `Set` it. A `Get()` inside
the view records a dependency, so the view is rebuilt when the property
changes; there are no observers to wire. The version below adds a
`-screenshot` flag so it also runs without a window.

Put it in a new directory inside `examples/` (the `replace ../` there picks
up the framework), or in your own module with
`require github.com/sushvibez/gfx`:

```go
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

func button(label string, onClick func()) ui.Element {
	return ui.Div().Size(72, 40).Bg(ui.RGB(0x3b5bdb)).Rounded(8).
		HoverBg(ui.RGB(0x4c6ef5)).PressBg(ui.RGB(0x2f44ad)).
		JustifyCenter().ItemsCenter().OnClick(onClick).
		Children(ui.Text(label, ui.RGB(0xffffff)))
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	flag.Parse()

	var count core.Property[int]
	view := func() ui.Element {
		return ui.Div().Col().P(24).Gap(16).Bg(ui.RGB(0x16161e)).Children(
			ui.Text(fmt.Sprintf("count: %d", count.Get()), ui.RGB(0xe8e8f0)),
			ui.Div().Row().Gap(12).Children(
				button("-", func() { count.Set(count.Get() - 1) }),
				button("+", func() { count.Set(count.Get() + 1) }),
			),
		)
	}

	opts := shell.Options{
		Title: "hello gfx", Width: 420, Height: 240,
		FontSize: 20, MaxFrames: *frames,
	}
	var err error
	if *shot != "" {
		err = shell.ScreenshotUI(opts, view, *shot)
	} else {
		err = shell.RunUI(opts, view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hello:", err)
		os.Exit(1)
	}
}
```

```bash
cd examples && mkdir hello            # then save the program as hello/main.go
go run ./hello                        # a window with two buttons
go run ./hello -screenshot hello.png  # the same frame, no window
GOOS=js GOARCH=wasm go build -o main.wasm ./hello   # the browser build (see platforms.md)
```

What is in it: `shell.Options` sets the window, the font size and
`MaxFrames`; `shell.RunUI` opens a window (X11, Win32 or a browser canvas)
and drives the view; `shell.ScreenshotUI` renders the same view into an
offscreen texture and writes a PNG. `ui.Div()` starts a flex container,
`.Col()` / `.Row()` set its direction, `.P` / `.Gap` / `.Size` / `.Rounded`
/ `.Bg` style it, `.OnClick` attaches a handler, `ui.Text` is a text leaf
and `ui.RGB` makes a colour from `0xRRGGBB`.

## Example catalogue

Every directory under `examples/`. "Headless" lists the deterministic flags
each one takes: `-screenshot out.png` renders offscreen, `-frames N` exits
after N frames.

| Directory | What it shows | Headless |
|---|---|---|
| `actor` | A binary-FBX character with a separately shipped FBX clip retargeted onto it (`-mesh`, `-anim`; bring your own Mixamo export). | `-screenshot`, `-frames` |
| `actorblend` | Skeletal animation: five retargeted clips crossfaded per joint (keys 1-5), a rifle attached to a hand joint. | `-screenshot`, `-frames` |
| `anchors` | A `.gml` layout built entirely with QML anchors, hot-reloaded on save. | `-screenshot`, `-frames` |
| `animation` | The core animation engine: a spring toggle, a ping-pong pulse, an eased progress bar. | `-screenshot`, `-frames` |
| `counter` | The smallest UI demo: one property, two buttons; native and web from one `main`. | `-frames` |
| `cubes` | The first 3D demo: a spinning parent cube with orbiting children through the scene tree, depth-tested and lit. Builds for Android too. | `-screenshot`, `-frames` |
| `devcheck` | The on-device smoke test: the triangle plus adapter limits, a clipboard round trip and an audio tone, printing PASS/FAIL lines (`adb logcat -s gfx` on Android). | `-frames` |
| `elements` | QML `MouseArea`, `Grid` and `Flickable` composed with anchors and a `Repeater`, hot-reloaded. | `-screenshot`, `-frames` |
| `fps` | First-person walkthrough on the kinematic character controller: corridors, stairs, a ramp, a moving platform, a pressure-plate door. | `-screenshot`, `-frames` |
| `frost` | Offscreen layers: a `ui.Layer` rendered to its own texture and composited blurred (frosted glass), slider-driven. | `-screenshot`, `-frames` |
| `gallery` | A small widget showcase; the original headless-mode demo. | `-screenshot`, `-frames` |
| `gpui` | Ports of Zed's GPUI examples in Go for side-by-side comparison (`-example name`, `-list`). | `-screenshot`, `-frames` |
| `images` | The QML `Image` element: a lazy file source, fit modes, a missing-source placeholder. | `-screenshot`, `-frames` |
| `interact` | Keyboard focus with Tab and a focus ring, cursor shapes, drag-and-drop. | `-screenshot`, `-frames` |
| `kitchensink` | Nearly everything the `ui` toolkit does on one scrolling page: bindings, flex, gradients, shadows, text input, a 10k-row virtualized list, canvas, SVG, overlays, animations. | `-screenshot`, `-frames` |
| `materials` | PBR material sets (base colour, normal, ARM) tiled plainly and with stochastic hex tiling, under point lights and fog. Needs textures (see the assets note above). | `-screenshot`, `-frames` |
| `model` | Loads and spins a glTF asset (`-file`; `Duck.glb` from `build/assets` by default). | `-screenshot`, `-frames` |
| `monitors` | Prints the display layout `app` sees; `-open N` opens a borderless fullscreen window on monitor N. | none (prints without a window) |
| `msdftext` | Multi-channel distance-field text beside the rasterised glyph atlas it replaces. | `-screenshot`, `-frames` |
| `overlays` | Wrapped paragraph text, an opacity-faded panel, a dropdown as a z-layered overlay. | `-screenshot`, `-frames` |
| `paint` | A gradient header, a `Canvas` star path, a virtualized 10k-row list. | `-screenshot`, `-frames` |
| `particles` | One million particles seeded through the ECS and integrated by a compute shader (`-n`; `go generate` emits its WGSL layout). | `-frames` |
| `partviewer` | A Shapr3D-style part viewer: a full-window 3D viewport under a floating 2D UI whose sliders regenerate the part; the first example compositing `Renderer3D` and `Renderer2D` in one frame. (Its doc comment still says `go run ./cad`; the directory is `partviewer`.) | `-screenshot`, `-frames` |
| `pdfsmoke` | Renders one PDF page to a PNG through the framework's renderer (`-page`, `-scale`, `-out`). | writes a PNG by design |
| `pong` | A minimal real-time game on the 2D renderer: delta-time loop, held keys, immediate-mode drawing. | `-screenshot` |
| `probe` | Acquires an adapter and device headlessly and prints them; proof that the no-cgo GPU path works. | prints without a window |
| `qml` | Runs any `.gml` document with hot reload (`-file`, `-w`, `-h`, `-script`); hosts the 32 GPUI ports under `qml/gpui/`. | `-screenshot`, `-frames` |
| `qmlgo` | Hand-written Go ports of two QML corpus pages (`qmlgo/buttons`, `qmlgo/sliders`) over shared `qmlgo/corpus` controls, used by the QML comparison scripts. | none |
| `scripting` | The JS-subset engine: function declarations, loops and `Math` builtins driving live bindings. | `-screenshot`, `-frames` |
| `settings` | The phase-1 exit demo: a settings panel with toggles, sliders, text fields and a scrollable body; native, web and headless. | `-screenshot`, `-frames` |
| `sink2d` | The 2D kitchen sink twice: the Go builder API and the `.gml` document (`-gml`), for side-by-side comparison. | `-screenshot`, `-frames` |
| `skinned` | A skinned, animated glTF character (CesiumMan) through `render.AnimationPlayer`. | `-screenshot`, `-frames` |
| `states` | QML states and transitions in `.gml`, hot-reloaded. | `-screenshot`, `-frames` |
| `timer` | The QML `Timer` element and custom signals. | `-screenshot`, `-frames` |
| `triangle` | One triangle through the full stack; the phase-0 example that runs native, web and Android from one `go build`. | `-frames` |
| `types` | The `.gml` type system: `color` properties, `var` holding parsed JSON, `id.property` references. | `-screenshot`, `-frames` |
| `viewport3d` | `ui.Viewport3D`: a live 3D scene embedded in a 2D layout as an ordinary element, with ray picking. | `-screenshot`, `-frames` |
| `xwarp` | Moves the X11 pointer to a root-window point and exits; a Linux-only dev tool for the QML comparison harness. | none |
| `internal/orbit` | Not an example: the drag-to-orbit camera the 3D examples share. | |
