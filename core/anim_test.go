package core

import (
	"testing"
	"time"
)

func TestTweenReachesTarget(t *testing.T) {
	var p Property[float32]
	Tween(&p, 100, 100*time.Millisecond, EaseLinear)
	if !AnimationsRunning() {
		t.Fatal("tween not registered")
	}
	TickAnimations(50 * time.Millisecond)
	if got := p.Get(); got != 50 {
		t.Fatalf("midpoint = %v, want 50", got)
	}
	if still := TickAnimations(60 * time.Millisecond); still {
		t.Fatal("tween should have finished")
	}
	if got := p.Get(); got != 100 {
		t.Fatalf("end = %v, want 100", got)
	}
}

func TestTweenRetargetsFromCurrent(t *testing.T) {
	var p Property[float32]
	Tween(&p, 100, 100*time.Millisecond, EaseLinear)
	TickAnimations(50 * time.Millisecond) // p = 50
	Tween(&p, 0, 100*time.Millisecond, EaseLinear)
	TickAnimations(50 * time.Millisecond)
	if got := p.Get(); got != 25 {
		t.Fatalf("retarget midpoint = %v, want 25 (50 -> 0 halfway)", got)
	}
	TickAnimations(time.Second)
	if got := p.Get(); got != 0 {
		t.Fatalf("end = %v, want 0", got)
	}
}

func TestTweenIntAndStop(t *testing.T) {
	var p Property[int]
	Tween(&p, 10, 100*time.Millisecond, EaseLinear)
	TickAnimations(50 * time.Millisecond)
	if got := p.Get(); got != 5 {
		t.Fatalf("int midpoint = %v, want 5", got)
	}
	StopTween(&p)
	TickAnimations(time.Second)
	if got := p.Get(); got != 5 {
		t.Fatalf("value after stop = %v, want 5", got)
	}
	if AnimationsRunning() {
		t.Fatal("stop left a tween running")
	}
}

func TestSpringSettlesAtTarget(t *testing.T) {
	var p Property[float32]
	SpringTo(&p, 100)
	if !AnimationsRunning() {
		t.Fatal("spring not registered")
	}
	// Advance ~2s in 16ms steps; a 170/26 spring settles well within that.
	settled := false
	for i := 0; i < 200; i++ {
		if !TickAnimations(16 * time.Millisecond) {
			settled = true
			break
		}
	}
	if !settled {
		t.Fatal("spring never settled")
	}
	if got := p.Get(); got < 99.9 || got > 100.1 {
		t.Fatalf("settled at %v, want ~100", got)
	}
}

func TestSpringRetargetsKeepingVelocity(t *testing.T) {
	var p Property[float32]
	SpringTo(&p, 100)
	for i := 0; i < 5; i++ {
		TickAnimations(16 * time.Millisecond)
	}
	mid := p.Get()
	if mid <= 0 {
		t.Fatalf("spring didn't move: %v", mid)
	}
	// Retarget mid-flight; should not snap, and only one spring stays live.
	SpringTo(&p, 0)
	if len(springs) != 1 {
		t.Fatalf("retarget created %d springs, want 1", len(springs))
	}
	for i := 0; i < 300 && AnimationsRunning(); i++ {
		TickAnimations(16 * time.Millisecond)
	}
	if got := p.Get(); got < -0.1 || got > 0.1 {
		t.Fatalf("retargeted spring settled at %v, want ~0", got)
	}
}

func TestPingPongLoops(t *testing.T) {
	var p Property[float32]
	TweenLoop(&p, 10, 100*time.Millisecond, EaseLinear, PingPong)
	TickAnimations(50 * time.Millisecond) // halfway forward → ~5
	if got := p.Get(); got < 4 || got > 6 {
		t.Fatalf("ping-pong forward mid = %v, want ~5", got)
	}
	TickAnimations(50 * time.Millisecond) // reach 10, flip
	TickAnimations(50 * time.Millisecond) // halfway back → ~5
	if got := p.Get(); got < 4 || got > 6 {
		t.Fatalf("ping-pong reverse mid = %v, want ~5", got)
	}
	if !AnimationsRunning() {
		t.Fatal("ping-pong should keep running")
	}
	StopTween(&p)
	if AnimationsRunning() {
		t.Fatal("StopTween left it running")
	}
}

func TestTweenZeroDurationIsImmediate(t *testing.T) {
	var p Property[float64]
	Tween(&p, 42, 0, nil)
	if p.Get() != 42 || AnimationsRunning() {
		t.Fatalf("zero-duration tween: %v running=%v", p.Get(), AnimationsRunning())
	}
}

// QML names its easing curves one by one (`easing.type: Easing.OutBounce`),
// and every one of them has to be a real curve: with only four mapped, four
// differently-eased animations moved as one — which is exactly how a
// comparison against Qt catches it.
func TestEasingCurvesAreDistinct(t *testing.T) {
	curves := map[string]Easing{
		"Linear": EaseLinear, "InQuad": EaseInQuad, "OutQuad": EaseOutQuad,
		"InOutQuad": EaseInOutQuad, "InCubic": EaseInCubic, "OutCubic": EaseOutCubic,
		"InOutCubic": EaseInOutCubic, "InQuart": EaseInQuart, "OutQuart": EaseOutQuart,
		"InOutQuart": EaseInOutQuart, "InQuint": EaseInQuint, "OutQuint": EaseOutQuint,
		"InOutQuint": EaseInOutQuint, "InSine": EaseInSine, "OutSine": EaseOutSine,
		"InOutSine": EaseInOutSine, "InExpo": EaseInExpo, "OutExpo": EaseOutExpo,
		"InOutExpo": EaseInOutExpo, "InCirc": EaseInCirc, "OutCirc": EaseOutCirc,
		"InOutCirc": EaseInOutCirc, "InBack": EaseInBack, "OutBack": EaseOutBack,
		"InOutBack": EaseInOutBack, "InElastic": EaseInElastic,
		"OutElastic": EaseOutElastic, "InOutElastic": EaseInOutElastic,
		"InBounce": EaseInBounce, "OutBounce": EaseOutBounce,
		"InOutBounce": EaseInOutBounce,
	}
	// Every curve starts at 0, ends at 1, and none of them is another one.
	mid := map[string]float32{}
	for name, e := range curves {
		if got := e(0); got < -0.001 || got > 0.001 {
			t.Errorf("%s(0) = %v, want 0", name, got)
		}
		if got := e(1); got < 0.999 || got > 1.001 {
			t.Errorf("%s(1) = %v, want 1", name, got)
		}
		mid[name] = e(0.35)
	}
	// The families pull apart where it matters: an "In" curve is behind
	// linear at a third of the way, an "Out" curve ahead of it.
	for _, in := range []string{"InQuad", "InCubic", "InQuart", "InSine", "InExpo", "InCirc"} {
		if mid[in] >= mid["Linear"] {
			t.Errorf("%s is not slower than Linear early on (%v vs %v)", in, mid[in], mid["Linear"])
		}
	}
	for _, out := range []string{"OutQuad", "OutCubic", "OutQuart", "OutSine", "OutExpo", "OutCirc"} {
		if mid[out] <= mid["Linear"] {
			t.Errorf("%s is not faster than Linear early on (%v vs %v)", out, mid[out], mid["Linear"])
		}
	}
	// Back and Elastic overshoot their ends; Bounce never does.
	if EaseOutBack(0.85) <= 1 {
		t.Error("OutBack does not overshoot")
	}
	if EaseInBack(0.15) >= 0 {
		t.Error("InBack does not undershoot")
	}
	for i := 0; i <= 20; i++ {
		t0 := float32(i) / 20
		if v := EaseOutBounce(t0); v < -0.001 || v > 1.001 {
			t.Errorf("OutBounce(%v) = %v, outside [0,1]", t0, v)
		}
	}
}

// TweenAfter holds the value until its delay passes, then tweens from the
// value it has at that moment.
func TestTweenAfter(t *testing.T) {
	var p Property[float32]
	p.Set(10)
	TweenAfter(&p, 110, 200*time.Millisecond, 100*time.Millisecond, EaseLinear)
	TickAnimations(150 * time.Millisecond)
	if p.Get() != 10 {
		t.Errorf("value %v before the delay passed, want the held 10", p.Get())
	}
	p.Set(20)                              // something else moves it meanwhile: the tween starts from here
	TickAnimations(100 * time.Millisecond) // 250 ms: 50 ms into the tween
	if v := p.Get(); v < 60 || v > 70 {
		t.Errorf("value %v half way (from 20 to 110), want 65", v)
	}
	TickAnimations(100 * time.Millisecond)
	if p.Get() != 110 || AnimationsRunning() {
		t.Errorf("value %v running %v at the end, want 110 and done", p.Get(), AnimationsRunning())
	}
}
