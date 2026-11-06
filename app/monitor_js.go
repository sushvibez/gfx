//go:build js

package app

import (
	"fmt"
	"syscall/js"
)

// The browser exposes no monitor layout — window.screen describes only the
// screen the page happens to be on, and the multi-screen Window Placement API
// needs a user permission prompt we do not ask for. The web backend therefore
// reports exactly one monitor: the screen the document is displayed on.
func Monitors() ([]Monitor, error) {
	screen := js.Global().Get("screen")
	if !screen.Truthy() {
		return nil, fmt.Errorf("app: no window.screen (not running in a browser?)")
	}
	scale := scaleOverride()
	if scale <= 0 {
		if v := js.Global().Get("devicePixelRatio"); v.Truthy() {
			scale = float32(v.Float())
		}
	}
	if scale <= 0 {
		scale = 1
	}
	w, h := screen.Get("width"), screen.Get("height")
	if !w.Truthy() || !h.Truthy() {
		return nil, fmt.Errorf("app: window.screen has no size")
	}
	return []Monitor{{
		Name:    "screen",
		Width:   int(float32(w.Int())*scale + 0.5),
		Height:  int(float32(h.Int())*scale + 0.5),
		Scale:   scale,
		Primary: true,
	}}, nil
}

// SetFullscreen asks the browser to take the canvas fullscreen (or leave it).
// Browsers only grant requestFullscreen from inside a user-gesture handler,
// so a call made from arbitrary frame code is rejected — the rejection is
// swallowed here, exactly as the WM ignoring the request is on X11.
func (w *Window) SetFullscreen(on bool) {
	if !w.canvas.Truthy() {
		return
	}
	doc := js.Global().Get("document")
	if on {
		if fn := w.canvas.Get("requestFullscreen"); fn.Truthy() {
			p := w.canvas.Call("requestFullscreen")
			if p.Truthy() && p.Get("catch").Truthy() {
				p.Call("catch", js.FuncOf(func(js.Value, []js.Value) any { return nil }))
			}
		}
		return
	}
	if doc.Truthy() && doc.Get("exitFullscreen").Truthy() {
		doc.Call("exitFullscreen")
	}
}

// Fullscreen reports whether the canvas is the document's fullscreen element.
func (w *Window) Fullscreen() bool {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return false
	}
	el := doc.Get("fullscreenElement")
	return el.Truthy() && el.Equal(w.canvas)
}

// MonitorIndex is always 0 on the web: there is only ever one reported
// monitor (see Monitors).
func (w *Window) MonitorIndex() int { return 0 }
