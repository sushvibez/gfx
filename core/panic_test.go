package core

import "testing"

// recovered runs fn and reports whether it panicked.
func recovered(fn func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	fn()
	return false
}

// A binding whose first evaluation panics stays bound and waiting: the
// next Get, or the next flush, tries it again.
func TestBindFirstEvaluationPanicIsRetried(t *testing.T) {
	for _, how := range []string{"get", "flush"} {
		var src, p Property[int]
		src.Set(41)
		p.Set(7)
		attempts := 0
		fn := func() int {
			v := src.Get() // a source read before the failure
			attempts++
			if attempts == 1 {
				panic("first evaluation")
			}
			return v + 1
		}
		if !recovered(func() { p.Bind(fn) }) {
			t.Fatalf("%s: the panic did not reach the caller", how)
		}
		if current != nil {
			current = nil
			t.Fatalf("%s: the failed binding is still being evaluated", how)
		}
		if how == "flush" {
			FlushBindings()
			if attempts != 2 {
				t.Fatalf("flush: %d attempts, want 2", attempts)
			}
		}
		if got := p.Get(); got != 42 {
			t.Fatalf("%s: got %d, want 42 (attempts %d)", how, got, attempts)
		}
		src.Set(50)
		if got := p.Get(); got != 51 {
			t.Fatalf("%s: the retried binding does not follow its source: %d", how, got)
		}
	}
}

// A result computed from a property is not fresh once that property is
// bound to something that has yet to produce a value, although nothing was
// written.
func TestBindFirstEvaluationPanicStalesReadSets(t *testing.T) {
	var p Property[int]
	p.Set(3)
	rs := TrackReads(func() { _ = p.Get() })
	if rs.Stale() {
		t.Fatal("stale before anything changed")
	}
	first := true
	recovered(func() {
		p.Bind(func() int {
			if first {
				first = false
				panic("first evaluation")
			}
			return 9
		})
	})
	if !rs.Stale() {
		t.Fatal("a read-set over a property waiting on its first evaluation reads as fresh")
	}
	if p.Get() != 9 || !rs.Stale() {
		t.Fatal("after the retry the value changed, and the set must say so")
	}
}

// Rebinding to a closure that fails keeps the old value and retries.
func TestRebindToAPanickingClosure(t *testing.T) {
	var p Property[int]
	p.Bind(func() int { return 1 })
	fail := true
	recovered(func() {
		p.Bind(func() int {
			if fail {
				panic("no")
			}
			return 2
		})
	})
	if !recovered(func() { p.Get() }) {
		t.Fatal("a still-failing binding did not fail again on Get")
	}
	fail = false
	if got := p.Get(); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
	FlushBindings() // the queue entries left behind are harmless
}

// A slot that panics does not leave the signal emitting.
func TestSignalSurvivesAPanickingSlot(t *testing.T) {
	var s Signal[int]
	var got []int
	var c2 Connection
	boom := true
	s.Connect(func(int) { c2.Disconnect() })
	c2 = s.Connect(func(v int) { got = append(got, -v) })
	s.Connect(func(int) {
		if boom {
			panic("slot")
		}
	})
	s.Connect(func(v int) { got = append(got, v) })
	if !recovered(func() { s.Emit(1) }) {
		t.Fatal("the panic did not reach the caller")
	}
	if s.emitting != 0 || s.dead != 0 || len(s.slots) != 3 {
		t.Fatalf("after a recovered panic: emitting %d, dead %d, %d slots", s.emitting, s.dead, len(s.slots))
	}
	boom = false
	c := s.Connect(func(int) {})
	c.Disconnect()
	if len(s.slots) != 3 {
		t.Fatalf("a disconnect outside an emit left %d slots", len(s.slots))
	}
	s.Emit(5)
	if len(got) != 1 || got[0] != 5 {
		t.Fatalf("got %v, want [5]", got)
	}
}

// The same through a nested emit: the depth comes back to zero.
func TestSignalNestedEmitWithAPanickingSlot(t *testing.T) {
	var outer, inner Signal[int]
	inner.Connect(func(int) { panic("inner slot") })
	var c Connection
	c = outer.Connect(func(v int) {
		c.Disconnect()
		inner.Emit(v)
	})
	if !recovered(func() { outer.Emit(1) }) {
		t.Fatal("the panic did not reach the caller")
	}
	if outer.emitting != 0 || inner.emitting != 0 || len(outer.slots) != 0 || outer.dead != 0 {
		t.Fatalf("outer emitting %d slots %d dead %d, inner emitting %d", outer.emitting, len(outer.slots), outer.dead, inner.emitting)
	}
}

// A posted callback that panics costs only itself: the ones behind it run
// in the next step, in order, ahead of what was posted meanwhile.
func TestLoopStepKeepsCallbacksBehindAPanic(t *testing.T) {
	l := NewLoop()
	var got []int
	l.Post(func() { got = append(got, 1) })
	l.Post(func() {
		l.Post(func() { got = append(got, 4) })
		panic("callback")
	})
	l.Post(func() { got = append(got, 2) })
	l.Post(func() { got = append(got, 3) })
	if !recovered(func() { l.Step(0) }) {
		t.Fatal("the panic did not reach the caller")
	}
	if len(got) != 1 {
		t.Fatalf("after the panic: %v", got)
	}
	l.Step(0)
	if len(got) != 4 || got[1] != 2 || got[2] != 3 || got[3] != 4 {
		t.Fatalf("got %v, want [1 2 3 4]", got)
	}
	l.Step(0)
	if len(got) != 4 {
		t.Fatalf("a callback ran twice: %v", got)
	}
}

// failingBind binds p to fn behind one failed first evaluation: the first
// call panics (after reading src when readFirst), later ones return fn().
func failingBind(p *Property[int], src *Property[int], readFirst bool, fn func() int) {
	first := true
	recovered(func() {
		p.Bind(func() int {
			if readFirst {
				_ = src.Get()
			}
			if first {
				first = false
				panic("first evaluation")
			}
			return fn()
		})
	})
}

// Bindings that read a property wait with it when its new binding fails
// its first evaluation: their next Get pulls the retry, with no flush, and
// read-sets over them are not fresh in the meantime. A chain and a diamond,
// an upstream that was plain or already bound, a failure before or after a
// source read.
func TestBindFirstEvaluationPanicReachesDependents(t *testing.T) {
	for _, wasBound := range []bool{false, true} {
		for _, readFirst := range []bool{false, true} {
			var src, p, left, right, sum Property[int]
			src.Set(1)
			if wasBound {
				p.Bind(func() int { return src.Get() })
			} else {
				p.Set(1)
			}
			left.Bind(func() int { return p.Get() * 2 })
			right.Bind(func() int { return p.Get() + 10 })
			sum.Bind(func() int { return left.Get() + right.Get() })
			var inner *ReadSet
			outer := TrackReads(func() {
				inner = TrackReads(func() { _ = sum.Get() })
			})
			direct := TrackReads(func() { _ = left.Get() })
			if outer.Stale() || inner.Stale() || direct.Stale() {
				t.Fatal("stale before anything changed")
			}
			changes := 0
			sum.Changed().Connect(func(int) { changes++ })

			failingBind(&p, &src, readFirst, func() int { return 3 })
			if !direct.Stale() || !inner.Stale() || !outer.Stale() {
				t.Fatalf("bound=%v read=%v: read-sets over dependents of a waiting property read as fresh (%v %v %v)",
					wasBound, readFirst, direct.Stale(), inner.Stale(), outer.Stale())
			}
			if got := sum.Get(); got != 19 { // 3*2 + 3+10
				t.Fatalf("bound=%v read=%v: sum = %d without a flush, want 19", wasBound, readFirst, got)
			}
			if changes != 1 {
				t.Fatalf("bound=%v read=%v: %d change notices, want 1", wasBound, readFirst, changes)
			}
			if !outer.Stale() || !direct.Stale() {
				t.Fatal("the values changed, and the sets must say so")
			}
			FlushBindings()
			if got := sum.Get(); got != 19 || changes != 1 {
				t.Fatalf("after the flush: sum %d, %d change notices", got, changes)
			}
		}
	}
}

// A retry that produces the value the property already had settles
// everything: dependents keep their values, nothing is announced, and the
// read-sets are fresh again.
func TestBindFirstEvaluationPanicRetryToSameValue(t *testing.T) {
	for _, how := range []string{"get", "flush"} {
		var src, p, dep Property[int]
		src.Set(1)
		p.Set(4)
		dep.Bind(func() int { return p.Get() * 2 })
		var inner *ReadSet
		outer := TrackReads(func() {
			inner = TrackReads(func() { _ = dep.Get() })
		})
		changes := 0
		dep.Changed().Connect(func(int) { changes++ })
		p.Changed().Connect(func(int) { changes++ })

		failingBind(&p, &src, true, func() int { return 4 })
		if !inner.Stale() || !outer.Stale() {
			t.Fatalf("%s: fresh while the retry is waiting", how)
		}
		if how == "flush" {
			FlushBindings()
		} else if got := dep.Get(); got != 8 {
			t.Fatalf("get: dep = %d, want 8", got)
		}
		if inner.Stale() || outer.Stale() {
			t.Fatalf("%s: still stale after the retry gave the same value", how)
		}
		if changes != 0 {
			t.Fatalf("%s: %d change notices for an unchanged value", how, changes)
		}
		if dep.Get() != 8 {
			t.Fatalf("%s: dep = %d, want 8", how, dep.Get())
		}
	}
}
