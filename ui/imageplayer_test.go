package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
	"time"
)

// gifOf encodes a GIF with one 4×4 frame per delay (centiseconds).
func gifOf(t *testing.T, delays ...int) []byte {
	t.Helper()
	g := &gif.GIF{}
	for _, d := range delays {
		g.Image = append(g.Image, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White}))
		g.Delay = append(g.Delay, d)
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestImagePlayer(t *testing.T) {
	t0 := time.Unix(1000, 0)
	now := t0
	at := func(ms int) { now = t0.Add(time.Duration(ms) * time.Millisecond) }
	p := &ImagePlayer{cell: &ImageCell{Data: gifOf(t, 10, 20, 10)}, speed: 1, playing: true, origin: t0,
		Now: func() time.Time { return now }}
	if p.FrameCount() != 3 {
		t.Fatalf("FrameCount = %d, want 3", p.FrameCount())
	}
	for ms, want := range map[int]int{0: 0, 99: 0, 100: 1, 299: 1, 300: 2, 400: 0, 550: 1} {
		at(ms)
		if got := p.Frame(); got != want {
			t.Errorf("Frame at %d ms = %d, want %d", ms, got, want)
		}
	}
	at(150)
	p.SetPaused(true)
	at(5000)
	if p.Frame() != 1 || p.Running() {
		t.Errorf("paused: frame %d running %v, want 1 false", p.Frame(), p.Running())
	}
	p.SetPaused(false)
	at(5200)
	if got := p.Frame(); got != 2 {
		t.Errorf("resumed +200 ms = %d, want 2", got)
	}
	at(6000)
	p.Seek(2)
	if got := p.Frame(); got != 2 {
		t.Errorf("after Seek(2): %d", got)
	}
	p.SetSpeed(2) // frame 2 (100 ms) passes in 50 ms
	at(6060)
	if got := p.Frame(); got != 0 {
		t.Errorf("speed 2, +60 ms = %d, want 0", got)
	}
	at(7000)
	p.Stop()
	held := p.Frame()
	at(8000)
	if p.Frame() != held || p.Playing() {
		t.Errorf("stopped: frame moved or still playing")
	}
	at(9000)
	p.Play()
	if got := p.Frame(); got != 0 {
		t.Errorf("Play restarts at %d, want 0", got)
	}
	// A still image is one frame and seeking does nothing.
	still := NewImagePlayer(&ImageCell{Data: []byte("not a gif")})
	still.Seek(3)
	if still.FrameCount() != 1 || still.Frame() != 0 {
		t.Errorf("still image: %d frames, frame %d", still.FrameCount(), still.Frame())
	}
}
