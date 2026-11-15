package core

import "testing"

func TestSignalEmitOrder(t *testing.T) {
	var s Signal[int]
	var got []int
	s.Connect(func(v int) { got = append(got, v*1) })
	s.Connect(func(v int) { got = append(got, v*2) })
	s.Emit(3)
	if len(got) != 2 || got[0] != 3 || got[1] != 6 {
		t.Fatalf("got %v", got)
	}
}

func TestSignalDisconnect(t *testing.T) {
	var s Signal[int]
	n := 0
	c := s.Connect(func(int) { n++ })
	s.Emit(0)
	c.Disconnect()
	c.Disconnect() // idempotent
	s.Emit(0)
	if n != 1 {
		t.Fatalf("slot ran %d times, want 1", n)
	}
	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0", s.Len())
	}
}

func TestSignalDisconnectDuringEmit(t *testing.T) {
	var s Signal[int]
	n := 0
	var c1, c2 Connection
	c1 = s.Connect(func(int) { c2.Disconnect() }) // disconnect the *next* slot mid-emit
	c2 = s.Connect(func(int) { n++ })
	s.Emit(0)
	if n != 0 {
		t.Fatal("disconnected slot still ran")
	}
	c1.Disconnect()
	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0", s.Len())
	}
}

func TestSignalConnectDuringEmit(t *testing.T) {
	var s Signal[int]
	n := 0
	s.Connect(func(int) {
		if n == 0 {
			s.Connect(func(int) { n += 100 }) // must not run this emit
		}
		n++
	})
	s.Emit(0)
	if n != 1 {
		t.Fatalf("n = %d, want 1 (late connection ran early)", n)
	}
	s.Emit(0)
	if n != 102 {
		t.Fatalf("n = %d, want 102", n)
	}
}

// Disconnecting a slot twice while the signal emits counts it once: Len
// keeps reporting the slots that are still connected.
func TestSignalRepeatedDisconnectDuringEmit(t *testing.T) {
	var s Signal[int]
	var first Connection
	inside := -1
	first = s.Connect(func(int) {})
	s.Connect(func(int) {
		first.Disconnect()
		first.Disconnect()
		inside = s.Len()
	})
	s.Emit(0)
	if inside != 1 {
		t.Errorf("Len inside the emit = %d, want 1", inside)
	}
	if s.Len() != 1 {
		t.Errorf("Len after the emit = %d, want 1", s.Len())
	}
}

// The same holds through a nested emit, and a slot disconnected before its
// turn does not run.
func TestSignalDisconnectUpcomingSlotInNestedEmit(t *testing.T) {
	var s Signal[int]
	var last Connection
	ran, depth := 0, 0
	lens := []int{}
	s.Connect(func(int) {
		depth++
		if depth == 1 {
			s.Emit(0) // the nested emit disconnects below
		}
		depth--
	})
	s.Connect(func(int) {
		last.Disconnect()
		last.Disconnect()
		lens = append(lens, s.Len())
	})
	last = s.Connect(func(int) { ran++ })
	s.Emit(0)
	if ran != 0 {
		t.Errorf("a slot disconnected before its turn ran %d times", ran)
	}
	for _, n := range lens {
		if n != 2 {
			t.Errorf("Len during the emits = %v, want 2 each time", lens)
			break
		}
	}
	if s.Len() != 2 {
		t.Errorf("Len after the emits = %d, want 2", s.Len())
	}
}
