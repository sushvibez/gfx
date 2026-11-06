//go:build linux || windows || js

package shell

import (
	"math"
	"runtime"
	"time"
	"unicode"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
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

// Frame is the per-frame context handed to a game's update function: the
// drawing surface, its pixel size, elapsed seconds since the last frame, and
// the current input state. Draw with G (render.Renderer2D: FillRect/Quad/
// DrawText). Return false from the update to quit.
type Frame struct {
	G    *render.Renderer2D
	Font *render.Font
	W, H int
	Dt   float64 // seconds since previous frame
	In   *Input

	// DragRegion, set by the game, reports whether a point (logical
	// pixels) is the title-bar area that moves an Undecorated window;
	// presses there start a move from the next frame on. Undecorated
	// windows also resize from a ResizeBand-wide border unless
	// Unresizable. RunGame only.
	DragRegion func(x, y float32) bool

	skip bool
}

// Unchanged tells the shell this frame changed nothing, so it need not
// be submitted or presented and the loop can go to sleep until
// something happens.
//
// A game loop renders continuously because it has to: the shell cannot
// know whether the world moved. RunUI can — it watches properties,
// animations and input — and a static window there costs nothing. This
// is how a game-loop app says the same thing.
//
// Input, a resize and the window's own damage override it, so a frame
// that says this can still be drawn; the flag is a hint, not a veto.
// Call it and return early to skip the drawing as well — although the
// drawing is not where the cost is. Submitting and presenting a frame
// is about 0.3 ms of CPU whether it holds sixty runs of text or
// nothing at all.
func (f *Frame) Unchanged() { f.skip = true }

// RunGame opens a window and runs an immediate-mode game loop: each frame it
// drains input, hands you a Frame to update+draw, and presents. It is the
// sibling of RunUI for real-time content (no retained widget tree). dt is real
// wall-clock seconds, clamped so a stall doesn't teleport the simulation.
func RunGame(o Options, update func(*Frame) bool) error {
	o = o.withDefaults()
	// Hidden until the first frame presents, like Run.
	win, err := app.NewWindow(o.window(true))
	if err != nil {
		return err
	}
	defer win.Destroy()

	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		return err
	}
	surface, err := plat.NewSurface(inst, win)
	if err != nil {
		return err
	}
	adapter, err := inst.RequestAdapter(surface)
	if err != nil {
		return err
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		return err
	}
	queue := device.Queue()
	format := surface.PreferredFormat(adapter)
	if o.Transparent {
		surface.SetTransparent(adapter)
	}
	w, h := win.Size()
	surface.Configure(device, w, h, format)

	r, err := render.New2D(device, queue, format)
	if err != nil {
		return err
	}
	// The same font set-up as Run: FontPaths, CJK, symbol and emoji
	// fallbacks (a game's HUD uses the same arrows and dingbats).
	font, err := newFont(r, o)
	if err != nil {
		return err
	}
	clear := o.clearColor()
	var drag WindowDrag
	in := &Input{down: map[app.Key]bool{}, runes: map[rune]bool{}}
	rendered := 0
	last := time.Now()
	// The frame budget, when one was asked for. Waiting here rather than
	// presenting and letting vsync block means the frame is never built
	// at all, which is where the cost is. It is an EVENT wait, so a key
	// or a click still arrives at once and the loop is asleep in
	// between rather than spinning.
	var budget time.Duration
	if o.MaxFPS > 0 {
		budget = time.Second / time.Duration(o.MaxFPS)
	}
	fr := &Frame{G: r, Font: font, In: in}
	win.EachFrame(func() bool {
		if budget > 0 && runtime.GOOS != "js" {
			if d := budget - time.Since(last); d > 0 {
				win.WaitEvent(d)
			}
		}
		now := time.Now()
		dt := now.Sub(last).Seconds()
		last = now
		if dt > 0.1 { // clamp huge stalls (window drag, breakpoint)
			dt = 0.1
		}
		// Drain input: update held-key state and keep this frame's events.
		in.events = win.KeyEvents()
		for _, k := range in.events {
			if k.Key != app.KeyNone {
				in.down[k.Key] = k.Down
			}
			if k.Rune != 0 {
				in.runes[unicode.ToLower(k.Rune)] = k.Down
			}
		}
		// Drain pointer events: position, button edges, wheel (feedMouse).
		in.resetEdges()
		mouseEvents := win.MouseEvents()
		mouseMoved := len(mouseEvents) > 0
		if o.Undecorated {
			ws := win.Scale()
			drag.Handle(win, float32(w)/ws, float32(h)/ws, mouseEvents, o.resizeBand(), fr.DragRegion)
		}
		in.mouse = in.feedMouse(mouseEvents)
		resized := false
		if nw, nh := win.Size(); nw > 0 && nh > 0 && (nw != w || nh != h) {
			w, h = nw, nh // zero sizes (minimized) would fail GPU validation
			resized = true
			surface.Configure(device, w, h, format)
		}
		// The window's own damage (an Expose) is consumed once a frame,
		// whether or not the frame is drawn — leaving it set would make
		// the next idle check think the window still needs repainting.
		exposed := win.TakeDamage()

		// The frame is BUILT before the swapchain image is acquired, so
		// that a frame the game says changed nothing costs no acquire,
		// no submit and no present — which is all of the cost. Nothing
		// in update needs the image.
		r.Begin(w, h)
		// Game code draws in LOGICAL pixels (input arrives logical too);
		// the renderer maps them onto the physical framebuffer.
		scale := win.Scale()
		r.SetScale(scale)
		fr.W, fr.H, fr.Dt, fr.skip = int(float32(w)/scale), int(float32(h)/scale), dt, false
		cont := update(fr)
		if !cont {
			return false
		}

		// Nothing changed and nothing happened: sleep on the event queue
		// instead of drawing the same picture again. Input, a resize and
		// an expose all override it, and MaxFrames runs (screenshots,
		// tests, benchmarks) never skip — they are counting frames.
		if fr.skip && rendered > 0 && o.MaxFrames == 0 &&
			len(in.events) == 0 && !mouseMoved && !resized && !exposed && !in.Pending() {
			if runtime.GOOS != "js" {
				win.WaitEvent(250 * time.Millisecond)
			}
			return true
		}

		tex, ok := surface.CurrentTexture()
		if !ok {
			surface.Configure(device, w, h, format)
			tex, ok = surface.CurrentTexture()
			if !ok {
				return true
			}
		}
		view := tex.CreateView()
		enc := device.CreateCommandEncoder()
		pass := enc.BeginRenderPass(view, clear)
		_ = r.Flush(pass)
		pass.End()
		queue.Submit(enc.Finish())
		surface.Present()
		view.Release()
		tex.Release()
		if rendered == 0 {
			win.Show() // the first frame is on screen: reveal the window
		}
		rendered++
		return o.MaxFrames == 0 || rendered < o.MaxFrames
	})
	return nil
}

// winCtx bundles one window's per-window render state for RunGame2.
type winCtx struct {
	win      *app.Window
	surface  *gpu.Surface
	w, h     int
	in       *Input
	fr       Frame
	update   func(*Frame) bool
	clear    gpu.Color
	band     float32
	drag     WindowDrag
	o        Options
	rendered int
}

// RunGame2 opens two windows sharing one GPU device (e.g. a map and a
// status readout) — see RunGame2Deferred. The second window is shown
// immediately.
func RunGame2(o1, o2 Options, u1, u2 func(*Frame) bool) error {
	return RunGame2Deferred(o1, o2, u1, u2, nil)
}

// RunGame2Deferred is RunGame2 with the second window hidden until showSecond()
// first returns true (e.g. open the status screen on mission launch). A nil
// showSecond shows it from the start. Each window gets its own Frame (size +
// input), window options and clear colour; closing either ends the session;
// both updates share one Renderer2D, so FontSize, FontPaths, MaxFrames and
// MaxFPS come from o1 and MSDFText is on when either asks for it. The second
// window is a NewChild of the first (X11 and Windows).
func RunGame2Deferred(o1, o2 Options, u1, u2 func(*Frame) bool, showSecond func() bool) error {
	o1, o2 = o1.withDefaults(), o2.withDefaults()
	// Both hidden until their first frame presents, like Run.
	pw, err := app.NewWindow(o1.window(true))
	if err != nil {
		return err
	}
	defer pw.Destroy()
	cw, err := pw.NewChild(o2.window(true))
	if err != nil {
		return err
	}

	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		return err
	}
	ps, err := plat.NewSurface(inst, pw)
	if err != nil {
		return err
	}
	cs, err := plat.NewSurface(inst, cw)
	if err != nil {
		return err
	}
	adapter, err := inst.RequestAdapter(ps)
	if err != nil {
		return err
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		return err
	}
	queue := device.Queue()
	format := ps.PreferredFormat(adapter)
	r, err := render.New2D(device, queue, format)
	if err != nil {
		return err
	}
	fo := o1
	fo.MSDFText = o1.MSDFText || o2.MSDFText
	font, err := newFont(r, fo)
	if err != nil {
		return err
	}

	ctxs := make([]*winCtx, 2)
	for i, s := range []struct {
		o   Options
		win *app.Window
		sf  *gpu.Surface
		up  func(*Frame) bool
	}{{o1, pw, ps, u1}, {o2, cw, cs, u2}} {
		if s.o.Transparent {
			s.sf.SetTransparent(adapter)
		}
		w, h := s.win.Size()
		s.sf.Configure(device, w, h, format)
		in := &Input{down: map[app.Key]bool{}, runes: map[rune]bool{}}
		ctxs[i] = &winCtx{win: s.win, surface: s.sf, w: w, h: h, in: in, update: s.up,
			fr: Frame{G: r, Font: font, In: in}, clear: s.o.clearColor(), o: s.o}
	}

	// The second window may wait for showSecond() before its first frame.
	secondShown := showSecond == nil
	var budget time.Duration
	if o1.MaxFPS > 0 {
		budget = time.Second / time.Duration(o1.MaxFPS)
	}
	frames := 0
	last := time.Now()
	for pw.Poll() {
		if budget > 0 && runtime.GOOS != "js" {
			if d := budget - time.Since(last); d > 0 {
				pw.WaitEvent(d)
			}
		}
		now := time.Now()
		dt := now.Sub(last).Seconds()
		last = now
		if dt > 0.1 {
			dt = 0.1
		}
		if !secondShown && showSecond() {
			secondShown = true
		}
		quit := false
		// Games treat both screens as one session: closing either quits
		// (Poll now only ends the loop for the primary window).
		if cw.Closed() {
			quit = true
		}
		for i, c := range ctxs {
			if i == 1 && !secondShown {
				continue // second window not open yet
			}
			c.in.drain(c.win)
			scale := c.win.Scale() // logical-pixel game space; see RunGame
			if c.o.Undecorated {
				c.drag.Handle(c.win, float32(c.w)/scale, float32(c.h)/scale,
					c.in.mouse, c.o.resizeBand(), c.fr.DragRegion)
			}
			if nw, nh := c.win.Size(); nw > 0 && nh > 0 && (nw != c.w || nh != c.h) {
				c.w, c.h = nw, nh // zero sizes (minimized) fail GPU validation
				c.surface.Configure(device, c.w, c.h, format)
			}
			tex, ok := c.surface.CurrentTexture()
			if !ok {
				c.surface.Configure(device, c.w, c.h, format)
				if tex, ok = c.surface.CurrentTexture(); !ok {
					continue
				}
			}
			view := tex.CreateView()
			r.Begin(c.w, c.h)
			r.SetScale(scale)
			c.fr.W, c.fr.H, c.fr.Dt, c.fr.skip = int(float32(c.w)/scale), int(float32(c.h)/scale), dt, false
			if !c.update(&c.fr) {
				quit = true
			}
			enc := device.CreateCommandEncoder()
			pass := enc.BeginRenderPass(view, c.clear)
			_ = r.Flush(pass)
			pass.End()
			queue.Submit(enc.Finish())
			c.surface.Present()
			view.Release()
			tex.Release()
			if c.rendered == 0 {
				c.win.Show() // its first frame is on screen: reveal it
			}
			c.rendered++
		}
		frames++
		if quit || (o1.MaxFrames > 0 && frames >= o1.MaxFrames) {
			return nil
		}
	}
	return nil
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
func (in *Input) feedMouse(events []app.MouseEvent) []app.MouseEvent {
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
