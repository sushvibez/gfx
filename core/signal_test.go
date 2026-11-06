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
