package app

// Host is what every windowing backend provides: the seam between
// platform code and everything above it. *Window implements it on all
// platforms; *Headless implements it with no display at all.
type Host interface {
	// Poll pumps platform events; false once close was requested.
	Poll() bool
	// Size is the client area in PHYSICAL pixels (GPU surface size).
	Size() (width, height int)
	// Scale is the DPI scale: physical pixels per logical pixel. Input
	// events and layout are in logical pixels; only Windows reports values
	// other than 1 so far.
	Scale() float32
	// MouseEvents drains the pointer events received since the last call.
	MouseEvents() []MouseEvent
	// KeyEvents drains the key events received since the last call.
	KeyEvents() []KeyEvent
	// EachFrame runs fn once per frame until fn returns false or the
	// host closes.
	EachFrame(fn func() bool)
	// SetCursor sets the mouse-pointer shape.
	SetCursor(c Cursor)
	// Destroy releases the host.
	Destroy()
}

// Headless is the no-display host (DESIGN.md §8.1): tests, CI and agents
// inject synthetic input and step frames deterministically, pairing it
// with offscreen gpu rendering (CreateRenderTexture + ReadTexture) when
// pixels are wanted.
type Headless struct {
	width  int
	height int
	mouse  []MouseEvent
	keys   []KeyEvent
	closed bool

	// Placement, carried verbatim from Options so tests can assert what a
	// real backend would have been asked for. There is no display, so a
	// fullscreen headless host simply reports its single virtual monitor's
	// size as its own.
	x, y       int
	positioned bool
	fullscreen bool
}

// NewHeadless creates a headless host of the given size.
func NewHeadless(o Options) *Headless {
	if o.Width <= 0 {
		o.Width = 800
	}
	if o.Height <= 0 {
		o.Height = 600
	}
	h := &Headless{width: o.Width, height: o.Height,
		x: o.X, y: o.Y, positioned: o.Positioned, fullscreen: o.Fullscreen}
	if o.Fullscreen {
		// The virtual monitor is whatever size was asked for: a headless
		// fullscreen window covers its whole (single) screen by definition.
		h.x, h.y, h.positioned = 0, 0, true
	}
	return h
}

// Position reports the requested virtual-desktop position and whether one
// was requested at all (Options.Positioned).
func (h *Headless) Position() (x, y int, positioned bool) {
	return h.x, h.y, h.positioned
}

// Monitors reports the headless host's single virtual monitor, sized to the
// host itself — the package-level Monitors() needs a real display, this does
// not, so tests and CI can exercise monitor-aware code paths.
func (h *Headless) Monitors() []Monitor {
	return []Monitor{{
		Name: "headless", X: 0, Y: 0,
		Width: h.width, Height: h.height, Scale: 1, Primary: true,
	}}
}

// SetFullscreen records the request; there is no display to cover.
func (h *Headless) SetFullscreen(on bool) { h.fullscreen = on }

// Fullscreen reports the last state requested.
func (h *Headless) Fullscreen() bool { return h.fullscreen }

// MonitorIndex is always 0: the headless host has exactly one monitor.
func (h *Headless) MonitorIndex() int { return 0 }

// InjectMouse queues synthetic pointer events for the next frame.
func (h *Headless) InjectMouse(events ...MouseEvent) {
	h.mouse = append(h.mouse, events...)
}

// InjectKey queues synthetic key events for the next frame.
func (h *Headless) InjectKey(events ...KeyEvent) {
	h.keys = append(h.keys, events...)
}

// Click injects a full primary-button press and release at (x, y).
func (h *Headless) Click(x, y float32) {
	h.InjectMouse(
		MouseEvent{Kind: MouseDown, X: x, Y: y, Button: 1},
		MouseEvent{Kind: MouseUp, X: x, Y: y, Button: 1},
	)
}

// Type injects key-down events for each rune of s.
func (h *Headless) Type(s string) {
	for _, r := range s {
		h.InjectKey(KeyEvent{Down: true, Rune: r})
	}
}

// Resize changes the reported client size.
func (h *Headless) Resize(width, height int) { h.width, h.height = width, height }

// Close requests shutdown, like a window's close button.
func (h *Headless) Close() { h.closed = true }

func (h *Headless) Poll() bool       { return !h.closed }
func (h *Headless) Size() (int, int) { return h.width, h.height }
func (h *Headless) Scale() float32   { return 1 }
func (h *Headless) SetCursor(Cursor) {}
func (h *Headless) Destroy()         { h.closed = true }

func (h *Headless) MouseEvents() []MouseEvent {
	ev := h.mouse
	h.mouse = nil
	return ev
}

func (h *Headless) KeyEvents() []KeyEvent {
	ev := h.keys
	h.keys = nil
	return ev
}

// Step runs exactly one frame.
func (h *Headless) Step(fn func() bool) bool {
	if h.closed {
		return false
	}
	return fn()
}

// EachFrame runs fn back to back — no pacing, which is exactly what
// deterministic tests want. Use Step for single-frame control.
func (h *Headless) EachFrame(fn func() bool) {
	for h.Poll() {
		if !fn() {
			return
		}
	}
}

var _ Host = (*Headless)(nil)
