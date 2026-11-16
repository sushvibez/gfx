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
	"sync"
	"syscall/js"
	"time"
	"unsafe"
)

type webBackend struct {
	ctx     js.Value
	next    float64 // context time the next chunk starts at
	resumed bool

	// The pacing sleeps end early when the device closes: stop is closed
	// by interrupt, timer is the one timer every sleep reuses.
	stop     chan struct{}
	stopOnce sync.Once
	timer    *time.Timer

	// The gesture listener, kept so close can take it off the page.
	resume js.Func
}

// resumeEvents are the gestures a browser accepts as permission to start
// sound.
var resumeEvents = []string{"pointerdown", "keydown", "touchstart"}

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
	w := &webBackend{ctx: ctx, stop: make(chan struct{}), timer: time.NewTimer(0)}
	// Resume on the first user gesture, the only moment the browser lets
	// a page start sound.
	w.resume = js.FuncOf(func(js.Value, []js.Value) any {
		if w.ctx.Get("state").String() == "suspended" {
			w.ctx.Call("resume")
		}
		return nil
	})
	doc := js.Global().Get("document")
	for _, ev := range resumeEvents {
		doc.Call("addEventListener", ev, w.resume)
	}
	return w, nil
}

func (w *webBackend) write(buf []int16) error {
	frames := len(buf) / 2
	dur := float64(frames) / SampleRate
	if w.ctx.Get("state").String() != "running" {
		// Suspended (or closed): keep real-time pace, play nothing.
		w.next = 0
		return w.sleep(time.Duration(dur * float64(time.Second)))
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
		return w.sleep(time.Duration(lead * float64(time.Second)))
	}
	return nil
}

// sleep paces the mixer for d, or returns errInterrupted as soon as the
// device is closing. The lead it sleeps off grows without limit while a
// running context's clock stands still, so an uninterruptible sleep here
// could hold Close for as long as the page had been stalled.
func (w *webBackend) sleep(d time.Duration) error {
	w.timer.Reset(d)
	select {
	case <-w.stop:
		w.timer.Stop()
		return errInterrupted
	case <-w.timer.C:
		return nil
	}
}

func (w *webBackend) interrupt() { w.stopOnce.Do(func() { close(w.stop) }) }

// float32Array copies f into a JS Float32Array in one boundary crossing.
func float32Array(f []float32) js.Value {
	arr := js.Global().Get("Float32Array").New(len(f))
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&f[0])), len(f)*4)
	js.CopyBytesToJS(js.Global().Get("Uint8Array").New(arr.Get("buffer")), bytes)
	return arr
}

// close takes the gesture listener off the page and closes the context.
// Closing twice is harmless.
func (w *webBackend) close() {
	if !w.ctx.Truthy() {
		return
	}
	doc := js.Global().Get("document")
	for _, ev := range resumeEvents {
		doc.Call("removeEventListener", ev, w.resume)
	}
	w.resume.Release()
	w.ctx.Call("close")
	w.ctx = js.Undefined()
}
