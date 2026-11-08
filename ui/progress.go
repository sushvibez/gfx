package ui

import (
	"time"

	"github.com/sushvibez/gfx/core"
)

// Progress and activity (QML's ProgressBar and BusyIndicator): a track with
// the done part filled in the accent, and a spinner. An indeterminate bar
// and a running spinner move on their own, driven by an Activity, a looping
// phase kept across frames.

// Activity is the looping phase an indeterminate ProgressBar or a running
// BusyIndicator draws from. Keep one per control; Run starts and stops it.
type Activity struct {
	phase core.Property[float32] // 0..1, round and round
	on    bool
	start time.Duration // the animation clock when it started
}

// activityCycle is one round of the loop.
const activityCycle = 1200 * time.Millisecond

// Run starts (true) or stops the loop; calling it every frame with the same
// value is cheap.
func (a *Activity) Run(on bool) {
	if on == a.on {
		return
	}
	a.on = on
	a.start = core.AnimClock()
	a.phase.Set(0)
	// It runs off the animation clock rather than as a tween, and asks
	// the frame loop for frames while it runs.
	core.SetExternalAnimation(a, on)
}

// Running reports whether it is looping; Phase where in the round it is.
func (a *Activity) Running() bool { return a.on }
func (a *Activity) Phase() float32 {
	if a.on {
		// Written into the property as it is read, so a build that cached
		// the last phase sees it change.
		t := (core.AnimClock() - a.start) % activityCycle
		a.phase.Set(float32(t) / float32(activityCycle))
	}
	return a.phase.Get()
}

// The bar's look: a 6 px light track, the done part in the accent.
var progressTrack = RGB(0xced4da)

// ProgressOpts configures a ProgressBar.
type ProgressOpts struct {
	// From and To are the value's range; both 0 is 0..1.
	From, To float32
	// Width and Height: 0 is 200 by 6.
	Width, Height float32
	// Indeterminate shows a segment sweeping along the track instead of the
	// value (QML's indeterminate); it is started for you.
	Indeterminate *Activity
}

// ProgressBar shows value within the range as a filled part of the track.
func ProgressBar(value float32, o ProgressOpts) *DivEl {
	w, h := o.Width, o.Height
	if w == 0 {
		w = 200
	}
	if h == 0 {
		h = 6
	}
	track := Div().Row().Size(w, h).Rounded(h / 2).Bg(progressTrack).Justify(AlignStart).Clip()
	if a := o.Indeterminate; a != nil {
		a.Run(true)
		// A third of the track, sweeping in from the left and out the right.
		seg := w / 3
		x := (w+seg)*a.Phase() - seg
		lead := x
		if lead < 0 {
			lead = 0
		}
		width := seg
		if x < 0 {
			width = seg + x
		}
		if lead+width > w {
			width = w - lead
		}
		if width < 0 {
			width = 0
		}
		return track.Children(Div().Size(lead, h), Div().Size(width, h).Rounded(h/2).Bg(Accent))
	}
	from, to := o.From, o.To
	if from == 0 && to == 0 {
		to = 1
	}
	frac := float32(0)
	if to != from {
		frac = clamp01((value - from) / (to - from))
	}
	return track.Children(Div().Size(w*frac, h).Rounded(h / 2).Bg(Accent))
}

// BusyIndicator is a spinner, size across (0: 32): a light ring with an
// accent dot going round while a runs (a nil or stopped one shows the dot
// still at the top).
func BusyIndicator(a *Activity, size float32) *DivEl {
	if size <= 0 {
		size = 32
	}
	dot := size / 5
	// The ring looks the same at any angle, so it turns with the dot on it.
	ring := Div().Size(size, size).Rounded(size/2).Border(3, progressTrack).
		Col().ItemsCenter().P(2).
		Children(Div().Size(dot, dot).Rounded(dot / 2).Bg(Accent))
	if a != nil && a.Running() {
		ring.Rotate(a.Phase() * 360)
	}
	return ring
}
