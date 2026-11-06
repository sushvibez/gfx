package audio

import "time"

// nullBackend discards audio but paces writes in real time, so voices
// advance and finish exactly as they would audibly — the no-device
// fallback and the test backend.
type nullBackend struct {
	next time.Time
}

func newNullBackend() backend { return &nullBackend{next: time.Now()} }

func (n *nullBackend) write(buf []int16) error {
	n.next = n.next.Add(time.Duration(len(buf)/2) * time.Second / SampleRate)
	if d := time.Until(n.next); d > 0 {
		time.Sleep(d)
	}
	return nil
}

func (n *nullBackend) close() {}
