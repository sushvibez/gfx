package shell

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/app"
)

// The splash over the first frames: off without a hold or fade, opaque
// through Init and SplashMin (whichever ends later), then a smooth fade to
// nothing over SplashFade.
func TestSplashAlpha(t *testing.T) {
	t0 := time.Unix(1000, 0)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	var none *splash
	if a := none.alpha(t0); a != 0 {
		t.Errorf("no splash: %v", a)
	}
	if a := (&splash{shownAt: t0, initDone: at(10)}).alpha(at(20)); a != 0 {
		t.Errorf("no hold or fade: %v, want 0 (the first frame replaces it)", a)
	}
	o := Options{SplashMin: time.Second, SplashFade: 400 * time.Millisecond}
	s := &splash{o: o, shownAt: t0, inInit: true}
	if a := s.alpha(at(100)); a != 0 {
		t.Errorf("in Init: %v, want 0 (Init presents it itself)", a)
	}
	s.inInit, s.initDone = false, at(300)
	for _, c := range []struct {
		ms      int
		lo, hi  float32
		comment string
	}{
		{500, 1, 1, "held: SplashMin not over"},
		{999, 1, 1, "held"},
		{1200, 0.45, 0.55, "half way through the fade"},
		{1399, 0, 0.01, "nearly gone"},
		{1400, 0, 0, "gone"},
	} {
		if a := s.alpha(at(c.ms)); a < c.lo || a > c.hi {
			t.Errorf("%d ms (%s): %v, want %v…%v", c.ms, c.comment, a, c.lo, c.hi)
		}
	}
	// A slow Init holds it past SplashMin; the fade starts when Init ends.
	s.initDone = at(2000)
	if a := s.alpha(at(1500)); a != 1 {
		t.Errorf("slow Init, before it returned: %v", a)
	}
	if a := s.alpha(at(2200)); a < 0.45 || a > 0.55 {
		t.Errorf("slow Init, half way through the fade: %v", a)
	}
	s.finish()
	if a := s.alpha(at(2100)); a != 0 {
		t.Errorf("finished: %v", a)
	}
}

// The status line's default colour reads on the clear colour.
func TestSplashTextColor(t *testing.T) {
	if c := (Options{ClearHex: 0xe7eaed}).splashTextColor(); c.R > 0.5 {
		t.Errorf("light ground: %v, want dark text", c)
	}
	if c := (Options{ClearHex: 0x070708}).splashTextColor(); c.R < 0.5 {
		t.Errorf("dark ground: %v, want light text", c)
	}
	if c := (Options{ClearHex: 0x070708, SplashTextHex: 0xff0000}).splashTextColor(); c.R != 1 || c.G != 0 {
		t.Errorf("set: %v", c)
	}
}

// The splash window is centred by its physical size (logical × the
// monitor's scale): the 480×300 splash on a 1920×1080 monitor at 150 %
// is 720×450 physical, so its corner is at (600, 315); on a second monitor
// the monitor's origin carries over.
func TestSplashOrigin(t *testing.T) {
	for _, c := range []struct {
		m    app.Monitor
		x, y int
	}{
		{app.Monitor{Width: 1920, Height: 1080, Scale: 1}, 720, 390},
		{app.Monitor{Width: 1920, Height: 1080, Scale: 1.5}, 600, 315},
		{app.Monitor{Width: 3840, Height: 2160, Scale: 2}, 1440, 780},
		{app.Monitor{X: 1920, Width: 2560, Height: 1440, Scale: 1.25}, 1920 + 980, 532},
		{app.Monitor{Width: 1920, Height: 1080}, 720, 390}, // no scale reported
	} {
		x, y, ok := splashOrigin(c.m, 480, 300)
		if !ok || x != c.x || y != c.y {
			t.Errorf("%+v: (%d, %d), want (%d, %d)", c.m, x, y, c.x, c.y)
		}
	}
}
