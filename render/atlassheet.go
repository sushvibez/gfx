package render

import (
	"image"

	"github.com/sushvibez/gfx/gpu"
)

// A font's glyphs live on sheets, and there can be more than one.
//
// One sheet used to be all there was, and a glyph that did not fit was
// dropped — the text simply had a hole in it, permanently, and the
// comment where that happened said multi-page atlases would come with
// the real batcher. They can come now: the batcher already breaks a
// batch when the bind group changes, so a second sheet costs one more
// draw call on the frames that use both and nothing on the frames that
// do not.
//
// This matters more since sub-pixel positioning went to sixteen phases:
// sixteen rasterisations of every glyph is sixteen times the sheet, and
// a heading face at forty points now fills one.
type atlasSheet struct {
	pix  []byte // atlasSize² × bpt
	bpt  int    // 1 for coverage, 4 for colour
	tex  *Image
	pack packer

	dirty    bool
	dy0, dy1 int // the band of rows written since the last upload
}

// newAtlasSheet allocates a sheet and its texture. bpt picks the
// format: one byte a texel for a coverage mask, four for colour.
func newAtlasSheet(r *Renderer2D, bpt int) *atlasSheet {
	s := &atlasSheet{pix: make([]byte, atlasSize*atlasSize*bpt), bpt: bpt}
	s.pack.reset(atlasSize, atlasSize)
	if bpt == 1 {
		s.tex = r.NewAlphaImage(atlasSize, atlasSize, s.pix)
	} else {
		s.tex = r.NewImage(atlasSize, atlasSize, s.pix)
	}
	return s
}

// newAlphaSheet and newColorSheet are the two ordinary sheets; the
// subpixel one needs a bind group of its own because the LCD pipeline
// takes the text-parameter uniform alongside the texture.
func newAlphaSheet(r *Renderer2D) *atlasSheet { return newAtlasSheet(r, 1) }
func newColorSheet(r *Renderer2D) *atlasSheet { return newAtlasSheet(r, 4) }

func newSubpixelSheet(r *Renderer2D) *atlasSheet {
	s := &atlasSheet{pix: make([]byte, atlasSize*atlasSize*4), bpt: 4}
	s.pack.reset(atlasSize, atlasSize)
	tex := r.device.CreateTexture2D(atlasSize, atlasSize, gpu.FormatRGBA8Unorm,
		gpu.TextureBinding|gpu.TextureCopyDst)
	view := tex.CreateView()
	bind := r.device.CreateBindGroup(r.subpixelLayout,
		gpu.BindGroupEntry{Binding: 0, Sampler: r.sampler},
		gpu.BindGroupEntry{Binding: 1, TextureView: view},
		gpu.BindGroupEntry{Binding: 2, Buffer: r.textParamsBuf},
	)
	s.tex = &Image{tex: tex, view: view, bind: bind, W: atlasSize, H: atlasSize}
	return s
}

// offset is the index into pix of a texel.
func (s *atlasSheet) offset(x, y int) int { return (y*atlasSize + x) * s.bpt }

// rgba wraps a colour sheet for the image/draw calls that fill it.
func (s *atlasSheet) rgba() *image.RGBA {
	return &image.RGBA{Pix: s.pix, Stride: atlasSize * 4, Rect: image.Rect(0, 0, atlasSize, atlasSize)}
}

// mark records that rows [y0, y1) changed.
func (s *atlasSheet) mark(y0, y1 int) {
	if !s.dirty {
		s.dy0, s.dy1, s.dirty = y0, y1, true
		return
	}
	s.dy0, s.dy1 = min(s.dy0, y0), max(s.dy1, y1)
}

// upload sends the changed band, which is usually a few rows. A sheet
// is a megabyte and a new letter is a few hundred texels.
func (s *atlasSheet) upload(r *Renderer2D) {
	if !s.dirty || s.tex == nil {
		return
	}
	row := atlasSize * s.bpt
	r.queue.WriteTextureRows(s.tex.tex, atlasSize, s.dy0, s.dy1-s.dy0, s.pix[s.dy0*row:s.dy1*row])
	s.dirty = false
}

func (s *atlasSheet) release() {
	if s.tex != nil {
		s.tex.Release()
		s.tex = nil
	}
}

// sheetFor finds room for a w×h glyph on one of the pages, opening a
// new one when every existing page is full. The page index goes in the
// glyph, so drawing knows which texture to bind.
func sheetFor(r *Renderer2D, pages *[]*atlasSheet, mk func(*Renderer2D) *atlasSheet, w, h int) (page, x, y int, ok bool) {
	for i, s := range *pages {
		if x, y, ok := s.pack.alloc(w, h); ok {
			return i, x, y, true
		}
	}
	// A glyph that will not fit an EMPTY sheet is not going to fit a
	// new one either, so refuse before opening one: a run of them would
	// otherwise take a megabyte apiece. Zero-sized rects are the same
	// case and the one that actually happened — a space has an advance
	// and no bitmap.
	if w <= 0 || h <= 0 || w+atlasPad > atlasSize || h+atlasPad > atlasSize {
		return 0, 0, 0, false
	}
	s := mk(r)
	*pages = append(*pages, s)
	x, y, ok = s.pack.alloc(w, h)
	return len(*pages) - 1, x, y, ok
}
