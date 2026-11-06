package render

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"strings"
	"sync"

	"github.com/chewxy/math32"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/sushvibez/gfx/gpu"
)

// Font rasterizes glyphs lazily into a shared atlas texture and draws text
// as batched textured quads. Glyph coverage is stored as white+alpha, so
// DrawText's color tints it through the standard pipeline — no extra shader.
// This is the glyph-atlas stage of DESIGN.md §9; shaping (ligatures,
// complex scripts) and SDF rendering come later.
type Font struct {
	r       *Renderer2D
	face    font.Face
	faceM   font.Face          // unhinted metrics face: fractional advances/kerning
	gposK   *gposKern          // GPOS 'kern' feature (nil: legacy kern table)
	upem    float32            // font units per em, for scaling GPOS values
	kcache  map[uint64]float32 // kern pair cache, key gid1<<32|gid2
	parsed  *opentype.Font     // retained for glyph-coverage (fallback) checks
	idx     sfnt.Buffer        // scratch buffer for GlyphIndex
	gidx    map[rune]uint16    // rune -> glyph id (0 = .notdef/absent), cached
	fbChain [][]byte           // fallback fonts, in order (this face lacks a rune)
	fb      *Font              // lazily-built first fallback at this font's size
	emoji   *emojiFont         // color-emoji fallback (CBDT PNGs), shared across sizes
	fam     *msdfFamily        // distance-field sheet, shared by every size of this face
	mglyphs map[rune]msdfGlyph // this size's tiles and advances (msdftext.go)
	ascent  float32
	height  float32
	// Qt's line metrics for the same face (see SetQtTextMetrics); 0 when
	// the font has no usable OS/2 table.
	winAscent float32
	winHeight float32
	// The glyph sheets: one byte a texel, because a glyph is a coverage
	// mask and the three bytes of white that RGBA also stored were
	// thrown away by every shader that sampled it. More than one when
	// the character set outgrows a sheet (atlassheet.go).
	pages  []*atlasSheet
	glyphs map[rune]glyph
	// Colour emoji are pixels, not coverage, so they keep sheets of
	// their own — allocated only if one is ever drawn.
	epages []*atlasSheet
	size   float64
	ttf    []byte            // retained so AtSize can derive other sizes
	sizes  map[float64]*Font // cache of derived sizes (kept on the base font)

	// Subpixel (LCD) text: a parallel atlas of per-channel RGB coverage,
	// rasterized at 3x horizontal resolution via face3. See subpixel.go.
	face3   font.Face
	spages  []*atlasSheet
	sglyphs map[uint64]glyph // key: rune<<4 | phase
}

type glyph struct {
	u0, v0, u1, v1     float32 // atlas uv
	w, h               float32 // quad size in pixels
	bearingX, bearingY float32 // offset from pen position (baseline)
	advance            float32
	color              bool // atlas holds real colors (emoji): draw untinted
	page               int  // which sheet of the font's atlas it is on
}

const atlasSize = 1024
const atlasPad = 1

// NewFont parses TTF/OTF bytes at a pixel size. Pass font bytes such as
// golang.org/x/image/font/gofont/goregular.TTF.
func (r *Renderer2D) NewFont(ttf []byte, sizePx float64) (*Font, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, fmt.Errorf("render: parsing font: %w", err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size: sizePx, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("render: creating face: %w", err)
	}
	// Layout metrics come from an UNHINTED face: hinting quantizes advances
	// and kerning to whole pixels, which mis-spaces kerned pairs ("To") and
	// wastes the quarter-pixel positioning of the subpixel text path.
	// DirectWrite/GPUI lay text out with fractional (natural) metrics the
	// same way; the hinted face above still shapes the raster bitmaps.
	faceM, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size: sizePx, DPI: 72, Hinting: font.HintingNone,
	})
	if err != nil {
		faceM = nil // fall back to hinted metrics
	}
	m := face.Metrics()
	winAsc, winDesc := winMetrics(ttf)
	f := &Font{
		r:      r,
		face:   face,
		faceM:  faceM,
		gposK:  parseGPOSKern(ttf),
		upem:   float32(parsed.UnitsPerEm()),
		parsed: parsed,
		ascent: fixedToFloat(m.Ascent),
		height: fixedToFloat(m.Height),
		glyphs: map[rune]glyph{},
		size:   sizePx,
		ttf:    ttf,
		fam:    &msdfFamily{},
	}
	// Qt-compatible line metrics, kept alongside the face's own: a line box
	// as tall as the font's OS/2 WINDOWS ascent+descent, rounded up. That
	// is what Qt reports as a Text item's implicitHeight, and text laid out
	// against a Qt reference lands several pixels off without it (7px at a
	// 32px heading in Quicksand).
	if upem := float32(parsed.UnitsPerEm()); upem > 0 && winAsc+winDesc > 0 {
		sc := float32(sizePx) / upem
		f.winAscent = math32.Ceil(float32(winAsc) * sc)
		f.winHeight = math32.Ceil(float32(winAsc+winDesc) * sc)
	}
	return f, nil
}

// winMetrics reads the ascent and descent Qt lays a line out with, in font
// units (0, 0 when unreadable). Which pair of numbers that is, is the
// face's own choice: OS/2 fsSelection bit 7 (USE_TYPO_METRICS) says "use my
// TYPOGRAPHIC ascender/descender", and FreeType — so Qt — honours it;
// otherwise the WINDOWS ascent/descent apply. The two differ a lot:
// Ubuntu Sans sets the flag and asks for 940/260 where its win metrics say
// 1020/223, which put our baseline 2px low and our line box 1px tall at
// 20px, on every unstyled label in the corpus. Quicksand does NOT set it,
// and needs the win metrics it was measured against (item 59).
func winMetrics(ttf []byte) (asc, desc int) {
	if len(ttf) < 12 {
		return 0, 0
	}
	n := int(binary.BigEndian.Uint16(ttf[4:6]))
	off := 12
	for i := 0; i < n && off+16 <= len(ttf); i++ {
		if string(ttf[off:off+4]) == "OS/2" {
			t := int(binary.BigEndian.Uint32(ttf[off+8 : off+12]))
			if t+78 > len(ttf) {
				return 0, 0
			}
			win := int(binary.BigEndian.Uint16(ttf[t+74 : t+76]))
			winD := int(binary.BigEndian.Uint16(ttf[t+76 : t+78]))
			// fsSelection bit 7: the face asks for its typo metrics.
			if fs := binary.BigEndian.Uint16(ttf[t+62 : t+64]); fs&0x80 != 0 {
				typoA := int(int16(binary.BigEndian.Uint16(ttf[t+68 : t+70])))
				typoD := int(int16(binary.BigEndian.Uint16(ttf[t+70 : t+72])))
				// The descender is signed and points DOWN; the line gap
				// (t+72) is leading, which Qt keeps out of the line box.
				if typoA > 0 && typoA-typoD > 0 {
					return typoA, -typoD
				}
			}
			return win, winD
		}
		off += 16
	}
	return 0, 0
}

// qtMetrics selects Qt's line metrics for every font in the process. The
// gml engine turns it on, since markup written against Qt is laid out
// against Qt's numbers; a UI built on the Go builders keeps the face's own
// metrics, which is what GPUI lays out with.
var qtMetrics bool

// SetQtTextMetrics chooses which line metrics Height and Ascent report: the
// face's own (false, the default) or Qt's (true). It applies to fonts
// already built, since it is read when the metrics are asked for.
func SetQtTextMetrics(on bool) { qtMetrics = on }

// AtSize returns this font's family at a different pixel size, creating and
// caching it on first use (the cache lives on the receiver, so call it on a
// long-lived base font). Returns the receiver when size is unchanged or the
// face can't be built.
func (f *Font) AtSize(sizePx float64) *Font {
	if sizePx <= 0 || sizePx == f.size || len(f.ttf) == 0 {
		return f
	}
	if g := f.sizes[sizePx]; g != nil {
		return g
	}
	g, err := f.r.NewFont(f.ttf, sizePx)
	if err != nil {
		return f
	}
	if f.sizes == nil {
		f.sizes = map[float64]*Font{}
	}
	g.fbChain = f.fbChain // derived sizes inherit the fallback chain
	g.emoji = f.emoji
	// One distance-field sheet for the whole family: a size is a
	// multiply in the vertex, so every size shares the same tiles.
	g.fam = f.fam
	f.sizes[sizePx] = g
	return g
}

// SetFallback registers fonts (TTF/OTF bytes) to draw runes this font's face
// doesn't cover — e.g. a CJK font behind a Latin UI font, and a symbol font
// behind both. They are tried in the order given; each face is created lazily
// at this font's size and propagates to sizes derived via AtSize.
func (f *Font) SetFallback(ttf ...[]byte) {
	chain := make([][]byte, 0, len(ttf))
	for _, b := range ttf {
		if len(b) > 0 {
			chain = append(chain, b)
		}
	}
	f.fbChain = chain
	f.fb = nil
	for _, g := range f.sizes {
		g.SetFallback(ttf...)
	}
}

// SetEmojiFallback registers a color-emoji font (CBDT/CBLC embedded PNGs,
// e.g. Noto Color Emoji) for runes no monochrome face in the chain covers.
// The parsed font is shared across derived sizes; each size rasterizes the
// PNGs into its own atlas scaled to its line height.
func (f *Font) SetEmojiFallback(data []byte) error {
	em, err := parseEmojiFont(data)
	if err != nil {
		return err
	}
	f.emoji = em
	for _, g := range f.sizes {
		g.emoji = em
	}
	return nil
}

// covers reports whether this font's face has a real glyph (not .notdef) for r.
func (f *Font) covers(r rune) bool {
	if f.parsed == nil {
		return true
	}
	return f.glyphID(r) != 0
}

// glyphID is the cmap lookup, memoized. sfnt walks the cmap format-4
// segments on every call behind its own per-call closure, and a page that
// measures and draws its text every frame pays for that thousands of times
// a second: the lookups were a fifth of a live frame on the buttons page.
// Absent glyphs cache as 0, so a miss costs the walk once too.
func (f *Font) glyphID(r rune) uint16 {
	if f.parsed == nil {
		return 0
	}
	if id, ok := f.gidx[r]; ok {
		return id
	}
	i, err := f.parsed.GlyphIndex(&f.idx, r)
	if err != nil {
		i = 0
	}
	if f.gidx == nil {
		f.gidx = make(map[rune]uint16, 64)
	}
	f.gidx[r] = uint16(i)
	return uint16(i)
}

// Covers reports whether this face has a glyph for r — which a PDF's
// subsetted font routinely does not, since its own cmap covers only the
// glyphs that document uses (or is absent entirely). A caller that has its
// own fallback can then choose a face per glyph.
func (f *Font) Covers(r rune) bool { return f.covers(r) }

// pick returns the first font in the fallback chain that covers r (this font
// if it does), defaulting to this font.
func (f *Font) pick(r rune) *Font {
	if f.covers(r) {
		return f
	}
	if len(f.fbChain) > 0 {
		if f.fb == nil {
			if nf, err := f.r.NewFont(f.fbChain[0], f.size); err == nil {
				nf.fbChain = f.fbChain[1:] // the rest of the chain hangs off it
				f.fb = nf
			}
		}
		if f.fb != nil {
			if g := f.fb.pick(r); g.covers(r) {
				return g
			}
		}
	}
	return f
}

func (f *Font) Release() {
	for _, s := range f.pages {
		s.release()
	}
	for _, s := range f.epages {
		s.release()
	}
	for _, s := range f.spages {
		s.release()
	}
	// The family sheet belongs to the face, not the size: only the font
	// that made it hands it back, and the derived sizes it was shared
	// with are being released alongside.
	for _, g := range f.sizes {
		g.fam = nil
	}
	f.fam.release()
	f.fam = nil
}

// Height returns the line height in pixels.
func (f *Font) Height() float32 {
	if qtMetrics && f.winHeight > 0 {
		return f.winHeight
	}
	return f.height
}

// Ascent returns the baseline offset from the top of a line.
func (f *Font) Ascent() float32 {
	if qtMetrics && f.winAscent > 0 {
		return f.winAscent
	}
	return f.ascent
}

// layoutAscent is where the baseline sits below the top of a line box: the
// face's own ascent, taken off the RASTER font so glyph positions stay on
// the physical pixel grid, or Qt's when the layout is Qt's (the line box is
// taller there, and the glyphs sit lower in it).
func (f *Font) layoutAscent(rf *Font, rs float32) float32 {
	if qtMetrics && f.winAscent > 0 {
		return f.winAscent
	}
	return rf.ascent / rs
}

// FaceHeight and FaceAscent are the face's own metrics whatever
// SetQtTextMetrics says — for drawing decisions that follow the glyphs
// (underline offsets, the raster baseline) rather than the line box.
func (f *Font) FaceHeight() float32 { return f.height }
func (f *Font) FaceAscent() float32 { return f.ascent }

func fixedToFloat(v fixed.Int26_6) float32 { return float32(v) / 64 }

// Size returns the font's pixel size (the em size layout line-heights are
// relative to; Height() is the face's natural line height, typically larger).
func (f *Font) Size() float64 { return f.size }

// ensure rasterizes a rune into the atlas on first use.
func (f *Font) ensure(r rune) (glyph, bool) {
	if g, ok := f.glyphs[r]; ok {
		return g, true
	}
	dr, mask, maskp, advance, ok := f.face.Glyph(fixed.Point26_6{}, r)
	if !ok {
		return glyph{}, false
	}
	adv := fixedToFloat(advance)
	if f.faceM != nil {
		if a, ok := f.faceM.GlyphAdvance(r); ok {
			adv = fixedToFloat(a) // fractional (unhinted) layout width
		}
	}
	w, h := dr.Dx(), dr.Dy()
	if w <= 0 || h <= 0 {
		// A space: an advance and no coverage. It still has to be
		// CACHED — asking the pager for a zero-sized rect finds no room
		// on any sheet, opens a new one, fails to place it there
		// either, and answers "no glyph", so the next space does it all
		// again. Sixty lines of text with nine spaces each came to five
		// hundred megabytes of empty sheets a frame.
		g := glyph{advance: adv}
		f.glyphs[r] = g
		return g, true
	}
	// A sheet is allocated by the first glyph that needs it rather than
	// when the Font is made: AtSize hands out a Font per pixel size, and
	// building sheets eagerly charged a megabyte to every derived size,
	// including ones that only ever draw through the distance-field path
	// and rasterise nothing. A full sheet opens another rather than
	// dropping the glyph, which used to leave a permanent hole.
	page, px, py, ok := sheetFor(f.r, &f.pages, newAlphaSheet, w, h)
	if !ok {
		return glyph{}, false
	}
	sheet := f.pages[page]
	// The mask's coverage, one byte a texel. The shader tints it; there
	// is no colour here to store.
	if am, ok := mask.(*image.Alpha); ok {
		for yy := range h {
			for xx := range w {
				sheet.pix[sheet.offset(px+xx, py+yy)] = am.AlphaAt(maskp.X+xx, maskp.Y+yy).A
			}
		}
	} else {
		alpha := &image.Alpha{Pix: sheet.pix, Stride: atlasSize,
			Rect: image.Rect(0, 0, atlasSize, atlasSize)}
		dst := image.Rect(px, py, px+w, py+h)
		draw.DrawMask(alpha, dst, image.Opaque, image.Point{}, mask, maskp, draw.Src)
	}
	g := glyph{
		page:     page,
		u0:       float32(px) / atlasSize,
		v0:       float32(py) / atlasSize,
		u1:       float32(px+w) / atlasSize,
		v1:       float32(py+h) / atlasSize,
		w:        float32(w),
		h:        float32(h),
		bearingX: float32(dr.Min.X),
		bearingY: float32(dr.Min.Y),
		advance:  adv,
	}
	sheet.mark(py, py+h)
	f.glyphs[r] = g
	return g, true
}

// ensureEmoji rasterizes a color-emoji rune into the atlas on first use: the
// strike PNG is decoded and scaled to sit on the baseline at ascent height.
func (f *Font) ensureEmoji(r rune) (glyph, bool) {
	if g, ok := f.glyphs[r]; ok {
		return g, g.color
	}
	data, ok := f.emoji.lookup(r)
	if !ok {
		return glyph{}, false
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return glyph{}, false
	}
	// Scale to a square box of the ascent (emoji sit on the baseline and
	// fill the cap area, like other renderers).
	box := int(f.ascent + 0.5)
	if box < 1 {
		return glyph{}, false
	}
	sb := src.Bounds()
	w := box * sb.Dx() / max(sb.Dy(), 1)
	h := box
	page, px, py, ok := sheetFor(f.r, &f.epages, newColorSheet, w, h)
	if !ok {
		return glyph{}, false
	}
	sheet := f.epages[page]
	erg := sheet.rgba()
	dst := image.Rect(px, py, px+w, py+h)
	xdraw.ApproxBiLinear.Scale(erg, dst, src, sb, xdraw.Src, nil)
	// The pipeline blends straight alpha; PNG decode gives straight alpha
	// already (image.NRGBA), but Scale writes premultiplied into RGBA —
	// unmultiply so the blend doesn't darken edges.
	for yy := py; yy < py+h; yy++ {
		for xx := px; xx < px+w; xx++ {
			o := erg.PixOffset(xx, yy)
			if a := erg.Pix[o+3]; a > 0 && a < 255 {
				erg.Pix[o+0] = uint8(int(erg.Pix[o+0]) * 255 / int(a))
				erg.Pix[o+1] = uint8(int(erg.Pix[o+1]) * 255 / int(a))
				erg.Pix[o+2] = uint8(int(erg.Pix[o+2]) * 255 / int(a))
			}
		}
	}
	g := glyph{
		page:     page,
		u0:       float32(px) / atlasSize,
		v0:       float32(py) / atlasSize,
		u1:       float32(px+w) / atlasSize,
		v1:       float32(py+h) / atlasSize,
		w:        float32(w),
		h:        float32(h),
		bearingX: 0,
		bearingY: -float32(h), // bottom on the baseline
		advance:  float32(w) + 1,
		color:    true,
	}
	sheet.mark(py, py+h)
	f.glyphs[r] = g
	return g, true
}

// kern returns the kerning adjustment between two runes in fractional
// pixels. Fonts with a GPOS 'kern' feature use it and IGNORE the legacy
// kern table, exactly like DirectWrite/HarfBuzz — the tables can disagree
// wildly (Segoe UI's legacy table over-tucks "To" by ~3px at 16px). Fonts
// without GPOS fall back to the legacy table via the unhinted metrics face.
func (f *Font) kern(a, b rune) float32 {
	if f.gposK != nil {
		ga, gb := f.glyphID(a), f.glyphID(b)
		if ga == 0 || gb == 0 {
			return 0
		}
		key := uint64(ga)<<32 | uint64(gb)
		if v, ok := f.kcache[key]; ok {
			return v
		}
		v := float32(f.gposK.Pair(int(ga), int(gb))) * float32(f.size) / f.upem
		if f.kcache == nil {
			f.kcache = map[uint64]float32{}
		}
		f.kcache[key] = v
		return v
	}
	if f.faceM != nil {
		return fixedToFloat(f.faceM.Kern(a, b))
	}
	return fixedToFloat(f.face.Kern(a, b))
}

// zeroWidth reports the invisible joiners/selectors that shape emoji
// sequences (ZWJ families, VS15/16 presentation). Without full shaping the
// components render individually; skipping these keeps them from showing as
// tofu boxes or stray gaps.
func zeroWidth(r rune) bool {
	switch {
	case r == 0x200C || r == 0x200D: // ZWNJ, ZWJ
		return true
	case r >= 0xFE00 && r <= 0xFE0F: // variation selectors (incl. VS15/16)
		return true
	case r == 0x20E3: // combining keycap
		return true
	}
	return false
}

// emojiPick reports whether r should render from the color-emoji fallback:
// no monochrome face in the chain covers it, and the emoji font does.
func (f *Font) emojiPick(r rune) bool {
	if f.emoji == nil || f.covers(r) {
		return false
	}
	if sf := f.pick(r); sf != f && sf.covers(r) {
		return false
	}
	return f.emoji.covers(r)
}

// upload pushes atlas changes to the GPU before drawing.
func (f *Font) upload() {
	for _, s := range f.pages {
		s.upload(f.r)
	}
	for _, s := range f.epages {
		s.upload(f.r)
	}
}

// DrawText queues s at the pixel position (x, y) of its top-left corner.
// Each glyph quad is snapped to the integer pixel grid so the atlas (which is
// rasterized at integer texels) maps 1:1 to screen pixels — fractional
// placement under linear sampling is what blurs text. The pen advances in
// fractional units so overall spacing stays correct; only the per-glyph quad
// origin is snapped.
func (r *Renderer2D) DrawText(f *Font, x, y float32, s string, c RGBA) {
	r.drawText(f, x, y, s, c, 0, 0)
}

// DrawTextTracked draws s with extra letter-spacing (px added after each
// glyph) — the CSS `letter-spacing` / QML font.letterSpacing behavior.
func (r *Renderer2D) DrawTextTracked(f *Font, x, y float32, s string, c RGBA, tracking float32) {
	r.drawText(f, x, y, s, c, 0, tracking)
}

// TextWidth measures s with the same advances/kerning DrawText uses, so
// callers can size backing boxes exactly (dimension labels, tooltips).
func (f *Font) TextWidth(s string) float32 {
	var pen float32
	prev := rune(-1)
	for _, ch := range s {
		if zeroWidth(ch) {
			continue
		}
		if f.emojiPick(ch) {
			if g, ok := f.ensureEmoji(ch); ok {
				pen += g.advance
			}
			prev = -1
			continue
		}
		sf := f.pick(ch)
		if prev >= 0 {
			pen += sf.kern(prev, ch)
		}
		if g, ok := sf.ensure(ch); ok {
			pen += g.advance
		}
		prev = ch
	}
	return pen
}

// TextWidthTracked measures s including per-glyph letter-spacing.
func (f *Font) TextWidthTracked(s string, tracking float32) float32 {
	w := f.TextWidth(s)
	if tracking == 0 {
		return w
	}
	n := 0
	for _, ch := range s {
		if !zeroWidth(ch) {
			n++
		}
	}
	if n > 0 {
		w += tracking * float32(n)
	}
	return w
}

// DrawTextItalic draws s as synthetic oblique (a horizontal shear), since our
// faces are upright-only — a stand-in for a true italic face.
func (r *Renderer2D) DrawTextItalic(f *Font, x, y float32, s string, c RGBA) {
	r.drawText(f, x, y, s, c, obliqueSkew, 0)
}

const obliqueSkew = 0.21 // tan(~12°) horizontal shear for synthetic italic

// rasterFont returns the face glyphs rasterize from under the current DPI
// scale, plus the raster scale rs (1 when no scaled face can be derived).
// Layout metrics divide the raster face's values by rs: hinting quantizes
// advances/kerning to whole pixels AT THE FACE SIZE, so the 2x face carries
// twice the fractional precision of the 1x face — driving the pen with 1x
// metrics at 2x lands every glyph on an even physical pixel and visibly
// mis-spaces kerned pairs ("To"), while raster metrics keep spacing aligned
// with the rendered bitmaps. MeasureText divides the same values, so
// measure and draw agree at every scale.
func (r *Renderer2D) rasterFont(f *Font) (*Font, float32) {
	rs := r.sc()
	if rs == 1 {
		return f, 1
	}
	if rf := f.AtSize(f.size * float64(rs)); rf != f {
		return rf, rs
	}
	return f, 1
}

func (r *Renderer2D) drawText(f *Font, x, y float32, s string, c RGBA, skew, tracking float32) {
	// Distance-field text when asked for: one sheet per family, drawn at
	// any size (msdftext.go).
	if r.fieldText(f) {
		r.drawTextMSDF(f, x, y, s, c, skew, tracking, 1)
		return
	}
	// Subpixel (LCD) text when available — the GPUI-matching path.
	if r.subpixelText && r.subpixelPipeline != nil && len(f.ttf) > 0 {
		r.drawTextSubpixel(f, x, y, s, c, skew, tracking)
		return
	}
	rf, rs := r.rasterFont(f)
	baseline := y + f.layoutAscent(rf, rs)
	// Synthetic oblique pivots at the optical center (not the baseline), so
	// the sheared glyph stays centered in its advance instead of leaning
	// visibly right of it.
	pivot := baseline - f.layoutAscent(rf, rs)*0.45
	// Laid out on the RASTER face and divided by rs — hinting quantizes
	// advances to whole pixels at the face's own size, so the 2x face
	// carries twice the fractional precision. Emoji come off the logical
	// face and are already in logical pixels.
	run := r.shape(f, rf, nil, s, 1/rs, 1)
	var used [4]*Font
	nused := 0
	for i := range run.glyphs {
		sg := &run.glyphs[i]
		pen := x + sg.pen + float32(i)*tracking
		if sg.emoji {
			// Color emoji: rasterized PNGs drawn untinted (white multiplies
			// to identity), keeping only the caller's opacity.
			if g, ok := f.ensureEmoji(sg.ch); ok {
				gx := math32.Round(pen + g.bearingX)
				gy := math32.Round(baseline + g.bearingY)
				r.glyphQuad(kindSprite, f.epages[g.page].tex.bind, gx, gy, g.w, g.h, g.u0, g.v0, g.u1, g.v1, RGBA{R: 1, G: 1, B: 1, A: c.A}, pivot, skew)
				nused = touchFont(&used, nused, f)
			}
			continue
		}
		rsf := sg.font
		if g, ok := rsf.ensure(sg.ch); ok && g.w > 0 {
			gx := math32.Round(pen*rs+g.bearingX) / rs
			gy := math32.Round(baseline*rs+g.bearingY) / rs
			r.glyphQuad(kindGlyph, rsf.pages[g.page].tex.bind, gx, gy, g.w/rs, g.h/rs, g.u0, g.v0, g.u1, g.v1, c, pivot, skew)
			nused = touchFont(&used, nused, rsf)
		}
	}
	for i := 0; i < nused; i++ {
		used[i].upload()
	}
}

// touchFont records a font whose sheet a run added to, for the upload at
// the end. A run is one face and at most a fallback or two, so this is a
// four-slot array rather than the map it used to allocate per call.
func touchFont(used *[4]*Font, n int, f *Font) int {
	for i := 0; i < n; i++ {
		if used[i] == f {
			return n
		}
	}
	if n < len(used) {
		used[n] = f
		n++
	}
	return n
}

// glyphQuad emits a glyph quad, optionally sheared (skew != 0) into a
// right-leaning parallelogram for synthetic italics.
func (r *Renderer2D) glyphQuad(kind batchKind, bind *gpu.BindGroup, x, y, w, h, u0, v0, u1, v1 float32, c RGBA, baseline, skew float32) {
	if skew == 0 {
		r.spriteQuad(kind, bind, x, y, w, h, u0, v0, u1, v1, c)
		return
	}
	st := skew * (baseline - y)       // top-edge x shift
	sb := skew * (baseline - (y + h)) // bottom-edge x shift
	// Each corner through the transform on its own — see spriteQuad.
	tlx, tly := r.toNDC(x+st, y)
	trx, try := r.toNDC(x+w+st, y)
	blx, bly := r.toNDC(x+sb, y+h)
	brx, bry := r.toNDC(x+w+sb, y+h)
	if isGlyphKind(kind) {
		b := r.ensureBatch(kind, bind, len(r.glyphInst)/floatsPerGlyphInst)
		b.cover([4]float32{tlx, trx, brx, blx}, [4]float32{tly, try, bry, bly})
		r.glyphInstance(tlx, tly, trx, try, brx, bry, blx, bly, u0, v0, u1, v1, c)
		b.count++
		return
	}
	b := r.ensureBatch(kind, bind, len(r.verts)/floatsPerVertex)
	r.vertex(tlx, tly, u0, v0, c)
	r.vertex(trx, try, u1, v0, c)
	r.vertex(brx, bry, u1, v1, c)
	r.vertex(tlx, tly, u0, v0, c)
	r.vertex(brx, bry, u1, v1, c)
	r.vertex(blx, bly, u0, v1, c)
	b.count += 6
}

// drawTextSubpixel draws s using the LCD atlas + dual-source blend pipeline.
func (r *Renderer2D) drawTextSubpixel(f *Font, x, y float32, s string, c RGBA, skew, tracking float32) {
	baseline := y + f.Ascent()
	pivot := baseline - f.Ascent()*0.45 // see drawText
	// DPI raster scale — see drawText: physical-grid rasters, logical layout.
	rs := r.sc()
	rf := f
	if rs != 1 {
		if rf = f.AtSize(f.size * float64(rs)); rf == f {
			rs = 1
		}
	}
	// Laid out on the LOGICAL face here, unlike the grayscale path: the
	// subpixel raster is a 3x-wide mask placed by its own bearings, and
	// the pen it is placed against is the logical one.
	run := r.shape(f, f, rf, s, 1, 1)
	var used [4]*Font
	nused := 0
	emojiUsed := false
	for i := range run.glyphs {
		sg := &run.glyphs[i]
		pen := x + sg.pen + float32(i)*tracking
		if sg.emoji {
			// Color emoji render through the regular sprite path (their
			// pixels are colors, not LCD coverage), interleaved with the run.
			if g, ok := f.ensureEmoji(sg.ch); ok {
				gx := math32.Round(pen + g.bearingX)
				gy := math32.Round(baseline + g.bearingY)
				r.glyphQuad(kindSprite, f.epages[g.page].tex.bind, gx, gy, g.w, g.h, g.u0, g.v0, g.u1, g.v1, RGBA{R: 1, G: 1, B: 1, A: c.A}, pivot, skew)
				emojiUsed = true
			}
			continue
		}
		rsf := sg.raster
		// Subpixel positioning: keep the fractional PHYSICAL pen, rasterize
		// the glyph pre-shifted by the nearest quarter pixel, and place the
		// quad on whole physical pixels using the RASTER's own bearing
		// (aligned to its 1x grid in ensureSubpixel) — 1x-hinted bearings
		// differ per glyph from the 3x raster and would shift individual
		// glyphs sideways. The phase depends on where the run landed, so
		// it cannot be shaped ahead of time.
		penP := pen * rs
		floorP := math32.Floor(penP)
		phase := int((penP - floorP) * subpixelPhases)
		if g, ok := rsf.ensureSubpixel(sg.ch, phase); ok && g.w > 0 {
			gy := math32.Round(baseline*rs+g.bearingY) / rs
			// The sheet the glyph landed on — a face with a large
			// character set, or sixteen phases of a big one, spills
			// onto more than one.
			bind := rsf.spages[g.page].tex.bind
			r.glyphQuad(kindSubpixel, bind, (floorP+g.bearingX)/rs, gy, g.w/rs, g.h/rs, g.u0, g.v0, g.u1, g.v1, c, pivot, skew)
			nused = touchFont(&used, nused, rsf)
		}
	}
	// Re-upload each font whose sheets gained glyphs this call.
	for i := 0; i < nused; i++ {
		r.uploadSubpixel(used[i])
	}
	if emojiUsed {
		f.upload()
	}
}

// WrapText breaks s into lines that each fit within maxWidth, splitting at
// spaces and honoring explicit newlines; words longer than maxWidth are
// hard-broken by rune. Greedy line-breaking (the common case); Knuth-Plass
// justification is a later refinement.
func (r *Renderer2D) WrapText(f *Font, s string, maxWidth float32) []string {
	var lines []string
	for _, para := range splitLines(s) {
		line := ""
		lineW := float32(0)
		for _, word := range splitWords(para) {
			ww, _ := r.MeasureText(f, word)
			spaceW := float32(0)
			if line != "" {
				spaceW, _ = r.MeasureText(f, " ")
			}
			if line != "" && lineW+spaceW+ww > maxWidth {
				// The running sum adds each word and space on its own,
				// which can total a hair MORE than the same text measured
				// in one piece — and a box sized to exactly its text's
				// width (`width: label.implicitWidth`, the QML idiom) then
				// wraps a line that does fit. Only break once the whole
				// candidate line really is too wide; the check runs only at
				// the margin, so a long paragraph still wraps in one pass.
				cand := line + " " + word
				if cw, _ := r.MeasureText(f, cand); cw > maxWidth {
					lines = append(lines, line)
					line, lineW = "", 0
					spaceW = 0
				} else {
					// Re-sync the running sum to the measured line, so the
					// drift does not accumulate across the next words.
					lineW = cw - spaceW - ww
				}
			}
			// Hard-break a single word wider than the line.
			if line == "" && ww > maxWidth {
				broken, rest := r.breakWord(f, word, maxWidth)
				lines = append(lines, broken)
				word = rest
				ww, _ = r.MeasureText(f, word)
			}
			if line == "" {
				line, lineW = word, ww
			} else {
				line += " " + word
				lineW += spaceW + ww
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func (r *Renderer2D) breakWord(f *Font, word string, maxWidth float32) (head, rest string) {
	runes := []rune(word)
	for i := 1; i < len(runes); i++ {
		if w, _ := r.MeasureText(f, string(runes[:i+1])); w > maxWidth {
			return string(runes[:i]), string(runes[i:])
		}
	}
	return word, ""
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	return append(out, cur)
}

func splitWords(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == ' ' || c == '\t' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// DrawTextWrapped lays s out within maxWidth and draws it as stacked lines
// starting at (x, y); it returns the total height drawn.
func (r *Renderer2D) DrawTextWrapped(f *Font, x, y, maxWidth float32, s string, c RGBA) float32 {
	lines := r.WrapText(f, s, maxWidth)
	for i, line := range lines {
		r.DrawText(f, x, y+float32(i)*f.Height(), line, c)
	}
	return float32(len(lines)) * f.Height()
}

// MeasureText returns the advance width and line height of s in pixels.
//
// Memoized, because a retained-mode page measures the SAME strings on
// every frame — once to lay out and again to paint — and the per-rune
// walk was a fifth of a live frame. It shares the shaping cache with
// drawing, so a run that is measured and then drawn is walked once
// between them rather than once each.
func (r *Renderer2D) MeasureText(f *Font, s string) (w, h float32) {
	return r.shape(f, f, nil, s, 1, 1).width, f.Height()
}

// FontStyle selects a face variant within a font family. Values combine:
// StyleBold | StyleItalic is the bold-italic face.
type FontStyle uint8

const (
	StyleRegular FontStyle = 0
	StyleBold    FontStyle = 1 << 0
	StyleItalic  FontStyle = 1 << 1
)

// fontKey identifies a registered face: family name + style variant. The
// empty family names the UI's default font, so styled defaults resolve
// without callers knowing a family name.
type fontKey struct {
	family string
	style  FontStyle
}

// RegisterFont associates a font-family name with TTF/OTF bytes for NamedFont
// (e.g. "monospace", "serif"). The example shell registers a few system fonts.
func (r *Renderer2D) RegisterFont(family string, ttf []byte) {
	r.RegisterFontStyled(family, StyleRegular, ttf)
}

// globalFonts holds process-wide family registrations (QML FontLoader —
// Qt's font database is global the same way). NamedFontStyled falls back
// here when a renderer-local registration misses. Guarded for safety,
// though registration normally happens on the UI goroutine.
//
// Keyed by (family, style), because a family arrives one FACE at a time:
// the two FontLoaders a QML file writes for a family's regular and bold
// files both report the same family name, and keying by family alone let
// whichever loaded last answer for both.
var (
	globalFontsMu sync.RWMutex
	globalFonts   = map[fontKey][]byte{}
)

// RegisterFontBytes registers a face process-globally (FontLoader), under
// the style the FILE declares — its weight class and italic bit — so a
// family assembled from several files keeps its faces apart.
func RegisterFontBytes(family string, ttf []byte) {
	RegisterFontBytesStyled(family, FaceStyle(ttf), ttf)
}

// RegisterFontBytesStyled is RegisterFontBytes with the style stated
// rather than read from the file.
func RegisterFontBytesStyled(family string, style FontStyle, ttf []byte) {
	globalFontsMu.Lock()
	globalFonts[fontKey{strings.ToLower(family), style}] = ttf
	globalFontsMu.Unlock()
}

// globalFontBytes returns a globally registered face's bytes, or nil. A
// style with no face of its own falls back to the family's regular (and
// then to any face it has), which is what a font database does: asking a
// one-face family for bold gets that face, emboldened synthetically.
func globalFontBytes(family string, style FontStyle) []byte {
	globalFontsMu.RLock()
	defer globalFontsMu.RUnlock()
	fam := strings.ToLower(family)
	if b := globalFonts[fontKey{fam, style}]; b != nil {
		return b
	}
	if b := globalFonts[fontKey{fam, StyleRegular}]; b != nil {
		return b
	}
	// Any face of the family, preferring the least styled one.
	var best []byte
	bestStyle := FontStyle(0xff)
	for k, b := range globalFonts {
		if k.family == fam && k.style < bestStyle {
			best, bestStyle = b, k.style
		}
	}
	return best
}

// FaceStyle reads the style a font FILE declares: bold from its OS/2
// weight class (600+, so semibold counts), italic from the head table's
// macStyle bit. A family's "Book" or "Light" face declares neither, which
// is right — it is the regular face of that family.
func FaceStyle(ttf []byte) FontStyle {
	var style FontStyle
	if FaceWeightClass(ttf) >= 600 {
		style |= StyleBold
	}
	if macStyle(ttf)&0x02 != 0 {
		style |= StyleItalic
	}
	return style
}

// macStyle is the head table's style bits (bit 0 bold, bit 1 italic), 0
// when unreadable.
func macStyle(ttf []byte) uint16 {
	if len(ttf) < 12 {
		return 0
	}
	n := int(binary.BigEndian.Uint16(ttf[4:6]))
	off := 12
	for i := 0; i < n && off+16 <= len(ttf); i++ {
		if string(ttf[off:off+4]) == "head" {
			t := int(binary.BigEndian.Uint32(ttf[off+8 : off+12]))
			if t+46 <= len(ttf) {
				return binary.BigEndian.Uint16(ttf[t+44 : t+46])
			}
			return 0
		}
		off += 16
	}
	return 0
}

// FontFamilyName reads a face's family name from its name table.
func FontFamilyName(ttf []byte) (string, error) {
	f, err := sfnt.Parse(ttf)
	if err != nil {
		return "", err
	}
	return f.Name(nil, sfnt.NameIDFamily)
}

// FaceWeightClass reads a face's OS/2 usWeightClass (400 regular, 700 bold);
// 0 when unparseable. Callers registering a bold style should verify the file
// really is bold: distros increasingly symlink "-Bold.ttf" names to variable
// fonts, whose DEFAULT instance (all our loader can render) is regular.
func FaceWeightClass(ttf []byte) int {
	if len(ttf) < 12 {
		return 0
	}
	n := int(binary.BigEndian.Uint16(ttf[4:6]))
	off := 12
	for i := 0; i < n && off+16 <= len(ttf); i++ {
		if string(ttf[off:off+4]) == "OS/2" {
			t := int(binary.BigEndian.Uint32(ttf[off+8 : off+12]))
			if t+6 <= len(ttf) {
				return int(binary.BigEndian.Uint16(ttf[t+4 : t+6]))
			}
			return 0
		}
		off += 16
	}
	return 0
}

// RegisterFontStyled registers a styled face (bold/italic/bold-italic) of a
// family. Text styled Bold()/Italic() resolves these real faces first and
// falls back to synthetic double-strike/shear only when none is registered.
// The empty family registers styled variants of the default font.
func (r *Renderer2D) RegisterFontStyled(family string, style FontStyle, ttf []byte) {
	if r.namedTTF == nil {
		r.namedTTF = map[fontKey][]byte{}
	}
	r.namedTTF[fontKey{family, style}] = ttf
}

// RegisterFallbacks stores the glyph fallbacks and color-emoji face that every
// lazily-built NamedFont inherits — without this, switching a text to a named
// family (e.g. "monospace") silently loses emoji/CJK coverage that the default
// font has. Any argument may be nil; `more` are further fallbacks tried after
// cjk (a symbol face, say).
func (r *Renderer2D) RegisterFallbacks(cjk, emoji []byte, more ...[][]byte) {
	r.fallbackTTF, r.emojiTTF = cjk, emoji
	r.fallbackMore = nil
	for _, m := range more {
		r.fallbackMore = append(r.fallbackMore, m...)
	}
}

// NamedFont returns a registered family's face at sizePx (lazily built and
// size-cached), or nil if the family wasn't registered. Used by gml Text's
// font.family.
func (r *Renderer2D) NamedFont(family string, sizePx float64) *Font {
	return r.NamedFontStyled(family, StyleRegular, sizePx)
}

// NamedFontStyled returns a family's registered styled face at sizePx, or nil
// if that exact (family, style) wasn't registered — the caller decides whether
// to fall back to another style or to synthetic emphasis.
func (r *Renderer2D) NamedFontStyled(family string, style FontStyle, sizePx float64) *Font {
	key := fontKey{family, style}
	base := r.namedFonts[key]
	if base == nil {
		ttf := r.namedTTF[key]
		if ttf == nil {
			ttf = globalFontBytes(family, style) // FontLoader registrations
		}
		if ttf == nil {
			return nil
		}
		bs := sizePx
		if bs <= 0 {
			bs = 18
		}
		nf, err := r.NewFont(ttf, bs)
		if err != nil {
			return nil
		}
		if r.fallbackTTF != nil || len(r.fallbackMore) > 0 {
			nf.SetFallback(append([][]byte{r.fallbackTTF}, r.fallbackMore...)...)
		}
		if r.emojiTTF != nil {
			_ = nf.SetEmojiFallback(r.emojiTTF)
		}
		if r.namedFonts == nil {
			r.namedFonts = map[fontKey]*Font{}
		}
		r.namedFonts[key] = nf
		base = nf
	}
	if sizePx > 0 {
		return base.AtSize(sizePx)
	}
	return base
}
