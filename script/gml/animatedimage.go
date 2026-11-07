package gml

import (
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// AnimatedImage's playback over ui.ImagePlayer (the Go API for the same):
// each build turns the playing, paused, speed and currentFrame attributes
// into player calls when they change — playing restarts from the first
// frame, as QMovie does; paused holds the frame and resumes from it;
// writing currentFrame seeks. frameCount, currentFrame, playing and
// paused are on the id object, read at build.

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

// animPlayer is one AnimatedImage's player and the attribute values its
// calls were made for, kept across builds.
type animPlayer struct {
	p       *ui.ImagePlayer
	playing bool
	paused  bool
	speed   float64
	setTo   int // the last currentFrame written by markup (-1: none)
	started bool
}

// animPlayer returns the element's player, brought up to this build's
// attributes.
func (doc *docState) animPlayer(n *node, src string, a animatedImageAttrs) *ui.ImagePlayer {
	if doc.animPlayers == nil {
		doc.animPlayers = map[string]*animPlayer{}
	}
	key := doc.key(n) + "\x00" + src
	ap := doc.animPlayers[key]
	if ap == nil {
		ap = &animPlayer{p: ui.NewImagePlayer(doc.imageCell(src)), playing: true, speed: 1, setTo: -1}
		doc.animPlayers[key] = ap
	}
	p := ap.p
	playing := !a.hasPlaying || a.playing
	if a.paused != ap.paused || !ap.started {
		p.SetPaused(a.paused)
		ap.paused = a.paused
	}
	if playing != ap.playing || !ap.started {
		if playing {
			p.Play()
		} else {
			p.Stop()
		}
		ap.playing = playing
	}
	speed := 1.0
	if a.hasSpeed && a.speed > 0 {
		speed = a.speed
	}
	if speed != ap.speed {
		p.SetSpeed(speed)
		ap.speed = speed
	}
	// A currentFrame the markup wrote seeks, after the run state: it wins
	// over a (re)start's first frame.
	if a.hasFrame && a.frame != ap.setTo {
		p.Seek(a.frame)
		ap.setTo = a.frame
	}
	ap.started = true
	return p
}

// wireAnimatedImage gives the image its player and the id object its
// frameCount, currentFrame, playing and paused.
func (doc *docState) wireAnimatedImage(n *node, img *ui.LazyImageEl, src string, a animatedImageAttrs) {
	p := doc.animPlayer(n, src, a)
	img.Player(p)
	id := nodeID(n)
	if id == "" {
		return
	}
	obj := doc.elemObjs.get(doc.intern(id))
	if obj == nil {
		return
	}
	obj.setProp("frameCount", float64(p.FrameCount()))
	obj.setProp("currentFrame", float64(p.Frame()))
	obj.setProp("playing", p.Playing())
	obj.setProp("paused", p.Paused())
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
