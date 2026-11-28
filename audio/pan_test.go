package audio

import "testing"

// A voice's pan is a balance: centred is unchanged, a side fades the far
// speaker and never boosts the near one; PlayMix starts the voice with its
// volume and pan already set.
func TestVoicePan(t *testing.T) {
	d := &Device{vol: 1}
	one := []float32{1, 1, 1, 1} // 2 frames of +1 both channels
	s := &Sound{dev: d, samples: one}
	for _, c := range []struct {
		vol, pan, l, r float32
	}{
		{1, 0, 1, 1},
		{1, 1, 0, 1},
		{1, -1, 1, 0},
		{1, 0.5, 0.5, 1},
		{0.5, -0.5, 0.5, 0.25},
		{1, 3, 0, 1}, // clamped
	} {
		v := s.PlayMix(c.vol, c.pan)
		mix := make([]float32, 4)
		v.mixInto(mix)
		if mix[0] != c.l || mix[1] != c.r || mix[2] != c.l || mix[3] != c.r {
			t.Errorf("vol %g pan %g: mix %v, want L %g R %g", c.vol, c.pan, mix, c.l, c.r)
		}
	}
	v := s.Play()
	v.SetPan(-1)
	mix := make([]float32, 4)
	v.mixInto(mix)
	if mix[0] != 1 || mix[1] != 0 {
		t.Errorf("SetPan(-1): mix %v", mix)
	}
}
