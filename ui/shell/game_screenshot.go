//go:build linux || windows

package shell

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/render"
)

// ScreenshotGame steps a game `steps` times at a fixed dt (no window, fully
// headless), then writes the final frame as a PNG. prep, if non-nil, runs
// before each step to script input (held keys, one-shot events). This is the
// game-loop sibling of Screenshot, used to capture a deterministic frame.
func ScreenshotGame(o Options, steps int, dt float64, update func(*Frame) bool, prep func(step int, in *Input), path string) error {
	defer gpu.FlushCallTrace() // a run shorter than GFX_CALLS's window still reports
	if o.FontSize == 0 {
		o.FontSize = 18
	}
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
	const msaa = 4
	r, err := render.New2DMSAA(device, queue, gpu.FormatRGBA8Unorm, msaa)
	if err != nil {
		return err
	}
	msaaTex := device.CreateMSAATexture(o.Width, o.Height, gpu.FormatRGBA8Unorm, msaa)
	font, err := r.NewFont(defaultFontBytes(), o.FontSize)
	if err != nil {
		return err
	}
	// CJK plus the symbol chain, the same coverage the UI shell installs:
	// a game's HUD uses the same arrows and dingbats.
	if fb, sym := fallbackFontBytes(), symbolFontChain(); fb != nil || len(sym) > 0 {
		font.SetFallback(append([][]byte{fb}, sym...)...)
	}
	registerNamedFonts(r)
	applyTextTuning(r)
	applyTextPath(r, o.MSDFText)
	texView := target.CreateView()
	msaaView := msaaTex.CreateView()
	in := NewInput()

	if steps < 1 {
		steps = 1
	}
	for s := 0; s < steps; s++ {
		in.events = nil
		if prep != nil {
			prep(s, in)
		}
		r.Begin(o.Width, o.Height)
		update(&Frame{G: r, Font: font, W: o.Width, H: o.Height, Dt: dt, In: in})
		enc := device.CreateCommandEncoder()
		var pass *gpu.RenderPass
		if s == steps-1 {
			pass = enc.BeginRenderPassResolve(msaaView, texView, gpu.Color{A: 1})
		} else {
			// Intermediate steps still need a pass to flush draw state, but we
			// discard them (render into the msaa target, no resolve readback).
			pass = enc.BeginRenderPass(msaaView, gpu.Color{A: 1})
		}
		if err := r.Flush(pass); err != nil {
			return err
		}
		pass.End()
		queue.Submit(enc.Finish())
	}
	pix, err := device.ReadTexture(queue, target, o.Width, o.Height)
	if err != nil {
		return err
	}
	img := &image.RGBA{Pix: pix, Stride: o.Width * 4, Rect: image.Rect(0, 0, o.Width, o.Height)}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}
