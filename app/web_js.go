//go:build js

package app

import (
	"fmt"
	"syscall/js"
	"time"
)

// Web windowing: a Window wraps a <canvas>. If the host page provides an
// element with id "gfx-canvas" it is used (so pages control layout);
// otherwise a canvas is created and appended to <body>.

// Options configures a new window.
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
	// first present); the canvas starts with visibility: hidden.
	Hidden bool
	// Unmovable / Unresizable are accepted for API parity; the browser
	// owns canvas placement and sizing.
	Unmovable     bool
	Unresizable   bool
	Unminimizable bool
	// X, Y and Positioned request a virtual-desktop position; the browser places the page, so they are accepted for API parity only.
	X, Y       int
	Positioned bool
	// Fullscreen requests a borderless window covering the target monitor;
	// on the web this only means the canvas starts at screen size — actual
	// fullscreen needs a user gesture (see SetFullscreen).
	Fullscreen bool
}

// Window is a canvas plus the requestAnimationFrame pump.
type Window struct {
	canvas   js.Value
	width    int
	height   int
	mouse    []MouseEvent
	keys     []KeyEvent
	touch    []TouchEvent
	scale    float32 // devicePixelRatio at creation (or GFX_SCALE)
	cursor   Cursor
	captured bool
	closed   bool
	// OnCloseRequest, when set, runs when the user asks the window manager
	// to close the window (the title bar's ×, Alt+F4, WM_DELETE_WINDOW);
	// returning false keeps the window open (an unsaved-changes dialog).
	// Close() always closes.
	OnCloseRequest func() bool
	blurred        bool // window blur/focus events
}

// domKeys maps DOM KeyboardEvent.key names to Key values.
var domKeys = map[string]Key{
	"Backspace": KeyBackspace, "Delete": KeyDelete,
	"ArrowLeft": KeyLeft, "ArrowRight": KeyRight,
	"ArrowUp": KeyUp, "ArrowDown": KeyDown,
	"Home": KeyHome, "End": KeyEnd,
	"Enter": KeyEnter, "Tab": KeyTab, "Escape": KeyEscape,
}

func NewWindow(o Options) (*Window, error) {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return nil, fmt.Errorf("app: no DOM (not running in a browser?)")
	}
	if o.Width <= 0 {
		o.Width = 800
	}
	if o.Height <= 0 {
		o.Height = 600
	}
	canvas := doc.Call("getElementById", "gfx-canvas")
	if !canvas.Truthy() {
		canvas = doc.Call("createElement", "canvas")
		canvas.Set("id", "gfx-canvas")
		doc.Get("body").Call("appendChild", canvas)
	}
	// HiDPI: the backing store is cssSize x devicePixelRatio (physical px,
	// what Size() reports and the GPU surface configures) while the CSS box
	// stays at the logical size; DOM event coords are CSS px, i.e. already
	// logical. GFX_SCALE overrides for testing.
	dpr := scaleOverride()
	if dpr <= 0 {
		if v := js.Global().Get("devicePixelRatio"); v.Truthy() {
			dpr = float32(v.Float())
		}
	}
	if dpr <= 0 {
		dpr = 1
	}
	pw := int(float32(o.Width)*dpr + 0.5)
	ph := int(float32(o.Height)*dpr + 0.5)
	canvas.Set("width", pw)
	canvas.Set("height", ph)
	style := canvas.Get("style")
	style.Set("width", fmt.Sprintf("%dpx", o.Width))
	style.Set("height", fmt.Sprintf("%dpx", o.Height))
	if o.Title != "" {
		doc.Set("title", o.Title)
	}
	if o.Hidden {
		// Hidden until the first present (Show()) — parity with the native
		// backends: no blank canvas while GPU/font init runs. display:none
		// would zero the layout box, so hide by visibility.
		canvas.Get("style").Set("visibility", "hidden")
	}
	w := &Window{canvas: canvas, width: pw, height: ph, scale: dpr}
	listen := func(name string, kind MouseKind) {
		// Listeners live as long as the canvas; never released.
		canvas.Call("addEventListener", name, js.FuncOf(func(_ js.Value, args []js.Value) any {
			e := args[0]
			ev := MouseEvent{
				Kind:   kind,
				X:      float32(e.Get("offsetX").Float()),
				Y:      float32(e.Get("offsetY").Float()),
				Button: int(e.Get("button").Int()) + 1, // DOM 0-based → 1 = primary
				Shift:  e.Get("shiftKey").Bool(),
				Ctrl:   e.Get("ctrlKey").Bool(),
			}
			// Pointer Lock: movementX/Y are the relative deltas.
			if kind == MouseMove && w.captured {
				ev.DX = float32(e.Get("movementX").Float())
				ev.DY = float32(e.Get("movementY").Float())
				ev.X, ev.Y = float32(w.width)/w.Scale()/2, float32(w.height)/w.Scale()/2
			}
			w.mouse = append(w.mouse, ev)
			return nil
		}))
	}
	// Track Pointer Lock state (Esc releases it browser-side).
	doc.Call("addEventListener", "pointerlockchange", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		w.captured = doc.Get("pointerLockElement").Equal(canvas)
		return nil
	}))
	listen("mousedown", MouseDown)
	listen("mouseup", MouseUp)
	listen("mousemove", MouseMove)
	canvas.Call("addEventListener", "wheel", js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		w.mouse = append(w.mouse, MouseEvent{
			Kind:    MouseScroll,
			X:       float32(e.Get("offsetX").Float()),
			Y:       float32(e.Get("offsetY").Float()),
			ScrollY: float32(e.Get("deltaY").Float()),
			ScrollX: float32(e.Get("deltaX").Float()),
		})
		e.Call("preventDefault")
		return nil
	}))

	// Touch: full multi-touch stream. Single-finger defaults stay browser-
	// handled (the synthesized mouse events make taps click); with two or
	// more fingers we preventDefault so pinches drive our gestures instead
	// of zooming the page.
	touchListen := func(name string, phase TouchPhase) {
		canvas.Call("addEventListener", name, js.FuncOf(func(_ js.Value, args []js.Value) any {
			e := args[0]
			rect := canvas.Call("getBoundingClientRect")
			left, top := float32(rect.Get("left").Float()), float32(rect.Get("top").Float())
			changed := e.Get("changedTouches")
			for i := 0; i < changed.Length(); i++ {
				t := changed.Index(i)
				w.touch = append(w.touch, TouchEvent{
					ID:    t.Get("identifier").Int(),
					Phase: phase,
					X:     float32(t.Get("clientX").Float()) - left,
					Y:     float32(t.Get("clientY").Float()) - top,
				})
			}
			if e.Get("touches").Length() >= 2 {
				e.Call("preventDefault")
			}
			return nil
		}))
	}
	touchListen("touchstart", TouchBegin)
	touchListen("touchmove", TouchMove)
	touchListen("touchend", TouchEnd)
	touchListen("touchcancel", TouchCancel)

	keyListen := func(name string, down bool) {
		doc.Call("addEventListener", name, js.FuncOf(func(_ js.Value, args []js.Value) any {
			e := args[0]
			key := e.Get("key").String()
			ke := KeyEvent{Down: down, Key: domKeys[key],
				Shift: e.Get("shiftKey").Bool(), Ctrl: e.Get("ctrlKey").Bool()}
			if ke.Key == KeyNone && len([]rune(key)) == 1 {
				ke.Rune = []rune(key)[0]
			}
			if ke.Key != KeyNone || ke.Rune != 0 {
				w.keys = append(w.keys, ke)
				if !isClipboardChord(e) {
					e.Call("preventDefault") // keep Tab/Backspace from navigating
				}
			}
			return nil
		}))
	}
	keyListen("keydown", true)
	keyListen("keyup", false)
	doc.Call("addEventListener", "paste", js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		if cd := e.Get("clipboardData"); cd.Truthy() {
			clipboardFallback = cd.Call("getData", "text/plain").String()
		}
		e.Call("preventDefault")
		return nil
	}))
	g := js.Global()
	g.Call("addEventListener", "blur", js.FuncOf(func(js.Value, []js.Value) any { w.blurred = true; return nil }))
	g.Call("addEventListener", "focus", js.FuncOf(func(js.Value, []js.Value) any { w.blurred = false; return nil }))
	return w, nil
}

// TouchEvents returns and clears the touch events received since the last
// call. Drain it once per frame.
func (w *Window) TouchEvents() []TouchEvent {
	ev := w.touch
	w.touch = nil
	return ev
}

// KeyEvents returns and clears the key events received since the last
// call. Drain it once per frame.
func (w *Window) KeyEvents() []KeyEvent {
	ev := w.keys
	w.keys = nil
	return ev
}

// MouseEvents returns and clears the mouse events received since the last
// call. Drain it once per frame.
func (w *Window) MouseEvents() []MouseEvent {
	ev := w.mouse
	w.mouse = nil
	return ev
}

var domCursor = map[Cursor]string{
	CursorDefault: "default", CursorPointer: "pointer", CursorText: "text",
	CursorEW: "ew-resize",
}

// SetCursor sets the canvas CSS cursor.
func (w *Window) SetCursor(c Cursor) {
	if c == w.cursor {
		return
	}
	w.cursor = c
	w.canvas.Get("style").Set("cursor", domCursor[c])
}

// Poll reports whether the window is still open. Browser tabs have no
// close-request the page can observe, so this is always true; loops should
// rely on EachFrame's fn returning false.
func (w *Window) Poll() bool { return true }

// Size returns the canvas size in pixels.
func (w *Window) Size() (width, height int) { return w.width, w.height }

// Scale is the DPI scale (physical px per logical px): devicePixelRatio at
// canvas creation (GFX_SCALE overrides).
func (w *Window) Scale() float32 {
	if w.scale <= 0 {
		return 1
	}
	return w.scale
}

// Resizing reports an interactive OS resize in progress; not a concept the
// browser exposes, so always false.
func (w *Window) Resizing() bool { return false }

// The browser clipboard has no synchronous read, so the system's text
// reaches this process by two routes that both land in clipboardFallback
// before a frame can ask for it:
//   - the document's `paste` event, whose clipboardData is readable
//     synchronously and needs no permission — it fires right after the
//     Ctrl+V keydown and before the next animation frame drains the keys,
//     so the key event already sees the pasted text. For that to fire the
//     keydown must not be preventDefault'ed (see the key listener).
//   - navigator.clipboard.readText(), asked for on every ClipboardText
//     call; it resolves later (and only in a secure context, with the
//     permission the browser grants or asks for), updating the fallback
//     for the next read — a paste from a menu or a button gets the
//     system text on its second attempt at worst.
// Writes go to navigator.clipboard.writeText, which browsers allow after
// a user gesture.

// ClipboardText returns the clipboard text as last seen from the browser.
func ClipboardText() string {
	if cb := js.Global().Get("navigator").Get("clipboard"); cb.Truthy() {
		p := cb.Call("readText")
		var ok, fail js.Func
		ok = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 && args[0].Type() == js.TypeString {
				clipboardFallback = args[0].String()
			}
			ok.Release()
			fail.Release()
			return nil
		})
		fail = js.FuncOf(func(js.Value, []js.Value) any { ok.Release(); fail.Release(); return nil })
		p.Call("then", ok, fail)
	}
	return clipboardFallback
}

// SetClipboardText makes s the clipboard text.
func SetClipboardText(s string) {
	clipboardFallback = s
	if cb := js.Global().Get("navigator").Get("clipboard"); cb.Truthy() {
		var fail js.Func
		fail = js.FuncOf(func(js.Value, []js.Value) any { fail.Release(); return nil })
		cb.Call("writeText", s).Call("catch", fail)
	}
}

// isClipboardChord reports a Ctrl+C / Ctrl+V / Ctrl+X keydown: left to the
// browser so its copy/paste/cut events fire (the paste one carries the
// text), while the key event still reaches the app.
func isClipboardChord(e js.Value) bool {
	if !e.Get("ctrlKey").Bool() && !e.Get("metaKey").Bool() {
		return false
	}
	switch e.Get("key").String() {
	case "c", "v", "x", "C", "V", "X":
		return true
	}
	return false
}

// Destroy removes the canvas from the page.
// SetTitle sets the document title, which is the browser tab's name.
func (w *Window) SetTitle(title string) {
	js.Global().Get("document").Set("title", title)
}

func (w *Window) Destroy() {
	w.closed = true
	if w.canvas.Truthy() {
		w.canvas.Call("remove")
	}
}

// Canvas exposes the underlying canvas element for GPU surface creation.
// Only the platform glue should touch this.
func (w *Window) Canvas() js.Value { return w.canvas }

// EachFrame runs fn once per requestAnimationFrame tick until fn returns
// false. The callback only signals; fn itself runs on the Go goroutine, so
// it may block briefly without wedging the browser's event loop.
func (w *Window) EachFrame(fn func() bool) {
	frame := make(chan struct{}, 1)
	raf := js.FuncOf(func(js.Value, []js.Value) any {
		select {
		case frame <- struct{}{}:
		default:
		}
		return nil
	})
	defer raf.Release()
	g := js.Global()
	for {
		g.Call("requestAnimationFrame", raf)
		<-frame
		if !fn() {
			return
		}
	}
}

// Closed reports whether this window has been closed.
func (w *Window) Closed() bool { return w.closed }

// CloseChild is a no-op: NewChild always fails here (child windows are
// X11 and Windows only).
func (w *Window) CloseChild(*Window) {}

// Focused reports whether the page/tab has focus.
func (w *Window) Focused() bool { return !w.blurred }

// NewChild opens a second top-level window. It fails here: the web app has a
// single canvas (multi-window is X11 and Windows only).
func (w *Window) NewChild(o Options) (*Window, error) {
	return nil, fmt.Errorf("app: multi-window is not implemented on web")
}

// Hide removes the canvas from layout; Show restores it.
func (w *Window) Hide() { w.canvas.Get("style").Set("display", "none") }

// Show makes a hidden canvas visible again.
func (w *Window) Show() {
	w.canvas.Get("style").Set("display", "")
	w.canvas.Get("style").Set("visibility", "")
}

var _ Host = (*Window)(nil)

// Close, Minimize and ToggleMaximize exist for API parity with the desktop
// backends; the browser tab does not expose these window controls.
// TakeDamage always reports false on web: a canvas keeps its contents, so
// only input/animation/revision changes require repainting.
func (w *Window) TakeDamage() bool { return false }

// WaitEvent is a no-op on web: requestAnimationFrame paces the loop and the
// browser delivers events between frames.
func (w *Window) WaitEvent(time.Duration) {}

func (w *Window) Close()          {}
func (w *Window) Minimize()       {}
func (w *Window) ToggleMaximize() {}

// BeginMove and BeginResize exist for API parity; not applicable here.
func (w *Window) BeginMove()             {}
func (w *Window) BeginResize(ResizeEdge) {}

// RootPointer, Geometry and SetGeometry exist for API parity; the browser
// surface has no movable OS window.
func (w *Window) RootPointer() (int, int)        { return 0, 0 }
func (w *Window) Geometry() (int, int, int, int) { return 0, 0, w.width, w.height }
func (w *Window) SetGeometry(int, int, int, int) {}

// SetBackground is X11-only: a canvas has no server-side background.
func (w *Window) SetBackground(uint32) {}

// RetainDisplay is X11-only (keeps the connection open after Destroy);
// a no-op here.
func (w *Window) RetainDisplay() {}

// SetPointerCapture asks for Pointer Lock (relative mouse look): while it
// holds, the cursor hides and MouseMove events carry DX/DY deltas. The
// browser grants it only shortly after a user gesture (a click) and the
// user can end it with Esc, so PointerCaptured follows the browser's
// state (pointerlockchange) rather than this request.
func (w *Window) SetPointerCapture(on bool) {
	doc := js.Global().Get("document")
	if on {
		if !doc.Get("pointerLockElement").Equal(w.canvas) {
			w.canvas.Call("requestPointerLock")
		}
		return
	}
	if doc.Get("pointerLockElement").Equal(w.canvas) {
		doc.Call("exitPointerLock")
	}
}

// PointerCaptured reports whether the canvas holds Pointer Lock.
func (w *Window) PointerCaptured() bool { return w.captured }
