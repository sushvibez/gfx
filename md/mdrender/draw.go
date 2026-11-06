package mdrender

import (
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/render"
)

// Fonts are the faces a page is drawn in. Any of them may be supplied by
// the host (its own UI font, say); LoadFonts fills them with the Go fonts,
// which are always there.
type Fonts struct {
	Regular, Bold, Italic, BoldItalic, Mono, MonoBold *render.Font
	r                                                 *render.Renderer2D
}

// LoadFonts loads the Go fonts at a size.
func LoadFonts(r *render.Renderer2D, sizePx float64) (*Fonts, error) {
	fs := &Fonts{r: r}
	for _, f := range []struct {
		dst *(*render.Font)
		ttf []byte
	}{
		{&fs.Regular, goregular.TTF}, {&fs.Bold, gobold.TTF}, {&fs.Italic, goitalic.TTF},
		{&fs.BoldItalic, gobolditalic.TTF}, {&fs.Mono, gomono.TTF}, {&fs.MonoBold, gomonobold.TTF},
	} {
		font, err := r.NewFont(f.ttf, sizePx)
		if err != nil {
			fs.Release()
			return nil, err
		}
		*f.dst = font
	}
	return fs, nil
}

// WithFonts builds Fonts from faces the host already has. Missing faces
// fall back to Regular.
func WithFonts(r *render.Renderer2D, regular, bold, italic, boldItalic, mono *render.Font) *Fonts {
	or := func(f *render.Font) *render.Font {
		if f == nil {
			return regular
		}
		return f
	}
	return &Fonts{Regular: regular, Bold: or(bold), Italic: or(italic), BoldItalic: or(boldItalic),
		Mono: or(mono), MonoBold: or(mono), r: r}
}

// Release frees fonts LoadFonts made.
func (fs *Fonts) Release() {
	for _, f := range []*render.Font{fs.Regular, fs.Bold, fs.Italic, fs.BoldItalic, fs.Mono, fs.MonoBold} {
		if f != nil {
			f.Release()
		}
	}
}

// Font is the face for a Face, at its size.
func (fs *Fonts) Font(f Face) *render.Font {
	var base *render.Font
	switch {
	case f.Mono && f.Bold:
		base = fs.MonoBold
	case f.Mono:
		base = fs.Mono
	case f.Bold && f.Italic:
		base = fs.BoldItalic
	case f.Bold:
		base = fs.Bold
	case f.Italic:
		base = fs.Italic
	default:
		base = fs.Regular
	}
	if base == nil {
		base = fs.Regular
	}
	return base.AtSize(float64(f.Size))
}

// Metrics measures with these fonts.
func (fs *Fonts) Metrics() Metrics { return fontMetrics{fs} }

type fontMetrics struct{ fs *Fonts }

func (m fontMetrics) Width(f Face, s string) float32 {
	w, _ := m.fs.r.MeasureText(m.fs.Font(f), s)
	return w
}

func (m fontMetrics) LineHeight(f Face) float32 { return m.fs.Font(f).Height() }

// Draw draws the page with its top-left corner at x, y.
func (p *Page) Draw(r *render.Renderer2D, fs *Fonts, x, y float32) {
	for _, o := range p.Ops {
		switch o.Kind {
		case OpText:
			r.DrawText(fs.Font(o.Face), x+o.X, y+o.Y, o.Text, o.Color)
		case OpRect:
			if o.Radius > 0 {
				r.FillRoundRect(x+o.X, y+o.Y, o.W, o.H, o.Radius, o.Color)
			} else {
				r.FillRect(x+o.X, y+o.Y, o.W, o.H, o.Color)
			}
		case OpFrame:
			r.Quad(x+o.X, y+o.Y, o.W, o.H, render.RGBA{}, o.Radius, 1, o.Color)
		case OpImage:
			if p.imgs == nil {
				continue
			}
			if img := p.imgs.Image(r, o.Text); img != nil {
				r.DrawImageFit(img, x+o.X, y+o.Y, o.W, o.H, o.Color)
			}
		}
	}
}
