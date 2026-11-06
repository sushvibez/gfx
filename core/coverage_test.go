package core

import (
	"testing"
	"time"

	"github.com/chewxy/math32"
)

func approx(a, b float32) bool { return math32.Abs(a-b) < 1e-4 }

func nearV3(a, b Vec3) bool { return approx(a.X, b.X) && approx(a.Y, b.Y) && approx(a.Z, b.Z) }

// ---- animation -----------------------------------------------------------

// A tween takes a property over from a spring instead of fighting it.
func TestTweenStopsSpring(t *testing.T) {
	var p Property[float32]
	SpringTo(&p, 100)
	TickAnimations(16 * time.Millisecond)
	Tween(&p, 0, 100*time.Millisecond, EaseLinear)
	for i := 0; i < 20; i++ {
		TickAnimations(16 * time.Millisecond)
	}
	if got := p.Get(); got != 0 {
		t.Errorf("value %v, want the tween's 0 (a spring kept pulling)", got)
	}
	if AnimationsRunning() {
		t.Error("an animation is still running")
	}
}

// An integer tween rounds: 0→10 over 100 ms is 1 at 6 ms (0.6), not 0.
func TestTweenIntRounds(t *testing.T) {
	var p Property[int]
	Tween(&p, 10, 100*time.Millisecond, EaseLinear)
	TickAnimations(6 * time.Millisecond)
	if got := p.Get(); got != 1 {
		t.Errorf("int tween at 6%% = %d, want 1", got)
	}
	StopTween(&p)
	var n Property[int]
	n.Set(0)
	Tween(&n, -10, 100*time.Millisecond, EaseLinear)
	TickAnimations(6 * time.Millisecond)
	if got := n.Get(); got != -1 {
		t.Errorf("negative int tween at 6%% = %d, want -1", got)
	}
	StopTween(&n)
}

// TweenLoop with no duration sets the value and runs nothing, like Tween.
func TestTweenLoopZeroDuration(t *testing.T) {
	var p Property[float32]
	TweenLoop(&p, 5, 0, EaseLinear, LoopForever)
	if p.Get() != 5 || AnimationsRunning() {
		t.Errorf("value %v, running %v: want 5 and nothing running", p.Get(), AnimationsRunning())
	}
}

// A bare core.Loop advances animations on Step.
func TestLoopStepTicksAnimations(t *testing.T) {
	l := NewLoop()
	var p Property[float32]
	Tween(&p, 10, 100*time.Millisecond, EaseLinear)
	l.Step(50 * time.Millisecond)
	if got := p.Get(); !approx(got, 5) {
		t.Errorf("after Step(50ms) value %v, want 5", got)
	}
	StopTween(&p)
}

// ---- math ----------------------------------------------------------------

func TestVec2Vec4(t *testing.T) {
	if got := V2(1, -2).Neg(); got != V2(-1, 2) {
		t.Errorf("Vec2.Neg = %v", got)
	}
	a, b := V4(1, 2, 3, 4), V4(5, 6, 7, 8)
	if got := a.Mul(b); got != V4(5, 12, 21, 32) {
		t.Errorf("Vec4.Mul = %v", got)
	}
	if got := a.Neg(); got != V4(-1, -2, -3, -4) {
		t.Errorf("Vec4.Neg = %v", got)
	}
	if got := a.Lerp(b, 0.25); got != V4(2, 3, 4, 5) {
		t.Errorf("Vec4.Lerp = %v", got)
	}
	if got := V4(0, 3, 0, 4).Length(); !approx(got, 5) {
		t.Errorf("Vec4.Length = %v", got)
	}
	if got := V4(0, 3, 0, 4).Normalize(); !approx(got.Y, 0.6) || !approx(got.W, 0.8) {
		t.Errorf("Vec4.Normalize = %v", got)
	}
	if got := (Vec4{}).Normalize(); got != (Vec4{}) {
		t.Errorf("zero Vec4 normalizes to %v", got)
	}
	if got := V2(3, 4).Length(); !approx(got, 5) {
		t.Errorf("Vec2.Length = %v", got)
	}
}

// MulDir leaves translation out; TRS is translate·rotate·scale.
func TestMulDirAndTRS(t *testing.T) {
	r := QuatAxisAngle(V3(0, 0, 1), math32.Pi/2)
	m := Mat4TRS(V3(10, 0, 0), r, V3(2, 3, 4))
	want := Mat4Translate(V3(10, 0, 0)).Mul(r.Mat4()).Mul(Mat4Scale(V3(2, 3, 4)))
	for i := range m {
		if !approx(m[i], want[i]) {
			t.Fatalf("TRS[%d] = %v, want %v", i, m[i], want[i])
		}
	}
	// The x axis scaled by 2, turned a quarter about z, then moved.
	if got := m.MulPoint(V3(1, 0, 0)); !nearV3(got, V3(10, 2, 0)) {
		t.Errorf("TRS point = %v, want (10, 2, 0)", got)
	}
	if got := m.MulDir(V3(1, 0, 0)); !nearV3(got, V3(0, 2, 0)) {
		t.Errorf("TRS direction = %v, want (0, 2, 0) (no translation)", got)
	}
}

// ---- geometry ------------------------------------------------------------

func TestRayAABB(t *testing.T) {
	lo, hi := V3(-1, -1, -1), V3(1, 1, 1)
	if d, ok := RayAABB(V3(-5, 0, 0), V3(1, 0, 0), lo, hi); !ok || !approx(d, 4) {
		t.Errorf("hit = %v %v, want 4", d, ok)
	}
	if _, ok := RayAABB(V3(-5, 3, 0), V3(1, 0, 0), lo, hi); ok {
		t.Error("ray above the box hit it")
	}
	if _, ok := RayAABB(V3(-5, 0, 0), V3(-1, 0, 0), lo, hi); ok {
		t.Error("ray pointing away hit the box")
	}
	if d, ok := RayAABB(V3(0, 0, 0), V3(0, 1, 0), lo, hi); !ok || d != 0 {
		t.Errorf("from inside = %v %v, want 0 true", d, ok)
	}
}

func TestRayTriangle(t *testing.T) {
	a, b, c := V3(0, 0, 0), V3(1, 0, 0), V3(0, 1, 0)
	if d, ok := RayTriangle(V3(0.2, 0.2, 5), V3(0, 0, -1), a, b, c); !ok || !approx(d, 5) {
		t.Errorf("front hit = %v %v, want 5", d, ok)
	}
	if d, ok := RayTriangle(V3(0.2, 0.2, -5), V3(0, 0, 1), a, b, c); !ok || !approx(d, 5) {
		t.Errorf("back hit = %v %v, want 5 (both-sided)", d, ok)
	}
	if _, ok := RayTriangle(V3(0.8, 0.8, 5), V3(0, 0, -1), a, b, c); ok {
		t.Error("ray outside the triangle hit it")
	}
	if _, ok := RayTriangle(V3(0.2, 0.2, 5), V3(1, 0, 0), a, b, c); ok {
		t.Error("ray parallel to the triangle hit it")
	}
	if _, ok := RayTriangle(V3(0.2, 0.2, 5), V3(0, 0, 1), a, b, c); ok {
		t.Error("triangle behind the ray was hit")
	}
}

func TestRaySphere(t *testing.T) {
	if d, ok := RaySphere(V3(0, 0, -10), V3(0, 0, 1), V3(0, 0, 0), 2); !ok || !approx(d, 8) {
		t.Errorf("hit = %v %v, want 8", d, ok)
	}
	if d, ok := RaySphere(V3(0, 0, 0), V3(0, 0, 1), V3(0, 0, 0), 2); !ok || d != 0 {
		t.Errorf("from inside = %v %v, want 0 true", d, ok)
	}
	if _, ok := RaySphere(V3(0, 5, -10), V3(0, 0, 1), V3(0, 0, 0), 2); ok {
		t.Error("ray past the sphere hit it")
	}
	if _, ok := RaySphere(V3(0, 0, 10), V3(0, 0, 1), V3(0, 0, 0), 2); ok {
		t.Error("sphere behind the ray was hit")
	}
}

func TestClosestOnSegment(t *testing.T) {
	a, b := V3(0, 0, 0), V3(10, 0, 0)
	for _, c := range []struct{ p, want Vec3 }{
		{V3(4, 3, 0), V3(4, 0, 0)},
		{V3(-5, 1, 0), a},
		{V3(15, -1, 2), b},
	} {
		if got := ClosestOnSegment(c.p, a, b); !nearV3(got, c.want) {
			t.Errorf("closest to %v = %v, want %v", c.p, got, c.want)
		}
	}
	if got := ClosestOnSegment(V3(3, 3, 3), a, a); got != a {
		t.Errorf("degenerate segment = %v", got)
	}
}

func TestClosestOnTriangle(t *testing.T) {
	a, b, c := V3(0, 0, 0), V3(4, 0, 0), V3(0, 4, 0)
	for _, k := range []struct {
		name    string
		p, want Vec3
	}{
		{"face", V3(1, 1, 5), V3(1, 1, 0)},
		{"vertex a", V3(-2, -2, 1), a},
		{"vertex b", V3(6, -1, 0), b},
		{"vertex c", V3(-1, 6, 0), c},
		{"edge ab", V3(2, -3, 0), V3(2, 0, 0)},
		{"edge ac", V3(-3, 2, 0), V3(0, 2, 0)},
		{"edge bc", V3(3, 3, 1), V3(2, 2, 0)},
	} {
		if got := ClosestOnTriangle(k.p, a, b, c); !nearV3(got, k.want) {
			t.Errorf("%s: closest to %v = %v, want %v", k.name, k.p, got, k.want)
		}
	}
}

// ---- read tracking -------------------------------------------------------

// A read-set goes stale when something it read changes, and only then.
func TestTrackReadsStale(t *testing.T) {
	var a, b Property[int]
	rs := TrackReads(func() { _ = a.Get() })
	if rs.Stale() {
		t.Fatal("fresh read-set is stale")
	}
	b.Set(1)
	if rs.Stale() {
		t.Error("stale after a write to a property it never read")
	}
	a.Set(1)
	if !rs.Stale() {
		t.Error("not stale after a write to a property it read")
	}
}

// Nested scopes: a child's reads make the parent stale; the parent's own
// reads stay fresh (OwnStale), so just the child can be recomputed.
func TestTrackReadsNested(t *testing.T) {
	var own, inner Property[int]
	var child *ReadSet
	parent := TrackReads(func() {
		_ = own.Get()
		child = TrackReads(func() { _ = inner.Get() })
	})
	inner.Set(5)
	if !child.Stale() || !parent.Stale() {
		t.Fatalf("child stale %v, parent stale %v: want both", child.Stale(), parent.Stale())
	}
	if parent.OwnStale() {
		t.Error("parent's own reads reported stale")
	}
	// Recompute the child and swap it in: the parent is fresh again.
	fresh := TrackReads(func() { _ = inner.Get() })
	if !parent.ReplaceSub(child, fresh) {
		t.Fatal("ReplaceSub did not find the child")
	}
	if parent.Stale() {
		t.Error("parent still stale after the child was replaced")
	}
}

// A cache hit replays its reads into the enclosing scope; an untracked
// read poisons a set for good.
func TestReplayAndUntracked(t *testing.T) {
	var p Property[int]
	cached := TrackReads(func() { _ = p.Get() })
	outer := TrackReads(func() { ReplayReads(cached) })
	p.Set(9)
	if !outer.Stale() {
		t.Error("replayed reads did not reach the outer scope")
	}
	un := TrackReads(func() { MarkUntrackedRead() })
	if !un.Untracked() || !un.Stale() {
		t.Error("an untracked read did not make the set stale")
	}
}
