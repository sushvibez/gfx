package app

import "math"

// Gestures folds a per-frame touch-event stream into high-level gesture
// state: two-finger pinch (scale + pan) and single-finger pan. Feed each
// frame's TouchEvents once; then read the state fields. Pure state machine —
// no platform calls — so hosts on any backend (and tests) drive it the same
// way. Tap detection is left to the platform's mouse synthesis.
type Gestures struct {
	// Pinching is true while two or more fingers are down.
	Pinching bool
	// Scale is the cumulative pinch factor since the pinch began (1 at
	// start; >1 spread, <1 squeeze). ScaleD is this frame's factor alone.
	Scale  float32
	ScaleD float32
	// Angle is the cumulative rotation of the pinch since it began, in
	// degrees clockwise (screen y grows down); AngleD is this frame's alone.
	Angle  float32
	AngleD float32
	// CX, CY is the current centroid of the active fingers; DX, DY is the
	// centroid's motion this frame (two-finger pan while pinching,
	// one-finger drag otherwise).
	CX, CY float32
	DX, DY float32
	// Down is the number of fingers currently on the surface.
	Down int

	touches map[int][2]float32 // live finger positions by ID
	baseHyp float32            // finger distance when the pinch began
	prevHyp float32
	prevAng float32 // the fingers' angle last frame, degrees
	prevCX  float32
	prevCY  float32
	hadPrev bool
}

// Feed applies one frame's touch events and recomputes the gesture state.
// Call once per frame with the full drain (an empty slice keeps the state
// but zeroes the per-frame deltas).
func (g *Gestures) Feed(events []TouchEvent) {
	if g.touches == nil {
		g.touches = map[int][2]float32{}
	}
	for _, e := range events {
		switch e.Phase {
		case TouchBegin, TouchMove:
			g.touches[e.ID] = [2]float32{e.X, e.Y}
		case TouchEnd, TouchCancel:
			delete(g.touches, e.ID)
		}
	}
	g.Down = len(g.touches)
	g.DX, g.DY, g.ScaleD, g.AngleD = 0, 0, 1, 0

	if g.Down == 0 {
		g.Pinching = false
		g.Scale, g.Angle = 1, 0
		g.hadPrev = false
		return
	}

	// Centroid of all active fingers.
	var cx, cy float32
	for _, p := range g.touches {
		cx += p[0]
		cy += p[1]
	}
	cx /= float32(g.Down)
	cy /= float32(g.Down)

	// Pinch scale and rotation: the two fingers with the lowest IDs — the
	// same pair every frame (map order is random, and a swapped pair turns
	// the angle half a turn).
	if g.Down >= 2 {
		var ida, idb int
		haveA, haveB := false, false
		for id := range g.touches {
			switch {
			case !haveA || id < ida:
				ida, idb, haveB = id, ida, haveA
				haveA = true
			case !haveB || id < idb:
				idb, haveB = id, true
			}
		}
		a, b := g.touches[ida], g.touches[idb]
		ang := float32(math.Atan2(float64(b[1]-a[1]), float64(b[0]-a[0])) * 180 / math.Pi)
		hyp := float32(math.Hypot(float64(a[0]-b[0]), float64(a[1]-b[1])))
		if !g.Pinching {
			g.Pinching = true
			g.baseHyp, g.prevHyp = hyp, hyp
			g.Scale, g.Angle, g.prevAng = 1, 0, ang
			// A new finger joining moves the centroid — don't count that
			// jump as pan.
			g.hadPrev = false
		}
		if g.baseHyp > 0 {
			g.Scale = hyp / g.baseHyp
		}
		if g.prevHyp > 0 {
			g.ScaleD = hyp / g.prevHyp
		}
		g.prevHyp = hyp
		d := ang - g.prevAng
		for d > 180 {
			d -= 360
		}
		for d <= -180 {
			d += 360
		}
		g.AngleD, g.Angle, g.prevAng = d, g.Angle+d, ang
	} else if g.Pinching {
		// Dropped back to one finger: pinch over; the remaining finger
		// pans from here (centroid jumps — suppress that frame's delta).
		g.Pinching = false
		g.Scale, g.Angle = 1, 0
		g.hadPrev = false
	}

	if g.hadPrev {
		g.DX, g.DY = cx-g.prevCX, cy-g.prevCY
	}
	g.CX, g.CY = cx, cy
	g.prevCX, g.prevCY = cx, cy
	g.hadPrev = true
}
