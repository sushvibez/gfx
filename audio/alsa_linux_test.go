//go:build !android

package audio

import (
	"sync/atomic"
	"testing"
	"time"
)

// fakeALSA replaces the bound libasound functions with a device in
// memory, so the write loop runs without a sound card: room says whether
// snd_pcm_wait finds space, and the scripted errors are returned by the
// next writei calls.
type fakeALSA struct {
	room     atomic.Bool
	waits    atomic.Int32
	written  int
	writeErr []int // results of the next writei calls (negative errno), then success
	recovers int
	perWrite int // frames one writei takes; 0 = all it is given
}

func (f *fakeALSA) install(t *testing.T) {
	old := alsa
	t.Cleanup(func() { alsa = old })
	alsa.wait = func(pcm uintptr, timeoutMs int32) int32 {
		f.waits.Add(1)
		if f.room.Load() {
			return 1
		}
		time.Sleep(time.Millisecond) // the timeout, shortened
		return 0
	}
	alsa.writei = func(pcm uintptr, buf uintptr, frames uintptr) int {
		if len(f.writeErr) > 0 {
			e := f.writeErr[0]
			f.writeErr = f.writeErr[1:]
			return e
		}
		n := int(frames)
		if f.perWrite > 0 {
			n = min(n, f.perWrite)
		}
		f.written += n
		return n
	}
	alsa.recover = func(pcm uintptr, err int32, silent int32) int32 {
		f.recovers++
		if err == -19 { // ENODEV: the card is gone
			return err
		}
		return 0
	}
	alsa.close = func(pcm uintptr) int32 { return 0 }
}

// A device with no room (stalled server, suspended card) holds write only
// until the backend is interrupted, and every later write fails at once.
func TestALSAInterruptEndsAStalledWrite(t *testing.T) {
	f := &fakeALSA{}
	f.install(t)
	a := &alsaBackend{pcm: 1}
	done := make(chan error, 1)
	go func() { done <- a.write(make([]int16, chunkFrames*2)) }()
	for f.waits.Load() < 3 { // it is waiting, in slices
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("write returned %v from a device with no room", err)
	default:
	}
	a.interrupt()
	select {
	case err := <-done:
		if err != errInterrupted {
			t.Errorf("interrupted write: %v, want errInterrupted", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interrupt did not end the stalled write")
	}
	if f.written != 0 {
		t.Errorf("%d frames written to a device with no room", f.written)
	}
	if err := a.write(make([]int16, chunkFrames*2)); err != errInterrupted {
		t.Errorf("write after interrupt: %v, want errInterrupted", err)
	}
}

// The whole Device over the stalled ALSA device: Close returns promptly,
// through interrupt and not through the mixer-exit bound.
func TestALSADeviceCloseOnAStalledDevice(t *testing.T) {
	f := &fakeALSA{}
	f.install(t)
	d := &Device{vol: 1, be: &alsaBackend{pcm: 1}, stop: make(chan struct{}), done: make(chan struct{})}
	go d.mixLoop()
	for f.waits.Load() < 3 {
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	d.Close()
	if took := time.Since(start); took > mixerExitWait/2 {
		t.Errorf("Close took %v on a stalled device", took)
	}
	select {
	case <-d.done:
	default:
		t.Error("Close returned with the mixer still inside the backend")
	}
}

// With room, a chunk goes out whole across short writes, an underrun is
// recovered and retried, and a device that is gone is an error.
func TestALSAWritePaths(t *testing.T) {
	f := &fakeALSA{perWrite: 300, writeErr: []int{-32}} // EPIPE first
	f.room.Store(true)
	f.install(t)
	a := &alsaBackend{pcm: 1}
	if err := a.write(make([]int16, chunkFrames*2)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if f.written != chunkFrames || f.recovers != 1 {
		t.Errorf("%d frames written with %d recoveries, want %d and 1", f.written, f.recovers, chunkFrames)
	}
	f.writeErr = []int{-19}
	if err := a.write(make([]int16, chunkFrames*2)); err == nil || err == errInterrupted {
		t.Errorf("write to a vanished device: %v, want the device's error", err)
	}
}
