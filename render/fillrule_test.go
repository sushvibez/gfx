package render

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/core"
)

// polyArea is the signed area of a simple outline (absolute value).
func polyArea(p []core.Vec2) float64 {
	var a float64
	for i := range p {
		q := p[(i+1)%len(p)]
		a += float64(p[i].X*q.Y - q.X*p[i].Y)
	}
	return math.Abs(a) / 2
}

// trisArea sums the areas of a triangle list.
func trisArea(t []core.Vec2) float64 {
	var a float64
	for i := 0; i+2 < len(t); i += 3 {
		a += math.Abs(float64((t[i+1].X-t[i].X)*(t[i+2].Y-t[i].Y)-
			(t[i+2].X-t[i].X)*(t[i+1].Y-t[i].Y))) / 2
	}
	return a
}

// sampledArea is the ground truth: how much of a 200x200 box the outline
// covers under a fill rule, decided per sample point by counting the
// crossings of a ray (parity) or summing their directions (winding).
func sampledArea(poly []core.Vec2, rule FillRule) float64 {
	const step = 0.5
	var area float64
	for y := 0.0; y < 200; y += step {
		for x := 0.0; x < 200; x += step {
			w, crossings := 0, 0
			for i := range poly {
				a, b := poly[i], poly[(i+1)%len(poly)]
				if (float64(a.Y) <= y) == (float64(b.Y) <= y) {
					continue
				}
				t := (y - float64(a.Y)) / float64(b.Y-a.Y)
				if float64(a.X)+t*float64(b.X-a.X) <= x {
					continue
				}
				crossings++
				if b.Y > a.Y {
					w++
				} else {
					w--
				}
			}
			inside := crossings%2 == 1
			if rule == NonZero {
				inside = w != 0
			}
			if inside {
				area += step * step
			}
		}
	}
	return area
}

// pentagram: five points connected skipping one, so the outline crosses
// itself — the shape a fill rule is about.
func pentagram() []core.Vec2 {
	var star []core.Vec2
	for i := 0; i < 5; i++ {
		a := -math.Pi/2 + float64(i)*4*math.Pi/5
		star = append(star, core.Vec2{
			X: float32(100 + 80*math.Cos(a)),
			Y: float32(100 + 80*math.Sin(a)),
		})
	}
	return star
}

// The corpus star: concave but SIMPLE, so both rules cover exactly its
// area — including when the path closes by repeating its start point, which
// used to make the top vertex look flat and lose the whole point.
func TestSimpleStarFillsWholeArea(t *testing.T) {
	star := []core.Vec2{
		{X: 70, Y: 5}, {X: 85, Y: 55}, {X: 135, Y: 55}, {X: 95, Y: 85},
		{X: 110, Y: 135}, {X: 70, Y: 105}, {X: 30, Y: 135}, {X: 45, Y: 85},
		{X: 5, Y: 55}, {X: 55, Y: 55},
	}
	closed := append(append([]core.Vec2{}, star...), star[0])
	want := polyArea(star)
	for _, tc := range []struct {
		name string
		poly []core.Vec2
	}{{"open", star}, {"closed by repeating the start", closed}} {
		for _, rule := range []FillRule{EvenOdd, NonZero} {
			got := trisArea(triangulateRule([][]core.Vec2{tc.poly}, rule, nil))
			if math.Abs(got-want)/want > 0.02 {
				t.Errorf("%s, rule %v: area %.0f, want %.0f", tc.name, rule, got, want)
			}
		}
	}
}

// On a self-crossing outline each rule must match what a per-pixel test of
// that rule says: even-odd leaves the middle pentagon hollow, non-zero
// fills it.
func TestPentagramMatchesEachFillRule(t *testing.T) {
	star := pentagram()
	if !selfIntersects(star) {
		t.Fatal("pentagram not detected as self-crossing")
	}
	for _, rule := range []FillRule{EvenOdd, NonZero} {
		want := sampledArea(star, rule)
		got := trisArea(triangulateRule([][]core.Vec2{star}, rule, nil))
		if math.Abs(got-want)/want > 0.02 {
			t.Errorf("rule %v: tessellated %.0f, sampled %.0f", rule, got, want)
		}
	}
	odd := trisArea(triangulateRule([][]core.Vec2{star}, EvenOdd, nil))
	non := trisArea(triangulateRule([][]core.Vec2{star}, NonZero, nil))
	if non <= odd {
		t.Errorf("non-zero (%.0f) must enclose more than even-odd (%.0f)", non, odd)
	}
}

// Ordinary paths must not be mistaken for self-crossing ones: that would
// trade the exact ear clipper for the scanline on every fill.
func TestSelfIntersectsSimplePaths(t *testing.T) {
	star := []core.Vec2{
		{X: 70, Y: 5}, {X: 85, Y: 55}, {X: 135, Y: 55}, {X: 95, Y: 85},
		{X: 110, Y: 135}, {X: 70, Y: 105}, {X: 30, Y: 135}, {X: 45, Y: 85},
		{X: 5, Y: 55}, {X: 55, Y: 55},
	}
	if selfIntersects(star) {
		t.Error("the concave star is simple, but was reported as crossing itself")
	}
	p := NewPath()
	p.MoveTo(10, 10).LineTo(110, 10).LineTo(110, 60).LineTo(10, 60).Close()
	p.flushSub()
	if selfIntersects(p.subs[0]) {
		t.Error("a rectangle was reported as crossing itself")
	}
	rr := NewPath()
	rr.MoveTo(20, 10).LineTo(80, 10)
	for i := 0; i <= 8; i++ { // a flattened corner arc
		a := -math.Pi/2 + float64(i)*math.Pi/16
		rr.LineTo(float32(80+10*math.Cos(a)), float32(20+10*math.Sin(a)))
	}
	rr.LineTo(90, 60).LineTo(20, 60).Close()
	rr.flushSub()
	if selfIntersects(rr.subs[0]) {
		t.Error("a rounded rect was reported as crossing itself")
	}
}
