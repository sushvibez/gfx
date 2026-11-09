//go:build !android

// GOOS=android also sets the linux build tag, so the _linux suffix alone
// would pull X11 into Android builds; exclude it explicitly.

package app

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sushvibez/gfx/ffi"
)

// Phase-0 windowing: X11 via purego (works everywhere on Linux, including
// XWayland). Native Wayland support follows later per DESIGN.md §8; the
// Window API is the part that stays.

// X11 event type codes and masks (X11/X.h).
const (
	xKeyPress        = 2
	xKeyRelease      = 3
	xButtonPress     = 4
	xButtonRelease   = 5
	xMotionNotify    = 6
	xExpose          = 12
	xConfigureNotify = 22
	xClientMessage   = 33

	xKeyPressMask        = 1 << 0
	xKeyReleaseMask      = 1 << 1
	xButtonPressMask     = 1 << 2
	xButtonReleaseMask   = 1 << 3
	xPointerMotionMask   = 1 << 6
	xExposureMask        = 1 << 15
	xStructureNotifyMask = 1 << 17
	xFocusChangeMask     = 1 << 21

	xFocusIn  = 9
	xFocusOut = 10
)

var (
	xOpenDisplay               func(name uintptr) uintptr
	xCloseDisplay              func(d uintptr) int32
	xDefaultRootWindow         func(d uintptr) uintptr
	xCreateSimpleWindow        func(d, parent uintptr, x, y int32, w, h, borderW uint32, border, background uintptr) uintptr
	xCreateWindow              func(d, parent uintptr, x, y int32, w, h, borderW uint32, depth int32, class uint32, visual uintptr, valueMask uint64, attrs *xSetWindowAttributes) uintptr
	xMatchVisualInfo           func(d uintptr, screen, depth, class int32, vinfo *xVisualInfo) int32
	xCreateColormap            func(d, w uintptr, visual uintptr, alloc int32) uintptr
	xGetInputFocus             func(d uintptr, focus *uintptr, revertTo *int32) int32
	xDestroyWindow             func(d, w uintptr) int32
	xSelectInput               func(d, w uintptr, mask int64) int32
	xMapWindow                 func(d, w uintptr) int32
	xUnmapWindow               func(d, w uintptr) int32
	xStoreName                 func(d, w uintptr, name string) int32
	xInternAtom                func(d uintptr, name string, onlyIfExists int32) uintptr
	xChangeProperty            func(d, w, prop, typ uintptr, format, mode int32, data uintptr, nelements int32) int32
	xIconifyWindow             func(d, w uintptr, screen int32) int32
	xDefaultScreen             func(d uintptr) int32
	xSendEvent                 func(d, w uintptr, propagate int32, mask int64, ev *xEvent) int32
	xQueryPointer              func(d, w uintptr, root, child *uintptr, rx, ry, wx, wy *int32, mask *uint32) int32
	xGetGeometry               func(d, w uintptr, root *uintptr, x, y *int32, width, height, border, depth *uint32) int32
	xTranslateCoordinates      func(d, src, dst uintptr, sx, sy int32, dx, dy *int32, child *uintptr) int32
	xMoveResizeWindow          func(d, w uintptr, x, y int32, width, height uint32) int32
	xSetWMProtocols            func(d, w uintptr, protocols *uintptr, count int32) int32
	xPending                   func(d uintptr) int32
	xNextEvent                 func(d uintptr, ev *xEvent) int32
	xConnectionNumber          func(d uintptr) int32
	xResourceManagerString     func(d uintptr) *byte
	xSetWMNormalHints          func(d, w uintptr, hints *xSizeHints)
	xSetTransientForHint       func(d, w, parent uintptr) int32
	xFlush                     func(d uintptr) int32
	xLookupString              func(ev *xEvent, buf *byte, n int32, keysym *uintptr, status uintptr) int32
	xCreateFontCursor          func(d uintptr, shape uint32) uintptr
	xDefineCursor              func(d, w, cursor uintptr) int32
	xSetWindowBackgroundPixmap func(d, w, pixmap uintptr) int32
	xSetWindowBackground       func(d, w uintptr, pixel uint64) int32
	xGrabPointer               func(d, w uintptr, ownerEvents int32, mask uint32, pmode, kmode int32, confineTo, cursor, time uintptr) int32
	xUngrabPointer             func(d, time uintptr) int32
	xWarpPointer               func(d, src, dst uintptr, sx, sy int32, sw, sh uint32, dx, dy int32) int32

	// Optional libXfixes cursor hiding (nil when absent).
	xfixesHideCursor func(d, w uintptr)
	xfixesShowCursor func(d, w uintptr)
)

var loadX11 = sync.OnceValue(func() error {
	lib, err := ffi.Open("libX11.so.6", "libX11.so")
	if err != nil {
		return err
	}
	// Before anything else touches Xlib: the clipboard runs on a
	// connection and a thread of its own (clipboard_x11_linux.go).
	var xInitThreads func() int32
	lib.Bind(&xInitThreads, "XInitThreads")
	xInitThreads()
	lib.Bind(&xOpenDisplay, "XOpenDisplay")
	lib.Bind(&xCloseDisplay, "XCloseDisplay")
	lib.Bind(&xDefaultRootWindow, "XDefaultRootWindow")
	lib.Bind(&xCreateSimpleWindow, "XCreateSimpleWindow")
	lib.Bind(&xCreateWindow, "XCreateWindow")
	lib.Bind(&xMatchVisualInfo, "XMatchVisualInfo")
	lib.Bind(&xCreateColormap, "XCreateColormap")
	lib.Bind(&xGetInputFocus, "XGetInputFocus")
	lib.Bind(&xDestroyWindow, "XDestroyWindow")
	lib.Bind(&xSelectInput, "XSelectInput")
	lib.Bind(&xMapWindow, "XMapWindow")
	lib.Bind(&xUnmapWindow, "XUnmapWindow")
	lib.Bind(&xStoreName, "XStoreName")
	lib.Bind(&xInternAtom, "XInternAtom")
	lib.Bind(&xChangeProperty, "XChangeProperty")
	lib.Bind(&xIconifyWindow, "XIconifyWindow")
	lib.Bind(&xDefaultScreen, "XDefaultScreen")
	lib.Bind(&xSendEvent, "XSendEvent")
	lib.Bind(&xQueryPointer, "XQueryPointer")
	lib.Bind(&xGetGeometry, "XGetGeometry")
	lib.Bind(&xTranslateCoordinates, "XTranslateCoordinates")
	lib.Bind(&xMoveResizeWindow, "XMoveResizeWindow")
	lib.Bind(&xSetWMProtocols, "XSetWMProtocols")
	lib.Bind(&xPending, "XPending")
	lib.Bind(&xNextEvent, "XNextEvent")
	lib.Bind(&xConnectionNumber, "XConnectionNumber")
	lib.Bind(&xResourceManagerString, "XResourceManagerString")
	lib.Bind(&xSetWMNormalHints, "XSetWMNormalHints")
	lib.Bind(&xSetTransientForHint, "XSetTransientForHint")
	lib.Bind(&xFlush, "XFlush")
	lib.Bind(&xLookupString, "XLookupString")
	lib.Bind(&xCreateFontCursor, "XCreateFontCursor")
	lib.Bind(&xDefineCursor, "XDefineCursor")
	lib.Bind(&xSetWindowBackgroundPixmap, "XSetWindowBackgroundPixmap")
	lib.Bind(&xSetWindowBackground, "XSetWindowBackground")
	lib.Bind(&xGrabPointer, "XGrabPointer")
	lib.Bind(&xUngrabPointer, "XUngrabPointer")
	lib.Bind(&xWarpPointer, "XWarpPointer")
	if fx, err := ffi.Open("libXfixes.so.3", "libXfixes.so"); err == nil {
		fx.Bind(&xfixesHideCursor, "XFixesHideCursor")
		fx.Bind(&xfixesShowCursor, "XFixesShowCursor")
	}
	return nil
})

// X11 cursor font shapes (X11/cursorfont.h).
var x11CursorShape = map[Cursor]uint32{
	CursorDefault: 68,  // XC_left_ptr
	CursorPointer: 60,  // XC_hand2
	CursorText:    152, // XC_xterm
	CursorEW:      108, // XC_sb_h_double_arrow
}

// X11 keysyms for editing/navigation keys (X11/keysymdef.h).
var x11Keys = map[uintptr]Key{
	0xFF08: KeyBackspace,
	0xFF09: KeyTab,
	0xFE20: KeyTab, // ISO_Left_Tab: what X delivers for Shift+Tab
	0xFF0D: KeyEnter,
	0xFF1B: KeyEscape,
	0xFF50: KeyHome,
	0xFF51: KeyLeft,
	0xFF52: KeyUp,
	0xFF53: KeyRight,
	0xFF54: KeyDown,
	0xFF57: KeyEnd,
	0xFFFF: KeyDelete,
	0xFF8D: KeyEnter, // keypad enter
}

// xEvent mirrors XEvent: a 192-byte union; fields are read at known offsets.
type xEvent struct {
	data [24]uint64
}

func (e *xEvent) kind() int32 { return *(*int32)(unsafe.Pointer(&e.data[0])) }

// window is the XAnyEvent.window field (offset 32 on 64-bit = data[4]) — which
// window the event targets, for routing in a multi-window connection.
func (e *xEvent) window() uint64 { return e.data[4] }

// XClientMessageEvent.data.l[0] (offset 56 on 64-bit).
func (e *xEvent) clientData0() uint64 { return e.data[7] }

// XConfigureEvent.width / .height (offsets 56 and 60).
func (e *xEvent) configureSize() (w, h int32) {
	w = *(*int32)(unsafe.Pointer(&e.data[7]))
	h = *(*int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&e.data[7])) + 4))
	return
}

// XButtonEvent/XMotionEvent .x/.y (offsets 64 and 68) and .button (offset 84).
func (e *xEvent) pointerXY() (x, y int32) {
	x = *(*int32)(unsafe.Pointer(&e.data[8]))
	y = *(*int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&e.data[8])) + 4))
	return
}

func (e *xEvent) button() int32 {
	return *(*int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&e.data[10])) + 4))
}

// XKeyEvent.state (offset 80) carries modifier masks: ShiftMask=1,
// ControlMask=4.
func (e *xEvent) keyState() uint32 { return *(*uint32)(unsafe.Pointer(&e.data[10])) }

// xVisualInfo mirrors Xlib's XVisualInfo (LP64 layout).
type xVisualInfo struct {
	visual       uintptr // Visual*
	visualid     uint64
	screen       int32
	depth        int32
	class        int32
	_            int32 // padding before the ulong masks
	redMask      uint64
	greenMask    uint64
	blueMask     uint64
	colormapSize int32
	bitsPerRGB   int32
}

// xSetWindowAttributes mirrors Xlib's XSetWindowAttributes (LP64 layout).
type xSetWindowAttributes struct {
	backgroundPixmap   uintptr
	backgroundPixel    uint64
	borderPixmap       uintptr
	borderPixel        uint64
	bitGravity         int32
	winGravity         int32
	backingStore       int32
	_                  int32
	backingPlanes      uint64
	backingPixel       uint64
	saveUnder          int32
	_                  int32
	eventMask          int64
	doNotPropagateMask int64
	overrideRedirect   int32
	_                  int32
	colormap           uintptr
	cursor             uintptr
}

// Options configures a new window.
type Options struct {
	Title  string
	Width  int
	Height int
	// Undecorated removes the window manager's title bar and borders —
	// for apps that draw their own chrome (custom titlebars).
	Undecorated bool
	// Transparent creates the window with a 32-bit ARGB visual so the
	// compositor honors the framebuffer's alpha channel — for client-side
	// decorations with soft shadows over the desktop. X11-only today.
	Transparent bool
	// Hidden creates the window without showing it; Show() maps it after
	// the first present so GPU/font init never flashes a blank window.
	Hidden bool
	// Unmovable / Unresizable / Unminimizable pin position / size /
	// iconify (gpui's is_movable & co): Motif function hints plus, for
	// Unresizable, WM_NORMAL_HINTS min==max. WM-cooperative, like all X11
	// window management.
	Unmovable     bool
	Unresizable   bool
	Unminimizable bool
	// X, Y place the window at a virtual-desktop pixel position — the
	// coordinate space Monitors() and Geometry() use, so a window opens on
	// a chosen monitor by naming a point inside it. Honored only when
	// Positioned is true, so the zero Options still means "let the window
	// manager decide".
	X, Y       int
	Positioned bool
	// Fullscreen opens the window borderless over the whole monitor that
	// contains (X, Y) — or the primary monitor when Positioned is false.
	// No display mode is changed; this is the borderless-fullscreen shape
	// every modern app uses.
	Fullscreen bool
}

// xSizeHints mirrors Xlib's XSizeHints (LP64: long flags, then ints).
type xSizeHints struct {
	flags                  int64
	x, y, width, height    int32 // legacy fields, unused
	minWidth, minHeight    int32
	maxWidth, maxHeight    int32
	widthInc, heightInc    int32
	minAspectX, minAspectY int32
	maxAspectX, maxAspectY int32
	baseWidth, baseHeight  int32
	winGravity             int32
	_                      int32 // pad to the C struct size
}

// Window is a native window plus its pending-event pump.
type Window struct {
	display  uintptr
	win      uintptr
	wmDelete uintptr
	width    int
	height   int
	closed   bool
	// OnCloseRequest, when set, runs when the user asks the window manager
	// to close the window (the title bar's ×, Alt+F4, WM_DELETE_WINDOW);
	// returning false keeps the window open (an unsaved-changes dialog).
	// Close() always closes.
	OnCloseRequest func() bool
	// mapped is whether the window is on screen yet. A window created
	// Hidden is not, and the difference matters to the window manager:
	// it does not manage an unmapped window, so a _NET_WM_STATE request
	// sent about one is simply dropped.
	mapped  bool
	maxed   bool // last ToggleMaximize state (Maximized)
	mouse   []MouseEvent
	keys    []KeyEvent
	cursor  Cursor
	cursors map[Cursor]uintptr

	// Pointer capture (relative mouse look): motion reports deltas from the
	// window center and the cursor warps back each event.
	captured bool
	warpSkip bool // ignore the motion event the warp itself generates

	transparent bool // ARGB visual: the pixels we do not own stay clear
	damaged     bool // an Expose arrived: contents need repainting (TakeDamage)
	focused     bool // keyboard focus (FocusIn/FocusOut); animations may pause unfocused

	scale float32 // DPI scale (physical px per logical px); Xft.dpi/96 or GFX_SCALE

	// Multi-window: the primary owns the display and routes events to all
	// windows (itself + siblings) by window id. Siblings share the display and
	// don't poll independently.
	siblings      []*Window
	ownsDisp      bool
	retainDisplay bool // see RetainDisplay

	fullscreen bool // last state asked for (Options.Fullscreen/SetFullscreen)
}

// NewWindow opens and maps a window. Phase-0 X11; the constructor is the
// platform seam — the rest of the API is portable.
// SetTitle changes the window's title after it has been created — a
// game whose window says what it is currently showing.
func (w *Window) SetTitle(title string) {
	if w == nil || w.display == 0 || w.win == 0 {
		return
	}
	setWindowTitle(w.display, w.win, title)
	xFlush(w.display)
}

// setWindowTitle publishes a window title in both places a window
// manager may look.
//
// XStoreName sets WM_NAME, which is a Latin-1 STRING: anything outside
// it — an em dash in a title, say — arrives as mojibake. Modern window
// managers prefer _NET_WM_NAME, which is UTF-8, so the title goes there
// as written and WM_NAME gets an ASCII-folded copy for anything old
// enough to still read it.
func setWindowTitle(display, win uintptr, title string) {
	netName := xInternAtom(display, "_NET_WM_NAME", 0)
	utf8Atom := xInternAtom(display, "UTF8_STRING", 0)
	if netName != 0 && utf8Atom != 0 {
		b := append([]byte(title), 0)
		xChangeProperty(display, win, netName, utf8Atom, 8, 0, /*PropModeReplace*/
			uintptr(unsafe.Pointer(&b[0])), int32(len(title)))
	}
	xStoreName(display, win, asciiFold(title))
}

// asciiFold replaces the punctuation that shows up in titles with ASCII
// that Latin-1 can carry, and drops anything else non-ASCII.
func asciiFold(s string) string {
	repl := map[rune]string{
		'\u2014': "-", '\u2013': "-", '\u2018': "'", '\u2019': "'",
		'\u201c': `"`, '\u201d': `"`, '\u2026': "...", '\u00b7': "-",
		'\u00d7': "x", '\u00b0': " deg", '\u2192': "->", '\u2605': "*",
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case repl[r] != "":
			b.WriteString(repl[r])
		}
	}
	return b.String()
}

func NewWindow(o Options) (*Window, error) {
	if err := loadX11(); err != nil {
		return nil, err
	}
	display := xOpenDisplay(0)
	if display == 0 {
		return nil, fmt.Errorf("app: cannot open X display (is DISPLAY set?)")
	}
	if o.Width <= 0 {
		o.Width = 800
	}
	if o.Height <= 0 {
		o.Height = 600
	}
	w, err := createWindow(display, o)
	if err != nil {
		xCloseDisplay(display)
		return nil, err
	}
	w.ownsDisp = true
	// Monitors() reuses the first window's connection rather than opening
	// its own (see procDisplay).
	if procDisplay == 0 {
		procDisplay = display
	}
	return w, nil
}

// createWindow makes a top-level window on an existing display connection
// (without owning the display). Shared by NewWindow and NewChild.
func createWindow(display uintptr, o Options) (*Window, error) {
	if o.Width <= 0 {
		o.Width = 800
	}
	if o.Height <= 0 {
		o.Height = 600
	}
	// Options sizes are LOGICAL pixels; the window is created at physical
	// size. X11 has no per-monitor DPI events — Xft.dpi at creation is the
	// desktop convention (GFX_SCALE overrides for testing).
	scale := scaleOverride()
	if scale <= 0 {
		scale = xftDPIScale(display)
	}
	o.Width = int(float32(o.Width)*scale + 0.5)
	o.Height = int(float32(o.Height)*scale + 0.5)
	// Placement. Options.X/Y are already virtual-desktop PHYSICAL pixels
	// (the space Monitors() reports), so they are never scaled. A
	// fullscreen window takes its target monitor's rect verbatim.
	posX, posY := int32(0), int32(0)
	positioned := o.Positioned
	monIdx := -1
	if o.Positioned {
		posX, posY = int32(o.X), int32(o.Y)
	}
	if o.Fullscreen {
		if mons, err := monitorsOn(display); err == nil {
			if m, i, ok := pickMonitor(mons, o.X, o.Y, o.Positioned); ok {
				monIdx = i
				posX, posY = int32(m.X), int32(m.Y)
				o.Width, o.Height = m.Width, m.Height
				positioned = true
			}
		}
		// Undecorated as well as EWMH-fullscreen: a window manager that
		// ignores _NET_WM_STATE_FULLSCREEN (or none at all, which is a real
		// configuration for a projector wall) still ends up with a
		// borderless window exactly covering the monitor rect.
		o.Undecorated = true
	}
	root := xDefaultRootWindow(display)
	var win uintptr
	if o.Transparent {
		// 32-bit ARGB visual + matching colormap: the compositor blends the
		// window by its framebuffer alpha (transparent where we clear a=0).
		var vi xVisualInfo
		if xMatchVisualInfo(display, xDefaultScreen(display), 32, 4 /*TrueColor*/, &vi) == 0 {
			return nil, fmt.Errorf("app: no 32-bit TrueColor visual (compositor off?)")
		}
		cmap := xCreateColormap(display, root, vi.visual, 0 /*AllocNone*/)
		attrs := xSetWindowAttributes{colormap: cmap} // border/background pixel 0
		const cwBackPixel, cwBorderPixel, cwColormap = 1 << 1, 1 << 3, 1 << 13
		win = xCreateWindow(display, root, posX, posY, uint32(o.Width), uint32(o.Height), 0,
			int32(vi.depth), 1 /*InputOutput*/, vi.visual,
			cwBackPixel|cwBorderPixel|cwColormap, &attrs)
	} else {
		win = xCreateSimpleWindow(display, root, posX, posY, uint32(o.Width), uint32(o.Height), 0, 0, 0)
	}
	if win == 0 {
		return nil, fmt.Errorf("app: XCreateWindow failed")
	}
	// Background None (0): the X server won't repaint the window on resize,
	// so there's no black flash before our frame presents (we own all
	// pixels via wgpu).
	xSetWindowBackgroundPixmap(display, win, 0)
	xSelectInput(display, win, xExposureMask|xStructureNotifyMask|xFocusChangeMask|xKeyPressMask|
		xKeyReleaseMask|xButtonPressMask|xButtonReleaseMask|xPointerMotionMask)
	if o.Title != "" {
		setWindowTitle(display, win, o.Title)
	}
	wmDelete := xInternAtom(display, "WM_DELETE_WINDOW", 0)
	xSetWMProtocols(display, win, &wmDelete, 1)
	if o.Undecorated || o.Unmovable || o.Unresizable || o.Unminimizable {
		// Motif WM hints: flags bit 1 = functions, bit 2 = decorations.
		// Functions are an allow-list (RESIZE=2 MOVE=4 MINIMIZE=8
		// MAXIMIZE=16 CLOSE=32); window hints strip entries from it.
		var flags, funcs, deco uint64
		if o.Undecorated {
			flags |= 2 // decorations = 0
		}
		if o.Unmovable || o.Unresizable || o.Unminimizable {
			flags |= 1
			funcs = 2 | 4 | 8 | 16 | 32
			if o.Unmovable {
				funcs &^= 4
			}
			if o.Unresizable {
				funcs &^= 2 | 16
			}
			if o.Unminimizable {
				funcs &^= 8
			}
		}
		hintsAtom := xInternAtom(display, "_MOTIF_WM_HINTS", 0)
		hints := [5]uint64{flags, funcs, deco, 0, 0}
		xChangeProperty(display, win, hintsAtom, hintsAtom, 32, 0, /*PropModeReplace*/
			uintptr(unsafe.Pointer(&hints[0])), 5)
	}
	if o.Unresizable || positioned {
		var h xSizeHints
		if positioned {
			// USPosition|PPosition|USSize|PSize: the create-time x/y are only
			// a request until WM_NORMAL_HINTS says the USER chose them —
			// without the US flags most window managers re-place the window
			// by their own policy and it lands on the wrong head.
			h.flags |= 1<<0 | 1<<1 | 1<<2 | 1<<3
			h.x, h.y = posX, posY
			h.width, h.height = int32(o.Width), int32(o.Height)
		}
		if o.Unresizable {
			// WM_NORMAL_HINTS with min == max pins the size (the portable
			// way; the Motif RESIZE strip above only removes the WM's
			// affordances).
			h.flags |= 1<<4 | 1<<5 // PMinSize|PMaxSize
			h.minWidth, h.minHeight = int32(o.Width), int32(o.Height)
			h.maxWidth, h.maxHeight = int32(o.Width), int32(o.Height)
		}
		xSetWMNormalHints(display, win, &h)
	}
	if o.Fullscreen {
		// _NET_WM_STATE set as a PROPERTY before mapping is how the EWMH
		// spec has a client open already-fullscreen; the ClientMessage form
		// below only works on a mapped window.
		state := xInternAtom(display, "_NET_WM_STATE", 0)
		fs := xInternAtom(display, "_NET_WM_STATE_FULLSCREEN", 0)
		atoms := [1]uint64{uint64(fs)}
		const xaAtom = 4                                    // XA_ATOM
		xChangeProperty(display, win, state, xaAtom, 32, 0, /*PropModeReplace*/
			uintptr(unsafe.Pointer(&atoms[0])), 1)
	}
	// Hidden windows stay unmapped until Show() — the shell shows after the
	// first present so GPU/font init never flashes a blank window. Vulkan
	// surface creation on an unmapped X window is legal.
	if !o.Hidden {
		xMapWindow(display, win)
	}
	if positioned {
		// Belt and braces: the WM may have placed the window itself despite
		// the hints. Moving after the map is the request it does honor.
		xMoveResizeWindow(display, win, posX, posY, uint32(o.Width), uint32(o.Height))
	}
	xFlush(display)
	// Initial focus from the server: a window launched into the background
	// may never receive a FocusOut, so assuming focus would stick forever.
	// The WM's FocusIn arrives if/when it actually focuses us.
	var focusWin uintptr
	var revert int32
	xGetInputFocus(display, &focusWin, &revert)
	w := &Window{
		display:     display,
		win:         win,
		wmDelete:    wmDelete,
		width:       o.Width,
		height:      o.Height,
		scale:       scale,
		transparent: o.Transparent,
		focused:     focusWin == win,
		fullscreen:  o.Fullscreen,
	}
	w.mapped = !o.Hidden
	if o.Fullscreen && !o.Hidden {
		// Now that the window is mapped the WM accepts the state change
		// (idempotent with the pre-map property) and the monitor pin.
		fs := xInternAtom(display, "_NET_WM_STATE_FULLSCREEN", 0)
		w.netWMState(1, fs, 0)
		if monIdx >= 0 {
			w.setFullscreenMonitors(monIdx)
		}
	}
	return w, nil
}

// NewChild opens a second top-level window sharing this window's X display
// connection. The primary window routes events to children in its Poll, so
// only the primary should be polled; children are read via their own
// MouseEvents/KeyEvents/Size. Returns an error if called on a non-primary.
func (w *Window) NewChild(o Options) (*Window, error) {
	if !w.ownsDisp {
		return nil, fmt.Errorf("app: NewChild must be called on the primary window")
	}
	c, err := createWindow(w.display, o)
	if err != nil {
		return nil, err
	}
	// Owned child (Windows parity): transient-for keeps it above its parent
	// and groups minimize; place it centered in the parent. A Positioned or
	// Fullscreen child is an independent top-level (a projector window on
	// another monitor): no transient hint, no re-centering.
	independent := o.Positioned || o.Fullscreen
	if !independent {
		xSetTransientForHint(w.display, c.win, w.win)
	}
	var root uintptr
	var px, py int32
	var pw, ph, bw, depth uint32
	if !independent && xGetGeometry(w.display, w.win, &root, &px, &py, &pw, &ph, &bw, &depth) != 0 {
		var ax, ay int32
		var child uintptr
		xTranslateCoordinates(w.display, w.win, root, 0, 0, &ax, &ay, &child)
		cx := ax + int32(pw)/2 - int32(c.width)/2
		cy := ay + int32(ph)/2 - int32(c.height)/2
		xMoveResizeWindow(w.display, c.win, cx, cy, uint32(c.width), uint32(c.height))
		xFlush(w.display)
	}
	w.siblings = append(w.siblings, c)
	return c, nil
}

// Poll pumps pending window events without blocking. It returns false once
// the user has requested close, which makes `for win.Poll() { ... }` the
// frame loop shape.
func (w *Window) Poll() bool {
	if w.closed && len(w.siblings) == 0 {
		return false
	}
	var ev xEvent
	for xPending(w.display) > 0 {
		xNextEvent(w.display, &ev)
		// Route to the targeted window (self or a sibling); the primary owns
		// the single event queue for the whole connection.
		target := w
		if id := ev.window(); id != uint64(w.win) {
			for _, s := range w.siblings {
				if id == uint64(s.win) {
					target = s
					break
				}
			}
		}
		target.apply(&ev)
	}
	// The connection runs while the primary window is open. Sibling windows
	// close individually — callers watch Closed() and tear them down (the
	// game shell treats a closed second screen as quit; the UI shell just
	// closes that child).
	return !w.closed
}

// Closed reports whether this window has been closed (by the user or
// Destroy). Frame loops use it to tear down child windows.
func (w *Window) Closed() bool { return w.closed }

// CloseChild destroys a child window created with NewChild and removes it
// from event routing. Safe to call for an already-closed child.
func (w *Window) CloseChild(c *Window) {
	for i, s := range w.siblings {
		if s == c {
			w.siblings = append(w.siblings[:i], w.siblings[i+1:]...)
			break
		}
	}
	if c.win != 0 {
		xDestroyWindow(w.display, c.win)
		c.win = 0
	}
	c.closed = true
	c.display = 0
}

// apply updates a single window's state/queues from one event.
func (w *Window) apply(ev *xEvent) {
	switch ev.kind() {
	case xClientMessage:
		if uintptr(ev.clientData0()) == w.wmDelete && (w.OnCloseRequest == nil || w.OnCloseRequest()) {
			w.closed = true
		}
	case xConfigureNotify:
		cw, ch := ev.configureSize()
		if cw > 0 && ch > 0 {
			w.width, w.height = int(cw), int(ch)
		}
	case xExpose:
		w.damaged = true
	case xFocusIn:
		w.focused = true
	case xFocusOut:
		w.focused = false
	case xButtonPress, xButtonRelease:
		x, y := ev.pointerXY()
		btn := ev.button()
		// X11 buttons 4/5 are the vertical wheel, 6/7 the horizontal tilt;
		// one event per press notch. Shift+wheel scrolls horizontally (the
		// desktop convention; XButtonEvent.state shares XKeyEvent's offset).
		if btn >= 4 && btn <= 7 {
			if ev.kind() == xButtonPress {
				// One notch is what Qt scrolls for one: three lines of
				// twenty pixels (QStyleHints::wheelScrollLines), which is
				// also the desktop convention.
				const notch = wheelNotchPx
				const shiftMask = 1
				e := MouseEvent{Kind: MouseScroll, X: float32(x) / w.Scale(), Y: float32(y) / w.Scale()}
				horizontal := ev.keyState()&shiftMask != 0
				switch btn {
				case 4:
					e.ScrollY = -notch
				case 5:
					e.ScrollY = notch
				case 6:
					e.ScrollX = -notch
				case 7:
					e.ScrollX = notch
				}
				if horizontal && e.ScrollY != 0 {
					e.ScrollX, e.ScrollY = e.ScrollY, 0
				}
				w.mouse = append(w.mouse, e)
			}
			return
		}
		kind := MouseDown
		if ev.kind() == xButtonRelease {
			kind = MouseUp
		}
		w.mouse = append(w.mouse, MouseEvent{Kind: kind, X: float32(x) / w.Scale(), Y: float32(y) / w.Scale(),
			Button: int(btn), Shift: ev.keyState()&1 != 0, Ctrl: ev.keyState()&4 != 0})
	case xMotionNotify:
		x, y := ev.pointerXY()
		if w.captured {
			cx, cy := int32(w.width/2), int32(w.height/2)
			if w.warpSkip && x == cx && y == cy {
				w.warpSkip = false
				return
			}
			dx, dy := float32(x-cx), float32(y-cy)
			if dx != 0 || dy != 0 {
				s := w.Scale()
				w.mouse = append(w.mouse, MouseEvent{Kind: MouseMove,
					X: float32(cx) / s, Y: float32(cy) / s, DX: dx / s, DY: dy / s})
				xWarpPointer(w.display, 0, w.win, 0, 0, 0, 0, cx, cy)
				w.warpSkip = true
				xFlush(w.display)
			}
			return
		}
		w.mouse = append(w.mouse, MouseEvent{Kind: MouseMove, X: float32(x) / w.Scale(), Y: float32(y) / w.Scale(),
			Shift: ev.keyState()&1 != 0, Ctrl: ev.keyState()&4 != 0})
	case xKeyPress, xKeyRelease:
		var buf [8]byte
		var keysym uintptr
		n := xLookupString(ev, &buf[0], int32(len(buf)), &keysym, 0)
		state := ev.keyState()
		ke := KeyEvent{
			Down:  ev.kind() == xKeyPress,
			Key:   x11Keys[keysym],
			Shift: state&1 != 0,
			Ctrl:  state&4 != 0,
		}
		// XLookupString yields Latin-1; printable range only. With
		// Ctrl held, suppress the control-char rune (e.g. ^A = 0x01).
		if ke.Key == KeyNone && !ke.Ctrl && n > 0 && buf[0] >= 0x20 && buf[0] != 0x7F {
			ke.Rune = rune(buf[0])
		}
		// Surface Ctrl+letter as the bare letter rune for shortcuts.
		if ke.Ctrl && keysym >= 'a' && keysym <= 'z' {
			ke.Rune = rune(keysym)
		}
		if ke.Key != KeyNone || ke.Rune != 0 {
			w.keys = append(w.keys, ke)
		}
	}
}

// KeyEvents returns and clears the key events received since the last
// call. Drain it once per frame after Poll.
func (w *Window) KeyEvents() []KeyEvent {
	ev := w.keys
	w.keys = nil
	return ev
}

// MouseEvents returns and clears the mouse events received since the last
// call. Drain it once per frame after Poll.
func (w *Window) MouseEvents() []MouseEvent {
	ev := w.mouse
	w.mouse = nil
	return ev
}

// SetCursor sets the window's mouse-pointer shape (cached per shape).
func (w *Window) SetCursor(c Cursor) {
	if c == w.cursor {
		return
	}
	w.cursor = c
	if w.cursors == nil {
		w.cursors = map[Cursor]uintptr{}
	}
	cur, ok := w.cursors[c]
	if !ok {
		cur = xCreateFontCursor(w.display, x11CursorShape[c])
		w.cursors[c] = cur
	}
	xDefineCursor(w.display, w.win, cur)
	xFlush(w.display)
}

// SetPointerCapture grabs (or releases) the pointer for relative mouse
// look: while captured, the cursor hides, MouseMove events carry DX/DY
// deltas, and the pointer stays centered in the window.
func (w *Window) SetPointerCapture(on bool) {
	if on == w.captured || w.display == 0 {
		return
	}
	w.captured = on
	if on {
		const mask = xButtonPressMask | xButtonReleaseMask | xPointerMotionMask
		xGrabPointer(w.display, w.win, 1, mask, 1 /*GrabModeAsync*/, 1, w.win, 0, 0)
		if xfixesHideCursor != nil {
			xfixesHideCursor(w.display, w.win)
		}
		xWarpPointer(w.display, 0, w.win, 0, 0, 0, 0, int32(w.width/2), int32(w.height/2))
		w.warpSkip = true
	} else {
		xUngrabPointer(w.display, 0)
		if xfixesShowCursor != nil {
			xfixesShowCursor(w.display, w.win)
		}
	}
	xFlush(w.display)
}

// PointerCaptured reports whether relative mouse mode is active.
func (w *Window) PointerCaptured() bool { return w.captured }

// Size returns the current client area in pixels.
func (w *Window) Size() (width, height int) { return w.width, w.height }

// Scale is the DPI scale (physical px per logical px); 1 on this backend.
func (w *Window) Scale() float32 {
	if w.scale <= 0 {
		return 1
	}
	return w.scale
}

// Resizing reports an interactive OS resize in progress; the WM drives
// resizes out-of-process on X11, so it is always false here.
func (w *Window) Resizing() bool { return false }

// ClipboardText / SetClipboardText: clipboard_x11_linux.go.

// Hide unmaps the window (removes it from screen); Show maps it again. Used to
// defer a secondary window until it's needed (e.g. open on mission launch).
// SetBackground gives the window a solid background COLOUR for the pixels
// the X server exposes before our next frame reaches the screen.
//
// NOT used by the shell, and measured before it was left out: the server
// then clears the WHOLE window on every ConfigureNotify, so an
// interactive resize — thirty configures a second — shows the background
// colour far more than it shows the app. Undefined slivers at the growing
// edge are the lesser evil; the real fix is a repaint fast enough not to
// be seen (see the SVG triangulation cache).
//
// Ignored on a transparent (ARGB) window, whose whole point is that the
// pixels it does not own stay clear.
func (w *Window) SetBackground(rgb uint32) {
	if w.display == 0 || w.win == 0 || w.transparent || xSetWindowBackground == nil {
		return
	}
	xSetWindowBackground(w.display, w.win, uint64(rgb&0xffffff))
}

func (w *Window) Hide() {
	if w.display != 0 && w.win != 0 {
		xUnmapWindow(w.display, w.win)
		xFlush(w.display)
	}
}

// Show maps the window onto the screen.
func (w *Window) Show() {
	if w.display != 0 && w.win != 0 {
		xMapWindow(w.display, w.win)
		w.mapped = true
		xFlush(w.display)
	}
}

// EachFrame runs fn once per frame until fn returns false or the window is
// closed. This is the portable loop shape: native pacing comes from the
// FIFO present blocking inside fn; on web the browser's requestAnimationFrame
// drives it.
func (w *Window) EachFrame(fn func() bool) {
	for w.Poll() {
		if !fn() {
			return
		}
	}
}

// xftDPIScale reads Xft.dpi from the root window's resource string
// (xrdb; what desktops set for text scaling) and returns dpi/96, or 1.
func xftDPIScale(display uintptr) float32 {
	p := xResourceManagerString(display)
	if p == nil {
		return 1
	}
	// The resource string is NUL-terminated "key:\tvalue\n" lines.
	var sb []byte
	for ptr := unsafe.Pointer(p); *(*byte)(ptr) != 0; ptr = unsafe.Pointer(uintptr(ptr) + 1) {
		sb = append(sb, *(*byte)(ptr))
	}
	for _, line := range strings.Split(string(sb), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) != "Xft.dpi" {
			continue
		}
		if dpi, err := strconv.ParseFloat(strings.TrimSpace(v), 32); err == nil && dpi > 0 {
			return float32(dpi) / 96
		}
	}
	return 1
}

// WaitEvent blocks until an event is pending on the display connection or
// maxWait elapses — the idle frame loop's true-0%-CPU sleep. The connection
// is shared by sibling windows, so any window's input, expose, configure or
// focus traffic wakes it. Same-goroutine state can't change while blocked
// and animations disable idle skipping, so nothing repaint-worthy can happen
// without an X event; maxWait only bounds staleness against misuse (property
// writes from foreign goroutines without Post).
func (w *Window) WaitEvent(maxWait time.Duration) {
	if w.display == 0 || w.closed || xPending(w.display) > 0 {
		return
	}
	fd := xConnectionNumber(w.display)
	if fd < 0 || int(fd) >= 64*len(syscall.FdSet{}.Bits) {
		time.Sleep(4 * time.Millisecond) // can't select: degrade to the old poll pace
		return
	}
	var rf syscall.FdSet
	rf.Bits[fd/64] |= 1 << (uint(fd) % 64)
	tv := syscall.NsecToTimeval(maxWait.Nanoseconds())
	_, _ = syscall.Select(int(fd)+1, &rf, nil, nil, &tv)
}

// TouchEvents reports multi-touch input; the X11 backend has none wired yet
// (XInput2 touch needs hardware to validate against — mouse input covers
// desktop Linux). Backends with touch: android, web, windows (WM_TOUCH).
func (w *Window) TouchEvents() []TouchEvent { return nil }

// TakeDamage reports whether the platform invalidated the window's contents
// (an Expose) since the last call, and clears the flag. Frame loops that skip
// idle frames must repaint when this fires.
func (w *Window) TakeDamage() bool {
	d := w.damaged
	w.damaged = false
	return d
}

// Focused reports whether the window holds keyboard focus — frame loops may
// pause time-driven content (GIFs) for unfocused windows.
func (w *Window) Focused() bool { return w.focused }

// Destroy closes the window (and any sibling windows), then the shared display
// connection if this window owns it.
func (w *Window) Destroy() {
	if w.display == 0 {
		w.closed = true
		return
	}
	if w.win != 0 {
		xDestroyWindow(w.display, w.win)
		w.win = 0
	}
	for _, s := range w.siblings {
		if s.win != 0 {
			xDestroyWindow(w.display, s.win)
			s.win = 0
		}
		s.closed, s.display = true, 0
	}
	if w.ownsDisp && !w.retainDisplay {
		if procDisplay == w.display {
			procDisplay = 0
		}
		xCloseDisplay(w.display)
	}
	w.display = 0
	w.closed = true
}

// RetainDisplay keeps the X connection open after Destroy. A Vulkan ICD
// registers a close-display hook on the connection a surface was made
// from; releasing the GPU instance unloads the ICD, and XCloseDisplay
// after that jumps into unmapped code (every example that returned from
// its frame loop died this way, in the deferred Destroy). The GPU surface
// glue calls this; the connection goes with the process.
func (w *Window) RetainDisplay() { w.retainDisplay = true }

// XlibDisplay and XlibWindow expose the native handles for GPU surface
// creation. Only the platform glue should touch these.
func (w *Window) XlibDisplay() uintptr { return w.display }
func (w *Window) XlibWindow() uint64   { return uint64(w.win) }

var _ Host = (*Window)(nil)

// Close requests the window close: the frame loop observes it the same way
// as a WM close (EachFrame ends). For apps drawing their own titlebar.
func (w *Window) Close() { w.closed = true }

// Minimize iconifies the window.
func (w *Window) Minimize() {
	xIconifyWindow(w.display, w.win, xDefaultScreen(w.display))
	xFlush(w.display)
}

// ToggleMaximize asks the window manager to toggle maximization (EWMH
// _NET_WM_STATE with both MAXIMIZED atoms).
// ToggleMaximize maximises the window, or restores it if it already is.
//
// On a window that has not been mapped yet it also SHOWS it, maximised.
// That is not a convenience, it is what the other backends do — Windows
// maximises with ShowWindow(SW_MAXIMIZE), which shows — and three games
// open hidden, initialise the GPU and then call this expecting a
// maximised window to appear. On X11 it did not: a window manager does
// not manage an unmapped window, so the _NET_WM_STATE ClientMessage
// asking it to maximise went nowhere and the window never appeared at
// all. The EWMH way to open already-maximised is to set the state as a
// PROPERTY before mapping, which is what the Fullscreen option in
// NewWindow already does.
// Maximized reports whether the window was last maximised from here.
func (w *Window) Maximized() bool { return w.maxed }

func (w *Window) ToggleMaximize() {
	w.maxed = !w.maxed
	state := xInternAtom(w.display, "_NET_WM_STATE", 0)
	maxH := xInternAtom(w.display, "_NET_WM_STATE_MAXIMIZED_HORZ", 0)
	maxV := xInternAtom(w.display, "_NET_WM_STATE_MAXIMIZED_VERT", 0)
	if !w.mapped {
		atoms := [2]uint64{uint64(maxH), uint64(maxV)}
		const xaAtom = 4                                        // XA_ATOM
		xChangeProperty(w.display, w.win, state, xaAtom, 32, 0, /*PropModeReplace*/
			uintptr(unsafe.Pointer(&atoms[0])), 2)
		xMapWindow(w.display, w.win)
		w.mapped = true
		xFlush(w.display)
		return
	}
	var ev xEvent
	*(*int32)(unsafe.Pointer(&ev.data[0])) = xClientMessage
	ev.data[4] = uint64(w.win)                  // window
	ev.data[5] = uint64(state)                  // message_type
	*(*int32)(unsafe.Pointer(&ev.data[6])) = 32 // format
	ev.data[7] = 2                              // data.l[0]: _NET_WM_STATE_TOGGLE
	ev.data[8] = uint64(maxH)                   // data.l[1]
	ev.data[9] = uint64(maxV)                   // data.l[2]
	root := xDefaultRootWindow(w.display)
	// SubstructureRedirectMask|SubstructureNotifyMask = 0x180000
	xSendEvent(w.display, root, 0, 0x180000, &ev)
	xFlush(w.display)
}

// startMoveResize hands the current drag to the window manager (EWMH
// _NET_WM_MOVERESIZE): the WM moves/resizes the window from here on.
func (w *Window) startMoveResize(action int32) {
	var rootRet, childRet uintptr
	var rx, ry, wx, wy int32
	var mask uint32
	xQueryPointer(w.display, w.win, &rootRet, &childRet, &rx, &ry, &wx, &wy, &mask)
	xUngrabPointer(w.display, 0) // spec: release implicit grabs first
	atom := xInternAtom(w.display, "_NET_WM_MOVERESIZE", 0)
	var ev xEvent
	*(*int32)(unsafe.Pointer(&ev.data[0])) = xClientMessage
	ev.data[4] = uint64(w.win)
	ev.data[5] = uint64(atom)
	*(*int32)(unsafe.Pointer(&ev.data[6])) = 32
	ev.data[7] = uint64(uint32(rx))
	ev.data[8] = uint64(uint32(ry))
	ev.data[9] = uint64(uint32(action))
	ev.data[10] = 1 // button 1
	ev.data[11] = 1 // source: normal application
	root := xDefaultRootWindow(w.display)
	xSendEvent(w.display, root, 0, 0x180000, &ev)
	xFlush(w.display)
}

// BeginMove starts a WM-driven window move from the current pointer press
// (call from a mouse-down on the app's drawn titlebar).
func (w *Window) BeginMove() { w.startMoveResize(8) }

// BeginResize starts a WM-driven resize from the given window edge (call
// from a mouse-down on the window border).
func (w *Window) BeginResize(edge ResizeEdge) { w.startMoveResize(int32(edge)) }

// RootPointer returns the pointer position in root (screen) coordinates —
// the coordinate space Geometry/SetGeometry use, so window move/resize
// drags can be driven by deltas that stay valid while the window moves.
func (w *Window) RootPointer() (x, y int) {
	var rootRet, childRet uintptr
	var rx, ry, wx, wy int32
	var mask uint32
	xQueryPointer(w.display, w.win, &rootRet, &childRet, &rx, &ry, &wx, &wy, &mask)
	return int(rx), int(ry)
}

// Geometry returns the window's root-space position and size.
func (w *Window) Geometry() (x, y, width, height int) {
	var root uintptr
	var gx, gy int32
	var gw, gh, border, depth uint32
	xGetGeometry(w.display, w.win, &root, &gx, &gy, &gw, &gh, &border, &depth)
	var tx, ty int32
	var child uintptr
	xTranslateCoordinates(w.display, w.win, root, 0, 0, &tx, &ty, &child)
	return int(tx), int(ty), int(gw), int(gh)
}

// SetGeometry moves and resizes the window (root coordinates).
func (w *Window) SetGeometry(x, y, width, height int) {
	// Degenerate-size guard only; size policy (resize floors) belongs to
	// the caller — clamping here grew small dialogs on their first move.
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	xMoveResizeWindow(w.display, w.win, int32(x), int32(y), uint32(width), uint32(height))
	xFlush(w.display)
}
