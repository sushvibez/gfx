// Package app owns the platform layer: window/surface creation, the input
// event stream, and lifecycle. High-DPI handling and lifecycle events
// (suspend/resume on Android, visibilitychange on web) are still to come.
//
// Per-platform status (see /DESIGN.md §8):
//   - Windows: Win32 via stdlib syscall; D3D12 through wgpu-native, verified
//     on Windows 11 (headless and windowed).
//   - Linux: X11 via ffi/purego; native Wayland follows later. X11 is the
//     only backend with multi-window (NewChild/Hide/Show) so far.
//   - Android: NativeActivity via a small cgo glue (the one sanctioned
//     cgo exception); touch mapped onto the mouse stream, soft keyboard
//     later.
//   - Web: canvas element + DOM and touch events via syscall/js.
//   - Headless (Godot-style, /DESIGN.md §8.1): no display needed —
//     offscreen GPU, synthetic input injection (InjectMouse/InjectKey,
//     Click/Type), single-frame stepping (Step) so CI and AI agents can
//     drive and screenshot any app. The null-GPU variant is not built yet,
//     so headless still needs the wgpu-native library.
//
// # Monitors and multi-window
//
// Monitors() reports the virtual-desktop layout (XRandR on X11,
// EnumDisplayMonitors on Win32, window.screen on the web, the activity
// surface on Android) without needing an open window, and Options.X/Y +
// Positioned and Options.Fullscreen place a new window on a chosen head —
// what a multi-projector installation needs: one borderless fullscreen
// window per projector. Window.SetFullscreen/MonitorIndex cover the same
// ground after creation.
//
// Two coexisting top-level windows work differently on the two desktop
// backends, and both are supported:
//
//   - X11: NewWindow opens its OWN display connection per window, so two
//     top-level windows have two event queues and each Poll drains only its
//     own — no cross-routing is possible. NewChild is the other shape: it
//     SHARES the parent's connection, and there the primary's Poll routes by
//     XAnyEvent.window to itself or a sibling. Only the primary may be
//     polled in that arrangement. Use NewWindow per projector (independent
//     loops, independent lifetimes), NewChild for dialogs owned by a
//     main window.
//   - Win32: messages are per-THREAD, not per-window. Poll calls
//     PeekMessageW with a nil HWND filter, draining the whole thread queue,
//     and the shared WndProc routes each message to its *Window through the
//     hwnd map. Two windows on the same (LockOSThread'd) goroutine therefore
//     share one pump: polling EITHER window services BOTH, which is correct
//     but means a window whose Poll is never called still gets its events.
//
// The package delivers a uniform event stream into the core event loop; no
// platform types leak above this layer.
package app
