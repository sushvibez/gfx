package render

import "testing"

// stroke-dasharray reaches the shape, in either separator style, and
// "none" clears an inherited one.
func TestSVGStrokeDashArray(t *testing.T) {
	doc := `<svg width="100" height="100">
	  <g stroke-dasharray="4, 2" stroke-dashoffset="1">
	    <path d="M0 0 L100 0" stroke="#fff"/>
	    <path d="M0 10 L100 10" stroke="#fff" stroke-dasharray="6 3"/>
	    <path d="M0 20 L100 20" stroke="#fff" stroke-dasharray="none"/>
	  </g>
	</svg>`
	s, err := ParseSVG([]byte(doc))
	if err != nil {
		t.Fatalf("ParseSVG: %v", err)
	}
	if len(s.shapes) != 3 {
		t.Fatalf("got %d shapes, want 3", len(s.shapes))
	}
	if got := s.shapes[0].dash; len(got) != 2 || got[0] != 4 || got[1] != 2 {
		t.Errorf("inherited dash = %v, want [4 2]", got)
	}
	if got := s.shapes[0].dashOff; got != 1 {
		t.Errorf("dash offset = %v, want 1", got)
	}
	if got := s.shapes[1].dash; len(got) != 2 || got[0] != 6 || got[1] != 3 {
		t.Errorf("own dash = %v, want [6 3]", got)
	}
	if got := s.shapes[2].dash; got != nil {
		t.Errorf("dasharray none = %v, want no dashes", got)
	}
}
