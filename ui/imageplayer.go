package ui

import (
	"bytes"
	"image/gif"
	"time"
)

// ImagePlayer plays an animated image (a multi-frame GIF in an ImageCell)
// under the program's control: play, stop, pause, speed and seeking, as
// QML's AnimatedImage offers. Attach it with LazyImageEl.Player; the
// image asks it for its frame at paint time, so a playing image does not
// rebuild the tree each frame, and a stopped or paused one lets the
// window go idle. Without a player a GIF loops on the wall clock.
//
// Play restarts from the first frame and Stop holds the frame showing, as
// QMovie does; SetPaused holds and resumes where it was. A new player is
// playing from the first frame. Keep one across frames.
type ImagePlayer struct {
	cell *ImageCell
	// Now is the clock (nil: time.Now); tests set it.
	Now func() time.Time

	delays          []time.Duration
	total           time.Duration
	loaded          bool
	origin          time.Time // when frame 0 started, at speed
	speed           float64
	playing, paused bool
	heldAt          int // the frame shown while stopped or paused
}

// NewImagePlayer returns a player over cell, playing from its first frame.
func NewImagePlayer(cell *ImageCell) *ImagePlayer {
	p := &ImagePlayer{cell: cell, speed: 1, playing: true}
	p.origin = p.now()
	return p
}

func (p *ImagePlayer) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// load reads the frame delays from the cell's bytes, once (CPU decode:
// the count is known before any texture exists). A GIF frame with no
// delay shows for 100 ms, as the texture decode treats it.
func (p *ImagePlayer) load() {
	if p.loaded || p.cell == nil || len(p.cell.Data) == 0 {
		return
	}
	p.loaded = true
	g, err := gif.DecodeAll(bytes.NewReader(p.cell.Data))
	if err != nil || len(g.Image) < 2 {
		return
	}
	for _, cs := range g.Delay {
		if cs <= 0 {
			cs = 10
		}
		d := time.Duration(cs) * 10 * time.Millisecond
		p.delays = append(p.delays, d)
		p.total += d
	}
}

// FrameCount is the number of frames (1 for a still image).
func (p *ImagePlayer) FrameCount() int {
	p.load()
	return max(len(p.delays), 1)
}

// Playing reports whether the player is playing (it may still be paused).
func (p *ImagePlayer) Playing() bool { return p.playing }

// Paused reports whether the player is paused.
func (p *ImagePlayer) Paused() bool { return p.paused }

// Running reports whether frames are advancing: playing and not paused.
func (p *ImagePlayer) Running() bool { return p.playing && !p.paused }

// Frame is the frame showing now.
func (p *ImagePlayer) Frame() int {
	p.load()
	if !p.Running() || p.total <= 0 {
		return p.heldAt
	}
	pos := time.Duration(float64(p.now().Sub(p.origin)) * p.speed)
	pos %= p.total
	if pos < 0 {
		pos += p.total
	}
	for i, d := range p.delays {
		if pos < d {
			return i
		}
		pos -= d
	}
	return len(p.delays) - 1
}

// startAt runs the clock from frame i now.
func (p *ImagePlayer) startAt(i int) {
	var off time.Duration
	for j := 0; j < i && j < len(p.delays); j++ {
		off += p.delays[j]
	}
	p.origin = p.now().Add(-time.Duration(float64(off) / p.speed))
}

// Play starts playing from the first frame (a paused player shows the
// first frame and stays paused).
func (p *ImagePlayer) Play() {
	p.load()
	p.playing, p.heldAt = true, 0
	if !p.paused {
		p.startAt(0)
	}
}

// Stop stops on the frame showing.
func (p *ImagePlayer) Stop() {
	if p.playing {
		p.heldAt = p.Frame()
		p.playing = false
	}
}

// SetPaused pauses on the frame showing, or resumes from it.
func (p *ImagePlayer) SetPaused(paused bool) {
	if paused == p.paused {
		return
	}
	if paused {
		p.heldAt = p.Frame()
		p.paused = true
		return
	}
	p.paused = false
	if p.playing {
		p.startAt(p.heldAt)
	}
}

// SetSpeed sets the playback rate (1 is the image's own; non-positive
// values are ignored), carrying on from the frame showing.
func (p *ImagePlayer) SetSpeed(s float64) {
	if s <= 0 || s == p.speed {
		return
	}
	cur := p.Frame()
	p.speed = s
	if p.Running() {
		p.startAt(cur)
	}
}

// Seek shows frame i (clamped), carrying on from it when running.
func (p *ImagePlayer) Seek(i int) {
	p.load()
	if len(p.delays) == 0 {
		return
	}
	i = min(max(i, 0), len(p.delays)-1)
	if p.Running() {
		p.startAt(i)
	} else {
		p.heldAt = i
	}
}

// Player attaches p: the image shows p's frame (see ImagePlayer).
func (e *LazyImageEl) Player(p *ImagePlayer) *LazyImageEl {
	return e.FrameClock(func() (int, bool) { return p.Frame(), p.Running() })
}
