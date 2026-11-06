# Platforms

gfx targets Linux, Windows, the web (WebAssembly) and Android from one
`go build`, with no C toolchain except for Android packaging. The `app`
package owns windows, input and lifecycle per platform; `gpu/plat` makes a
WebGPU surface from an `app.Window`; everything above those two layers is
platform-independent. This page says what each backend is made of, what it
has been verified on, and which `app.Window` methods are stubs where.

| Platform | Window and input | GPU | Source files | Status |
|---|---|---|---|---|
| Linux | X11 via `ffi` (purego) | wgpu-native, Vulkan | `app/x11_linux.go`, `app/monitor_x11_linux.go`, `app/clipboard_x11_linux.go`, `app/gamepad_linux.go`, `gpu/plat/plat_linux.go` | Development platform; everything is exercised here |
| Windows | Win32 via stdlib `syscall` + purego | wgpu-native, D3D12 by default | `app/win32_windows.go`, `app/monitor_windows.go`, `app/gamepad_windows.go`, `gpu/plat/plat_windows.go`, `gpu/wgpu_abi_windows.go` | Headless rendering verified on Windows 11; windowed use less exercised (below) |
| Web | `<canvas>` + DOM events via `syscall/js` | the browser's `navigator.gpu` | `app/web_js.go`, `app/monitor_js.go`, `app/gamepad_js.go`, `gpu/api_js.go`, `gpu/plat/plat_js.go` | Builds and vets on every `check.sh` run; on-screen checks are manual |
| Android | `NativeActivity` + a small cgo glue | wgpu-native cross-compiled from source, Vulkan | `app/android.go`, `app/android_glue.c`, `app/android_glue.h`, `app/asset_android.go`, `app/sensor_android.go`, `gpu/plat/plat_android.go` | Verified on an Adreno 740 phone (Android 16) and the API 36 x86_64 emulator |
| Headless | virtual window, synthetic input | offscreen wgpu-native | `app/headless.go` | See [headless-and-ci.md](headless-and-ci.md) |
| macOS / iOS | none | (wgpu-native's Metal backend would cover rendering) | | Not supported; `fetch-wgpu.sh` can download the dylib but there is no `app` backend |

`app.Host` (`app/headless.go`) is the interface every backend provides:
`Poll`, `Size`, `Scale`, `MouseEvents`, `KeyEvents`, `EachFrame`,
`SetCursor`, `Destroy`. `*app.Window` implements it on all four platforms and
`*app.Headless` implements it with no display.

## What every backend has, and where the stubs are

`app.Window` carries the same method set on every platform, so code above
`app` compiles everywhere; where a feature does not exist the method is a
no-op or returns an error.

| Feature | X11 | Win32 | Web | Android |
|---|---|---|---|---|
| `NewChild` (second window) | Shares the parent's display connection; only the primary may be polled, and `NewChild` on a non-primary window returns an error | Owned top-level window (dialog semantics); `Positioned`/`Fullscreen` children are independent top-levels | Error: `multi-window is not implemented on web` | Error: `multi-window is not implemented on android` |
| `Hide` / `Show` | Withdraw / map the window | `SW_HIDE` / `SW_SHOW` | Sets the canvas `display` style | No-ops |
| `SetTitle` | Yes | Yes | Yes | No-op (the activity owns the title) |
| `Monitors()`, `SetFullscreen`, `MonitorIndex` | XRandR | `EnumDisplayMonitors` | `window.screen`; `requestFullscreen` is only granted from a user-gesture handler, so a call from frame code is silently rejected | The activity surface; `SetFullscreen` is a no-op and `MonitorIndex` is 0 |
| `RetainDisplay` | Keeps the X connection open after `Destroy` (the Vulkan ICD hooks it) | No-op | No-op | No-op |
| `Options.Transparent` (per-pixel alpha) | ARGB visual; needs a compositor | Accepted, not implemented | Accepted, not implemented | Accepted, not implemented |
| `Options.Hidden`, `Unmovable`, `Unresizable`, `Unminimizable` | Accepted | Implemented | Accepted (the browser owns the canvas) | Accepted |
| `Scale()` (DPI) | `Xft.dpi` at creation | Per-monitor DPI | `devicePixelRatio` | 1 |
| `ClipboardText` / `SetClipboardText` | X11 selections with INCR | Win32 clipboard | `navigator.clipboard` plus the `paste` event (compile-checked, not verified in a browser) | `ClipboardManager` over JNI (verified on a phone) |
| Gamepad | joydev (`/dev/input/jsN`) | XInput, with a WinMM fallback for DirectInput-only sticks | `navigator.getGamepads()` | None yet |
| Touch (`TouchEvents`, `app.Gestures`) | None | None (`WM_TOUCH` not wired) | Multi-touch | Multi-touch |
| `app.Accelerometer` / `app.Orientation` | Stub (`ok=false`) | Stub | Stub | Real |
| `app.Asset` / `app.DataDir` | Error / `""` | Error / `""` | Error / `""` | APK assets copied into the data directory on demand / the activity's data path |
| Audio (`audio/`) | ALSA via dlopen | winmm waveOut | Web Audio (compile-checked only) | AAudio (verified on a phone) |

GPU readback (`Device.ReadTexture`, `Device.ReadBuffer`, in
`gpu/resources_native.go` and `gpu/compute_native.go`) exists only on native
builds; the web has no equivalent, so every screenshot path returns an error
there (`ui/shell/screenshot_js.go`, `ui/shell/game_screenshot_js.go`).

## Linux

The window is X11 through purego (`app/x11_linux.go`): it works on every
Linux desktop including Wayland sessions through XWayland. Native Wayland is
not implemented; the `gpu` package only has the surface type constant for it,
and DESIGN.md lists Wayland under later work. The build tag is `!android`
rather than `linux` because `GOOS=android` also sets the `linux` tag.

Each `app.NewWindow` opens its own display connection with its own event
queue, so two top-level windows never cross-route; `NewChild` shares the
primary's connection instead and is routed by `XAnyEvent.window`. `Monitors()`
uses XRandR; `Options.X`/`Y` plus `Positioned` and `Fullscreen` place a
borderless window on a chosen head (what a multi-projector installation needs,
`examples/monitors -open N`). Transparent (ARGB) windows need a running
compositor. DPI comes from `Xft.dpi`; `GFX_SCALE` overrides it.

wgpu-native uses Vulkan here; a machine without a GPU can render headlessly
through lavapipe (Mesa's software Vulkan). `scripts/check.sh`,
`scripts/bounded.sh` and the QML comparison scripts are written for this
platform.

## Windows

Win32 windowing goes through stdlib `syscall` DLL loading and purego
function binding (`app/win32_windows.go`); the layout-sensitive structs
(`WNDCLASSEXW`, `MSG`) are kept field-for-field with the headers. Because
`PeekMessageW` only returns messages for windows created by the calling
thread, the package locks the main goroutine to its OS thread in `init`.
Messages are per thread, so two windows on the same goroutine share one pump:
polling either window services both.

GPU defaults differ from Linux: `GFX_WGPU_BACKEND` defaults to `dx12` (the
Vulkan swapchain path was observed blocking ~500 ms per frame on hybrid
laptops, and DX12 presents through DWM like every browser), and the DX12
swapchain is a DirectComposition visual (`GFX_WGPU_DX12_SWAPCHAIN=visual`)
so interactive resizes do not show the previous frame stretched. Set
`GFX_WGPU_BACKEND=vulkan` or `GFX_WGPU_DX12_SWAPCHAIN=hwnd` to experiment.

The ABI note: purego cannot pass struct arguments by value on Windows. The
three wgpu request functions (`RequestAdapter`, `RequestDevice`,
`BufferMapAsync`) take a 40-byte callback-info struct by value, and the Win64
convention passes any by-value struct larger than 8 bytes as a pointer to a
caller-made copy, so `gpu/wgpu_abi_windows.go` binds them as pointer-argument
functions and passes `&callbackInfo`. The SysV platforms use their own
trampoline instead (`gpu/wgpu_abi_other.go`, per-architecture files). The
`SetViewport` float signature stays on `purego.RegisterFunc` on Windows.

Status: the Win32 backend was developed on Linux. While writing this
page the headless path (`settings`, `kitchensink`, `cubes`, `qml` with
`-screenshot`, and `probe`) was run on Windows 11 with Go 1.27 and
wgpu-native 29.0.0.0 on D3D12 and produced correct PNGs, which also exercises
the by-pointer request calls. Windowed runs, input, DPI scaling (the only
backend reporting scales other than 1), the DirectComposition swapchain and
XInput/WinMM gamepads were not exercised for this page. Windows-only
`Options`: `Hidden` (create unshown, `Show` after the first present),
`Unmovable`, `Unresizable`, `Unminimizable`. `scripts/check.sh` runs from Git
Bash.

## Web (WebAssembly)

The web target is `GOOS=js GOARCH=wasm`. `app/web_js.go` wraps a `<canvas>`:
if the host page has an element with id `gfx-canvas` it is used, otherwise a
canvas is appended to `<body>`. `gpu/api_js.go` calls `navigator.gpu`
directly; there is no wgpu-native on the web (`plat.LibPaths()` returns nil).

`scripts/build-web.sh [example]` (default `triangle`) builds one example into
`examples/<name>/web/`:

1. copies `wasm_exec.js` from `$(go env GOROOT)/lib/wasm/`,
2. runs `GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o examples/<name>/web/main.wasm ./<name>` from `examples/`,
3. lists the directory.

The script does not write an `index.html`; `counter`, `cubes`, `kitchensink`,
`settings` and `triangle` ship one (`examples/counter/web/index.html` is the
template: a `<canvas id="gfx-canvas">`, a WebGPU availability check, then
`new Go()` and `WebAssembly.instantiateStreaming(fetch("main.wasm"), …)`).
Other examples need a copy of that page. Serve the directory over HTTP, not
`file://`, because `instantiateStreaming` fetches the module:

```bash
scripts/build-web.sh settings
cd examples/settings/web && python3 -m http.server 8000
# open http://localhost:8000/ in a browser with WebGPU enabled
```

Examples build for the web when their `main.go` build tag includes `js`
(`animation`, `counter`, `cubes`, `devcheck`, `frost`, `interact`,
`kitchensink`, `materials`, `model`, `msdftext`, `overlays`, `paint`,
`particles`, `pong`, `settings`, `sink2d`, `triangle`, and `qmlgo/*`).

What is different or stubbed on js:

- **No GPU readback**: `Device.ReadTexture` and `Device.ReadBuffer` do not
  exist, so `shell.Screenshot`, `shell.ScreenshotUI` and
  `shell.ScreenshotGame` return errors and `-screenshot` is native-only.
  Examples keep their screenshot code in `screenshot_native.go` /
  `screenshot_js.go` pairs.
- **No threads**: goroutines multiplex on one thread. Anything parallel in
  the framework must degrade to single-threaded as a scheduling policy, not a
  build variant; `shell` disables present-on-damage skipping on js.
- Dual-source blending (subpixel text) is not available on WebGPU, so text
  uses the grayscale path.
- `NewChild` returns an error; `SetFullscreen` only works from a user
  gesture; clipboard access goes through `navigator.clipboard` and the
  `paste` event and is compile-checked only.
- Touch is a full multi-touch stream; gamepads come from
  `navigator.getGamepads()`.
- Binary size: plain Go wasm starts at a few MB compressed; the script
  strips symbols (`-s -w`). TinyGo is kept as an option, not used.

`scripts/check.sh` builds and vets both modules for js/wasm on every run, so
the web target cannot silently rot; what it cannot do is look at the result,
so on-screen behaviour (and the 3D path in particular) is verified by hand in
a browser, not by CI.

## Android

Android is the one place the project uses cgo: `app/android_glue.c`
implements `ANativeActivity_onCreate`, owns the input and sensor loopers, and
runs the program's render loop on its own thread by calling the `//export
gfxAndroidMain` the program defines (see `examples/triangle/android.go`).
purego also needs cgo enabled on bionic. The window comes from the system,
so `app.Options` size and title are ignored; `app.NewWindow` blocks until the
activity hands over a surface, and Android 16 destroys and recreates the
first window during initial layout, so the backend waits for a live one.

Requirements (both scripts assume a Linux host: the NDK's
`prebuilt/linux-x86_64` toolchain path is hard-coded):

| Need | For |
|---|---|
| `ANDROID_HOME` (or `~/Android/Sdk`) with an NDK, build-tools and a platform | `aapt2`, `zipalign`, `apksigner`, `android.jar`, the NDK clang |
| rustup with the `aarch64-linux-android` and `x86_64-linux-android` targets, git | `scripts/build-wgpu-android.sh` compiles wgpu-native from source: upstream ships no Android prebuilt |
| `keytool` (a JDK) and `zip` | The debug keystore (`build/debug.keystore`, generated once) and adding native libraries to the APK |
| No Gradle, no Java sources | The manifest declares `android:hasCode="false"` and `android.app.NativeActivity` |

Build:

```bash
ABIS="arm64-v8a x86_64" scripts/build-wgpu-android.sh   # once; -> build/wgpu/android/<abi>/libwgpu_native.so
scripts/build-apk.sh triangle arm64-v8a                  # a phone (abi x86_64 = the emulator, the default)
adb install build/apk/triangle/triangle.apk
adb logcat -s gfx                                        # stdout/stderr of the program
```

`build-wgpu-android.sh` clones the pinned tag (`v29.0.0.0`, the same the
desktop fetches) into `/tmp/wgpu-native-v29.0.0.0` (`WGPU_SRC` overrides),
adds the Rust targets to the toolchain the checkout pins, and builds release
libraries for each ABI in `ABIS` (default both). `build-apk.sh <name> [abi]`
then compiles the Go program with `CGO_ENABLED=1 GOOS=android` and the NDK
clang in `-buildmode=c-shared`, writes an `AndroidManifest.xml`, links with
`aapt2`, adds `lib/<abi>/lib<name>.so` and `libwgpu_native.so`, zipaligns and
signs with the debug key. Output: `build/apk/<name>/<name>.apk`, package
`io.github.sushvibez.gfx.<name>` (the package id is hard-coded in the
script and does not follow the Go module path). Minimum SDK 29 (Vulkan on
every device, the one decided floor), target SDK 34.

| Variable | Meaning | Default |
|---|---|---|
| `GFX_APK_MODULE` | Directory of the module containing the program, relative to the repository root. | `examples` |
| `GFX_APK_PKG` | Package path to build inside that module. | `./<name>` |
| `GFX_APK_ORIENTATION` | `android:screenOrientation` of the activity (`landscape` for a game). | `unspecified` |
| `GFX_APK_ASSETS` | A directory packed into the APK's `assets/`; `app.Asset(name)` copies one out into the data directory on first use. | none |
| `GFX_LDFLAGS` | Linker flags for the Go build. | `-s -w` |

Examples with an Android entry point (`android.go`): `triangle`, `cubes`,
`devcheck`. `materials` also carries the `android` build tag. `devcheck` is
the on-device smoke test: it draws the triangle and prints PASS/FAIL lines for
the adapter limits, a clipboard round trip and an audio tone to logcat.

Verified (2026-10-15, per DESIGN.md §6 and §8): on a OnePlus phone (Adreno
740, Android 16) `devcheck` passes every line (window, surface, device, JNI
clipboard, AAudio, 600 frames without device errors), and the `ffi` tests run
on the device itself. On the API 36 x86_64 emulator with `-gpu host`, device,
clipboard and audio pass but the first queue submit dies inside the emulator's
gfxstream Vulkan driver, and the SwiftShader image fails at `vkCreateDevice`;
both are emulator-side. Known limitations: there is no gamepad backend; a
locked screen destroys the activity's window and nothing recreates the GPU
surface, so the backend exits the process on window destruction rather than
show a black activity (the launcher restarts it); the soft keyboard is not
wired; touch is delivered both as `TouchEvents` and folded into the mouse
stream.
