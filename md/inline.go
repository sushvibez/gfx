package md

import (
	"html"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Inline structure, CommonMark's way. The text is scanned once into a
// list of pieces — plain text, code spans, autolinks, line breaks — and
// two stacks: one of `*`/`_`/`~` runs that may become emphasis, one of `[`
// and `![` that may become links. A `]` resolves the nearest bracket into a
// link or image (or leaves it as text); emphasis is matched afterwards,
// innermost first, by the flanking rules.

// piece is an inline node in the doubly linked list the scan builds.
type piece struct {
	n          *Node
	prev, next *piece
}

// delim is a run of * _ ~ that may open or close emphasis, or a [ / ![.
type delim struct {
	p        *piece
	ch       byte
	n        int // characters left in the run
	orig     int // its length as read, for the rule of three
	canOpen  bool
	canClose bool
	image    bool // ![
	active   bool // a bracket that may still become a link
	pos      int  // a bracket: where its text starts in the source
	prev     *delim
}

type inlineParser struct {
	p          *parser
	s          string
	i          int
	head, tail *piece
	delims     *delim // emphasis delimiters, top of the stack
	brackets   *delim // [ and ![, top of the stack
}

func (p *parser) parseInlines(s string) []*Node {
	ip := &inlineParser{p: p, s: s}
	ip.scan()
	ip.emphasis(nil)
	var out []*Node
	for c := ip.head; c != nil; c = c.next {
		out = append(out, c.n)
	}
	return mergeText(out)
}

// mergeText joins adjacent text nodes, everywhere under the list.
func mergeText(ns []*Node) []*Node {
	var out []*Node
	for _, n := range ns {
		if len(n.Children) > 0 {
			n.Children = mergeText(n.Children)
		}
		if n.Kind == Text && n.Text == "" {
			continue
		}
		if n.Kind == Text && len(out) > 0 && out[len(out)-1].Kind == Text {
			out[len(out)-1].Text += n.Text
			continue
		}
		out = append(out, n)
	}
	return out
}

func (ip *inlineParser) push(n *Node) *piece {
	pc := &piece{n: n, prev: ip.tail}
	if ip.tail != nil {
		ip.tail.next = pc
	} else {
		ip.head = pc
	}
	ip.tail = pc
	return pc
}

func (ip *inlineParser) text(s string) { ip.push(&Node{Kind: Text, Text: s}) }

// scan reads the whole string.
func (ip *inlineParser) scan() {
	s := ip.s
	start := ip.i
	flush := func() {
		if ip.i > start {
			ip.text(s[start:ip.i])
		}
	}
	// flushTo writes the text before a construct that has already moved
	// the scan past itself.
	flushTo := func(j int) {
		if j > start {
			ip.text(s[start:j])
		}
	}
	for ip.i < len(s) {
		c := s[ip.i]
		// An email address (GitHub), seen from the start of its word —
		// its _ would otherwise be taken for emphasis before the @ came.
		if ip.p.gfm && localChar(c) && (ip.i == 0 || !localChar(s[ip.i-1])) {
			j := ip.i
			for j < len(s) && localChar(s[j]) {
				j++
			}
			if j < len(s) && s[j] == '@' {
				w := ip.i
				ip.i = j
				if n, ls, ok := ip.emailLink(w); ok {
					flushTo(ls)
					ip.push(n)
					start = ip.i
					continue
				}
				ip.i = w
			}
		}
		switch c {
		case '\\':
			flush()
			if ip.i+1 < len(s) && s[ip.i+1] == '\n' {
				ip.push(&Node{Kind: HardBreak})
				ip.i += 2
				ip.skipSpaces()
			} else if ip.i+1 < len(s) && isPunct(s[ip.i+1]) {
				ip.text(s[ip.i+1 : ip.i+2])
				ip.i += 2
			} else {
				ip.text("\\")
				ip.i++
			}
			start = ip.i
			continue
		case '`':
			flush()
			ip.codeSpan()
			start = ip.i
			continue
		case '~':
			if !ip.p.gfm {
				break
			}
			flush()
			ip.delimRun(c)
			start = ip.i
			continue
		case '*', '_':
			flush()
			ip.delimRun(c)
			start = ip.i
			continue
		case '[':
			if ip.p.gfm && ip.i+1 < len(s) && s[ip.i+1] == '^' {
				j := ip.i
				if n, ok := ip.footnoteRef(); ok {
					flushTo(j)
					ip.push(n)
					start = ip.i
					continue
				}
			}
			flush()
			ip.openBracket(false)
			start = ip.i
			continue
		case '!':
			if ip.i+1 < len(s) && s[ip.i+1] == '[' {
				flush()
				ip.openBracket(true)
				start = ip.i
				continue
			}
		case ']':
			flush()
			ip.closeBracket()
			start = ip.i
			continue
		case '<':
			j := ip.i
			if n, ok := ip.autolink(); ok {
				flushTo(j)
				ip.push(n)
				start = ip.i
				continue
			}
			if end, ok := htmlTag(s, ip.i); ok {
				flush()
				ip.push(&Node{Kind: HTMLInline, Text: s[ip.i:end]})
				ip.i = end
				start = ip.i
				continue
			}
		case '&':
			if r, n := entity(s[ip.i:]); n > 0 {
				flush()
				ip.text(r)
				ip.i += n
				start = ip.i
				continue
			}
		case '\n':
			// A line break: hard after two spaces, soft otherwise. The
			// spaces at the end of the line go either way.
			end := ip.i
			for end > start && s[end-1] == ' ' {
				end--
			}
			hard := ip.i-end >= 2
			if end > start {
				ip.text(s[start:end])
			}
			if hard {
				ip.push(&Node{Kind: HardBreak})
			} else {
				ip.push(&Node{Kind: SoftBreak})
			}
			ip.i++
			ip.skipSpaces()
			start = ip.i
			continue
		case 'h', 'w', 'f':
			// A bare link (GitHub): http://, https://, ftp:// or www., at
			// the start of a word.
			if ip.p.gfm && (ip.i == 0 || !isAlnum(s[ip.i-1])) {
				j := ip.i
				if n, ok := ip.bareLink(); ok {
					flushTo(j)
					ip.push(n)
					start = ip.i
					continue
				}
			}
		}
		ip.i++
	}
	flush()
}

func (ip *inlineParser) skipSpaces() {
	for ip.i < len(ip.s) && ip.s[ip.i] == ' ' {
		ip.i++
	}
}

// codeSpan: a run of backticks closed by a run of the same length. The
// content's line endings become spaces, and one space each side is
// stripped when both are there (so "“ `x` “" shows `x`).
func (ip *inlineParser) codeSpan() {
	s := ip.s
	n := 0
	for ip.i+n < len(s) && s[ip.i+n] == '`' {
		n++
	}
	open := ip.i + n
	for j := open; j < len(s); {
		if s[j] != '`' {
			j++
			continue
		}
		k := 0
		for j+k < len(s) && s[j+k] == '`' {
			k++
		}
		if k == n {
			body := strings.ReplaceAll(s[open:j], "\n", " ")
			if len(body) >= 2 && body[0] == ' ' && body[len(body)-1] == ' ' && strings.Trim(body, " ") != "" {
				body = body[1 : len(body)-1]
			}
			ip.push(&Node{Kind: Code, Text: body})
			ip.i = j + k
			return
		}
		j += k
	}
	// No closing run: the backticks are text.
	ip.text(s[ip.i:open])
	ip.i = open
}

// delimRun reads a run of * _ or ~ and records whether it can open or
// close emphasis, by the flanking rules.
func (ip *inlineParser) delimRun(c byte) {
	s := ip.s
	n := 0
	for ip.i+n < len(s) && s[ip.i+n] == c {
		n++
	}
	before, after := ' ', ' '
	if ip.i > 0 {
		before, _ = utf8.DecodeLastRuneInString(s[:ip.i])
	}
	if ip.i+n < len(s) {
		after, _ = utf8.DecodeRuneInString(s[ip.i+n:])
	}
	leftFlank := !unicode.IsSpace(after) && (!isPunctRune(after) || unicode.IsSpace(before) || isPunctRune(before))
	rightFlank := !unicode.IsSpace(before) && (!isPunctRune(before) || unicode.IsSpace(after) || isPunctRune(after))
	canOpen, canClose := leftFlank, rightFlank
	if c == '_' {
		canOpen = leftFlank && (!rightFlank || isPunctRune(before))
		canClose = rightFlank && (!leftFlank || isPunctRune(after))
	}
	if c == '~' && n > 2 {
		canOpen, canClose = false, false // strikethrough is one or two tildes
	}
	pc := ip.push(&Node{Kind: Text, Text: s[ip.i : ip.i+n]})
	ip.i += n
	if canOpen || canClose {
		ip.delims = &delim{p: pc, ch: c, n: n, orig: n, canOpen: canOpen, canClose: canClose, prev: ip.delims}
	}
}

func (ip *inlineParser) openBracket(image bool) {
	w := 1
	if image {
		w = 2
	}
	pc := ip.push(&Node{Kind: Text, Text: ip.s[ip.i : ip.i+w]})
	ip.i += w
	ip.brackets = &delim{p: pc, image: image, active: true, prev: ip.brackets, ch: '[', pos: ip.i}
	// Remember where emphasis stood, so a link's text is matched on its own.
	ip.brackets.n = ip.delimDepth()
}

func (ip *inlineParser) delimDepth() int {
	n := 0
	for d := ip.delims; d != nil; d = d.prev {
		n++
	}
	return n
}

// closeBracket resolves a ] against the nearest [ or ![: an inline link
// "(dest "title")", a full "[label]", collapsed "[]" or shortcut reference.
func (ip *inlineParser) closeBracket() {
	op := ip.brackets
	ip.i++ // past the ]
	if op == nil {
		ip.text("]")
		return
	}
	ip.brackets = op.prev
	if !op.active {
		ip.text("]")
		return
	}
	textStart := op.p.next
	label := ip.s[op.pos : ip.i-1] // the raw text between the brackets
	dest, title, ok := "", "", false
	if d, t, end, good := linkTail(ip.s, ip.i); good {
		dest, title, ok = d, t, true
		ip.i = end
	} else if l, end, good := linkLabel(ip.s, ip.i); good {
		key := l
		if key == "" {
			key = label
		}
		if r, found := ip.p.refs[normLabel(key)]; found {
			dest, title, ok = r.dest, r.title, true
			ip.i = end
		}
	} else if r, found := ip.p.refs[normLabel(label)]; found {
		dest, title, ok = r.dest, r.title, true
	}
	if !ok {
		ip.text("]")
		return
	}
	kind := Link
	if op.image {
		kind = Image
	}
	n := &Node{Kind: kind, Dest: dest, Title: title}
	// The link's text is everything after the opener; emphasis inside it
	// is matched before it is moved under the link.
	ip.emphasis(ip.delimAt(op.n))
	for c := textStart; c != nil; c = c.next {
		n.Children = append(n.Children, c.n)
	}
	// Replace the opener and its text with the link.
	op.p.n = n
	op.p.next = nil
	ip.tail = op.p
	// No links inside links: earlier [ openers are dead.
	if !op.image {
		for b := ip.brackets; b != nil; b = b.prev {
			if !b.image {
				b.active = false
			}
		}
	}
}

// delimAt is the emphasis delimiter that was on top when there were n of
// them, or nil.
func (ip *inlineParser) delimAt(n int) *delim {
	total := ip.delimDepth()
	d := ip.delims
	for i := total; i > n && d != nil; i-- {
		d = d.prev
	}
	return d
}

// emphasis matches the delimiters above bottom into Emph, Strong and
// Strike, closers left to right, each with the nearest opener it can.
func (ip *inlineParser) emphasis(bottom *delim) {
	// The stack is linked newest first; work oldest first.
	var list []*delim
	for d := ip.delims; d != bottom; d = d.prev {
		list = append([]*delim{d}, list...)
	}
	for ci := 0; ci < len(list); ci++ {
		cl := list[ci]
		if !cl.canClose || cl.n == 0 {
			continue
		}
		for oi := ci - 1; oi >= 0; oi-- {
			op := list[oi]
			if op.ch != cl.ch || !op.canOpen || op.n == 0 {
				continue
			}
			if cl.ch == '~' {
				if op.n != cl.n {
					continue
				}
			} else if (op.canClose || cl.canOpen) && (op.orig+cl.orig)%3 == 0 && !(op.orig%3 == 0 && cl.orig%3 == 0) {
				continue // the rule of three
			}
			use := 1
			kind := Emph
			switch {
			case cl.ch == '~':
				use, kind = cl.n, Strike
			case op.n >= 2 && cl.n >= 2:
				use, kind = 2, Strong
			}
			// Wrap everything between them.
			n := &Node{Kind: kind}
			for c := op.p.next; c != nil && c != cl.p; c = c.next {
				n.Children = append(n.Children, c.n)
			}
			op.n -= use
			cl.n -= use
			op.p.n.Text = op.p.n.Text[:op.n]
			cl.p.n.Text = cl.p.n.Text[:cl.n]
			wrap := &piece{n: n, prev: op.p, next: cl.p}
			op.p.next = wrap
			cl.p.prev = wrap
			// Delimiters between them are spent.
			for k := oi + 1; k < ci; k++ {
				list[k].n = 0
			}
			if cl.n > 0 {
				ci-- // the rest of this closer may close another opener
			}
			break
		}
	}
	// Spent delimiters stay as (possibly empty) text; empty text is
	// dropped by mergeText.
	ip.delims = bottom
}

// autolink: <scheme:...> or <email>.
func (ip *inlineParser) autolink() (*Node, bool) {
	s := ip.s[ip.i:]
	end := strings.IndexByte(s, '>')
	if end < 0 {
		return nil, false
	}
	inner := s[1:end]
	if inner == "" || strings.ContainsAny(inner, " <\n") {
		return nil, false
	}
	if k := strings.IndexByte(inner, ':'); k >= 2 && k <= 32 && isScheme(inner[:k]) {
		ip.i += end + 1
		return &Node{Kind: Link, Dest: inner, Children: []*Node{{Kind: Text, Text: inner}}}, true
	}
	if at := strings.IndexByte(inner, '@'); at > 0 && at < len(inner)-1 && !strings.ContainsAny(inner, "\\") {
		ip.i += end + 1
		return &Node{Kind: Link, Dest: "mailto:" + inner, Children: []*Node{{Kind: Text, Text: inner}}}, true
	}
	return nil, false
}

func isScheme(s string) bool {
	if !(s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z') {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !isAlnum(c) && c != '+' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// bareLink is GitHub's extended autolink at the cursor: www., http://,
// https:// or ftp://, then a valid domain and a path, less the
// punctuation that ends a sentence after it, a ) it has no ( for, and an
// entity-like &name; at its very end.
func (ip *inlineParser) bareLink() (*Node, bool) {
	s := ip.s[ip.i:]
	prefix := ""
	switch {
	case strings.HasPrefix(s, "https://"), strings.HasPrefix(s, "http://"), strings.HasPrefix(s, "ftp://"):
	case strings.HasPrefix(s, "www."):
		prefix = "http://"
	default:
		return nil, false
	}
	end := 0
	for end < len(s) && s[end] != ' ' && s[end] != '\t' && s[end] != '\n' && s[end] != '<' {
		end++
	}
	end = trimLinkEnd(s, end)
	text := s[:end]
	host := text
	if k := strings.Index(host, "://"); k >= 0 {
		host = host[k+3:]
	}
	if k := strings.IndexAny(host, "/?#"); k >= 0 {
		host = host[:k]
	}
	if !validDomain(host) {
		return nil, false
	}
	ip.i += end
	return &Node{Kind: Link, Dest: prefix + text, Children: []*Node{{Kind: Text, Text: text}}}, true
}

// trimLinkEnd takes back the end of an extended autolink.
func trimLinkEnd(s string, end int) int {
	for end > 0 {
		c := s[end-1]
		switch {
		case strings.IndexByte("?!.,:*_~'\"", c) >= 0:
			end--
		case c == ')' && strings.Count(s[:end], "(") < strings.Count(s[:end], ")"):
			end--
		case c == ';':
			// &name; at the end looks like an entity, and is not the link's.
			k := end - 2
			for k >= 0 && isAlnum(s[k]) {
				k--
			}
			if k >= 0 && k < end-2 && s[k] == '&' {
				end = k
				continue
			}
			return end
		default:
			return end
		}
	}
	return end
}

// validDomain: labels of letters, digits, _ and -, at least two of them,
// and no _ in the last two.
func validDomain(h string) bool {
	parts := strings.Split(h, ".")
	if len(parts) < 2 {
		return false
	}
	for i, p := range parts {
		if p == "" && i < len(parts)-1 {
			return false
		}
		for k := 0; k < len(p); k++ {
			if c := p[k]; !(isAlnum(c) || c == '-' || c == '_') {
				return false
			}
		}
		if i >= len(parts)-2 && strings.Contains(p, "_") {
			return false
		}
	}
	return true
}

// emailLink is GitHub's email autolink around the @ at the cursor: a
// local part of letters, digits and . _ + - before it (the text since
// from), a domain of at least two labels after it, a final . left out,
// and nothing that ends in - or _.
func (ip *inlineParser) emailLink(from int) (*Node, int, bool) {
	s, at := ip.s, ip.i
	ls := at
	for ls > from && (isAlnum(s[ls-1]) || strings.IndexByte("._+-", s[ls-1]) >= 0) {
		ls--
	}
	if ls == at {
		return nil, 0, false
	}
	end := at + 1
	dots := 0
	for end < len(s) && (isAlnum(s[end]) || strings.IndexByte("._-", s[end]) >= 0) {
		if s[end] == '.' {
			dots++
		}
		end++
	}
	if end > at+1 && s[end-1] == '.' {
		end--
		dots--
	}
	if dots < 1 || end == at+1 {
		return nil, 0, false
	}
	if c := s[end-1]; c == '-' || c == '_' {
		return nil, 0, false
	}
	text := s[ls:end]
	ip.i = end
	return &Node{Kind: Link, Dest: "mailto:" + text, Children: []*Node{{Kind: Text, Text: text}}}, ls, true
}

// --- links ----------------------------------------------------------------

// linkTail reads "(dest "title")" at i.
func linkTail(s string, i int) (dest, title string, end int, ok bool) {
	if i >= len(s) || s[i] != '(' {
		return
	}
	j := skipWS(s, i+1)
	dest, j, ok = linkDest(s, j)
	if !ok {
		return
	}
	k := skipWS(s, j)
	if k < len(s) && k > j && (s[k] == '"' || s[k] == '\'' || s[k] == '(') {
		var t string
		var tok bool
		t, k, tok = linkTitle(s, k)
		if !tok {
			return "", "", 0, false
		}
		title = t
		k = skipWS(s, k)
	}
	if k >= len(s) || s[k] != ')' {
		return "", "", 0, false
	}
	return dest, title, k + 1, true
}

func skipWS(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\n') {
		i++
	}
	return i
}

// linkDest: <anything but newline and <>> or a run without spaces or
// control characters, its parentheses balanced.
func linkDest(s string, i int) (string, int, bool) {
	if i < len(s) && s[i] == '<' {
		for j := i + 1; j < len(s); j++ {
			switch s[j] {
			case '>':
				return unescapeString(s[i+1 : j]), j + 1, true
			case '\n', '<':
				return "", 0, false
			case '\\':
				j++
			}
		}
		return "", 0, false
	}
	depth, j := 0, i
	for j < len(s) {
		c := s[j]
		if c == '\\' && j+1 < len(s) && isPunct(s[j+1]) {
			j += 2
			continue
		}
		if c == ' ' || c == '\n' || c < 0x20 {
			break
		}
		if c == '(' {
			depth++
		}
		if c == ')' {
			if depth == 0 {
				break
			}
			depth--
		}
		j++
	}
	if depth != 0 {
		return "", 0, false
	}
	return unescapeString(s[i:j]), j, true
}

func linkTitle(s string, i int) (string, int, bool) {
	closer := s[i]
	if closer == '(' {
		closer = ')'
	}
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case closer:
			return unescapeString(s[i+1 : j]), j + 1, true
		}
	}
	return "", 0, false
}

// linkLabel reads "[label]" at i (the label may be empty: "[]").
func linkLabel(s string, i int) (string, int, bool) {
	if i >= len(s) || s[i] != '[' {
		return "", 0, false
	}
	for j := i + 1; j < len(s) && j-i <= 1000; j++ {
		switch s[j] {
		case '\\':
			j++
		case '[':
			return "", 0, false
		case ']':
			return s[i+1 : j], j + 1, true
		}
	}
	return "", 0, false
}

// normLabel is how labels are matched: case-folded, whitespace collapsed.
func normLabel(s string) string {
	// Unicode case folding, not just lower case: ẞ and SS are the same
	// label, and lower-casing alone stops at ß.
	return strings.ReplaceAll(strings.ToLower(strings.Join(strings.Fields(s), " ")), "ß", "ss")
}

// parseRefDef reads a link reference definition at the start of a
// paragraph: [label]: dest "title", the title optional and on the same or
// the next line.
func parseRefDef(s string) (label, dest, title, rest string, ok bool) {
	t := strings.TrimLeft(s, " ")
	if len(s)-len(t) > 3 {
		return
	}
	l, j, lok := linkLabel(t, 0)
	if !lok || strings.TrimSpace(l) == "" || j >= len(t) || t[j] != ':' {
		return
	}
	j = skipWS(t, j+1)
	d, k, dok := linkDest(t, j)
	if !dok || (d == "" && (j >= len(t) || t[j] != '<')) { // empty only as <>
		return
	}
	// A title, if one follows; otherwise the definition ends with the line.
	after := k
	m := skipWS(t, k)
	if m > k && m < len(t) && (t[m] == '"' || t[m] == '\'' || t[m] == '(') {
		if tt, e, tok := linkTitle(t, m); tok {
			if rl := restOfLine(t, e); rl >= 0 {
				return l, d, tt, t[rl:], true
			}
		}
	}
	if rl := restOfLine(t, after); rl >= 0 {
		return l, d, "", t[rl:], true
	}
	return
}

// restOfLine: only spaces to the end of the line → the index past it, or
// -1 when there is anything else.
func restOfLine(s string, i int) int {
	for i < len(s) && s[i] == ' ' {
		i++
	}
	if i == len(s) {
		return i
	}
	if s[i] == '\n' {
		return i + 1
	}
	return -1
}

// --- characters -----------------------------------------------------------

func isPunct(c byte) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
}

func isPunctRune(r rune) bool {
	if r < 0x80 {
		return isPunct(byte(r))
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// unescapeString resolves backslash escapes and entities in a link's
// destination or title, or a fence's info string.
func unescapeString(s string) string {
	if !strings.ContainsAny(s, "\\&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) && isPunct(s[i+1]) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		if c == '&' {
			if r, n := entity(s[i:]); n > 0 {
				b.WriteString(r)
				i += n - 1
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// htmlTag recognises raw HTML at i, by the spec's grammar: an open tag
// (name, attributes, optional /), a closing tag, a comment, a processing
// instruction, a declaration or a CDATA section. It returns where it ends.
func htmlTag(s string, i int) (int, bool) {
	t := s[i:]
	end := func(n int) (int, bool) { return i + n, true }
	switch {
	case strings.HasPrefix(t, "<!-->"):
		return end(5)
	case strings.HasPrefix(t, "<!--->"):
		return end(6)
	case strings.HasPrefix(t, "<!--"):
		if e := strings.Index(t[4:], "-->"); e >= 0 {
			return end(4 + e + 3)
		}
		return 0, false
	case strings.HasPrefix(t, "<?"):
		if e := strings.Index(t[2:], "?>"); e >= 0 {
			return end(2 + e + 2)
		}
		return 0, false
	case strings.HasPrefix(t, "<![CDATA["):
		if e := strings.Index(t[9:], "]]>"); e >= 0 {
			return end(9 + e + 3)
		}
		return 0, false
	case strings.HasPrefix(t, "<!"):
		if len(t) > 2 && isLetter(t[2]) {
			if e := strings.IndexByte(t, '>'); e >= 0 {
				return end(e + 1)
			}
		}
		return 0, false
	case strings.HasPrefix(t, "</"):
		k := tagName(t, 2)
		if k < 0 {
			return 0, false
		}
		k = skipHTMLSpace(t, k)
		if k < len(t) && t[k] == '>' {
			return end(k + 1)
		}
		return 0, false
	}
	k := tagName(t, 1)
	if k < 0 {
		return 0, false
	}
	for {
		// An attribute needs whitespace before it.
		w := skipHTMLSpace(t, k)
		if w == k || w >= len(t) || !(isLetter(t[w]) || t[w] == '_' || t[w] == ':') {
			k = w
			break
		}
		k = w + 1
		for k < len(t) && (isAlnum(t[k]) || strings.IndexByte("_.:-", t[k]) >= 0) {
			k++
		}
		// An optional value.
		v := skipHTMLSpace(t, k)
		if v < len(t) && t[v] == '=' {
			v = skipHTMLSpace(t, v+1)
			if v >= len(t) {
				return 0, false
			}
			switch q := t[v]; q {
			case '"', '\'':
				e := strings.IndexByte(t[v+1:], q)
				if e < 0 {
					return 0, false
				}
				k = v + 1 + e + 1
			default:
				e := v
				for e < len(t) && strings.IndexByte(" \t\n\r\f\v\"'=<>`", t[e]) < 0 {
					e++
				}
				if e == v {
					return 0, false
				}
				k = e
			}
		}
	}
	if k < len(t) && t[k] == '/' {
		k++
	}
	if k < len(t) && t[k] == '>' {
		return end(k + 1)
	}
	return 0, false
}

// tagName reads a tag name at k: a letter, then letters, digits and
// hyphens. It returns where it ends, or -1.
func tagName(t string, k int) int {
	if k >= len(t) || !isLetter(t[k]) {
		return -1
	}
	for k < len(t) && (isAlnum(t[k]) || t[k] == '-') {
		k++
	}
	return k
}

// skipHTMLSpace skips spaces, tabs and at most one line ending.
func skipHTMLSpace(t string, k int) int {
	nl := false
	for k < len(t) {
		switch t[k] {
		case ' ', '\t':
		case '\n':
			if nl {
				return k
			}
			nl = true
		default:
			return k
		}
		k++
	}
	return k
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// entity resolves an HTML entity at the start of s: named (the common
// ones), decimal or hex. It returns the text and how many bytes it took,
// or 0.
func entity(s string) (string, int) {
	end := strings.IndexByte(s, ';')
	if end < 2 || end > 32 {
		return "", 0
	}
	name := s[1:end]
	if name[0] == '#' {
		var v int
		digits := name[1:]
		base := 10
		if len(digits) > 0 && (digits[0] == 'x' || digits[0] == 'X') {
			base, digits = 16, digits[1:]
		}
		if digits == "" || len(digits) > 7 {
			return "", 0
		}
		for _, c := range digits {
			d := strings.IndexRune("0123456789abcdef", unicode.ToLower(c))
			if d < 0 || d >= base {
				return "", 0
			}
			v = v*base + d
		}
		if v == 0 || v > unicode.MaxRune {
			v = 0xFFFD
		}
		return string(rune(v)), end + 1
	}
	// Named: any of HTML5's two thousand, which the standard library
	// knows. Only the name with its semicolon counts — the legacy forms
	// without one are not entities in Markdown.
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return "", 0
		}
	}
	whole := s[:end+1]
	if r := html.UnescapeString(whole); r != whole && (name == "semi" || !strings.HasSuffix(r, ";")) {
		return r, end + 1
	}
	return "", 0
}

// footnoteRef reads [^label] at the cursor when a note of that label is
// defined, and numbers the note on its first reference.
func (ip *inlineParser) footnoteRef() (*Node, bool) {
	s := ip.s
	end := strings.IndexByte(s[ip.i:], ']')
	if end < 3 {
		return nil, false
	}
	label := s[ip.i+2 : ip.i+end]
	if strings.ContainsAny(label, " \t\n[") {
		return nil, false
	}
	note := ip.p.note(label)
	if note == nil {
		return nil, false
	}
	note.Level++ // how many references it has had
	ip.i += end + 1
	return &Node{Kind: FootnoteRef, Label: note.Label, Start: note.Start, Level: note.Level}, true
}

// localChar can be in an email address's local part.
func localChar(c byte) bool { return isAlnum(c) || c == '.' || c == '_' || c == '+' || c == '-' }
