package render

// Packing glyphs into a sheet.
//
// The naive shelf packer this replaces kept one open row and made it as
// tall as the tallest glyph that happened to land in it. Glyphs arrive
// in the order the text asks for them, so a row is a random mix of
// heights and every short glyph in it wastes the difference to the
// tallest — a cap-height 'H' next to a comma leaves most of a row empty
// above the comma, for the whole width of the sheet.
//
// This is the same idea with the rows sorted: a shelf's height is the
// requested height rounded up to a small quantum, and a glyph goes on a
// shelf of its own bucket. Short glyphs share short shelves. It is
// etagere's shape without etagere's free list, because nothing here
// ever frees a glyph — when a sheet fills, the font opens another one
// rather than reclaiming from this one.
type packer struct {
	w, h    int
	shelves []shelf
	top     int // first row below every shelf opened so far
}

// shelf is one row of the sheet: where it starts, how tall it is, and
// how far along it the next glyph goes.
type shelf struct {
	y, h, penX int
}

// shelfQuantum is how coarsely shelf heights are bucketed. Too fine and
// a sheet fills with half-used shelves of nearly equal height; too
// coarse and it is the old packer again. Four texels is under a tenth
// of a line at reading sizes.
const shelfQuantum = 4

// reset empties the sheet.
func (p *packer) reset(w, h int) {
	p.w, p.h, p.top = w, h, 0
	p.shelves = p.shelves[:0]
}

// alloc finds room for a w×h glyph, leaving atlasPad between it and its
// neighbours. ok is false when the sheet is full.
func (p *packer) alloc(w, h int) (x, y int, ok bool) {
	if w <= 0 || h <= 0 || w+atlasPad > p.w {
		return 0, 0, false
	}
	need := h + atlasPad
	if r := need % shelfQuantum; r != 0 {
		need += shelfQuantum - r
	}
	// A shelf of this glyph's own bucket, with room left. Not the
	// shortest shelf that would HOLD it: letting a short glyph settle
	// on a taller shelf fills the tall ones and leaves the short ones
	// empty, and measured that way a sheet holds 0–13 % more glyphs
	// against 17–31 % for an exact bucket.
	//
	// It does cost a few rows on a font that only ever draws a hundred
	// glyphs, since each bucket opens a shelf whether or not it fills.
	// That is three rows of a thousand, and the number worth measuring
	// is how many glyphs a sheet holds before it overflows, not how
	// tightly it packs a set that was never going to fill it.
	for i := range p.shelves {
		sh := &p.shelves[i]
		if sh.h == need && sh.penX+w+atlasPad <= p.w {
			x, y = sh.penX, sh.y
			sh.penX += w + atlasPad
			return x, y, true
		}
	}
	if p.top+need > p.h {
		return 0, 0, false
	}
	p.shelves = append(p.shelves, shelf{y: p.top, h: need, penX: w + atlasPad})
	x, y = 0, p.top
	p.top += need
	return x, y, true
}
