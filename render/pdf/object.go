// Package pdf reads PDF files and draws their pages through the 2D
// renderer. There is no separate rasterizer: a page's content stream is
// interpreted straight into render.Renderer2D calls — paths, clips, images
// and text — so PDF pages get the same antialiasing, transforms and glyph
// atlas as everything else the framework draws.
//
// The reader covers what real documents use: classic xref tables and xref
// streams (with /Prev chains and hybrid /XRefStm), object streams, the
// standard filters, and the page tree with inherited attributes. What it
// does not do is stated where it is missing rather than guessed at.
package pdf

import (
	"fmt"
	"strconv"
)

// Object is any PDF object: nil, bool, int64, float64, Name, String,
// Array, Dict, Ref or *Stream.
type Object any

// Name is a PDF name object (/Type), without the slash.
type Name string

// String is a PDF string object — bytes, not text: its encoding depends on
// where it is used (a text string, a date, binary font data).
type String []byte

// Array is a PDF array object.
type Array []Object

// Dict is a PDF dictionary object.
type Dict map[Name]Object

// Ref is an indirect reference ("12 0 R").
type Ref struct{ Num, Gen int }

// Stream is a stream object: its dictionary plus the raw (still encoded)
// bytes. Decode reads it through its /Filter chain.
type Stream struct {
	Dict Dict
	Raw  []byte
	doc  *Document // for /Length given as an indirect reference
}

// lexer scans PDF syntax out of a byte slice.
type lexer struct {
	b   []byte
	pos int
}

func isWhite(c byte) bool {
	switch c {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}

func isDelim(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

// skipSpace advances past whitespace and comments.
func (l *lexer) skipSpace() {
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		switch {
		case isWhite(c):
			l.pos++
		case c == '%':
			for l.pos < len(l.b) && l.b[l.pos] != '\n' && l.b[l.pos] != '\r' {
				l.pos++
			}
		default:
			return
		}
	}
}

// token reads the next raw token: a regular run of characters, or a single
// delimiter (with "<<" and ">>" kept together). Returns "" at the end.
func (l *lexer) token() string {
	l.skipSpace()
	if l.pos >= len(l.b) {
		return ""
	}
	c := l.b[l.pos]
	if isDelim(c) {
		if c == '<' && l.pos+1 < len(l.b) && l.b[l.pos+1] == '<' {
			l.pos += 2
			return "<<"
		}
		if c == '>' && l.pos+1 < len(l.b) && l.b[l.pos+1] == '>' {
			l.pos += 2
			return ">>"
		}
		l.pos++
		return string(c)
	}
	start := l.pos
	for l.pos < len(l.b) && !isWhite(l.b[l.pos]) && !isDelim(l.b[l.pos]) {
		l.pos++
	}
	return string(l.b[start:l.pos])
}

// peekToken reads the next token without consuming it.
func (l *lexer) peekToken() string {
	save := l.pos
	t := l.token()
	l.pos = save
	return t
}

// name reads a name object's body after the '/'. Two-digit #xx escapes are
// resolved, as the spec requires.
func (l *lexer) name() Name {
	var out []byte
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		if isWhite(c) || isDelim(c) {
			break
		}
		if c == '#' && l.pos+2 < len(l.b) {
			if v, err := strconv.ParseUint(string(l.b[l.pos+1:l.pos+3]), 16, 8); err == nil {
				out = append(out, byte(v))
				l.pos += 3
				continue
			}
		}
		out = append(out, c)
		l.pos++
	}
	return Name(out)
}

// literalString reads a (…) string, with its escapes and nesting.
func (l *lexer) literalString() String {
	var out []byte
	depth := 1
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		l.pos++
		switch c {
		case '\\':
			if l.pos >= len(l.b) {
				return out
			}
			e := l.b[l.pos]
			l.pos++
			switch e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\n': // a line continuation contributes nothing
			case '\r':
				if l.pos < len(l.b) && l.b[l.pos] == '\n' {
					l.pos++
				}
			default:
				if e >= '0' && e <= '7' { // up to three octal digits
					v := int(e - '0')
					for i := 0; i < 2 && l.pos < len(l.b) && l.b[l.pos] >= '0' && l.b[l.pos] <= '7'; i++ {
						v = v*8 + int(l.b[l.pos]-'0')
						l.pos++
					}
					out = append(out, byte(v))
				} else {
					out = append(out, e)
				}
			}
		case '(':
			depth++
			out = append(out, c)
		case ')':
			depth--
			if depth == 0 {
				return out
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

// hexString reads a <…> string. An odd final digit is padded with 0.
func (l *lexer) hexString() String {
	var out []byte
	var hi byte
	half := false
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		l.pos++
		if c == '>' {
			break
		}
		var v byte
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'a' && c <= 'f':
			v = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		default:
			continue // whitespace and anything else is skipped
		}
		if half {
			out = append(out, hi<<4|v)
			half = false
		} else {
			hi, half = v, true
		}
	}
	if half {
		out = append(out, hi<<4)
	}
	return out
}

// object parses one object at the current position. Indirect references
// ("12 0 R") are recognised by lookahead, as the grammar requires.
func (l *lexer) object() (Object, error) {
	l.skipSpace()
	if l.pos >= len(l.b) {
		return nil, fmt.Errorf("pdf: unexpected end of file")
	}
	switch c := l.b[l.pos]; {
	case c == '/':
		l.pos++
		return l.name(), nil
	case c == '(':
		l.pos++
		return l.literalString(), nil
	case c == '[':
		l.pos++
		arr := Array{}
		for {
			l.skipSpace()
			if l.pos >= len(l.b) {
				return nil, fmt.Errorf("pdf: unterminated array")
			}
			if l.b[l.pos] == ']' {
				l.pos++
				return arr, nil
			}
			o, err := l.object()
			if err != nil {
				return nil, err
			}
			arr = append(arr, o)
		}
	case c == '<':
		if l.pos+1 < len(l.b) && l.b[l.pos+1] == '<' {
			l.pos += 2
			return l.dict()
		}
		l.pos++
		return l.hexString(), nil
	}
	// A regular token: keyword, number, or the start of a reference.
	save := l.pos
	t := l.token()
	switch t {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	case "":
		return nil, fmt.Errorf("pdf: unexpected end of file")
	}
	if n, err := strconv.ParseInt(t, 10, 64); err == nil {
		// "<num> <gen> R" is a reference; anything else is just the number.
		mark := l.pos
		if g, err := strconv.ParseInt(l.token(), 10, 64); err == nil {
			if l.token() == "R" {
				return Ref{Num: int(n), Gen: int(g)}, nil
			}
		}
		l.pos = mark
		return n, nil
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil {
		return f, nil
	}
	// PDF numbers like ".5" or "4." and stray keywords land here.
	if f, err := parseLooseNumber(t); err == nil {
		return f, nil
	}
	l.pos = save
	return nil, fmt.Errorf("pdf: unexpected token %q", t)
}

// dict parses a dictionary body (after "<<"), and the stream that may
// follow it.
func (l *lexer) dict() (Object, error) {
	d := Dict{}
	for {
		l.skipSpace()
		if l.pos >= len(l.b) {
			return nil, fmt.Errorf("pdf: unterminated dictionary")
		}
		if l.b[l.pos] == '>' {
			if l.pos+1 < len(l.b) && l.b[l.pos+1] == '>' {
				l.pos += 2
				return d, nil
			}
			l.pos++
			continue
		}
		if l.b[l.pos] != '/' {
			// A malformed entry: skip a token so a broken dictionary in one
			// object cannot stall the whole file.
			if l.token() == "" {
				return nil, fmt.Errorf("pdf: unterminated dictionary")
			}
			continue
		}
		l.pos++
		key := l.name()
		val, err := l.object()
		if err != nil {
			return nil, err
		}
		d[key] = val
	}
}

// parseLooseNumber accepts the number forms PDF allows that Go's parser
// rejects outright: a leading "+", a bare ".5", a trailing "4.", and the
// "--5" some producers emit.
func parseLooseNumber(t string) (float64, error) {
	s := t
	neg := false
	for len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		if s[0] == '-' {
			neg = !neg
		}
		s = s[1:]
	}
	if s == "" || s == "." {
		return 0, fmt.Errorf("pdf: not a number %q", t)
	}
	f, err := strconv.ParseFloat("0"+s+"0", 64)
	if err != nil {
		return 0, fmt.Errorf("pdf: not a number %q", t)
	}
	// "0"+".5"+"0" is 0.50 and "0"+"4."+"0" is 040 — recover the magnitude.
	f, err = strconv.ParseFloat(trimNumber(s), 64)
	if err != nil {
		return 0, fmt.Errorf("pdf: not a number %q", t)
	}
	if neg {
		f = -f
	}
	return f, nil
}

// trimNumber makes a PDF number literal parseable: ".5" → "0.5", "4." → "4".
func trimNumber(s string) string {
	if s[0] == '.' {
		return "0" + s
	}
	if s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}
