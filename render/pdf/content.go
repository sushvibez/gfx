package pdf

// Content-stream scanning: a content stream is postfix — operands then an
// operator — so the scanner collects operands until it reads a keyword.

import "strconv"

// op is one operator with the operands that preceded it.
type op struct {
	name string
	args []Object
	// inline is an inline image's dictionary and data (the BI…ID…EI
	// sequence, which is not an ordinary operator).
	inline *Stream
}

// scanContent splits a content stream into operators.
func scanContent(b []byte, doc *Document) []op {
	l := &lexer{b: b}
	var ops []op
	var args []Object
	for {
		l.skipSpace()
		if l.pos >= len(b) {
			return ops
		}
		c := b[l.pos]
		// An operand: anything that starts a PDF object.
		if c == '/' || c == '(' || c == '[' || c == '<' || c == '+' || c == '-' || c == '.' ||
			(c >= '0' && c <= '9') {
			o, err := l.object()
			if err != nil {
				l.pos++ // skip the offending byte rather than stopping the page
				continue
			}
			args = append(args, o)
			continue
		}
		if c == ']' || c == ')' || c == '>' || c == '}' || c == '{' {
			l.pos++ // stray delimiter
			continue
		}
		t := l.token()
		switch t {
		case "":
			return ops
		case "true":
			args = append(args, true)
			continue
		case "false":
			args = append(args, false)
			continue
		case "null":
			args = append(args, nil)
			continue
		case "BI":
			if s := scanInlineImage(l, doc); s != nil {
				ops = append(ops, op{name: "INLINE_IMAGE", inline: s})
			}
			args = args[:0]
			continue
		}
		ops = append(ops, op{name: t, args: args})
		args = nil
	}
}

// scanInlineImage reads BI <dict entries> ID <data> EI.
func scanInlineImage(l *lexer, doc *Document) *Stream {
	d := Dict{}
	for {
		l.skipSpace()
		if l.pos >= len(l.b) {
			return nil
		}
		if l.b[l.pos] == '/' {
			l.pos++
			key := l.name()
			val, err := l.object()
			if err != nil {
				return nil
			}
			d[expandInlineKey(key)] = val
			continue
		}
		if l.peekToken() == "ID" {
			l.token()
			break
		}
		if l.token() == "" {
			return nil
		}
	}
	// Exactly one whitespace byte separates ID from the data.
	if l.pos < len(l.b) && isWhite(l.b[l.pos]) {
		l.pos++
	}
	start := l.pos
	// The data ends at "EI" delimited by whitespace — the only reliable
	// marker, since an inline image carries no length.
	for i := start; i+1 < len(l.b); i++ {
		if l.b[i] == 'E' && l.b[i+1] == 'I' &&
			(i == 0 || isWhite(l.b[i-1])) &&
			(i+2 >= len(l.b) || isWhite(l.b[i+2]) || isDelim(l.b[i+2])) {
			data := l.b[start:i]
			if i > start && isWhite(l.b[i-1]) {
				data = l.b[start : i-1]
			}
			l.pos = i + 2
			return &Stream{Dict: d, Raw: data, doc: doc}
		}
	}
	l.pos = len(l.b)
	return nil
}

// expandInlineKey maps an inline image's abbreviated keys to the names the
// image code reads.
func expandInlineKey(k Name) Name {
	switch k {
	case "W":
		return "Width"
	case "H":
		return "Height"
	case "BPC":
		return "BitsPerComponent"
	case "CS":
		return "ColorSpace"
	case "F":
		return "Filter"
	case "D":
		return "Decode"
	case "DP":
		return "DecodeParms"
	case "IM":
		return "ImageMask"
	case "I":
		return "Interpolate"
	}
	return k
}

// numArgs reads an operator's operands as floats, padding with zeros so a
// malformed stream cannot index out of range.
func numArgs(o op, n int) []float32 {
	out := make([]float32, n)
	// Take the LAST n operands: a stream with a stray extra operand still
	// means what it says.
	base := len(o.args) - n
	for i := 0; i < n; i++ {
		j := base + i
		if j < 0 || j >= len(o.args) {
			continue
		}
		if v, ok := numOf(o.args[j]); ok {
			out[i] = float32(v)
		}
	}
	return out
}

// nameArg reads the operand at i as a name.
func nameArg(o op, i int) Name {
	if i < 0 || i >= len(o.args) {
		return ""
	}
	n, _ := o.args[i].(Name)
	return n
}

// itoa is strconv.Itoa, for keys built while drawing.
func itoa(v int) string { return strconv.Itoa(v) }
