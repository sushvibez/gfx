//go:build linux && !android

package app

import "unsafe"

// SetMinSize publishes the smallest size (physical px) the window may be
// resized to, as WM_NORMAL_HINTS PMinSize: the window manager's own
// resizes (its borders, a drag it was handed with BeginResize, tiling)
// stop there. Unchanged values are not resent; an Unresizable window keeps
// its pinned size.
func (w *Window) SetMinSize(minW, minH int) {
	if w.display == 0 || w.win == 0 || w.unresizable {
		return
	}
	minW, minH = max(minW, 1), max(minH, 1)
	if minW == w.minW && minH == w.minH {
		return
	}
	w.minW, w.minH = minW, minH
	var h xSizeHints
	h.flags = 1 << 4 // PMinSize
	h.minWidth, h.minHeight = int32(minW), int32(minH)
	xSetWMNormalHints(w.display, w.win, &h)
	xFlush(w.display)
}

// HasWM reports whether a window manager runs on the display (EWMH
// _NET_SUPPORTING_WM_CHECK on the root window, naming a window that names
// itself): moves and resizes then go through it (BeginMove, BeginResize)
// rather than the client moving its own window, which a WM such as mutter
// under XWayland does not follow well. Asked once per window.
func (w *Window) HasWM() bool {
	if w.wmChecked {
		return w.wm
	}
	w.wmChecked = true
	if w.display == 0 || loadClipboardX11() != nil {
		return false
	}
	atom := xInternAtom(w.display, "_NET_SUPPORTING_WM_CHECK", 1)
	if atom == 0 {
		return false
	}
	check := func(win uintptr) uintptr {
		var typ uintptr
		var format int32
		var n, after uint64
		var data *byte
		const xaWindow = 33
		if xGetWindowProperty(w.display, win, atom, 0, 1, 0, xaWindow, &typ, &format, &n, &after, &data) != 0 || data == nil {
			return 0
		}
		defer xFree(unsafe.Pointer(data))
		if typ != xaWindow || format != 32 || n < 1 {
			return 0
		}
		return uintptr(*(*uint64)(unsafe.Pointer(data))) // format 32 is a C long
	}
	child := check(xDefaultRootWindow(w.display))
	w.wm = child != 0 && check(child) == child
	return w.wm
}
