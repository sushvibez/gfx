package render

import (
	"math"
	"testing"
)

// run feeds n frames of ms milliseconds and returns the events seen.
func qrun(d *DynamicResolution, ms float32, seconds float32) (ev []DynResEvent) {
	for t := float32(0); t < seconds; t += ms / 1000 {
		if e := d.Update(ms / 1000); e != DynResHeld {
			ev = append(ev, e)
		}
	}
	return ev
}

func qnear(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func TestDynResShrinksToFloorThenDropsTier(t *testing.T) {
	var d DynamicResolution
	if d.Scale() != 1 {
		t.Fatalf("zero value scale %v, want 1", d.Scale())
	}
	// 25 ms: over 21.5 but under 1.5×, so one tenth per half second.
	qrun(&d, 25, 0.55)
	if !qnear(d.Scale(), 0.9) {
		t.Fatalf("after one slow interval scale %v, want 0.9", d.Scale())
	}
	if m := d.Mean(); m < 24 || m > 26 {
		t.Fatalf("mean %v, want 25", m)
	}
	ev := qrun(&d, 25, 5)
	if !qnear(d.Scale(), 0.6) {
		t.Fatalf("scale %v, want the floor 0.6", d.Scale())
	}
	drops := 0
	for _, e := range ev {
		if e == DynResDropTier {
			drops++
		}
	}
	if drops == 0 {
		t.Fatalf("no tier drop while slow at the floor: %v", ev)
	}
}

func TestDynResBigStepWhenFarOver(t *testing.T) {
	var d DynamicResolution
	qrun(&d, 40, 0.52)
	if !qnear(d.Scale(), 0.8) {
		t.Fatalf("scale %v after a 40 ms interval, want 0.8", d.Scale())
	}
}

func TestDynResHoldsInBand(t *testing.T) {
	var d DynamicResolution
	d.SetScale(0.8)
	if ev := qrun(&d, 19, 10); len(ev) != 0 || !qnear(d.Scale(), 0.8) {
		t.Fatalf("19 ms moved the scale: %v → %v", ev, d.Scale())
	}
}

func TestDynResGrowsAfterWaitAndBacksOff(t *testing.T) {
	var d DynamicResolution
	d.SetScale(0.7)
	// A vsync-paced frame with headroom: up a step every wait (2 s).
	qrun(&d, 16.7, 1.9)
	if !qnear(d.Scale(), 0.7) {
		t.Fatalf("grew before the wait: %v", d.Scale())
	}
	qrun(&d, 16.7, 0.7)
	if !qnear(d.Scale(), 0.8) {
		t.Fatalf("scale %v, want 0.8 after the wait", d.Scale())
	}
	// The raise does not hold: slow again at once. The next raise waits
	// twice as long.
	qrun(&d, 25, 0.6)
	if !qnear(d.Scale(), 0.7) {
		t.Fatalf("scale %v, want back to 0.7", d.Scale())
	}
	qrun(&d, 16.7, 3)
	if !qnear(d.Scale(), 0.7) {
		t.Fatalf("grew within the doubled wait: %v", d.Scale())
	}
	qrun(&d, 16.7, 1.6)
	if !qnear(d.Scale(), 0.8) {
		t.Fatalf("scale %v, want 0.8 after the doubled wait", d.Scale())
	}
	// Never past Ceil.
	qrun(&d, 16.7, 120)
	if !qnear(d.Scale(), 1) {
		t.Fatalf("scale %v, want 1", d.Scale())
	}
}

func TestDynResIgnoresHitches(t *testing.T) {
	var d DynamicResolution
	for i := 0; i < 20; i++ {
		if d.Update(0.5) != DynResHeld {
			t.Fatal("a hitch frame moved the scale")
		}
	}
	if d.Scale() != 1 {
		t.Fatalf("scale %v", d.Scale())
	}
}

func TestQualityDropsTier(t *testing.T) {
	q := NewQuality(QualityHigh)
	if !qnear(q.Dyn.Scale(), 1) || !qnear(q.Dyn.Floor, 0.6) {
		t.Fatalf("high starts at %v floor %v", q.Dyn.Scale(), q.Dyn.Floor)
	}
	dropped := false
	for i := 0; i < 600 && !dropped; i++ {
		dropped = q.Frame(0.030)
	}
	if !dropped || q.Tier != QualityMedium {
		t.Fatalf("tier %v after a slow run, want medium", q.Tier)
	}
	if !qnear(q.Dyn.Scale(), 0.7) {
		t.Fatalf("medium restarts at %v, want a step over its floor", q.Dyn.Scale())
	}
	// Never under Lowest.
	q.Lowest = QualityMedium
	for i := 0; i < 2000; i++ {
		q.Frame(0.030)
	}
	if q.Tier != QualityMedium {
		t.Fatalf("dropped under Lowest: %v", q.Tier)
	}
	// Off, nothing moves.
	q2 := NewQuality(QualityHigh)
	q2.Dynamic = false
	for i := 0; i < 600; i++ {
		q2.Frame(0.030)
	}
	if q2.Tier != QualityHigh || q2.Dyn.Scale() != 1 {
		t.Fatal("static quality moved")
	}
}

func TestQualityPixelCap(t *testing.T) {
	q := NewQuality(QualityHigh)
	if s := q.Scale(1600, 900); s != 1 {
		t.Fatalf("1600×900 on high: %v, want 1", s)
	}
	s := q.Scale(3840, 2160)
	if px := float32(3840*2160) * s * s; px > 2_300_001 || px < 2_200_000 {
		t.Fatalf("4K on high: scale %v gives %v px, want the 2.3 Mpx budget", s, px)
	}
	if CapScale(0.5, 3840, 2160, 0) != 0.5 {
		t.Fatal("no cap moved the scale")
	}
}

func TestQualityForAdapter(t *testing.T) {
	cases := []struct {
		name, goos string
		want       QualityTier
	}{
		{"NVIDIA GeForce RTX 5060 Ti (vulkan)", "linux", QualityHigh},
		{"AMD Radeon RX 7800 XT (vulkan)", "windows", QualityHigh},
		{"Intel(R) UHD Graphics 620 (vulkan)", "linux", QualityMedium},
		{"Intel(R) Arc(TM) A770 Graphics (vulkan)", "windows", QualityHigh},
		{"llvmpipe (LLVM 17.0.6, 256 bits) (vulkan)", "linux", QualityLow},
		{"Adreno (TM) 740 (vulkan)", "android", QualityLow},
		{"Mali-G78 (webgpu)", "js", QualityMedium},
		{" (, webgpu)", "js", QualityMedium},
		{"Apple M2 (metal)", "darwin", QualityHigh},
	}
	for _, c := range cases {
		if got := qualityFor(c.name, c.goos); got != c.want {
			t.Errorf("%q on %s: %v, want %v", c.name, c.goos, got, c.want)
		}
	}
}

func TestQualityBudgetApply(t *testing.T) {
	s := DefaultPostSettings()
	s.MotionBlur, s.TAA, s.LensFlare = 1, true, 0.3
	a := Atmosphere{Shafts: 0.01, Lamps: 1, Rain: 1}
	QualityLow.Budget().Apply(&s)
	QualityLow.Budget().ApplyAtmosphere(&a)
	if s.AO != 0 || s.Bloom != 0 || s.MotionBlur != 0 || s.TAA || s.LensFlare != 0 {
		t.Fatalf("low left effects on: %+v", s)
	}
	if a.Shafts != 0 || a.Lamps != 0 || !qnear(a.Rain, 0.4) {
		t.Fatalf("low air: %+v", a)
	}
	s = DefaultPostSettings()
	s.MotionBlur, s.TAA = 1, true
	QualityUltra.Budget().Apply(&s)
	if s.AO == 0 || s.MotionBlur != 1 || !s.TAA {
		t.Fatal("ultra switched something off")
	}
	for i, n := range []string{"low", "Medium", "HIGH", "ultra"} {
		if tier, ok := ParseQualityTier(n); !ok || tier != QualityTier(i) || tier.String() != qualityNames[i] {
			t.Errorf("parse %q: %v %v", n, tier, ok)
		}
	}
	if _, ok := ParseQualityTier("max"); ok {
		t.Error("parsed an unknown tier")
	}
}
