package render

import (
	"os"
	"testing"

	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// TestGPOSKernSegoe sanity-checks the GPOS 'kern' reader against Segoe UI
// when available (Windows hosts); GPOS values must be far smaller than the
// font's legacy kern table, which over-tucks pairs like "To" (~-12u/64 ≈
// -3px at 16px) that DirectWrite never applies.
func TestGPOSKernSegoe(t *testing.T) {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	ttf, err := os.ReadFile(root + `\Fonts\segoeui.ttf`)
	if err != nil {
		t.Skip("segoeui.ttf not available:", err)
	}
	k := parseGPOSKern(ttf)
	if k == nil {
		t.Fatal("expected GPOS kern feature in Segoe UI")
	}
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	gid := func(r rune) int {
		g, err := parsed.GlyphIndex(&buf, r)
		if err != nil || g == 0 {
			t.Fatalf("no glyph for %c", r)
		}
		return int(g)
	}
	upem := float32(parsed.UnitsPerEm())
	for _, p := range []struct {
		a, b     rune
		lo, hi   float32 // sane px range at 16px (negative = tuck)
		wantKern bool
	}{
		{'T', 'o', -2, 0, true},
		{'A', 'V', -2, 0, true},
		{'o', 'o', -0.5, 0.5, false}, // round pair: no kern expected
	} {
		v := float32(k.Pair(gid(p.a), gid(p.b))) * 16 / upem
		t.Logf("GPOS kern %c%c = %.3fpx @16px", p.a, p.b, v)
		if v < p.lo || v > p.hi {
			t.Errorf("kern %c%c = %.3fpx, want within [%.1f, %.1f]", p.a, p.b, v, p.lo, p.hi)
		}
		if p.wantKern && v == 0 {
			t.Errorf("kern %c%c = 0, expected a nonzero GPOS adjustment", p.a, p.b)
		}
	}
}
