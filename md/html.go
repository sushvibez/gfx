package md

import (
	"fmt"
	"strings"
)

// HTML renders a document as HTML, the way the CommonMark reference does:
// one block a line, paragraphs in tight lists unwrapped, raw HTML passed
// through. It is what the parser's tests compare against, and an export.
func HTML(doc *Node) string {
	var b strings.Builder
	renderHTML(&b, doc, false)
	if doc.GFM {
		return filterTags(b.String())
	}
	return b.String()
}

// filterTags is GitHub's tag filter: raw HTML that would change how the
// rest of the page is read — a <title>, a <style>, a <script> — has its <
// written as &lt;, open and close alike. Only raw HTML can contain these:
// text and attributes are already escaped.
func filterTags(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	low := strings.ToLower(s)
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		j := i + 1
		if j < len(s) && s[j] == '/' {
			j++
		}
		for _, t := range filteredTags {
			if strings.HasPrefix(low[j:], t) {
				if k := j + len(t); k == len(s) || strings.IndexByte(" \t\n/>", s[k]) >= 0 {
					b.WriteString(s[last:i])
					b.WriteString("&lt;")
					last = i + 1
				}
				break
			}
		}
	}
	b.WriteString(s[last:])
	return b.String()
}

var filteredTags = []string{"title", "textarea", "style", "xmp", "iframe", "noembed", "noframes", "script", "plaintext"}

func renderHTML(b *strings.Builder, n *Node, tight bool) {
	esc := escapeHTML
	kids := func(t bool) {
		for _, c := range n.Children {
			renderHTML(b, c, t)
		}
	}
	switch n.Kind {
	case Document:
		kids(false)
	case Paragraph:
		if tight {
			kids(false)
			return
		}
		b.WriteString("<p>")
		kids(false)
		b.WriteString("</p>\n")
	case Heading:
		fmt.Fprintf(b, "<h%d>", n.Level)
		kids(false)
		fmt.Fprintf(b, "</h%d>\n", n.Level)
	case ThematicBreak:
		b.WriteString("<hr />\n")
	case BlockQuote:
		b.WriteString("<blockquote>\n")
		kids(false)
		b.WriteString("</blockquote>\n")
	case List:
		tag := "ul"
		if n.Ordered {
			tag = "ol"
		}
		if n.Ordered && n.Start != 1 {
			fmt.Fprintf(b, "<ol start=\"%d\">\n", n.Start)
		} else {
			fmt.Fprintf(b, "<%s>\n", tag)
		}
		for _, c := range n.Children {
			renderHTML(b, c, n.Tight)
		}
		fmt.Fprintf(b, "</%s>\n", tag)
	case ListItem:
		b.WriteString("<li>")
		switch n.Task {
		case TaskOpen:
			b.WriteString(`<input type="checkbox" disabled="" /> `)
		case TaskDone:
			b.WriteString(`<input type="checkbox" checked="" disabled="" /> `)
		}
		// A tight list's paragraphs are written bare; every other block
		// starts on a line of its own.
		for _, c := range n.Children {
			if !(tight && c.Kind == Paragraph) {
				cr(b)
			}
			renderHTML(b, c, tight)
		}
		if k := len(n.Children); k > 0 && !(tight && n.Children[k-1].Kind == Paragraph) {
			cr(b)
		}
		b.WriteString("</li>\n")
	case CodeBlock:
		b.WriteString("<pre><code")
		if lang := strings.Fields(n.Info); len(lang) > 0 {
			fmt.Fprintf(b, " class=\"language-%s\"", esc(lang[0]))
		}
		b.WriteString(">")
		b.WriteString(esc(n.Text))
		b.WriteString("</code></pre>\n")
	case HTMLBlock:
		b.WriteString(n.Text)
		b.WriteString("\n")
	case Table:
		b.WriteString("<table>\n")
		for i, r := range n.Children {
			if i == 0 {
				b.WriteString("<thead>\n")
			}
			if i == 1 {
				b.WriteString("<tbody>\n")
			}
			renderHTML(b, r, false)
			if i == 0 {
				b.WriteString("</thead>\n")
			}
		}
		if len(n.Children) > 1 {
			b.WriteString("</tbody>\n")
		}
		b.WriteString("</table>\n")
	case TableRow:
		b.WriteString("<tr>\n")
		for _, c := range n.Children {
			tag := "td"
			if n.Header {
				tag = "th"
			}
			b.WriteString("<" + tag)
			switch c.Cell {
			case AlignLeft:
				b.WriteString(` align="left"`)
			case AlignCenter:
				b.WriteString(` align="center"`)
			case AlignRight:
				b.WriteString(` align="right"`)
			}
			b.WriteString(">")
			for _, k := range c.Children {
				renderHTML(b, k, false)
			}
			b.WriteString("</" + tag + ">\n")
		}
		b.WriteString("</tr>\n")
	case Text:
		b.WriteString(esc(n.Text))
	case SoftBreak:
		b.WriteString("\n")
	case HardBreak:
		b.WriteString("<br />\n")
	case Emph:
		b.WriteString("<em>")
		kids(false)
		b.WriteString("</em>")
	case Strong:
		b.WriteString("<strong>")
		kids(false)
		b.WriteString("</strong>")
	case Strike:
		b.WriteString("<del>")
		kids(false)
		b.WriteString("</del>")
	case Code:
		b.WriteString("<code>" + esc(n.Text) + "</code>")
	case Link:
		fmt.Fprintf(b, `<a href="%s"`, esc(NormalizeURL(n.Dest)))
		if n.Title != "" {
			fmt.Fprintf(b, ` title="%s"`, esc(n.Title))
		}
		b.WriteString(">")
		kids(false)
		b.WriteString("</a>")
	case Image:
		fmt.Fprintf(b, `<img src="%s" alt="%s"`, esc(NormalizeURL(n.Dest)), esc(PlainText(n)))
		if n.Title != "" {
			fmt.Fprintf(b, ` title="%s"`, esc(n.Title))
		}
		b.WriteString(" />")
	case HTMLInline:
		b.WriteString(n.Text)
	case FootnoteRef:
		id := "fnref-" + esc(n.Label)
		if n.Level > 1 {
			id += fmt.Sprintf("-%d", n.Level)
		}
		fmt.Fprintf(b, `<sup class="footnote-ref"><a href="#fn-%s" id="%s" data-footnote-ref>%d</a></sup>`,
			esc(n.Label), id, n.Start)
	case Footnotes:
		b.WriteString("<section class=\"footnotes\" data-footnotes>\n<ol>\n")
		kids(false)
		b.WriteString("</ol>\n</section>\n")
	case FootnoteDef:
		// The way back to each reference goes at the end of the note's
		// last paragraph, as GitHub writes it.
		fmt.Fprintf(b, "<li id=\"fn-%s\">\n", esc(n.Label))
		var back strings.Builder
		for k := 1; k <= max(n.Level, 1); k++ {
			id := "fnref-" + esc(n.Label)
			if k > 1 {
				id += fmt.Sprintf("-%d", k)
			}
			fmt.Fprintf(&back, ` <a href="#%s" class="footnote-backref" data-footnote-backref data-footnote-backref-idx="%d" aria-label="Back to reference %d">↩</a>`, id, n.Start, n.Start)
		}
		for i, c := range n.Children {
			if i == len(n.Children)-1 && c.Kind == Paragraph {
				b.WriteString("<p>")
				for _, k := range c.Children {
					renderHTML(b, k, false)
				}
				b.WriteString(back.String() + "</p>\n")
				back.Reset()
				continue
			}
			renderHTML(b, c, false)
		}
		if back.Len() > 0 {
			b.WriteString(strings.TrimPrefix(back.String(), " ") + "\n")
		}
		b.WriteString("</li>\n")
	case DefList:
		b.WriteString("<dl>\n")
		kids(false)
		b.WriteString("</dl>\n")
	case DefTerm:
		b.WriteString("<dt>")
		kids(false)
		b.WriteString("</dt>\n")
	case DefDesc:
		// A definition of one paragraph is written without the <p>, the
		// way a tight list item is.
		if len(n.Children) == 1 && n.Children[0].Kind == Paragraph {
			b.WriteString("<dd>")
			for _, k := range n.Children[0].Children {
				renderHTML(b, k, false)
			}
			b.WriteString("</dd>\n")
			return
		}
		b.WriteString("<dd>\n")
		kids(false)
		b.WriteString("</dd>\n")
	}
}

// cr starts a new line unless the output is at the start of one.
func cr(b *strings.Builder) {
	if s := b.String(); s != "" && !strings.HasSuffix(s, "\n") {
		b.WriteString("\n")
	}
}

// escapeHTML escapes text for HTML the way the CommonMark reference does:
// &, <, > and ", and nothing else (an apostrophe is left alone).
func escapeHTML(s string) string {
	if !strings.ContainsAny(s, "&<>\"") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// NormalizeURL percent-encodes a link destination the way browsers and
// the CommonMark reference expect: every byte outside the characters a
// URL may carry as they are is written %XX, and an existing %XX escape
// is left alone.
func NormalizeURL(s string) string {
	const safe = ";/?:@&=+$,-_.!~*'()#"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte(safe, c) >= 0:
			b.WriteByte(c)
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
