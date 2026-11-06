package ui

import "github.com/sushvibez/gfx/render"

// Span is a run of text with its own color (and optional font) within a
// RichText block.
type Span struct {
	Text  string
	Color RGBA
	Font  *render.Font // nil = the context default
}

// RichEl is wrapped, multi-color text: a sequence of styled spans flowed
// inline and wrapped at a fixed width (GPUI styled text / text runs). Word
// breaking happens within spans; each word keeps its span's color.
type RichEl struct {
	spans []Span
	w     float32
	rect  Rect
}

// Rich builds a rich-text block wrapping at width px.
func Rich(width float32, spans ...Span) *RichEl {
	return &RichEl{spans: spans, w: width}
}

// token is one positioned word with its style.
type token struct {
	text  string
	color RGBA
	font  *render.Font
	x, y  float32
}

// flow wraps the spans into positioned tokens and returns them plus the
// total height. Greedy word wrap; needs the renderer for measurement.
func (e *RichEl) flow(ctx *Context) ([]token, float32) {
	if ctx.R == nil || ctx.Font == nil {
		return nil, 0
	}
	lineH := ctx.Font.Height()
	spaceW, _ := ctx.R.MeasureText(ctx.Font, " ")
	var toks []token
	var penX, penY float32
	lineStart := true
	for _, sp := range e.spans {
		f := sp.Font
		if f == nil {
			f = ctx.Font
		}
		for _, word := range splitFields(sp.Text) {
			ww, _ := ctx.R.MeasureText(f, word)
			if !lineStart && penX+spaceW+ww > e.w {
				penX, penY = 0, penY+lineH // wrap
				lineStart = true
			}
			if !lineStart {
				penX += spaceW
			}
			toks = append(toks, token{text: word, color: sp.Color, font: f, x: penX, y: penY})
			penX += ww
			lineStart = false
		}
	}
	return toks, penY + lineH
}

func (e *RichEl) measure(ctx *Context) (float32, float32) {
	_, h := e.flow(ctx)
	if h == 0 {
		h = 16
	}
	return e.w, h
}

func (e *RichEl) layout(_ *Context, r Rect) { e.rect = r }

func (e *RichEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	toks, _ := e.flow(ctx)
	for _, t := range toks {
		ctx.R.DrawText(t.font, e.rect.X+t.x, e.rect.Y+t.y, t.text, ctx.color(t.color))
	}
}

func (e *RichEl) hit(float32, float32) Element { return nil }
func (e *RichEl) onClick() func()              { return nil }
func (e *RichEl) bounds() Rect                 { return e.rect }
func (e *RichEl) fixedWH() (float32, float32)  { return e.w, 0 }

// splitFields splits on spaces, dropping empties (whitespace-collapsing).
func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' {
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
