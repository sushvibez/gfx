//go:build windows

package app

import "unsafe"

// SetMinSize sets the smallest client size (physical px) the window may be
// resized to: WM_GETMINMAXINFO answers with it (plus the frame), so the
// system's border drags and Aero snap stop there.
func (w *Window) SetMinSize(minW, minH int) {
	w.minW, w.minH = max(minW, 0), max(minH, 0)
}

// HasWM is false on Windows: the shell resizes undecorated windows itself
// there (the system's own resize is reached with BeginResize when wanted).
func (w *Window) HasWM() bool { return false }

// minMaxInfo is MINMAXINFO.
type minMaxInfo struct {
	reserved, maxSize, maxPosition, minTrack, maxTrack winPoint
}

// answerMinMax answers WM_GETMINMAXINFO: the minimum client size as a
// window size (the frame added, measured from the window as it stands).
func (w *Window) answerMinMax(lparam uintptr) {
	if w == nil || (w.minW == 0 && w.minH == 0) || lparam == 0 {
		return
	}
	var wr, cr winRect
	getWindowRect(w.hwnd, &wr)
	getClientRect(w.hwnd, &cr)
	fw := (wr.right - wr.left) - (cr.right - cr.left)
	fh := (wr.bottom - wr.top) - (cr.bottom - cr.top)
	// The message's pointer, read through its address (a uintptr held by
	// the system for the call, not Go memory).
	mmi := *(**minMaxInfo)(unsafe.Pointer(&lparam))
	mmi.minTrack.x = max(mmi.minTrack.x, int32(w.minW)+fw)
	mmi.minTrack.y = max(mmi.minTrack.y, int32(w.minH)+fh)
}
