// Package mdrender lays a Markdown document out to a width and draws it
// with the framework's 2D renderer.
//
// Layout and drawing are separate. Layout turns the tree into a Page: a
// list of positioned text runs and rectangles, its height, and where its
// links are. It measures through a Metrics, so it runs (and is tested)
// without a GPU. Drawing replays the page with real fonts.
//
//	page := mdrender.Layout(md.Parse(src), 600, mdrender.DarkStyle(15), fonts.Metrics(r))
//	page.Draw(r, fonts, x, y)
package mdrender

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sushvibez/gfx/md"
	"github.com/sushvibez/gfx/render"
)

// Face is a font choice: the weight, the slant, the family, the size.
type Face struct {
	Bold, Italic, Mono bool
	Size               float32
}

// Metrics measures text for layout.
type Metrics interface {
	// Width is how wide a string is in a face.
	Width(f Face, s string) float32
	// LineHeight is the height of a line of that face.
	LineHeight(f Face) float32
}

// OpKind is what an Op draws.
type OpKind int

const (
	OpText  OpKind = iota // Text in Face at X, Y (the top of its line)
	OpRect                // a filled rectangle, Radius for its corners
	OpFrame               // a rectangle outline, 1 px
	OpImage               // the picture named by Text, scaled into X, Y, W, H
)

// Op is one thing the page draws, relative to the page's origin.
type Op struct {
	Kind       OpKind
	X, Y, W, H float32
	Face       Face
	Text       string
	Color      render.RGBA
	Radius     float32
}

// LinkBox is where a link is on the page.
type LinkBox struct {
	X, Y, W, H float32
	Dest       string
}

// Page is a laid-out document.
type Page struct {
	W, H  float32
	Ops   []Op
	Links []LinkBox
	// Anchors are where a link to "#name" goes: every heading by its
	// slug, every footnote ("fn-label") and every reference to one
	// ("fnref-label", "fnref-label-2"), as a height on the page.
	Anchors map[string]float32
	imgs    ImageSource
	m       Metrics
}

// Anchor is where a "#name" link points, if the page has it.
func (p *Page) Anchor(dest string) (float32, bool) {
	y, ok := p.Anchors[strings.TrimPrefix(dest, "#")]
	return y, ok
}

// LinkAt is the link under a point on the page, if there is one.
func (p *Page) LinkAt(x, y float32) (string, bool) {
	for _, l := range p.Links {
		if x >= l.X && x < l.X+l.W && y >= l.Y && y < l.Y+l.H {
			return l.Dest, true
		}
	}
	return "", false
}

// Style is how a page looks: sizes, spacing and colours.
type Style struct {
	Size       float32 // body text, px
	Text       render.RGBA
	Muted      render.RGBA // quotes, list markers, image alt text
	Link       render.RGBA
	Code       render.RGBA // inline and block code text
	CodeBg     render.RGBA
	Rule       render.RGBA // rules, quote bars, table borders
	HeaderBg   render.RGBA // a table's header row
	Check      render.RGBA // a done task's box
	BlockGap   float32     // between blocks, as a fraction of Size
	LineSpread float32     // line height over the font's, for body text
}

// DarkStyle is light text on a dark page.
func DarkStyle(size float32) *Style {
	return &Style{Size: size,
		Text:     rgb(0xe6e8ee),
		Muted:    rgb(0x9aa3b2),
		Link:     rgb(0x6cb6ff),
		Code:     rgb(0xf0c987),
		CodeBg:   rgba(0xffffff, 0.06),
		Rule:     rgba(0xffffff, 0.16),
		HeaderBg: rgba(0xffffff, 0.05),
		Check:    rgb(0x56c271),
		BlockGap: 0.8, LineSpread: 1.25}
}

// LightStyle is dark text on a light page.
func LightStyle(size float32) *Style {
	return &Style{Size: size,
		Text:     rgb(0x1f2328),
		Muted:    rgb(0x59636e),
		Link:     rgb(0x0969da),
		Code:     rgb(0x953800),
		CodeBg:   rgba(0x000000, 0.05),
		Rule:     rgba(0x000000, 0.14),
		HeaderBg: rgba(0x000000, 0.04),
		Check:    rgb(0x1a7f37),
		BlockGap: 0.8, LineSpread: 1.25}
}

func rgb(h uint32) render.RGBA { return rgba(h, 1) }

func rgba(h uint32, a float32) render.RGBA {
	return render.RGBA{R: float32(h>>16&0xff) / 255, G: float32(h>>8&0xff) / 255, B: float32(h&0xff) / 255, A: a}
}

// headingScale is a heading's size against body text, by level.
var headingScale = [7]float32{1, 2, 1.5, 1.25, 1.1, 1, 0.9}

// Layout lays a document out to a width. Pictures are shown as their alt
// text; LayoutImages draws them.
func Layout(doc *md.Node, width float32, st *Style, m Metrics) *Page {
	return LayoutImages(doc, width, st, m, nil)
}

// LayoutImages lays a document out with its pictures from imgs (nil for
// none). A picture is as large as it is, scaled down to fit the column; on
// a line of its own it is a block, inside text it sits on the baseline.
func LayoutImages(doc *md.Node, width float32, st *Style, m Metrics, imgs ImageSource) *Page {
	l := &layouter{st: st, m: m, imgs: imgs, page: &Page{W: width, imgs: imgs, m: m, Anchors: map[string]float32{}}}
	y := l.blocks(doc.Children, 0, 0, width, 0)
	l.page.H = y
	return l.page
}

type layouter struct {
	st   *Style
	m    Metrics
	imgs ImageSource
	page *Page
}

func (l *layouter) gap() float32 { return l.st.Size * l.st.BlockGap }

func (l *layouter) op(o Op) {
	if o.Kind == OpText && o.W == 0 {
		o.W = l.m.Width(o.Face, o.Text) // for selection
	}
	l.page.Ops = append(l.page.Ops, o)
}

func (l *layouter) anchor(name string, y float32) {
	if _, seen := l.page.Anchors[name]; name != "" && !seen {
		l.page.Anchors[name] = y
	}
}

// blocks lays a run of blocks out from y, between x and x+w, and returns
// where the run ends. depth is the list nesting, for the bullet shapes.
func (l *layouter) blocks(ns []*md.Node, x, y, w float32, depth int) float32 {
	for i, n := range ns {
		if i > 0 {
			y += l.gap()
		}
		y = l.block(n, x, y, w, depth, false)
	}
	return y
}

func (l *layouter) block(n *md.Node, x, y, w float32, depth int, tight bool) float32 {
	st := l.st
	body := Face{Size: st.Size}
	switch n.Kind {
	case md.Paragraph:
		return l.flow(n.Children, x, y, w, body, st.Text, md.AlignNone)
	case md.Heading:
		lvl := min(max(n.Level, 1), 6)
		f := Face{Bold: true, Size: st.Size * headingScale[lvl]}
		if lvl > 1 {
			y += st.Size * 0.3
		}
		l.anchor(n.ID, y)
		col := st.Text
		if lvl == 6 {
			col = st.Muted
		}
		y = l.flow(n.Children, x, y, w, f, col, md.AlignNone)
		if lvl <= 2 {
			y += st.Size * 0.3
			l.op(Op{Kind: OpRect, X: x, Y: y, W: w, H: 1, Color: st.Rule})
			y++
		}
		return y
	case md.ThematicBreak:
		y += st.Size * 0.4
		l.op(Op{Kind: OpRect, X: x, Y: y, W: w, H: 2, Color: st.Rule})
		return y + 2 + st.Size*0.4
	case md.BlockQuote:
		inset := st.Size
		top := y
		end := l.withColor(st.Muted, func() float32 {
			return l.blocks(n.Children, x+inset, y, w-inset, depth)
		})
		l.op(Op{Kind: OpRect, X: x + 2, Y: top, W: 3, H: end - top, Color: st.Rule, Radius: 1.5})
		return end
	case md.List:
		return l.list(n, x, y, w, depth)
	case md.CodeBlock:
		return l.codeBlock(n.Text, x, y, w)
	case md.HTMLBlock:
		return l.htmlBlock(n.Text, x, y, w)
	case md.DefList:
		for i, c := range n.Children {
			if i > 0 {
				y += st.Size * 0.3
			}
			if c.Kind == md.DefTerm {
				y = l.flow(c.Children, x, y, w, Face{Bold: true, Size: st.Size}, st.Text, md.AlignNone)
				continue
			}
			y = l.blocks(c.Children, x+st.Size*1.6, y, w-st.Size*1.6, depth)
		}
		return y
	case md.Footnotes:
		return l.footnotes(n, x, y, w)
	case md.Table:
		return l.table(n, x, y, w)
	}
	return y
}

// withColor lays out a block run with every text colour that would be
// the body text colour replaced: a quote's text is the muted colour.
func (l *layouter) withColor(c render.RGBA, fn func() float32) float32 {
	from := len(l.page.Ops)
	y := fn()
	for i := from; i < len(l.page.Ops); i++ {
		if o := &l.page.Ops[i]; o.Kind == OpText && o.Color == l.st.Text {
			o.Color = c
		}
	}
	return y
}

// list lays out a list's items, each with its marker in a gutter.
func (l *layouter) list(n *md.Node, x, y, w float32, depth int) float32 {
	st := l.st
	body := Face{Size: st.Size}
	gutter := st.Size * 1.5
	if n.Ordered {
		last := n.Start + len(n.Children) - 1
		gutter = max(gutter, l.m.Width(body, strconv.Itoa(last)+".")+st.Size*0.5)
	}
	itemGap := st.Size * 0.3
	if !n.Tight {
		itemGap = l.gap()
	}
	lh := l.lineH(body)
	for i, it := range n.Children {
		if i > 0 {
			y += itemGap
		}
		cx := x + gutter
		// The marker, on the item's first line.
		switch {
		case it.Task != md.NoTask:
			box := st.Size * 0.85
			by := y + (lh-box)/2
			if it.Task == md.TaskDone {
				l.op(Op{Kind: OpRect, X: x + gutter - box - st.Size*0.4, Y: by, W: box, H: box, Color: st.Check, Radius: 3})
				l.tick(x+gutter-box-st.Size*0.4, by, box)
			} else {
				l.op(Op{Kind: OpFrame, X: x + gutter - box - st.Size*0.4, Y: by, W: box, H: box, Color: st.Muted, Radius: 3})
			}
		case n.Ordered:
			s := strconv.Itoa(n.Start+i) + "."
			mw := l.m.Width(body, s)
			l.op(Op{Kind: OpText, X: x + gutter - mw - st.Size*0.4, Y: y, Face: body, Text: s, Color: st.Muted})
		default:
			// A disc, a ring, a square, by depth.
			d := st.Size * 0.32
			bx, by := x+gutter-st.Size*0.85, y+(lh-d)/2
			switch depth % 3 {
			case 0:
				l.op(Op{Kind: OpRect, X: bx, Y: by, W: d, H: d, Color: st.Muted, Radius: d / 2})
			case 1:
				l.op(Op{Kind: OpFrame, X: bx, Y: by, W: d, H: d, Color: st.Muted, Radius: d / 2})
			default:
				l.op(Op{Kind: OpRect, X: bx, Y: by, W: d, H: d, Color: st.Muted})
			}
		}
		if len(it.Children) == 0 {
			y += lh
			continue
		}
		// A done task's text is muted and struck through.
		from := len(l.page.Ops)
		gap := l.gap()
		if n.Tight {
			gap = st.Size * 0.3
		}
		for j, c := range it.Children {
			if j > 0 {
				y += gap
			}
			y = l.block(c, cx, y, w-gutter, depth+1, n.Tight)
		}
		if it.Task == md.TaskDone {
			for k := from; k < len(l.page.Ops); k++ {
				if o := &l.page.Ops[k]; o.Kind == OpText {
					o.Color = st.Muted
				}
			}
		}
	}
	return y
}

// tick draws a check mark in a box, as two strokes made of small squares.
func (l *layouter) tick(x, y, s float32) {
	ink := render.RGBA{R: 1, G: 1, B: 1, A: 1}
	dot := max(1.5, s*0.12)
	steps := 8
	for i := 0; i <= steps; i++ {
		t := float32(i) / float32(steps)
		// Short stroke down to the corner, then the long one up.
		ax, ay := x+s*(0.22+0.18*t), y+s*(0.52+0.2*t)
		bx, by := x+s*(0.40+0.38*t), y+s*(0.72-0.44*t)
		l.op(Op{Kind: OpRect, X: ax - dot/2, Y: ay - dot/2, W: dot, H: dot, Color: ink, Radius: dot / 2})
		l.op(Op{Kind: OpRect, X: bx - dot/2, Y: by - dot/2, W: dot, H: dot, Color: ink, Radius: dot / 2})
	}
}

// codeBlock lays out preformatted text on a tinted panel, wrapped at the
// panel's edge (by character: code has no word boundaries to trust).
func (l *layouter) codeBlock(text string, x, y, w float32) float32 {
	st := l.st
	f := Face{Mono: true, Size: st.Size * 0.9}
	pad := st.Size * 0.6
	lh := l.m.LineHeight(f) * 1.15
	top := y
	y += pad
	inner := w - 2*pad
	text = strings.TrimSuffix(text, "\n")
	var lines []string
	for _, ln := range strings.Split(text, "\n") {
		ln = expandTabs(ln) // a font draws a tab as nothing
		for ln != "" && l.m.Width(f, ln) > inner {
			cut := fitRunes(l.m, f, ln, inner)
			lines = append(lines, ln[:cut])
			ln = ln[cut:]
		}
		lines = append(lines, ln)
	}
	bgAt := len(l.page.Ops)
	l.op(Op{}) // the panel, filled in once its height is known
	for _, ln := range lines {
		if ln != "" {
			l.op(Op{Kind: OpText, X: x + pad, Y: y, Face: f, Text: ln, Color: st.Code})
		}
		y += lh
	}
	y += pad
	l.page.Ops[bgAt] = Op{Kind: OpRect, X: x, Y: top, W: w, H: y - top, Color: st.CodeBg, Radius: 6}
	return y
}

// fitRunes is how many bytes of s fit in w (at least one rune).
func fitRunes(m Metrics, f Face, s string, w float32) int {
	n := 0
	for i := range s {
		if i > 0 && m.Width(f, s[:i]) > w {
			break
		}
		n = i
	}
	if n == 0 {
		_, sz := utf8.DecodeRuneInString(s)
		return sz
	}
	return n
}

func (l *layouter) lineH(f Face) float32 { return l.m.LineHeight(f) * l.st.LineSpread }

// footnotes lays out the notes at the end of the page: under a short
// rule, smaller, numbered, each ending with a way back to where it was
// referred to.
func (l *layouter) footnotes(n *md.Node, x, y, w float32) float32 {
	y += l.gap()
	l.op(Op{Kind: OpRect, X: x, Y: y, W: min(w, l.st.Size*12), H: 1, Color: l.st.Rule})
	y += l.gap()
	small := *l.st
	small.Size *= 0.88
	full := l.st
	l.st = &small
	defer func() { l.st = full }()
	body := Face{Size: small.Size}
	gutter := l.m.Width(body, strconv.Itoa(len(n.Children))+".") + small.Size*0.6
	for i, def := range n.Children {
		if i > 0 {
			y += small.Size * 0.4
		}
		l.anchor("fn-"+def.Label, y)
		num := strconv.Itoa(def.Start) + "."
		l.op(Op{Kind: OpText, X: x + gutter - l.m.Width(body, num) - small.Size*0.4, Y: y, Face: body, Text: num, Color: small.Muted})
		kids := def.Children
		back := &md.Node{Kind: md.Link, Dest: "#fnref-" + def.Label, Children: []*md.Node{{Kind: md.Text, Text: "↑"}}}
		if k := len(kids); k > 0 && kids[k-1].Kind == md.Paragraph {
			// The way back joins the note's last line.
			last := *kids[k-1]
			last.Children = append(append([]*md.Node{}, last.Children...), &md.Node{Kind: md.Text, Text: " "}, back)
			kids = append(append([]*md.Node{}, kids[:k-1]...), &last)
		} else {
			kids = append(append([]*md.Node{}, kids...), &md.Node{Kind: md.Paragraph, Children: []*md.Node{back}})
		}
		y = l.blocks(kids, x+gutter, y, w-gutter, 0)
	}
	return y
}

// expandTabs turns tabs into spaces at four-column stops, for drawing.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}
