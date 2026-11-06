//go:build linux || windows || js

// Package shell is the application runner: it owns the window/GPU/UI
// bootstrap, the frame loop, headless screenshots, fonts, and — for
// undecorated windows — move/resize handling. Apps provide hooks for
// their scene and input; everything else is common. (Promoted from
// examples/internal/shell, which it supersedes for app modules.)
package shell

import (
	"fmt"
	"math"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strconv"
	"syscall"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// Options configures Run and Screenshot.
type Options struct {
	Title     string
	Width     int
	Height    int
	FontSize  float64  // default 18
	FontPaths []string // preferred font files, tried before the defaults
	MaxFrames int      // 0 = run until closed
	// MaxFPS caps the frame rate (0 = as fast as the display allows).
	//
	// Vsync alone is not a cap worth relying on: a 240 Hz monitor asks
	// for 240 frames a second, and a window that redraws unconditionally
	// obliges — at about a third of a millisecond of CPU each, almost
	// all of it inside wgpu's submit and present, that is a tenth of a
	// core spent on a window where nothing is moving. A demo or a tool
	// that does not need every refresh should say so.
	MaxFPS   int
	ClearHex uint32 // window clear color, default 0x0d0d12
	// Undecorated removes OS window decorations; the shell then provides
	// border-band resizing and (via Hooks.DragRegion) titlebar moving.
	Undecorated bool
	ResizeBand  float32 // undecorated resize border width, default 5
	// Transparent opens a per-pixel-alpha window (X11 ARGB) cleared to
	// alpha 0 — for client-side decorations with shadows over the desktop.
	Transparent bool
	// LineHeight is the default text line height as a multiple of font
	// size (CSS line-height; gpui uses phi = 1.618). 0 keeps text boxes at
	// the face's natural height.
	LineHeight float64
	// EventDriven marks Hooks.FrameStart as an input tap that only reacts
	// to the events passed to it: the window keeps present-on-damage idle
	// skipping instead of rendering continuously. Leave false for hooks
	// that animate or poll per-frame state (cameras, benchmarks).
	EventDriven bool
	// MSDFText makes distance-field text the window's default path
	// (ui.UI.SetMSDFText — crisp when rotated or at a size that slides,
	// one sheet for every size). GFX_MSDF=1|0 overrides it without a
	// rebuild, for an A/B against the raster path.
	MSDFText bool
	// Unmovable / Unresizable / Unminimizable pin the window's position /
	// size / minimize control (gpui's is_movable/is_resizable/
	// is_minimizable window hints; Windows-only today).
	Unmovable     bool
	Unresizable   bool
	Unminimizable bool
	// X, Y place the window (when Positioned) in virtual-desktop pixels;
	// Fullscreen opens it borderless over the monitor containing (X, Y).
	// Used by OpenChild for one-window-per-monitor setups (projector rigs).
	X, Y       int
	Positioned bool
	Fullscreen bool
}

// App hands the runner's state to hooks.
type App struct {
	Win    *app.Window // nil when headless
	Device *gpu.Device
	Queue  *gpu.Queue
	Format gpu.TextureFormat
	R2     *render.Renderer2D
	Font   *render.Font
	UI     *ui.UI
	W, H   int

	// Gest folds the window's multi-touch stream into pinch/pan state,
	// updated by the frame loop each iteration on backends with touch
	// (android, web, windows); Touches is that frame's raw drain. Empty on
	// backends without touch (X11 today).
	Gest    app.Gestures
	Touches []app.TouchEvent

	host *childHost // child-window management (nil when headless)
}

// Hooks are the app-specific extension points; all optional except Init.
type Hooks struct {
	// Init runs once after the renderers exist and returns the view
	// function (build scenes, register gml env values, wire callbacks).
	Init func(a *App) (func() ui.Element, error)
	// FrameStart runs each frame before UI event dispatch — raw input
	// handling (cameras), per-frame state mirroring, animations.
	FrameStart func(a *App, dt float32, mouse []app.MouseEvent, keys []app.KeyEvent)
	// Encode3D records 3D work (offscreen View3Ds or direct passes) after
	// UI layout and before the 2D pass. When set, the 2D pass loads the
	// target instead of clearing it, and a depth view is maintained.
	Encode3D func(a *App, enc *gpu.CommandEncoder, target, depth *gpu.TextureView) error
	// DragRegion reports whether a point is in the app's titlebar move
	// region (undecorated windows).
	DragRegion func(a *App, x, y float32) bool
}

// RunUI opens a window and drives a plain ui view — the no-hooks case.
func RunUI(o Options, view func() ui.Element) error {
	return Run(o, Hooks{Init: func(*App) (func() ui.Element, error) { return view, nil }})
}

// Run opens a window and drives the app until the window closes.
func Run(o Options, hooks Hooks) error {
	if o.FontSize == 0 {
		o.FontSize = 18
	}
	if o.ClearHex == 0 {
		o.ClearHex = 0x0d0d12
	}
	if o.ResizeBand == 0 {
		o.ResizeBand = 5
	}
	// GFX_CPUPROFILE profiles a LIVE window too, not just a capture: what
	// a frame costs while something on the page is animating is only
	// visible here (Screenshot has the same knob).
	// GFX_CPUPROFILE_SKIP=<frames> delays the profile past start-up: font
	// atlases and pipelines cost more than a hundred steady frames, and a
	// short profile that includes them says nothing about the frame.
	profSkip := 0
	if v := os.Getenv("GFX_CPUPROFILE_SKIP"); v != "" {
		profSkip, _ = strconv.Atoi(v)
	}
	var profStart func()
	if p := os.Getenv("GFX_CPUPROFILE"); p != "" {
		if f, err := os.Create(p); err == nil {
			profStart = func() {
				pprof.StartCPUProfile(f)
				profStart = nil
			}
			if profSkip <= 0 {
				profStart()
			}
			stop := func() {
				pprof.StopCPUProfile()
				f.Close()
			}
			defer stop()
			// A profiled run is usually ended by `timeout`: flush the
			// profile on SIGTERM instead of losing it.
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
			go func() {
				<-sig
				stop()
				os.Exit(0)
			}()
		}
	}
	// GFX_MEMPROFILE writes a heap profile when the window closes — for
	// the leak a long-running animated page shows as growing RSS.
	if p := os.Getenv("GFX_MEMPROFILE"); p != "" {
		defer func() {
			if f, err := os.Create(p); err == nil {
				runtime.GC()
				pprof.WriteHeapProfile(f)
				f.Close()
			}
		}()
	}
	// Hidden until the first frame presents: GPU/font init takes long
	// enough that an early-shown window sits as a blank flash (GPUI does
	// the same — no WS_VISIBLE, placement applied when content is ready).
	win, err := app.NewWindow(app.Options{Title: o.Title, Width: o.Width, Height: o.Height,
		Undecorated: o.Undecorated, Transparent: o.Transparent, Hidden: true})
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

	// Pure-2D windows render into a 4x MSAA target resolved onto the swap
	// chain, matching the headless screenshot path: SDF quads and atlas text
	// are already analytically smooth, but raw path/SVG triangles need it.
	msaa := 0
	if hooks.Encode3D == nil {
		msaa = 4
	}
	var r *render.Renderer2D
	var msaaTex *gpu.Texture
	var msaaView *gpu.TextureView
	if msaa > 0 {
		r, err = render.New2DMSAA(device, queue, format, msaa)
		if err != nil {
			return err
		}
		msaaTex = device.CreateMSAATexture(w, h, format, msaa)
		msaaView = msaaTex.CreateView()
	} else {
		r, err = render.New2D(device, queue, format)
		if err != nil {
			return err
		}
	}
	font, err := newFont(r, o)
	if err != nil {
		return err
	}
	a := &App{Win: win, Device: device, Queue: queue, Format: format, R2: r, Font: font, W: w, H: h,
		host: &childHost{inst: inst, device: device, format: format, msaa: msaa}}
	view, err := hooks.Init(a)
	if err != nil {
		return err
	}
	u := ui.New(r, font, view)
	u.SetLineHeight(o.LineHeight)
	a.host.lineHeight = o.LineHeight
	a.UI = u
	// Windowed apps use the OS clipboard for Ctrl+C/X/V (backends without
	// integration fall back to an in-process buffer inside app); headless
	// runs and tests keep ui's own buffer and never touch the real one.
	ui.SetClipboard(app.ClipboardText, app.SetClipboardText)

	var depth *gpu.Texture
	var depthView *gpu.TextureView
	if hooks.Encode3D != nil {
		depth = device.CreateDepthTexture(w, h)
		depthView = depth.CreateView()
	}

	clear := gpu.Color{
		R: float64(o.ClearHex>>16&0xff) / 255,
		G: float64(o.ClearHex>>8&0xff) / 255,
		B: float64(o.ClearHex&0xff) / 255,
		A: 1,
	}
	if o.Transparent {
		clear = gpu.Color{} // alpha 0: the desktop shows through undrawn pixels
	}
	var drag WindowDrag
	rendered := 0
	var lastRev, childRev uint64
	last := time.Now()
	animT := time.Now()
	resizing := false // interactive-resize state; drives the vsync toggle
	// presentFallback: the swapchain watchdog below gave up on vsync.
	presentFallback := false
	lastFallback := false
	acquireStalls := 0
	// Damage-driven repaint needs a persistent MSAA target to load last
	// frame from (a swapchain image is two or three frames old), an opaque
	// window (a transparent one cannot be re-based), and no 3D pass under
	// it. GFX_NO_DAMAGE=1 turns it off for comparison.
	damageOK := msaa > 0 && !o.Transparent && hooks.Encode3D == nil &&
		runtime.GOOS != "js" && os.Getenv("GFX_NO_DAMAGE") == ""
	u.EnableDamage(damageOK)
	var dmgArea float64
	dmgFrames := 0
	skippedFrames := 0
	// Per-phase wall time, accumulated per second for the GFX_FPS line:
	// where a frame's milliseconds go, live, without a profiler.
	var tLayout, tLayers, tPaint, tFlush, tPresent, tWait time.Duration
	var tMark time.Time
	lap := func(acc *time.Duration) {
		now := time.Now()
		*acc += now.Sub(tMark)
		tMark = now
	}
	// frameCap is the shortest gap between two ANIMATION-driven frames
	// (see the rate cap below); 0 disables it.
	frameCap := time.Second / 60
	if v := os.Getenv("GFX_MAX_FPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			frameCap = time.Second / time.Duration(n)
		} else if n == 0 {
			frameCap = 0
		}
	}
	var lastFrame time.Time

	// GFX_FPS=1 reports presented frames and loop passes per second — the
	// two differ when the rate cap or the damage skip is doing its job.
	fpsEvery := 0
	if os.Getenv("GFX_FPS") != "" {
		fpsEvery = 1
	}
	fpsCount, loopPasses := 0, 0
	fpsAt := time.Now()
	// With GFX_FPS set, a MaxFrames run reports its CPU time at exit: the
	// process's own rusage, all threads — the deterministic per-frame
	// cost, which live sampling (vsync waits and all) is not.
	if fpsEvery > 0 && o.MaxFrames > 0 {
		defer func() {
			if cpu, ok := processCPU(); ok {
				fmt.Fprintf(os.Stderr, "shell: exit after %d frames, cpu %.3fs\n", o.MaxFrames, cpu.Seconds())
			}
		}()
	}
	win.EachFrame(func() bool {
		now := time.Now()
		dt := float32(now.Sub(last).Seconds())
		core.TickAnimations(now.Sub(last))
		last = now
		if dt > 0.1 {
			dt = 0.1
		}
		// The UI runs in LOGICAL pixels: the window reports physical size
		// (surface/MSAA/depth below) and a DPI scale; input events arrive
		// logical from the backend, and the renderer maps logical→physical.
		scale := win.Scale()
		a.W, a.H = int(float32(w)/scale), int(float32(h)/scale)

		mouse := win.MouseEvents()
		keys := win.KeyEvents()
		// Touch is a per-backend extra (not part of Host yet — the win32
		// backend grows WM_TOUCH separately): drain when present so the
		// gesture state is always current and the buffer never accumulates.
		if tw, ok := any(win).(interface{ TouchEvents() []app.TouchEvent }); ok {
			a.Touches = tw.TouchEvents()
		} else {
			a.Touches = nil
		}
		a.Gest.Feed(a.Touches)
		if o.Undecorated {
			inRegion := func(x, y float32) bool {
				return hooks.DragRegion != nil && hooks.DragRegion(a, x, y)
			}
			drag.Handle(win, float32(w)/scale, float32(h)/scale, mouse, o.ResizeBand, inRegion)
		}
		if hooks.FrameStart != nil && !drag.Active() {
			hooks.FrameStart(a, dt, mouse, keys)
		}
		u.Mouse(mouse)
		for _, k := range keys {
			u.Key(k)
		}
		win.SetCursor(u.Cursor())
		// Time-driven content (GIFs) pauses while unfocused: the animation
		// clock only advances when the window has focus, so hover-driven
		// repaints show a frozen frame.
		if win.Focused() {
			animT = now
		}
		u.SetAnimTime(animT)

		// Child windows: each runs its own input/damage/render cycle every
		// loop iteration, independent of the primary's idle skipping. A
		// global invalidation (running animations, property writes) forces
		// them all to repaint, since state is shared.
		if len(a.host.children) > 0 {
			rev := core.Revision()
			force := core.AnimationsRunning() || rev != childRev
			for i := 0; i < len(a.host.children); {
				c := a.host.children[i]
				if c.frame(a.host, a, force) {
					i++
				} // teardown removed it from the slice otherwise
			}
			childRev = core.Revision()
		}

		loopPasses++
		resized := false
		// Interactive resize: drop vsync so each new-size present reaches
		// the screen immediately — a FIFO present inside the OS modal
		// resize loop waits out a vblank while the compositor stretches
		// the previous frame to the new size (visible text shimmer).
		if rz := win.Resizing(); rz != resizing {
			resizing = rz
			if rz || presentFallback {
				gpu.SetPresentOverride("immediate")
			} else {
				gpu.SetPresentOverride("")
			}
			resized = true // reconfigure below picks up the present mode
		}
		// Skip zero sizes (minimized, or transitional during cross-monitor
		// DPI moves): configuring a 0-wide surface/MSAA target is a GPU
		// validation error that poisons the whole frame.
		if nw, nh := win.Size(); resized || (nw > 0 && nh > 0 && (nw != w || nh != h)) {
			if nw > 0 && nh > 0 {
				w, h = nw, nh
			}
			resized = true
			surface.Configure(device, w, h, format)
			if depthView != nil {
				depthView.Release()
				depth.Release()
				depth = device.CreateDepthTexture(w, h)
				depthView = depth.CreateView()
			}
			if msaaView != nil {
				msaaView.Release()
				msaaTex.Release()
				msaaTex = device.CreateMSAATexture(w, h, format, msaa)
				msaaView = msaaTex.CreateView()
			}
		}

		// Present on damage only (GPUI-style): a frame whose inputs cannot
		// have changed any pixel — no events, no running animations, no
		// property writes since the last present, no resize/expose — is
		// skipped entirely instead of re-rendered. Per-frame hooks
		// (FrameStart/Encode3D) and MaxFrames test runs opt out: they render
		// continuously by design. On js the rAF callback simply returns; on
		// native the loop blocks on the platform's event wait (true 0% idle
		// CPU) until input/expose/configure traffic — for any window on the
		// connection, children included — wakes it. Nothing repaint-worthy
		// can happen without an event while this goroutine is blocked, so
		// the cap only bounds staleness.
		// Unfocused windows pause time-driven content (GIFs): WantsFrame
		// only forces rendering while the window has focus. Input, damage,
		// animations and property changes still repaint as usual.
		// The window's own damage (an Expose) is consumed HERE, once: it
		// decides both whether to render and whether the render is full.
		exposed := win.TakeDamage()
		if rendered > 0 && o.MaxFrames == 0 &&
			(hooks.FrameStart == nil || o.EventDriven) && hooks.Encode3D == nil &&
			len(mouse) == 0 && len(keys) == 0 && !drag.Active() &&
			!resized && !exposed && !(u.WantsFrame() && win.Focused()) &&
			!core.AnimationsRunning() && !core.ExternalAnimating() && core.Revision() == lastRev {
			if runtime.GOOS != "js" {
				win.WaitEvent(250 * time.Millisecond)
			}
			return true
		}

		// RATE-CAP the time-driven frames. A frame that only exists
		// because something is animating does not have to keep up with
		// the display: a 240Hz monitor asked for four times the work a
		// 60Hz one does, and the whole window is redrawn either way, so
		// one pulsing ring on the buttons page cost 44% of a core. Input,
		// damage and resizes still render the moment they arrive — this
		// only bounds the ANIMATION rate, and the wait is an event wait,
		// so the loop stays asleep instead of spinning.
		if rendered > 0 && o.MaxFrames == 0 && frameCap > 0 &&
			len(mouse) == 0 && len(keys) == 0 && !drag.Active() && !resized {
			if wait := frameCap - now.Sub(lastFrame); wait > 0 {
				if runtime.GOOS != "js" {
					t0 := time.Now()
					win.WaitEvent(wait)
					tWait += time.Since(t0)
				}
				return true
			}
		}
		lastFrame = now
		if profStart != nil && rendered >= profSkip {
			profStart()
		}
		if fpsEvery > 0 {
			fpsCount++
			if now.Sub(fpsAt) >= time.Second {
				bc, bb, tc, tb := gpu.UploadStats()
				bn, tn := gpu.UploadNanos()
				n := max(fpsCount, 1)
				culled, painted := u.DamageStats()
				if skippedFrames > 0 {
					fmt.Fprintf(os.Stderr, "shell: %d frames skipped (nothing visible changed)\n", skippedFrames)
					skippedFrames = 0
				}
				dr, bi, pi, sc, vb, en, pa := gpu.CallStats()
				mc, lc := u.FrameCounts()
				fmt.Fprintf(os.Stderr, "shell: per frame calls: %d draws, %d bind groups, %d pipelines, %d scissors, %d vertex bufs, %d encoders, %d passes | ui: %d measures, %d layouts\n",
					dr/int64(n), bi/int64(n), pi/int64(n), sc/int64(n), vb/int64(n), en/int64(n), pa/int64(n), mc/n, lc/n)
				area := 0.0
				if dmgFrames > 0 {
					area = 100 * dmgArea / float64(dmgFrames) / float64(w*h)
				}
				// cpu= is the process's CPU so far (all threads): the slope of
				// this figure against the frame count across a run's steady
				// lines is the per-frame cost without any start-up term.
				cpuSoFar := 0.0
				if c, ok := processCPU(); ok {
					cpuSoFar = c.Seconds()
				}
				fmt.Fprintf(os.Stderr,
					"shell: %d fps, %d loop passes, frames=%d cpu=%.3fs | per frame: %d buffer uploads (%d KB, %.2f ms), %d texture uploads (%d KB, %.2f ms) | damage: %d/%d frames partial, %.1f%% of the window, paint %d culled / %d ran\n",
					fpsCount, loopPasses, rendered, cpuSoFar, bc/int64(n), bb/int64(n)/1024,
					float64(bn)/float64(n)/1e6, tc/int64(n), tb/int64(n)/1024,
					float64(tn)/float64(n)/1e6, dmgFrames, fpsCount, area, culled, painted)
				ms := func(d time.Duration) float64 { return float64(d) / float64(n) / 1e6 }
				fmt.Fprintf(os.Stderr, "shell: per frame ms: layout %.2f, layers %.2f, paint %.2f, flush %.2f, present %.2f | wait %.2f\n",
					ms(tLayout), ms(tLayers), ms(tPaint), ms(tFlush), ms(tPresent), ms(tWait))
				tLayout, tLayers, tPaint, tFlush, tPresent, tWait = 0, 0, 0, 0, 0, 0
				fmt.Fprintf(os.Stderr, "shell: layers re-rendered per frame: %.2f (not-ready %d, rect %d, revision %d, blur %d)\n",
					float64(ui.LayerRenders)/float64(n), ui.LayerRenderWhy[0], ui.LayerRenderWhy[1], ui.LayerRenderWhy[2], ui.LayerRenderWhy[3])
				ui.LayerRenders = 0
				ui.LayerRenderWhy = [4]int{}
				dmgArea, dmgFrames = 0, 0
				gpu.ResetUploadStats()
				fpsCount, loopPasses, fpsAt = 0, 0, now
			}
		}

		// NOTHING VISIBLE CHANGED? Lay out first (state is current either
		// way) and diff before touching the swapchain: an animation that
		// ticked but moved nothing on screen (the buttons page's ring
		// below the fold), or a property write that changed no pixel,
		// then costs no encoder, no submit and no present — which together
		// are half a millisecond a frame of wgpu and driver time.
		lw, lh := float32(w)/scale, float32(h)/scale
		tMark = time.Now()
		u.Layout(lw, lh)
		lap(&tLayout)
		if damageOK {
			if resized || exposed || rendered == 0 || presentFallback != lastFallback {
				u.ForceFullRedraw()
			}
			lastFallback = presentFallback
			u.PrepareDamage()
			if dr, full := u.Damage(); !full && (dr.W <= 0 || dr.H <= 0 ||
				dr.X >= lw || dr.Y >= lh || dr.X+dr.W <= 0 || dr.Y+dr.H <= 0) {
				skippedFrames++
				u.Frame(lw, lh) // consume the prepared diff; paints nothing visible
				lastRev = core.Revision()
				lastFrame = now
				return true
			}
		}
		acquireAt := time.Now()
		tex, ok := surface.CurrentTexture()
		if !ok {
			// Outdated swapchain (mid-resize): reconfigure and retry in the
			// same frame so we still present at the new size.
			surface.Configure(device, w, h, format)
			tex, ok = surface.CurrentTexture()
			if !ok {
				return true
			}
		}
		// SWAPCHAIN WATCHDOG. A vsync'd (FIFO) acquire waits for the
		// display's vblank; when the server or the compositor stops
		// delivering those to us — seen after a desktop session restart,
		// where the very same windows fell from 60fps to one frame a
		// second while a headless render was fine — every frame blocks
		// for the driver's ~1s timeout and the app looks frozen. Three
		// stalls in a row and we stop asking for vsync: immediate
		// presentation, which the interactive-resize path already uses.
		// Rate is still bounded by the animation cap above.
		// (Timed from the acquire through the present below: the wait can
		// land in either call depending on the driver.)
		view := tex.CreateView()

		tMark = time.Now()
		// Offscreen layers (blurred/frosted items) render into their own
		// targets first: the frame below samples them. The DPI scale has to
		// be in effect already — the targets are sized in real pixels.
		r.SetScale(scale)
		u.RenderLayers()
		lap(&tLayers)
		enc := device.CreateCommandEncoder()
		if hooks.Encode3D != nil {
			if err := hooks.Encode3D(a, enc, view, depthView); err != nil {
				fmt.Fprintln(os.Stderr, "shell:", err)
				return false
			}
		}
		r.Begin(w, h)
		r.SetScale(scale) // after children may have set their own (child.go)
		// DAMAGE-DRIVEN REPAINT (ui/damage.go). The diff runs against the
		// laid-out tree before anything is recorded, so the frame can open
		// with the clear colour over just the damaged rectangle, paint only
		// what touches it, and draw into a pass that LOADS the persistent
		// MSAA target — the resolve still writes the whole swapchain image.
		// Anything the diff cannot see forces a full frame.
		dmgRect, dmgFull := u.Damage()
		partial := damageOK && !dmgFull
		if partial {
			// Re-base the damaged rectangle on the clear colour: the pass
			// below loads last frame, and a translucent element painted
			// over its own old pixels would otherwise double up.
			r.FillRect(dmgRect.X, dmgRect.Y, dmgRect.W, dmgRect.H,
				render.RGBA{R: float32(clear.R), G: float32(clear.G), B: float32(clear.B), A: 1})
		}
		u.Frame(lw, lh)
		lap(&tPaint)
		var pass *gpu.RenderPass
		switch {
		case partial:
			pass = enc.BeginRenderPassResolveLoad(msaaView, view)
			x0 := int(math.Floor(float64(dmgRect.X * scale)))
			y0 := int(math.Floor(float64(dmgRect.Y * scale)))
			x1 := int(math.Ceil(float64((dmgRect.X + dmgRect.W) * scale)))
			y1 := int(math.Ceil(float64((dmgRect.Y + dmgRect.H) * scale)))
			r.SetDamage(x0, y0, x1-x0, y1-y0)
			if fpsEvery > 0 {
				dmgArea += float64((x1 - x0) * (y1 - y0))
				dmgFrames++
			}
		case msaaView != nil:
			pass = enc.BeginRenderPassResolve(msaaView, view, clear)
		case hooks.Encode3D != nil:
			pass = enc.BeginRenderPassLoad(view)
		default:
			pass = enc.BeginRenderPass(view, clear)
		}
		err := r.Flush(pass)
		r.ClearDamage()
		lap(&tFlush)
		if err != nil {
			fmt.Fprintln(os.Stderr, "shell:", err)
			return false
		}
		pass.End()
		queue.Submit(enc.Finish())
		// Present BEFORE releasing the swapchain texture — releasing first
		// leaks the swapchain slot until acquire blocks (frozen window).
		surface.Present()
		view.Release()
		tex.Release()
		lap(&tPresent)
		if !presentFallback {
			if time.Since(acquireAt) > 250*time.Millisecond {
				acquireStalls++
			} else {
				acquireStalls = 0
			}
			if acquireStalls >= 3 {
				presentFallback = true
				gpu.SetPresentOverride("immediate")
				surface.Configure(device, w, h, format)
				fmt.Fprintln(os.Stderr, "shell: the vsync'd swapchain stalled three frames running; presenting immediately from now on")
			}
		}
		if rendered == 0 {
			win.Show() // first real frame is on screen — reveal the window
		}
		rendered++
		// Capture the revision AFTER the frame: building/painting flushes
		// bindings and runs hover/press callbacks, whose property writes are
		// already reflected in what was just presented.
		lastRev = core.Revision()
		return o.MaxFrames == 0 || rendered < o.MaxFrames
	})
	return nil
}

// newFont loads the preferred font files, then the platform defaults, and
// attaches CJK/emoji fallbacks.
func newFont(r *render.Renderer2D, o Options) (*render.Font, error) {
	data := []byte(nil)
	path := ""
	for _, p := range o.FontPaths {
		if b, err := os.ReadFile(p); err == nil {
			data, path = b, p
			break
		}
	}
	if data == nil {
		data, path = defaultFontFile()
	}
	font, err := r.NewFont(data, o.FontSize)
	if err != nil {
		return nil, err
	}
	fb, eb := fallbackFontBytes(), emojiFontBytes()
	sym := symbolFontChain()
	if fb != nil || len(sym) > 0 {
		font.SetFallback(append([][]byte{fb}, sym...)...)
	}
	if eb != nil {
		_ = font.SetEmojiFallback(eb)
	}
	// Named families (monospace/serif) inherit the same fallbacks, so
	// switching font family keeps emoji/CJK coverage.
	r.RegisterFallbacks(fb, eb, sym)
	registerNamedFonts(r)
	applyTextTuning(r)
	applyTextPath(r, o.MSDFText)
	registerDefaultStyles(r, path)
	return font, nil
}

// ---- undecorated window move/resize ------------------------------------

const (
	dragNone = iota
	dragMove
	dragResize
)

// WindowDrag drives app-side window move/resize for undecorated windows:
// deltas are tracked in root (screen) coordinates via RootPointer, so they
// stay valid while the window itself moves under the cursor. shell.Run
// uses it automatically; apps with their own frame loops call Handle each
// frame with the window's mouse events.
type WindowDrag struct {
	mode             int
	edge             app.ResizeEdge
	startPX, startPY int // root pointer at press
	startX, startY   int // window origin at press
	startW, startH   int
}

// Active reports whether a move/resize drag is in progress (callers
// usually suppress camera/selection input while it is).
func (d *WindowDrag) Active() bool { return d.mode != dragNone }

// Handle consumes a frame's mouse events: presses on the window's border
// band start a resize, presses where inDragRegion reports true start a
// move; motion is applied via SetGeometry until release.
func (d *WindowDrag) Handle(win *app.Window, w, h float32, mouse []app.MouseEvent,
	band float32, inDragRegion func(x, y float32) bool) {
	if band <= 0 {
		band = 5
	}
	for _, m := range mouse {
		switch m.Kind {
		case app.MouseDown:
			if m.Button != 1 || d.mode != dragNone {
				continue
			}
			edge, onEdge := edgeAt(m.X, m.Y, w, h, band)
			inDrag := inDragRegion != nil && inDragRegion(m.X, m.Y)
			if !onEdge && !inDrag {
				continue
			}
			d.startPX, d.startPY = win.RootPointer()
			d.startX, d.startY, d.startW, d.startH = win.Geometry()
			if onEdge {
				d.mode, d.edge = dragResize, edge
			} else {
				d.mode = dragMove
			}
		case app.MouseUp:
			if m.Button == 1 {
				d.mode = dragNone
			}
		}
	}
	if d.mode == dragNone {
		return
	}
	px, py := win.RootPointer()
	dx, dy := px-d.startPX, py-d.startPY
	switch d.mode {
	case dragMove:
		win.SetGeometry(d.startX+dx, d.startY+dy, d.startW, d.startH)
	case dragResize:
		x, y, gw, gh := d.startX, d.startY, d.startW, d.startH
		switch d.edge {
		case app.EdgeLeft, app.EdgeTopLeft, app.EdgeBottomLeft:
			x, gw = d.startX+dx, d.startW-dx
		case app.EdgeRight, app.EdgeTopRight, app.EdgeBottomRight:
			gw = d.startW + dx
		}
		switch d.edge {
		case app.EdgeTop, app.EdgeTopLeft, app.EdgeTopRight:
			y, gh = d.startY+dy, d.startH-dy
		case app.EdgeBottom, app.EdgeBottomLeft, app.EdgeBottomRight:
			gh = d.startH + dy
		}
		// Resize floor: 320x240, but never above the window's own starting
		// size — dialogs smaller than the floor must keep their size, and
		// SetGeometry itself no longer clamps (a move of a small window used
		// to grow it to the floor on the first titlebar click).
		minW, minH := 320, 240
		if d.startW < minW {
			minW = d.startW
		}
		if d.startH < minH {
			minH = d.startH
		}
		if gw < minW {
			gw = minW
			if d.edge == app.EdgeLeft || d.edge == app.EdgeTopLeft || d.edge == app.EdgeBottomLeft {
				x = d.startX + d.startW - gw
			}
		}
		if gh < minH {
			gh = minH
			if d.edge == app.EdgeTop || d.edge == app.EdgeTopLeft || d.edge == app.EdgeTopRight {
				y = d.startY + d.startH - gh
			}
		}
		win.SetGeometry(x, y, gw, gh)
	}
}

// edgeAt maps a point near the window border to its resize edge.
func edgeAt(x, y, w, h, band float32) (app.ResizeEdge, bool) {
	l, r := x < band, x > w-band
	t, b := y < band, y > h-band
	switch {
	case t && l:
		return app.EdgeTopLeft, true
	case t && r:
		return app.EdgeTopRight, true
	case b && l:
		return app.EdgeBottomLeft, true
	case b && r:
		return app.EdgeBottomRight, true
	case t:
		return app.EdgeTop, true
	case b:
		return app.EdgeBottom, true
	case l:
		return app.EdgeLeft, true
	case r:
		return app.EdgeRight, true
	}
	return 0, false
}
