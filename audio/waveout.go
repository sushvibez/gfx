package audio

// The waveOut backend apart from the six winmm calls it makes. Those go
// through waveOutAPI, which winmm_windows.go fills with the real
// procedures and the tests with a device that fails on request; this file
// has no build tag so those tests run on every platform, not only where
// winmm exists.

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// waveOutAPI is winmm's waveOut as the backend uses it. Every call returns
// an MMRESULT: 0 for success.
type waveOutAPI struct {
	open      func(h *uintptr, fx *waveFormatEx) uintptr
	prepare   func(h uintptr, hdr *waveHdr) uintptr
	write     func(h uintptr, hdr *waveHdr) uintptr
	unprepare func(h uintptr, hdr *waveHdr) uintptr
	reset     func(h uintptr) uintptr
	close     func(h uintptr) uintptr
}

type waveFormatEx struct {
	formatTag      uint16
	channels       uint16
	samplesPerSec  uint32
	avgBytesPerSec uint32
	blockAlign     uint16
	bitsPerSample  uint16
	cbSize         uint16
}

type waveHdr struct {
	data          uintptr
	bufferLength  uint32
	bytesRecorded uint32
	user          uintptr
	flags         uint32
	loops         uint32
	next          uintptr
	reserved      uintptr
}

const (
	whdrDone           = 0x0001
	waverrStillPlaying = 33 // unprepare of a header the device still holds
)

type winmmBackend struct {
	api     *waveOutAPI
	h       uintptr
	hdrs    [2]waveHdr
	bufs    [2][]int16
	cur     int
	stopped atomic.Bool // set by interrupt: writes fail from here on

	// What the device holds of ours, so every failure path gives back
	// exactly that and a repeated shutdown asks for nothing twice: the
	// open handle, and each header between its prepare and its unprepare.
	isOpen   bool
	prepared [2]bool

	// unprepareWait is how long shutdown waits for the device to hand a
	// header back before taking "still playing" for an answer.
	unprepareWait time.Duration
}

// errInterrupted ends a write the device was closed under.
var errInterrupted = errors.New("audio: device closing")

// unreleased keeps backends whose shutdown failed reachable: the device
// may still hold their headers and sample buffers, which are Go memory it
// must not find collected.
var (
	unreleasedMu sync.Mutex
	unreleased   []*winmmBackend
)

// park keeps b reachable for as long as the device holds part of it.
func (b *winmmBackend) park() {
	unreleasedMu.Lock()
	unreleased = append(unreleased, b)
	unreleasedMu.Unlock()
}

// retryUnreleased tries again to shut down the backends an earlier close
// had to park, without waiting: a device that was still playing then may
// have finished since. The ones that let go are dropped. Opening a device
// calls it, so a program that opens and closes audio repeatedly does not
// gather devices it could not close at the first attempt.
func retryUnreleased() {
	unreleasedMu.Lock()
	defer unreleasedMu.Unlock()
	kept := unreleased[:0]
	for _, b := range unreleased {
		b.unprepareWait = 0
		b.shutdown()
		if b.holding() {
			kept = append(kept, b)
		}
	}
	clear(unreleased[len(kept):])
	unreleased = kept
}

// openWaveOut opens the default device and prepares the two buffers. When
// a step fails, what the steps before it acquired is given back before the
// error is returned; if giving back fails too, the error says so and the
// backend is parked, because the device still holds its handle or headers.
func openWaveOut(api *waveOutAPI) (*winmmBackend, error) {
	fx := waveFormatEx{
		formatTag:      1, // WAVE_FORMAT_PCM
		channels:       2,
		samplesPerSec:  SampleRate,
		bitsPerSample:  16,
		blockAlign:     4,
		avgBytesPerSec: SampleRate * 4,
	}
	b := &winmmBackend{api: api, unprepareWait: time.Second}
	if r := api.open(&b.h, &fx); r != 0 {
		return nil, fmt.Errorf("audio: waveOutOpen: %d", r)
	}
	b.isOpen = true
	for i := range b.bufs {
		b.bufs[i] = make([]int16, chunkFrames*2)
		b.hdrs[i].data = uintptr(unsafe.Pointer(&b.bufs[i][0]))
		b.hdrs[i].bufferLength = uint32(len(b.bufs[i]) * 2)
		if r := api.prepare(b.h, &b.hdrs[i]); r != 0 {
			err := fmt.Errorf("audio: waveOutPrepareHeader: %d", r)
			// Nothing is queued yet, so this unprepares the headers
			// prepared so far and closes the device.
			if cerr := b.shutdown(); cerr != nil {
				err = errors.Join(err, cerr)
			}
			if b.holding() {
				b.park()
			}
			return nil, err
		}
		b.prepared[i] = true
		// Free initially. Marked after preparing, which clears the flag:
		// set before, the first write waited for a buffer that was never
		// submitted to finish, and nothing ever played.
		b.hdrs[i].flags |= whdrDone
	}
	return b, nil
}

func (b *winmmBackend) write(buf []int16) error {
	hdr := &b.hdrs[b.cur]
	// Wait for this buffer's previous submission to finish: the device
	// sets the flag from its own thread. A device that never does (it was
	// unplugged, or stalled) must not hold a closing mixer here.
	for atomic.LoadUint32(&hdr.flags)&whdrDone == 0 {
		if b.stopped.Load() {
			return errInterrupted
		}
		time.Sleep(time.Millisecond)
	}
	if b.stopped.Load() {
		return errInterrupted
	}
	copy(b.bufs[b.cur], buf)
	hdr.flags &^= whdrDone
	if r := b.api.write(b.h, hdr); r != 0 {
		// Refused, so not queued: the buffer is still free. Left marked
		// busy, the next write to it would wait for a playback that never
		// began.
		hdr.flags |= whdrDone
		return fmt.Errorf("audio: waveOutWrite: %d", r)
	}
	b.cur = (b.cur + 1) % len(b.bufs)
	return nil
}

func (b *winmmBackend) interrupt() { b.stopped.Store(true) }

// holding reports whether the device still has something of this backend:
// its handle is open (the headers and buffers go with the handle).
func (b *winmmBackend) holding() bool { return b.isOpen }

// close releases the device. A shutdown that fails cannot be retried by
// the caller (Device.Close reports nothing), so it is reported on stderr
// and, if the device still holds the backend, the backend is kept alive
// for it and tried again the next time a device is opened.
func (b *winmmBackend) close() {
	if err := b.shutdown(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if b.holding() {
		b.park()
	}
}

// shutdown returns the queued buffers, unprepares the headers and closes
// the device, in that order: the mixer stops with its last chunks still
// queued, and a header the device is playing can be neither unprepared
// nor closed over. Every step is attempted whatever the ones before it
// returned, and every failure is in the error:
//
//   - a failed reset leaves the buffers playing; the unprepares wait
//     unprepareWait each for them to finish on their own;
//   - a header that cannot be unprepared stays recorded as prepared, and
//     the close is still tried: a device that closes has let go of its
//     headers whatever unprepare said;
//   - a failed close leaves the handle open, and holding reports it.
//
// It is repeatable: a second call asks the device only for what the first
// could not get back, and nothing at all once the device is closed.
func (b *winmmBackend) shutdown() error {
	b.stopped.Store(true)
	if !b.isOpen {
		return nil
	}
	var err error
	fail := func(call string, r uintptr) {
		err = errors.Join(err, fmt.Errorf("audio: %s: %d", call, r))
	}
	// Reset stops playback and hands every queued buffer back, marked done.
	if r := b.api.reset(b.h); r != 0 {
		fail("waveOutReset", r)
	}
	for i := range b.hdrs {
		if !b.prepared[i] {
			continue
		}
		// The hand-back can trail the reset; give it a moment before
		// taking "still playing" for an answer.
		deadline := time.Now().Add(b.unprepareWait)
		var r uintptr
		for {
			r = b.api.unprepare(b.h, &b.hdrs[i])
			if r != waverrStillPlaying || !time.Now().Before(deadline) {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if r != 0 {
			fail("waveOutUnprepareHeader", r)
			continue
		}
		b.prepared[i] = false
	}
	if r := b.api.close(b.h); r != 0 {
		fail("waveOutClose", r)
		return err
	}
	b.isOpen = false
	b.prepared = [2]bool{}
	return err
}
