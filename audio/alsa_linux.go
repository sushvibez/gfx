//go:build !android

package audio

// ALSA playback via libasound, loaded with ffi (dlopen, no cgo) — the same
// pattern as the wgpu binding. Four functions cover blocking playback:
// open, set_params, writei, recover; writei's blocking paces the mixer.

import (
	"fmt"
	"unsafe"

	"github.com/sushvibez/gfx/ffi"
)

type alsaBackend struct {
	pcm uintptr
}

var alsa struct {
	open      func(pcm *uintptr, name string, stream int32, mode int32) int32
	setParams func(pcm uintptr, format, access int32, channels, rate uint32, softResample int32, latencyUs uint32) int32
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

func (a *alsaBackend) close() {
	if a.pcm != 0 {
		alsa.close(a.pcm)
		a.pcm = 0
	}
}
