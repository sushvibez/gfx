package audio

// Windows playback via winmm waveOut — in every Windows since 3.1, loaded
// lazily so machines without audio just get the null fallback. Double
// buffering: write blocks polling the previous header's DONE flag, which
// paces the mixer.

import (
	"fmt"
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

const whdrDone = 0x0001

type winmmBackend struct {
	h    uintptr
	hdrs [2]waveHdr
	bufs [2][]int16
	cur  int
}

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
		b.hdrs[i].flags = whdrDone // free initially
		waveOutPrepare.Call(b.h, uintptr(unsafe.Pointer(&b.hdrs[i])), unsafe.Sizeof(waveHdr{}))
	}
	return b, nil
}

func (b *winmmBackend) write(buf []int16) error {
	hdr := &b.hdrs[b.cur]
	// Wait for this buffer's previous submission to finish.
	for hdr.flags&whdrDone == 0 {
		time.Sleep(time.Millisecond)
	}
	copy(b.bufs[b.cur], buf)
	hdr.flags &^= whdrDone
	if r, _, _ := waveOutWrite.Call(b.h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(waveHdr{})); r != 0 {
		return fmt.Errorf("audio: waveOutWrite: %d", r)
	}
	b.cur = (b.cur + 1) % len(b.bufs)
	return nil
}

func (b *winmmBackend) close() {
	for i := range b.hdrs {
		waveOutUnprepare.Call(b.h, uintptr(unsafe.Pointer(&b.hdrs[i])), unsafe.Sizeof(waveHdr{}))
	}
	waveOutCloseProc.Call(b.h)
}
