package audio

import (
	"math"
	"os"
	"testing"
)

// The testdata files are a 300ms 44.1kHz mono 440Hz sine encoded with
// ffmpeg (libvorbis / libmp3lame). Decoding must yield ~300ms of stereo at
// the device rate with sine-like energy (not silence, not clipping).
func testDecodedSine(t *testing.T, s *Sound) {
	t.Helper()
	frames := s.Frames()
	want := SampleRate * 3 / 10
	// Codecs pad edges (LAME adds ~65ms of encoder delay+padding): accept
	// the nominal length up to +100ms, minus 15%.
	if frames < want*85/100 || frames > want+SampleRate/10 {
		t.Fatalf("frames = %d, want ~%d", frames, want)
	}
	var peak, sumSq float64
	for _, v := range s.samples {
		a := math.Abs(float64(v))
		if a > peak {
			peak = a
		}
		sumSq += float64(v) * float64(v)
	}
	rms := math.Sqrt(sumSq / float64(len(s.samples)))
	if peak < 0.3 || peak > 1.01 {
		t.Fatalf("peak = %v, want sine-like (0.3..1)", peak)
	}
	if rms < 0.2 || rms > 0.8 {
		t.Fatalf("rms = %v, want sine-like (~0.7*peak)", rms)
	}
	// Stereo channels must match (mono upmix).
	if s.samples[1000] != s.samples[1001] {
		t.Fatalf("upmixed channels differ: %v vs %v", s.samples[1000], s.samples[1001])
	}
}

func TestLoadOGG(t *testing.T) {
	data, err := os.ReadFile("testdata/sine440.ogg")
	if err != nil {
		t.Skip("testdata missing")
	}
	d := &Device{vol: 1}
	s, err := d.Load(data) // exercises the sniffing path too
	if err != nil {
		t.Fatalf("Load ogg: %v", err)
	}
	testDecodedSine(t, s)
}

func TestLoadMP3(t *testing.T) {
	data, err := os.ReadFile("testdata/sine440.mp3")
	if err != nil {
		t.Skip("testdata missing")
	}
	d := &Device{vol: 1}
	s, err := d.Load(data)
	if err != nil {
		t.Fatalf("Load mp3: %v", err)
	}
	testDecodedSine(t, s)
}
