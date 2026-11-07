package gml

import (
	"bytes"
	"image/gif"
	"time"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// AnimatedImage's playback: playing, paused, speed and currentFrame drive
// a per-element clock, which the image asks for its frame at paint time
// (ui.LazyImageEl.FrameClock), so a playing GIF does not rebuild the page
// every frame. Playing restarts from the first frame, as QMovie does;
// paused holds the frame and resumes from it; writing currentFrame jumps
// there. frameCount and currentFrame are on the id object, read at build.
// The frame delays come from decoding the GIF on the CPU (image/gif), with
// the same 0 → 100 ms rule the texture decode uses.

// animatedImageAttrs are an AnimatedImage's playback attributes.
type animatedImageAttrs struct {
	playing, paused      bool
	speed                float64
	frame                int
	hasPlaying, hasSpeed bool
	hasFrame             bool
}

func (a *animatedImageAttrs) read(at attr, env *Env) error {
	switch at.name {
	case "playing", "paused":
		b, err := attrBool(at, env)
		if err != nil {
			return err
		}
		if at.name == "playing" {
			a.playing, a.hasPlaying = b, true
		} else {
			a.paused = b
		}
	case "speed":
		f, err := attrFloat(at, env)
		if err != nil {
			return err
		}
		a.speed, a.hasSpeed = float64(f), true
	case "currentFrame":
		f, err := attrFloat(at, env)
		if err != nil {
			return err
		}
		a.frame, a.hasFrame = int(f), true
	}
	return nil
}

// gifClock is one AnimatedImage's playback state, kept across builds.
type gifClock struct {
	delays  []time.Duration // per frame
	total   time.Duration
	origin  time.Time // when frame 0 started, at speed
	speed   float64
	held    bool // stopped or paused: showing heldAt
	heldAt  int
	playing bool // the last build's playing
	setTo   int  // the last currentFrame written by markup (-1: none)
}

// frameAt is the frame at now.
func (c *gifClock) frameAt(now time.Time) int {
	if c.held || c.total <= 0 || len(c.delays) == 0 {
		return c.heldAt
	}
	pos := time.Duration(float64(now.Sub(c.origin)) * c.speed)
	pos %= c.total
	if pos < 0 {
		pos += c.total
	}
	for i, d := range c.delays {
		if pos < d {
			return i
		}
		pos -= d
	}
	return len(c.delays) - 1
}

// offset is where frame i starts in the cycle.
func (c *gifClock) offset(i int) time.Duration {
	var t time.Duration
	for j := 0; j < i && j < len(c.delays); j++ {
		t += c.delays[j]
	}
	return t
}

// startAt runs the clock from frame i at now.
func (c *gifClock) startAt(i int, now time.Time) {
	c.held = false
	c.origin = now.Add(-time.Duration(float64(c.offset(i)) / c.speed))
}

// update applies one build's attributes at now.
func (c *gifClock) update(a animatedImageAttrs, now time.Time) {
	playing := !a.hasPlaying || a.playing
	speed := 1.0
	if a.hasSpeed && a.speed > 0 {
		speed = a.speed
	}
	cur := c.frameAt(now)
	if speed != c.speed {
		c.speed = speed
		if !c.held {
			c.startAt(cur, now) // the new speed from the frame showing
		}
	}
	switch run := playing && !a.paused; {
	case playing && !c.playing:
		c.heldAt = 0 // (re)started: from the first frame
		if run {
			c.startAt(0, now)
		} else {
			c.held = true
		}
	case run && c.held:
		c.startAt(c.heldAt, now) // resumed
	case !run && !c.held:
		c.heldAt, c.held = cur, true
	}
	// A currentFrame the markup wrote jumps there, after the run state:
	// it wins over a (re)start's first frame.
	if a.hasFrame && a.frame != c.setTo && len(c.delays) > 0 {
		c.setTo = a.frame
		cur = min(max(a.frame, 0), len(c.delays)-1)
		if c.held {
			c.heldAt = cur
		} else {
			c.startAt(cur, now)
		}
	}
	c.playing = playing
}

// animClock returns the element's clock, updated for this build.
func (doc *docState) animClock(n *node, src string, a animatedImageAttrs) *gifClock {
	if doc.gifClocks == nil {
		doc.gifClocks = map[string]*gifClock{}
	}
	key := doc.key(n) + "\x00" + src
	c := doc.gifClocks[key]
	now := time.Now()
	if c == nil {
		c = &gifClock{speed: 1, setTo: -1}
		c.delays = doc.gifDelays(src)
		for _, d := range c.delays {
			c.total += d
		}
		c.origin = now
		doc.gifClocks[key] = c
	}
	c.update(a, now)
	return c
}

// gifDelays decodes a GIF source's frame delays once (nil for a source
// that is not a multi-frame GIF).
func (doc *docState) gifDelays(src string) []time.Duration {
	if doc.gifDelayCache == nil {
		doc.gifDelayCache = map[string][]time.Duration{}
	}
	if d, ok := doc.gifDelayCache[src]; ok {
		return d
	}
	var out []time.Duration
	if c := doc.imageCell(src); c != nil && len(c.Data) > 0 {
		if g, err := gif.DecodeAll(bytes.NewReader(c.Data)); err == nil && len(g.Image) > 1 {
			for _, cs := range g.Delay {
				if cs <= 0 {
					cs = 10
				}
				out = append(out, time.Duration(cs)*10*time.Millisecond)
			}
		}
	}
	doc.gifDelayCache[src] = out
	return out
}

// wireAnimatedImage gives the image its clock and the id object its
// frameCount, currentFrame, playing and paused.
func (doc *docState) wireAnimatedImage(n *node, img *ui.LazyImageEl, src string, a animatedImageAttrs) {
	c := doc.animClock(n, src, a)
	img.FrameClock(func() (int, bool) {
		return c.frameAt(time.Now()), !c.held
	})
	id := nodeID(n)
	if id == "" {
		return
	}
	obj := doc.elemObjs.get(doc.intern(id))
	if obj == nil {
		return
	}
	count := len(c.delays)
	if count == 0 {
		count = 1
	}
	obj.setProp("frameCount", float64(count))
	obj.setProp("currentFrame", float64(c.frameAt(time.Now())))
	obj.setProp("playing", c.playing)
	obj.setProp("paused", c.held && c.playing)
}

// fillModeTile maps Image.fillMode's tiling values (Tile 3,
// TileVertically 4, TileHorizontally 5, Pad 6) to a ui fill; anything
// else is the plain fit.
func fillModeTile(v any) ui.ImageFill {
	if s, ok := v.(string); ok {
		for name, f := range map[string]ui.ImageFill{"TileVertically": ui.FillTileVertically,
			"TileHorizontally": ui.FillTileHorizontally, "Pad": ui.FillPad} {
			if len(s) >= len(name) && s[len(s)-len(name):] == name {
				return f
			}
		}
		if len(s) >= 4 && s[len(s)-4:] == "Tile" {
			return ui.FillTile
		}
		return ui.FillFit
	}
	f, err := toFloat64(v)
	if err != nil {
		return ui.FillFit
	}
	switch f {
	case 3:
		return ui.FillTile
	case 4:
		return ui.FillTileVertically
	case 5:
		return ui.FillTileHorizontally
	case 6:
		return ui.FillPad
	}
	return ui.FillFit
}

// patchModeOf maps BorderImage.TileMode (Stretch 0, Repeat 1, Round 2).
func patchModeOf(v any) render.PatchMode {
	if s, ok := v.(string); ok {
		switch {
		case len(s) >= 6 && s[len(s)-6:] == "Repeat":
			return render.PatchRepeat
		case len(s) >= 5 && s[len(s)-5:] == "Round":
			return render.PatchRound
		}
		return render.PatchStretch
	}
	if f, err := toFloat64(v); err == nil {
		switch f {
		case 1:
			return render.PatchRepeat
		case 2:
			return render.PatchRound
		}
	}
	return render.PatchStretch
}
