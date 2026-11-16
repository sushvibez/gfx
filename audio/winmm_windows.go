package audio

// Windows playback via winmm waveOut — in every Windows since 3.1, loaded
// lazily so machines without audio just get the null fallback. Double
// buffering: write blocks polling the previous header's DONE flag, which
// paces the mixer. The backend itself is in waveout.go; this file is the
// six procedures it calls.

import (
	"syscall"
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

// winmmAPI is the real device.
var winmmAPI = waveOutAPI{
	open: func(h *uintptr, fx *waveFormatEx) uintptr {
		const waveMapper = ^uintptr(0) // WAVE_MAPPER (-1)
		r, _, _ := waveOutOpen.Call(uintptr(unsafe.Pointer(h)), waveMapper,
			uintptr(unsafe.Pointer(fx)), 0, 0, 0)
		return r
	},
	prepare: func(h uintptr, hdr *waveHdr) uintptr {
		r, _, _ := waveOutPrepare.Call(h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(waveHdr{}))
		return r
	},
	write: func(h uintptr, hdr *waveHdr) uintptr {
		r, _, _ := waveOutWrite.Call(h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(waveHdr{}))
		return r
	},
	unprepare: func(h uintptr, hdr *waveHdr) uintptr {
		r, _, _ := waveOutUnprepare.Call(h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(waveHdr{}))
		return r
	},
	reset: func(h uintptr) uintptr {
		r, _, _ := waveOutResetProc.Call(h)
		return r
	},
	close: func(h uintptr) uintptr {
		r, _, _ := waveOutCloseProc.Call(h)
		return r
	},
}

func openBackend() (backend, error) {
	if err := waveOutOpen.Find(); err != nil {
		return nil, errUnsupported
	}
	// Devices an earlier Close could not shut down may have let go since.
	retryUnreleased()
	b, err := openWaveOut(&winmmAPI)
	if err != nil {
		return nil, err // not a nil *winmmBackend in a backend
	}
	return b, nil
}
