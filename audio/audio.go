// Package audio is the engine's sound playback: a software mixer over a
// tiny platform backend (Godot/Ebiten-style). Decoded sounds are PCM
// buffers; playing one adds a voice to the mixer, which streams mixed
// chunks to the device from its own goroutine.
//
//	dev, _ := audio.Open()
//	snd, _ := dev.LoadWAV(wavBytes)
//	snd.Play()                    // fire-and-forget
//	bgm := snd.Loop()             // looping voice
//	bgm.SetVolume(0.4)
//
// Backends: Linux ALSA (libasound via ffi, no cgo), Windows winmm waveOut,
// Android AAudio (cgo, like the rest of that backend), the browser's Web
// Audio (a queue of scheduled buffers). Platforms without a backend get a
// null device that keeps the API working (voices advance and finish)
// without output. Formats: WAV, Ogg Vorbis and MP3, all pure Go.
package audio

import (
	"fmt"
	"sync"
)

// SampleRate is the mixer's output rate; sounds resample to it at load.
const SampleRate = 48000

// chunkFrames is the mix quantum: writes of this many frames pace the mixer
// (~21ms at 48kHz — small enough for game feedback, large enough to be safe
// from underruns on desktop).
const chunkFrames = 1024

// backend is a blocking PCM sink: write paces the mix loop.
type backend interface {
	// write consumes interleaved stereo S16 frames, blocking until the
	// device has room. An error stops the mixer.
	write(buf []int16) error
	close()
}

// Device owns the backend and the mix goroutine.
type Device struct {
	mu     sync.Mutex
	voices []*Voice
	vol    float32
	be     backend
	stop   chan struct{}
	done   chan struct{}
}

// Open starts the platform audio device (falling back to a silent null
// device, so callers never need a special no-audio path).
func Open() (*Device, error) {
	be, err := openBackend()
	if err != nil {
		be = newNullBackend()
	}
	d := &Device{vol: 1, be: be, stop: make(chan struct{}), done: make(chan struct{})}
	go d.mixLoop()
	return d, nil
}

// SetVolume scales all output (0..1).
func (d *Device) SetVolume(v float32) {
	d.mu.Lock()
	d.vol = clamp01(v)
	d.mu.Unlock()
}

// Close stops the mixer and releases the device.
func (d *Device) Close() {
	close(d.stop)
	<-d.done
	d.be.close()
}

// Sound is a decoded, device-rate PCM buffer (interleaved stereo float32),
// shareable by any number of simultaneous voices.
type Sound struct {
	dev     *Device
	samples []float32 // interleaved L,R at SampleRate
}

// Frames returns the sound's length in frames.
func (s *Sound) Frames() int { return len(s.samples) / 2 }

// Play starts the sound once, at full volume.
func (s *Sound) Play() *Voice { return s.start(false) }

// Loop starts the sound looping until stopped.
func (s *Sound) Loop() *Voice { return s.start(true) }

func (s *Sound) start(loop bool) *Voice {
	v := &Voice{snd: s, vol: 1, loop: loop}
	d := s.dev
	d.mu.Lock()
	d.voices = append(d.voices, v)
	d.mu.Unlock()
	return v
}

// Voice is one playing instance of a Sound.
type Voice struct {
	snd  *Sound
	pos  int // frame cursor
	vol  float32
	loop bool
	stop bool
}

// SetVolume scales this voice (0..1).
func (v *Voice) SetVolume(vol float32) {
	d := v.snd.dev
	d.mu.Lock()
	v.vol = clamp01(vol)
	d.mu.Unlock()
}

// Stop ends the voice; it is removed on the next mix.
func (v *Voice) Stop() {
	d := v.snd.dev
	d.mu.Lock()
	v.stop = true
	d.mu.Unlock()
}

// Playing reports whether the voice is still audible (not stopped/finished).
func (v *Voice) Playing() bool {
	d := v.snd.dev
	d.mu.Lock()
	defer d.mu.Unlock()
	return !v.stop
}

// mixLoop mixes voices into S16 chunks and feeds the backend until Close.
func (d *Device) mixLoop() {
	defer close(d.done)
	mix := make([]float32, chunkFrames*2)
	out := make([]int16, chunkFrames*2)
	for {
		select {
		case <-d.stop:
			return
		default:
		}
		for i := range mix {
			mix[i] = 0
		}
		d.mu.Lock()
		vol := d.vol
		live := d.voices[:0]
		for _, v := range d.voices {
			if v.stop {
				continue
			}
			v.mixInto(mix)
			if !v.stop {
				live = append(live, v)
			}
		}
		d.voices = live
		d.mu.Unlock()
		for i, s := range mix {
			s *= vol
			switch {
			case s > 1:
				s = 1
			case s < -1:
				s = -1
			}
			out[i] = int16(s * 32767)
		}
		if err := d.be.write(out); err != nil {
			return
		}
	}
}

// mixInto adds the voice's next chunk into mix (interleaved stereo),
// advancing the cursor; a non-looping voice that reaches the end stops.
func (v *Voice) mixInto(mix []float32) {
	src := v.snd.samples
	for i := 0; i < len(mix); i += 2 {
		if v.pos*2 >= len(src) {
			if !v.loop {
				v.stop = true
				return
			}
			v.pos = 0
		}
		mix[i] += src[v.pos*2] * v.vol
		mix[i+1] += src[v.pos*2+1] * v.vol
		v.pos++
	}
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// errUnsupported is returned by platforms without a native backend.
var errUnsupported = fmt.Errorf("audio: no backend on this platform")
