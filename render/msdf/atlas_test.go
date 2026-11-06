package msdf

import (
	"math"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

func testAtlas(t *testing.T) *Atlas {
	t.Helper()
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return NewAtlas(f)
}

const ascii = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789 .,:;!?-+/()[]{}"

// The sheet has to hold a working character set without the packer
// putting two glyphs on the same texels — the failure that shows up as
// one letter wearing a slice of another.
func TestAtlasPacksWithoutOverlap(t *testing.T) {
	a := testAtlas(t)
	type rect struct {
		r                    rune
		x0, y0, x1, y1, w, h int
	}
	var placed []rect
	for _, r := range ascii {
		g, ok := a.Ensure(r)
		if !ok {
			t.Fatalf("%q did not make it into the sheet", r)
		}
		if g.W == 0 {
			continue // a space: an advance and no tile
		}
		if g.X < 0 || g.Y < 0 || g.X+g.W > SheetDim || g.Y+g.H > SheetDim {
			t.Fatalf("%q lands outside the sheet: %d,%d %dx%d", r, g.X, g.Y, g.W, g.H)
		}
		placed = append(placed, rect{r, g.X, g.Y, g.X + g.W, g.Y + g.H, g.W, g.H})
	}
	for i := range placed {
		for j := i + 1; j < len(placed); j++ {
			a, b := placed[i], placed[j]
			if a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1 {
				t.Errorf("%q and %q overlap in the sheet", a.r, b.r)
			}
		}
	}
	// Asking again must not pack a second copy.
	before := len(a.Glyphs)
	for _, r := range ascii {
		a.Ensure(r)
	}
	if len(a.Glyphs) != before {
		t.Errorf("a second pass added %d more tiles", len(a.Glyphs)-before)
	}
	used := 0
	for _, g := range a.Glyphs {
		used += g.W * g.H
	}
	t.Logf("%d glyphs in one sheet, %.1f%% of a %d-square sheet, highest row %d",
		len(a.Glyphs), 100*float64(used)/(SheetDim*SheetDim), SheetDim, a.penY+a.rowH)
}

// What lands in the sheet must be exactly the field that was generated
// for it — the copy is a stride change from RGB to RGBA and it is the
// kind of loop that silently shears an image by a texel.
func TestAtlasTileMatchesTheField(t *testing.T) {
	a := testAtlas(t)
	for _, r := range "Age" {
		g, ok := a.Ensure(r)
		if !ok || g.W == 0 {
			t.Fatalf("%q has no tile", r)
		}
		s, ok := Load(a.font, &a.buf, r, fixed.I(TilePPEM), DefaultCornerCos)
		if !ok {
			t.Fatalf("%q has no outline", r)
		}
		f := GenerateTile(s, TilePad)
		if f.W != g.W || f.H != g.H {
			t.Fatalf("%q: tile %dx%d, field %dx%d", r, g.W, g.H, f.W, f.H)
		}
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				o := ((g.Y+y)*SheetDim + g.X + x) * 4
				s := (y*f.W + x) * 3
				if a.Pix[o] != f.Pix[s] || a.Pix[o+1] != f.Pix[s+1] || a.Pix[o+2] != f.Pix[s+2] {
					t.Fatalf("%q texel %d,%d: sheet %v, field %v",
						r, x, y, a.Pix[o:o+3], f.Pix[s:s+3])
				}
			}
		}
	}
}

// The em metrics are the contract with the draw path: multiply by the
// size in pixels and you get the quad. Check them against what the font
// itself reports at that size, since a tile generated once at 32 px/em
// is the only thing standing between one sheet and eight.
func TestAtlasMetricsScaleToEverySize(t *testing.T) {
	a := testAtlas(t)
	var buf sfnt.Buffer
	for _, px := range []float64{9, 14, 24, 48, 160} {
		ppem := fixed.Int26_6(px * 64)
		for _, r := range "Hamburgefonstiv" {
			g, ok := a.Ensure(r)
			if !ok {
				t.Fatalf("%q missing", r)
			}
			gi, err := a.font.GlyphIndex(&buf, r)
			if err != nil || gi == 0 {
				t.Fatalf("%q has no glyph", r)
			}
			adv, err := a.font.GlyphAdvance(&buf, gi, ppem, 0)
			if err != nil {
				t.Fatalf("advance: %v", err)
			}
			want := float64(adv) / 64
			got := float64(g.Advance) * px
			// A tenth of a pixel: the font's own advance is hinted to
			// the ppem it was asked at, ours is scaled from one.
			if math.Abs(got-want) > 0.1+want*0.01 {
				t.Errorf("%q at %gpx advances %.3f, the face says %.3f", r, px, got, want)
			}
		}
	}
	// And the tile must cover the outline at any size: the quad is the
	// tile, so a bounding box that drifts clips the glyph.
	g, _ := a.Ensure('g')
	s, _ := Load(a.font, &a.buf, 'g', fixed.I(TilePPEM), DefaultCornerCos)
	for _, px := range []float64{9, 48, 160} {
		k := px / TilePPEM
		if float64(g.OffX)*px > s.MinX*k || float64(g.OffY)*px > s.MinY*k {
			t.Errorf("at %gpx the tile starts inside the outline", px)
		}
		if (float64(g.OffX)+float64(g.EmW))*px < s.MaxX*k ||
			(float64(g.OffY)+float64(g.EmH))*px < s.MaxY*k {
			t.Errorf("at %gpx the tile ends inside the outline", px)
		}
	}
}

// The dirty band is what the renderer uploads. It has to cover every row
// written since the last upload and reset once taken, or a new letter
// draws as whatever was in that texture before it.
func TestAtlasDirtyBandCoversNewRows(t *testing.T) {
	a := testAtlas(t)
	for _, r := range "abc" {
		a.Ensure(r)
	}
	y0, y1 := a.Dirty()
	for _, r := range "abc" {
		g := a.Glyphs[r]
		if g.Y < y0 || g.Y+g.H > y1 {
			t.Errorf("%q occupies rows %d..%d, the band is %d..%d", r, g.Y, g.Y+g.H, y0, y1)
		}
	}
	if y0, y1 := a.Dirty(); y0 != 0 || y1 != 0 {
		t.Errorf("the band survived being taken: %d..%d", y0, y1)
	}
	for _, r := range "WW" {
		a.Ensure(r)
	}
	y0, y1 = a.Dirty()
	g := a.Glyphs['W']
	if g.Y < y0 || g.Y+g.H > y1 {
		t.Errorf("the second band %d..%d misses W at %d..%d", y0, y1, g.Y, g.Y+g.H)
	}
}

// Every texel the median calls "inside" has to actually be inside, to
// within the texel the antialiasing blurs anyway.
//
// It is not automatic. Beyond a convex corner the two edges meeting
// there are exactly equidistant and disagree by 180 degrees about which
// side the point is on, so whichever the search happened to reach first
// decided the sign of a whole wedge of empty padding. On a 'v' that put
// two specks of ink above the left tip and a spur off the right one,
// and they scaled up with the text.
func TestNoSpecksInThePadding(t *testing.T) {
	a := testAtlas(t)
	for _, r := range ascii {
		s, ok := Load(a.font, &a.buf, r, fixed.I(TilePPEM), DefaultCornerCos)
		if !ok {
			continue
		}
		f := GenerateTile(s, TilePad)
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				cr, cg, cb := f.At(x, y)
				if Median(cr, cg, cb) < 128 {
					continue
				}
				p := point{f.MinX + float64(x) + 0.5, f.MinY + float64(y) + 0.5}
				if _, d := sample(s, p); d < -1 {
					t.Errorf("%q: texel %d,%d reads as inside the glyph, and is %.2f texels outside it",
						r, x, y, -d)
				}
			}
		}
	}
}
