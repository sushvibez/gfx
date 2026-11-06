package core

// Signal is a typed multicast delegate: the signal half of Qt's
// signal/slot mechanism. Any func(T) is a slot.
//
// Signals follow the framework's single-owner threading model: Connect,
// Disconnect and Emit must all happen on the owning goroutine (the event
// loop). Use Loop.Post to emit from other goroutines — the equivalent of a
// Qt queued connection.
//
// The zero value is ready to use, so signals embed in structs with no
// constructor.
type Signal[T any] struct {
	slots    []slot[T]
	nextID   uint64
	emitting int
	dead     int // disconnected-while-emitting tombstones awaiting compaction
}

type slot[T any] struct {
	id uint64
	fn func(T)
}

// Connection identifies one Connect call so it can be undone.
type Connection struct {
	disconnect func()
}

// Disconnect removes the slot. Safe to call more than once, safe to call
// while the signal is emitting (the slot will not run again).
func (c Connection) Disconnect() {
	if c.disconnect != nil {
		c.disconnect()
	}
}

// Connect registers fn to run on every Emit, in connection order.
func (s *Signal[T]) Connect(fn func(T)) Connection {
	s.nextID++
	id := s.nextID
	s.slots = append(s.slots, slot[T]{id, fn})
	return Connection{disconnect: func() { s.remove(id) }}
}

func (s *Signal[T]) remove(id uint64) {
	for i := range s.slots {
		if s.slots[i].id == id {
			if s.emitting > 0 {
				s.slots[i].fn = nil // tombstone; compacted after emit
				s.dead++
			} else {
				s.slots = append(s.slots[:i], s.slots[i+1:]...)
			}
			return
		}
	}
}

// Emit calls every connected slot with v. Slots connected during an emit do
// not run until the next emit; slots disconnected during an emit are skipped.
func (s *Signal[T]) Emit(v T) {
	s.emitting++
	n := len(s.slots) // snapshot: late connections wait for the next emit
	for i := range n {
		if fn := s.slots[i].fn; fn != nil {
			fn(v)
		}
	}
	s.emitting--
	if s.emitting == 0 && s.dead > 0 {
		live := s.slots[:0]
		for _, sl := range s.slots {
			if sl.fn != nil {
				live = append(live, sl)
			}
		}
		clear(s.slots[len(live):])
		s.slots = live
		s.dead = 0
	}
}

// Len reports the number of connected slots.
func (s *Signal[T]) Len() int { return len(s.slots) - s.dead }
