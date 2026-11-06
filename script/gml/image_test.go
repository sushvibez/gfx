package gml

import "testing"

// Image.fillMode resolves to the enum's NUMBER (PreserveAspectCrop is 2),
// so the value has to be read numerically: reading it as a name left every
// cropped photo letterboxed inside its frame.
func TestFillModeFlagsReadsTheEnumNumber(t *testing.T) {
	cases := []struct {
		v             any
		stretch, crop bool
	}{
		{float64(0), true, false},  // Image.Stretch
		{float64(1), false, false}, // PreserveAspectFit (the default)
		{float64(2), false, true},  // PreserveAspectCrop
		{float64(3), false, false}, // Tile — not modeled, fits
		{"PreserveAspectCrop", false, true},
		{"Image.Stretch", true, false},
	}
	for _, c := range cases {
		st, cr := fillModeFlags(c.v)
		if st != c.stretch || cr != c.crop {
			t.Errorf("fillModeFlags(%v) = stretch %v crop %v, want %v %v",
				c.v, st, cr, c.stretch, c.crop)
		}
	}
}
