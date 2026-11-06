package gml

import "testing"

// A binding can divide by a zero it read before the thing it divides by
// exists, and JS answers Infinity or NaN. Neither is a length: they poison
// every rect derived from them, and a layout handed one walks an infinite
// box — the corpus' blueprint viewer took 40 SECONDS a frame once its
// implicit size was real, laying out an Inf-wide item. Qt treats such a
// value as nothing, and so does this.
func TestNonFiniteGeometryIsNothing(t *testing.T) {
	env := NewEnv()
	for _, src := range []string{"1 / 0", "-1 / 0", "0 / 0"} {
		e, err := parseExprSrc(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		got, err := attrFloat(attr{name: "width", expr: e}, env)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if got != 0 {
			t.Errorf("%q gave %v, want 0", src, got)
		}
	}
	// A finite value still comes through.
	e, _ := parseExprSrc("12.5")
	if got, err := attrFloat(attr{name: "width", expr: e}, env); err != nil || got != 12.5 {
		t.Errorf("12.5 gave %v (%v), want 12.5", got, err)
	}
}
