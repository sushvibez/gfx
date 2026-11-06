package app

// clipboardFallback backs ClipboardText/SetClipboardText on backends
// without OS clipboard integration yet (X11 selections, the async browser
// clipboard API): in-process copy/paste keeps working everywhere.
var clipboardFallback string

// MouseKind classifies a mouse event.
type MouseKind int

const (
	MouseDown MouseKind = iota
	MouseUp
	MouseMove
	MouseScroll
)

// MouseEvent is a pointer event in window pixel coordinates. For
// MouseScroll, ScrollY/ScrollX are in pixels with the DOM convention:
// positive ScrollY scrolls the content down (wheel pulled toward you),
// positive ScrollX scrolls it right (tilt wheel / horizontal gesture).
type MouseEvent struct {
	Kind    MouseKind
	X, Y    float32
	Button  int // 1 = primary
	ScrollY float32
	ScrollX float32
	Shift   bool // shift held at event time (constrained drawing/selection)
	Ctrl    bool // control held at event time (snapping)

	// Relative motion (MouseMove while the pointer is captured — see
	// Window.SetPointerCapture). Zero when uncaptured.
	DX, DY float32
}

// TouchPhase classifies one touch point's lifecycle event.
type TouchPhase int

const (
	TouchBegin TouchPhase = iota
	TouchMove
	TouchEnd
	TouchCancel
)

// TouchEvent is one finger's update in window logical pixels. ID is stable
// for the finger's lifetime (Begin through End/Cancel) and may be reused
// afterwards. Platforms that synthesize mouse events from the primary touch
// (Android, browsers, Windows) keep doing so — plain UIs work untouched;
// consume this stream for multi-touch gestures (see Gestures).
type TouchEvent struct {
	ID    int
	Phase TouchPhase
	X, Y  float32
}

// Cursor names a mouse-pointer shape. Backends map these to the OS cursor.
// ResizeEdge names a window border for BeginResize (values follow EWMH
// _NET_WM_MOVERESIZE directions).
type ResizeEdge int

const (
	EdgeTopLeft     ResizeEdge = 0
	EdgeTop         ResizeEdge = 1
	EdgeTopRight    ResizeEdge = 2
	EdgeRight       ResizeEdge = 3
	EdgeBottomRight ResizeEdge = 4
	EdgeBottom      ResizeEdge = 5
	EdgeBottomLeft  ResizeEdge = 6
	EdgeLeft        ResizeEdge = 7
)

type Cursor int

const (
	CursorDefault Cursor = iota // arrow
	CursorPointer               // hand (clickable)
	CursorText                  // I-beam (editable text)
	CursorEW                    // left-right arrows (a value dragged sideways)
)

// Key identifies a non-printable key. Printable input arrives as
// KeyEvent.Rune instead.
type Key int

const (
	KeyNone Key = iota
	KeyBackspace
	KeyDelete
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	KeyEnter
	KeyTab
	KeyEscape
	KeyPageUp
	KeyPageDown
	KeyInsert
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

// KeyEvent is a keyboard event: Rune holds the printable character if any,
// Key the editing/navigation key if any, plus the Shift/Ctrl/Alt modifier
// state at the time of the event. IME text (CJK composition results) also
// arrives as Rune events, one per rune.
type KeyEvent struct {
	Down  bool
	Rune  rune
	Key   Key
	Shift bool
	Ctrl  bool
	Alt   bool
}

// wheelNotchPx is how far one wheel notch scrolls. Measured from Qt rather
// than assumed: a QtTest case sending one notch (an angle delta of 120) to
// a ListView and a Flickable moves each of them 72 pixels, and notches add
// up linearly (three notches, 216). Qt scales the angle delta by 0.6.
const wheelNotchPx = 72

// WheelNotchPx is wheelNotchPx for code outside the package that
// synthesises wheel events (a pinch, in the shell).
const WheelNotchPx = wheelNotchPx
