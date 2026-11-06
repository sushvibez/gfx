package ui

import (
	"math"
	"unsafe"

	"github.com/sushvibez/gfx/render"
)

// TextEl renders a single line of text in the context's default font. An
// explicit width/height with horizontal/vertical alignment positions the
// text within its box (QML Text semantics); a pixel size selects a derived
// font from the same family.
type TextEl struct {
	s         string
	color     RGBA
	font      *render.Font
	sizePx    float64 // 0 = context default font size
	w, h      float32 // 0 = size to content
	halign    Align
	valign    Align
	bold      bool // synthetic bold (double-strike) — single-weight font fallback
	italic    bool // synthetic oblique (shear)
	uline     bool
	ulineWav  bool    // wavy underline (CSS text-decoration-style: wavy)
	ulineC    RGBA    // decoration color; zero = text color
	ulineT    float32 // decoration thickness in px; 0 = auto (height/14)
	strike    bool
	natW      float32 // content size from the last measure (QML implicitWidth)
	natH      float32
	trunc     bool    // ellipsize to W (or the laid-out width) on overflow
	truncCl   bool    // truncate by clipping, without the "…"
	family    string  // named font family (resolved via the renderer's registry)
	tracking  float32 // extra px between glyphs (CSS letter-spacing)
	lineH     float32 // line height ÷ font size (CSS line-height); 0 = ctx default
	glow      RGBA    // soft halo drawn behind the text (0 alpha = off)
	field     int8    // text path: 0 inherit the UI's, +1 distance field, -1 raster
	grow      float32 // flex-grow: text asked to fill its line
	crossFill bool    // asked to fill the parent's cross axis
	selfAl    Align
	hasSelfAl bool
	rect      Rect
	// measure memo per frame (see measure)
	mFrame uint64
	mW, mH float32
}

// DistanceField picks the text path for this one run, overriding the
// UI-wide default (UI.SetMSDFText). This is QML's `Text.renderType`:
// on is QtRendering (a distance field — crisp at any scale, rotated or
// with a size that slides, and where the weight/outline/glow effects
// come from), off is NativeRendering (the rasterised coverage atlas,
// hinted to the pixel grid).
//
// Set it per run where one run wants the other path — a heading that
// animates its size next to body text that does not — rather than as a
// habit: each switch breaks the batch.
func (t *TextEl) DistanceField(on bool) *TextEl {
	if on {
		t.field = 1
	} else {
		t.field = -1
	}
	return t
}

// FontFamily renders with a registered font family (e.g. "monospace"); unknown
// families fall back to the context font.
func (t *TextEl) FontFamily(name string) *TextEl { t.family = name; return t }

// Grow lets the text take the leftover width of its row (QML's
// Layout.fillWidth on a Label). A single-line text does not change shape
// for it, but it does claim the space — which is what an alignment or a
// following item is positioned against.
func (t *TextEl) Grow() *TextEl { t.grow = 1; return t }

// GrowBy is Grow with a weight; see DivEl.GrowBy.
func (t *TextEl) GrowBy(f float32) *TextEl { t.grow = f; return t }

func (t *TextEl) growFactor() float32 { return t.grow }

// CrossFill marks text that asked to fill its parent's CROSS axis — QML's
// `Layout.fillWidth` on a Label inside a ColumnLayout. It stretches to the
// line like an auto-sized box, and it tells the column that something
// inside it can use extra width (see DivEl.layoutMax).
func (t *TextEl) CrossFill() *TextEl { t.crossFill = true; return t }

func (t *TextEl) crossFills() bool { return t.crossFill }

// AlignSelf states the text's own alignment across its parent's axis
// (Layout.alignment on a Label).
func (t *TextEl) AlignSelf(a Align) *TextEl { t.selfAl, t.hasSelfAl = a, true; return t }

func (t *TextEl) selfAlign() (Align, bool) { return t.selfAl, t.hasSelfAl }

// Tracking adds letter-spacing in px between glyphs.
func (t *TextEl) Tracking(px float32) *TextEl { t.tracking = px; return t }

// Glow draws a soft halo behind the text (the theme's text-shadow glow).
func (t *TextEl) Glow(c RGBA) *TextEl { t.glow = c; return t }

// Italic renders italic text: the family's real italic face when one is
// registered, else a synthetic oblique shear.
func (t *TextEl) Italic() *TextEl { t.italic = true; return t }

// Truncate clips the text to its width, appending "…" when it overflows
// (QML elide / GPUI text_ellipsis). Uses W when set, else the width layout
// assigned (a stretched/grown box).
func (t *TextEl) Truncate() *TextEl { t.trunc = true; return t }

// TruncateClip is Truncate without the "…" — a clean cut at the last glyph
// that fits (GPUI TextOverflow::Truncate(""), CSS overflow hidden nowrap).
func (t *TextEl) TruncateClip() *TextEl { t.trunc, t.truncCl = true, true; return t }

// Text builds a text element.
func Text(s string, c RGBA) *TextEl { return &TextEl{s: s, color: c} }

// Bold renders bold text: the family's real bold face when one is
// registered, else synthetic double-strike emphasis.
func (t *TextEl) Bold() *TextEl { t.bold = true; return t }

// Underline draws a line under the text.
func (t *TextEl) Underline() *TextEl { t.uline = true; return t }

// UnderlineWavy draws the underline as a sine squiggle (CSS
// text-decoration-style: wavy, GPUI text_decoration_wavy).
func (t *TextEl) UnderlineWavy() *TextEl { t.uline, t.ulineWav = true, true; return t }

// UnderlineColor colors the underline/strike independently of the text
// (CSS text-decoration-color).
func (t *TextEl) UnderlineColor(c RGBA) *TextEl { t.ulineC = c; return t }

// UnderlineThickness sets the decoration line width in px (GPUI
// text_decoration_2 etc.); 0 picks an automatic weight.
func (t *TextEl) UnderlineThickness(px float32) *TextEl { t.ulineT = px; return t }

// StrikeThrough draws a line through the middle of the text.
func (t *TextEl) StrikeThrough() *TextEl { t.strike = true; return t }

// Font overrides the context's default font.
func (t *TextEl) Font(f *render.Font) *TextEl { t.font = f; return t }

// SizePx renders the text at the given pixel size (derived from the context
// font's family).
func (t *TextEl) SizePx(px float64) *TextEl { t.sizePx = px; return t }

// LineHeight sets this text's line box as a multiple of its font size (CSS
// line-height, QML lineHeight, gpui line_height); glyphs center vertically
// in the box (half-leading). Overrides the UI-wide default.
func (t *TextEl) LineHeight(rel float64) *TextEl { t.lineH = float32(rel); return t }

// lineBox returns the layout height for the text (the line box) and the
// half-leading offset glyphs are drawn at inside it. With no line height in
// play both stay at the face's natural height / zero offset.
func (t *TextEl) lineBox(ctx *Context, f *render.Font) (box, lead float32) {
	h := f.Height()
	rel := t.lineH
	if rel == 0 {
		rel = ctx.defLineH
	}
	if rel <= 0 {
		return h, 0
	}
	// gpui rounds the resolved line height to whole pixels.
	box = max(h, float32(math.Round(float64(rel)*f.Size())))
	return box, (box - h) / 2
}

// W / H give the text an explicit box; alignment positions glyphs within it.
func (t *TextEl) W(px float32) *TextEl { t.w = px; return t }
func (t *TextEl) H(px float32) *TextEl { t.h = px; return t }

// HAlign / VAlign set alignment of the text within its box.
func (t *TextEl) HAlign(a Align) *TextEl { t.halign = a; return t }
func (t *TextEl) VAlign(a Align) *TextEl { t.valign = a; return t }

// resolveFont picks the face for this text. Bold/italic resolve real styled
// faces from the renderer's registry first (the empty family holds the
// default font's variants); synthBold/synthItalic report which requested
// styles found no face and still need synthetic emphasis (double-strike /
// shear) at paint time — the browser's faux-styling model.
func (t *TextEl) resolveFont(ctx *Context) (f *render.Font, synthBold, synthItalic bool) {
	// Registry lookups need an explicit size: match the face we'd otherwise
	// use, so switching family or style never changes the text's size.
	size := t.sizePx
	if size <= 0 {
		if t.font != nil {
			size = t.font.Size()
		} else if ctx.Font != nil {
			size = ctx.Font.Size()
		}
	}
	if ctx.R != nil && (t.bold || t.italic) {
		style := render.StyleRegular
		if t.bold {
			style |= render.StyleBold
		}
		if t.italic {
			style |= render.StyleItalic
		}
		if f := ctx.R.NamedFontStyled(t.family, style, size); f != nil {
			return f, false, false
		}
		// Bold-italic with only one styled face present: use it and
		// synthesize the missing half.
		if t.bold && t.italic {
			if f := ctx.R.NamedFontStyled(t.family, render.StyleBold, size); f != nil {
				return f, false, true
			}
			if f := ctx.R.NamedFontStyled(t.family, render.StyleItalic, size); f != nil {
				return f, true, false
			}
		}
	}
	if t.family != "" && ctx.R != nil {
		if f := ctx.R.NamedFont(t.family, size); f != nil {
			return f, t.bold, t.italic
		}
	}
	f = t.font
	if f == nil {
		f = ctx.Font
	}
	if f != nil && t.sizePx > 0 {
		f = f.AtSize(t.sizePx)
	}
	return f, t.bold, t.italic
}

// synthBoldOffset is how far a synthetic bold over-strikes, in pixels, for
// text of a given size: a fixed fraction of the size (a 32px heading needs
// ~1.4px, a 13px label ~0.6px), which lands near the ink a real bold face
// would put down.
func synthBoldOffset(size float32) float32 {
	return max(0.6, size*0.055)
}

// synthBoldStrikes are the offsets a synthetic bold over-strikes at: a
// small dilation rather than one sideways copy, since a real bold face
// thickens the horizontal bars as well as the stems. The vertical spread
// is half the horizontal one, the way a humanist sans weights up.
func synthBoldStrikes(size float32) [][2]float32 {
	d := synthBoldOffset(size)
	if d < 0.9 {
		return [][2]float32{{d, 0}}
	}
	return [][2]float32{{d / 2, 0}, {d, 0}, {d / 2, -d / 2}, {d, -d / 2}}
}

// textContent: see ui.TextOf.
func (t *TextEl) textContent() string { return t.s }

func (t *TextEl) measure(ctx *Context) (float32, float32) {
	ctx.MeasureCalls++
	if t.mFrame == ctx.frame && ctx.frame != 0 {
		return t.mW, t.mH
	}
	w, h := t.measureUncached(ctx)
	t.mFrame, t.mW, t.mH = ctx.frame, w, h
	return w, h
}

func (t *TextEl) measureUncached(ctx *Context) (float32, float32) {
	f, _, _ := t.resolveFont(ctx)
	if f == nil {
		return t.w, t.h
	}
	cw, _ := ctx.R.MeasureText(f, t.s)
	ch, _ := t.lineBox(ctx, f)
	// The content size, whatever width the layout goes on to assign — QML's
	// implicitWidth, which markup sizes boxes from (a chat bubble is as wide
	// as its message, capped).
	t.natW, t.natH = cw, ch
	if t.w > 0 {
		cw = t.w
	}
	// A label that both grows and truncates asks for whatever its line has
	// and cuts the rest, so it needs no width of its own (CSS min-width:0
	// on a flex item with overflow hidden). Measured at its full text, a
	// long name made a scrolling column's content wider than the view and
	// pushed the sibling after it off the right edge.
	if t.trunc && t.grow > 0 && t.w == 0 {
		cw = 0
	}
	if t.h > 0 {
		ch = t.h
	}
	return cw, ch
}

func (t *TextEl) natural() (float32, float32) { return t.natW, t.natH }

func (t *TextEl) layout(_ *Context, r Rect) { t.rect = r }

// paintBounds: a glow halo reaches a couple of pixels past the text.
func (t *TextEl) paintBounds() Rect {
	if t.glow.A > 0 {
		return inflate(t.rect, 3)
	}
	return t.rect
}

// paintSig hashes what TextEl.paint reads from the element (see damage.go).
func (t *TextEl) paintSig() uint64 {
	h := newSig(0x7e7)
	h.str(t.s)
	h.col(t.color)
	h.ptr(unsafe.Pointer(t.font))
	h.f64(t.sizePx)
	h.f32(t.w)
	h.f32(t.h)
	h.i(int(t.halign))
	h.i(int(t.valign))
	h.b(t.bold)
	h.b(t.italic)
	h.b(t.uline)
	h.b(t.ulineWav)
	h.col(t.ulineC)
	h.f32(t.ulineT)
	h.b(t.strike)
	h.b(t.trunc)
	h.b(t.truncCl)
	h.str(t.family)
	h.f32(t.tracking)
	h.f32(t.lineH)
	h.col(t.glow)
	h.i(int(t.field))
	return uint64(h)
}

func (t *TextEl) paint(ctx *Context) {
	if ctx.culled(t) {
		return
	}
	f, synthBold, synthItalic := t.resolveFont(ctx)
	if f == nil {
		return
	}
	x, y := t.rect.X, t.rect.Y
	s := t.s
	cw, _ := ctx.R.MeasureText(f, s)
	ch, lead := t.lineBox(ctx, f)
	if t.tracking != 0 {
		cw = f.TextWidthTracked(s, t.tracking)
	}
	// Ellipsize/clip to the box width if requested and overflowing. An
	// explicit W wins; otherwise the width layout assigned (grown boxes).
	tw := t.w
	if tw == 0 {
		tw = t.rect.W
	}
	if t.trunc && tw > 0 && cw > tw {
		ell := "…"
		if t.truncCl {
			ell = ""
		}
		ew, _ := ctx.R.MeasureText(f, ell)
		budget := tw - ew
		runes := []rune(t.s)
		n := len(runes)
		for n > 0 {
			w, _ := ctx.R.MeasureText(f, string(runes[:n]))
			if w <= budget {
				break
			}
			n--
		}
		s = string(runes[:n]) + ell
		cw, ch = ctx.R.MeasureText(f, s)
	}
	switch t.halign {
	case AlignCenter:
		x += (t.rect.W - cw) / 2
	case AlignEnd:
		x += t.rect.W - cw
	}
	switch t.valign {
	case AlignCenter:
		y += (t.rect.H - ch) / 2
	case AlignEnd:
		y += t.rect.H - ch
	}
	y += lead // half-leading: glyphs center within the line box
	col := ctx.color(t.color)
	// A per-run text path is renderer state around the draws, restored
	// after: the batcher breaks a batch when the pipeline changes, so
	// this costs one extra draw call on a frame that mixes the two.
	if t.field != 0 {
		was, auto := ctx.R.MSDFTextEnabled(), ctx.R.MSDFAuto()
		ctx.R.SetMSDFText(t.field > 0)
		if t.field < 0 {
			ctx.R.SetMSDFAuto(false) // NativeRendering: the raster even when transformed
		}
		defer func() {
			ctx.R.SetMSDFText(was)
			ctx.R.SetMSDFAuto(auto)
		}()
	}
	draw := func(dx, dy float32, c RGBA) {
		switch {
		case synthItalic:
			ctx.R.DrawTextItalic(f, x+dx, y+dy, s, c)
		case t.tracking != 0:
			ctx.R.DrawTextTracked(f, x+dx, y+dy, s, c, t.tracking)
		default:
			ctx.R.DrawText(f, x+dx, y+dy, s, c)
		}
	}
	if t.glow.A > 0 {
		// Cheap halo: ring of low-alpha copies approximating a blurred
		// text-shadow (0 0 ~8px). Only pay the cost where a glow is set.
		g := ctx.color(t.glow)
		g.A *= 0.22
		for _, o := range [...][2]float32{{1.5, 0}, {-1.5, 0}, {0, 1.5}, {0, -1.5}, {1.1, 1.1}, {-1.1, 1.1}, {1.1, -1.1}, {-1.1, -1.1}} {
			draw(o[0], o[1], g)
		}
	}
	draw(0, 0, col)
	if synthBold {
		// No real bold face: thicken by over-striking. The offset has to
		// scale with the size or a 32px heading stays visibly light —
		// a real bold face carries about 1.5x the ink of its regular, and
		// synthBoldOffset is calibrated to that. Two strikes, so a wide
		// offset does not read as a doubled outline.
		for _, o := range synthBoldStrikes(float32(f.Size())) {
			draw(o[0], o[1], col)
		}
	}
	if t.uline || t.strike {
		gh := f.Height() // decorations track the glyphs, not the line box
		thick := t.ulineT
		if thick <= 0 {
			thick = max(gh/14, 1)
		}
		dc := col
		if t.ulineC.A > 0 {
			dc = ctx.color(t.ulineC)
		}
		if t.uline {
			if t.ulineWav {
				// Sine squiggle: wavelength/amplitude scale with thickness
				// like browsers render wavy decorations.
				amp := thick
				wl := thick * 6
				yc := y + gh - amp
				p := render.NewPath()
				p.MoveTo(x, yc)
				for xx := float32(2); xx <= cw; xx += 2 {
					p.LineTo(x+xx, yc+amp*float32(math.Sin(float64(2*math.Pi*xx/wl))))
				}
				ctx.R.StrokePathRound(p, thick, dc)
			} else {
				ctx.R.FillRect(x, y+gh-thick*2, cw, thick, dc)
			}
		}
		if t.strike {
			ctx.R.FillRect(x, y+gh*0.58, cw, thick, dc)
		}
	}
}

func (t *TextEl) hit(_, _ float32) Element { return nil }
func (t *TextEl) onClick() func()          { return nil }
func (t *TextEl) bounds() Rect             { return t.rect }

// fixedWH lets flex containers stretch an unsized text to the line (so a
// grown box's Truncate sees the real available width); explicit W/H win.
func (t *TextEl) fixedWH() (float32, float32) { return t.w, t.h }
