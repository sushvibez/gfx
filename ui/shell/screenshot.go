//go:build !js

package shell

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"runtime/pprof"
	"strconv"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// ScriptStepSettle is how much animation time a scripted step advances
// before its capture. The QtTest case generated from the same script waits
// the same amount (scripts/qml_interact_gen.py), so a running animation is
// sampled at the same moment on both sides.
const ScriptStepSettle = 700 * time.Millisecond

// ScreenshotScript renders the app headlessly and captures a SEQUENCE of
// PNGs, letting the caller drive input between them — a scripted
// interaction pass: click a tab, capture; drag a slider, capture.
//
// step is called before each capture with the frame's UI, so it can inject
// clicks, drags and keys (ui.UI.Click / Drag / Fling / Key), and returns
// the file to write and whether to keep going. Between steps the frame
// settles exactly as Screenshot's does, so animations a step starts (a
// menu opening, a tab sliding) are captured after they finish.
func ScreenshotScript(o Options, hooks Hooks, step func(i int, u *ui.UI) (path string, more bool)) error {
	return screenshotRun(o, hooks, "", step)
}

// Screenshot renders the app headlessly (no window) and writes a PNG.
// With Encode3D two frames run — the first lays out the UI so offscreen
// viewports (View3D targets) get sized. Pure-2D apps render with 4x MSAA.
func Screenshot(o Options, hooks Hooks, path string) error {
	return screenshotRun(o, hooks, path, nil)
}

func screenshotRun(o Options, hooks Hooks, path string, step func(i int, u *ui.UI) (string, bool)) error {
	defer gpu.FlushCallTrace() // a run shorter than GFX_CALLS's window still reports
	if p := os.Getenv("GFX_CPUPROFILE"); p != "" {
		f, err := os.Create(p)
		if err == nil {
			pprof.StartCPUProfile(f)
			defer func() {
				pprof.StopCPUProfile()
				f.Close()
			}()
		}
	}
	if o.FontSize == 0 {
		o.FontSize = 18
	}
	// A capture is sampled at a KNOWN animation time, so the clock advances
	// only in the slices below — not with the wall clock, which would add
	// this process' startup and GPU init on top and land a moving page
	// later than the reference it is compared against.
	core.SetVirtualClock(true)
	defer core.SetVirtualClock(false)
	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		return err
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		return err
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		return err
	}
	queue := device.Queue()

	target := device.CreateRenderTexture(o.Width, o.Height, gpu.FormatRGBA8Unorm)
	texView := target.CreateView()

	msaa := 0
	if hooks.Encode3D == nil || hooks.Offscreen3D {
		msaa = 4 // 2D over nothing drawn directly: anti-alias path/SVG edges via resolve
	}
	var r *render.Renderer2D
	var msaaView *gpu.TextureView
	if msaa > 0 {
		r, err = render.New2DMSAA(device, queue, gpu.FormatRGBA8Unorm, msaa)
		if err != nil {
			return err
		}
		msaaTex := device.CreateMSAATexture(o.Width, o.Height, gpu.FormatRGBA8Unorm, msaa)
		msaaView = msaaTex.CreateView()
	} else {
		r, err = render.New2D(device, queue, gpu.FormatRGBA8Unorm)
		if err != nil {
			return err
		}
	}
	font, err := newFont(r, o)
	if err != nil {
		return err
	}
	a := &App{Device: device, Queue: queue, Format: gpu.FormatRGBA8Unorm,
		R2: r, Font: font, W: o.Width, H: o.Height}
	view, err := hooks.Init(a)
	if err != nil {
		return err
	}
	u := ui.New(r, font, view)
	a.UI = u

	var depthView *gpu.TextureView
	if hooks.Encode3D != nil {
		depthView = device.CreateDepthTexture(o.Width, o.Height).CreateView()
	}
	clear := gpu.Color{
		R: float64(o.ClearHex>>16&0xff) / 255,
		G: float64(o.ClearHex>>8&0xff) / 255,
		B: float64(o.ClearHex&0xff) / 255,
		A: 1,
	}

	frames := 1
	if hooks.Encode3D != nil {
		frames = 2
	}
	// Layout-dependent views settle over successive frames: a binding that
	// reads its own item's geometry (gml's `width: parent.side`, anchored
	// subtrees) only sees real bounds after the frame that laid them out.
	// Keep rendering while property revisions keep moving, up to a cap, so
	// a headless shot matches what an interactive window shows.
	//
	// Time advances too: without it every tween sits at its start value, so
	// anything a Behavior or a state transition animates (a progress bar's
	// fill, a panel sliding in) would be captured as if it had never begun.
	// A settle step is a big slice of time, enough for the short tweens UI
	// uses to reach their targets.
	//
	// One capture: settle the frame (animations included) and read the
	// target back into a PNG. A scripted pass runs this once per step.
	settleAndShoot := func(out string, fixed time.Duration) error {
		// A page whose animations keep running has no "settled" state, so
		// the capture happens at a KNOWN time instead: keep advancing until
		// nothing is moving, up to this budget. It is the same 2.5s the
		// QML compare harness lets Qt run before grabbing its reference, so
		// an animated page is sampled at the same phase on both sides.
		settleBudget := 2500 * time.Millisecond
		if v := os.Getenv("GFX_SETTLE_MS"); v != "" { // profiling knob
			if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
				settleBudget = time.Duration(ms) * time.Millisecond
			}
		}
		// The step is a 60fps FRAME, not a coarse slice. Pages routinely
		// cap their own advance (`Math.min(frameTime, 0.05)`, the corpus
		// idiom, so one long frame cannot fling a simulation across the
		// screen): fed 125ms slices, such a page threw away 75ms of every
		// one and its clock ran at 40% of the budget, which is why the
		// portal's swirl and the toast queue sat at the wrong phase. A
		// static page still exits after a couple of frames — the loop
		// breaks as soon as nothing is moving.
		settleStep := time.Second / 60
		steps := int(settleBudget / settleStep) // exactly the budget
		if fixed > 0 {
			// A scripted step advances a FIXED slice of animation time, so
			// both engines sample a running animation at the same moment
			// (the QtTest case the same script generates waits exactly this
			// long). Settling "until nothing moves" instead would capture
			// our side at the end of a tween and Qt's mid-flight.
			steps = max(int(fixed/(time.Second/60)), 1)
			settleStep = fixed / time.Duration(steps)
		}
		lastRev := ^uint64(0)
		for i := 0; i < steps; i++ {
			core.TickAnimations(settleStep)
			u.Layout(float32(o.Width), float32(o.Height))
			u.RenderLayers()
			enc := device.CreateCommandEncoder()
			if hooks.Encode3D != nil {
				if err := hooks.Encode3D(a, enc, texView, depthView); err != nil {
					return err
				}
			}
			r.Begin(o.Width, o.Height)
			u.Frame(float32(o.Width), float32(o.Height))
			var pass *gpu.RenderPass
			switch {
			case msaa > 0:
				pass = enc.BeginRenderPassResolve(msaaView, texView, clear)
			case hooks.Encode3D != nil:
				pass = enc.BeginRenderPassLoad(texView)
			default:
				pass = enc.BeginRenderPass(texView, clear)
			}
			if err := r.Flush(pass); err != nil {
				return err
			}
			pass.End()
			queue.Submit(enc.Finish())
			if fixed == 0 && i+1 >= frames && core.Revision() == lastRev &&
				!core.AnimationsRunning() && !u.WantsFrame() {
				break
			}
			lastRev = core.Revision()
		}
		pix, err := device.ReadTexture(queue, target, o.Width, o.Height)
		if err != nil {
			return err
		}
		img := &image.RGBA{Pix: pix, Stride: o.Width * 4, Rect: image.Rect(0, 0, o.Width, o.Height)}
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			return err
		}
		fmt.Println("wrote", out)
		return nil
	}

	if step == nil {
		return settleAndShoot(path, 0)
	}
	for i := 0; ; i++ {
		out, more := step(i, u)
		if out != "" {
			// The first capture is the page as it opens — settled, like a
			// plain screenshot. Every step after it advances the fixed
			// slice both engines share.
			budget := ScriptStepSettle
			if i == 0 {
				budget = 0
			}
			if err := settleAndShoot(out, budget); err != nil {
				return err
			}
		}
		if !more {
			return nil
		}
	}
}
