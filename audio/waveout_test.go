package audio

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeWaveOut is a waveOut device in memory that fails where a test asks it
// to. It keeps what a real device keeps (the open handle, which headers are
// prepared, which are queued), so a test can ask afterwards whether
// anything was left behind. No real device, so these run on every platform.
type fakeWaveOut struct {
	mu       sync.Mutex
	fail     map[string][]uintptr // per call: the results of its next uses (0 passes it through)
	calls    []string
	open     map[uintptr]bool
	prepared map[*waveHdr]bool
	queued   map[*waveHdr]bool
	nextH    uintptr
	// drainAfter, when positive, plays the queued buffers out after that
	// many refused unprepares: a device whose reset failed but which is
	// still playing.
	drainAfter int
	refused    int
}

const (
	mmErrInvalHandle = 5
	whdrPrepared     = 0x0002
	whdrInQueue      = 0x0010
)

func newFakeWaveOut() *fakeWaveOut {
	return &fakeWaveOut{
		fail:     map[string][]uintptr{},
		open:     map[uintptr]bool{},
		prepared: map[*waveHdr]bool{},
		queued:   map[*waveHdr]bool{},
		nextH:    100,
	}
}

// forced logs the call and pops its injected result, if one is waiting.
func (f *fakeWaveOut) forced(call string) uintptr {
	f.calls = append(f.calls, call)
	if q := f.fail[call]; len(q) > 0 {
		f.fail[call] = q[1:]
		return q[0]
	}
	return 0
}

// finish plays every queued buffer out, as the device's own thread would.
func (f *fakeWaveOut) finish() {
	for hdr := range f.queued {
		delete(f.queued, hdr)
		for {
			old := atomic.LoadUint32(&hdr.flags)
			if atomic.CompareAndSwapUint32(&hdr.flags, old, old&^whdrInQueue|whdrDone) {
				break
			}
		}
	}
}

func (f *fakeWaveOut) api() *waveOutAPI {
	return &waveOutAPI{
		open: func(h *uintptr, fx *waveFormatEx) uintptr {
			f.mu.Lock()
			defer f.mu.Unlock()
			if r := f.forced("open"); r != 0 {
				return r
			}
			f.nextH++
			*h = f.nextH
			f.open[*h] = true
			return 0
		},
		prepare: func(h uintptr, hdr *waveHdr) uintptr {
			f.mu.Lock()
			defer f.mu.Unlock()
			if r := f.forced("prepare"); r != 0 {
				return r
			}
			if !f.open[h] {
				return mmErrInvalHandle
			}
			f.prepared[hdr] = true
			hdr.flags = whdrPrepared // preparing clears DONE, as winmm does
			return 0
		},
		write: func(h uintptr, hdr *waveHdr) uintptr {
			f.mu.Lock()
			defer f.mu.Unlock()
			if r := f.forced("write"); r != 0 {
				return r
			}
			if !f.open[h] || !f.prepared[hdr] {
				return mmErrInvalHandle
			}
			f.queued[hdr] = true
			atomic.StoreUint32(&hdr.flags, whdrPrepared|whdrInQueue)
			return 0
		},
		unprepare: func(h uintptr, hdr *waveHdr) uintptr {
			f.mu.Lock()
			defer f.mu.Unlock()
			if r := f.forced("unprepare"); r != 0 {
				return r
			}
			if !f.open[h] {
				return mmErrInvalHandle
			}
			if f.queued[hdr] {
				if f.refused++; f.drainAfter > 0 && f.refused >= f.drainAfter {
					f.finish()
				}
				return waverrStillPlaying
			}
			delete(f.prepared, hdr)
			return 0
		},
		reset: func(h uintptr) uintptr {
			f.mu.Lock()
			defer f.mu.Unlock()
			if r := f.forced("reset"); r != 0 {
				return r
			}
			if !f.open[h] {
				return mmErrInvalHandle
			}
			f.finish()
			return 0
		},
		close: func(h uintptr) uintptr {
			f.mu.Lock()
			defer f.mu.Unlock()
			if r := f.forced("close"); r != 0 {
				return r
			}
			if !f.open[h] {
				return mmErrInvalHandle
			}
			if len(f.queued) > 0 {
				return waverrStillPlaying
			}
			delete(f.open, h)
			return 0
		},
	}
}

// held describes what the device still holds, "" when nothing.
func (f *fakeWaveOut) held() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var parts []string
	if n := len(f.open); n > 0 {
		parts = append(parts, "an open device")
	}
	if n := len(f.prepared); n > 0 {
		parts = append(parts, "prepared headers")
	}
	if n := len(f.queued); n > 0 {
		parts = append(parts, "queued buffers")
	}
	return strings.Join(parts, ", ")
}

func (f *fakeWaveOut) count(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

// kept reports how many backends are parked as unreleased, and forgets
// them so tests do not see each other's.
func takeUnreleased() []*winmmBackend {
	unreleasedMu.Lock()
	defer unreleasedMu.Unlock()
	u := unreleased
	unreleased = nil
	return u
}

func openFake(t *testing.T, f *fakeWaveOut) *winmmBackend {
	t.Helper()
	b, err := openWaveOut(f.api())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	b.unprepareWait = 20 * time.Millisecond
	return b
}

var silence = make([]int16, chunkFrames*2)

// The plain path through the fake, so the failure tests below are known to
// fail for the reason they inject.
func TestWaveOutOpenWriteShutdown(t *testing.T) {
	f := newFakeWaveOut()
	b := openFake(t, f)
	for range 2 {
		if err := b.write(silence); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := b.shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if h := f.held(); h != "" {
		t.Errorf("after shutdown the device still holds %s", h)
	}
	if err := b.write(silence); err != errInterrupted {
		t.Errorf("write after shutdown: %v, want errInterrupted", err)
	}
	// A second shutdown has nothing to give back and asks the device
	// nothing.
	calls := len(f.calls)
	if err := b.shutdown(); err != nil || len(f.calls) != calls {
		t.Errorf("second shutdown: err %v, %d more device calls", err, len(f.calls)-calls)
	}
}

// Opening fails at each step; whatever the steps before it acquired is
// given back, and nothing is parked.
func TestWaveOutOpenFailuresReleaseWhatWasAcquired(t *testing.T) {
	defer takeUnreleased()
	for _, c := range []struct {
		name           string
		call           string
		results        []uintptr
		wantUnprepares int
		wantCloses     int
	}{
		{"open", "open", []uintptr{2}, 0, 0},
		{"first prepare", "prepare", []uintptr{7}, 0, 1},
		{"second prepare", "prepare", []uintptr{0, 7}, 1, 1},
	} {
		f := newFakeWaveOut()
		f.fail[c.call] = c.results
		b, err := openWaveOut(f.api())
		if err == nil || b != nil {
			t.Errorf("%s fails: openWaveOut returned %v, %v", c.name, b, err)
			continue
		}
		if h := f.held(); h != "" {
			t.Errorf("%s fails: the device still holds %s", c.name, h)
		}
		if got := f.count("unprepare"); got != c.wantUnprepares {
			t.Errorf("%s fails: %d unprepares, want %d", c.name, got, c.wantUnprepares)
		}
		if got := f.count("close"); got != c.wantCloses {
			t.Errorf("%s fails: %d closes, want %d", c.name, got, c.wantCloses)
		}
		if u := takeUnreleased(); len(u) != 0 {
			t.Errorf("%s fails: %d backends parked though nothing is held", c.name, len(u))
		}
	}
}

// A prepare fails and then the clean-up fails too: the error names both,
// and the half-open backend is parked rather than dropped, because the
// device still has its handle and header.
func TestWaveOutOpenFailureWithFailedCleanup(t *testing.T) {
	defer takeUnreleased()
	f := newFakeWaveOut()
	f.fail["prepare"] = []uintptr{0, 7}
	f.fail["close"] = []uintptr{mmErrInvalHandle}
	_, err := openWaveOut(f.api())
	if err == nil || !strings.Contains(err.Error(), "waveOutPrepareHeader: 7") || !strings.Contains(err.Error(), "waveOutClose: 5") {
		t.Fatalf("error %q, want it to name the prepare and the close that failed", err)
	}
	if f.count("unprepare") != 1 {
		t.Errorf("%d unprepares, want the first header's", f.count("unprepare"))
	}
	u := takeUnreleased()
	if len(u) != 1 {
		t.Fatalf("%d backends parked, want the one whose close failed", len(u))
	}
	// The parked backend can still be shut down once the device lets it.
	if err := u[0].shutdown(); err != nil {
		t.Errorf("retried shutdown: %v", err)
	}
	if h := f.held(); h != "" {
		t.Errorf("after the retry the device still holds %s", h)
	}
}

// A write the device refuses leaves the buffer free, reports the failure,
// and the shutdown after it is clean.
func TestWaveOutWriteFailure(t *testing.T) {
	f := newFakeWaveOut()
	b := openFake(t, f)
	f.fail["write"] = []uintptr{0, 11}
	if err := b.write(silence); err != nil {
		t.Fatalf("first write: %v", err)
	}
	err := b.write(silence)
	if err == nil || !strings.Contains(err.Error(), "waveOutWrite: 11") {
		t.Fatalf("second write: %v, want the device's refusal", err)
	}
	// The refused buffer was not queued, so it is still free: the next
	// write must not wait for it to finish playing.
	done := make(chan error, 1)
	go func() { done <- b.write(silence) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("write after a refused one: %v", err)
		}
	case <-time.After(2 * time.Second):
		b.interrupt()
		t.Fatal("write after a refused one waited for a buffer that was never queued")
	}
	if err := b.shutdown(); err != nil {
		t.Errorf("shutdown: %v", err)
	}
	if h := f.held(); h != "" {
		t.Errorf("the device still holds %s", h)
	}
}

// The whole Device over a backend whose write fails: the mixer stops,
// Close gives everything back.
func TestWaveOutDeviceCloseAfterWriteFailure(t *testing.T) {
	defer takeUnreleased()
	f := newFakeWaveOut()
	f.fail["write"] = []uintptr{0, 11}
	d := &Device{vol: 1, be: openFake(t, f), stop: make(chan struct{}), done: make(chan struct{})}
	go d.mixLoop()
	select {
	case <-d.done:
	case <-time.After(5 * time.Second):
		t.Fatal("mixer kept running after a failed write")
	}
	d.Close()
	if h := f.held(); h != "" {
		t.Errorf("the device still holds %s", h)
	}
	if u := takeUnreleased(); len(u) != 0 {
		t.Errorf("%d backends parked", len(u))
	}
}

// Reset fails with both buffers queued, but the device goes on playing:
// the unprepare retries see them finish, so everything is released and the
// reset is still reported.
func TestWaveOutResetFailsButBuffersDrain(t *testing.T) {
	f := newFakeWaveOut()
	b := openFake(t, f)
	b.unprepareWait = 5 * time.Second
	b.write(silence)
	b.write(silence)
	f.fail["reset"] = []uintptr{mmErrInvalHandle}
	f.drainAfter = 3
	err := b.shutdown()
	if err == nil || !strings.Contains(err.Error(), "waveOutReset: 5") {
		t.Errorf("shutdown: %v, want the failed reset reported", err)
	}
	if h := f.held(); h != "" {
		t.Errorf("the device still holds %s", h)
	}
	if b.holding() {
		t.Error("the backend thinks the device still holds something")
	}
}

// Reset fails and the buffers never come back: unprepare and close are
// refused, shutdown gives up after its wait, and close parks the backend.
// When the device lets go later, a retry releases it.
func TestWaveOutStuckBuffersAreParkedAndRetried(t *testing.T) {
	defer takeUnreleased()
	f := newFakeWaveOut()
	b := openFake(t, f)
	b.write(silence)
	b.write(silence)
	f.fail["reset"] = []uintptr{mmErrInvalHandle, mmErrInvalHandle, mmErrInvalHandle}
	start := time.Now()
	b.close()
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("close took %v with a 20 ms wait per header", took)
	}
	if h := f.held(); !strings.Contains(h, "open device") || !strings.Contains(h, "queued") {
		t.Fatalf("device holds %q, want it still open with buffers queued", h)
	}
	unreleasedMu.Lock()
	n := len(unreleased)
	unreleasedMu.Unlock()
	if n != 1 || !b.holding() {
		t.Fatalf("%d backends parked (holding %v), want this one", n, b.holding())
	}
	// A retry while it is still stuck keeps it parked and does not wait.
	start = time.Now()
	retryUnreleased()
	if took := time.Since(start); took > time.Second {
		t.Errorf("a retry waited %v", took)
	}
	unreleasedMu.Lock()
	n = len(unreleased)
	unreleasedMu.Unlock()
	if n != 1 {
		t.Fatalf("%d backends parked after a failed retry, want 1", n)
	}
	// The device finishes playing; the next retry releases everything.
	f.mu.Lock()
	f.finish()
	f.mu.Unlock()
	retryUnreleased()
	if h := f.held(); h != "" {
		t.Errorf("after the retry the device still holds %s", h)
	}
	if u := takeUnreleased(); len(u) != 0 || b.holding() {
		t.Errorf("%d backends still parked after a successful retry", len(u))
	}
}

// One header's unprepare fails outright: the other is still unprepared and
// the device still closed, which is what frees the first.
func TestWaveOutUnprepareFailure(t *testing.T) {
	defer takeUnreleased()
	f := newFakeWaveOut()
	b := openFake(t, f)
	b.write(silence)
	f.fail["unprepare"] = []uintptr{mmErrInvalHandle}
	err := b.shutdown()
	if err == nil || !strings.Contains(err.Error(), "waveOutUnprepareHeader: 5") {
		t.Errorf("shutdown: %v, want the failed unprepare reported", err)
	}
	if f.count("unprepare") != 2 || f.count("close") != 1 {
		t.Errorf("%d unprepares and %d closes, want 2 and 1", f.count("unprepare"), f.count("close"))
	}
	if b.holding() {
		t.Error("the device was closed, yet the backend counts as held")
	}
	// The device closed, so close() has nothing to park.
	f2 := newFakeWaveOut()
	b2 := openFake(t, f2)
	f2.fail["unprepare"] = []uintptr{mmErrInvalHandle}
	b2.close()
	if u := takeUnreleased(); len(u) != 0 {
		t.Errorf("%d backends parked after the device closed", len(u))
	}
}

// Close fails: the handle is still open, so the backend is parked, once,
// and a later shutdown closes it without unpreparing anything twice.
func TestWaveOutCloseFailure(t *testing.T) {
	defer takeUnreleased()
	f := newFakeWaveOut()
	b := openFake(t, f)
	f.fail["close"] = []uintptr{mmErrInvalHandle}
	b.close()
	u := takeUnreleased()
	if len(u) != 1 || u[0] != b || !b.holding() {
		t.Fatalf("%d backends parked, want the one whose close failed", len(u))
	}
	if f.count("unprepare") != 2 {
		t.Fatalf("%d unprepares, want 2", f.count("unprepare"))
	}
	if err := b.shutdown(); err != nil {
		t.Errorf("retried shutdown: %v", err)
	}
	if f.count("unprepare") != 2 {
		t.Errorf("the retry unprepared headers again (%d calls)", f.count("unprepare"))
	}
	if h := f.held(); h != "" || b.holding() {
		t.Errorf("after the retry the device still holds %q", h)
	}
}

// A write waiting for the device to finish a buffer returns when the
// backend is interrupted.
func TestWaveOutInterruptEndsAWaitingWrite(t *testing.T) {
	f := newFakeWaveOut()
	b := openFake(t, f)
	b.write(silence)
	b.write(silence) // both queued; the fake never plays them
	done := make(chan error, 1)
	go func() { done <- b.write(silence) }()
	select {
	case err := <-done:
		t.Fatalf("a write with no free buffer returned %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	b.interrupt()
	select {
	case err := <-done:
		if err != errInterrupted {
			t.Errorf("interrupted write: %v, want errInterrupted", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interrupt did not end the waiting write")
	}
	if err := b.shutdown(); err != nil {
		t.Errorf("shutdown: %v", err)
	}
	if h := f.held(); h != "" {
		t.Errorf("the device still holds %s", h)
	}
}
