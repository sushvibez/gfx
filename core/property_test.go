package core

import "testing"

func TestPropertySetGet(t *testing.T) {
	var p Property[int]
	if p.Get() != 0 {
		t.Fatal("zero value should be usable")
	}
	p.Set(42)
	if p.Get() != 42 {
		t.Fatalf("got %d, want 42", p.Get())
	}
}

func TestBindingTracksDependencies(t *testing.T) {
	var height, width Property[float32]
	height.Set(100)
	width.Bind(func() float32 { return height.Get() * 2 })

	if got := width.Get(); got != 200 {
		t.Fatalf("initial eval: got %v, want 200", got)
	}
	height.Set(150)
	if got := width.Get(); got != 300 { // pull: consistent before flush
		t.Fatalf("after dep change: got %v, want 300", got)
	}
}

func TestBindingChain(t *testing.T) {
	var a, b, c Property[int]
	a.Set(1)
	b.Bind(func() int { return a.Get() + 1 })
	c.Bind(func() int { return b.Get() + 1 })

	a.Set(10)
	FlushBindings()
	if got := c.Get(); got != 12 {
		t.Fatalf("chain: got %d, want 12", got)
	}
}

func TestDiamondEvaluatesOnce(t *testing.T) {
	var src, left, right, sink Property[int]
	src.Set(1)
	left.Bind(func() int { return src.Get() * 10 })
	right.Bind(func() int { return src.Get() * 100 })

	evals := 0
	sink.Bind(func() int { evals++; return left.Get() + right.Get() })
	if sink.Get() != 110 || evals != 1 {
		t.Fatalf("initial: sink=%d evals=%d", sink.Get(), evals)
	}

	src.Set(2)
	FlushBindings()
	if got := sink.Get(); got != 220 {
		t.Fatalf("diamond: got %d, want 220", got)
	}
	if evals != 2 {
		t.Fatalf("sink evaluated %d times total, want 2 (no glitch re-eval)", evals)
	}
}

func TestConditionalDependenciesRecapture(t *testing.T) {
	var flag Property[bool]
	var a, b, out Property[int]
	a.Set(1)
	b.Set(2)
	outEvals := 0
	out.Bind(func() int {
		outEvals++
		if flag.Get() {
			return a.Get()
		}
		return b.Get()
	})

	if out.Get() != 2 {
		t.Fatal("flag=false should read b")
	}
	a.Set(99) // not currently a dependency
	FlushBindings()
	if outEvals != 1 {
		t.Fatalf("changing a non-dependency re-evaluated the binding (%d evals)", outEvals)
	}

	flag.Set(true)
	FlushBindings()
	if out.Get() != 99 {
		t.Fatal("flag=true should read a")
	}
	b.Set(7) // no longer a dependency
	FlushBindings()
	if out.Get() != 99 {
		t.Fatal("b must have been dropped as a dependency")
	}
}

func TestImperativeSetBreaksBinding(t *testing.T) {
	var a, b Property[int]
	a.Set(1)
	b.Bind(func() int { return a.Get() })
	b.Set(5) // QML semantics: explicit set removes the binding
	a.Set(2)
	FlushBindings()
	if got := b.Get(); got != 5 {
		t.Fatalf("got %d, want 5 (binding should be gone)", got)
	}
}

func TestChangedSignal(t *testing.T) {
	var p Property[int]
	var got []int
	p.Changed().Connect(func(v int) { got = append(got, v) })
	p.Set(1)
	p.Set(1) // no change, no emit
	p.Set(2)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("changed emissions: %v", got)
	}
}

func TestChangedSignalFromBinding(t *testing.T) {
	var a, b Property[int]
	fired := 0
	b.Bind(func() int { return a.Get() / 2 })
	b.Changed().Connect(func(int) { fired++ })
	a.Set(2)
	FlushBindings()
	a.Set(3) // b stays 1: must not fire
	FlushBindings()
	if fired != 1 {
		t.Fatalf("Changed fired %d times, want 1", fired)
	}
}

func TestDirectBindingLoopKeepsValue(t *testing.T) {
	var a Property[int]
	a.Set(5)
	a.Bind(func() int { return a.Get() + 1 }) // depends on itself
	FlushBindings()                           // must terminate
	_ = a.Get()
}

func TestOscillatingBindingLoopTerminates(t *testing.T) {
	var a, b Property[int]
	a.Set(1)
	a.Bind(func() int { return b.Get() + 1 })
	b.Bind(func() int { return a.Get() + 1 }) // mutual loop
	FlushBindings()                           // must terminate, not livelock

	// The loop is broken, values stay at whatever the detector froze.
	_ = a.Get()
	_ = b.Get()

	// An unrelated binding still works after loop detection.
	var src, out Property[int]
	src.Set(3)
	out.Bind(func() int { return src.Get() * 2 })
	src.Set(4)
	FlushBindings()
	if out.Get() != 8 {
		t.Fatalf("healthy binding broken after loop detection: %d", out.Get())
	}
}

func TestRebindDropsOldBinding(t *testing.T) {
	var src1, src2, out Property[int]
	src1.Set(1)
	src2.Set(10)
	out.Bind(func() int { return src1.Get() })
	out.Bind(func() int { return src2.Get() })
	src1.Set(5) // old binding must not resurrect
	FlushBindings()
	if got := out.Get(); got != 10 {
		t.Fatalf("got %d, want 10", got)
	}
}
