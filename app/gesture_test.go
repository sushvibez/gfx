package app

import (
	"math"
	"testing"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func TestGesturesSingleFingerPan(t *testing.T) {
	var g Gestures
	g.Feed([]TouchEvent{{ID: 1, Phase: TouchBegin, X: 100, Y: 100}})
	if g.Down != 1 || g.Pinching || g.DX != 0 || g.DY != 0 {
		t.Fatalf("after begin: %+v", g)
	}
	g.Feed([]TouchEvent{{ID: 1, Phase: TouchMove, X: 110, Y: 95}})
	if !near(g.DX, 10) || !near(g.DY, -5) {
		t.Fatalf("pan delta = %v,%v want 10,-5", g.DX, g.DY)
	}
	g.Feed(nil) // idle frame: state holds, deltas zero
	if g.DX != 0 || g.DY != 0 || g.Down != 1 {
		t.Fatalf("idle frame: %+v", g)
	}
	g.Feed([]TouchEvent{{ID: 1, Phase: TouchEnd, X: 110, Y: 95}})
	if g.Down != 0 {
		t.Fatalf("after end: %+v", g)
	}
}

func TestGesturesPinch(t *testing.T) {
	var g Gestures
	g.Feed([]TouchEvent{
		{ID: 1, Phase: TouchBegin, X: 100, Y: 100},
		{ID: 2, Phase: TouchBegin, X: 200, Y: 100},
	})
	if !g.Pinching || !near(g.Scale, 1) {
		t.Fatalf("pinch start: %+v", g)
	}
	// Spread fingers to double the distance: scale = 2, centroid unmoved.
	g.Feed([]TouchEvent{
		{ID: 1, Phase: TouchMove, X: 50, Y: 100},
		{ID: 2, Phase: TouchMove, X: 250, Y: 100},
	})
	if !near(g.Scale, 2) || !near(g.ScaleD, 2) {
		t.Fatalf("scale = %v (d %v), want 2", g.Scale, g.ScaleD)
	}
	if !near(g.DX, 0) || !near(g.DY, 0) {
		t.Fatalf("centroid moved: %v,%v", g.DX, g.DY)
	}
	// Two-finger pan: both fingers +30x — centroid follows, scale holds.
	g.Feed([]TouchEvent{
		{ID: 1, Phase: TouchMove, X: 80, Y: 100},
		{ID: 2, Phase: TouchMove, X: 280, Y: 100},
	})
	if !near(g.DX, 30) || !near(g.Scale, 2) || !near(g.ScaleD, 1) {
		t.Fatalf("pan while pinched: %+v", g)
	}
	// Lift one finger: pinch ends, no spurious pan jump.
	g.Feed([]TouchEvent{{ID: 2, Phase: TouchEnd, X: 280, Y: 100}})
	if g.Pinching || g.DX != 0 || g.DY != 0 || g.Down != 1 {
		t.Fatalf("after lift: %+v", g)
	}
	// The survivor pans normally from its own position.
	g.Feed([]TouchEvent{{ID: 1, Phase: TouchMove, X: 90, Y: 110}})
	if !near(g.DX, 10) || !near(g.DY, 10) {
		t.Fatalf("survivor pan: %v,%v", g.DX, g.DY)
	}
}

func TestGesturesSecondFingerNoJump(t *testing.T) {
	var g Gestures
	g.Feed([]TouchEvent{{ID: 1, Phase: TouchBegin, X: 100, Y: 100}})
	g.Feed([]TouchEvent{{ID: 1, Phase: TouchMove, X: 120, Y: 100}})
	// Second finger lands far away: centroid jumps, but DX/DY must not.
	g.Feed([]TouchEvent{{ID: 2, Phase: TouchBegin, X: 300, Y: 300}})
	if g.DX != 0 || g.DY != 0 {
		t.Fatalf("centroid jump leaked into pan: %v,%v", g.DX, g.DY)
	}
	if !g.Pinching {
		t.Fatal("two fingers down but not pinching")
	}
}

func TestGesturesCancel(t *testing.T) {
	var g Gestures
	g.Feed([]TouchEvent{
		{ID: 1, Phase: TouchBegin, X: 0, Y: 0},
		{ID: 2, Phase: TouchBegin, X: 10, Y: 0},
	})
	g.Feed([]TouchEvent{
		{ID: 1, Phase: TouchCancel},
		{ID: 2, Phase: TouchCancel},
	})
	if g.Down != 0 || g.Pinching || !near(g.Scale, 1) {
		t.Fatalf("after cancel: %+v", g)
	}
}

// Two fingers turning a quarter turn clockwise: Angle follows, and the pair
// stays the same however the map orders them.
func TestGesturesRotation(t *testing.T) {
	var g Gestures
	g.Feed([]TouchEvent{{ID: 7, Phase: TouchBegin, X: 100, Y: 100}, {ID: 3, Phase: TouchBegin, X: 200, Y: 100}})
	for i := 1; i <= 9; i++ { // 10° a step around the centre (150,100)
		a := float64(i) * 10 * math.Pi / 180
		g.Feed([]TouchEvent{
			{ID: 3, Phase: TouchMove, X: 150 + 50*float32(math.Cos(a)), Y: 100 + 50*float32(math.Sin(a))},
			{ID: 7, Phase: TouchMove, X: 150 - 50*float32(math.Cos(a)), Y: 100 - 50*float32(math.Sin(a))},
		})
		if math.Abs(float64(g.AngleD)-10) > 0.01 {
			t.Fatalf("step %d: AngleD = %v, want 10", i, g.AngleD)
		}
	}
	if math.Abs(float64(g.Angle)-90) > 0.01 || math.Abs(float64(g.Scale)-1) > 1e-4 {
		t.Errorf("Angle %v Scale %v, want 90, 1", g.Angle, g.Scale)
	}
}
