//go:build android

package app

/*
#cgo LDFLAGS: -landroid -llog
#include <stdlib.h>
#include "android_glue.h"
*/
import "C"

import (
	"fmt"
	"os"
	"time"
	"unicode/utf8"
	"unsafe"
)

// Android backend (DESIGN.md §8): the window comes from the system via
// NativeActivity, not from us. The C glue (android_glue.c) owns
// ANativeActivity_onCreate and runs the example's render loop on a
// dedicated thread; the example exposes that loop as an //export
// gfxAndroidMain that the glue calls.

// Options configures a new window. On Android size comes from the system,
// so only Title is meaningful (and unused for now).
type Options struct {
	Title  string
	Width  int
	Height int
	// Undecorated is accepted for API parity; this backend has no OS
	// window decorations to remove.
	Undecorated bool
	// Transparent requests a per-pixel-alpha window; only the X11 backend
	// implements it (accepted here for API parity).
	Transparent bool
	// Hidden creates the window without showing it (call Show after the
	// first present); accepted for API parity, the system shows the window.
	Hidden bool
	// Unmovable / Unresizable are accepted for API parity; Android windows
	// are fullscreen surfaces.
	Unmovable     bool
	Unresizable   bool
	Unminimizable bool
	// X, Y and Positioned request a virtual-desktop position; Android has no desktop, so they are accepted for API parity only.
	X, Y       int
	Positioned bool
	// Fullscreen requests a borderless window covering the target monitor;
	// an Android surface is always fullscreen already.
	Fullscreen bool
}

// Window wraps the system ANativeWindow.
type Window struct {
	width  int
	height int
	scale  float32 // density / 160, cached by Scale
	closed bool
	// OnCloseRequest, when set, runs when the user asks the window manager
	// to close the window (the title bar's ×, Alt+F4, WM_DELETE_WINDOW);
	// returning false keeps the window open (an unsaved-changes dialog).
	// Close() always closes.
	OnCloseRequest func() bool
	mouse          []MouseEvent
	keys           []KeyEvent
	touch          []TouchEvent

	// primaryID/touchDown map the first finger onto the mouse stream.
	primaryID int
	touchDown int
}

// DataDir is the app's private, writable directory (the activity's
// internalDataPath): where a game keeps its stores on a phone, since the
// repository paths the desktop uses do not exist. Also exported as
// GFX_DATA_DIR for packages that resolve paths without importing app.
func DataDir() string { return C.GoString(C.gfx_data_dir()) }

// NewWindow blocks until the system hands us a surface, then wraps it.
func NewWindow(o Options) (*Window, error) {
	if C.gfx_wait_window() == 0 {
		return nil, fmt.Errorf("app: activity destroyed before a window appeared")
	}
	if d := DataDir(); d != "" {
		os.Setenv("GFX_DATA_DIR", d)
	}
	w := &Window{}
	w.updateSize()
	return w, nil
}

func (w *Window) updateSize() {
	var cw, ch C.int
	C.gfx_window_size(&cw, &ch)
	w.width, w.height = int(cw), int(ch)
	// Remembered for Monitors(), which has no window to ask (see
	// monitor_android.go).
	androidScreenW, androidScreenH = w.width, w.height
}

// androidScreenW/H are the last known surface size — the device screen, and
// the whole of the "monitor layout" this platform has.
var androidScreenW, androidScreenH int

// AndroidWindow returns the ANativeWindow* for GPU surface creation.
// AndroidWindow returns the ANativeWindow the surface is made on. It
// waits for one: on Android 16 the activity's first window is destroyed
// and recreated during its initial layout, so the window NewWindow woke
// up on can already be gone by the time the surface glue asks (a null
// window took the Vulkan loader down inside vkCreateAndroidSurfaceKHR).
// nil only once the activity itself is destroyed.
func (w *Window) AndroidWindow() unsafe.Pointer {
	if C.gfx_wait_window() == 0 {
		return nil
	}
	return unsafe.Pointer(C.gfx_window())
}

// Poll drains queued window and input events.
func (w *Window) Poll() bool {
	if w.closed {
		return false
	}
	var ev C.gfx_event
	for {
		C.gfx_poll_event(&ev)
		switch ev.kind {
		case C.GFX_EV_NONE:
			return !w.closed
		case C.GFX_EV_WINDOW_RESIZED:
			w.updateSize()
		case C.GFX_EV_WINDOW_DESTROYED:
			// The screen locked or the app went to the background: the
			// surface is gone and nothing here recreates it (the GPU
			// surface belongs to the window that was destroyed). Rather
			// than leave a black activity behind for the player to find,
			// end the process; the launcher starts it fresh. Surface
			// re-creation on resume is the real fix (TODO.md).
			fmt.Fprintln(os.Stderr, "app: window destroyed; exiting (no surface re-creation yet)")
			os.Exit(0)
		case C.GFX_EV_DESTROY:
			w.closed = true
		case C.GFX_EV_TOUCH_DOWN:
			w.touch = append(w.touch, TouchEvent{ID: int(ev.id), Phase: TouchBegin, X: float32(ev.x), Y: float32(ev.y)})
			// The first finger down is the primary: it alone drives the
			// synthesized mouse stream so plain UIs keep working.
			if w.touchDown == 0 {
				w.primaryID = int(ev.id)
				w.mouse = append(w.mouse, MouseEvent{Kind: MouseDown, X: float32(ev.x), Y: float32(ev.y), Button: 1})
			}
			w.touchDown++
		case C.GFX_EV_TOUCH_MOVE:
			w.touch = append(w.touch, TouchEvent{ID: int(ev.id), Phase: TouchMove, X: float32(ev.x), Y: float32(ev.y)})
			if w.touchDown > 0 && int(ev.id) == w.primaryID {
				w.mouse = append(w.mouse, MouseEvent{Kind: MouseMove, X: float32(ev.x), Y: float32(ev.y), Button: 1})
			}
		case C.GFX_EV_TOUCH_UP:
			w.touch = append(w.touch, TouchEvent{ID: int(ev.id), Phase: TouchEnd, X: float32(ev.x), Y: float32(ev.y)})
			if w.touchDown > 0 {
				w.touchDown--
			}
			if int(ev.id) == w.primaryID {
				w.mouse = append(w.mouse, MouseEvent{Kind: MouseUp, X: float32(ev.x), Y: float32(ev.y), Button: 1})
			}
		case C.GFX_EV_TOUCH_CANCEL:
			w.touch = append(w.touch, TouchEvent{ID: int(ev.id), Phase: TouchCancel, X: float32(ev.x), Y: float32(ev.y)})
			if w.touchDown > 0 {
				w.touchDown--
			}
			if int(ev.id) == w.primaryID {
				w.mouse = append(w.mouse, MouseEvent{Kind: MouseUp, X: float32(ev.x), Y: float32(ev.y), Button: 1})
			}
		}
	}
}

// Size returns the current surface size in pixels.
func (w *Window) Size() (int, int) { return w.width, w.height }

// Scale is the DPI scale (physical px per logical px); 1 on this backend.
// Scale is the display's density relative to 160 dpi (Android's
// density-independent pixel), at least 1 — the same meaning as the X11
// and Win32 backends' DPI scale, so a HUD drawn in logical pixels comes
// out the same physical size on a phone. A screen laid out for a desktop
// window should cap it against its own design size (a 2.6× phone leaves
// 880×415 logical pixels of a 2292×1080 screen).
func (w *Window) Scale() float32 {
	if w.scale == 0 {
		w.scale = 1
		if d := int(C.gfx_density()); d > 0 {
			w.scale = max(1, float32(d)/160)
		}
	}
	return w.scale
}

// Resizing reports an interactive OS resize in progress; never on Android.
func (w *Window) Resizing() bool { return false }

// ClipboardText reads the system clipboard (ClipboardManager over JNI,
// android_glue.c). Android 10+ only serves the clip to the focused app;
// a refused or empty read falls back to the text this process last set,
// so copy and paste inside the app always work.
func ClipboardText() string {
	buf := make([]byte, 4096)
	n := int(C.gfx_clipboard_get((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))))
	if n >= len(buf) {
		buf = make([]byte, n+1)
		n = int(C.gfx_clipboard_get((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))))
	}
	if n < 0 || n >= len(buf) {
		return clipboardFallback
	}
	return decodeModifiedUTF8(buf[:n])
}

// SetClipboardText makes s the system clipboard's text.
func SetClipboardText(s string) {
	clipboardFallback = s
	cs := C.CString(s)
	C.gfx_clipboard_set(cs)
	C.free(unsafe.Pointer(cs))
}

// decodeModifiedUTF8 turns JNI's modified UTF-8 into UTF-8: an embedded
// NUL arrives as C0 80, and a character outside the BMP as a surrogate
// pair encoded as two 3-byte sequences. Plain text passes through as is.
func decodeModifiedUTF8(b []byte) string {
	plain := true
	for i := 0; i+1 < len(b); i++ {
		if b[i] == 0xC0 && b[i+1] == 0x80 || b[i] == 0xED && b[i+1]&0xE0 == 0xA0 {
			plain = false
			break
		}
	}
	if plain {
		return string(b)
	}
	out := make([]rune, 0, len(b))
	for i := 0; i < len(b); {
		if b[i] == 0xC0 && i+1 < len(b) && b[i+1] == 0x80 {
			out = append(out, 0)
			i += 2
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 3 && b[i] == 0xED {
			// A surrogate half: pair it with the next one.
			hi := rune(b[i]&0x0F)<<12 | rune(b[i+1]&0x3F)<<6 | rune(b[i+2]&0x3F)
			if i+5 < len(b) && b[i+3] == 0xED {
				lo := rune(b[i+3]&0x0F)<<12 | rune(b[i+4]&0x3F)<<6 | rune(b[i+5]&0x3F)
				out = append(out, (hi-0xD800)<<10+(lo-0xDC00)+0x10000)
				i += 6
				continue
			}
		}
		out = append(out, r)
		i += size
	}
	return string(out)
}

// TouchEvents returns and clears the touch events received since the last
// call — the full multi-touch stream (the primary finger is also mirrored
// onto MouseEvents).
func (w *Window) TouchEvents() []TouchEvent {
	ev := w.touch
	w.touch = nil
	return ev
}

// MouseEvents drains touch events mapped to the pointer stream.
func (w *Window) MouseEvents() []MouseEvent {
	ev := w.mouse
	w.mouse = nil
	return ev
}

// KeyEvents drains key events (none yet on Android; soft keyboard is later).
func (w *Window) KeyEvents() []KeyEvent {
	ev := w.keys
	w.keys = nil
	return ev
}

// EachFrame runs fn once per frame until fn returns false or the activity
// is destroyed. Pacing comes from the FIFO present inside fn.
func (w *Window) EachFrame(fn func() bool) {
	for w.Poll() {
		if !fn() {
			return
		}
	}
}

// SetCursor is a no-op on touch devices.
func (w *Window) SetCursor(Cursor) {}

// Destroy is a no-op: the system owns the window's lifetime.
// SetTitle does nothing: an Android activity has no title bar of its
// own to change.
func (w *Window) SetTitle(string) {}

func (w *Window) Destroy() { w.closed = true }

// Closed reports whether this window has been closed.
func (w *Window) Closed() bool { return w.closed }

// CloseChild is a no-op: NewChild always fails here (child windows are
// X11 and Windows only).
func (w *Window) CloseChild(*Window) {}

// Focused is always true on Android: a visible activity has focus.
func (w *Window) Focused() bool { return true }

// NewChild fails: Android has a single NativeActivity window (multi-window
// is X11 and Windows only).
func (w *Window) NewChild(o Options) (*Window, error) {
	return nil, fmt.Errorf("app: multi-window is not implemented on android")
}

// Hide and Show are no-ops: the system owns the window's visibility.
func (w *Window) Hide() {}

// Show is a no-op on Android.
func (w *Window) Show() {}

var _ Host = (*Window)(nil)

// SetPointerCapture is not available on Android (touch input has no cursor).
func (w *Window) SetPointerCapture(bool) {}

// PointerCaptured is always false on Android.
func (w *Window) PointerCaptured() bool { return false }

// Close, Minimize and ToggleMaximize exist for API parity with the desktop
// backends; the activity does not expose these window controls.
// TakeDamage always reports false on Android: surface loss/recreation is
// handled by the lifecycle, not per-frame damage events.
func (w *Window) TakeDamage() bool { return false }

// WaitEvent sleeps briefly; the Android backend has no pollable event fd
// wired yet, so idle loops keep the old short-poll pace.
func (w *Window) WaitEvent(time.Duration) { time.Sleep(4 * time.Millisecond) }

func (w *Window) Close()          {}
func (w *Window) Minimize()       {}
func (w *Window) ToggleMaximize() {}

// BeginMove and BeginResize exist for API parity; not applicable here.
func (w *Window) BeginMove()             {}
func (w *Window) BeginResize(ResizeEdge) {}

// RootPointer, Geometry and SetGeometry exist for API parity; the Android
// surface has no movable OS window.
func (w *Window) RootPointer() (int, int)        { return 0, 0 }
func (w *Window) Geometry() (int, int, int, int) { return 0, 0, w.width, w.height }
func (w *Window) SetGeometry(int, int, int, int) {}

// SetBackground is X11-only.
func (w *Window) SetBackground(uint32) {}

// RetainDisplay is X11-only (keeps the connection open after Destroy);
// a no-op here.
func (w *Window) RetainDisplay() {}
