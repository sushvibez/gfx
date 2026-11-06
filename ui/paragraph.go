package ui

import (
	"math"
	"strings"

	"github.com/sushvibez/gfx/render"
)

// Paragraph is multi-line wrapped text within a fixed width — the GPUI
// text_wrapper / Qt Text-with-wrapping equivalent. Height is derived from
// how many lines the text wraps to at that width; inside a flex container
// Grow()/CrossFill() supply the width and the height follows from it.
type Paragraph struct {
	text   string
	color  RGBA
	w      float32
	sizePx float64 // 0 = context default
	center bool
	family string  // named font family (renderer registry); "" = context font
	lineH  float32 // line height ÷ font size (CSS line-height); 0 = ctx default
	maxLn  int     // line_clamp: 0 = unlimited
	noEll  bool    // clamp by clipping instead of ellipsizing
	natW   float32 // longest line, unwrapped — QML implicitWidth
	natH   float32
	// wrapAt is the width a row settled on for this paragraph (see
	// heightFor): the paint wraps at it, so measure and paint agree on the
	// very first frame of a rebuilt tree.
	wrapAt    float32
	grow      float32 // flex-grow: a paragraph asked to fill its line
	crossFill bool    // asked to fill the parent's cross axis
	bold      bool
	italic    bool
	selfAl    Align
	hasSelfAl bool
	rect      Rect
}

// Grow lets the paragraph take the leftover width of its row — QML's
// `Layout.fillWidth: true` on a wrapping Label, which is how nearly every
// description line in a card is written. The paragraph then wraps to the
// width it is given rather than to the whole line.
func (p *Paragraph) Grow() *Paragraph { p.grow = 1; return p }

func (p *Paragraph) growFactor() float32 { return p.grow }

// Bold and Italic style a wrapping paragraph — a real face of the family
// when one is registered, and synthetic emphasis otherwise, exactly as a
// single-line text does.
func (p *Paragraph) Bold() *Paragraph   { p.bold = true; return p }
func (p *Paragraph) Italic() *Paragraph { p.italic = true; return p }

// CrossFill marks a paragraph that asked to fill its parent's CROSS axis
// (`Layout.fillWidth` on a wrapping Label inside a ColumnLayout): it
// stretches to the line, and the column learns that something inside it
// can use extra width (see DivEl.layoutMax).
func (p *Paragraph) CrossFill() *Paragraph { p.crossFill = true; return p }

func (p *Paragraph) crossFills() bool { return p.crossFill }

// AlignSelf states the paragraph's own alignment across its parent's axis
// (Layout.alignment on a wrapping Label).
func (p *Paragraph) AlignSelf(a Align) *Paragraph { p.selfAl, p.hasSelfAl = a, true; return p }

func (p *Paragraph) selfAlign() (Align, bool) { return p.selfAl, p.hasSelfAl }

// LineHeight sets the paragraph's line advance as a multiple of its font
// size (CSS line-height); glyphs center in each line slot. Overrides the
// UI-wide default.
func (p *Paragraph) LineHeight(rel float64) *Paragraph { p.lineH = float32(rel); return p }

// Lines clamps the paragraph to at most n wrapped lines (GPUI line_clamp),
// ellipsizing the last line when the text overflows.
func (p *Paragraph) Lines(n int) *Paragraph { p.maxLn = n; return p }

// NoEllipsis makes a Lines clamp cut cleanly instead of appending "…"
// (GPUI TextOverflow::Truncate("")).
func (p *Paragraph) NoEllipsis() *Paragraph { p.noEll = true; return p }

// Wrapped builds a paragraph of s in color c, wrapping at width px.
func Wrapped(s string, c RGBA, width float32) *Paragraph {
	return &Paragraph{text: s, color: c, w: width}
}

// SizePx renders the paragraph at a derived font size.
func (p *Paragraph) SizePx(px float64) *Paragraph { p.sizePx = px; return p }

// Center horizontally centers each wrapped line within the width.
func (p *Paragraph) Center() *Paragraph { p.center = true; return p }

// FontFamily wraps/renders with a registered font family (e.g. "monospace");
// unknown families fall back to the context font.
func (p *Paragraph) FontFamily(name string) *Paragraph { p.family = name; return p }

// font is the face this paragraph draws with; synthetic reports whether a
// requested bold found no real face and has to be over-struck at paint.
func (p *Paragraph) font(ctx *Context) *render.Font {
	f, _ := p.faceFor(ctx)
	return f
}

// faceFor picks the paragraph's face, preferring a real styled one — the
// empty family holds the default font's variants, so a bold paragraph in
// the default font gets the real bold face when the system has one.
func (p *Paragraph) faceFor(ctx *Context) (*render.Font, bool) {
	if ctx.R != nil && (p.bold || p.italic) {
		size := p.sizePx
		if size <= 0 && ctx.Font != nil {
			size = ctx.Font.Size()
		}
		style := render.StyleRegular
		if p.bold {
			style |= render.StyleBold
		}
		if p.italic {
			style |= render.StyleItalic
		}
		if f := ctx.R.NamedFontStyled(p.family, style, size); f != nil {
			return f, false
		}
	}
	return p.plainFont(ctx), p.bold
}

func (p *Paragraph) plainFont(ctx *Context) *render.Font {
	if p.family != "" && ctx.R != nil {
		// Pass an explicit size so switching family never changes the
		// paragraph's size (the registry would default to its own).
		size := p.sizePx
		if size <= 0 && ctx.Font != nil {
			size = ctx.Font.Size()
		}
		// A styled face of the family when one is registered — a bold
		// paragraph must WRAP on the bold face's advances, or it breaks
		// its lines in the wrong places.
		style := render.StyleRegular
		if p.bold {
			style |= render.StyleBold
		}
		if p.italic {
			style |= render.StyleItalic
		}
		if style != render.StyleRegular {
			if f := ctx.R.NamedFontStyled(p.family, style, size); f != nil {
				return f
			}
		}
		if f := ctx.R.NamedFont(p.family, size); f != nil {
			return f
		}
	}
	f := ctx.Font
	if f != nil && p.sizePx > 0 {
		f = f.AtSize(p.sizePx)
	}
	return f
}

func (p *Paragraph) lineHeight(ctx *Context) float32 {
	box, _ := p.lineBox(ctx)
	return box
}

// lineBox resolves the per-line slot height and the half-leading offset
// glyphs draw at within it (see TextEl.lineBox).
func (p *Paragraph) lineBox(ctx *Context) (box, lead float32) {
	f := p.font(ctx)
	if f == nil {
		return 16, 0
	}
	h := f.Height()
	rel := p.lineH
	if rel == 0 {
		rel = ctx.defLineH
	}
	if rel <= 0 {
		return h, 0
	}
	box = max(h, float32(math.Round(float64(rel)*f.Size())))
	return box, (box - h) / 2
}

func (p *Paragraph) wrapped(ctx *Context, f *render.Font) []string {
	return p.wrappedAt(ctx, f, p.wrapWidth(ctx))
}

// wrappedAt wraps to an explicit width (0 = no wrapping, one line per hard
// break).
func (p *Paragraph) wrappedAt(ctx *Context, f *render.Font, w float32) []string {
	if w <= 0 {
		return strings.Split(p.text, "\n")
	}
	lines := ctx.R.WrapText(f, p.text, w)
	if p.maxLn > 0 && len(lines) > p.maxLn {
		lines = lines[:p.maxLn]
		if p.noEll {
			return lines // clean cut (TextOverflow::Truncate(""))
		}
		// Ellipsize the last kept line to its width.
		last := lines[p.maxLn-1]
		ew, _ := ctx.R.MeasureText(f, "…")
		runes := []rune(last)
		for n := len(runes); n >= 0; n-- {
			lw, _ := ctx.R.MeasureText(f, string(runes[:n]))
			if lw+ew <= w {
				lines[p.maxLn-1] = string(runes[:n]) + "…"
				break
			}
		}
	}
	return lines
}

// textContent: see ui.TextOf.
func (p *Paragraph) textContent() string { return p.text }

func (p *Paragraph) measure(ctx *Context) (float32, float32) {
	w := p.wrapWidth(ctx)
	lines := 1
	if ctx.R != nil {
		if f := p.font(ctx); f != nil {
			ls := p.wrappedAt(ctx, f, w)
			lines = len(ls)
			// The unwrapped content width, which is what markup sizing a
			// box off this text asks for (QML implicitWidth).
			p.natW = 0
			for _, ln := range ls {
				lw, _ := ctx.R.MeasureText(f, ln)
				p.natW = max(p.natW, lw)
			}
		}
	}
	h := float32(lines) * p.lineHeight(ctx)
	p.natH = h
	return p.w, h
}

func (p *Paragraph) natural() (float32, float32) { return p.natW, p.natH }

// heightFor is the paragraph's height when wrapped to w — what a row needs
// once it has shared its width out (CSS height-for-width). It also fixes
// the width the paint will wrap at, so the two agree even on the first
// frame of an immediate-mode tree, where there is no previous rect to
// learn from.
func (p *Paragraph) heightFor(ctx *Context, w float32) float32 {
	if p.w > 0 || ctx.R == nil {
		return p.natH
	}
	f := p.font(ctx)
	if f == nil {
		return p.natH
	}
	p.wrapAt = w
	lines := p.wrappedAt(ctx, f, w)
	h := float32(len(lines)) * p.lineHeight(ctx)
	p.natH = h
	return h
}

// wrapWidth is the width to wrap at: the explicit one, else the width the
// container offered while measuring (auto-width wrapping), else the box the
// element was actually given. Whether a paragraph wraps AT ALL is decided
// where the markup is still visible (gml only builds one when a width is
// imposed on the text — see textWidthImposed); everything that reaches
// here is meant to wrap to what it is given.
func (p *Paragraph) wrapWidth(ctx *Context) float32 {
	switch {
	case p.w > 0:
		return p.w
	case p.wrapAt > 0:
		return p.wrapAt
	case p.rect.W > 0:
		return p.rect.W
	case ctx != nil && ctx.availW > 0:
		return ctx.availW
	}
	return 0
}

// layout takes the box the parent assigned. A paragraph with no width of
// its own that nothing constrained — a plain child of an item, positioned
// by x/y rather than laid out — is as wide as its text: QML's Text takes
// its implicitWidth there, and wrapMode only starts breaking lines once a
// width IS imposed. Without this a wrapping Label reported width 0, and
// anything sized off it (`someLabel.width`) measured nothing.
func (p *Paragraph) layout(_ *Context, r Rect) {
	if r.W == 0 && p.w == 0 && p.natW > 0 {
		r.W = p.natW
	}
	p.rect = r
}

func (p *Paragraph) paint(ctx *Context) {
	if ctx.culled(p) {
		return
	}
	f := p.font(ctx)
	if ctx.R == nil || f == nil {
		return
	}
	col := ctx.color(p.color)
	lh, lead := p.lineBox(ctx)
	_, synth := p.faceFor(ctx)
	for i, line := range p.wrapped(ctx, f) {
		x := p.rect.X
		if p.center {
			lw, _ := ctx.R.MeasureText(f, line)
			x += (p.wrapWidth(ctx) - lw) / 2
		}
		y := p.rect.Y + float32(i)*lh + lead
		ctx.R.DrawText(f, x, y, line, col)
		if synth {
			// No real bold face for this family: over-strike, as TextEl
			// does (see synthBoldOffset).
			for _, o := range synthBoldStrikes(float32(f.Size())) {
				ctx.R.DrawText(f, x+o[0], y+o[1], line, col)
			}
		}
	}
}

// paintSig hashes what Paragraph.paint reads from the element (see damage.go).
func (p *Paragraph) paintSig() uint64 {
	h := newSig(0x9a9)
	h.str(p.text)
	h.col(p.color)
	h.f32(p.w)
	h.f64(p.sizePx)
	h.b(p.center)
	h.str(p.family)
	h.f32(p.lineH)
	h.i(p.maxLn)
	h.b(p.noEll)
	h.f32(p.wrapAt)
	h.b(p.bold)
	h.b(p.italic)
	return uint64(h)
}

func (p *Paragraph) hit(float32, float32) Element { return nil }
func (p *Paragraph) onClick() func()              { return nil }
func (p *Paragraph) bounds() Rect                 { return p.rect }
func (p *Paragraph) fixedWH() (float32, float32)  { return p.w, 0 }
