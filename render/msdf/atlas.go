package msdf

import (
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// The atlas is generated at one size for every draw size, so that size
// is a compromise: large enough that a texel is small against the
// curvature of the outline, small enough that a family's glyphs fit one
// sheet. 32 px/em is msdfgen's usual recommendation and holds up from
// 8 px to a few hundred.
//
// The padding around each glyph is the distance range, and it is what
// limits how far an outline or a glow can reach before it runs out of
// field: 8 texels at 32 px/em is a quarter of an em either side of the
// outline. Four was not enough — a glow looked like a dark rim rather
// than a glow, because that was all the field it had.
const (
	TilePPEM = 32 // px per em the field is generated at
	TilePad  = 8  // texels of margin, and so the range in texels
	SheetDim = 1024
)

// Glyph is one packed tile: where it sits in the sheet, and where it
// sits relative to the pen, in ems.
//
// Everything outside the sheet rect is in em fractions rather than
// pixels, because that is the whole point of the atlas — the same entry
// draws at any size, by multiplying through by the size asked for.
type Glyph struct {
	X, Y, W, H           int     // texels in the sheet
	OffX, OffY, EmW, EmH float32 // tile rect relative to the pen, in ems
	Advance              float32 // ems
	U0, V0, U1, V1       float32 // the sheet rect, normalised
}

// Atlas is one sheet of distance fields for a font family: keyed by rune
// alone, with no size in the key, which is the difference from the
// rasterised atlas it replaces. A UI that draws the same text at eight
// sizes rasterises eight sets of glyphs today and one here.
type Atlas struct {
	Pix    []byte // RGBA, SheetDim square
	Glyphs map[rune]Glyph

	font *sfnt.Font
	buf  sfnt.Buffer
	upem float64

	penX, penY, rowH int
	dy0, dy1         int // the rows added since the last upload
	full             bool
}

// NewAtlas starts an empty sheet for a parsed font.
func NewAtlas(f *sfnt.Font) *Atlas {
	return &Atlas{
		Pix:    make([]byte, SheetDim*SheetDim*4),
		Glyphs: map[rune]Glyph{},
		font:   f,
		upem:   float64(f.UnitsPerEm()),
	}
}

// Ensure returns a rune's tile, generating and packing it on first ask.
// The second result is false for a rune the font has no outline for, and
// for anything once the sheet is full.
func (a *Atlas) Ensure(r rune) (Glyph, bool) {
	if g, ok := a.Glyphs[r]; ok {
		return g, true
	}
	if a.full {
		return Glyph{}, false
	}
	ppem := fixed.I(TilePPEM)
	gi, err := a.font.GlyphIndex(&a.buf, r)
	if err != nil || gi == 0 {
		return Glyph{}, false
	}
	adv, err := a.font.GlyphAdvance(&a.buf, gi, ppem, 0)
	if err != nil {
		return Glyph{}, false
	}
	g := Glyph{Advance: float32(adv) / 64 / TilePPEM}

	s, ok := Load(a.font, &a.buf, r, ppem, DefaultCornerCos)
	if ok {
		f := GenerateTile(s, TilePad)
		if f.W > 0 && !a.place(f, &g) {
			return Glyph{}, false
		}
	}
	// A space has an advance and no outline: an entry with a zero rect,
	// so the caller still gets its advance without a wasted tile.
	a.Glyphs[r] = g
	return g, true
}

// place packs a tile into the sheet and fills in its rects.
func (a *Atlas) place(f *Field, g *Glyph) bool {
	if a.penX+f.W+1 > SheetDim {
		a.penX = 0
		a.penY += a.rowH + 1
		a.rowH = 0
	}
	if a.penY+f.H+1 > SheetDim {
		a.full = true
		return false
	}
	for y := 0; y < f.H; y++ {
		dst := ((a.penY+y)*SheetDim + a.penX) * 4
		src := y * f.W * 3
		for x := 0; x < f.W; x++ {
			a.Pix[dst] = f.Pix[src]
			a.Pix[dst+1] = f.Pix[src+1]
			a.Pix[dst+2] = f.Pix[src+2]
			a.Pix[dst+3] = 255
			dst += 4
			src += 3
		}
	}
	if a.dy1 == 0 && a.dy0 == 0 {
		a.dy0 = a.penY
	}
	a.dy0 = min(a.dy0, a.penY)
	a.dy1 = max(a.dy1, a.penY+f.H)

	g.X, g.Y, g.W, g.H = a.penX, a.penY, f.W, f.H
	g.U0 = float32(a.penX) / SheetDim
	g.V0 = float32(a.penY) / SheetDim
	g.U1 = float32(a.penX+f.W) / SheetDim
	g.V1 = float32(a.penY+f.H) / SheetDim
	// The tile's glyph-space rect was generated at TilePPEM, so dividing
	// by it turns pixels into ems. Y is already down from the baseline,
	// the convention x/image hands outlines back in.
	g.OffX = float32(f.MinX / TilePPEM)
	g.OffY = float32(f.MinY / TilePPEM)
	g.EmW = float32(f.W) / TilePPEM
	g.EmH = float32(f.H) / TilePPEM

	a.penX += f.W + 1
	a.rowH = max(a.rowH, f.H)
	return true
}

// Dirty reports the band of rows added since it was last called, and
// clears it. A glyph atlas grows a row at a time and re-uploading a
// megabyte of sheet for one new letter costs more than the rest of a
// text-heavy frame put together.
func (a *Atlas) Dirty() (y0, y1 int) {
	y0, y1 = a.dy0, a.dy1
	a.dy0, a.dy1 = 0, 0
	return
}
