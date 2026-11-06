package app

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/sushvibez/gfx/ffi"
)

// Monitor enumeration and borderless fullscreen on Win32. EnumDisplayMonitors
// walks the virtual desktop, GetMonitorInfoW gives each monitor's rect, device
// name and the primary flag, and GetDpiForMonitor (shcore.dll, Win8.1+) the
// per-monitor DPI — the one platform here that really does report a different
// scale per screen.

const (
	monitorInfoFPrimary = 0x0001
	gwlStyle            = -16
	mdtEffectiveDPI     = 0

	swpNoZOrder      = 0x0004
	swpNoActivate    = 0x0010
	swpFrameChanged  = 0x0020
	swpNoOwnerZOrder = 0x0200
)

var (
	enumDisplayMonitors func(hdc, clip, proc uintptr, data uintptr) int32
	getMonitorInfoW     func(hmon uintptr, info *monitorInfoExW) int32
	getWindowLongPtrW   func(hwnd uintptr, index int32) uintptr
	setWindowLongPtrW   func(hwnd uintptr, index int32, v uintptr) uintptr

	// shcore.dll, Win8.1+ — nil on older systems, where every monitor
	// reports the process-wide DPI instead.
	getDpiForMonitor func(hmon uintptr, dpiType uint32, dpiX, dpiY *uint32) int32
)

// monitorInfoExW mirrors MONITORINFOEXW (cbSize 104 on 64-bit).
type monitorInfoExW struct {
	cbSize    uint32
	rcMonitor winRect
	rcWork    winRect
	dwFlags   uint32
	szDevice  [32]uint16
}

var loadMonitorAPI = sync.OnceValue(func() error {
	if err := loadWin32(); err != nil {
		return err
	}
	user32, err := ffi.Open("user32.dll")
	if err != nil {
		return err
	}
	user32.Bind(&enumDisplayMonitors, "EnumDisplayMonitors")
	user32.Bind(&getMonitorInfoW, "GetMonitorInfoW")
	// The Ptr spellings exist on 64-bit; 32-bit user32 exports only the
	// plain ones (same ABI there, uintptr is 32 bits).
	for _, b := range []struct {
		fptr any
		name string
	}{
		{&getWindowLongPtrW, "GetWindowLongPtrW"},
		{&setWindowLongPtrW, "SetWindowLongPtrW"},
	} {
		if _, err := user32.Sym(b.name); err == nil {
			user32.Bind(b.fptr, b.name)
		}
	}
	if getWindowLongPtrW == nil {
		user32.Bind(&getWindowLongPtrW, "GetWindowLongW")
		user32.Bind(&setWindowLongPtrW, "SetWindowLongW")
	}
	if shcore, err := ffi.Open("shcore.dll"); err == nil {
		if _, err := shcore.Sym("GetDpiForMonitor"); err == nil {
			shcore.Bind(&getDpiForMonitor, "GetDpiForMonitor")
		}
	}
	return nil
})

// enumHandles collects HMONITORs during one EnumDisplayMonitors call. The
// whole package is single-goroutine (the window thread is LockOSThread'd),
// so one buffer is enough and the callback stays allocation-free.
var enumHandles []uintptr

// monitorEnumProc is created once: callbacks are never freed, so making one
// per enumeration would leak a slot per call.
var monitorEnumProc = sync.OnceValue(func() uintptr {
	return ffi.Callback(func(hmon, hdc, lprect, data uintptr) uintptr {
		// The LPRECT the OS passes is ignored deliberately: reading it would
		// mean converting a foreign uintptr to unsafe.Pointer, and
		// GetMonitorInfoW below returns the same rect safely.
		enumHandles = append(enumHandles, hmon)
		return 1 // keep enumerating
	})
})

// Monitors returns the current monitor layout in virtual-desktop pixels.
// It needs no window: the display layout is a process-wide property.
func Monitors() ([]Monitor, error) {
	if err := loadMonitorAPI(); err != nil {
		return nil, err
	}
	enumHandles = enumHandles[:0]
	enumDisplayMonitors(0, 0, monitorEnumProc(), 0)
	if len(enumHandles) == 0 {
		return nil, fmt.Errorf("app: EnumDisplayMonitors returned no monitors")
	}
	mons := make([]Monitor, 0, len(enumHandles))
	for _, h := range enumHandles {
		var mi monitorInfoExW
		mi.cbSize = uint32(unsafe.Sizeof(mi))
		if getMonitorInfoW(h, &mi) == 0 {
			continue
		}
		scale := float32(1)
		if getDpiForMonitor != nil {
			var dx, dy uint32
			if getDpiForMonitor(h, mdtEffectiveDPI, &dx, &dy) == 0 && dx != 0 {
				scale = float32(dx) / 96
			}
		} else if getDpiForSystem != nil {
			if d := getDpiForSystem(); d != 0 {
				scale = float32(d) / 96
			}
		}
		if s := scaleOverride(); s > 0 {
			scale = s
		}
		mons = append(mons, Monitor{
			Name:    utf16Name(mi.szDevice[:]),
			X:       int(mi.rcMonitor.left),
			Y:       int(mi.rcMonitor.top),
			Width:   int(mi.rcMonitor.right - mi.rcMonitor.left),
			Height:  int(mi.rcMonitor.bottom - mi.rcMonitor.top),
			Scale:   scale,
			Primary: mi.dwFlags&monitorInfoFPrimary != 0,
		})
	}
	if len(mons) == 0 {
		return nil, fmt.Errorf("app: GetMonitorInfoW failed for every monitor")
	}
	return mons, nil
}

// utf16Name decodes a NUL-padded fixed-size UTF-16 device name.
func utf16Name(u []uint16) string {
	n := 0
	for n < len(u) && u[n] != 0 {
		n++
	}
	return string(utf16Decode(u[:n]))
}

// SetFullscreen switches the window between borderless fullscreen over its
// current monitor and its previous windowed placement. The display mode is
// never changed — exclusive fullscreen buys nothing with a composited
// swapchain and costs a mode switch on every alt-tab.
func (w *Window) SetFullscreen(on bool) {
	if w.hwnd == 0 || loadMonitorAPI() != nil {
		return
	}
	if on == w.fullscreen {
		return
	}
	if on {
		var r winRect
		if getWindowRect(w.hwnd, &r) != 0 {
			w.savedRect = r
			w.savedStyle = getWindowLongPtrW(w.hwnd, gwlStyle)
		}
		mons, err := Monitors()
		if err != nil || len(mons) == 0 {
			return
		}
		i := w.MonitorIndex()
		if i < 0 {
			i = 0
		}
		m := mons[i]
		setWindowLongPtrW(w.hwnd, gwlStyle, uintptr(wsPopup|wsVisible))
		setWindowPos(w.hwnd, 0, int32(m.X), int32(m.Y), int32(m.Width), int32(m.Height),
			swpNoZOrder|swpNoActivate|swpFrameChanged|swpNoOwnerZOrder)
		w.fullscreen = true
		return
	}
	if w.savedStyle != 0 {
		setWindowLongPtrW(w.hwnd, gwlStyle, w.savedStyle)
	}
	r := w.savedRect
	if r.right > r.left && r.bottom > r.top {
		setWindowPos(w.hwnd, 0, r.left, r.top, r.right-r.left, r.bottom-r.top,
			swpNoZOrder|swpNoActivate|swpFrameChanged|swpNoOwnerZOrder)
	}
	w.fullscreen = false
}

// Fullscreen reports whether the window is in borderless fullscreen.
func (w *Window) Fullscreen() bool { return w.fullscreen }

// MonitorIndex returns the index into Monitors() of the monitor containing
// the window's center, or -1 when the layout cannot be queried.
func (w *Window) MonitorIndex() int {
	if w.hwnd == 0 {
		return -1
	}
	mons, err := Monitors()
	if err != nil || len(mons) == 0 {
		return -1
	}
	x, y, ww, hh := w.Geometry()
	return monitorIndexAt(mons, x+ww/2, y+hh/2)
}
