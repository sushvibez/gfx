package md

import (
	"fmt"
	"strings"
	"unicode"
)

// Block structure, CommonMark's way: the document is read a line at a
// time against a stack of open blocks. Each line first continues as many
// of the open containers as it can (a quote's ">", an item's indent), then
// may open new containers and at most one new leaf, and what is left of it
// is the leaf's content. Inline structure — emphasis, links, code spans —
// is parsed only once every block is closed, because a link can refer to a
// definition anywhere in the document.

// Parse reads a document, GitHub's extensions included: tables, task
// lists, strikethrough and bare links.
func Parse(src string) *Node { return parse(src, true) }

// ParseCommonMark reads a document as strict CommonMark, without the
// extensions — what the spec's own examples are checked against.
func ParseCommonMark(src string) *Node { return parse(src, false) }

func parse(src string, gfm bool) *Node {
	p := &parser{refs: map[string]ref{}, gfm: gfm}
	p.doc = &block{node: &Node{Kind: Document, Line: 1, GFM: gfm}}
	p.stack = []*block{p.doc}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	src = strings.TrimSuffix(src, "\n")
	for i, ln := range strings.Split(src, "\n") {
		p.line = i + 1
		p.setLine(ln)
		p.addLine(p.exp)
	}
	for len(p.stack) > 0 {
		p.close(p.stack[len(p.stack)-1])
		p.stack = p.stack[:len(p.stack)-1]
	}
	if gfm {
		p.takeFootnotes(p.doc.node)
	}
	p.inlines(p.doc.node)
	if gfm {
		p.placeFootnotes(p.doc.node)
		slugHeadings(p.doc.node)
	}
	return p.doc.node
}

// ref is a link reference definition.
type ref struct{ dest, title string }

type parser struct {
	doc   *block
	stack []*block
	refs  map[string]ref
	line  int
	gfm   bool // GitHub's extensions
	// Footnote definitions by label, and the ones referred to, in the
	// order they were first referred to.
	notes     map[string]*Node
	noteOrder []*Node
	// The line being read: as written (orig), with its tabs expanded to
	// four-column stops (exp), and where each byte of exp came from.
	orig, exp string
	from      []int  // exp byte → orig byte
	tabFill   []bool // exp byte is a tab's second or later column
}

// setLine takes a new line, expanding its tabs for the structure (every
// indent rule is a count of columns) while remembering the original, so
// content keeps its tabs (see keep).
func (p *parser) setLine(ln string) {
	p.orig = ln
	p.from = p.from[:0]
	p.tabFill = p.tabFill[:0]
	if !strings.Contains(ln, "\t") {
		p.exp = ln
		return
	}
	var b strings.Builder
	col := 0
	for i, r := range ln {
		if r == '\t' {
			n := 4 - col%4
			for k := 0; k < n; k++ {
				b.WriteByte(' ')
				p.from = append(p.from, i)
				p.tabFill = append(p.tabFill, k > 0)
			}
			col += n
			continue
		}
		w := b.Len()
		b.WriteRune(r)
		for k := w; k < b.Len(); k++ {
			p.from = append(p.from, i+(k-w))
			p.tabFill = append(p.tabFill, false)
		}
		col++
	}
	p.exp = b.String()
}

// keep turns content cut from the expanded line back into what was
// written: tabs stay tabs, except the columns of one the indentation only
// partly consumed, which CommonMark says become spaces.
func (p *parser) keep(s string) string {
	if len(p.from) == 0 {
		return s
	}
	off := len(p.exp) - len(s)
	if off < 0 || off > len(p.exp) || p.exp[off:] != s {
		return s // not a suffix of this line: leave it as it is
	}
	if off == len(p.exp) {
		return ""
	}
	j := p.from[off]
	if !p.tabFill[off] {
		return p.orig[j:]
	}
	end := off
	for end < len(p.exp) && p.tabFill[end] && p.from[end] == j {
		end++
	}
	return strings.Repeat(" ", end-off) + p.orig[j+1:]
}

// block is an open block while the document is read.
type block struct {
	node *Node
	// lines is a leaf's content so far: a paragraph's text, a code
	// block's lines, a table's rows.
	lines []string
	// A fenced code block's fence: the character, its length, and how
	// far it was indented (that much is taken off each content line).
	fence       byte
	fenceLen    int
	fenceIndent int
	// indent is how far a list item's content is indented.
	indent int
	// empty: a list item that opened on a blank line and has had no
	// content yet.
	empty bool
	// htmlEnd is what closes an HTML block ("" = a blank line).
	htmlEnd string
}

func (p *parser) tip() *block { return p.stack[len(p.stack)-1] }

// addLine reads one line.
func (p *parser) addLine(ln string) {
	pos := 0
	matched := 1 // the document always continues
	// 1. Continue the open blocks.
	for ; matched < len(p.stack); matched++ {
		b := p.stack[matched]
		rest := ln[min(pos, len(ln)):]
		ind := indentOf(rest)
		blank := isBlank(rest)
		switch b.node.Kind {
		case BlockQuote:
			if !blank && ind <= 3 && rest[ind] == '>' {
				pos += ind + 1
				if pos < len(ln) && ln[pos] == ' ' {
					pos++
				}
				continue
			}
		case List, DefList:
			continue // a list goes on as long as an item does, or a new one starts
		case FootnoteDef, DefDesc:
			// Like a list item's content: indented under it, blank lines
			// inside it allowed.
			if blank {
				pos = len(ln)
				continue
			}
			if ind >= b.indent {
				pos += b.indent
				continue
			}
		case ListItem:
			if blank {
				if b.empty && len(b.node.Children) == 0 && len(b.lines) == 0 {
					break // an item can start with one blank line, not two
				}
				pos = len(ln)
				continue
			}
			if ind >= b.indent {
				pos += b.indent
				b.empty = false
				continue
			}
		case CodeBlock:
			if b.fence != 0 {
				if ind <= 3 && closesFence(rest[ind:], b.fence, b.fenceLen) {
					p.touch(matched + 1)
					p.closeFrom(matched)
					return
				}
				b.lines = append(b.lines, p.keep(stripIndent(rest, b.fenceIndent)))
				p.touch(matched + 1)
				return
			}
			if ind >= 4 {
				b.lines = append(b.lines, p.keep(rest[4:]))
				p.touch(matched + 1)
				return
			}
			if blank {
				b.lines = append(b.lines, "")
				return
			}
		case HTMLBlock:
			if b.htmlEnd == "" && blank {
				break
			}
			b.lines = append(b.lines, p.keep(rest))
			p.touch(matched + 1)
			if b.htmlEnd != "" && strings.Contains(strings.ToLower(rest), b.htmlEnd) {
				p.closeFrom(matched)
			}
			return
		case Paragraph, Table:
			if blank {
				break
			}
			continue
		case Heading, ThematicBreak:
		}
		break
	}
	allMatched := matched == len(p.stack)
	p.touch(matched)
	if pos > len(ln) {
		pos = len(ln)
	}

	// 2. Open new containers, then perhaps a leaf.
	container := p.stack[matched-1]
	opened := false
	for {
		rest := ln[pos:]
		ind := indentOf(rest)
		blank := isBlank(rest)
		tipPara := !opened && allMatched && p.tip().node.Kind == Paragraph
		if blank {
			break
		}
		if ind >= 4 {
			// Indented code, unless it would interrupt a paragraph (or
			// be a lazy continuation of one).
			if tipPara || (!allMatched && !opened && p.tip().node.Kind == Paragraph) {
				break
			}
			p.closeFrom(matched)
			b := p.addBlock(&block{node: &Node{Kind: CodeBlock}})
			b.lines = append(b.lines, p.keep(rest[4:]))
			return
		}
		s := rest[ind:]
		// A block quote.
		if s[0] == '>' {
			p.closeFrom(matched)
			p.addBlock(&block{node: &Node{Kind: BlockQuote}})
			matched, opened = len(p.stack), true
			pos += ind + 1
			if pos < len(ln) && ln[pos] == ' ' {
				pos++
			}
			container = p.tip()
			continue
		}
		// Setext heading underline: turns the paragraph above into a
		// heading. Checked before a thematic break, which "---" also is.
		if tipPara && (s[0] == '=' || s[0] == '-') && isSetext(s) && p.headingLeft(p.tip()) {
			b := p.tip()
			b.node.Kind = Heading
			b.node.Level = 1
			if s[0] == '-' {
				b.node.Level = 2
			}
			p.closeFrom(len(p.stack) - 1)
			return
		}
		if isThematicBreak(s) {
			p.closeFrom(matched)
			p.addBlock(&block{node: &Node{Kind: ThematicBreak}})
			p.closeFrom(len(p.stack) - 1)
			return
		}
		if lvl, text, ok := atxHeading(s); ok {
			p.closeFrom(matched)
			b := p.addBlock(&block{node: &Node{Kind: Heading, Level: lvl}})
			b.lines = append(b.lines, text)
			p.closeFrom(len(p.stack) - 1)
			return
		}
		if ch, n, info, ok := openFence(s); ok {
			p.closeFrom(matched)
			p.addBlock(&block{node: &Node{Kind: CodeBlock, Info: info}, fence: ch, fenceLen: n, fenceIndent: ind})
			return
		}
		if end, ok := htmlStart(s, tipPara); ok {
			p.closeFrom(matched)
			b := p.addBlock(&block{node: &Node{Kind: HTMLBlock}, htmlEnd: end})
			b.lines = append(b.lines, p.keep(rest))
			if end != "" && strings.Contains(strings.ToLower(s[1:]), end) {
				p.closeFrom(len(p.stack) - 1)
			}
			return
		}
		// A table: a one-line paragraph followed by a delimiter row with
		// as many cells.
		if p.gfm && tipPara && len(p.tip().lines) == 1 {
			if aligns, ok := delimiterRow(s); ok && len(splitRow(p.tip().lines[0])) == len(aligns) {
				b := p.tip()
				b.node.Kind = Table
				b.node.Align = aligns
				return
			}
		}
		// GitHub's footnote definition: [^label]: text, its content
		// indented four under it after the first line.
		if p.gfm && strings.HasPrefix(s, "[^") {
			if label, n, ok := footnoteStart(s); ok {
				p.closeFrom(matched)
				p.addBlock(&block{node: &Node{Kind: FootnoteDef, Label: label}, indent: 4})
				matched, opened = len(p.stack), true
				pos += ind + n
				container = p.tip()
				continue
			}
		}
		// A definition: ": text" under a term (the paragraph before it,
		// a line a term), or under another definition of the same list.
		if p.gfm && len(s) > 1 && s[0] == ':' && (s[1] == ' ' || s[1] == '\t') && (tipPara || container.node.Kind == DefList) {
			if tipPara {
				p.termsFromParagraph()
			} else {
				p.closeFrom(matched) // the definition before this one
			}
			w := 1
			for w < len(s) && w < 5 && s[w] == ' ' {
				w++
			}
			p.addBlock(&block{node: &Node{Kind: DefDesc}, indent: ind + w})
			matched, opened = len(p.stack), true
			pos += ind + w
			container = p.tip()
			continue
		}
		// A list item.
		if it, ok := listMarker(s, tipPara); ok {
			p.closeFrom(matched)
			// Into the open list if it is the same kind, or a new list.
			top := p.tip()
			if top.node.Kind != List || top.node.Ordered != it.ordered || top.node.Delim != it.delim {
				if top.node.Kind == List {
					p.closeFrom(len(p.stack) - 1)
				}
				p.addBlock(&block{node: &Node{Kind: List, Ordered: it.ordered, Start: it.start, Delim: it.delim, Tight: true}})
			}
			item := p.addBlock(&block{node: &Node{Kind: ListItem}, indent: ind + it.width, empty: it.blank})
			matched, opened = len(p.stack), true
			pos += min(ind+it.width, len(rest))
			container = item
			// A task list item: "[ ] " or "[x] " at the start of the content.
			rest2 := ln[pos:]
			if p.gfm && len(rest2) >= 3 && rest2[0] == '[' && rest2[2] == ']' && (len(rest2) == 3 || rest2[3] == ' ') {
				switch rest2[1] {
				case ' ':
					item.node.Task = TaskOpen
				case 'x', 'X':
					item.node.Task = TaskDone
				}
				if item.node.Task != NoTask {
					pos += min(4, len(rest2))
				}
			}
			continue
		}
		break
	}
	_ = container

	// 3. What is left is text, or a blank line.
	rest := ln[pos:]
	if isBlank(rest) {
		// A blank line ends a paragraph or a table, and marks the
		// containers it passed through.
		if !allMatched && !opened {
			p.closeFrom(matched)
		}
		if t := p.tip(); t.node.Kind == Paragraph || t.node.Kind == Table {
			p.closeFrom(len(p.stack) - 1)
		}
		return
	}
	// A lazy continuation: a paragraph goes on even when the containers
	// round it did not (a quoted paragraph's next line without its ">").
	if !allMatched && !opened && p.tip().node.Kind == Paragraph {
		p.tip().lines = append(p.tip().lines, p.keep(rest))
		p.touch(len(p.stack))
		return
	}
	if !opened {
		p.closeFrom(matched)
	}
	switch t := p.tip(); t.node.Kind {
	case Paragraph, Table:
		t.lines = append(t.lines, p.keep(rest))
	case List, DefList:
		// Text at a list's level ends the list.
		p.closeFrom(len(p.stack) - 1)
		fallthrough
	default:
		b := p.addBlock(&block{node: &Node{Kind: Paragraph}})
		b.lines = append(b.lines, p.keep(rest))
	}
}

// addBlock opens a block as a child of the innermost open container.
func (p *parser) addBlock(b *block) *block {
	// A leaf cannot hold children: close it first.
	for {
		t := p.tip()
		switch t.node.Kind {
		case Document, BlockQuote, ListItem, FootnoteDef, DefDesc:
		case List:
			if b.node.Kind == ListItem {
				break
			}
			p.closeFrom(len(p.stack) - 1)
			continue
		case DefList:
			if b.node.Kind == DefDesc {
				break
			}
			p.closeFrom(len(p.stack) - 1)
			continue
		default:
			p.closeFrom(len(p.stack) - 1)
			continue
		}
		break
	}
	parent := p.tip()
	b.node.Line, b.node.EndLine = p.line, p.line
	parent.node.Children = append(parent.node.Children, b.node)
	p.stack = append(p.stack, b)
	return b
}

// closeFrom closes every open block from index i up.
func (p *parser) closeFrom(i int) {
	for len(p.stack) > i {
		p.close(p.tip())
		p.stack = p.stack[:len(p.stack)-1]
	}
}

// close finishes a block's content.
func (p *parser) close(b *block) {
	n := b.node
	switch n.Kind {
	case Paragraph:
		text := strings.Join(b.lines, "\n")
		text = p.takeRefs(text)
		if strings.TrimSpace(text) == "" {
			p.remove(n)
			return
		}
		n.Text = strings.TrimSpace(text)
	case Heading:
		n.Text = strings.TrimSpace(strings.Join(b.lines, "\n"))
	case CodeBlock:
		lines := b.lines
		if b.fence == 0 {
			for len(lines) > 0 && isBlank(lines[len(lines)-1]) {
				lines = lines[:len(lines)-1]
			}
		}
		if len(lines) > 0 {
			n.Text = strings.Join(lines, "\n") + "\n"
		}
	case HTMLBlock:
		n.Text = strings.Join(b.lines, "\n")
	case Table:
		p.closeTable(b)
	case ListItem, FootnoteDef, DefDesc, DefList:
		// An item ends where its last block does: trailing blank lines,
		// and a paragraph that turned out to be only link definitions,
		// are not part of it.
		n.EndLine = n.Line
		if k := len(n.Children); k > 0 {
			n.EndLine = n.Children[k-1].EndLine
		}
	case List:
		n.Tight = !looseList(n)
		if k := len(n.Children); k > 0 {
			n.EndLine = n.Children[k-1].EndLine
		}
	}
}

// headingLeft takes the link definitions off the front of a paragraph a
// setext underline is about to turn into a heading, and reports whether
// any text is left to be the heading. A paragraph that was nothing but
// definitions is not a heading, and the underline is plain text.
func (p *parser) headingLeft(b *block) bool {
	rest := p.takeRefs(strings.Join(b.lines, "\n"))
	if strings.TrimSpace(rest) == "" {
		b.lines = b.lines[:0]
		return false
	}
	b.lines = strings.Split(rest, "\n")
	return true
}

// touch records that this line, if it has content, is part of the first
// n open blocks.
func (p *parser) touch(n int) {
	if isBlank(p.exp) {
		return
	}
	for _, b := range p.stack[:min(n, len(p.stack))] {
		b.node.EndLine = p.line
	}
}

// looseList is CommonMark's rule: a list is loose if any of its items is
// separated from the next by a blank line, or any item holds two blocks
// with a blank line between them. A blank line is a gap between where
// one block's content ends and the next one starts.
func looseList(list *Node) bool {
	gap := func(a, b *Node) bool { return b.Line > a.EndLine+1 }
	for i, it := range list.Children {
		if i+1 < len(list.Children) && gap(it, list.Children[i+1]) {
			return true
		}
		for j := 0; j+1 < len(it.Children); j++ {
			if gap(it.Children[j], it.Children[j+1]) {
				return true
			}
		}
	}
	return false
}

// remove takes an empty paragraph out of its parent.
func (p *parser) remove(n *Node) {
	for _, b := range p.stack {
		for i, c := range b.node.Children {
			if c == n {
				b.node.Children = append(b.node.Children[:i], b.node.Children[i+1:]...)
				return
			}
		}
	}
}

// closeTable turns a table's lines into rows of cells.
func (p *parser) closeTable(b *block) {
	n := b.node
	cols := len(n.Align)
	for i, ln := range b.lines {
		row := &Node{Kind: TableRow, Header: i == 0}
		cells := splitRow(ln)
		for c := 0; c < cols; c++ {
			cell := &Node{Kind: TableCell, Cell: n.Align[c]}
			if c < len(cells) {
				cell.Text = cells[c]
			}
			row.Children = append(row.Children, cell)
		}
		n.Children = append(n.Children, row)
	}
}

// takeRefs takes link reference definitions off the front of a
// paragraph, and returns what is left.
func (p *parser) takeRefs(text string) string {
	for {
		label, dest, title, rest, ok := parseRefDef(text)
		if !ok {
			return text
		}
		key := normLabel(label)
		if _, seen := p.refs[key]; !seen {
			p.refs[key] = ref{dest, title}
		}
		text = rest
	}
}

// inlines parses the inline content of every leaf that has some.
func (p *parser) inlines(n *Node) {
	switch n.Kind {
	case Paragraph, Heading, TableCell, DefTerm:
		n.Children = p.parseInlines(n.Text)
		n.Text = ""
		return
	}
	for _, c := range n.Children {
		p.inlines(c)
	}
}

// --- line tests -----------------------------------------------------------

func indentOf(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

func stripIndent(s string, n int) string {
	i := 0
	for i < n && i < len(s) && s[i] == ' ' {
		i++
	}
	return s[i:]
}

// isThematicBreak: three or more of one of - * _, spaces allowed.
func isThematicBreak(s string) bool {
	var ch byte
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
		case (c == '-' || c == '*' || c == '_') && (ch == 0 || c == ch):
			ch = c
			n++
		default:
			return false
		}
	}
	return n >= 3
}

// isSetext: a line of only = or only -, trailing spaces allowed.
func isSetext(s string) bool {
	t := strings.TrimRight(s, " ")
	if t == "" {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] != t[0] {
			return false
		}
	}
	return true
}

// atxHeading: 1–6 #s, then a space or the end; closing #s are dropped.
func atxHeading(s string) (int, string, bool) {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || (n < len(s) && s[n] != ' ') {
		return 0, "", false
	}
	text := strings.TrimSpace(s[n:])
	// A closing sequence: #s preceded by a space (or the whole text).
	if t := strings.TrimRight(text, "#"); t != text && (t == "" || strings.HasSuffix(t, " ")) {
		text = strings.TrimSpace(t)
	}
	return n, text, true
}

// openFence: three or more ` or ~, then an info string (no backticks in a
// backtick fence's).
func openFence(s string) (byte, int, string, bool) {
	if len(s) < 3 || (s[0] != '`' && s[0] != '~') {
		return 0, 0, "", false
	}
	ch := s[0]
	n := 0
	for n < len(s) && s[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	info := strings.TrimSpace(s[n:])
	if ch == '`' && strings.Contains(info, "`") {
		return 0, 0, "", false
	}
	return ch, n, unescapeString(info), true
}

// closesFence: at least as many of the fence's character, and nothing
// after them but spaces.
func closesFence(s string, ch byte, n int) bool {
	k := 0
	for k < len(s) && s[k] == ch {
		k++
	}
	return k >= n && isBlank(s[k:])
}

// listItem is a list marker as read off a line.
type listItem struct {
	ordered bool
	start   int
	delim   byte
	width   int  // the marker and the spaces after it
	blank   bool // nothing after the marker
}

// listMarker reads a bullet (- + *) or an ordered marker (1. or 1)) and
// the spaces after it. One that would interrupt a paragraph must have
// content after it, and an ordered one must start at 1.
func listMarker(s string, interrupting bool) (listItem, bool) {
	var it listItem
	n := 0
	switch {
	case s[0] == '-' || s[0] == '+' || s[0] == '*':
		it.delim, n = s[0], 1
	default:
		for n < len(s) && n < 9 && s[n] >= '0' && s[n] <= '9' {
			it.start = it.start*10 + int(s[n]-'0')
			n++
		}
		if n == 0 || n >= len(s) || (s[n] != '.' && s[n] != ')') {
			return it, false
		}
		it.ordered, it.delim = true, s[n]
		n++
	}
	if n < len(s) && s[n] != ' ' {
		return it, false
	}
	rest := s[n:]
	it.blank = isBlank(rest)
	if interrupting && (it.blank || (it.ordered && it.start != 1)) {
		return it, false
	}
	sp := indentOf(rest)
	switch {
	case it.blank:
		sp = 1
	case sp >= 5:
		sp = 1 // the rest is indented code inside the item
	case sp == 0:
		sp = 1
	}
	it.width = n + sp
	return it, true
}

// htmlStart recognises the start of an HTML block, and returns the text
// that ends it ("" = a blank line). Kinds 1–6 of CommonMark's seven; the
// seventh (any tag alone on a line) cannot interrupt a paragraph.
func htmlStart(s string, interrupting bool) (string, bool) {
	if len(s) < 2 || s[0] != '<' {
		return "", false
	}
	low := strings.ToLower(s)
	for _, t := range []string{"<script", "<pre", "<style", "<textarea"} {
		if strings.HasPrefix(low, t) && (len(low) == len(t) || strings.ContainsRune(" >\t", rune(low[len(t)]))) {
			return "</" + t[1:] + ">", true
		}
	}
	switch {
	case strings.HasPrefix(s, "<!--"):
		return "-->", true
	case strings.HasPrefix(s, "<?"):
		return "?>", true
	case strings.HasPrefix(s, "<![CDATA["):
		return "]]>", true
	case len(s) > 2 && s[1] == '!' && s[2] >= 'A' && s[2] <= 'Z':
		return ">", true
	}
	name := low[1:]
	name = strings.TrimPrefix(name, "/")
	end := 0
	for end < len(name) && (isAlnum(name[end]) || name[end] == '-') {
		end++
	}
	if end > 0 && blockTags[name[:end]] {
		after := name[end:]
		if after == "" || after[0] == ' ' || after[0] == '>' || strings.HasPrefix(after, "/>") {
			return "", true
		}
	}
	if !interrupting && end > 0 {
		// Kind 7: a complete open or closing tag alone on its line.
		if t := strings.TrimSpace(s); strings.HasSuffix(t, ">") && !strings.Contains(t[1:len(t)-1], "<") {
			if _, ok := htmlTag(t, 0); ok && len(htmlTagLen(t)) == len(t) {
				return "", true
			}
		}
	}
	return "", false
}

func htmlTagLen(s string) string {
	if n, ok := htmlTag(s, 0); ok {
		return s[:n]
	}
	return ""
}

var blockTags = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`address article aside base basefont blockquote body caption center col
		colgroup dd details dialog dir div dl dt fieldset figcaption figure footer form frame frameset h1 h2
		h3 h4 h5 h6 head header hr html iframe legend li link main menu menuitem nav noframes ol optgroup
		option p param search section summary table tbody td tfoot th thead title tr track ul`) {
		blockTags[t] = true
	}
}

// delimiterRow reads a table's delimiter row: cells of dashes with
// optional colons, separated by pipes.
func delimiterRow(s string) ([]Align, bool) {
	if !strings.Contains(s, "-") || (!strings.Contains(s, "|") && !strings.Contains(s, ":")) {
		return nil, false
	}
	var out []Align
	for _, c := range splitRow(s) {
		c = strings.TrimSpace(c)
		if c == "" {
			return nil, false
		}
		left, right := strings.HasPrefix(c, ":"), strings.HasSuffix(c, ":")
		dashes := strings.Trim(c, ":")
		if dashes == "" || strings.Trim(dashes, "-") != "" {
			return nil, false
		}
		switch {
		case left && right:
			out = append(out, AlignCenter)
		case left:
			out = append(out, AlignLeft)
		case right:
			out = append(out, AlignRight)
		default:
			out = append(out, AlignNone)
		}
	}
	return out, len(out) > 0
}

// splitRow splits a table row at its unescaped pipes, dropping a leading
// and a trailing one.
func splitRow(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var out []string
	var cur strings.Builder
	inCode := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case c == '`':
			inCode = !inCode
			cur.WriteByte(c)
		case c == '|' && !inCode:
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(out, strings.TrimSpace(cur.String()))
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// footnoteStart reads "[^label]:" at the start of a line, and returns the
// label and where the definition's text starts.
func footnoteStart(s string) (string, int, bool) {
	end := strings.Index(s, "]:")
	if end < 3 {
		return "", 0, false
	}
	label := s[2:end]
	if strings.ContainsAny(label, " \t[]") {
		return "", 0, false
	}
	n := end + 2
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return label, n, true
}

// termsFromParagraph turns the open paragraph into a definition list's
// terms, one a line, for the definition about to open under them.
func (p *parser) termsFromParagraph() {
	para := p.tip()
	parent := p.stack[len(p.stack)-2]
	dl := &Node{Kind: DefList, Line: para.node.Line, EndLine: para.node.EndLine}
	for i, ln := range para.lines {
		dl.Children = append(dl.Children, &Node{Kind: DefTerm, Text: strings.TrimSpace(ln),
			Line: para.node.Line + i, EndLine: para.node.Line + i})
	}
	kids := parent.node.Children
	kids[len(kids)-1] = dl
	p.stack[len(p.stack)-1] = &block{node: dl}
}

// takeFootnotes lifts every footnote definition out of the tree, to be
// numbered as the text refers to them.
func (p *parser) takeFootnotes(n *Node) {
	kept := n.Children[:0]
	for _, c := range n.Children {
		if c.Kind == FootnoteDef {
			if p.notes == nil {
				p.notes = map[string]*Node{}
			}
			if key := normLabel(c.Label); p.notes[key] == nil {
				p.notes[key] = c
			}
			continue
		}
		p.takeFootnotes(c)
		kept = append(kept, c)
	}
	n.Children = kept
}

// placeFootnotes parses the notes that were referred to and puts them,
// numbered, in a Footnotes block at the end. A note can refer to another,
// so this goes on until no new one turns up.
func (p *parser) placeFootnotes(doc *Node) {
	for i := 0; i < len(p.noteOrder); i++ {
		p.inlines(p.noteOrder[i])
	}
	if len(p.noteOrder) == 0 {
		return
	}
	fn := &Node{Kind: Footnotes}
	fn.Children = append(fn.Children, p.noteOrder...)
	doc.Children = append(doc.Children, fn)
}

// note is the footnote a reference names, numbered on its first use.
func (p *parser) note(label string) *Node {
	n := p.notes[normLabel(label)]
	if n != nil && n.Start == 0 {
		p.noteOrder = append(p.noteOrder, n)
		n.Start = len(p.noteOrder)
	}
	return n
}

// slugHeadings gives every heading the anchor GitHub would: lower case,
// punctuation dropped, spaces as hyphens, and -1, -2 on repeats.
func slugHeadings(doc *Node) {
	seen := map[string]int{}
	Walk(doc, func(n *Node) bool {
		if n.Kind != Heading {
			return true
		}
		base := Slug(PlainText(n))
		id := base
		if k := seen[base]; k > 0 {
			id = fmt.Sprintf("%s-%d", base, k)
		}
		seen[base]++
		n.ID = id
		return false
	})
}

// Slug is GitHub's anchor for a heading's text.
func Slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_' || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}
