# Headless rendering and CI

Headless mode is a standard backend, not a build variant: any program built
on the framework can render offscreen, take synthetic input and be stepped one
frame at a time, so CI and automated agents can drive it and look at the
pixels. It needs the wgpu-native library (`scripts/fetch-wgpu.sh`) but no
display server, window system or GPU: a software adapter (lavapipe on Linux,
the D3D12 software adapter on Windows) is enough. The null-GPU variant
DESIGN.md §8.1 describes, where the `gpu` package accepts every call and draws
nothing, does not exist yet.

There are two headless paths in the tree, used for different jobs.

## The headless host: `app.Headless`

`app/headless.go` implements `app.Host`, the interface every windowing backend
provides, with no display at all:

| Method | What it does |
|---|---|
| `app.NewHeadless(app.Options)` | Creates a host of `Width` x `Height` (800 x 600 when zero). `X`/`Y`/`Positioned`/`Fullscreen` are recorded so tests can assert on placement. |
| `Step(fn func() bool) bool` | Runs exactly one frame; `false` once `Close` was called. |
| `EachFrame(fn func() bool)` | Runs frames back to back with no pacing until `fn` returns `false` or the host closes. |
| `InjectMouse(events ...app.MouseEvent)`, `InjectKey(events ...app.KeyEvent)` | Queue synthetic events; the next `MouseEvents()` / `KeyEvents()` drain returns them, exactly the stream a real backend produces. |
| `Click(x, y float32)` | A primary-button press and release at a point. |
| `Type(s string)` | A key-down per rune. |
| `Resize(w, h int)`, `Close()`, `Destroy()` | Change the reported size; request shutdown. |
| `Monitors()`, `Position()`, `Fullscreen()`, `SetFullscreen`, `MonitorIndex()` | One virtual monitor the size of the host, so monitor-aware code runs without a display. |
| `Size()`, `Scale()` (always 1), `SetCursor` (no-op), `Poll()` | The rest of `app.Host`. |

Pixels come from pairing the host with offscreen GPU rendering: create an
instance with `gpu.NewInstance(plat.LibPaths()...)`, request an adapter with
`inst.RequestAdapter(nil)` (no surface), a device, then
`device.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)` as the frame target
and `device.ReadTexture(queue, target, w, h)` to read the RGBA bytes back.
`ui/headless_test.go` (`TestHeadlessHostDrivesApp`) is the end-to-end
example: it builds a `ui.UI` over a headless host, clicks a button and types
into a field through `InjectMouse`/`InjectKey`, steps one frame at a time,
and asserts on both the view state and the pixels read back.
`app/monitor_test.go` uses the host without any GPU to check that placement
options survive the trip.

## The screenshot runners: `ui/shell`

The examples do not go through `app.Headless`; the shell harness renders the
view straight into an offscreen texture (`ui/shell/screenshot.go`):

| Function | Use |
|---|---|
| `shell.ScreenshotUI(o Options, view func() ui.Element, path string)` | A plain view, the headless counterpart of `shell.RunUI`. |
| `shell.Screenshot(o Options, hooks Hooks, path string)` | With hooks (`Init`, `FrameStart`, `Encode3D` for offscreen 3D viewports); two frames are rendered when `Encode3D` is set so viewports get laid out first. |
| `shell.ScreenshotScript(o, hooks, step func(i int, u *ui.UI) (path string, more bool))` | A sequence of PNGs with input injected between them through `ui.UI.Click` / `Drag` / `Fling` / `Key`. |
| `shell.RunScript(o, hooks, spec, base string)` | The same driven by a text script (`spec` is the text or `@file`); step N writes `<base>-NN-<step>.png`. |
| `shell.ScreenshotGame(o, steps int, dt float64, update func(*Frame) bool, prep func(step int, in *Input), path)` | The game-loop sibling (`shell.RunGame` apps): step `steps` times at a fixed `dt`, script held keys through `prep`, write the last frame. |

How a capture is taken: the core clock is switched to a virtual clock
(`core.SetVirtualClock`) so animation time advances only in the settle loop,
never with wall time; pure-2D captures render with 4x MSAA
(`render.New2DMSAA`), 3D captures without. The frame is then advanced in
60 fps steps until no property revision or animation moves, up to a budget of
2.5 s of animation time (`GFX_SETTLE_MS`), so a page that is still tweening is
sampled at a known moment and a static page exits after a couple of frames.
The target is read back with `Device.ReadTexture` and written as PNG. Fonts
resolve exactly as in a window (`GFX_FONT` and friends apply).

Interaction scripts (`ui/shell/script.go`), one step per line, `#` comments:

```
click 120,60          press and release at a point
press 120,60          press and hold
move 200,60           move the pointer (hover states, drag tracking)
release 200,60        let go
drag 40,80 200,80     press-drag-release
fling 40,80 40,-60    flick with velocity (list momentum)
wheel 200,300 -120    scroll; a delta of 120 is one notch
key Enter             a named key, optionally ctrl+/shift+/alt+ prefixed
type hello            each rune as a key event
wait                  no input, just settle and capture
```

Each step advances a fixed 700 ms of animation time (`shell.ScriptStepSettle`)
before its capture, so an animation a step starts is sampled at the same
moment on every run. The runner is built for `linux || windows`.

Readback is native-only. On js the same functions exist but return an error
(`ui/shell/screenshot_js.go`, `ui/shell/game_screenshot_js.go`), so a
`-screenshot` run must be a native build.

## `-screenshot` in the examples

By convention an example registers `-screenshot out.png` and `-frames N`
and, when it needs anything beyond `ScreenshotUI`, keeps its screenshot code
in a `screenshot_native.go` / `screenshot_js.go` pair so the js build stays
green (`examples/kitchensink`, `settings`, `cubes`, `model`, `materials`).
`examples/cubes/screenshot_native.go` shows the fully manual version for a 3D
scene: instance, adapter, device, a colour and a depth texture, one
`render.Renderer3D` frame, `ReadTexture`, `png.Encode`. The `qml` example adds
`-script` (a text script or `@file`) for a scripted pass and `-w`/`-h` for
the size.

```bash
cd examples
go run ./settings -screenshot out.png
go run ./qml -file qml/gpui/hello_world.gml -w 600 -h 400 -screenshot out.png
go run ./qml -file qml/gpui/popover.gml -screenshot shots.png -script @steps.txt
go run ./cubes -screenshot out.png
go run ./kitchensink -frames 120      # windowed, but exits deterministically
```

The catalogue in [getting-started.md](getting-started.md) lists which flags
each example takes. `counter`, `triangle`, `particles` and `devcheck` only
have `-frames`.

## Tests

Tests that need a GPU find wgpu-native the same way the examples do: a
`wgpuLib(t)` helper (`ui/ui_test.go`, `ecs/gpu_test.go`, the render tests)
returns `GFX_WGPU_LIB` or `../build/wgpu/<name>` and calls `t.Skip` when
neither exists, so `go test ./...` passes on a machine without the library
and simply covers less. With the library present the suite renders for real:
`ui/headless_test.go` drives an app through the headless host,
`ecs/gpu_test.go` round-trips a column through a compute shader,
`render/renderer2d_test.go` and `render/gpuskin_test.go` draw and read back.
Tests needing optional assets (`GFX_FBX_ASSETS`, `GFX_QML_CORPUS`, the glTF
samples from `scripts/fetch-assets.sh`) skip the same way.

```bash
go test ./ui/...                       # one package tree
go test -run TestHeadlessHostDrivesApp ./ui/
cd examples && go test ./...           # the examples module
```

## `scripts/check.sh`

The full verification sweep; CI runs it, and it is the gate before a commit.
It runs from the repository root in bash (Git Bash on Windows) and stops at
the first failing stage:

| Stage | What it does | Fails when |
|---|---|---|
| `gofmt` | `gofmt -l .` minus `build/` | Any file is unformatted |
| build+vet | `go build ./... && go vet ./...` for the root module and `examples/`, for `GOOS=linux`, `GOOS=windows` and `GOOS=js GOARCH=wasm` | Any platform does not build or vet: platform code must live behind build tags, `app.Window` methods must exist on every backend, and js needs stubs for readback |
| layering | `go test ./internal/layering/...` | A package imports a layer of equal or higher rank (`core`/`mem`/`ffi` 0, `app`/`physics`/`audio` 1, `gpu` 2, `ecs`/`render` 3, `scene`/`ui` 4, `md` 5, `script`/`markdown` 6), or a top-level directory is missing from the rank table |
| tests | `go test -count=1 -timeout 300s ./...` in both modules, output filtered of `no test files` but exit status kept | Any package fails |
| screenshot smoke | `go run ./settings`, `./kitchensink` and `./frost -screenshot` into a temp dir; `frost` covers the offscreen-layer path (a second render pass) | A PNG is missing or empty. Skipped with a message when neither `build/wgpu/libwgpu_native.so`, `build/wgpu/wgpu_native.dll` nor `GFX_WGPU_LIB` exists |

The layering test asks `go list` for every package's imports, so it covers
test imports too; a new top-level directory has to be added to the `ranks`
table in `internal/layering/layering_test.go` or the test fails.

## The call counter: `GFX_CALLS=1`

`gpu/stats.go` keeps atomic counters of what a frame hands to wgpu: draws,
bind-group sets, pipeline sets, scissor sets, vertex-buffer sets, command
encoders and render passes (`gpu.CallStats`), and buffer/texture upload calls
and bytes (`gpu.UploadStats`, cleared with `gpu.ResetUploadStats`). They are
always on; two atomic adds per call. With `GFX_CALLS=1` the counts are
printed to stderr once a second from `Queue.Submit`, averaged per submit:

```
gpu: per submit: 14.0 draws, 9.0 bind groups, 5.0 pipelines, 3.0 scissors, 6.0 vertex bufs, 1.0 encoders, 2.0 passes | 7.0 buffer uploads (120 KB), 0.0 texture uploads (0 KB) | 60 submits in 1s
```

A run shorter than that second still reports: `Device.Release` and the
shell's screenshot paths flush the partial window (`gpu.FlushCallTrace`,
which a program that exits without releasing its device can call itself).
The first submit starts the window and is not counted. For apps running through
`ui/shell`, `GFX_FPS=1` prints a richer per-second line (presented frames,
loop passes, damage culling, measure/layout counts and the same upload
figures) and, with `MaxFrames` set, the process CPU time at exit.
`GFX_UPLOAD_TIMING=1` adds upload durations (`gpu.UploadNanos`) and
`GFX_UPLOAD_TRACE=1` logs individual slow buffer writes.

## Bounded runs: `scripts/bounded.sh`

A runaway build, test or render can take a desktop down (a full `check.sh`
is a dozen parallel compiles and test binaries plus a GPU smoke).
`scripts/bounded.sh <command...>` runs the command inside a memory- and
CPU-fenced cgroup:

```bash
scripts/bounded.sh scripts/check.sh
BOUND_MEM=3G BOUND_CPU=400% scripts/bounded.sh go test ./...
BOUND_TIMES=1 scripts/bounded.sh go run ./settings -frames 600   # prints user/sys CPU time at exit
```

| Knob | Meaning | Default |
|---|---|---|
| `BOUND_MEM` | `MemoryMax` of the whole process tree; `MemorySwapMax=0`, so the cap is hit by an OOM-kill of this tree, never by paging the desktop out | `6G` |
| `BOUND_CPU` | `CPUQuota` (600% = six cores) | `600%` |
| `GOFLAGS`, `GOMAXPROCS` | Exported to cap parallel compiles and test processes | `-p=4`, `6` |
| `BOUND_TIMES` | Any value: print the command's CPU time on exit | unset |

It needs a user systemd (`systemd-run --user --scope`) and so only fences on
Linux; elsewhere it prints a notice and falls back to `nice -n 10` plus the
Go knobs. Whatever the platform, keep automated runs bounded: give every
windowed run a `-frames N`, prefer `-screenshot` where a picture is the goal,
and keep `go test` under its timeout (`check.sh` uses 300 s per module).
