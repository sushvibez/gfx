//go:build linux || windows || js

package shell

import (
	"math"
	"unicode"

	"github.com/sushvibez/gfx/app"
)

// NewInput builds an empty input state, for ScreenshotGame's scripted prep.
func NewInput() *Input { return &Input{down: map[app.Key]bool{}, runes: map[rune]bool{}} }

// SetDown sets/clears a held special key (for scripted screenshots).
func (i *Input) SetDown(k app.Key, down bool) { i.down[k] = down }

// SetRune sets/clears a held printable key (for scripted screenshots).
func (i *Input) SetRune(r rune, down bool) { i.runes[unicode.ToLower(r)] = down }

// SetEvents sets this frame's edge events (for scripted Pressed()).
func (i *Input) SetEvents(ev ...app.KeyEvent) { i.events = ev }

// Input is the keyboard state for a game frame: which keys are currently held
// (edge-tracked from press/release events) plus the events received this frame.
type Input struct {
	down   map[app.Key]bool
	runes  map[rune]bool
	events []app.KeyEvent
	mouse  []app.MouseEvent // this frame's pointer events (drain)
	// carry holds pointer events put off to the next frame: a release that
	// arrived in the same frame as its press (feedMouse).
	carry []app.MouseEvent

	// PointerScale converts pointer positions as they are read: they
	// arrive in the window's logical pixels (physical / window.Scale()),
	// and a screen that draws at a different scale — a layout capped to
	// fit, drawn at physical / s — sets PointerScale = window.Scale() / s
	// before each drain so the pointer lands where it draws. 0 or 1 leaves
	// positions alone.
	PointerScale float32

	// Pointer state for the frame.
	MouseX, MouseY float32 // last cursor position (screen pixels)
	mouseDown      bool    // primary button currently held
	clicked        bool    // primary button pressed this frame (edge)
	released       bool    // primary button released this frame (edge)
	// The secondary button, tracked the same way. Buttons are numbered
	// as X11 and the DOM number them: 1 primary, 2 middle, 3 secondary.
	rightDown     bool
	rightClicked  bool
	rightReleased bool
	// The middle button, for the orbit-and-pan gesture CAD tools use.
	midDown     bool
	midClicked  bool
	midReleased bool
	// WheelY is this frame's scroll delta in PIXELS — one wheel notch is
	// app.wheelNotchPx (72, measured from Qt), and the sign is the DOM's
	// deltaY: POSITIVE is the wheel turned down, which moves a list's
	// content up past the viewport. All three backends agree on this
	// (x11 button 5, win32's negated WM_MOUSEWHEEL delta, and the web's
	// deltaY passed straight through); the comment here used to say the
	// opposite, and code written to it scrolled the wrong way.
	WheelY float32
	// ModShift and ModCtrl are the modifiers as of the latest key or
	// pointer event: held or not while a drag goes on, which is what a
	// fine or snapped drag reads.
	ModShift, ModCtrl bool

	// Multi-touch: this frame's raw events plus folded pinch/pan gesture
	// state (empty/idle on backends without touch — X11 today). The primary
	// finger also arrives through the mouse fields above.
	Touches []app.TouchEvent
	Gest    app.Gestures

	pinchAccum float32 // pinch spread not yet emitted as a wheel notch
}

// MiddleDown reports whether the middle button is currently held.
func (i *Input) MiddleDown() bool { return i.midDown }

// MiddleClicked reports a middle-button press this frame.
func (i *Input) MiddleClicked() bool { return i.midClicked }

// MiddleReleased reports a middle-button release this frame.
func (i *Input) MiddleReleased() bool { return i.midReleased }

// RightDown reports whether the secondary button is currently held.
func (i *Input) RightDown() bool { return i.rightDown }

// RightClicked reports a secondary-button press this frame.
func (i *Input) RightClicked() bool { return i.rightClicked }

// RightReleased reports a secondary-button release this frame.
func (i *Input) RightReleased() bool { return i.rightReleased }

// MouseDown reports whether the primary button is currently held.
func (i *Input) MouseDown() bool { return i.mouseDown }

// MouseClicked reports a primary-button press edge this frame.
func (i *Input) MouseClicked() bool { return i.clicked }

// MouseReleased reports a primary-button release edge this frame.
func (i *Input) MouseReleased() bool { return i.released }

// Down reports whether a non-printable key (arrows, enter, ...) is held.
func (i *Input) Down(k app.Key) bool { return i.down[k] }

// Events is this frame's key events, in order. Pressed answers "was this
// key hit", which is what a game loop wants; a text field wants the
// characters themselves, with their case and their order, and this is
// the only way to get at them.
func (i *Input) Events() []app.KeyEvent { return i.events }

// Rune reports whether a printable key is held (case-insensitive).
func (i *Input) Rune(r rune) bool { return i.runes[unicode.ToLower(r)] }

// Pressed reports whether a key (rune or special) had a key-down event this
// frame — useful for one-shot actions like pause/serve.
func (i *Input) Pressed(k app.Key, r rune) bool {
	r = unicode.ToLower(r)
	for _, e := range i.events {
		if !e.Down {
			continue
		}
		if k != app.KeyNone && e.Key == k {
			return true
		}
		if r != 0 && unicode.ToLower(e.Rune) == r {
			return true
		}
	}
	return false
}

// Drain pulls one frame of key + pointer events from win — for games that
// run their own frame loop (3D examples) instead of RunGame.
func (in *Input) Drain(win *app.Window) { in.drain(win) }

// DrainEvents feeds pre-drained event slices — for games that need the raw
// events themselves (relative mouse deltas) and the tracked state.
func (in *Input) DrainEvents(keys []app.KeyEvent, mouse []app.MouseEvent) {
	in.resetEdges()
	in.events = keys
	for _, k := range keys {
		if k.Key != app.KeyNone {
			in.down[k.Key] = k.Down
		}
		if k.Rune != 0 {
			in.runes[unicode.ToLower(k.Rune)] = k.Down
		}
		in.ModShift, in.ModCtrl = k.Shift, k.Ctrl
	}
	in.mouse = in.feedMouse(mouse)
}

// drain pulls one frame of key + pointer events from win into the Input.
func (in *Input) drain(win *app.Window) {
	in.resetEdges() // first: the pinch below feeds WheelY
	// Multi-touch (backends that have it): keep the gesture state current.
	if tw, ok := any(win).(interface{ TouchEvents() []app.TouchEvent }); ok {
		in.Touches = tw.TouchEvents()
	} else {
		in.Touches = nil
	}
	in.Gest.Feed(in.Touches)
	// A pinch is the wheel: spreading two fingers zooms in the way the
	// wheel turned away does, in whole notches so a screen that steps per
	// wheel event (a carousel) steps once per 15 % of spread rather than
	// every frame. The primary finger still drives the pointer, so a
	// pinch pans too, as it does in a map app.
	if in.Gest.Pinching && in.Gest.ScaleD > 0 && in.Gest.ScaleD != 1 {
		in.pinchAccum += float32(math.Log(float64(in.Gest.ScaleD)) / math.Log(1.15))
		for in.pinchAccum >= 1 {
			in.WheelY -= app.WheelNotchPx
			in.pinchAccum--
		}
		for in.pinchAccum <= -1 {
			in.WheelY += app.WheelNotchPx
			in.pinchAccum++
		}
	} else if !in.Gest.Pinching {
		in.pinchAccum = 0
	}
	in.events = win.KeyEvents()
	for _, k := range in.events {
		if k.Key != app.KeyNone {
			in.down[k.Key] = k.Down
		}
		if k.Rune != 0 {
			in.runes[unicode.ToLower(k.Rune)] = k.Down
		}
		in.ModShift, in.ModCtrl = k.Shift, k.Ctrl
	}
	in.mouse = in.feedMouse(win.MouseEvents())
}

// feedMouse folds a frame's pointer events into the Input and returns the
// ones it took. A release that arrives in the same frame as its own press
// — a touchpad tap, which Windows delivers as a down and an up a few
// milliseconds apart, or a very quick click — is put off to the next
// frame with everything after it, so every frame-based reader sees the
// button down for one frame and released the next, exactly as for a
// slower click. Without it a tap was clicked, released and not down all
// at once, and anything that acts while the button is down (a drag, the
// view cube, a hold-to-act control, a press-then-release button) never
// saw it.
func (in *Input) feedMouse(events []app.MouseEvent) (taken []app.MouseEvent) {
	if inputTrace {
		defer func() { in.traceFrame(taken) }()
	}
	if f := in.PointerScale; f > 0 && f != 1 && len(events) > 0 {
		scaled := make([]app.MouseEvent, len(events))
		for i, m := range events {
			m.X, m.Y, m.DX, m.DY = m.X*f, m.Y*f, m.DX*f, m.DY*f
			scaled[i] = m
		}
		events = scaled
	}
	if len(in.carry) > 0 {
		events = append(in.carry, events...)
		in.carry = nil
	}
	var pressedNow [4]bool // buttons pressed in this frame
	for i, m := range events {
		if m.Kind == app.MouseUp && m.Button >= 1 && m.Button <= 3 && pressedNow[m.Button] {
			in.carry = append([]app.MouseEvent(nil), events[i:]...)
			return events[:i]
		}
		if m.Kind != app.MouseScroll {
			in.ModShift, in.ModCtrl = m.Shift, m.Ctrl
		}
		switch m.Kind {
		case app.MouseMove:
			in.MouseX, in.MouseY = m.X, m.Y
		case app.MouseDown:
			in.MouseX, in.MouseY = m.X, m.Y
			if m.Button >= 1 && m.Button <= 3 {
				pressedNow[m.Button] = true
			}
			switch m.Button {
			case 1:
				in.mouseDown, in.clicked = true, true
			case 2:
				in.midDown, in.midClicked = true, true
			case 3:
				in.rightDown, in.rightClicked = true, true
			}
		case app.MouseUp:
			in.MouseX, in.MouseY = m.X, m.Y
			switch m.Button {
			case 1:
				in.mouseDown, in.released = false, true
			case 2:
				in.midDown, in.midReleased = false, true
			case 3:
				in.rightDown, in.rightReleased = false, true
			}
		case app.MouseScroll:
			in.WheelY += m.ScrollY
		}
	}
	return events
}

// resetEdges clears the one-frame state (button edges, the wheel) before
// a frame's events are read.
func (in *Input) resetEdges() {
	in.clicked, in.released, in.WheelY = false, false, 0
	in.rightClicked, in.rightReleased = false, false
	in.midClicked, in.midReleased = false, false
}

// Pending reports pointer events held for the next frame (a release that
// came in the same frame as its press, see feedMouse): a loop that sleeps
// when nothing happened must run one more frame first.
func (in *Input) Pending() bool { return len(in.carry) > 0 }
