package core

import (
	"math"
	"time"

	"github.com/chewxy/math32"
)

// Property animation: Tween drives a property toward a target over time,
// re-evaluated by TickAnimations once per frame — the engine under QML-style
// Behavior animations. Starting a new tween on a property retargets it from
// its current value, so rapid updates glide instead of jumping.

// Easing maps linear progress (0..1) to eased progress.
type Easing func(t float32) float32

func EaseLinear(t float32) float32 { return t }

func EaseOutQuad(t float32) float32 { return t * (2 - t) }

func EaseInOutQuad(t float32) float32 {
	if t < 0.5 {
		return 2 * t * t
	}
	return -1 + (4-2*t)*t
}

func EaseOutCubic(t float32) float32 {
	u := t - 1
	return u*u*u + 1
}

// The rest of QML's easing curves (QEasingCurve), which markup names one by
// one — `easing.type: Easing.OutBounce`. Every one of them is a real curve
// with a distinct feel; collapsing the ones a UI toolkit lacks onto
// InOutQuad makes four differently-eased animations move as one, which is
// exactly how a comparison against Qt finds them.
func EaseInQuad(t float32) float32  { return t * t }
func EaseInCubic(t float32) float32 { return t * t * t }

func EaseInOutCubic(t float32) float32 {
	if t < 0.5 {
		return 4 * t * t * t
	}
	u := 2*t - 2
	return 0.5*u*u*u + 1
}

func EaseInQuart(t float32) float32 { return t * t * t * t }

func EaseOutQuart(t float32) float32 {
	u := t - 1
	return 1 - u*u*u*u
}

func EaseInOutQuart(t float32) float32 {
	if t < 0.5 {
		return 8 * t * t * t * t
	}
	u := t - 1
	return 1 - 8*u*u*u*u
}

func EaseInQuint(t float32) float32 { return t * t * t * t * t }

func EaseOutQuint(t float32) float32 {
	u := t - 1
	return 1 + u*u*u*u*u
}

func EaseInOutQuint(t float32) float32 {
	if t < 0.5 {
		return 16 * t * t * t * t * t
	}
	u := 2*t - 2
	return 0.5*u*u*u*u*u + 1
}

func EaseInSine(t float32) float32 {
	return 1 - float32(math.Cos(float64(t)*math.Pi/2))
}

func EaseOutSine(t float32) float32 {
	return float32(math.Sin(float64(t) * math.Pi / 2))
}

func EaseInOutSine(t float32) float32 {
	return -0.5 * (float32(math.Cos(math.Pi*float64(t))) - 1)
}

func EaseInExpo(t float32) float32 {
	if t <= 0 {
		return 0
	}
	return float32(math.Pow(2, 10*float64(t)-10))
}

func EaseOutExpo(t float32) float32 {
	if t >= 1 {
		return 1
	}
	return 1 - float32(math.Pow(2, -10*float64(t)))
}

func EaseInOutExpo(t float32) float32 {
	switch {
	case t <= 0:
		return 0
	case t >= 1:
		return 1
	case t < 0.5:
		return float32(math.Pow(2, 20*float64(t)-10)) / 2
	}
	return (2 - float32(math.Pow(2, -20*float64(t)+10))) / 2
}

func EaseInCirc(t float32) float32 {
	return 1 - float32(math.Sqrt(1-float64(t)*float64(t)))
}

func EaseOutCirc(t float32) float32 {
	u := float64(t) - 1
	return float32(math.Sqrt(1 - u*u))
}

func EaseInOutCirc(t float32) float32 {
	if t < 0.5 {
		return (1 - float32(math.Sqrt(1-4*float64(t)*float64(t)))) / 2
	}
	u := 2*float64(t) - 2
	return (float32(math.Sqrt(1-u*u)) + 1) / 2
}

// Back overshoots at one end (QEasingCurve's overshoot is 1.70158).
const easeBack = 1.70158

func EaseInBack(t float32) float32 { return t * t * ((easeBack+1)*t - easeBack) }

func EaseOutBack(t float32) float32 {
	u := t - 1
	return u*u*((easeBack+1)*u+easeBack) + 1
}

func EaseInOutBack(t float32) float32 {
	const c = easeBack * 1.525
	if t < 0.5 {
		u := 2 * t
		return (u * u * ((c+1)*u - c)) / 2
	}
	u := 2*t - 2
	return (u*u*((c+1)*u+c) + 2) / 2
}

// Elastic springs past its target and rings back (Qt's amplitude 1, period
// 0.3).
func EaseInElastic(t float32) float32 {
	switch {
	case t <= 0:
		return 0
	case t >= 1:
		return 1
	}
	const p = 0.3
	u := float64(t) - 1
	return -float32(math.Pow(2, 10*u) * math.Sin((u-p/4)*(2*math.Pi)/p))
}

func EaseOutElastic(t float32) float32 {
	switch {
	case t <= 0:
		return 0
	case t >= 1:
		return 1
	}
	const p = 0.3
	return float32(math.Pow(2, -10*float64(t))*math.Sin((float64(t)-p/4)*(2*math.Pi)/p)) + 1
}

func EaseInOutElastic(t float32) float32 {
	switch {
	case t <= 0:
		return 0
	case t >= 1:
		return 1
	}
	const p = 0.3 * 1.5
	u := 2*float64(t) - 1
	if t < 0.5 {
		return -0.5 * float32(math.Pow(2, 10*u)*math.Sin((u-p/4)*(2*math.Pi)/p))
	}
	return float32(math.Pow(2, -10*u)*math.Sin((u-p/4)*(2*math.Pi)/p))*0.5 + 1
}

// Bounce drops and rebounds, in Qt's four diminishing hops.
func EaseOutBounce(t float32) float32 {
	const n, d = 7.5625, 2.75
	switch {
	case t < 1/d:
		return n * t * t
	case t < 2/d:
		u := t - 1.5/d
		return n*u*u + 0.75
	case t < 2.5/d:
		u := t - 2.25/d
		return n*u*u + 0.9375
	}
	u := t - 2.625/d
	return n*u*u + 0.984375
}

func EaseInBounce(t float32) float32 { return 1 - EaseOutBounce(1-t) }

func EaseInOutBounce(t float32) float32 {
	if t < 0.5 {
		return (1 - EaseOutBounce(1-2*t)) / 2
	}
	return (1 + EaseOutBounce(2*t-1)) / 2
}

// Animatable covers the property types Tween can interpolate.
type Animatable interface {
	~int | ~float32 | ~float64
}

// LoopMode controls how a tween repeats.
type LoopMode int

const (
	Once        LoopMode = iota // run once, then stop
	LoopForever                 // restart from the beginning each cycle
	PingPong                    // alternate forward/backward each cycle
)

type tween struct {
	key     any // the property pointer; one active tween per property
	elapsed time.Duration
	dur     time.Duration
	mode    LoopMode
	rev     bool            // current ping-pong direction
	step    func(t float32) // writes the interpolated value for progress t
}

var tweens []*tween

// Tween animates p from its current value to target over d, replacing any
// tween or spring already driving p. A zero or negative duration sets the
// value immediately.
func Tween[T Animatable](p *Property[T], target T, d time.Duration, e Easing) {
	if e == nil {
		e = EaseInOutQuad
	}
	stopTween(p)
	stopSpring(p)
	if d <= 0 {
		p.Set(target)
		return
	}
	from := p.Get()
	tw := &tween{key: p, dur: d}
	tw.step = func(t float32) { p.Set(between(from, target, e(t))) }
	tweens = append(tweens, tw)
}

// TweenLoop animates p between its current value and target over d,
// repeating per mode (Loop restarts, PingPong reverses), replacing any
// tween or spring already driving p. Cancel with StopTween. Like Tween, a
// zero or negative duration sets the value immediately (there is no loop
// to run).
func TweenLoop[T Animatable](p *Property[T], target T, d time.Duration, e Easing, mode LoopMode) {
	if e == nil {
		e = EaseInOutQuad
	}
	stopTween(p)
	stopSpring(p)
	if d <= 0 {
		p.Set(target)
		return
	}
	from := p.Get()
	tw := &tween{key: p, dur: d, mode: mode}
	tw.step = func(t float32) { p.Set(between(from, target, e(t))) }
	tweens = append(tweens, tw)
}

// between is from + (to − from)·t in T: rounded to the nearest whole
// number for an integer type (truncating would hold an int tween 0→10 at
// 0 for the first tenth of its run), exact for a float.
func between[T Animatable](from, to T, t float32) T {
	v := float32(from) + (float32(to)-float32(from))*t
	if half := float32(0.5); T(half) == 0 {
		return T(math32.Round(v))
	}
	return T(v)
}

// StopTween cancels any running tween or spring on p, leaving its current
// value.
func StopTween[T Animatable](p *Property[T]) { stopTween(p); stopSpring(p) }

func stopTween(key any) {
	for i, tw := range tweens {
		if tw.key == key {
			tweens = append(tweens[:i], tweens[i+1:]...)
			return
		}
	}
}

// animClock is how far the frame loop has advanced animation time in total:
// wall-paced in a window, virtual under a headless capture (where a
// screenshot advances it in big steps to settle what is moving). Anything
// else that ticks from the frame loop — script/gml's Timers and its
// animations — reads it so it advances with the same clock the tweens do,
// instead of a wall clock that stands still between two headless frames.
var animClock time.Duration

// AnimClock reports the total animation time the frame loop has advanced.
// Read it as a monotonic clock and use the difference between two frames.
func AnimClock() time.Duration { return animClock }

// virtualClock makes time flow ONLY through TickAnimations: a headless
// capture advances the clock in fixed slices, and anything that also
// consults the wall clock (a script engine's Timers) would otherwise add
// the process' own startup and GPU-init time on top, sampling an animated
// page later than the reference it is compared against.
var virtualClock bool

// SetVirtualClock switches time sources: true = the animation clock is the
// only clock (deterministic captures), false = wall time also counts.
func SetVirtualClock(on bool) { virtualClock = on }

// VirtualClock reports whether the animation clock is the only clock.
func VirtualClock() bool { return virtualClock }

// TickAnimations advances all running tweens and springs; the frame loop
// calls it once per frame before bindings flush. It reports whether any
// animation is still running (so loops can keep rendering while things
// move).
func TickAnimations(dt time.Duration) bool {
	animClock += dt
	live := tweens[:0]
	for _, tw := range tweens {
		tw.elapsed += dt
		if tw.elapsed < tw.dur {
			tw.step(tw.progress())
			live = append(live, tw)
			continue
		}
		switch tw.mode {
		case LoopForever:
			tw.elapsed -= tw.dur
			tw.step(tw.progress())
			live = append(live, tw)
		case PingPong:
			tw.elapsed -= tw.dur
			tw.rev = !tw.rev
			tw.step(tw.progress())
			live = append(live, tw)
		default:
			tw.step(1) // settle exactly on the target
		}
	}
	clear(tweens[len(live):])
	tweens = live
	return tickSprings(dt) || len(tweens) > 0
}

func (tw *tween) progress() float32 {
	t := float32(tw.elapsed) / float32(tw.dur)
	if tw.rev {
		return 1 - t
	}
	return t
}

// AnimationsRunning reports whether any tween or spring is active. It is
// about THIS package's animations — a caller asking "should the frame loop
// keep going" wants that or ExternalAnimating.
func AnimationsRunning() bool { return len(tweens) > 0 || len(springs) > 0 }

// ExternalAnimating reports whether a driver outside this package has said
// it is animating (see SetExternalAnimation).
func ExternalAnimating() bool { return len(externalAnim) > 0 }

// externalAnim holds the keys of animation drivers that live outside this
// package — a .gml document's animators, timers and particle systems,
// which write attribute overrides rather than Properties.
var externalAnim = map[any]bool{}

// SetExternalAnimation registers (or clears) an animation driver of its
// own. A frame loop that renders only on damage asks AnimationsRunning
// whether to render at all, so a driver it cannot see would freeze the
// moment it stopped producing input: the document's OWN clock only
// advances while it is being rendered, so the last frame it painted is
// the last frame for ever. The key identifies the driver (one document),
// so several can be live at once.
func SetExternalAnimation(key any, on bool) {
	if on {
		externalAnim[key] = true
		return
	}
	delete(externalAnim, key)
}

// TweenRunning reports whether an animation is currently driving p — for a
// control that follows an animated value while it moves and owns it again
// once it stops (a flicked wheel settling on an item).
func TweenRunning[T Animatable](p *Property[T]) bool {
	for _, tw := range tweens {
		if tw.key == any(p) {
			return true
		}
	}
	for _, sp := range springs {
		if sp.key == any(p) {
			return true
		}
	}
	return false
}

// Springs: physically-based motion toward a target (SwiftUI/GPUI feel).
// A spring keeps velocity, so retargeting mid-flight glides naturally
// rather than snapping. Defaults are a snappy, slightly-damped response.

type spring struct {
	key       any
	target    float32
	vel       float32
	stiffness float32
	damping   float32
	get       func() float32
	set       func(float32)
}

var springs []*spring

// SpringTo eases p toward target with a spring. Calling it again with a new
// target retargets in place, preserving velocity for a natural feel.
func SpringTo[T Animatable](p *Property[T], target T) {
	SpringConfig(p, target, 170, 26)
}

// SpringConfig is SpringTo with explicit stiffness and damping.
func SpringConfig[T Animatable](p *Property[T], target T, stiffness, damping float32) {
	for _, s := range springs {
		if s.key == p { // retarget the live spring, keep velocity
			s.target, s.stiffness, s.damping = float32(target), stiffness, damping
			return
		}
	}
	stopTween(p)
	s := &spring{
		key: p, target: float32(target), stiffness: stiffness, damping: damping,
		get: func() float32 { return float32(p.Get()) },
		set: func(v float32) { p.Set(T(v)) },
	}
	springs = append(springs, s)
}

func stopSpring(key any) {
	for i, s := range springs {
		if s.key == key {
			springs = append(springs[:i], springs[i+1:]...)
			return
		}
	}
}

func tickSprings(dt time.Duration) bool {
	h := float32(dt.Seconds())
	if h > 1.0/30 {
		h = 1.0 / 30 // clamp to keep the integrator stable on slow frames
	}
	live := springs[:0]
	for _, s := range springs {
		x := s.get()
		// Semi-implicit Euler on a damped spring.
		force := -s.stiffness*(x-s.target) - s.damping*s.vel
		s.vel += force * h
		x += s.vel * h
		if abs32(x-s.target) < 0.0005 && abs32(s.vel) < 0.0005 {
			s.set(s.target) // settled
			continue
		}
		s.set(x)
		live = append(live, s)
	}
	clear(springs[len(live):])
	springs = live
	return len(springs) > 0
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
