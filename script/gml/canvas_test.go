package gml

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// Qt's Context2D takes a BOUNDING BOX where the HTML canvas takes a centre
// and two radii: `ellipse(x, y, w, h)`. A page written HTML-style (the
// multiplane clouds pass seven arguments, which Qt ignores past the fourth)
// therefore draws to the lower right of the point it names, at a quarter of
// the size — ours centred it there and drew it twice as big, which turned a
// small cloud into a glow across the whole sky.
func TestCanvasEllipseTakesABoundingBox(t *testing.T) {
	m := newContext2D(nil, nil, ui.Rect{})
	begin, _ := m["beginPath"].(jsFunc)
	ellipse, _ := m["ellipse"].(jsFunc)
	style, _ := m["fillStyle"].(fillStyleRef)
	if begin == nil || ellipse == nil || style.c == nil {
		t.Fatal("the context is missing beginPath/ellipse")
	}
	if _, err := begin(nil); err != nil {
		t.Fatal(err)
	}
	// x=100 y=50 w=40 h=20 — a box, so the centre is (120,60), radii 20x10.
	if _, err := ellipse([]any{100.0, 50.0, 40.0, 20.0, 0.0, 0.0, 2 * math.Pi}); err != nil {
		t.Fatal(err)
	}
	var minX, minY, maxX, maxY float32
	first := true
	for _, sub := range style.c.path.Polyline() {
		for _, pt := range sub {
			if first {
				minX, minY, maxX, maxY = pt.X, pt.Y, pt.X, pt.Y
				first = false
				continue
			}
			minX, minY = min(minX, pt.X), min(minY, pt.Y)
			maxX, maxY = max(maxX, pt.X), max(maxY, pt.Y)
		}
	}
	if first {
		t.Fatal("the ellipse added no points")
	}
	for _, tc := range []struct {
		name      string
		got, want float32
	}{
		{"left", minX, 100}, {"top", minY, 50},
		{"right", maxX, 140}, {"bottom", maxY, 70},
	} {
		if d := tc.got - tc.want; d < -1.5 || d > 1.5 {
			t.Errorf("%s edge = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestCanvasFontSpecReadsWeightAndSlant: a canvas font spec is CSS-ish and
// the corpus states a weight in 29 places (`ctx.font = "bold 22px
// sans-serif"` — a clock's numerals, a chart's centre label). Only the size
// used to be read, so every one of them drew in the regular face.
func TestCanvasFontSpecReadsWeightAndSlant(t *testing.T) {
	cases := []struct {
		spec         string
		size         float64
		bold, italic bool
		family       string
	}{
		{"22px sans-serif", 22, false, false, ""},
		{"bold 22px sans-serif", 22, true, false, ""},
		{"italic 13px monospace", 13, false, true, "monospace"},
		{"bold italic 16px 'Quicksand'", 16, true, true, "Quicksand"},
		{"600 14px sans-serif", 14, true, false, ""},
		{"12px Menlo, monospace", 12, false, false, "Menlo"},
	}
	for _, c := range cases {
		if got := cssFontSize(c.spec); got != c.size {
			t.Errorf("%q: size %v, want %v", c.spec, got, c.size)
		}
		if got := cssFontBold(c.spec); got != c.bold {
			t.Errorf("%q: bold %v, want %v", c.spec, got, c.bold)
		}
		if got := cssFontItalic(c.spec); got != c.italic {
			t.Errorf("%q: italic %v, want %v", c.spec, got, c.italic)
		}
		if got := cssFontFamily(c.spec); got != c.family {
			t.Errorf("%q: family %q, want %q", c.spec, got, c.family)
		}
	}
}

// TestCanvasShadowStateIsRead: shadowColor/shadowBlur/shadowOffset* used to
// be accepted and dropped, so a page that glows its text or its curves
// (canvas, portal, cards2d5) drew them flat. The state feeds a blurred,
// offset copy underneath whatever is painted while it is set.
func TestCanvasShadowStateIsRead(t *testing.T) {
	c := &context2D{}
	if _, _, _, ok := c.shadowSpec(); ok {
		t.Error("a fresh context casts a shadow")
	}
	c.shadowCol, c.shadowBlur = "#00D1A9", 8
	col, blur, _, ok := c.shadowSpec()
	if !ok {
		t.Fatal("shadowColor + shadowBlur casts no shadow")
	}
	if blur != 8 {
		t.Errorf("blur = %v, want 8", blur)
	}
	if col.G < 0.5 || col.R > 0.2 {
		t.Errorf("shadow colour = %+v, want the teal it was set to", col)
	}
	// QML resets the shadow by zeroing the blur and the offsets.
	c.shadowBlur = 0
	if _, _, _, ok := c.shadowSpec(); ok {
		t.Error("a zero blur with no offset still casts a shadow")
	}
	c.shadowOffX = 2
	if _, _, _, ok := c.shadowSpec(); !ok {
		t.Error("an offset with no blur casts no shadow")
	}
}
