//go:build linux || windows || js

package shell

import (
	"fmt"
	"os"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/ui"
)

// ChildWindow is a secondary top-level window opened by a running shell App
// (dialogs, tool palettes, multi-document views). It shares the App's GPU
// device, renderer and fonts; it has its own surface, UI tree, input focus
// and lifetime. Close it programmatically with Close, or let the user close
// it from the window manager — either way OnClose (if set) fires once and
// the shell tears the window down.
type ChildWindow struct {
	Win     *app.Window
	UI      *ui.UI
	OnClose func()
	// Encode3D, when set, records 3D work for this window each frame before
	// the 2D pass (which then LOADS the target instead of clearing it), with
	// a depth view sized to the window — the child-window twin of
	// Hooks.Encode3D. Set it right after OpenChild.
	Encode3D func(c *ChildWindow, enc *gpu.CommandEncoder, target, depth *gpu.TextureView) error
	// Clear is the 2D pass clear colour (default white).
	Clear gpu.Color

	view       func() ui.Element
	surface    *gpu.Surface
	msaaTex    *gpu.Texture
	msaaView   *gpu.TextureView
	depth      *gpu.Texture
	depthView  *gpu.TextureView
	w, h       int
	closing    bool
	lastRev    uint64
	animT      time.Time
	drag       WindowDrag
	dragRegion func(x, y float32) bool // undecorated: custom-titlebar move
	resizeBand float32                 // undecorated: border resize width
}

// SetDragRegion marks the window region (logical px) that drag-moves an
// UNDECORATED child window — its custom titlebar. Decorated children move
// via the OS titlebar and ignore this.
func (c *ChildWindow) SetDragRegion(in func(x, y float32) bool) { c.dragRegion = in }

// Close requests the child window's teardown (processed by the shell loop
// this frame).
func (c *ChildWindow) Close() { c.closing = true }

// Size returns the window's pixel size.
func (c *ChildWindow) Size() (int, int) { return c.w, c.h }

// childHost is the shell loop's handle for managing children; stored on App.
type childHost struct {
	inst       *gpu.Instance
	device     *gpu.Device
	format     gpu.TextureFormat
	msaa       int
	lineHeight float64
	children   []*ChildWindow
}

// OpenChild opens a secondary window with its own view function. Call it
// from Init or any UI callback (same goroutine as the frame loop). Children
// share the App's renderer and fonts — their text and styling match the
// primary window. Multi-window is platform-dependent (X11 and Windows); on
// other backends this returns the app layer's error.
func (a *App) OpenChild(o Options, view func() ui.Element) (*ChildWindow, error) {
	if a.host == nil || a.Win == nil {
		return nil, fmt.Errorf("shell: OpenChild needs a running windowed shell")
	}
	cw, err := a.Win.NewChild(o.window(false))
	if err != nil {
		return nil, err
	}
	h := a.host
	surface, err := plat.NewSurface(h.inst, cw)
	if err != nil {
		a.Win.CloseChild(cw)
		return nil, err
	}
	w, hh := cw.Size()
	surface.Configure(h.device, w, hh, h.format)
	c := &ChildWindow{Win: cw, view: view, surface: surface, w: w, h: hh,
		Clear: gpu.Color{R: 1, G: 1, B: 1, A: 1}}
	c.resizeBand = o.resizeBand()
	c.UI = ui.New(a.R2, a.Font, view)
	c.UI.SetLineHeight(h.lineHeight)
	if h.msaa > 0 {
		c.msaaTex = h.device.CreateMSAATexture(w, hh, h.format, h.msaa)
		c.msaaView = c.msaaTex.CreateView()
	}
	h.children = append(h.children, c)
	return c, nil
}

// frame processes one frame for the child: input, resize, render, present.
// Reports whether the child is still alive.
func (c *ChildWindow) frame(h *childHost, a *App, force bool) bool {
	if c.closing || c.Win.Closed() {
		c.teardown(h, a)
		return false
	}
	mouse := c.Win.MouseEvents()
	keys := c.Win.KeyEvents()
	// Undecorated children handle their own move (custom titlebar region)
	// and border-band resize, like shell.Run does for the primary window.
	if c.dragRegion != nil || c.resizeBand > 0 {
		scale := c.Win.Scale()
		c.drag.Handle(c.Win, float32(c.w)/scale, float32(c.h)/scale, mouse,
			c.resizeBand, c.dragRegion)
	}
	c.UI.Mouse(mouse)
	for _, k := range keys {
		c.UI.Key(k)
	}
	c.Win.SetCursor(c.UI.Cursor())
	if c.animT.IsZero() || c.Win.Focused() {
		c.animT = time.Now()
	}
	c.UI.SetAnimTime(c.animT)
	resized := false
	if nw, nh := c.Win.Size(); nw > 0 && nh > 0 && (nw != c.w || nh != c.h) {
		c.w, c.h = nw, nh
		resized = true
		c.surface.Configure(h.device, c.w, c.h, h.format)
		if c.msaaView != nil {
			c.msaaView.Release()
			c.msaaTex.Release()
			c.msaaTex = h.device.CreateMSAATexture(c.w, c.h, h.format, h.msaa)
			c.msaaView = c.msaaTex.CreateView()
		}
		if c.depthView != nil {
			c.depthView.Release()
			c.depth.Release()
			c.depth, c.depthView = nil, nil
		}
	}
	// A 3D child renders every frame (its scene animates; the 2D UI's
	// damage rule cannot see it).
	if c.Encode3D != nil {
		force = true
		if c.depthView == nil {
			c.depth = h.device.CreateDepthTexture(c.w, c.h)
			c.depthView = c.depth.CreateView()
		}
	}
	// Same present-on-damage rule as the primary window; force covers
	// global invalidation (animations/revision) computed by the caller.
	// Unfocused children pause their time-driven content too.
	if !force && len(mouse) == 0 && len(keys) == 0 && !resized &&
		!c.Win.TakeDamage() && !(c.UI.WantsFrame() && c.Win.Focused()) && c.lastRev != 0 {
		return true
	}
	tex, ok := c.surface.CurrentTexture()
	if !ok {
		c.surface.Configure(h.device, c.w, c.h, h.format)
		if tex, ok = c.surface.CurrentTexture(); !ok {
			return true
		}
	}
	view := tex.CreateView()
	scale := c.Win.Scale() // logical-pixel UI; see shell.Run
	lw, lh := float32(c.w)/scale, float32(c.h)/scale
	c.UI.Layout(lw, lh)
	a.R2.SetScale(scale) // layer targets are sized in real pixels
	c.UI.RenderLayers()
	enc := h.device.CreateCommandEncoder()
	if c.Encode3D != nil {
		if err := c.Encode3D(c, enc, view, c.depthView); err != nil {
			fmt.Fprintln(os.Stderr, "shell: child Encode3D:", err)
		}
	}
	a.R2.Begin(c.w, c.h)
	a.R2.SetScale(scale)
	c.UI.Frame(lw, lh)
	var pass *gpu.RenderPass
	switch {
	case c.Encode3D != nil:
		pass = enc.BeginRenderPassLoad(view)
	case c.msaaView != nil:
		pass = enc.BeginRenderPassResolve(c.msaaView, view, c.Clear)
	default:
		pass = enc.BeginRenderPass(view, c.Clear)
	}
	if err := a.R2.Flush(pass); err == nil {
		pass.End()
		a.Queue.Submit(enc.Finish())
		c.surface.Present()
	}
	view.Release()
	tex.Release()
	c.lastRev = 1
	return true
}

func (c *ChildWindow) teardown(h *childHost, a *App) {
	if c.msaaView != nil {
		c.msaaView.Release()
		c.msaaTex.Release()
	}
	if c.depthView != nil {
		c.depthView.Release()
		c.depth.Release()
	}
	c.surface.Release()
	a.Win.CloseChild(c.Win)
	for i, cc := range h.children {
		if cc == c {
			h.children = append(h.children[:i], h.children[i+1:]...)
			break
		}
	}
	if c.OnClose != nil {
		c.OnClose()
	}
}
