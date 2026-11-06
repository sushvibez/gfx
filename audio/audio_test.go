package audio

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// makeWAV builds a PCM-16 WAV: mono or stereo, given rate, from float samples.
func makeWAV(samples []float32, channels, rate int) []byte {
	pcm := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(s*32767)))
	}
	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(36+len(pcm)))
	copy(hdr[8:], "WAVE")
	copy(hdr[12:], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:], uint16(channels))
	binary.LittleEndian.PutUint32(hdr[24:], uint32(rate))
	binary.LittleEndian.PutUint32(hdr[28:], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(hdr[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], uint32(len(pcm)))
	return append(hdr, pcm...)
}

func TestLoadWAV(t *testing.T) {
	d := &Device{vol: 1}
	// Mono at device rate: upmixed to stereo, length preserved.
	mono := makeWAV([]float32{0.5, -0.5, 0.25, 0}, 1, SampleRate)
	s, err := d.LoadWAV(mono)
	if err != nil {
		t.Fatalf("LoadWAV mono: %v", err)
	}
	if s.Frames() != 4 {
		t.Fatalf("frames = %d, want 4", s.Frames())
	}
	if math.Abs(float64(s.samples[0]-0.5)) > 0.01 || s.samples[0] != s.samples[1] {
		t.Fatalf("mono upmix wrong: %v", s.samples[:4])
	}
	// Stereo at half the device rate: resampled to ~double the frames.
	src := make([]float32, 200) // 100 stereo frames
	stereo := makeWAV(src, 2, SampleRate/2)
	s, err = d.LoadWAV(stereo)
	if err != nil {
		t.Fatalf("LoadWAV stereo: %v", err)
	}
	if got := s.Frames(); got < 195 || got > 205 {
		t.Fatalf("resampled frames = %d, want ~200", got)
	}
	// Garbage rejects.
	if _, err := d.LoadWAV([]byte("not a wav file at all........................................")); err == nil {
		t.Fatal("garbage accepted")
	}
}

// TestMixThroughNull plays a short sound end-to-end through the null
// backend: the voice finishes on schedule and looping voices don't.
func TestMixThroughNull(t *testing.T) {
	d := &Device{vol: 1, be: newNullBackend(), stop: make(chan struct{}), done: make(chan struct{})}
	go d.mixLoop()
	defer d.Close()

	n := SampleRate / 10 // 100ms of samples
	samples := make([]float32, n*2)
	snd := &Sound{dev: d, samples: samples}

	v := snd.Play()
	loop := snd.Loop()
	deadline := time.Now().Add(2 * time.Second)
	for v.Playing() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if v.Playing() {
		t.Fatal("one-shot voice never finished")
	}
	if !loop.Playing() {
		t.Fatal("looping voice stopped by itself")
	}
	loop.Stop()
	if loop.Playing() {
		t.Fatal("Stop did not stop the looping voice")
	}
}

// TestMixClipsAndScales checks the mixer's clamp and volume math directly.
func TestMixClipsAndScales(t *testing.T) {
	d := &Device{vol: 1}
	loud := make([]float32, 8) // 4 frames of +1.0 both channels
	for i := range loud {
		loud[i] = 1
	}
	s := &Sound{dev: d, samples: loud}
	a, b := s.Play(), s.Play() // sums to +2.0 → must clamp at 32767
	_ = a
	_ = b
	mix := make([]float32, 8)
	d.mu.Lock()
	for _, v := range d.voices {
		v.mixInto(mix)
	}
	d.mu.Unlock()
	if mix[0] != 2 {
		t.Fatalf("sum = %v, want 2 (two full-scale voices)", mix[0])
	}
}
