//go:build !android

// GOOS=android also sets the linux build tag; X11/XRandR is desktop-only.

package app

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/sushvibez/gfx/ffi"
)

// Monitor enumeration through XRandR (libXrandr), bound the same no-cgo way
// as Xlib itself. libXrandr is opened lazily on the first Monitors() call so
// a machine without it still runs — the layout then degrades to one monitor
// covering the whole X screen.

var (
	xrrQueryExtension            func(d uintptr, eventBase, errorBase *int32) int32
	xrrGetScreenResourcesCurrent func(d, w uintptr) *xrrScreenResources
	xrrGetOutputInfo             func(d uintptr, res *xrrScreenResources, output uint64) *xrrOutputInfo
	xrrGetCrtcInfo               func(d uintptr, res *xrrScreenResources, crtc uint64) *xrrCrtcInfo
	xrrGetOutputPrimary          func(d, w uintptr) uint64
	xrrFreeScreenResources       func(res *xrrScreenResources)
	xrrFreeOutputInfo            func(o *xrrOutputInfo)
	xrrFreeCrtcInfo              func(c *xrrCrtcInfo)
)

// loadXrandr binds libXrandr once; a failure is remembered so the fallback
// path is taken without retrying the dlopen every call.
var loadXrandr = sync.OnceValue(func() error {
	lib, err := ffi.Open("libXrandr.so.2", "libXrandr.so.1", "libXrandr.so")
	if err != nil {
		return err
	}
	lib.Bind(&xrrQueryExtension, "XRRQueryExtension")
	lib.Bind(&xrrGetScreenResourcesCurrent, "XRRGetScreenResourcesCurrent")
	lib.Bind(&xrrGetOutputInfo, "XRRGetOutputInfo")
	lib.Bind(&xrrGetCrtcInfo, "XRRGetCrtcInfo")
	lib.Bind(&xrrGetOutputPrimary, "XRRGetOutputPrimary")
	lib.Bind(&xrrFreeScreenResources, "XRRFreeScreenResources")
	lib.Bind(&xrrFreeOutputInfo, "XRRFreeOutputInfo")
	lib.Bind(&xrrFreeCrtcInfo, "XRRFreeCrtcInfo")
	return nil
})

// xrrScreenResources mirrors XRRScreenResources (LP64). Pointer fields are
// typed as Go pointers, never uintptr, so indexing them needs no
// uintptr→unsafe.Pointer round trip (which `go vet -unsafeptr` rejects).
type xrrScreenResources struct {
	timestamp       uint64
	configTimestamp uint64
	ncrtc           int32
	_               int32
	crtcs           *uint64
	noutput         int32
	_               int32
	outputs         *uint64
	nmode           int32
	_               int32
	modes           uintptr // XRRModeInfo*, unused
}

// xrrOutputInfo mirrors XRROutputInfo (LP64).
type xrrOutputInfo struct {
	timestamp     uint64
	crtc          uint64
	name          *byte
	nameLen       int32
	_             int32
	mmWidth       uint64
	mmHeight      uint64
	connection    uint16 // 0 = RR_Connected
	subpixelOrder uint16
	ncrtc         int32
	crtcs         *uint64
	nclone        int32
	_             int32
	clones        *uint64
	nmode         int32
	npreferred    int32
	modes         uintptr
}

// xrrCrtcInfo mirrors XRRCrtcInfo (LP64).
type xrrCrtcInfo struct {
	timestamp uint64
	x, y      int32
	width     uint32
	height    uint32
	mode      uint64
	rotation  uint16
	noutput   int32
	outputs   *uint64
	rotations uint16
	npossible int32
	possible  *uint64
}

// xidAt indexes a C array of XIDs without an unsafe.Pointer/uintptr round
// trip (unsafe.Add keeps the pointer provenance vet wants).
func xidAt(base *uint64, i int) uint64 {
	return *(*uint64)(unsafe.Add(unsafe.Pointer(base), uintptr(i)*8))
}

// procDisplay is the display connection owned by the process's primary
// window, if one exists. Monitors() reuses it rather than opening a second
// connection (X servers cap client connections, and a query mid-frame should
// not cost a round trip's worth of handshake).
var procDisplay uintptr

// Monitors returns the current monitor layout in virtual-desktop pixels.
// It works with or without an open Window: without one it opens (and closes)
// its own display connection.
//
// XRandR is the source of truth; if libXrandr or the RandR extension is
// missing the whole X screen is reported as a single monitor.
func Monitors() ([]Monitor, error) {
	if err := loadX11(); err != nil {
		return nil, err
	}
	if procDisplay != 0 {
		return monitorsOn(procDisplay)
	}
	d := xOpenDisplay(0)
	if d == 0 {
		return nil, fmt.Errorf("app: cannot open X display (is DISPLAY set?)")
	}
	defer xCloseDisplay(d)
	return monitorsOn(d)
}

// monitorsOn enumerates the monitors of an open display connection.
func monitorsOn(d uintptr) ([]Monitor, error) {
	scale := scaleOverride()
	if scale <= 0 {
		scale = xftDPIScale(d)
	}
	root := xDefaultRootWindow(d)
	if mons := randrMonitors(d, root, scale); len(mons) > 0 {
		return mons, nil
	}
	// No RandR: the root window's geometry is the whole desktop, reported
	// as one monitor. Correct for single-head, and the only thing core X11
	// can say about a multi-head setup.
	var rootRet uintptr
	var x, y int32
	var w, h, border, depth uint32
	if xGetGeometry(d, root, &rootRet, &x, &y, &w, &h, &border, &depth) == 0 || w == 0 || h == 0 {
		return nil, fmt.Errorf("app: cannot query root window geometry")
	}
	return []Monitor{{
		Name: "screen", Width: int(w), Height: int(h), Scale: scale, Primary: true,
	}}, nil
}

// randrMonitors lists the connected, enabled RandR outputs. Returns nil when
// RandR is unavailable, so callers fall back to the root geometry.
func randrMonitors(d, root uintptr, scale float32) []Monitor {
	if loadXrandr() != nil {
		return nil
	}
	var evBase, errBase int32
	if xrrQueryExtension(d, &evBase, &errBase) == 0 {
		return nil
	}
	res := xrrGetScreenResourcesCurrent(d, root)
	if res == nil {
		return nil
	}
	defer xrrFreeScreenResources(res)
	primary := xrrGetOutputPrimary(d, root)
	var mons []Monitor
	for i := 0; i < int(res.noutput); i++ {
		out := xidAt(res.outputs, i)
		oi := xrrGetOutputInfo(d, res, out)
		if oi == nil {
			continue
		}
		// RR_Connected == 0; an output with no CRTC is connected but off.
		if oi.connection != 0 || oi.crtc == 0 {
			xrrFreeOutputInfo(oi)
			continue
		}
		ci := xrrGetCrtcInfo(d, res, oi.crtc)
		if ci == nil || ci.width == 0 || ci.height == 0 {
			if ci != nil {
				xrrFreeCrtcInfo(ci)
			}
			xrrFreeOutputInfo(oi)
			continue
		}
		mons = append(mons, Monitor{
			Name:    goStringN(oi.name, int(oi.nameLen)),
			X:       int(ci.x),
			Y:       int(ci.y),
			Width:   int(ci.width),
			Height:  int(ci.height),
			Scale:   scale,
			Primary: out == primary,
		})
		xrrFreeCrtcInfo(ci)
		xrrFreeOutputInfo(oi)
	}
	// XRRGetOutputPrimary returns 0 on a server with no primary set (and on
	// some XWayland builds): treat the first output as primary so callers
	// always have one.
	if len(mons) > 0 {
		havePrimary := false
		for _, m := range mons {
			havePrimary = havePrimary || m.Primary
		}
		if !havePrimary {
			mons[0].Primary = true
		}
	}
	return mons
}

// goStringN copies n bytes from a C string that is not NUL-terminated by
// contract (XRROutputInfo.name carries its length separately).
func goStringN(p *byte, n int) string {
	if p == nil || n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = *(*byte)(unsafe.Add(unsafe.Pointer(p), uintptr(i)))
	}
	return string(b)
}

// netWMState sends an EWMH _NET_WM_STATE ClientMessage to the root window.
// action: 0 remove, 1 add, 2 toggle.
func (w *Window) netWMState(action uint64, atom1, atom2 uintptr) {
	if w.display == 0 || w.win == 0 {
		return
	}
	state := xInternAtom(w.display, "_NET_WM_STATE", 0)
	var ev xEvent
	*(*int32)(unsafe.Pointer(&ev.data[0])) = xClientMessage
	ev.data[4] = uint64(w.win)
	ev.data[5] = uint64(state)
	*(*int32)(unsafe.Pointer(&ev.data[6])) = 32
	ev.data[7] = action
	ev.data[8] = uint64(atom1)
	ev.data[9] = uint64(atom2)
	ev.data[11] = 1 // source indication: normal application
	root := xDefaultRootWindow(w.display)
	xSendEvent(w.display, root, 0, 0x180000, &ev) // SubstructureRedirect|Notify
	xFlush(w.display)
}

// setFullscreenMonitors pins a fullscreen window to one monitor via EWMH
// _NET_WM_FULLSCREEN_MONITORS (top, bottom, left, right monitor indices —
// all the same index for a single-monitor fullscreen).
//
// The spec indexes monitors the way Xinerama enumerates them. Our Monitors()
// order comes from XRandR outputs, which on every server tested matches
// Xinerama's order, but the two are not guaranteed identical; a WM that does
// not implement the message ignores it entirely. The window is therefore ALSO
// moved to the monitor rect before mapping, so the wrong-monitor failure mode
// is "the WM re-fullscreens it where it already is", not "it lands nowhere".
func (w *Window) setFullscreenMonitors(idx int) {
	if w.display == 0 || w.win == 0 || idx < 0 {
		return
	}
	atom := xInternAtom(w.display, "_NET_WM_FULLSCREEN_MONITORS", 0)
	if atom == 0 {
		return
	}
	var ev xEvent
	*(*int32)(unsafe.Pointer(&ev.data[0])) = xClientMessage
	ev.data[4] = uint64(w.win)
	ev.data[5] = uint64(atom)
	*(*int32)(unsafe.Pointer(&ev.data[6])) = 32
	ev.data[7] = uint64(idx)  // top
	ev.data[8] = uint64(idx)  // bottom
	ev.data[9] = uint64(idx)  // left
	ev.data[10] = uint64(idx) // right
	ev.data[11] = 1           // source indication: normal application
	root := xDefaultRootWindow(w.display)
	xSendEvent(w.display, root, 0, 0x180000, &ev)
	xFlush(w.display)
}

// SetFullscreen turns borderless fullscreen on or off for the monitor the
// window currently sits on (EWMH _NET_WM_STATE_FULLSCREEN, plus
// _NET_WM_FULLSCREEN_MONITORS to pin the head in a multi-head setup).
func (w *Window) SetFullscreen(on bool) {
	if w.display == 0 || w.win == 0 {
		return
	}
	fs := xInternAtom(w.display, "_NET_WM_STATE_FULLSCREEN", 0)
	if on {
		idx := w.MonitorIndex()
		w.netWMState(1, fs, 0)
		w.setFullscreenMonitors(idx)
	} else {
		w.netWMState(0, fs, 0)
	}
	w.fullscreen = on
}

// Fullscreen reports the last state requested through SetFullscreen or
// Options.Fullscreen. The window manager has the final say — this is what we
// asked for, not a round trip to the server.
func (w *Window) Fullscreen() bool { return w.fullscreen }

// MonitorIndex returns the index into Monitors() of the monitor containing
// the window's center, or -1 when the layout cannot be queried.
func (w *Window) MonitorIndex() int {
	if w.display == 0 || w.win == 0 {
		return -1
	}
	mons, err := monitorsOn(w.display)
	if err != nil || len(mons) == 0 {
		return -1
	}
	x, y, ww, hh := w.Geometry()
	return monitorIndexAt(mons, x+ww/2, y+hh/2)
}
