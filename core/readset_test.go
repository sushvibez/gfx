package core

import "testing"

// A read-set that read a bound property is stale as soon as the binding's
// sources change, before any flush: the next Get would return a new value.
func TestReadSetStaleOnInvalidatedBinding(t *testing.T) {
	var a, b Property[int]
	b.Bind(func() int { return a.Get() * 2 })
	reads := TrackReads(func() { _ = b.Get() })
	if reads.Stale() || reads.OwnStale() {
		t.Fatal("fresh read-set is stale")
	}
	a.Set(1)
	if !reads.Stale() {
		t.Error("not stale after the binding's source changed (the next b.Get() returns 2)")
	}
	if !reads.OwnStale() {
		t.Error("OwnStale false after the binding's source changed")
	}
	FlushBindings()
	if !reads.Stale() {
		t.Error("not stale after the flush stored the new value")
	}
}

// Invalidation is not change: once the binding has re-evaluated to the
// value it already had, the read-set is fresh again.
func TestReadSetFreshWhenDerivedValueIsUnchanged(t *testing.T) {
	var a Property[int]
	var b Property[bool]
	b.Bind(func() bool { return a.Get() > 10 })
	reads := TrackReads(func() { _ = b.Get() })
	a.Set(5) // b is dirty, and will come out false again
	if !reads.Stale() {
		t.Error("not stale while the binding is waiting to re-evaluate")
	}
	FlushBindings()
	if reads.Stale() {
		t.Error("stale after the binding re-evaluated to the same value")
	}
	if reads.OwnStale() {
		t.Error("OwnStale after the binding re-evaluated to the same value")
	}
}

// The same through a nested read-set, whose parent asks first and must
// not remember the answer past the flush.
func TestReadSetNestedStaleOnInvalidatedBinding(t *testing.T) {
	var a Property[int]
	var b Property[bool]
	b.Bind(func() bool { return a.Get() > 10 })
	var child *ReadSet
	parent := TrackReads(func() {
		child = TrackReads(func() { _ = b.Get() })
	})
	a.Set(5)
	if !parent.Stale() || !child.Stale() {
		t.Errorf("parent stale %v, child stale %v while the binding is dirty: want both", parent.Stale(), child.Stale())
	}
	if parent.OwnStale() {
		t.Error("parent's own reads stale: it read nothing itself")
	}
	FlushBindings()
	if parent.Stale() || child.Stale() {
		t.Errorf("parent stale %v, child stale %v after an unchanged re-evaluation: want neither", parent.Stale(), child.Stale())
	}
	a.Set(50)
	FlushBindings()
	if !parent.Stale() || !child.Stale() {
		t.Errorf("parent stale %v, child stale %v after the derived value changed: want both", parent.Stale(), child.Stale())
	}
}

// A property whose binding was replaced by a plain value has nothing
// pending: only the write itself makes its readers stale.
func TestReadSetIgnoresADroppedBinding(t *testing.T) {
	var a, b Property[int]
	b.Bind(func() int { return a.Get() })
	a.Set(1) // b's binding is dirty
	b.Set(7) // and dropped
	reads := TrackReads(func() { _ = b.Get() })
	a.Set(2)
	if reads.Stale() {
		t.Error("stale from a binding the property no longer has")
	}
}

// A binding that panics leaves the property system usable for a caller
// that recovers: the binding is not stuck "evaluating", and reads made
// afterwards are not taken for its dependencies.
func TestBindingPanicRestoresTracking(t *testing.T) {
	var src, other, out Property[int]
	fail := false
	out.Bind(func() int {
		v := src.Get()
		if fail {
			panic("binding failed")
		}
		return v + 1
	})
	fail = true
	src.Set(1)
	func() {
		defer func() { _ = recover() }()
		out.Get()
		t.Error("the panic did not reach the caller")
	}()
	if current != nil {
		t.Error("the panicking binding is still the one being evaluated")
		current = nil // keep the tests after this one meaningful
	}
	fail = false
	_ = other.Get() // outside any binding: nobody's dependency
	src.Set(2)
	if got := out.Get(); got != 3 {
		t.Errorf("out = %d after the binding could evaluate again, want 3", got)
	}
	if n := len(other.subs.bindings); n != 0 {
		t.Errorf("a read outside any binding became a dependency of %d bindings", n)
	}
}

// A panic inside a flush does not strand the bindings queued behind it.
func TestFlushPanicKeepsTheQueue(t *testing.T) {
	var src, bad, good Property[int]
	fail := false
	bad.Bind(func() int {
		if fail {
			panic("binding failed")
		}
		return src.Get()
	})
	good.Bind(func() int { return src.Get() * 10 })
	var seen []int
	good.Changed().Connect(func(v int) { seen = append(seen, v) })
	fail = true
	src.Set(1)
	func() {
		defer func() { _ = recover() }()
		FlushBindings()
		t.Error("the panic did not reach the caller")
	}()
	if current != nil {
		t.Error("the panicking binding is still the one being evaluated")
		current = nil
	}
	fail = false
	FlushBindings()
	if len(seen) != 1 || seen[0] != 10 {
		t.Errorf("the binding queued behind the panic emitted %v, want [10]", seen)
	}
	FlushBindings()
	if bad.binding.dirty {
		t.Error("the binding that panicked was never retried by a flush")
	}
}

// A panic inside a TrackReads scope closes the scope: later reads are not
// recorded into a set nobody holds, and an enclosing scope carries on.
func TestTrackReadsPanicRestoresTracker(t *testing.T) {
	var p, q Property[int]
	outer := TrackReads(func() {
		func() {
			defer func() { _ = recover() }()
			TrackReads(func() {
				_ = p.Get()
				panic("computation failed")
			})
		}()
		_ = q.Get()
	})
	if tracker != nil {
		t.Error("a read-set is still recording after its scope ended")
		tracker = nil
	}
	q.Set(1)
	if !outer.OwnStale() {
		t.Error("the outer scope lost the read it made after the inner panic")
	}
}

// One source write and the flush that re-evaluates its binding.
func BenchmarkBindingReevaluate(b *testing.B) {
	var src, out Property[int]
	out.Bind(func() int { return src.Get() + 1 })
	i := 0
	b.ReportAllocs()
	for b.Loop() {
		i++
		src.Set(i)
		FlushBindings()
	}
}

// A staleness check over 200 reads after a write to something else: the
// memo is void, so every dependency is looked at.
func BenchmarkReadSetStale(b *testing.B) {
	props := make([]Property[int], 200)
	for i := 1; i < len(props); i += 2 {
		props[i].Bind(func() int { return props[i-1].Get() })
	}
	rs := TrackReads(func() {
		for i := range props {
			_ = props[i].Get()
		}
	})
	var other Property[int]
	i := 0
	b.ReportAllocs()
	for b.Loop() {
		i++
		other.Set(i)
		if rs.Stale() {
			b.Fatal("stale")
		}
	}
}
