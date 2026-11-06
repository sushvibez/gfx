//go:build linux || windows || js || android

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/audio"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
)

const shader = `
@vertex
fn vs(@builtin(vertex_index) i : u32) -> @builtin(position) vec4f {
    var p = array<vec2f, 3>(vec2f(0.0, 0.6), vec2f(-0.6, -0.6), vec2f(0.6, -0.6));
    return vec4f(p[i], 0.0, 1.0);
}
@fragment
fn fs() -> @location(0) vec4f { return vec4f(0.1, 0.8, 0.3, 1.0); }
`

// noAudio / noClip skip a check, to bisect a platform problem.
var noAudio, noClip bool

func report(name string, ok bool, detail string) {
	status := "PASS"
	if !ok {
		status = "FAIL"
	}
	fmt.Printf("devcheck: %s %s — %s\n", status, name, detail)
}

// toneWAV is one second of a 440 Hz sine as a 16-bit mono WAV at 48 kHz.
func toneWAV() []byte {
	const rate, n = 48000, 48000
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+n*2))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1)) // mono
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*2))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(n*2))
	for i := 0; i < n; i++ {
		v := 0.3 * math.Sin(2*math.Pi*440*float64(i)/rate)
		binary.Write(&b, binary.LittleEndian, int16(v*32767))
	}
	return b.Bytes()
}

func run(maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "graphics: devcheck", Width: 800, Height: 600})
	if err != nil {
		return err
	}
	defer win.Destroy()

	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		report("gpu instance", false, err.Error())
		return err
	}
	defer inst.Release()
	w0, h0 := win.Size()
	report("window", w0 > 0 && h0 > 0, "native window "+describeWindow(win))
	surface, err := plat.NewSurface(inst, win)
	if err != nil {
		report("surface", false, err.Error())
		return err
	}
	report("surface", true, "created")
	defer surface.Release()
	adapter, err := inst.RequestAdapter(surface)
	if err != nil {
		report("gpu adapter", false, err.Error())
		return err
	}
	defer adapter.Release()
	device, err := adapter.RequestDevice()
	if err != nil {
		report("gpu device", false, err.Error())
		return err
	}
	defer device.Release()
	lim := device.Limits()
	report("gpu device", true, fmt.Sprintf("%s; maxBufferSize %d MB, maxStorageBufferBindingSize %d MB",
		adapter.Name(), lim.MaxBufferSize>>20, lim.MaxStorageBindingSize>>20))
	queue := device.Queue()
	defer queue.Release()

	// Clipboard round trip through the system.
	const probe = "gfx-devcheck-7319"
	if !noClip {
		app.SetClipboardText(probe)
		got := app.ClipboardText()
		report("clipboard", got == probe, fmt.Sprintf("set %q, read back %q", probe, got))
	}

	// Audio: a one-second tone; the device opens for real or falls back to
	// silence, and the voice must advance and finish either way.
	dev, err := audio.Open()
	if noAudio {
		dev, err = nil, nil
	}
	if err != nil {
		report("audio", false, err.Error())
	} else if dev != nil {
		snd, err := dev.LoadWAV(toneWAV())
		if err != nil {
			report("audio", false, err.Error())
		} else {
			v := snd.Play()
			report("audio", v != nil, "440 Hz tone playing for one second")
		}
	}

	format := surface.PreferredFormat(adapter)
	w, h := win.Size()
	surface.Configure(device, w, h, format)
	module, err := device.CreateShaderModuleWGSL("devcheck", shader)
	if err != nil {
		return err
	}
	defer module.Release()
	pipeline, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: module, VertexEntry: "vs", FragmentEntry: "fs", Format: format,
	})
	if err != nil {
		return err
	}
	defer pipeline.Release()

	rendered := 0
	start := time.Now()
	win.EachFrame(func() bool {
		if nw, nh := win.Size(); nw != w || nh != h {
			w, h = nw, nh
			surface.Configure(device, w, h, format)
		}
		tex, ok := surface.CurrentTexture()
		if !ok {
			surface.Configure(device, w, h, format)
			return true
		}
		view := tex.CreateView()
		enc := device.CreateCommandEncoder()
		pass := enc.BeginRenderPass(view, gpu.Color{R: 0.05, G: 0.05, B: 0.08, A: 1})
		pass.SetPipeline(pipeline)
		pass.Draw(3, 1, 0, 0)
		pass.End()
		queue.Submit(enc.Finish())
		surface.Present()
		view.Release()
		tex.Release()
		rendered++
		if rendered == 60 {
			report("render", gpu.ErrorCount() == 0, fmt.Sprintf("60 frames at %dx%d in %v, %d device errors", w, h, time.Since(start).Round(time.Millisecond), gpu.ErrorCount()))
		}
		return maxFrames == 0 || rendered < maxFrames
	})
	fmt.Printf("devcheck: rendered %d frames\n", rendered)
	return nil
}
