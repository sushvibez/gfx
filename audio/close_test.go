package audio

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stalledBackend is a device stuck inside a write that nothing can
// interrupt (it has no interrupt method): a driver that never returns.
// release is the test letting it return after all.
type stalledBackend struct {
	in      sync.Once
	entered chan struct{} // closed once the mixer is inside write
	release chan struct{}
	writing atomic.Bool
	closes  atomic.Int32
	// closedInWrite records a close that arrived while write was running,
	// which is the one thing Close must never let happen.
	closedInWrite atomic.Bool
	closed        chan struct{}
}

func newStalledBackend() *stalledBackend {
	return &stalledBackend{entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
}

func (b *stalledBackend) write([]int16) error {
	b.writing.Store(true)
	b.in.Do(func() { close(b.entered) })
	<-b.release
	b.writing.Store(false)
	return errUnsupported
}

func (b *stalledBackend) close() {
	if b.writing.Load() {
		b.closedInWrite.Store(true)
	}
	if b.closes.Add(1) == 1 {
		close(b.closed)
	}
}

// Close on a backend stalled inside a write it cannot interrupt returns
// within its bound instead of waiting for ever, on every platform; the
// backend is not closed under the write, and is closed once the write
// lets go.
func TestCloseReturnsFromAStalledBackend(t *testing.T) {
	defer func(d time.Duration) { mixerExitWait = d }(mixerExitWait)
	mixerExitWait = 50 * time.Millisecond

	be := newStalledBackend()
	d := &Device{vol: 1, be: be, stop: make(chan struct{}), done: make(chan struct{})}
	go d.mixLoop()
	<-be.entered
	start := time.Now()
	returned := make(chan struct{})
	go func() {
		d.Close()
		d.Close() // repeatable while the device is still stalled
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		close(be.release)
		t.Fatal("Close waited for a stalled backend for ever")
	}
	if took := time.Since(start); took < mixerExitWait {
		t.Errorf("Close returned after %v, before the %v it gives the mixer", took, mixerExitWait)
	}
	if n := be.closes.Load(); n != 0 {
		t.Fatalf("the backend was closed %d times while its write was still running", n)
	}
	// A sound played on the closed device is finished at once and is not
	// queued for a mixer that will never run.
	snd := &Sound{dev: d, samples: make([]float32, 64)}
	if v := snd.Play(); v.Playing() {
		t.Error("a voice started after Close is playing")
	}
	d.mu.Lock()
	queued := len(d.voices)
	d.mu.Unlock()
	if queued != 0 {
		t.Errorf("%d voices queued on a closed device", queued)
	}

	// The write returns at last: the backend is closed then, once.
	close(be.release)
	select {
	case <-be.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the backend was never closed after its write returned")
	}
	<-d.done
	d.Close()
	if n := be.closes.Load(); n != 1 || be.closedInWrite.Load() {
		t.Errorf("backend closed %d times (inside a write: %v), want once and not inside", n, be.closedInWrite.Load())
	}
}

// With a healthy backend Close does not wait out its bound.
func TestCloseDoesNotWaitWhenTheMixerLeaves(t *testing.T) {
	d := &Device{vol: 1, be: newNullBackend(), stop: make(chan struct{}), done: make(chan struct{})}
	go d.mixLoop()
	start := time.Now()
	d.Close()
	if took := time.Since(start); took > mixerExitWait/2 {
		t.Errorf("Close took %v on a healthy device", took)
	}
}
