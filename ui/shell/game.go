//go:build linux || windows || js

package shell

import (
	"runtime"
	"time"
	"unicode"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
)

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
