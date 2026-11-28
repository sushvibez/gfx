//go:build linux || windows || js

package shell

import (
	"fmt"
	"os"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
)

// splashOn reports whether Run puts up a splash before Hooks.Init: one was
// asked for and the window will be seen.
func (o Options) splashOn() bool {
	return !o.Hidden && (o.Splash != nil || len(o.SplashImage) > 0)
}

// splash is the start-up splash: presented on its own while Hooks.Init
// runs (again on every App.SplashStatus), then — with SplashMin or
// SplashFade set — drawn over the app's first frames, opaque until
// SplashMin has passed since it went up and Init has returned, then fading
// out over SplashFade.
type splash struct {
	o        Options
	win      *app.Window
	surface  *gpu.Surface
	device   *gpu.Device
	queue    *gpu.Queue
	r        *render.Renderer2D
	msaaView *gpu.TextureView
	w, h     int

	img      *render.Image
	font     *render.Font // from Init on, for the status line
	status   string
	progress float32 // 0…1 as Init reports it (App.SplashProgress); <0: no bar
	shown    bool    // the window is up

	// own is a splash in a window of its own (Options.SplashWindow): the
	// app's window stays hidden until Init and the hold are over, then
	// shows and this one closes. msaaTex is its own target when the
	// renderer is multisampled.
	own     bool
	msaaTex *gpu.Texture
	// barFrom is the bar where Init left it, which the hold then fills.
	barFrom float32

	inInit   bool
	shownAt  time.Time
	initDone time.Time
	over     bool // the overlay has finished (and the image is freed)
}

// loadImage decodes o.SplashImage (nil without one, or on a bad image).
func (s *splash) loadImage() {
	if len(s.o.SplashImage) == 0 {
		return
	}
	img, err := s.r.LoadImage(s.o.SplashImage)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shell: splash image:", err)
		return
	}
	s.img = img
}

// present draws the splash into one swapchain image and presents it,
// showing the window the first time, so the window comes up at its final
// size with the splash on it while Init builds the app. A failure only
// costs the splash (the window then shows the first real frame).
func (s *splash) present() {
	if !s.shown {
		// Map the window first: an image presented to a window that is
		// not mapped yet is not kept on X11 (Xwayland, Xephyr) — the
		// window then came up black and stayed so until the first real
		// frame, after Init.
		s.win.Show()
		s.shown = true
		s.shownAt = time.Now()
	}
	tex, ok := s.surface.CurrentTexture()
	if !ok {
		return
	}
	view := tex.CreateView()
	scale := s.win.Scale()
	if scale <= 0 {
		scale = 1
	}
	s.r.Begin(s.w, s.h)
	s.r.SetScale(scale)
	s.draw(float32(s.w)/scale, float32(s.h)/scale, 1)
	enc := s.device.CreateCommandEncoder()
	var pass *gpu.RenderPass
	if s.msaaView != nil {
		pass = enc.BeginRenderPassResolve(s.msaaView, view, s.o.clearColor())
	} else {
		pass = enc.BeginRenderPass(view, s.o.clearColor())
	}
	err := s.r.Flush(pass)
	pass.End()
	if err == nil {
		s.queue.Submit(enc.Finish())
		s.surface.Present()
	} else {
		fmt.Fprintln(os.Stderr, "shell: splash:", err)
	}
	view.Release()
	tex.Release()
}

// draw records the splash over a lw×lh logical window at opacity alpha:
// the clear colour, the image centred (fitted into what the window leaves
// it), the status line and the progress bar under it, and — while opaque
// — the Splash hook. In a window of its own the image takes the space
// above a footer with the line and the bar; over the app's window they sit
// under the image at its own size.
func (s *splash) draw(lw, lh, alpha float32) {
	bg := s.o.clearColor()
	s.r.FillRect(0, 0, lw, lh, render.RGBA{R: float32(bg.R), G: float32(bg.G), B: float32(bg.B), A: alpha})
	const footer = 64
	areaH := lh * 0.9
	if s.own {
		areaH = lh - footer - 24
	}
	bottom := lh / 2
	if s.img != nil {
		// One image pixel to a logical pixel at most: the same size on a
		// high-DPI screen as the app's own layout.
		iw, ih := float32(s.img.W), float32(s.img.H)
		if k := min(lw*0.9/iw, areaH/ih); k < 1 {
			iw, ih = iw*k, ih*k
		}
		y := (lh - ih) / 2
		if s.own {
			y = 24 + (areaH-ih)/2
		}
		s.r.DrawImage(s.img, (lw-iw)/2, y, iw, ih, render.RGBA{R: 1, G: 1, B: 1, A: alpha})
		bottom = y + ih
	}
	text := s.o.splashTextColor()
	text.A *= alpha
	barW := min(lw-64, 280)
	textY, barY := bottom+24, bottom+24+s.lineH()+10
	if s.own {
		textY, barY = lh-footer+8, lh-footer+8+s.lineH()+10
	}
	if s.status != "" && s.font != nil {
		tw, _ := s.r.MeasureText(s.font, s.status)
		s.r.DrawText(s.font, float32(int((lw-tw)/2)), float32(int(textY)), s.status, text)
	}
	if p := s.barAt(time.Now()); p >= 0 {
		x := float32(int((lw - barW) / 2))
		track := text
		track.A *= 0.25
		s.r.FillRoundRect(x, barY, barW, 3, 1.5, track)
		acc := s.o.splashAccentColor()
		acc.A *= alpha
		s.r.FillRoundRect(x, barY, max(3, barW*p), 3, 1.5, acc)
	}
	if s.own {
		// A hairline round the window, so it reads against a desktop of
		// its own colour.
		edge := s.o.splashTextColor()
		edge.A = 0.22
		s.r.FillRect(0, 0, lw, 1, edge)
		s.r.FillRect(0, lh-1, lw, 1, edge)
		s.r.FillRect(0, 1, 1, lh-2, edge)
		s.r.FillRect(lw-1, 1, 1, lh-2, edge)
	}
	if s.o.Splash != nil && alpha >= 1 {
		s.o.Splash(s.r, lw, lh)
	}
}

func (s *splash) lineH() float32 {
	if s.font != nil {
		return s.font.Height()
	}
	return 16
}

// barAt is the progress bar's fill at now, or <0 for no bar (none was
// reported): what Init reported while it runs, then filling from there to
// full over the hold, so a quick start still shows the bar complete.
func (s *splash) barAt(now time.Time) float32 {
	if s.progress < 0 {
		return -1
	}
	if s.inInit || s.initDone.IsZero() {
		return s.progress
	}
	end := s.shownAt.Add(s.o.SplashMin)
	span := end.Sub(s.initDone)
	if span <= 0 {
		return 1
	}
	t := float32(now.Sub(s.initDone)) / float32(span)
	t = max(0, min(1, t))
	t = 1 - (1-t)*(1-t) // ease out
	return s.barFrom + (1-s.barFrom)*t
}

// alpha is the overlay's opacity at now: 1 through the hold, then the fade
// (smoothstep); 0 once it is over, or when no hold or fade was asked for.
func (s *splash) alpha(now time.Time) float32 {
	if s == nil || s.own || s.over || s.inInit || (s.o.SplashMin <= 0 && s.o.SplashFade <= 0) {
		return 0
	}
	start := s.shownAt.Add(s.o.SplashMin)
	if s.initDone.After(start) {
		start = s.initDone
	}
	if now.Before(start) {
		return 1
	}
	if s.o.SplashFade <= 0 {
		return 0
	}
	t := float32(now.Sub(start)) / float32(s.o.SplashFade)
	if t >= 1 {
		return 0
	}
	return 1 - t*t*(3-2*t)
}

// finish ends the overlay and frees the image.
func (s *splash) finish() {
	if s == nil || s.over {
		return
	}
	s.over = true
	s.release()
}

func (s *splash) release() {
	if s.img != nil {
		s.img.Release()
		s.img = nil
	}
	s.closeWindow()
}

// closeWindow closes a splash's own window (once).
func (s *splash) closeWindow() {
	if !s.own || s.win == nil {
		return
	}
	if s.msaaView != nil {
		s.msaaView.Release()
		s.msaaTex.Release()
		s.msaaView, s.msaaTex = nil, nil
	}
	s.surface.Release()
	s.win.Destroy()
	s.win, s.surface = nil, nil
}

// openSplashWindow makes the splash's own window: SplashWindow's size
// (logical px), undecorated, centred on the primary monitor, sharing the
// app's device and renderer. A failure returns nil, and the splash goes on
// the app's window instead.
func openSplashWindow(o Options, inst *gpu.Instance, device *gpu.Device, queue *gpu.Queue,
	r *render.Renderer2D, format gpu.TextureFormat, msaa int) *splash {
	sw, sh := o.SplashWindow[0], o.SplashWindow[1]
	wo := app.Options{Title: o.Title, Width: sw, Height: sh, Undecorated: true, Hidden: true,
		Unresizable: true, Unminimizable: true}
	if mons, err := app.Monitors(); err == nil && len(mons) > 0 {
		m := mons[0]
		for _, c := range mons {
			if c.Primary {
				m = c
			}
		}
		wo.X, wo.Y, wo.Positioned = splashOrigin(m, sw, sh)
	}
	win, err := app.NewWindow(wo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shell: splash window:", err)
		return nil
	}
	surface, err := plat.NewSurface(inst, win)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shell: splash window:", err)
		win.Destroy()
		return nil
	}
	w, h := win.Size()
	surface.Configure(device, w, h, format)
	s := &splash{o: o, win: win, surface: surface, device: device, queue: queue, r: r, w: w, h: h, own: true, progress: -1}
	if msaa > 0 {
		s.msaaTex = device.CreateMSAATexture(w, h, format, msaa)
		s.msaaView = s.msaaTex.CreateView()
	}
	return s
}

// splashOrigin is where a splash window of logical size sw×sh goes to sit
// centred on monitor m. app.Options sizes are LOGICAL (each backend
// multiplies them by the DPI scale) while X/Y are PHYSICAL desktop pixels,
// as Monitors reports them, so the centring uses the physical size the
// window will have. (Scaling the size here as well made a 150 % Windows
// display get a 2.25× window placed as if it were 1.5×: off down and right.)
func splashOrigin(m app.Monitor, sw, sh int) (x, y int, positioned bool) {
	k := m.Scale
	if k <= 0 {
		k = 1
	}
	pw, ph := int(float32(sw)*k+0.5), int(float32(sh)*k+0.5)
	return m.X + (m.Width-pw)/2, m.Y + (m.Height-ph)/2, true
}

// hold keeps a splash in its own window up until SplashMin has passed
// since it went up, redrawing it (the bar filling) about sixty times a
// second; the app's window is still hidden.
func (s *splash) hold() {
	if s == nil || !s.own || s.win == nil {
		return
	}
	end := s.shownAt.Add(s.o.SplashMin)
	for time.Now().Before(end) {
		s.win.Poll()
		s.present()
		time.Sleep(16 * time.Millisecond)
	}
	if s.progress >= 0 {
		s.progress, s.barFrom = 1, 1
		s.present()
	}
}

// SplashStatus sets the line under the splash image and puts it on screen
// at once — for Hooks.Init to say what it is loading ("Loading
// materials…"). It does nothing outside Init or without a splash.
func (a *App) SplashStatus(text string) {
	if s := a.splash; s != nil && s.inInit && s.status != text {
		s.status = text
		s.present()
	}
}

// SplashProgress sets the splash's progress bar, 0…1, and puts it on
// screen at once — for Hooks.Init to report how far it is. The bar shows
// from the first call; after Init it fills the rest of the way over the
// hold. It does nothing outside Init or without a splash.
func (a *App) SplashProgress(f float32) {
	if s := a.splash; s != nil && s.inInit {
		f = max(0, min(1, f))
		if f != s.progress {
			s.progress = f
			s.present()
		}
	}
}

// splashAccentColor is SplashAccentHex, or the status line's colour.
func (o Options) splashAccentColor() render.RGBA {
	if h := o.SplashAccentHex; h != 0 {
		return render.RGBA{R: float32(h>>16&0xff) / 255, G: float32(h>>8&0xff) / 255, B: float32(h&0xff) / 255, A: 1}
	}
	return o.splashTextColor()
}

// splashTextColor is SplashTextHex, or a muted grey that reads on the
// clear colour (dark on a light one, light on a dark one).
func (o Options) splashTextColor() render.RGBA {
	hex := o.SplashTextHex
	if hex == 0 {
		c := o.clearColor()
		if 0.2126*c.R+0.7152*c.G+0.0722*c.B > 0.5 {
			hex = 0x5f6164
		} else {
			hex = 0x8d8d8e
		}
	}
	return render.RGBA{R: float32(hex>>16&0xff) / 255, G: float32(hex>>8&0xff) / 255, B: float32(hex&0xff) / 255, A: 1}
}

// drawSplash records the opaque splash into r for a w×h target at scale
// (what present draws), returning the loaded image to release once the
// frame is submitted.
func drawSplash(o Options, r *render.Renderer2D, w, h int, scale float32) *render.Image {
	s := &splash{o: o, r: r, progress: -1}
	s.loadImage()
	r.Begin(w, h)
	r.SetScale(scale)
	s.draw(float32(w)/scale, float32(h)/scale, 1)
	return s.img
}
