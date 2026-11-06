package mdrender

import (
	"html"
	"strconv"
	"strings"

	"github.com/sushvibez/gfx/md"
	"github.com/sushvibez/gfx/render"
)

// Inline flow: a paragraph's inlines become a list of styled words, and the
// words are filled into lines no wider than the column.

// span is a run of text in one style.
type span struct {
	text   string
	face   Face
	color  render.RGBA
	link   string
	code   bool // inline code: a tinted box behind it
	strike bool
	brk    bool    // a hard line break
	img    string  // a picture: its destination, and its size in ih
	ih     float32 // the picture's height (its width is the word's)
	iw     float32
	rise   float32 // raised (sup, a footnote's number) or lowered (sub), in lines
	anchor string  // a "#name" that points here
}

// style is what an inline run is drawn in, as HTML tags change it.
type style struct {
	f      Face
	col    render.RGBA
	link   string
	strike bool
	code   bool
	rise   float32
}

// htmlFrame is an open HTML tag and the style it replaced.
type htmlFrame struct {
	tag  string
	prev style
}

// word is a piece of a span that is laid out whole: a word, or the
// spaces between words.
type word struct {
	span
	w     float32
	space bool
}

// placed is a word on a line.
type placed struct {
	word
	x float32
}

type line struct {
	words  []placed
	w, h   float32
	ascent float32
}

// spans flattens inlines into styled runs. Inline HTML is interpreted
// where it is a tag Markdown writers use (bold, italics, code, keys,
// strikethrough, sub- and superscript, small, line breaks, links,
// pictures); other tags are not shown.
func (l *layouter) spans(ns []*md.Node, f Face, col render.RGBA, link string, strike bool, out []span) []span {
	st := l.st
	cur := style{f: f, col: col, link: link, strike: strike}
	var open []htmlFrame
	for _, n := range ns {
		f, col, link, strike := cur.f, cur.col, cur.link, cur.strike
		switch n.Kind {
		case md.Text:
			out = append(out, span{text: strings.ReplaceAll(n.Text, "\t", " "), face: f, color: col, link: link, strike: strike, code: cur.code, rise: cur.rise})
		case md.SoftBreak:
			out = append(out, span{text: " ", face: f, color: col, link: link})
		case md.HardBreak:
			out = append(out, span{brk: true, face: f})
		case md.Emph:
			g := f
			g.Italic = true
			out = l.spans(n.Children, g, col, link, strike, out)
		case md.Strong:
			g := f
			g.Bold = true
			out = l.spans(n.Children, g, col, link, strike, out)
		case md.Strike:
			out = l.spans(n.Children, f, col, link, true, out)
		case md.Code:
			g := f
			g.Mono, g.Bold, g.Italic = true, false, false
			g.Size = f.Size * 0.9
			c := st.Code
			if link != "" {
				c = st.Link
			}
			out = append(out, span{text: n.Text, face: g, color: c, link: link, code: true, strike: strike})
		case md.Link:
			out = l.spans(n.Children, f, st.Link, n.Dest, strike, out)
		case md.Image:
			out = l.image(n.Dest, md.PlainText(n), f, link, out)
		case md.FootnoteRef:
			g := f
			g.Size = f.Size * 0.72
			anchor := "fnref-" + n.Label
			if n.Level > 1 {
				anchor += "-" + strconv.Itoa(n.Level)
			}
			out = append(out, span{text: strconv.Itoa(n.Start), face: g, color: st.Link,
				link: "#fn-" + n.Label, rise: 0.38, anchor: anchor})
		case md.HTMLInline:
			cur, open, out = l.htmlTag(n.Text, cur, open, out)
		}
	}
	return out
}

// image is a picture's span, or its alt text in brackets when there is no
// picture to show (none asked for, or not loaded) — so the reader knows
// something is meant to be there.
func (l *layouter) image(dest, alt string, f Face, link string, out []span) []span {
	if l.imgs != nil {
		if iw, ih, ok := l.imgs.Size(dest); ok && iw > 0 && ih > 0 {
			return append(out, span{img: dest, iw: iw, ih: ih, text: alt, face: f, link: link})
		}
	}
	if alt == "" {
		alt = dest
	}
	g := f
	g.Italic = true
	return append(out, span{text: "[" + alt + "]", face: g, color: l.st.Muted, link: dest})
}

// htmlTag applies one HTML tag to the running style.
func (l *layouter) htmlTag(raw string, cur style, open []htmlFrame, out []span) (style, []htmlFrame, []span) {
	name, closing, attrs, ok := parseTag(raw)
	if !ok {
		return cur, open, out // a comment, or not a tag at all
	}
	brk := func() {
		if len(out) > 0 && !out[len(out)-1].brk {
			out = append(out, span{brk: true, face: cur.f})
		}
	}
	if closing {
		switch name {
		case "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "dd", "dt":
			brk()
		}
		for i := len(open) - 1; i >= 0; i-- {
			if open[i].tag == name {
				cur = open[i].prev
				open = open[:i]
				break
			}
		}
		return cur, open, out
	}
	switch name {
	case "br":
		out = append(out, span{brk: true, face: cur.f})
		return cur, open, out
	case "img":
		return cur, open, l.image(attrs["src"], attrs["alt"], cur.f, cur.link, out)
	case "hr":
		brk()
		return cur, open, out
	}
	next := cur
	switch name {
	case "b", "strong":
		next.f.Bold = true
	case "i", "em", "cite", "var", "dfn":
		next.f.Italic = true
	case "code", "kbd", "samp", "tt":
		next.f.Mono, next.f.Bold, next.f.Italic = true, false, false
		next.f.Size = cur.f.Size * 0.9
		next.code, next.col = true, l.st.Code
	case "s", "del", "strike":
		next.strike = true
	case "sub":
		next.f.Size = cur.f.Size * 0.75
		next.rise = -0.12
	case "sup":
		next.f.Size = cur.f.Size * 0.75
		next.rise = 0.35
	case "small":
		next.f.Size = cur.f.Size * 0.85
	case "a":
		if href := attrs["href"]; href != "" {
			next.link, next.col = href, l.st.Link
		}
	case "h1", "h2", "h3", "h4", "h5", "h6":
		brk()
		next.f.Bold = true
		next.f.Size = l.st.Size * headingScale[int(name[1]-'0')]
	case "p", "div", "li", "tr", "blockquote", "dd", "dt":
		brk()
	default:
		return cur, open, out // a tag Markdown has no use for: not drawn
	}
	return next, append(open, htmlFrame{tag: name, prev: cur}), out
}

// parseTag reads an HTML tag's name, whether it closes, and its
// attributes (names lower-cased, values with entities resolved).
func parseTag(raw string) (name string, closing bool, attrs map[string]string, ok bool) {
	t := strings.TrimSpace(raw)
	if !strings.HasPrefix(t, "<") || !strings.HasSuffix(t, ">") || strings.HasPrefix(t, "<!") || strings.HasPrefix(t, "<?") {
		return "", false, nil, false
	}
	t = strings.TrimSuffix(strings.TrimSuffix(t[1:len(t)-1], "/"), " ")
	if strings.HasPrefix(t, "/") {
		closing, t = true, t[1:]
	}
	i := 0
	for i < len(t) && (t[i] >= 'a' && t[i] <= 'z' || t[i] >= 'A' && t[i] <= 'Z' || t[i] >= '0' && t[i] <= '9' || t[i] == '-') {
		i++
	}
	if i == 0 {
		return "", false, nil, false
	}
	name = strings.ToLower(t[:i])
	attrs = map[string]string{}
	rest := t[i:]
	for {
		rest = strings.TrimLeft(rest, " \t\n")
		j := strings.IndexAny(rest, "= \t\n")
		if rest == "" {
			break
		}
		if j < 0 {
			attrs[strings.ToLower(rest)] = ""
			break
		}
		key := strings.ToLower(rest[:j])
		rest = strings.TrimLeft(rest[j:], " \t\n")
		if !strings.HasPrefix(rest, "=") {
			attrs[key] = ""
			continue
		}
		rest = strings.TrimLeft(rest[1:], " \t\n")
		var val string
		if rest != "" && (rest[0] == '"' || rest[0] == '\'') {
			q := rest[0]
			e := strings.IndexByte(rest[1:], q)
			if e < 0 {
				val, rest = rest[1:], ""
			} else {
				val, rest = rest[1:1+e], rest[2+e:]
			}
		} else {
			e := strings.IndexAny(rest, " \t\n")
			if e < 0 {
				e = len(rest)
			}
			val, rest = rest[:e], rest[e:]
		}
		attrs[key] = html.UnescapeString(val)
	}
	return name, closing, attrs, true
}

// htmlBlock draws a block of raw HTML as the text and pictures it holds —
// the README way of centring a logo and a row of badges. Scripts and
// styles are not shown, and <pre> keeps its lines.
func (l *layouter) htmlBlock(src string, x, y, w float32) float32 {
	low := strings.ToLower(src)
	if strings.HasPrefix(strings.TrimSpace(low), "<pre") {
		inner := src
		if i := strings.Index(inner, ">"); i >= 0 {
			inner = inner[i+1:]
		}
		if i := strings.LastIndex(strings.ToLower(inner), "</pre"); i >= 0 {
			inner = inner[:i]
		}
		return l.codeBlock(html.UnescapeString(stripTags(inner)), x, y, w)
	}
	var ns []*md.Node
	align := md.AlignNone
	for i := 0; i < len(src); {
		if src[i] == '<' {
			e := strings.IndexByte(src[i:], '>')
			if e < 0 {
				ns = append(ns, &md.Node{Kind: md.Text, Text: src[i:]})
				break
			}
			tag := src[i : i+e+1]
			name, closing, attrs, ok := parseTag(tag)
			if ok && !closing && (name == "script" || name == "style") {
				if c := strings.Index(strings.ToLower(src[i:]), "</"+name); c >= 0 {
					i += c
					continue
				}
			}
			if ok && !closing && align == md.AlignNone {
				switch strings.ToLower(attrs["align"]) {
				case "center":
					align = md.AlignCenter
				case "right":
					align = md.AlignRight
				}
			}
			ns = append(ns, &md.Node{Kind: md.HTMLInline, Text: tag})
			i += e + 1
			continue
		}
		e := strings.IndexByte(src[i:], '<')
		if e < 0 {
			e = len(src) - i
		}
		// HTML's whitespace: any run of it is one space.
		txt := strings.Join(strings.Fields(html.UnescapeString(src[i:i+e])), " ")
		if txt != "" {
			if c := src[i]; c == ' ' || c == '\n' || c == '\t' {
				txt = " " + txt
			}
			if c := src[i+e-1]; c == ' ' || c == '\n' || c == '\t' {
				txt += " "
			}
			ns = append(ns, &md.Node{Kind: md.Text, Text: txt})
		}
		i += e
	}
	sp := l.spans(ns, Face{Size: l.st.Size}, l.st.Text, "", false, nil)
	for len(sp) > 0 && sp[len(sp)-1].brk {
		sp = sp[:len(sp)-1]
	}
	visible := false
	for _, s := range sp {
		visible = visible || s.img != "" || strings.TrimSpace(s.text) != ""
	}
	if !visible {
		return y
	}
	return l.flowSpans(sp, x, y, w, Face{Size: l.st.Size}, align)
}

func stripTags(s string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		e := strings.IndexByte(s[i:], '>')
		if e < 0 {
			return b.String()
		}
		s = s[i+e+1:]
	}
}

// words splits spans into words and the spaces between them, measured.
func (l *layouter) words(sp []span) []word {
	var out []word
	for _, s := range sp {
		if s.brk {
			out = append(out, word{span: s})
			continue
		}
		if s.img != "" {
			out = append(out, word{span: s, w: s.iw})
			continue
		}
		if s.code {
			// Inline code wraps at its spaces too, but keeps them.
			for _, part := range splitKeep(s.text) {
				w := s
				w.text = part
				out = append(out, word{span: w, w: l.m.Width(s.face, part)})
			}
			continue
		}
		for _, part := range splitKeep(s.text) {
			w := s
			w.text = part
			sp := strings.TrimSpace(part) == ""
			if sp {
				w.text = " "
			}
			out = append(out, word{span: w, w: l.m.Width(s.face, w.text), space: sp})
		}
	}
	return out
}

// splitKeep splits text at runs of spaces, keeping each run as its own
// piece.
func splitKeep(s string) []string {
	var out []string
	start := 0
	for i := 1; i <= len(s); i++ {
		if i == len(s) || (s[i] == ' ') != (s[start] == ' ') {
			out = append(out, s[start:i])
			start = i
		}
	}
	return out
}

// fill breaks words into lines no wider than w.
func (l *layouter) fill(ws []word, w float32) []line {
	var lines []line
	var cur line
	push := func() {
		// Spaces at the end of a line take no room.
		for len(cur.words) > 0 && cur.words[len(cur.words)-1].space {
			cur.w -= cur.words[len(cur.words)-1].w
			cur.words = cur.words[:len(cur.words)-1]
		}
		lines = append(lines, cur)
		cur = line{}
	}
	for _, wd := range ws {
		if wd.brk {
			cur.h = max(cur.h, l.lineH(wd.face))
			push()
			continue
		}
		if wd.img != "" && wd.w > w && w > 0 {
			// A picture wider than the column shrinks to fit it.
			wd.ih *= w / wd.w
			wd.w = w
		}
		cur.h = max(cur.h, l.wordH(wd))
		if wd.space && len(cur.words) == 0 {
			continue // no space at the start of a line
		}
		if cur.w+wd.w > w && len(cur.words) > 0 && !wd.space {
			push()
			cur.h = l.wordH(wd)
		}
		// A word wider than the whole line is broken where it must be.
		for !wd.space && wd.img == "" && wd.w > w && len(cur.words) == 0 {
			cut := fitRunes(l.m, wd.face, wd.text, w)
			head := wd
			head.text = wd.text[:cut]
			head.w = l.m.Width(wd.face, head.text)
			cur.words = append(cur.words, placed{word: head, x: 0})
			cur.w = head.w
			push()
			cur.h = l.lineH(wd.face)
			wd.text = wd.text[cut:]
			wd.w = l.m.Width(wd.face, wd.text)
		}
		cur.words = append(cur.words, placed{word: wd, x: cur.w})
		cur.w += wd.w
	}
	if len(cur.words) > 0 || len(lines) == 0 {
		push()
	}
	return lines
}

// wordH is how tall a word makes its line.
func (l *layouter) wordH(wd word) float32 {
	if wd.img != "" {
		return wd.ih
	}
	return l.lineH(wd.face)
}

// flow lays inlines out as a paragraph from y and returns where it ends.
func (l *layouter) flow(ns []*md.Node, x, y, w float32, f Face, col render.RGBA, align md.Align) float32 {
	return l.flowSpans(l.spans(ns, f, col, "", false, nil), x, y, w, f, align)
}

// flowSpans is flow for runs already styled.
func (l *layouter) flowSpans(sp []span, x, y, w float32, f Face, align md.Align) float32 {
	lines := l.fill(l.words(sp), w)
	for _, ln := range lines {
		if ln.h == 0 {
			ln.h = l.lineH(f)
		}
		dx := float32(0)
		switch align {
		case md.AlignCenter:
			dx = (w - ln.w) / 2
		case md.AlignRight:
			dx = w - ln.w
		}
		l.placeLine(ln, x+dx, y)
		y += ln.h
	}
	return y
}

// placeLine turns a line's words into ops. Every word sits on the line's
// baseline: a smaller face is pushed down by the difference.
func (l *layouter) placeLine(ln line, x, y float32) {
	st := l.st
	for i := 0; i < len(ln.words); i++ {
		p := ln.words[i]
		if p.img != "" {
			// A picture stands on the bottom of its line.
			top := y + ln.h - p.ih
			l.op(Op{Kind: OpImage, X: x + p.x, Y: top, W: p.w, H: p.ih, Text: p.img, Color: render.White})
			if p.link != "" {
				l.page.Links = append(l.page.Links, LinkBox{X: x + p.x, Y: top, W: p.w, H: p.ih, Dest: p.link})
			}
			continue
		}
		lh := l.lineH(p.face)
		wy := y + (ln.h - lh) + (lh-l.m.LineHeight(p.face))/2
		wy -= p.rise * l.m.LineHeight(Face{Size: st.Size})
		wx := x + p.x
		if p.anchor != "" {
			l.anchor(p.anchor, y)
		}
		if p.code && !p.space {
			pad := st.Size * 0.18
			l.op(Op{Kind: OpRect, X: wx - pad, Y: wy - 1, W: p.w + 2*pad, H: l.m.LineHeight(p.face) + 2, Color: st.CodeBg, Radius: 3})
		}
		if !p.space || p.code {
			l.op(Op{Kind: OpText, X: wx, Y: wy, Face: p.face, Text: p.text, Color: p.color})
		}
		th := l.m.LineHeight(p.face)
		if p.link != "" && p.rise != 0 {
			// A raised link (a footnote's number) is not underlined.
			l.page.Links = append(l.page.Links, LinkBox{X: wx, Y: y, W: p.w, H: ln.h, Dest: p.link})
		} else if p.link != "" {
			// The underline runs under the spaces between a link's words
			// as well, so a link reads as one thing.
			l.op(Op{Kind: OpRect, X: wx, Y: wy + th*0.92, W: p.w, H: 1, Color: p.color})
			l.page.Links = append(l.page.Links, LinkBox{X: wx, Y: y, W: p.w, H: ln.h, Dest: p.link})
		}
		if p.strike {
			l.op(Op{Kind: OpRect, X: wx, Y: wy + th*0.55, W: p.w, H: 1, Color: p.color})
		}
	}
}

// table lays a table out: columns as wide as their content where the
// width allows, shared out in proportion where it does not.
func (l *layouter) table(n *md.Node, x, y, w float32) float32 {
	st := l.st
	cols := len(n.Align)
	if cols == 0 || len(n.Children) == 0 {
		return y
	}
	pad := st.Size * 0.5
	body := Face{Size: st.Size}
	head := Face{Size: st.Size, Bold: true}
	natural := make([]float32, cols)
	least := make([]float32, cols)
	for _, row := range n.Children {
		f := body
		if row.Header {
			f = head
		}
		for c, cell := range row.Children {
			if c >= cols {
				break
			}
			var total, longest float32
			for _, wd := range l.words(l.spans(cell.Children, f, st.Text, "", false, nil)) {
				total += wd.w
				if !wd.space {
					longest = max(longest, wd.w)
				}
			}
			natural[c] = max(natural[c], total+2*pad)
			least[c] = max(least[c], min(longest, w/float32(cols))+2*pad)
		}
	}
	widths := make([]float32, cols)
	var sum, sumLeast float32
	for c := range natural {
		sum += natural[c]
		sumLeast += least[c]
	}
	switch {
	case sum <= w:
		copy(widths, natural)
	case sumLeast >= w:
		for c := range widths {
			widths[c] = w * least[c] / sumLeast
		}
	default:
		// Everyone gets their least, and the rest goes where it is wanted.
		spare := w - sumLeast
		want := sum - sumLeast
		for c := range widths {
			widths[c] = least[c] + spare*(natural[c]-least[c])/want
		}
	}
	var tw float32
	for _, cw := range widths {
		tw += cw
	}
	top := y
	for _, row := range n.Children {
		f := body
		if row.Header {
			f = head
		}
		// Lay each cell out, then the row is as tall as its tallest.
		var bg int
		if row.Header {
			bg = len(l.page.Ops)
			l.op(Op{}) // the header's tint, filled in once its height is known
		}
		cx := x
		var bottom float32
		for c := 0; c < cols; c++ {
			var cell *md.Node
			if c < len(row.Children) {
				cell = row.Children[c]
			}
			end := y + pad
			if cell != nil {
				end = l.flow(cell.Children, cx+pad, y+pad, widths[c]-2*pad, f, st.Text, n.Align[c])
			}
			bottom = max(bottom, end+pad)
			cx += widths[c]
		}
		if row.Header {
			l.page.Ops[bg] = Op{Kind: OpRect, X: x, Y: y, W: tw, H: bottom - y, Color: st.HeaderBg}
		}
		// The rule under the row.
		l.op(Op{Kind: OpRect, X: x, Y: bottom, W: tw, H: 1, Color: st.Rule})
		y = bottom + 1
	}
	// The frame and the column rules.
	l.op(Op{Kind: OpFrame, X: x, Y: top, W: tw, H: y - top, Color: st.Rule})
	cx := x
	for c := 0; c < cols-1; c++ {
		cx += widths[c]
		l.op(Op{Kind: OpRect, X: cx, Y: top, W: 1, H: y - top, Color: st.Rule})
	}
	return y
}
