package render

// Dashed strokes: a dash pattern is applied to the GEOMETRY, not the
// shader — the path is cut into its "on" runs and the result is stroked
// like any other path, so caps, joins and widths keep working, and the
// dashes stay put when the shape animates (they are measured along the
// outline, not in screen space).
//
// This is SVG's stroke-dasharray, canvas setLineDash and QML's
// ShapePath.dashPattern (whose lengths are in pen widths, scaled by the
// caller).

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// DashPath returns the on-pieces of p as a new path of open subpaths: each
// subpath is walked and cut into alternating on/off runs by the pattern
// (lengths in path units), starting `offset` units into it.
//
// An odd-length pattern repeats doubled, as canvas and SVG both specify —
// [5] dashes 5 on, 5 off. A pattern that is empty, negative or all zeros
// returns p unchanged, since "no dashes" means a solid stroke.
func DashPath(p *Path, pattern []float32, offset float32) *Path {
	pat := normalizeDash(pattern)
	if pat == nil {
		return p
	}
	p.flushSub()
	out := NewPath()
	var total float32
	for _, d := range pat {
		total += d
	}
	for si, sub := range p.subs {
		pts := sub
		if si < len(p.closed) && p.closed[si] && len(pts) >= 3 {
			pts = append(append([]core.Vec2{}, pts...), pts[0]) // walk the closing edge too
		}
		dashSubpath(out, pts, pat, total, offset)
	}
	out.flushSub()
	return out
}

// normalizeDash validates a pattern and doubles an odd-length one; nil
// means "no dashing".
func normalizeDash(pattern []float32) []float32 {
	var total float32
	for _, d := range pattern {
		if d < 0 {
			return nil // a negative run is not a pattern; SVG ignores it
		}
		total += d
	}
	if len(pattern) == 0 || total <= 0 {
		return nil
	}
	if len(pattern)%2 == 1 {
		return append(append([]float32{}, pattern...), pattern...)
	}
	return pattern
}

// dashSubpath walks one polyline, emitting the on-runs into out.
func dashSubpath(out *Path, pts []core.Vec2, pat []float32, total, offset float32) {
	if len(pts) < 2 {
		return
	}
	// Where in the pattern the subpath starts. Each subpath restarts the
	// phase, as Qt and SVG do.
	phase := offset
	if total > 0 {
		phase -= total * float32(int(phase/total))
		if phase < 0 {
			phase += total
		}
	}
	idx := 0
	for phase >= pat[idx] {
		phase -= pat[idx]
		idx = (idx + 1) % len(pat)
	}
	on := idx%2 == 0
	left := pat[idx] - phase // how much of the current run is still to come
	drawing := false
	if on {
		out.MoveTo(pts[0].X, pts[0].Y)
		drawing = true
	}
	// advance ends the current run at `at` and begins the next one there.
	advance := func(at core.Vec2) {
		if on {
			out.LineTo(at.X, at.Y)
			drawing = false
		} else {
			out.MoveTo(at.X, at.Y)
			drawing = true
		}
		on = !on
		idx = (idx + 1) % len(pat)
		left = pat[idx]
	}
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		dx, dy := b.X-a.X, b.Y-a.Y
		segLen := hypot(dx, dy)
		if segLen == 0 {
			continue
		}
		t := float32(0) // how far along this segment we are, 0..segLen
		for segLen-t > left {
			t += left
			at := core.Vec2{X: a.X + dx*t/segLen, Y: a.Y + dy*t/segLen}
			advance(at)
			// A zero-length run (`[10, 0]`, or the dotted-line idiom
			// `[0, 6]` with round caps) turns again at the same point,
			// rather than consuming any of the segment.
			for left == 0 {
				advance(at)
			}
		}
		left -= segLen - t
		if on && drawing {
			out.LineTo(b.X, b.Y)
		}
	}
}

func hypot(dx, dy float32) float32 {
	if dx == 0 {
		return abs32(dy)
	}
	if dy == 0 {
		return abs32(dx)
	}
	return math32.Hypot(dx, dy)
}

// StrokePathDashed strokes p with a dash pattern: it cuts the path into its
// on-runs and strokes those. `round` selects round caps, as StrokePathRound
// does for a solid stroke.
func (r *Renderer2D) StrokePathDashed(p *Path, width float32, c RGBA, pattern []float32, offset float32, round bool) {
	dashed := DashPath(p, pattern, offset)
	if round {
		r.StrokePathRound(dashed, width, c)
		return
	}
	r.StrokePath(dashed, width, c)
}
