package audio

// Windows playback via winmm waveOut — in every Windows since 3.1, loaded
// lazily so machines without audio just get the null fallback. Double
// buffering: write blocks polling the previous header's DONE flag, which
// paces the mixer.

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	winmm            = syscall.NewLazyDLL("winmm.dll")
	waveOutOpen      = winmm.NewProc("waveOutOpen")
	waveOutPrepare   = winmm.NewProc("waveOutPrepareHeader")
	waveOutWrite     = winmm.NewProc("waveOutWrite")
	waveOutUnprepare = winmm.NewProc("waveOutUnprepareHeader")
	waveOutResetProc = winmm.NewProc("waveOutReset")
	waveOutCloseProc = winmm.NewProc("waveOutClose")
)

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
	h       uintptr
	hdrs    [2]waveHdr
	bufs    [2][]int16
	cur     int
	stopped atomic.Bool // set by interrupt: writes fail from here on
}

// errInterrupted ends a write the device was closed under.
var errInterrupted = fmt.Errorf("audio: device closing")

// unreleased keeps backends whose shutdown failed reachable: the device
// may still hold their headers and sample buffers, which are Go memory it
// must not find collected.
var (
	unreleasedMu sync.Mutex
	unreleased   []*winmmBackend
)

func openBackend() (backend, error) {
	if err := waveOutOpen.Find(); err != nil {
		return nil, errUnsupported
	}
	fx := waveFormatEx{
		formatTag:      1, // WAVE_FORMAT_PCM
		channels:       2,
		samplesPerSec:  SampleRate,
		bitsPerSample:  16,
		blockAlign:     4,
		avgBytesPerSec: SampleRate * 4,
	}
	b := &winmmBackend{}
	const waveMapper = ^uintptr(0) // WAVE_MAPPER (-1)
	r, _, _ := waveOutOpen.Call(uintptr(unsafe.Pointer(&b.h)), waveMapper,
		uintptr(unsafe.Pointer(&fx)), 0, 0, 0)
	if r != 0 {
		return nil, fmt.Errorf("audio: waveOutOpen: %d", r)
	}
	for i := range b.bufs {
		b.bufs[i] = make([]int16, chunkFrames*2)
		b.hdrs[i].data = uintptr(unsafe.Pointer(&b.bufs[i][0]))
		b.hdrs[i].bufferLength = uint32(len(b.bufs[i]) * 2)
		if r, _, _ := waveOutPrepare.Call(b.h, uintptr(unsafe.Pointer(&b.hdrs[i])), unsafe.Sizeof(waveHdr{})); r != 0 {
			waveOutCloseProc.Call(b.h) // nothing is queued yet
			return nil, fmt.Errorf("audio: waveOutPrepareHeader: %d", r)
		}
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
	if r, _, _ := waveOutWrite.Call(b.h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(waveHdr{})); r != 0 {
		return fmt.Errorf("audio: waveOutWrite: %d", r)
	}
	b.cur = (b.cur + 1) % len(b.bufs)
	return nil
}

func (b *winmmBackend) interrupt() { b.stopped.Store(true) }

// close releases the device. A shutdown that fails cannot be retried by
// the caller (Device.Close reports nothing), so it is reported on stderr
// and the backend is kept alive for whatever the device still holds.
func (b *winmmBackend) close() {
	if err := b.shutdown(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		unreleasedMu.Lock()
		unreleased = append(unreleased, b)
		unreleasedMu.Unlock()
	}
}

// shutdown returns the queued buffers, unprepares the headers and closes
// the device, in that order: the mixer stops with its last chunks still
// queued, and a header the device is playing can be neither unprepared
// nor closed over. It reports the first call that failed.
func (b *winmmBackend) shutdown() error {
	b.stopped.Store(true)
	var first error
	check := func(call string, r uintptr) {
		if r != 0 && first == nil {
			first = fmt.Errorf("audio: %s: %d", call, r)
		}
	}
	// Reset stops playback and hands every queued buffer back, marked done.
	r, _, _ := waveOutResetProc.Call(b.h)
	check("waveOutReset", r)
	for i := range b.hdrs {
		// The hand-back can trail the reset; give it a moment before
		// taking "still playing" for an answer.
		deadline := time.Now().Add(time.Second)
		for {
			r, _, _ = waveOutUnprepare.Call(b.h, uintptr(unsafe.Pointer(&b.hdrs[i])), unsafe.Sizeof(waveHdr{}))
			if r != waverrStillPlaying || time.Now().After(deadline) {
				break
			}
			time.Sleep(time.Millisecond)
		}
		check("waveOutUnprepareHeader", r)
	}
	r, _, _ = waveOutCloseProc.Call(b.h)
	check("waveOutClose", r)
	return first
}
