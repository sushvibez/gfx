package render

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/core"
)

// pathLength is the total length of every subpath in p.
func pathLength(p *Path) float64 {
	p.flushSub()
	var total float64
	for si, sub := range p.subs {
		pts := sub
		if si < len(p.closed) && p.closed[si] && len(pts) >= 3 {
			pts = append(append([]core.Vec2{}, pts...), pts[0])
		}
		for i := 0; i+1 < len(pts); i++ {
			dx := float64(pts[i+1].X - pts[i].X)
			dy := float64(pts[i+1].Y - pts[i].Y)
			total += math.Hypot(dx, dy)
		}
	}
	return total
}

// A line dashed 10 on / 10 off keeps half its length, in pieces of 10.
func TestDashHalvesAnEvenPattern(t *testing.T) {
	p := NewPath()
	p.MoveTo(0, 0).LineTo(100, 0)
	d := DashPath(p, []float32{10, 10}, 0)
	if got, want := pathLength(d), 50.0; math.Abs(got-want) > 0.01 {
		t.Errorf("dashed length %.2f, want %.2f", got, want)
	}
	d.flushSub()
	if len(d.subs) != 5 {
		t.Fatalf("got %d dashes, want 5", len(d.subs))
	}
	for i, sub := range d.subs {
		if len(sub) != 2 {
			t.Errorf("dash %d has %d points, want 2", i, len(sub))
			continue
		}
		if l := float64(sub[1].X - sub[0].X); math.Abs(l-10) > 0.01 {
			t.Errorf("dash %d is %.2f long, want 10", i, l)
		}
	}
}

// An odd-length pattern repeats doubled ([5] is 5 on, 5 off), and the
// offset shifts the phase: at offset 5 the line starts in a gap.
func TestDashOddPatternAndOffset(t *testing.T) {
	p := NewPath()
	p.MoveTo(0, 0).LineTo(100, 0)
	if got, want := pathLength(DashPath(p, []float32{5}, 0)), 50.0; math.Abs(got-want) > 0.01 {
		t.Errorf("odd pattern length %.2f, want %.2f", got, want)
	}
	shifted := DashPath(p, []float32{5}, 5)
	shifted.flushSub()
	if len(shifted.subs) == 0 {
		t.Fatal("offset dashed the whole line away")
	}
	if x := shifted.subs[0][0].X; math.Abs(float64(x)-5) > 0.01 {
		t.Errorf("first dash starts at %.2f, want 5 (offset into a gap)", x)
	}
}

// Dashing walks around a closed outline, the closing edge included, and
// never lengthens the path.
func TestDashClosedPath(t *testing.T) {
	p := NewPath()
	p.MoveTo(0, 0).LineTo(40, 0).LineTo(40, 40).LineTo(0, 40).Close()
	full := pathLength(p)
	if math.Abs(full-160) > 0.01 {
		t.Fatalf("square perimeter %.2f, want 160", full)
	}
	d := pathLength(DashPath(p, []float32{8, 8}, 0))
	if math.Abs(d-full/2) > 0.01 {
		t.Errorf("dashed perimeter %.2f, want %.2f", d, full/2)
	}
}

// "No pattern" means a solid stroke: an empty, zero or negative pattern
// hands the path back untouched rather than dashing it away.
func TestDashNoPatternIsSolid(t *testing.T) {
	p := NewPath()
	p.MoveTo(0, 0).LineTo(100, 0)
	for _, pat := range [][]float32{nil, {}, {0, 0}, {-4, 4}} {
		if got := DashPath(p, pat, 0); got != p {
			t.Errorf("pattern %v: got a dashed copy, want the path itself", pat)
		}
	}
}

// A zero run inside a pattern must not spin the walk: [0, 10] is all gap,
// and [10, 0] is solid.
func TestDashZeroRuns(t *testing.T) {
	p := NewPath()
	p.MoveTo(0, 0).LineTo(100, 0)
	if got := pathLength(DashPath(p, []float32{0, 10}, 0)); got > 0.01 {
		t.Errorf("zero-on pattern drew %.2f, want nothing", got)
	}
	if got := pathLength(DashPath(p, []float32{10, 0}, 0)); math.Abs(got-100) > 0.01 {
		t.Errorf("zero-off pattern drew %.2f, want the whole 100", got)
	}
}
