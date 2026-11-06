package ui

import "testing"

// TestParseFunctionalColors: the CSS functional forms canvas code writes.
// Before these parsed, `rgba(255,200,80,0.4)` fell through to the hex path
// and came back opaque BLACK — 256 uses across the QML corpus, every soft
// glow and scrim among them.
func TestParseFunctionalColors(t *testing.T) {
	near := func(a, b float32) bool { return a-b < 0.01 && b-a < 0.01 }
	cases := []struct {
		in         string
		r, g, b, a float32
	}{
		{"rgb(255,0,0)", 1, 0, 0, 1},
		{"rgba(255,200,80,0.40)", 1, 0.784, 0.314, 0.4},
		{"rgba(0, 0, 0, 0)", 0, 0, 0, 0},
		{"rgb(50%, 0%, 100%)", 0.5, 0, 1, 1},
		{"hsl(0,100%,50%)", 1, 0, 0, 1},
		{"hsl(120,100%,50%)", 0, 1, 0, 1},
		{"hsla(240,100%,50%,0.5)", 0, 0, 1, 0.5},
		// An alpha above 1 is the 0..255 spelling some code uses.
		{"rgba(255,255,255,128)", 1, 1, 1, 0.502},
	}
	for _, c := range cases {
		got, err := ParseColor(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if !near(got.R, c.r) || !near(got.G, c.g) || !near(got.B, c.b) || !near(got.A, c.a) {
			t.Errorf("%s = %v, want (%v,%v,%v,%v)", c.in, got, c.r, c.g, c.b, c.a)
		}
	}
	// The other forms still work, and a bad string is still an error.
	if c, err := ParseColor("#ff8000"); err != nil || !near(c.R, 1) || !near(c.G, 0.502) {
		t.Errorf("#ff8000 = %v, %v", c, err)
	}
	if _, err := ParseColor("rgb(notanumber, 0, 0)"); err == nil {
		t.Error("a malformed rgb() should be an error")
	}
}
