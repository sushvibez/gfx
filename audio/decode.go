package audio

// Compressed formats via pure-Go decoders (no cgo, like everything else):
// Ogg Vorbis through jfreymuth/oggvorbis and MP3 through hajimehoshi/go-mp3.
// Load sniffs the container so callers can stay format-agnostic.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	mp3 "github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
)

// Load decodes WAV, Ogg Vorbis or MP3 by sniffing the leading bytes.
func (d *Device) Load(data []byte) (*Sound, error) {
	switch {
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		return d.LoadWAV(data)
	case len(data) >= 4 && string(data[0:4]) == "OggS":
		return d.LoadOGG(data)
	case len(data) >= 3 && (string(data[0:3]) == "ID3" ||
		(data[0] == 0xff && data[1]&0xe0 == 0xe0)):
		return d.LoadMP3(data)
	}
	return nil, fmt.Errorf("audio: unrecognized format (want WAV, Ogg Vorbis or MP3)")
}

// LoadOGG decodes an Ogg Vorbis file into a device-rate Sound.
func (d *Device) LoadOGG(data []byte) (*Sound, error) {
	pcm, format, err := oggvorbis.ReadAll(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("audio: ogg: %w", err)
	}
	switch format.Channels {
	case 1:
		st := make([]float32, len(pcm)*2)
		for i, s := range pcm {
			st[i*2], st[i*2+1] = s, s
		}
		pcm = st
	case 2:
		// already interleaved stereo
	default:
		return nil, fmt.Errorf("audio: ogg: %d channels unsupported", format.Channels)
	}
	if format.SampleRate != SampleRate {
		pcm = resampleStereo(pcm, format.SampleRate, SampleRate)
	}
	return &Sound{dev: d, samples: pcm}, nil
}

// LoadMP3 decodes an MP3 file into a device-rate Sound.
func (d *Device) LoadMP3(data []byte) (*Sound, error) {
	dec, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("audio: mp3: %w", err)
	}
	// go-mp3 always emits 16-bit LE stereo at the source rate.
	raw, err := io.ReadAll(dec)
	if err != nil {
		return nil, fmt.Errorf("audio: mp3: %w", err)
	}
	n := len(raw) / 2
	pcm := make([]float32, n)
	for i := 0; i < n; i++ {
		pcm[i] = float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / 32768
	}
	if rate := dec.SampleRate(); rate != SampleRate {
		pcm = resampleStereo(pcm, rate, SampleRate)
	}
	return &Sound{dev: d, samples: pcm}, nil
}
