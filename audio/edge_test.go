package audio

import (
	"sync"
	"testing"
	"time"
)

// A sound with no frames has nothing to play: its voice finishes on the
// first mix, looping or not, instead of the mixer reading sample zero of
// an empty buffer.
func TestEmptySoundFinishesAtOnce(t *testing.T) {
	d := &Device{vol: 1}
	snd, err := d.LoadWAV(makeWAV(nil, 2, SampleRate))
	if err != nil {
		t.Fatalf("LoadWAV of an empty data chunk: %v", err)
	}
	if snd.Frames() != 0 {
		t.Fatalf("frames = %d, want 0", snd.Frames())
	}
	for _, v := range []*Voice{snd.Loop(), snd.Play()} {
		mix := make([]float32, 8)
		v.mixInto(mix)
		if !v.stop {
			t.Errorf("voice (loop=%v) on an empty sound still playing", v.loop)
		}
		for i, s := range mix {
			if s != 0 {
				t.Errorf("mix[%d] = %v from an empty sound", i, s)
			}
		}
	}
}

// A stereo file cut in the middle of a frame keeps its whole frames; the
// half frame is dropped at load, and a buffer that reaches the mixer with
// one anyway is not read past its end.
func TestTruncatedStereoFrame(t *testing.T) {
	d := &Device{vol: 1}
	snd, err := d.LoadWAV(makeWAV([]float32{0.5, 0.5, 0.25, 0.25, 0.75}, 2, SampleRate))
	if err != nil {
		t.Fatalf("LoadWAV: %v", err)
	}
	if snd.Frames() != 2 || len(snd.samples) != 4 {
		t.Fatalf("frames = %d over %d samples, want 2 whole frames", snd.Frames(), len(snd.samples))
	}
	odd := &Sound{dev: d, samples: []float32{1, 1, 1}}
	for _, v := range []*Voice{odd.Loop(), odd.Play()} {
		mix := make([]float32, 8)
		v.mixInto(mix)
		if mix[0] != 1 || mix[1] != 1 {
			t.Errorf("first frame = %v,%v, want 1,1", mix[0], mix[1])
		}
		if !v.loop && (mix[2] != 0 || !v.stop) {
			t.Errorf("one-shot played past its whole frame: %v stop=%v", mix[:4], v.stop)
		}
	}
	// A single odd sample is no frame at all.
	lone := &Sound{dev: d, samples: []float32{1}}
	v := lone.Loop()
	v.mixInto(make([]float32, 8))
	if !v.stop {
		t.Error("looping voice on half a frame still playing")
	}
}

// A looping voice wraps to its first frame and keeps going.
func TestLoopWraps(t *testing.T) {
	d := &Device{vol: 1}
	snd := &Sound{dev: d, samples: []float32{1, -1, 2, -2, 3, -3}}
	v := snd.Loop()
	mix := make([]float32, 16)
	v.mixInto(mix)
	want := []float32{1, -1, 2, -2, 3, -3, 1, -1, 2, -2, 3, -3, 1, -1, 2, -2}
	for i := range want {
		if mix[i] != want[i] {
			t.Fatalf("mix = %v, want %v", mix, want)
		}
	}
	if v.stop || v.pos != 2 {
		t.Fatalf("after wrapping: stop=%v pos=%d, want playing at frame 2", v.stop, v.pos)
	}
}

// stepBackend lets a test run the mixer one chunk at a time.
type stepBackend struct {
	wrote chan struct{}
	next  chan struct{}
}

func (b *stepBackend) write([]int16) error {
	b.wrote <- struct{}{}
	<-b.next
	return nil
}
func (b *stepBackend) close() {}

// A finished voice leaves the mixer's list entirely: the slot it held in
// the backing array is cleared, so the sound's PCM is not kept alive by a
// pointer nothing reads.
func TestFinishedVoicesAreNotRetained(t *testing.T) {
	be := &stepBackend{wrote: make(chan struct{}), next: make(chan struct{})}
	d := &Device{vol: 1, be: be, stop: make(chan struct{}), done: make(chan struct{})}
	short := &Sound{dev: d, samples: make([]float32, 8)}
	long := &Sound{dev: d, samples: make([]float32, chunkFrames*8)}
	long.Play()
	short.Play()
	short.Play()
	go d.mixLoop()
	<-be.wrote // the first chunk is mixed: both short voices have finished
	d.mu.Lock()
	n := len(d.voices)
	tail := d.voices[n:cap(d.voices)]
	for i, v := range tail {
		if v != nil {
			t.Errorf("slot %d past the live voices still holds a finished voice", n+i)
		}
	}
	d.mu.Unlock()
	if n != 1 {
		t.Errorf("%d live voices, want 1", n)
	}
	close(d.stop)
	close(be.next)
	<-d.done
}

// stuckBackend is a device that stopped taking audio: write blocks until
// the backend is interrupted.
type stuckBackend struct {
	once, in sync.Once
	entered  chan struct{} // closed once the mixer is inside write
	release  chan struct{}
	closed   bool
}

func newStuckBackend() *stuckBackend {
	return &stuckBackend{entered: make(chan struct{}), release: make(chan struct{})}
}

func (b *stuckBackend) write([]int16) error {
	b.in.Do(func() { close(b.entered) })
	<-b.release
	return errUnsupported
}
func (b *stuckBackend) interrupt() { b.once.Do(func() { close(b.release) }) }
func (b *stuckBackend) close()     { b.closed = true }

// Close returns even when the mixer is blocked in a write the device will
// never finish, and a second Close is harmless.
func TestCloseWhileWriteIsBlocked(t *testing.T) {
	be := newStuckBackend()
	d := &Device{vol: 1, be: be, stop: make(chan struct{}), done: make(chan struct{})}
	go d.mixLoop()
	<-be.entered
	closed := make(chan struct{})
	go func() {
		d.Close()
		d.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		be.interrupt() // let the mixer goroutine go
		t.Fatal("Close did not return while the backend write was blocked")
	}
	if !be.closed {
		t.Error("backend not closed")
	}
}

// A backend that fails a write stops the mixer; Close still releases it.
func TestCloseAfterWriteFailure(t *testing.T) {
	be := newStuckBackend()
	be.interrupt() // every write fails at once
	d := &Device{vol: 1, be: be, stop: make(chan struct{}), done: make(chan struct{})}
	go d.mixLoop()
	select {
	case <-d.done:
	case <-time.After(5 * time.Second):
		t.Fatal("mixer kept running after a failed write")
	}
	d.Close()
	if !be.closed {
		t.Error("backend not closed")
	}
}
