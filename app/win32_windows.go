package app

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sushvibez/gfx/ffi"
)

// Win32 windows have OS-thread affinity: PeekMessageW only returns messages
// for windows created by the CALLING thread, and Windows ghosts a window
// ("Not Responding", busy cursor, DWM stops consuming presents — surface
// acquire then hits wgpu's 1s frame timeout) whose owning thread stops
// pumping. The Go scheduler freely migrates goroutines across threads, so
// pin the main goroutine to its thread before it creates a window. This
// init runs on the main goroutine during package initialization — the
// documented use of LockOSThread for main-thread-only platform APIs.
func init() { runtime.LockOSThread() }

// Win32 windowing through stdlib DLL loading + purego function binding —
// the same no-cgo recipe as every other platform. Developed on Linux and
// verified on Windows 11 (2026-10); the layout-sensitive parts are
// WNDCLASSEXW/MSG, kept field-for-field with the Win32 headers.

const (
	wmSize        = 0x0005
	wmSetFocus    = 0x0007
	wmKillFocus   = 0x0008
	wmPaint       = 0x000F
	wmClose       = 0x0010
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmChar        = 0x0102
	wmSetCursor   = 0x0020
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmMButtonDown = 0x0207
	wmMButtonUp   = 0x0208
	wmMouseWheel  = 0x020A
	wmMouseHWheel = 0x020E

	wmSysKeyDown     = 0x0104
	wmSysKeyUp       = 0x0105
	wmImeComposition = 0x010F
	gcsResultStr     = 0x0800 // GCS_RESULTSTR: composition committed
	wmTimer          = 0x0113
	wmEnterSizeMove  = 0x0231
	wmExitSizeMove   = 0x0232
	wmDpiChanged     = 0x02E0
	sizeMoveTimerID  = 1

	wsOverlappedWindow = 0x00CF0000
	wsPopup            = 0x80000000
	wsVisible          = 0x10000000
	wsExNoRedirBitmap  = 0x00200000 // WS_EX_NOREDIRECTIONBITMAP
	pmRemove           = 0x0001
	idcArrow           = 32512
)

// noRedirectionBitmap reports whether windows should be created without a
// GDI redirection bitmap. The gpu package presents DX12 through a
// DirectComposition visual by default (see gpu.dx12SwapchainKind); the
// visual composites OVER the redirection bitmap, so transparent windows
// (alpha-0 clears) show the bitmap's never-painted white instead of the
// desktop, and resizes grow fresh white bands behind the visual. GPUI sets
// WS_EX_NOREDIRECTIONBITMAP for the same reason. Skipped when the debug
// knobs select a non-DComp path (plain HWND swapchain, or the GL backend,
// which blits through GDI and NEEDS the bitmap).
func noRedirectionBitmap() bool {
	if os.Getenv("GFX_WGPU_DX12_SWAPCHAIN") == "hwnd" {
		return false
	}
	switch os.Getenv("GFX_WGPU_BACKEND") {
	case "", "dx12":
		return true
	}
	return false
}

var (
	getModuleHandleW          func(name uintptr) uintptr
	registerClassExW          func(cls *wndClassExW) uint16
	createWindowExW           func(exStyle uint32, className, windowName *uint16, style uint32, x, y, w, h int32, parent, menu, instance uintptr, param uintptr) uintptr
	setWindowTextW            func(hwnd uintptr, text *uint16) int32
	showWindow                func(hwnd uintptr, cmd int32) int32
	isZoomed                  func(hwnd uintptr) int32
	releaseCapture            func() int32
	sendMessageW              func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr
	getCursorPos              func(pt *winPoint) int32
	getWindowRect             func(hwnd uintptr, r *winRect) int32
	moveWindow                func(hwnd uintptr, x, y, w, h int32, repaint int32) int32
	destroyWindow             func(hwnd uintptr) int32
	defWindowProcW            func(hwnd, msg, wparam, lparam uintptr) uintptr
	peekMessageW              func(msg *msgW, hwnd uintptr, filterMin, filterMax, remove uint32) int32
	translateMessage          func(msg *msgW) int32
	dispatchMessageW          func(msg *msgW) uintptr
	loadCursorW               func(instance uintptr, name uintptr) uintptr
	setCursorW                func(cursor uintptr) uintptr
	getKeyState               func(vk int32) int16
	setCursorPos              func(x, y int32) int32
	showCursorW               func(show int32) int32
	clientToScreen            func(hwnd uintptr, pt *point32) int32
	msgWaitForMultipleObjects func(count uint32, handles uintptr, waitAll int32, ms uint32, wakeMask uint32) uint32
	setTimer                  func(hwnd uintptr, id uintptr, elapseMs uint32, proc uintptr) uintptr
	killTimer                 func(hwnd uintptr, id uintptr) int32
	adjustWindowRect          func(r *winRect, style uint32, menu int32, exStyle uint32) int32
	getClientRect             func(hwnd uintptr, r *winRect) int32
	copyRect                  func(dst *winRect, src uintptr) int32

	// Per-monitor DPI (Win10 1607+); nil when the OS predates them.
	setProcessDpiAwarenessContext func(ctx uintptr) int32
	setProcessDPIAware            func() int32
	getDpiForWindow               func(hwnd uintptr) uint32
	getDpiForSystem               func() uint32
	adjustWindowRectForDpi        func(r *winRect, style uint32, menu int32, exStyle uint32, dpi uint32) int32
	setWindowPos                  func(hwnd, insertAfter uintptr, x, y, w, h int32, flags uint32) int32

	// Multi-touch (Win7+; bound optionally so ancient systems still run).
	registerTouchWindow   func(hwnd uintptr, flags uint32) int32
	getTouchInputInfo     func(handle uintptr, count uint32, inputs *touchInput, cbSize int32) int32
	closeTouchInputHandle func(handle uintptr) int32
	screenToClient        func(hwnd uintptr, pt *point32) int32

	// IME composition (imm32.dll; bound optionally — nil disables IME text).
	immGetContext            func(hwnd uintptr) uintptr
	immReleaseContext        func(hwnd, ctx uintptr) int32
	immGetCompositionStringW func(ctx uintptr, index uint32, buf unsafe.Pointer, bufLen uint32) int32

	// OS clipboard (CF_UNICODETEXT). The two rtlMoveMemory bindings are the
	// same symbol with in/out signatures: copying through the native side
	// avoids dereferencing foreign uintptrs in Go (vet's unsafeptr check).
	openClipboard    func(hwnd uintptr) int32
	closeClipboard   func() int32
	emptyClipboard   func() int32
	getClipboardData func(format uint32) uintptr
	setClipboardData func(format uint32, h uintptr) uintptr
	globalAlloc      func(flags uint32, size uintptr) uintptr
	globalLock       func(h uintptr) uintptr
	globalUnlock     func(h uintptr) int32
	globalFree       func(h uintptr) uintptr
	lstrlenW         func(p uintptr) int32
	rtlCopyToGo      func(dst *uint16, src uintptr, n uintptr)
	rtlCopyFromGo    func(dst uintptr, src *uint16, n uintptr)
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// ClipboardText returns the OS clipboard's text, or the in-process
// fallback buffer when the clipboard is busy or holds no text.
func ClipboardText() string {
	if loadWin32() != nil || openClipboard(0) == 0 {
		return clipboardFallback
	}
	defer closeClipboard()
	h := getClipboardData(cfUnicodeText)
	if h == 0 {
		return clipboardFallback
	}
	p := globalLock(h)
	if p == 0 {
		return clipboardFallback
	}
	defer globalUnlock(h)
	n := lstrlenW(p)
	if n <= 0 {
		return ""
	}
	buf := make([]uint16, n)
	rtlCopyToGo(&buf[0], p, uintptr(n)*2)
	return string(utf16Decode(buf))
}

// SetClipboardText puts s on the OS clipboard (and mirrors it into the
// in-process fallback so copy/paste keeps working if the OS side fails).
func SetClipboardText(s string) {
	clipboardFallback = s
	u, err := syscall.UTF16FromString(s)
	if err != nil { // interior NUL: keep the fallback only
		return
	}
	if loadWin32() != nil || openClipboard(0) == 0 {
		return
	}
	defer closeClipboard()
	emptyClipboard()
	size := uintptr(len(u)) * 2
	h := globalAlloc(gmemMoveable, size)
	if h == 0 {
		return
	}
	p := globalLock(h)
	if p == 0 {
		globalFree(h)
		return
	}
	rtlCopyFromGo(p, &u[0], size)
	globalUnlock(h)
	if setClipboardData(cfUnicodeText, h) == 0 {
		globalFree(h) // the system owns h only on success
	}
}

// utf16Decode converts UTF-16 code units to runes, combining surrogate
// pairs (a minimal inline utf16.Decode to keep the import surface small).
func utf16Decode(u []uint16) []rune {
	out := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		r := rune(u[i])
		if r >= 0xD800 && r < 0xDC00 && i+1 < len(u) {
			if lo := rune(u[i+1]); lo >= 0xDC00 && lo < 0xE000 {
				r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
				i++
			}
		}
		if r != 0 {
			out = append(out, r)
		}
	}
	return out
}

// touchInput mirrors Win32 TOUCHINPUT (winuser.h), 48 bytes on amd64:
// x/y are in HUNDREDTHS of a physical screen pixel.
type touchInput struct {
	x, y      int32
	hSource   uintptr
	id        uint32
	flags     uint32
	mask      uint32
	time      uint32
	extraInfo uintptr
	cx, cy    uint32
}

// TOUCHINPUT.dwFlags bits and the WM_TOUCH message.
const (
	wmTouch            = 0x0240
	touchEventfMove    = 0x0001
	touchEventfDown    = 0x0002
	touchEventfUp      = 0x0004
	touchEventfPrimary = 0x0010
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  uintptr
	lpszClassName uintptr
	hIconSm       uintptr
}

type msgW struct {
	hwnd    uintptr
	message uint32
	_       uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
	_       uint32
}

var loadWin32 = sync.OnceValue(func() error {
	user32, err := ffi.Open("user32.dll")
	if err != nil {
		return err
	}
	kernel32, err := ffi.Open("kernel32.dll")
	if err != nil {
		return err
	}
	kernel32.Bind(&getModuleHandleW, "GetModuleHandleW")
	user32.Bind(&registerClassExW, "RegisterClassExW")
	user32.Bind(&createWindowExW, "CreateWindowExW")
	user32.Bind(&setWindowTextW, "SetWindowTextW")
	user32.Bind(&showWindow, "ShowWindow")
	user32.Bind(&isZoomed, "IsZoomed")
	user32.Bind(&releaseCapture, "ReleaseCapture")
	user32.Bind(&sendMessageW, "SendMessageW")
	user32.Bind(&getCursorPos, "GetCursorPos")
	user32.Bind(&getWindowRect, "GetWindowRect")
	user32.Bind(&moveWindow, "MoveWindow")
	user32.Bind(&destroyWindow, "DestroyWindow")
	user32.Bind(&defWindowProcW, "DefWindowProcW")
	user32.Bind(&peekMessageW, "PeekMessageW")
	user32.Bind(&translateMessage, "TranslateMessage")
	user32.Bind(&dispatchMessageW, "DispatchMessageW")
	user32.Bind(&loadCursorW, "LoadCursorW")
	user32.Bind(&setCursorW, "SetCursor")
	user32.Bind(&getKeyState, "GetKeyState")
	user32.Bind(&setCursorPos, "SetCursorPos")
	user32.Bind(&showCursorW, "ShowCursor")
	user32.Bind(&clientToScreen, "ClientToScreen")
	user32.Bind(&msgWaitForMultipleObjects, "MsgWaitForMultipleObjects")
	user32.Bind(&setTimer, "SetTimer")
	user32.Bind(&killTimer, "KillTimer")
	user32.Bind(&adjustWindowRect, "AdjustWindowRectEx")
	user32.Bind(&getClientRect, "GetClientRect")
	user32.Bind(&copyRect, "CopyRect")
	user32.Bind(&setWindowPos, "SetWindowPos")
	// Optional per-monitor-DPI APIs (Win10 1607+/1703+): bind when present.
	for _, b := range []struct {
		fptr any
		name string
	}{
		{&setProcessDpiAwarenessContext, "SetProcessDpiAwarenessContext"},
		{&setProcessDPIAware, "SetProcessDPIAware"},
		{&getDpiForWindow, "GetDpiForWindow"},
		{&getDpiForSystem, "GetDpiForSystem"},
		{&adjustWindowRectForDpi, "AdjustWindowRectExForDpi"},
		{&registerTouchWindow, "RegisterTouchWindow"},
		{&getTouchInputInfo, "GetTouchInputInfo"},
		{&closeTouchInputHandle, "CloseTouchInputHandle"},
		{&screenToClient, "ScreenToClient"},
	} {
		if _, err := user32.Sym(b.name); err == nil {
			user32.Bind(b.fptr, b.name)
		}
	}
	user32.Bind(&openClipboard, "OpenClipboard")
	user32.Bind(&closeClipboard, "CloseClipboard")
	user32.Bind(&emptyClipboard, "EmptyClipboard")
	user32.Bind(&getClipboardData, "GetClipboardData")
	user32.Bind(&setClipboardData, "SetClipboardData")
	kernel32.Bind(&globalAlloc, "GlobalAlloc")
	kernel32.Bind(&globalLock, "GlobalLock")
	kernel32.Bind(&globalUnlock, "GlobalUnlock")
	kernel32.Bind(&globalFree, "GlobalFree")
	kernel32.Bind(&lstrlenW, "lstrlenW")
	kernel32.Bind(&rtlCopyToGo, "RtlMoveMemory")
	kernel32.Bind(&rtlCopyFromGo, "RtlMoveMemory")
	// IME composition text (imm32; optional — absent on stripped-down SKUs,
	// where CJK IME input simply stays unavailable).
	if imm32, err := ffi.Open("imm32.dll"); err == nil {
		for _, b := range []struct {
			fptr any
			name string
		}{
			{&immGetContext, "ImmGetContext"},
			{&immReleaseContext, "ImmReleaseContext"},
			{&immGetCompositionStringW, "ImmGetCompositionStringW"},
		} {
			if _, err := imm32.Sym(b.name); err == nil {
				imm32.Bind(b.fptr, b.name)
			}
		}
	}
	// Declare per-monitor-v2 DPI awareness (GPUI does the same): without it
	// Windows renders the process at 96dpi and bitmap-stretches the window —
	// blurry everything on scaled displays. Must run before any HWND exists.
	if setProcessDpiAwarenessContext != nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 == (handle)-4
		setProcessDpiAwarenessContext(^uintptr(3))
	} else if setProcessDPIAware != nil {
		setProcessDPIAware()
	}
	return nil
})

func modifiers() (shift, ctrl, alt bool) {
	return getKeyState(0x10)&int16(-0x8000) != 0,
		getKeyState(0x11)&int16(-0x8000) != 0,
		getKeyState(0x12)&int16(-0x8000) != 0 // VK_MENU
}

// Options configures a new window.
type Options struct {
	Title  string
	Width  int
	Height int
	// Undecorated removes the OS title bar and borders — for apps that
	// draw their own chrome (custom titlebars).
	Undecorated bool
	// Transparent requests a per-pixel-alpha window; only the X11 backend
	// implements it (accepted here for API parity).
	Transparent bool
	// Hidden creates the window without showing it; call Show once the
	// first frame has been presented to avoid a blank flash during
	// GPU/font startup.
	Hidden bool
	// Unmovable pins the window in place (gpui's is_movable: false):
	// titlebar drags and Move system commands are refused.
	Unmovable bool
	// Unresizable removes the sizing border and maximize box (gpui's
	// is_resizable: false).
	Unresizable bool
	// Unminimizable removes the minimize box and refuses Minimize system
	// commands (gpui's is_minimizable: false).
	Unminimizable bool
	// X, Y place the window at a virtual-desktop pixel position — the
	// coordinate space Monitors() and Geometry() use, so a window opens on
	// a chosen monitor by naming a point inside it. Honored only when
	// Positioned is true, so the zero Options still means "let the OS
	// decide" (CW_USEDEFAULT).
	X, Y       int
	Positioned bool
	// Fullscreen opens the window borderless (WS_POPUP) over the whole
	// monitor that contains (X, Y) — or the primary monitor when Positioned
	// is false. The display mode is not changed.
	Fullscreen bool
}

// Window is a native Win32 window plus its message pump.
type Window struct {
	maximized bool
	hwnd      uintptr
	hinstance uintptr
	width     int
	height    int
	closed    bool
	// OnCloseRequest, when set, runs when the user asks the window manager
	// to close the window (the title bar's ×, Alt+F4, WM_DELETE_WINDOW);
	// returning false keeps the window open (an unsaved-changes dialog).
	// Close() always closes.
	OnCloseRequest func() bool
	mouse          []MouseEvent
	keys           []KeyEvent
	touch          []TouchEvent
	lastX          float32
	lastY          float32
	cursor         Cursor
	captured       bool
	warpSkip       bool
	unmovable      bool    // refuse SC_MOVE/titlebar drags (Options.Unmovable)
	unminimizable  bool    // refuse SC_MINIMIZE (Options.Unminimizable)
	sizing         bool    // inside WM_ENTERSIZEMOVE..WM_EXITSIZEMOVE (Resizing)
	damaged        bool    // WM_PAINT arrived: contents need repainting (TakeDamage)
	focused        bool    // WM_SETFOCUS/WM_KILLFOCUS; animations may pause unfocused
	scaleF         float32 // DPI scale (physical px per logical px); 1 pre-Win10
	highSurrogate  rune    // pending WM_CHAR UTF-16 high half (emoji etc.)

	fullscreen bool    // borderless fullscreen (Options.Fullscreen/SetFullscreen)
	savedStyle uintptr // windowed WS_* style, restored by SetFullscreen(false)
	savedRect  winRect // windowed outer rect, restored by SetFullscreen(false)
}

// DispatchMessageW blocks in the OS modal loop for the whole of an
// interactive move/resize, so EachFrame's loop starves and DWM stretches
// the stale swapchain image (text visibly scales during a border drag).
// frameFn lets the WndProc keep rendering from inside that loop:
// WM_ENTERSIZEMOVE starts a timer, WM_SIZE/WM_TIMER run a frame,
// WM_EXITSIZEMOVE stops it. Package-level (UI is single-goroutine): the
// one frame function drives ALL windows, so a modal drag on a CHILD keeps
// the whole app rendering too.
var (
	frameFn    func() bool
	frameOwner *Window // EachFrame's receiver; a false return closes it
	inFrame    bool
)

// frameNow runs one frame from inside the WndProc (modal size/move loop).
// Reentrancy guard: a frame itself pumps no messages, but WM_SIZE can
// arrive while a WM_TIMER frame is mid-flight via SendMessage paths.
func (w *Window) frameNow() {
	if frameFn == nil || inFrame || w.closed || w.width == 0 || w.height == 0 {
		return
	}
	inFrame = true
	if !frameFn() && frameOwner != nil {
		frameOwner.closed = true
	}
	inFrame = false
}

// point32 mirrors Win32 POINT.
type point32 struct{ x, y int32 }

// SetPointerCapture enables relative mouse mode: the cursor hides,
// MouseMove events carry DX/DY, and the pointer recenters each move.
func (w *Window) SetPointerCapture(on bool) {
	if on == w.captured {
		return
	}
	w.captured = on
	if on {
		showCursorW(0)
		w.recenter()
	} else {
		showCursorW(1)
	}
}

// PointerCaptured reports whether relative mouse mode is active.
func (w *Window) PointerCaptured() bool { return w.captured }

func (w *Window) recenter() {
	pt := point32{x: int32(w.width / 2), y: int32(w.height / 2)}
	clientToScreen(w.hwnd, &pt)
	setCursorPos(pt.x, pt.y)
	w.warpSkip = true
}

// idc maps our cursors to Win32 IDC_* standard cursor ids.
var idc = map[Cursor]uintptr{
	CursorDefault: 32512, // IDC_ARROW
	CursorPointer: 32649, // IDC_HAND
	CursorText:    32513, // IDC_IBEAM
	CursorEW:      32644, // IDC_SIZEWE
}

// vkKeys maps Win32 virtual-key codes to Key values.
var vkKeys = map[uintptr]Key{
	0x08: KeyBackspace, 0x09: KeyTab, 0x0D: KeyEnter, 0x1B: KeyEscape,
	0x21: KeyPageUp, 0x22: KeyPageDown,
	0x23: KeyEnd, 0x24: KeyHome,
	0x25: KeyLeft, 0x26: KeyUp, 0x27: KeyRight, 0x28: KeyDown,
	0x2D: KeyInsert, 0x2E: KeyDelete,
	0x70: KeyF1, 0x71: KeyF2, 0x72: KeyF3, 0x73: KeyF4,
	0x74: KeyF5, 0x75: KeyF6, 0x76: KeyF7, 0x77: KeyF8,
	0x78: KeyF9, 0x79: KeyF10, 0x7A: KeyF11, 0x7B: KeyF12,
}

// windows maps HWND→Window for the WndProc; UI is single-goroutine so a
// plain map is fine.
var windows = map[uintptr]*Window{}

var wndProc = sync.OnceValue(func() uintptr {
	return ffi.Callback(func(hwnd, msg, wparam, lparam uintptr) uintptr {
		w := windows[hwnd]
		if inputTrace {
			traceInput(msg, wparam, lparam)
		}
		switch msg {
		case wmClose:
			if w != nil && (w.OnCloseRequest == nil || w.OnCloseRequest()) {
				w.closed = true
			}
			return 0
		case wmSize:
			if w != nil {
				w.width = int(lparam & 0xFFFF)
				w.height = int((lparam >> 16) & 0xFFFF)
				w.frameNow() // live render during the modal resize loop
			}
			return 0
		case wmEnterSizeMove:
			// ~60Hz timer keeps frames flowing while the drag is idle
			// (move-only drags never send WM_SIZE).
			setTimer(hwnd, sizeMoveTimerID, 16, 0)
			if w != nil {
				w.sizing = true
			}
			return 0
		case wmExitSizeMove:
			killTimer(hwnd, sizeMoveTimerID)
			if w != nil {
				w.sizing = false
				w.frameNow() // one final vsync'd frame at the settled size
			}
			return 0
		case wmTimer:
			if w != nil && wparam == sizeMoveTimerID {
				w.frameNow()
				return 0
			}
		case 0x0112: // WM_SYSCOMMAND
			// Unmovable windows refuse the Move command (covers titlebar
			// drags — the modal move loop starts via SC_MOVE); unminimizable
			// ones refuse Minimize (taskbar clicks, Win+Down, system menu).
			if w != nil {
				switch wparam & 0xFFF0 {
				case 0xF010: // SC_MOVE
					if w.unmovable {
						return 0
					}
				case 0xF020: // SC_MINIMIZE
					if w.unminimizable {
						return 0
					}
				}
			}
		case wmDpiChanged:
			// The window moved to a monitor with a different DPI. Windows
			// REQUIRES adopting the suggested rect in lparam verbatim — it
			// is what the modal drag loop expects, and any other rect makes
			// the drag fight the resize (the window keeps its old physical
			// size on the new monitor). CopyRect does the lparam deref on
			// the native side, which keeps vet's unsafeptr check happy.
			if w != nil {
				w.scaleF = float32(wparam&0xFFFF) / 96
				var sug winRect
				if copyRect(&sug, lparam) != 0 {
					setWindowPos(hwnd, 0, sug.left, sug.top,
						sug.right-sug.left, sug.bottom-sug.top,
						0x0004|0x0010 /*SWP_NOZORDER|SWP_NOACTIVATE*/)
				}
			}
			return 0
		case wmSetFocus:
			if w != nil {
				w.focused = true
			}
			return 0
		case wmTouch:
			// Multi-touch: decode every contact into the touch stream. We
			// consume the message (no DefWindowProc), so Windows won't
			// promote the primary contact to mouse messages — mirror it
			// onto the mouse stream ourselves, like the Android backend.
			if w != nil && getTouchInputInfo != nil {
				count := uint32(wparam & 0xFFFF)
				if count > 0 && count <= 64 {
					inputs := make([]touchInput, count)
					if getTouchInputInfo(lparam, count, &inputs[0], int32(unsafe.Sizeof(touchInput{}))) != 0 {
						s := w.Scale()
						for _, ti := range inputs {
							// Hundredths of a physical screen px → client px
							// → logical px.
							pt := point32{x: ti.x / 100, y: ti.y / 100}
							if screenToClient != nil {
								screenToClient(w.hwnd, &pt)
							}
							x, y := float32(pt.x)/s, float32(pt.y)/s
							var phase TouchPhase
							switch {
							case ti.flags&touchEventfDown != 0:
								phase = TouchBegin
							case ti.flags&touchEventfUp != 0:
								phase = TouchEnd
							case ti.flags&touchEventfMove != 0:
								phase = TouchMove
							default:
								continue
							}
							w.touch = append(w.touch, TouchEvent{ID: int(ti.id), Phase: phase, X: x, Y: y})
							if ti.flags&touchEventfPrimary != 0 {
								switch phase {
								case TouchBegin:
									w.mouse = append(w.mouse, MouseEvent{Kind: MouseDown, X: x, Y: y, Button: 1})
								case TouchMove:
									w.mouse = append(w.mouse, MouseEvent{Kind: MouseMove, X: x, Y: y})
								case TouchEnd:
									w.mouse = append(w.mouse, MouseEvent{Kind: MouseUp, X: x, Y: y, Button: 1})
								}
							}
						}
					}
				}
				if closeTouchInputHandle != nil {
					closeTouchInputHandle(lparam)
				}
				return 0
			}
		case wmKillFocus:
			if w != nil {
				w.focused = false
			}
			return 0
		case wmPaint:
			// Note the invalidation, then let DefWindowProc validate the
			// region so Windows stops resending WM_PAINT.
			if w != nil {
				w.damaged = true
			}
		case wmKeyDown, wmKeyUp, wmSysKeyDown, wmSysKeyUp:
			if w != nil {
				down := msg == wmKeyDown || msg == wmSysKeyDown
				shift, ctrl, alt := modifiers()
				if k, ok := vkKeys[wparam]; ok {
					w.keys = append(w.keys, KeyEvent{Down: down, Key: k, Shift: shift, Ctrl: ctrl, Alt: alt})
				} else if down && wparam >= 'A' && wparam <= 'Z' && (ctrl || alt) {
					// Ctrl/Alt+letter shortcut (WM_CHAR is suppressed for
					// these chords).
					w.keys = append(w.keys, KeyEvent{Down: true, Rune: rune(wparam + 32), Ctrl: ctrl, Shift: shift, Alt: alt})
				} else if down && wparam >= '0' && wparam <= '9' && ctrl && !alt {
					// Ctrl+digit (Ctrl+1 …): no WM_CHAR comes for these either.
					// Not with Alt: Ctrl+Alt is AltGr, which types characters
					// (AltGr+2 is '@' on a German layout) through WM_CHAR.
					w.keys = append(w.keys, KeyEvent{Down: true, Rune: rune(wparam), Ctrl: ctrl, Shift: shift, Alt: alt})
				} else if !down {
					// Printable keys arrive as WM_CHAR (Down only) — the
					// release must come from WM_KEYUP, or a game's held-key
					// map (shell.Input.Rune: WASD) never sees the key let go.
					// X11 and the web deliver the release themselves.
					switch {
					case wparam >= 'A' && wparam <= 'Z':
						w.keys = append(w.keys, KeyEvent{Down: false, Rune: rune(wparam + 32), Ctrl: ctrl, Shift: shift, Alt: alt})
					case wparam >= '0' && wparam <= '9', wparam == 0x20:
						w.keys = append(w.keys, KeyEvent{Down: false, Rune: rune(wparam), Ctrl: ctrl, Shift: shift, Alt: alt})
					}
				}
			}
			// Syskeys fall through to DefWindowProc: Alt+F4, Alt+Space and
			// friends must keep their system meaning.
			if msg == wmKeyDown || msg == wmKeyUp {
				return 0
			}
		case wmChar:
			if w != nil && wparam >= 0x20 && wparam != 0x7F {
				r := rune(wparam)
				// WM_CHAR delivers UTF-16 units: recombine surrogate pairs
				// (emoji from the touch keyboard/clipboard) into one rune.
				switch {
				case r >= 0xD800 && r < 0xDC00:
					w.highSurrogate = r
					return 0
				case r >= 0xDC00 && r < 0xE000:
					if w.highSurrogate == 0 {
						return 0 // stray low half
					}
					r = 0x10000 + (w.highSurrogate-0xD800)<<10 + (r - 0xDC00)
					w.highSurrogate = 0
				default:
					w.highSurrogate = 0
				}
				shift, ctrl, alt := modifiers()
				w.keys = append(w.keys, KeyEvent{Down: true, Rune: r, Shift: shift, Ctrl: ctrl, Alt: alt})
			}
			return 0
		case wmImeComposition:
			// IME text input (CJK etc.): when a composition commits, pull the
			// result string and deliver it as rune events. Returning 0 for
			// the result keeps DefWindowProc from re-posting it as WM_CHARs;
			// in-progress composition (preedit) still flows to DefWindowProc
			// so the IME's own floating composition window keeps working.
			if w != nil && lparam&gcsResultStr != 0 && immGetContext != nil {
				if ctx := immGetContext(hwnd); ctx != 0 {
					if n := immGetCompositionStringW(ctx, gcsResultStr, nil, 0); n > 0 {
						buf := make([]uint16, (n+1)/2)
						immGetCompositionStringW(ctx, gcsResultStr, unsafe.Pointer(&buf[0]), uint32(n))
						for _, r := range utf16Decode(buf) {
							w.keys = append(w.keys, KeyEvent{Down: true, Rune: r})
						}
					}
					immReleaseContext(hwnd, ctx)
				}
				return 0
			}
		case wmLButtonDown, wmLButtonUp, wmRButtonDown, wmRButtonUp,
			wmMButtonDown, wmMButtonUp, wmMouseMove:
			if w != nil {
				// Button numbering matches X11 and the DOM: 1 primary,
				// 2 middle, 3 secondary. Windows has a message per
				// button instead of a number, so it is reconstructed
				// here rather than left for every caller to guess.
				kind, button := MouseMove, 0
				switch msg {
				case wmLButtonDown:
					kind, button = MouseDown, 1
				case wmLButtonUp:
					kind, button = MouseUp, 1
				case wmMButtonDown:
					kind, button = MouseDown, 2
				case wmMButtonUp:
					kind, button = MouseUp, 2
				case wmRButtonDown:
					kind, button = MouseDown, 3
				case wmRButtonUp:
					kind, button = MouseUp, 3
				}
				x := int16(lparam & 0xFFFF) // GET_X_LPARAM: signed
				y := int16((lparam >> 16) & 0xFFFF)
				s := w.Scale() // events are delivered in logical pixels
				if kind == MouseMove && w.captured {
					cx, cy := int16(w.width/2), int16(w.height/2)
					if w.warpSkip && x == cx && y == cy {
						w.warpSkip = false
						return 0
					}
					dx, dy := float32(x-cx)/s, float32(y-cy)/s
					if dx != 0 || dy != 0 {
						w.lastX, w.lastY = float32(cx)/s, float32(cy)/s
						w.mouse = append(w.mouse, MouseEvent{Kind: MouseMove,
							X: w.lastX, Y: w.lastY, DX: dx, DY: dy})
						w.recenter()
					}
					return 0
				}
				w.lastX, w.lastY = float32(x)/s, float32(y)/s
				// wparam carries MK_SHIFT (0x4) and MK_CONTROL (0x8) for
				// mouse messages.
				w.mouse = append(w.mouse, MouseEvent{Kind: kind, X: w.lastX, Y: w.lastY,
					Button: button, Shift: wparam&0x4 != 0, Ctrl: wparam&0x8 != 0})
			}
			return 0
		case wmSetCursor:
			// Override the class cursor with our desired shape while the
			// pointer is in the client area (low word of lparam == HTCLIENT).
			if w != nil && lparam&0xFFFF == 1 {
				setCursorW(loadCursorW(0, idc[w.cursor]))
				return 1
			}
			return defWindowProcW(hwnd, msg, wparam, lparam)
		case wmMouseWheel:
			if w != nil {
				// WHEEL_DELTA notches; lparam is screen-relative, so use
				// the last client-space position instead. Shift+wheel
				// scrolls horizontally (the desktop convention; Windows
				// only sends WM_MOUSEHWHEEL for real tilt wheels).
				// A precision touchpad sends a fraction of a notch per
				// event, and a pinch as Ctrl+wheel: the modifiers ride
				// along so a zoom can tell.
				delta := float32(int16(wparam>>16)) / 120
				shift, ctrl, _ := modifiers()
				e := MouseEvent{Kind: MouseScroll, X: w.lastX, Y: w.lastY, Shift: shift, Ctrl: ctrl}
				if shift {
					e.ScrollX = -delta * wheelNotchPx
				} else {
					e.ScrollY = -delta * wheelNotchPx
				}
				w.mouse = append(w.mouse, e)
			}
			return 0
		case wmMouseHWheel:
			if w != nil {
				delta := float32(int16(wparam>>16)) / 120
				w.mouse = append(w.mouse, MouseEvent{
					Kind: MouseScroll, X: w.lastX, Y: w.lastY, ScrollX: delta * wheelNotchPx,
				})
			}
			return 0
		}
		return defWindowProcW(hwnd, msg, wparam, lparam)
	})
})

var classRegistered bool

const className = "gfx-window"

// NewWindow opens and shows a window.
func NewWindow(o Options) (*Window, error) { return newWindow(o, 0) }

// newWindow creates a window, optionally OWNED by another (owner != 0):
// owned windows stay above their owner and skip the taskbar — Win32's
// dialog semantics, used by NewChild.
func newWindow(o Options, owner uintptr) (*Window, error) {
	if err := loadWin32(); err != nil {
		return nil, err
	}
	if o.Width <= 0 {
		o.Width = 800
	}
	if o.Height <= 0 {
		o.Height = 600
	}
	hinstance := getModuleHandleW(0)
	clsName, err := syscall.UTF16PtrFromString(className)
	if err != nil {
		return nil, err
	}
	if !classRegistered {
		cls := wndClassExW{
			style:         0x0020, // CS_OWNDC
			lpfnWndProc:   wndProc(),
			hInstance:     hinstance,
			hCursor:       loadCursorW(0, idcArrow),
			lpszClassName: uintptr(unsafe.Pointer(clsName)),
		}
		cls.cbSize = uint32(unsafe.Sizeof(cls))
		if registerClassExW(&cls) == 0 {
			return nil, fmt.Errorf("app: RegisterClassExW failed")
		}
		classRegistered = true
	}
	title, err := syscall.UTF16PtrFromString(o.Title)
	if err != nil {
		return nil, err
	}
	useDefault := int32(-2147483648) // CW_USEDEFAULT (0x80000000 as signed)
	style := uint32(wsOverlappedWindow)
	if o.Undecorated || o.Fullscreen {
		style = wsPopup
	}
	if o.Unresizable {
		style &^= 0x00040000 | 0x00010000 // WS_THICKFRAME | WS_MAXIMIZEBOX
	}
	if o.Unminimizable {
		style &^= 0x00020000 // WS_MINIMIZEBOX
	}
	if !o.Hidden {
		style |= wsVisible
	}
	// o.Width/o.Height are the desired LOGICAL client size: scale by the
	// DPI (the process is per-monitor aware, so all window units here are
	// physical), then grow by the decoration insets — CreateWindowExW takes
	// the outer size, and passing the client size stretches every surface
	// into a smaller client area.
	dpi := uint32(96)
	if getDpiForSystem != nil {
		if d := getDpiForSystem(); d != 0 {
			dpi = d
		}
	}
	outerFor := func(dpi uint32) (int32, int32) {
		s := float32(dpi) / 96
		r := winRect{0, 0, int32(float32(o.Width) * s), int32(float32(o.Height) * s)}
		ok := int32(0)
		if adjustWindowRectForDpi != nil {
			ok = adjustWindowRectForDpi(&r, style, 0, 0, dpi)
		} else {
			ok = adjustWindowRect(&r, style, 0, 0)
		}
		if ok == 0 {
			return r.right, r.bottom // unadjusted physical client size
		}
		return r.right - r.left, r.bottom - r.top
	}
	outerW, outerH := outerFor(dpi)
	// Borderless fullscreen: the window IS the monitor rect, in physical
	// pixels, so neither the DPI scaling nor the decoration insets above
	// apply. Resolve the target monitor before the HWND exists — the
	// display layout is a process property, not a window one.
	fsMon, haveFS := Monitor{}, false
	if o.Fullscreen {
		if mons, err := Monitors(); err == nil {
			if m, _, ok := pickMonitor(mons, o.X, o.Y, o.Positioned); ok {
				fsMon, haveFS = m, true
				outerW, outerH = int32(m.Width), int32(m.Height)
			}
		}
	}
	exStyle := uint32(0)
	if noRedirectionBitmap() {
		exStyle |= wsExNoRedirBitmap
	}
	// Owned (child) windows open centered within their owner instead of at
	// the OS-default position — dialog placement, matching gpui.
	x, y := useDefault, useDefault
	if o.Positioned {
		x, y = int32(o.X), int32(o.Y)
	}
	if haveFS {
		x, y = int32(fsMon.X), int32(fsMon.Y)
	}
	if owner != 0 && !o.Positioned && !haveFS {
		var or winRect
		if getWindowRect(owner, &or) != 0 {
			x = or.left + (or.right-or.left-outerW)/2
			y = or.top + (or.bottom-or.top-outerH)/2
		}
	}
	hwnd := createWindowExW(exStyle, clsName, title,
		style,
		x, y, outerW, outerH,
		owner, 0, hinstance, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("app: CreateWindowExW failed")
	}
	w := &Window{hwnd: hwnd, hinstance: hinstance, focused: true,
		scaleF: float32(dpi) / 96, unmovable: o.Unmovable, unminimizable: o.Unminimizable,
		fullscreen: o.Fullscreen}
	// The window may have landed on a monitor whose DPI differs from the
	// system DPI used above (mixed-DPI setups): resize to keep the LOGICAL
	// client size, before the window is ever shown. A fullscreen window is
	// sized in physical pixels by definition, so it only adopts the scale.
	if o.Fullscreen && getDpiForWindow != nil {
		if d := getDpiForWindow(hwnd); d != 0 {
			w.scaleF = float32(d) / 96
		}
	} else if getDpiForWindow != nil {
		if d := getDpiForWindow(hwnd); d != 0 && d != dpi {
			w.scaleF = float32(d) / 96
			ow, oh := outerFor(d)
			setWindowPos(hwnd, 0, 0, 0, ow, oh,
				0x0002|0x0004|0x0010 /*SWP_NOMOVE|SWP_NOZORDER|SWP_NOACTIVATE*/)
		}
	}
	// The WM_SIZE sent during CreateWindowExW is missed (windows[hwnd]
	// isn't registered yet) — read the true client size directly.
	var cr winRect
	if getClientRect(hwnd, &cr) != 0 {
		w.width, w.height = int(cr.right-cr.left), int(cr.bottom-cr.top)
	} else {
		w.width, w.height = int(outerW), int(outerH)
	}
	windows[hwnd] = w
	// Receive WM_TOUCH (multi-touch) instead of gestures; wndProc consumes
	// it and synthesizes the primary contact onto the mouse stream itself.
	if registerTouchWindow != nil {
		registerTouchWindow(hwnd, 0)
	}
	return w, nil
}

// Poll pumps pending window messages without blocking. It returns false
// once the user has requested close.
func (w *Window) Poll() bool {
	var msg msgW
	for peekMessageW(&msg, 0, 0, 0, pmRemove) != 0 {
		translateMessage(&msg)
		dispatchMessageW(&msg)
	}
	return !w.closed
}

// Size returns the current client area in PHYSICAL pixels (configure GPU
// surfaces with it); divide by Scale for the logical size layout uses.
func (w *Window) Size() (width, height int) { return w.width, w.height }

// Resizing reports whether an interactive OS move/resize (the modal
// size-move loop) is in progress — the shell drops vsync while it is.
func (w *Window) Resizing() bool { return w.sizing }

// Scale returns the window's DPI scale: physical pixels per logical pixel
// (1 on pre-Win10-1607 systems, where the process stays DPI-unaware).
func (w *Window) Scale() float32 {
	if w.scaleF <= 0 {
		return 1
	}
	return w.scaleF
}

// TouchEvents returns and clears the touch events received since the last
// call (WM_TOUCH; logical pixels). The primary contact is also mirrored
// onto MouseEvents so plain UIs work untouched.
func (w *Window) TouchEvents() []TouchEvent {
	ev := w.touch
	w.touch = nil
	return ev
}

// MouseEvents returns and clears the mouse events received since the last
// call. Drain it once per frame after Poll.
func (w *Window) MouseEvents() []MouseEvent {
	ev := w.mouse
	w.mouse = nil
	return ev
}

// KeyEvents returns and clears the key events received since the last
// call. Drain it once per frame after Poll.
func (w *Window) KeyEvents() []KeyEvent {
	ev := w.keys
	w.keys = nil
	return ev
}

// SetCursor sets the window's mouse-pointer shape; applied on the next
// WM_SETCURSOR.
func (w *Window) SetCursor(c Cursor) { w.cursor = c }

// EachFrame runs fn once per frame until fn returns false or the window is
// closed; pacing comes from the FIFO present inside fn. fn is also exposed
// to the WndProc so frames keep rendering inside OS modal size/move loops.
func (w *Window) EachFrame(fn func() bool) {
	frameFn, frameOwner = fn, w
	defer func() { frameFn, frameOwner = nil, nil }()
	for w.Poll() {
		if !fn() {
			return
		}
	}
}

// TakeDamage reports whether the platform invalidated the window's contents
// (a WM_PAINT) since the last call, and clears the flag. Frame loops that
// skip idle frames must repaint when this fires.
func (w *Window) TakeDamage() bool {
	d := w.damaged
	w.damaged = false
	return d
}

// WaitEvent blocks until a message is queued for this thread or maxWait
// elapses — the idle frame loop's true-0%-CPU sleep (QS_ALLINPUT wakes on
// any input, paint, or posted message).
func (w *Window) WaitEvent(maxWait time.Duration) {
	if w.closed {
		return
	}
	const qsAllInput = 0x04FF
	msgWaitForMultipleObjects(0, 0, 0, uint32(maxWait/time.Millisecond), qsAllInput)
}

// Focused reports whether the window holds keyboard focus.
func (w *Window) Focused() bool { return w.focused }

// Destroy closes the window.
// SetTitle changes the window's title after it has been created.
func (w *Window) SetTitle(title string) {
	if w == nil || w.hwnd == 0 || setWindowTextW == nil {
		return
	}
	p, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	setWindowTextW(w.hwnd, p)
}

func (w *Window) Destroy() {
	if w.hwnd != 0 {
		destroyWindow(w.hwnd)
		delete(windows, w.hwnd)
		w.hwnd = 0
	}
	w.closed = true
}

// HWND and HInstance expose the native handles for GPU surface creation.
// Only the platform glue should touch these.
func (w *Window) HWND() uintptr      { return w.hwnd }
func (w *Window) HInstance() uintptr { return w.hinstance }

// Closed reports whether this window has been closed.
func (w *Window) Closed() bool { return w.closed }

// NewChild opens a second top-level window owned by this one (kept above
// it, no taskbar entry — dialog semantics, matching gpui child windows).
// The shared WndProc routes messages by HWND and Poll pumps the whole
// thread's queue, so children need no extra event plumbing.
func (w *Window) NewChild(o Options) (*Window, error) {
	// A Positioned or Fullscreen child is an independent top-level (a
	// projector window on another monitor): unowned, so it neither stays
	// above the main window nor skips the taskbar.
	if o.Positioned || o.Fullscreen {
		return newWindow(o, 0)
	}
	return newWindow(o, w.hwnd)
}

// CloseChild destroys a child window created with NewChild. Safe to call
// for an already-closed child.
func (w *Window) CloseChild(c *Window) {
	if c == nil || c.hwnd == 0 {
		return
	}
	delete(windows, c.hwnd)
	destroyWindow(c.hwnd)
	c.hwnd = 0
	c.closed = true
}

// Hide withdraws the window from the screen.
func (w *Window) Hide() { showWindow(w.hwnd, 0 /*SW_HIDE*/) }

// Show makes the window visible without stealing foreground focus order
// games rely on (SW_SHOW keeps the normal activation path).
func (w *Window) Show() { showWindow(w.hwnd, 5 /*SW_SHOW*/) }

var _ Host = (*Window)(nil)

// Close requests the window close (frame loop ends like a WM close).
func (w *Window) Close() { w.closed = true }

// Minimize iconifies the window.
func (w *Window) Minimize() { showWindow(w.hwnd, 6) } // SW_MINIMIZE

// ToggleMaximize toggles between maximized and restored.
func (w *Window) ToggleMaximize() {
	if w.maximized {
		showWindow(w.hwnd, 9) // SW_RESTORE
	} else {
		showWindow(w.hwnd, 3) // SW_MAXIMIZE
	}
	w.maximized = !w.maximized
}

// Maximized reports whether the window is maximised (asked of the system:
// a title-bar double-click or an Aero snap counts too).
func (w *Window) Maximized() bool { return isZoomed(w.hwnd) != 0 }

// hit-test codes for WM_NCLBUTTONDOWN-driven move/resize.
var edgeHT = map[ResizeEdge]uintptr{
	EdgeTopLeft: 13, EdgeTop: 12, EdgeTopRight: 14, EdgeRight: 11,
	EdgeBottomRight: 17, EdgeBottom: 15, EdgeBottomLeft: 16, EdgeLeft: 10,
}

// BeginMove starts an OS-driven window move from the current press.
func (w *Window) BeginMove() {
	releaseCapture()
	sendMessageW(w.hwnd, 0xA1 /*WM_NCLBUTTONDOWN*/, 2 /*HTCAPTION*/, 0)
}

// BeginResize starts an OS-driven resize from the given edge.
func (w *Window) BeginResize(edge ResizeEdge) {
	releaseCapture()
	sendMessageW(w.hwnd, 0xA1, edgeHT[edge], 0)
}

type winPoint struct{ x, y int32 }
type winRect struct{ left, top, right, bottom int32 }

// RootPointer returns the cursor position in screen coordinates.
func (w *Window) RootPointer() (x, y int) {
	var pt winPoint
	getCursorPos(&pt)
	return int(pt.x), int(pt.y)
}

// Geometry returns the window's screen-space position and size.
func (w *Window) Geometry() (x, y, width, height int) {
	var r winRect
	getWindowRect(w.hwnd, &r)
	return int(r.left), int(r.top), int(r.right - r.left), int(r.bottom - r.top)
}

// SetGeometry moves and resizes the window (screen coordinates).
func (w *Window) SetGeometry(x, y, width, height int) {
	// Degenerate-size guard only; size policy (resize floors) belongs to
	// the caller — clamping here grew small dialogs on their first move.
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	moveWindow(w.hwnd, int32(x), int32(y), int32(width), int32(height), 1)
}

// SetBackground is X11-only (see the x11 implementation): Win32 erases a
// resized window with the class background brush already.
func (w *Window) SetBackground(uint32) {}

// RetainDisplay is X11-only (keeps the connection open after Destroy);
// a no-op here.
func (w *Window) RetainDisplay() {}
