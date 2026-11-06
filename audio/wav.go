package audio

// WAV (RIFF) decoding: PCM 8/16-bit and IEEE float32, mono or stereo, any
// rate — normalized to the mixer's stereo float32 at SampleRate with linear
// resampling. The format every asset pipeline can emit; compressed formats
// (ogg/mp3) come later if the games need them.

import (
	"encoding/binary"
	"fmt"
	"math"
)

// LoadWAV decodes a WAV file into a device-rate Sound.
func (d *Device) LoadWAV(data []byte) (*Sound, error) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("audio: not a WAV file")
	}
	var format, channels int
	var rate int
	var bits int
	var pcm []byte
	// Walk the chunks: fmt then data.
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		body := off + 8
		if body+size > len(data) {
			size = len(data) - body
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, fmt.Errorf("audio: truncated fmt chunk")
			}
			format = int(binary.LittleEndian.Uint16(data[body:]))
			channels = int(binary.LittleEndian.Uint16(data[body+2:]))
			rate = int(binary.LittleEndian.Uint32(data[body+4:]))
			bits = int(binary.LittleEndian.Uint16(data[body+14:]))
		case "data":
			pcm = data[body : body+size]
		}
		off = body + size + size&1 // chunks are word-aligned
	}
	if pcm == nil || channels == 0 || rate == 0 {
		return nil, fmt.Errorf("audio: WAV missing fmt/data")
	}
	if channels > 2 {
		return nil, fmt.Errorf("audio: %d channels unsupported (want mono/stereo)", channels)
	}
	// Decode to mono-or-stereo float32 frames.
	var frames []float32 // interleaved at source rate/channels
	switch {
	case format == 1 && bits == 16:
		n := len(pcm) / 2
		frames = make([]float32, n)
		for i := 0; i < n; i++ {
			frames[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
		}
	case format == 1 && bits == 8: // unsigned
		frames = make([]float32, len(pcm))
		for i, b := range pcm {
			frames[i] = (float32(b) - 128) / 128
		}
	case format == 3 && bits == 32: // IEEE float
		n := len(pcm) / 4
		frames = make([]float32, n)
		for i := 0; i < n; i++ {
			frames[i] = math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:]))
		}
	default:
		return nil, fmt.Errorf("audio: WAV format %d/%d-bit unsupported", format, bits)
	}
	// Upmix mono → stereo.
	if channels == 1 {
		st := make([]float32, len(frames)*2)
		for i, s := range frames {
			st[i*2], st[i*2+1] = s, s
		}
		frames = st
	}
	// Resample to the device rate (linear interpolation).
	if rate != SampleRate {
		frames = resampleStereo(frames, rate, SampleRate)
	}
	return &Sound{dev: d, samples: frames}, nil
}

// resampleStereo linearly resamples interleaved stereo frames.
func resampleStereo(src []float32, from, to int) []float32 {
	nIn := len(src) / 2
	if nIn < 2 {
		return src
	}
	nOut := int(int64(nIn) * int64(to) / int64(from))
	out := make([]float32, nOut*2)
	step := float64(from) / float64(to)
	for i := 0; i < nOut; i++ {
		pos := float64(i) * step
		j := int(pos)
		f := float32(pos - float64(j))
		k := j + 1
		if k >= nIn {
			k = nIn - 1
		}
		out[i*2] = src[j*2]*(1-f) + src[k*2]*f
		out[i*2+1] = src[j*2+1]*(1-f) + src[k*2+1]*f
	}
	return out
}
