//go:build !android

package audio

// ALSA playback via libasound, loaded with ffi (dlopen, no cgo) — the same
// pattern as the wgpu binding. Five functions cover blocking playback:
// open, set_params, wait, writei, recover; waiting for room and writei's
// blocking pace the mixer.

import (
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/sushvibez/gfx/ffi"
)

type alsaBackend struct {
	pcm     uintptr
	stopped atomic.Bool // set by interrupt: writes fail from here on
}

// alsaWaitMs is how long one wait for room in the device lasts. write
// looks at stopped between waits, so this is how soon a closing mixer gets
// out of a device that has stopped taking audio.
const alsaWaitMs = 100

var alsa struct {
	open      func(pcm *uintptr, name string, stream int32, mode int32) int32
	setParams func(pcm uintptr, format, access int32, channels, rate uint32, softResample int32, latencyUs uint32) int32
	wait      func(pcm uintptr, timeoutMs int32) int32
	writei    func(pcm uintptr, buf uintptr, frames uintptr) int
	recover   func(pcm uintptr, err int32, silent int32) int32
	close     func(pcm uintptr) int32
}

const (
	sndPcmStreamPlayback     = 0
	sndPcmFormatS16LE        = 2
	sndPcmAccessRWInterleave = 3
)

func openBackend() (backend, error) {
	lib, err := ffi.Open("libasound.so.2", "libasound.so")
	if err != nil {
		return nil, err
	}
	lib.Bind(&alsa.open, "snd_pcm_open")
	lib.Bind(&alsa.setParams, "snd_pcm_set_params")
	lib.Bind(&alsa.wait, "snd_pcm_wait")
	lib.Bind(&alsa.writei, "snd_pcm_writei")
	lib.Bind(&alsa.recover, "snd_pcm_recover")
	lib.Bind(&alsa.close, "snd_pcm_close")

	var pcm uintptr
	if rc := alsa.open(&pcm, "default", sndPcmStreamPlayback, 0); rc < 0 {
		return nil, fmt.Errorf("audio: snd_pcm_open: %d", rc)
	}
	// 2ch S16 at the mixer rate, ~50ms device latency, ALSA soft-resample on
	// (covers hardware that can't do 48k natively).
	if rc := alsa.setParams(pcm, sndPcmFormatS16LE, sndPcmAccessRWInterleave,
		2, SampleRate, 1, 50_000); rc < 0 {
		alsa.close(pcm)
		return nil, fmt.Errorf("audio: snd_pcm_set_params: %d", rc)
	}
	return &alsaBackend{pcm: pcm}, nil
}

func (a *alsaBackend) write(buf []int16) error {
	frames := uintptr(len(buf) / 2)
	for frames > 0 {
		// writei blocks for as long as the device has no room, which is for
		// ever on one that has stopped (a stalled sound server, a suspended
		// card). Waiting for room in slices first gives interrupt a point
		// to be noticed at. A wait that reports an error (underrun,
		// suspend) falls through: writei reports the same error and the
		// recovery below deals with it.
		if a.stopped.Load() {
			return errInterrupted
		}
		if alsa.wait(a.pcm, alsaWaitMs) == 0 {
			continue // no room yet
		}
		n := alsa.writei(a.pcm, uintptr(unsafe.Pointer(&buf[0])), frames)
		if n < 0 {
			// Underrun (or suspend): recover once, then retry the chunk.
			if rc := alsa.recover(a.pcm, int32(n), 1); rc < 0 {
				return fmt.Errorf("audio: writei failed: %d", n)
			}
			continue
		}
		frames -= uintptr(n)
		if frames > 0 {
			buf = buf[n*2:]
		}
	}
	return nil
}

func (a *alsaBackend) interrupt() { a.stopped.Store(true) }

func (a *alsaBackend) close() {
	if a.pcm != 0 {
		alsa.close(a.pcm)
		a.pcm = 0
	}
}
