package render

import (
	"image"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Subpixel (LCD) text. Glyphs are rasterized at 3x horizontal resolution and
// reduced to per-channel (R,G,B) coverage stored in a second atlas. The
// subpixel text pipeline then blends each channel against the framebuffer via
// dual-source blending, the way GPUI does — giving the crisp, slightly
// color-fringed edges of LCD antialiasing. Only correct over opaque
// backgrounds (the per-channel mask blends against whatever is behind).

const subpixelScale = 3 // horizontal oversampling

// subpixelPhases is how many pre-shifted rasterisations of each glyph
// are kept, so the pen can stay on its fractional position instead of
// snapping to a whole pixel. The residual is half a phase — a
// thirty-second of a pixel at sixteen, an eighth at four, and uneven
// tracking at an eighth is visible next to browser text.
//
// Sixteen is what GPUI keeps, and it costs: sixteen rasterisations of
// every glyph, and sixteen times the sheet. A heading face fills one
// sheet on its own now, which is why the sheets are paged.
const subpixelPhases = 16

// ensureFace3 lazily builds a face at 3x the font size for LCD sampling.
func (f *Font) ensureFace3() bool {
	if f.face3 != nil {
		return true
	}
	if len(f.ttf) == 0 {
		return false
	}
	parsed, err := opentype.Parse(f.ttf)
	if err != nil {
		return false
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size: f.size * subpixelScale, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return false
	}
	f.face3 = face
	return true
}

// ensureSubpixel rasterizes a rune's LCD coverage into the subpixel atlas at
// one of subpixelPhases horizontal phases (subpixel POSITIONING: the pen
// keeps its fractional x and picks the matching pre-shifted raster, so glyph
// spacing doesn't quantize to whole pixels — uneven tracking was visibly
// "weak" next to browser text). Metrics come from the 1x face so layout
// matches the grayscale path. ok is false when the glyph can't be produced.
func (f *Font) ensureSubpixel(r rune, phase int) (glyph, bool) {
	phase &= subpixelPhases - 1
	key := uint64(r)<<4 | uint64(phase)
	if g, ok := f.sglyphs[key]; ok {
		return g, true
	}
	// 1x metrics (bearing/advance) drive layout, identical to grayscale text.
	g1, ok := f.ensure(r)
	if !ok {
		return glyph{}, false
	}
	if f.sglyphs == nil {
		f.sglyphs = map[uint64]glyph{}
	}
	if !f.ensureFace3() {
		return glyph{}, false
	}
	// One 1x pixel is subpixelScale raster pixels, so 64*3 in 26.6; one
	// phase is that over subpixelPhases.
	dot := fixed.Point26_6{X: fixed.Int26_6(phase) * (64 * subpixelScale / subpixelPhases)}
	dr, mask, maskp, _, ok := f.face3.Glyph(dot, r)
	if !ok {
		// Whitespace etc.: no coverage, but a valid (empty) glyph for advance.
		g := glyph{w: 0, h: 0, bearingX: g1.bearingX, bearingY: g1.bearingY, advance: g1.advance}
		f.sglyphs[key] = g
		return g, true
	}
	w3, h3 := dr.Dx(), dr.Dy()
	// Align the downsample grid to whole 1x pixels: the 3x bitmap's left
	// edge (dr.Min.X = 3x bearing + phase shift) is generally not a multiple
	// of 3, and snapping every glyph to the 1x-hinted bearing instead shifts
	// each by a different sub-pixel amount — a visible gap before '6' in
	// digit runs, where '6' hints differently from its neighbours. Extend
	// the sample window left to the previous multiple of 3 and keep the
	// raster's true bearing (alignedMin/3) on the glyph.
	alignedMin := dr.Min.X
	if m := ((alignedMin % subpixelScale) + subpixelScale) % subpixelScale; m != 0 {
		alignedMin -= m
	}
	lead := dr.Min.X - alignedMin // 0..2 empty lead-in columns
	// The same alignment vertically, and for a sharper reason.
	//
	// The 3x raster is oversampled in both directions, and its top edge is
	// no more a multiple of 3 than its left edge. Downsampling rows in
	// threes from wherever that edge falls, and then placing the result at
	// the 1x face's bearing, shifts every glyph vertically by whatever its
	// own top happened to be modulo 3 — a third of a pixel, or two thirds,
	// or none, decided per glyph and per size. In Inter at 13 px the
	// capitals and the ascenders come out a third of a pixel low and the
	// x-height letters two thirds; at 14 px it is the other way about. The
	// effect is a line of text whose letters sit at three different
	// heights, which is exactly what it looks like.
	alignedMinY := dr.Min.Y
	if m := ((alignedMinY % subpixelScale) + subpixelScale) % subpixelScale; m != 0 {
		alignedMinY -= m
	}
	leadY := dr.Min.Y - alignedMinY // 0..2 empty lead-in rows
	wout := (w3 + lead + subpixelScale - 1) / subpixelScale
	hout := (h3 + leadY + subpixelScale - 1) / subpixelScale
	if wout <= 0 || hout <= 0 {
		g := glyph{w: 0, h: 0, bearingX: g1.bearingX, bearingY: g1.bearingY, advance: g1.advance}
		f.sglyphs[key] = g
		return g, true
	}

	page, px, py, ok := sheetFor(f.r, &f.spages, newSubpixelSheet, wout, hout)
	if !ok {
		return glyph{}, false
	}
	sheet := f.spages[page]
	satlas := sheet.rgba()

	am, _ := mask.(*image.Alpha)
	// Sample columns are relative to alignedMin; columns in the lead-in (or
	// past the bitmap) read as empty coverage.
	at := func(x, y int) float32 {
		sx, sy := maskp.X+x-lead, maskp.Y+y-leadY
		if am != nil {
			return float32(am.AlphaAt(sx, sy).A)
		}
		_, _, _, a := mask.At(sx, sy).RGBA()
		return float32(a >> 8)
	}
	// Each output pixel maps to 3 horizontal source subpixels (R,G,B). A light
	// [1,2,1] horizontal filter reduces color fringing (FreeType's default LCD
	// filter, simplified); vertically we average the 3 oversampled rows.
	cover := func(subcol, outy int) float32 {
		var sum float32
		for dy := 0; dy < subpixelScale; dy++ {
			sy := subpixelScale*outy + dy
			l, c, rr := at(subcol-1, sy), at(subcol, sy), at(subcol+1, sy)
			sum += (l + 2*c + rr) / 4
		}
		return sum / subpixelScale
	}
	for y := 0; y < hout; y++ {
		for x := 0; x < wout; x++ {
			base := subpixelScale * x
			cr := cover(base+0, y)
			cg := cover(base+1, y)
			cb := cover(base+2, y)
			o := satlas.PixOffset(px+x, py+y)
			satlas.Pix[o+0] = uint8(min32(cr, 255))
			satlas.Pix[o+1] = uint8(min32(cg, 255))
			satlas.Pix[o+2] = uint8(min32(cb, 255))
			satlas.Pix[o+3] = uint8(min32((cr+cg+cb)/3, 255))
		}
	}

	g := glyph{
		page:     page,
		u0:       float32(px) / atlasSize,
		v0:       float32(py) / atlasSize,
		u1:       float32(px+wout) / atlasSize,
		v1:       float32(py+hout) / atlasSize,
		w:        float32(wout),
		h:        float32(hout),
		bearingX: float32(alignedMin / subpixelScale),  // exact: aligned to 3
		bearingY: float32(alignedMinY / subpixelScale), // and likewise
		advance:  g1.advance,
	}
	sheet.mark(py, py+hout)
	f.sglyphs[key] = g
	return g, true
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
