//go:build js

package audio

// Browser playback through Web Audio. The mixer's blocking write model
// maps onto a queue of AudioBufferSourceNodes scheduled back to back on
// the context's clock: each chunk becomes one buffer started at the end
// of the previous one, and write sleeps (yielding to the browser event
// loop, which is how a wasm goroutine blocks) whenever more than
// leadChunks are queued ahead of the play head. That keeps latency near
// three chunks (~64 ms) without ever starving the output.
//
// Autoplay policy: a context created before a user gesture starts
// suspended and its clock does not advance. Until it runs, chunks are
// dropped at real-time pace (the null backend's behaviour), and the first
// pointer or key event on the page resumes it.

import (
	"fmt"
	"syscall/js"
	"time"
	"unsafe"
)

type webBackend struct {
	ctx     js.Value
	next    float64 // context time the next chunk starts at
	resumed bool
}

const leadChunks = 3

func openBackend() (backend, error) {
	ac := js.Global().Get("AudioContext")
	if !ac.Truthy() {
		ac = js.Global().Get("webkitAudioContext")
	}
	if !ac.Truthy() {
		return nil, fmt.Errorf("audio: no AudioContext in this browser")
	}
	opts := js.Global().Get("Object").New()
	opts.Set("sampleRate", SampleRate)
	ctx := ac.New(opts)
	if !ctx.Truthy() {
		return nil, fmt.Errorf("audio: AudioContext refused")
	}
	w := &webBackend{ctx: ctx}
	// Resume on the first user gesture, the only moment the browser lets
	// a page start sound.
	var resume js.Func
	resume = js.FuncOf(func(js.Value, []js.Value) any {
		if w.ctx.Get("state").String() == "suspended" {
			w.ctx.Call("resume")
		}
		return nil
	})
	doc := js.Global().Get("document")
	for _, ev := range []string{"pointerdown", "keydown", "touchstart"} {
		doc.Call("addEventListener", ev, resume)
	}
	return w, nil
}

func (w *webBackend) write(buf []int16) error {
	frames := len(buf) / 2
	dur := float64(frames) / SampleRate
	if w.ctx.Get("state").String() != "running" {
		// Suspended (or closed): keep real-time pace, play nothing.
		time.Sleep(time.Duration(dur * float64(time.Second)))
		w.next = 0
		return nil
	}
	now := w.ctx.Get("currentTime").Float()
	if w.next < now {
		w.next = now + 0.01 // (re)start a little ahead of the play head
	}
	ab := w.ctx.Call("createBuffer", 2, frames, SampleRate)
	left := make([]float32, frames)
	right := make([]float32, frames)
	for i := 0; i < frames; i++ {
		left[i] = float32(buf[i*2]) / 32768
		right[i] = float32(buf[i*2+1]) / 32768
	}
	ab.Call("copyToChannel", float32Array(left), 0)
	ab.Call("copyToChannel", float32Array(right), 1)
	src := w.ctx.Call("createBufferSource")
	src.Set("buffer", ab)
	src.Call("connect", w.ctx.Get("destination"))
	src.Call("start", w.next)
	w.next += dur
	// Pace: wait until fewer than leadChunks are queued ahead.
	if lead := w.next - now - float64(leadChunks)*dur; lead > 0 {
		time.Sleep(time.Duration(lead * float64(time.Second)))
	}
	return nil
}

// float32Array copies f into a JS Float32Array in one boundary crossing.
func float32Array(f []float32) js.Value {
	arr := js.Global().Get("Float32Array").New(len(f))
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&f[0])), len(f)*4)
	js.CopyBytesToJS(js.Global().Get("Uint8Array").New(arr.Get("buffer")), bytes)
	return arr
}

func (w *webBackend) close() {
	if w.ctx.Truthy() {
		w.ctx.Call("close")
	}
}
