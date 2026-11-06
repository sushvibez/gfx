package render

// AnimationPlayer plays glTF clips on a Model — the Godot AnimationPlayer
// shape, with crossfading:
//
//	player := render.NewAnimationPlayer(model, doc.Animations)
//	player.Play("Idle", render.PlayOpts{Loop: true})
//	// on a state change:
//	player.Play("Firing Rifle", render.PlayOpts{Fade: 0.15})
//	// per frame:
//	player.Advance(dt)
//	model.Draw(r3, transform)
//
// Advance samples the current clip into the model's node pose and
// recomputes item transforms. While a fade is running BOTH clips are
// sampled — each into its own gltf.Pose buffer, starting from the model's
// rest pose so joints only one clip drives fade against rest — and the two
// are mixed per joint (translation/scale lerp, rotation short-arc slerp)
// before being written back into the hierarchy. Buffers are reused, so a
// blended frame allocates nothing.

import "github.com/sushvibez/gfx/render/gltf"

// PlayOpts configures one Play call.
type PlayOpts struct {
	Loop  bool    // wrap at the clip end (else clamp, stop and report Finished)
	Fade  float32 // seconds to crossfade from the pose currently playing
	Speed float32 // playback rate; 0 means 1
}

// AnimationPlayer drives a Model's node pose from a clip list.
type AnimationPlayer struct {
	// Loop is the running clip's looping flag. PlayOpts.Loop writes it, a
	// Play/PlayIndex without options inherits it, and flipping it mid-clip
	// takes effect immediately (the pre-crossfade API's behaviour).
	Loop bool

	// OnFinished fires once when a non-looping clip reaches its end.
	OnFinished func(name string)
	// onLoop fires each time a looping clip wraps (SetClipCallbacks).
	onLoop func(name string)

	model *Model
	clips []gltf.Animation
	rest  gltf.Pose // model's bind pose: the baseline every sample starts from

	current  int
	t        float32
	speed    float32
	playing  bool
	finished bool

	// Crossfade source: the clip that was playing when Play(Fade>0) ran.
	prev      int
	prevT     float32
	prevSpeed float32
	prevLoop  bool
	fading    bool
	fadeT     float32
	fadeLen   float32

	poseCur, posePrev, poseOut gltf.Pose
}

// NewAnimationPlayer wraps a model with its document's clips.
func NewAnimationPlayer(m *Model, clips []gltf.Animation) *AnimationPlayer {
	p := &AnimationPlayer{Loop: true, model: m, clips: clips, current: -1, prev: -1, speed: 1}
	if m != nil {
		p.rest = gltf.NewPose(m.nodes)
		p.poseCur = gltf.NewPose(m.nodes)
		p.posePrev = gltf.NewPose(m.nodes)
		p.poseOut = gltf.NewPose(m.nodes)
	}
	return p
}

// Clips returns the clip names in order.
func (p *AnimationPlayer) Clips() []string {
	out := make([]string, len(p.clips))
	for i := range p.clips {
		out[i] = p.clips[i].Name
	}
	return out
}

// SetClipCallbacks registers the clip lifecycle hooks: finished fires once
// when a non-looping clip hits its end (same hook as OnFinished), looped
// fires every time a looping clip wraps. Either may be nil.
func (p *AnimationPlayer) SetClipCallbacks(finished, looped func(name string)) {
	p.OnFinished, p.onLoop = finished, looped
}

// Play starts the named clip, optionally crossfading from the current pose
// (false if the name is unknown). With no PlayOpts it behaves like the
// pre-crossfade API: restart at t=0 with the player's Loop flag.
func (p *AnimationPlayer) Play(name string, opts ...PlayOpts) bool {
	for i := range p.clips {
		if p.clips[i].Name == name {
			p.PlayIndex(i, opts...)
			return true
		}
	}
	return false
}

// PlayIndex starts clip i from t=0 with the same options as Play.
func (p *AnimationPlayer) PlayIndex(i int, opts ...PlayOpts) {
	if i < 0 || i >= len(p.clips) {
		return
	}
	o := PlayOpts{Loop: p.Loop}
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Speed == 0 {
		o.Speed = 1
	}
	// Crossfade from whatever is on screen now (including a fade already in
	// flight: its blended result is what the new clip fades away from —
	// approximated by the outgoing clip, which is where that blend is
	// heading).
	if o.Fade > 0 && p.current >= 0 {
		p.prev, p.prevT = p.current, p.t
		p.prevSpeed, p.prevLoop = p.speed, p.Loop
		p.fading, p.fadeT, p.fadeLen = true, 0, o.Fade
	} else {
		p.fading = false
	}
	p.current, p.t = i, 0
	p.Loop, p.speed = o.Loop, o.Speed
	p.playing, p.finished = true, false
	p.apply()
}

// Stop halts playback, freezing the last pose (it is not reset to rest and
// not blended away — a held stance).
func (p *AnimationPlayer) Stop() { p.playing = false }

// Resume restarts a stopped player from where Stop froze it.
func (p *AnimationPlayer) Resume() {
	if p.current >= 0 && !p.finished {
		p.playing = true
	}
}

// Playing returns the name of the clip currently advancing, or "" when the
// player is stopped, finished or has never been started.
func (p *AnimationPlayer) Playing() string {
	if !p.playing || p.current < 0 {
		return ""
	}
	return p.clips[p.current].Name
}

// Current returns the loaded clip's name whether or not it is advancing
// (a frozen Stop() pose still reports its clip).
func (p *AnimationPlayer) Current() string {
	if p.current < 0 {
		return ""
	}
	return p.clips[p.current].Name
}

// IsPlaying reports whether a clip is advancing.
func (p *AnimationPlayer) IsPlaying() bool { return p.playing }

// Finished reports whether the current non-looping clip ran to its end.
// Looping clips never finish.
func (p *AnimationPlayer) Finished() bool { return p.finished }

// Fading reports whether a crossfade is in flight, and how far along it is
// (0 at the start, 1 at the end).
func (p *AnimationPlayer) Fading() (bool, float32) {
	if !p.fading || p.fadeLen <= 0 {
		return false, 1
	}
	return true, p.fadeT / p.fadeLen
}

// Speed returns the current playback rate.
func (p *AnimationPlayer) Speed() float32 { return p.speed }

// SetSpeed changes the playback rate of the running clip.
func (p *AnimationPlayer) SetSpeed(s float32) {
	if s != 0 {
		p.speed = s
	}
}

// Position returns the current clip time in seconds.
func (p *AnimationPlayer) Position() float32 { return p.t }

// Duration returns the current clip's length (0 when none).
func (p *AnimationPlayer) Duration() float32 {
	if p.current < 0 {
		return 0
	}
	return p.clips[p.current].Duration
}

// Advance moves playback by dt seconds and re-poses the model. A looping
// player wraps; a non-looping one clamps to the end, stops and reports
// Finished (firing OnFinished once). A running crossfade advances its
// outgoing clip too, so the blend mixes two live poses.
func (p *AnimationPlayer) Advance(dt float32) {
	if !p.playing || p.current < 0 {
		return
	}
	var done bool
	p.t, done = advanceClip(p.t, dt*p.speed, p.clips[p.current].Duration, p.Loop)
	if done && p.Loop && p.onLoop != nil {
		p.onLoop(p.clips[p.current].Name)
	}
	if p.fading {
		if p.prev >= 0 && p.prev < len(p.clips) {
			p.prevT, _ = advanceClip(p.prevT, dt*p.prevSpeed, p.clips[p.prev].Duration, p.prevLoop)
		}
		p.fadeT += dt
		if p.fadeLen <= 0 || p.fadeT >= p.fadeLen {
			p.fading, p.prev = false, -1
		}
	}
	if done && !p.Loop {
		p.playing, p.finished = false, true
	}
	p.apply()
	if done && !p.Loop && p.OnFinished != nil {
		p.OnFinished(p.clips[p.current].Name)
	}
}

// advanceClip moves t by dt within a clip of length d, returning the new
// time and whether it hit the end (wrapped when looping, clamped if not).
func advanceClip(t, dt, d float32, loop bool) (float32, bool) {
	t += dt
	if d <= 0 {
		return 0, false
	}
	switch {
	case t >= d:
		if !loop {
			return d, true
		}
		for t >= d {
			t -= d
		}
		return t, true
	case t < 0:
		if !loop {
			return 0, true
		}
		for t < 0 {
			t += d
		}
		return t, true
	}
	return t, false
}

// Seek jumps to an absolute clip time (clamped) and re-poses the model.
func (p *AnimationPlayer) Seek(t float32) {
	if p.current < 0 {
		return
	}
	if d := p.clips[p.current].Duration; t > d {
		t = d
	}
	if t < 0 {
		t = 0
	}
	p.t = t
	p.apply()
}

// apply samples the clip(s) and writes the resulting pose into the model.
func (p *AnimationPlayer) apply() {
	if p.current < 0 || p.model == nil {
		return
	}
	if p.poseCur.Len() != len(p.model.nodes) {
		// The model was built after the player (or swapped): (re)snapshot.
		p.rest = gltf.NewPose(p.model.nodes)
		p.poseCur = gltf.NewPose(p.model.nodes)
		p.posePrev = gltf.NewPose(p.model.nodes)
		p.poseOut = gltf.NewPose(p.model.nodes)
	}
	p.poseCur.Reset(p.rest)
	p.clips[p.current].SampleInto(p.t, p.poseCur)
	out := p.poseCur
	if p.fading && p.prev >= 0 && p.prev < len(p.clips) && p.fadeLen > 0 {
		p.posePrev.Reset(p.rest)
		p.clips[p.prev].SampleInto(p.prevT, p.posePrev)
		gltf.BlendPose(p.poseOut, p.posePrev, p.poseCur, p.fadeT/p.fadeLen)
		out = p.poseOut
	}
	out.Write(p.model.nodes)
	p.model.updateWorlds()
}
