//go:build !js

package render

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/core"
)

// A 2D-canvas gradient is placed by its own two points: it holds its end
// colours beyond them however much of the ramp the shape covers. Ours used
// to stretch the ramp over the shape, so the PFD's sky — a gradient
// running from two radii above the horizon down to it — came out navy at
// the top of the instrument instead of mid-blue.
func TestFillPathRampAxisIsPlacedNotStretched(t *testing.T) {
	const W, H = 60, 120
	stops := []GradientStop{
		{Pos: 0, Col: RGBA{R: 1, A: 1}},
		{Pos: 1, Col: RGBA{B: 1, A: 1}},
	}
	// The gradient runs from y=-120 to y=120; the shape covers only its
	// bottom quarter (y=60..120), so it should be nearly all blue.
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		p := NewPath()
		p.MoveTo(0, 60)
		p.LineTo(W, 60)
		p.LineTo(W, H)
		p.LineTo(0, H)
		p.Close()
		r.FillPathRampAxis(p, 0, -120, 0, 120, stops, NonZero)
	})
	at := func(x, y int) (int, int, int) {
		i := (y*W + x) * 4
		return int(pix[i]), int(pix[i+1]), int(pix[i+2])
	}
	// Top of the shape sits at ramp position 0.75 → mostly blue.
	if red, _, blue := at(30, 62); blue < 150 || red > 110 {
		t.Errorf("top of the shape = (%d,_,%d), want mostly blue", red, blue)
	}
	// The bottom is the ramp's end: pure blue.
	if red, _, blue := at(30, H-2); blue < 200 || red > 60 {
		t.Errorf("bottom of the shape = (%d,_,%d), want blue", red, blue)
	}
	// Stretched over the shape, the top would have been red instead.
	stretched := drawClipped(t, W, H, func(r *Renderer2D) {
		p := NewPath()
		p.MoveTo(0, 60)
		p.LineTo(W, 60)
		p.LineTo(W, H)
		p.LineTo(0, H)
		p.Close()
		r.FillPathRampRule(p, 1.5707963, stops, NonZero)
	})
	i := ((62)*W + 30) * 4
	if stretched[i] < 150 {
		t.Errorf("the shape-relative ramp starts at red=%d, want ~255 "+
			"(the two modes must differ)", stretched[i])
	}
}

// dropCollinear must collapse a genuinely straight run without decimating a
// CURVE. The test is anchored, not per-vertex: on a finely sampled arc every
// single point looks flat next to its two neighbours, so a per-vertex test
// dropped nearly all of them and the fill came out a polygon (the Qt logo's
// Q was a dodecagon at 300px). A straight edge still collapses to its ends.
func TestDropCollinearKeepsCurvesAndDropsStraights(t *testing.T) {
	// A quarter arc of radius 40, sampled every 0.5°: 180 points, each
	// locally flat to ~1e-3 px.
	var arc []core.Vec2
	for i := 0; i <= 180; i++ {
		a := float64(i) * math.Pi / 2 / 180
		arc = append(arc, core.Vec2{X: float32(40 * math.Cos(a)), Y: float32(40 * math.Sin(a))})
	}
	kept := dropCollinear(arc)
	if len(kept) < 20 {
		t.Errorf("kept %d of %d arc points: the curve was decimated", len(kept), len(arc))
	}
	// And what it keeps still tracks the arc: every dropped point is within
	// a fraction of a pixel of the kept outline.
	for _, p := range arc {
		best := float32(1e9)
		for i := 0; i+1 < len(kept); i++ {
			if d := segDist(p, kept[i], kept[i+1]); d < best {
				best = d
			}
		}
		if best > 0.1 {
			t.Fatalf("point %v strayed %v px from the simplified outline", p, best)
		}
	}
	// A straight run collapses: 100 points along one line become its ends
	// (plus the two off-line corners that make it a polygon).
	var edge []core.Vec2
	for i := 0; i <= 100; i++ {
		edge = append(edge, core.Vec2{X: float32(i), Y: 0})
	}
	edge = append(edge, core.Vec2{X: 50, Y: 30})
	if got := dropCollinear(edge); len(got) > 4 {
		t.Errorf("a straight run kept %d points: %v", len(got), got)
	}
}

// TestBigOutlineFillsFast: ear clipping used to scan the whole remaining
// ring for every candidate ear, which is O(n^3) — an outline of a thousand
// points (the shapes page's mandala and rose curves, rebuilt every frame)
// took SECONDS a frame. Only a REFLEX vertex can lie inside an ear, so the
// containment test scans those alone. The fill must still cover the shape:
// a 500-point circle of radius 100 has an area of ~31400, and a convex
// outline has no reflex vertices at all, so this is also the fast path.
func TestBigOutlineFillsFast(t *testing.T) {
	const n, r = 500, float32(100)
	poly := make([]core.Vec2, 0, n)
	for i := 0; i < n; i++ {
		th := 2 * math.Pi * float64(i) / n
		poly = append(poly, core.Vec2{
			X: 150 + r*float32(math.Cos(th)),
			Y: 150 + r*float32(math.Sin(th)),
		})
	}
	tris := triangulateRule([][]core.Vec2{poly}, NonZero, nil)
	if len(tris) < 3 || len(tris)%3 != 0 {
		t.Fatalf("triangulated into %d vertices", len(tris))
	}
	var area float32
	for i := 0; i+2 < len(tris); i += 3 {
		a, b, c := tris[i], tris[i+1], tris[i+2]
		ar := (b.X-a.X)*(c.Y-a.Y) - (c.X-a.X)*(b.Y-a.Y)
		if ar < 0 {
			ar = -ar
		}
		area += ar / 2
	}
	if area < 30000 || area > 32500 {
		t.Errorf("filled area = %.0f, want ~31400 (a circle of radius 100)", area)
	}
}
