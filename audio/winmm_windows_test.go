package audio

import (
	"testing"
	"time"
)

// openWinmm opens the real waveOut device, or skips on a machine that has
// none. These tests write silence only.
func openWinmm(t *testing.T) *winmmBackend {
	t.Helper()
	be, err := openBackend()
	if err != nil {
		t.Skipf("no waveOut device: %v", err)
	}
	return be.(*winmmBackend)
}

// A device closed with chunks still queued, which is how the mixer always
// leaves it, gives its buffers back before the headers are unprepared:
// every call of the shutdown succeeds.
func TestWinmmShutdownWithQueuedBuffers(t *testing.T) {
	b := openWinmm(t)
	silence := make([]int16, chunkFrames*2)
	for range 2 { // both buffers queued, neither played out
		if err := b.write(silence); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := b.shutdown(); err != nil {
		t.Fatalf("shutdown with buffers queued: %v", err)
	}
	if err := b.write(silence); err == nil {
		t.Error("write succeeded on a device that was shut down")
	}
}

// Open then close at once, and again: nothing lingers that stops the next
// open.
func TestWinmmOpenCloseCycles(t *testing.T) {
	silence := make([]int16, chunkFrames*2)
	for i := range 3 {
		b := openWinmm(t)
		for range i { // 0, 1 and 2 chunks queued at shutdown
			if err := b.write(silence); err != nil {
				t.Fatalf("cycle %d: write: %v", i, err)
			}
		}
		if err := b.shutdown(); err != nil {
			t.Fatalf("cycle %d: shutdown: %v", i, err)
		}
	}
}

// The whole path: a Device over the real backend plays a voice and closes
// while it sounds.
func TestWinmmDeviceCloseDuringPlayback(t *testing.T) {
	d := &Device{vol: 1, be: openWinmm(t), stop: make(chan struct{}), done: make(chan struct{})}
	go d.mixLoop()
	snd := &Sound{dev: d, samples: make([]float32, SampleRate*2)} // a second of silence
	v := snd.Loop()
	started := func() bool { d.mu.Lock(); defer d.mu.Unlock(); return v.pos > 0 }
	for deadline := time.Now().Add(5 * time.Second); !started(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the mixer never wrote a chunk to the device")
		}
	}
	d.Close()
	unreleasedMu.Lock()
	n := len(unreleased)
	unreleasedMu.Unlock()
	if n != 0 {
		t.Errorf("%d backends failed to shut down", n)
	}
}
