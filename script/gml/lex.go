// Package gml implements the declarative UI document format (DESIGN.md
// roadmap "QML interlude"): QML-inspired markup that maps 1:1 onto the ui
// builder API, with live expressions, event handlers, and dev-time hot
// reload.
//
//	Div {
//	    col: true; p: 20; gap: 12; bg: "#16161e"
//	    Text { text: "count: " + count; color: "#e8e8f0" }
//	    Div {
//	        size: [72, 40]; bg: "#3b5bdb"; rounded: 8
//	        justify: "center"; items: "center"
//	        onClicked: count = count + 1
//	        Text { text: "+"; color: "#ffffff" }
//	    }
//	}
//
// Because the ui layer rebuilds its element tree every frame, every
// expression in a document is re-evaluated each frame against the live
// property values — QML-style bindings with no binding machinery in the
// markup layer at all.
package gml

import (
	"fmt"
	"strings"
	"unicode"
)

type tokKind int

const (
	tokEOF tokKind = iota
	tokNewline
	tokIdent
	tokNumber
	tokString
	tokTemplate // `backtick ${expr} string` — raw inner text, expanded by parser
	tokPunct    // single/double char punctuation and operators
	tokRegex    // /pattern/flags: text is the pattern, a NUL, then the flags
)

type token struct {
	kind tokKind
	text string
	line int
}

type lexer struct {
	src  []rune
	pos  int
	line int
	out  []token
}

func lex(src string) ([]token, error) {
	l := &lexer{src: []rune(src), line: 1}
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			l.emit(tokNewline, "\n")
			l.line++
			l.pos++
		case c == ' ' || c == '\t' || c == '\r':
			l.pos++
		case c == '/' && l.peek(1) == '/':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		case c == '/' && l.peek(1) == '*':
			// Block comment; may span lines.
			l.pos += 2
			for l.pos < len(l.src) && !(l.src[l.pos] == '*' && l.peek(1) == '/') {
				if l.src[l.pos] == '\n' {
					l.line++
				}
				l.pos++
			}
			l.pos += 2
		case c == '/' && l.regexAhead():
			// JS regex literal in a value position (`regularExpression:
			// /[0-9]*/i`, `/ab+/.test(s)`): a RegExp value (regexp.go).
			l.pos++
			var b strings.Builder
			for l.pos < len(l.src) && l.src[l.pos] != '/' && l.src[l.pos] != '\n' {
				if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
					b.WriteRune(l.src[l.pos])
					l.pos++
				}
				b.WriteRune(l.src[l.pos])
				l.pos++
			}
			if l.pos >= len(l.src) || l.src[l.pos] != '/' {
				return nil, fmt.Errorf("gml:%d: unterminated regex literal", l.line)
			}
			l.pos++ // closing slash
			flags := l.pos
			for l.pos < len(l.src) && strings.ContainsRune("dgimsuvy", l.src[l.pos]) {
				l.pos++
			}
			l.emit(tokRegex, b.String()+"\x00"+string(l.src[flags:l.pos]))
		case unicode.IsLetter(c) || c == '_':
			start := l.pos
			for l.pos < len(l.src) && (unicode.IsLetter(l.src[l.pos]) || unicode.IsDigit(l.src[l.pos]) || l.src[l.pos] == '_') {
				l.pos++
			}
			l.emit(tokIdent, string(l.src[start:l.pos]))
		case unicode.IsDigit(c) || (c == '.' && unicode.IsDigit(l.peek(1))):
			start := l.pos
			// A radix prefix: 0x1F / 0b1011 / 0o17, JS's integer literals.
			// Without this `0x30A0` lexed as `0` and an identifier
			// `x30A0` — `String.fromCharCode(0x30A0 + ...)` failed to
			// resolve and the matrix-rain canvas drew nothing.
			if c == '0' && l.pos+1 < len(l.src) {
				if base := radixOf(l.src[l.pos+1]); base > 0 {
					l.pos += 2
					for l.pos < len(l.src) && isRadixDigit(l.src[l.pos], base) {
						l.pos++
					}
					l.emit(tokNumber, string(l.src[start:l.pos]))
					continue
				}
			}
			for l.pos < len(l.src) && (unicode.IsDigit(l.src[l.pos]) || l.src[l.pos] == '.') {
				l.pos++
			}
			// JS exponent suffix: 1.7e9, 2E+5, 3e-2.
			if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
				p := l.pos + 1
				if p < len(l.src) && (l.src[p] == '+' || l.src[p] == '-') {
					p++
				}
				if p < len(l.src) && unicode.IsDigit(l.src[p]) {
					l.pos = p
					for l.pos < len(l.src) && unicode.IsDigit(l.src[l.pos]) {
						l.pos++
					}
				}
			}
			l.emit(tokNumber, string(l.src[start:l.pos]))
		case c == '"' || c == '\'':
			quote := c
			l.pos++
			var b strings.Builder
			for l.pos < len(l.src) && l.src[l.pos] != quote {
				if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
					l.pos++
					switch l.src[l.pos] {
					case 'n':
						b.WriteRune('\n')
					case 't':
						b.WriteRune('\t')
					case 'u':
						// \uXXXX (JS BMP escape, e.g. "▶" ▶). Anything
						// short of 4 hex digits falls through as literal 'u'.
						if l.pos+4 < len(l.src) && isHex(l.src[l.pos+1]) && isHex(l.src[l.pos+2]) &&
							isHex(l.src[l.pos+3]) && isHex(l.src[l.pos+4]) {
							var r rune
							for i := 1; i <= 4; i++ {
								r = r<<4 | hexVal(l.src[l.pos+i])
							}
							l.pos += 4
							// A surrogate pair spells one astral rune
							// (\uD83D\uDCC1 = 📁): JS has no \u{...}, so
							// QML text for emoji always arrives this way.
							if r >= 0xD800 && r <= 0xDBFF && l.pos+6 < len(l.src) &&
								l.src[l.pos+1] == '\\' && l.src[l.pos+2] == 'u' &&
								isHex(l.src[l.pos+3]) && isHex(l.src[l.pos+4]) &&
								isHex(l.src[l.pos+5]) && isHex(l.src[l.pos+6]) {
								var lo rune
								for i := 3; i <= 6; i++ {
									lo = lo<<4 | hexVal(l.src[l.pos+i])
								}
								if lo >= 0xDC00 && lo <= 0xDFFF {
									r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
									l.pos += 6
								}
							}
							b.WriteRune(r)
						} else {
							b.WriteRune('u')
						}
					default:
						b.WriteRune(l.src[l.pos])
					}
				} else {
					b.WriteRune(l.src[l.pos])
				}
				l.pos++
			}
			if l.pos >= len(l.src) {
				return nil, fmt.Errorf("gml:%d: unterminated string", l.line)
			}
			l.pos++ // closing quote
			l.emit(tokString, b.String())
		case c == '`':
			// Template literal: capture raw inner text (incl. ${...}); the
			// parser expands it into a string-concatenation expression.
			l.pos++
			start := l.pos
			for l.pos < len(l.src) && l.src[l.pos] != '`' {
				if l.src[l.pos] == '\n' {
					l.line++
				}
				l.pos++
			}
			if l.pos >= len(l.src) {
				return nil, fmt.Errorf("gml:%d: unterminated template literal", l.line)
			}
			l.emit(tokTemplate, string(l.src[start:l.pos]))
			l.pos++ // closing backtick
		case c == '?':
			// ?. (optional chaining) and ?? (nullish coalescing) vs bare ? (ternary).
			switch l.peek(1) {
			case '.':
				l.emit(tokPunct, "?.")
				l.pos += 2
			case '?':
				l.emit(tokPunct, "??")
				l.pos += 2
			default:
				l.emit(tokPunct, "?")
				l.pos++
			}
		case strings.ContainsRune("{}[]():;,", c):
			l.emit(tokPunct, string(c))
			l.pos++
		case strings.ContainsRune("+-*/%!<>=&|.", c):
			two := string(c) + string(l.peek(1))
			three := two + string(l.peek(2))
			switch {
			case three == "...":
				l.emit(tokPunct, "...")
				l.pos += 3
			case three == "===" || three == "!==":
				// JS strict (in)equality; the engine is loosely typed, so it
				// behaves like ==/!= — emit the 2-char op to reuse eval.
				l.emit(tokPunct, three[:2])
				l.pos += 3
			case two == "==" || two == "!=" || two == "<=" || two == ">=" ||
				two == "&&" || two == "||" || two == "+=" || two == "-=" ||
				two == "*=" || two == "/=" || two == "%=" || two == "++" || two == "--" ||
				two == "=>" || two == "<<" || two == ">>" ||
				two == "|=" || two == "&=" || two == "^=":
				l.emit(tokPunct, two)
				l.pos += 2
			default:
				l.emit(tokPunct, string(c))
				l.pos++
			}
		case c == '#':
			// Color literal: #rrggbb or #rrggbbaa.
			start := l.pos
			l.pos++
			for l.pos < len(l.src) && isHex(l.src[l.pos]) {
				l.pos++
			}
			l.emit(tokString, string(l.src[start:l.pos])) // kept as "#...."
		default:
			return nil, fmt.Errorf("gml:%d: unexpected character %q", l.line, c)
		}
	}
	l.emit(tokEOF, "")
	return l.out, nil
}

func (l *lexer) peek(n int) rune {
	if l.pos+n < len(l.src) {
		return l.src[l.pos+n]
	}
	return 0
}

func (l *lexer) emit(k tokKind, s string) {
	l.out = append(l.out, token{kind: k, text: s, line: l.line})
}

func isHex(c rune) bool {
	return unicode.IsDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexVal(c rune) rune {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

// regexAhead reports whether a '/' at the current position starts a regex
// literal rather than a division. JS resolves this by context: a regex can
// only appear where a VALUE is expected — at the start of an expression or
// straight after an operator, never after an identifier, number, string or
// a closing bracket.
func (l *lexer) regexAhead() bool {
	for i := len(l.out) - 1; i >= 0; i-- {
		t := l.out[i]
		if t.kind == tokNewline {
			continue
		}
		switch t.kind {
		case tokIdent, tokNumber, tokString, tokTemplate, tokRegex:
			return false
		case tokPunct:
			switch t.text {
			case ")", "]", "}", "++", "--":
				return false
			}
			return true
		}
		return true
	}
	return true
}

// radixOf maps a JS numeric-literal prefix letter to its base (0 = not one).
func radixOf(c rune) int {
	switch c {
	case 'x', 'X':
		return 16
	case 'b', 'B':
		return 2
	case 'o', 'O':
		return 8
	}
	return 0
}

func isRadixDigit(c rune, base int) bool {
	switch {
	case c >= '0' && c <= '9':
		return int(c-'0') < base
	case base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'):
		return true
	}
	return false
}
