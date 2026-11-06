package pdf

// Code-to-text mapping: the base encodings a simple font selects, its
// /Differences, and the /ToUnicode CMap a composite font carries. Only the
// text matters here — the widths come from the PDF itself.

import "strconv"

// encodingMap builds a font's code-to-rune map from its /Encoding.
func encodingMap(doc *Document, dict Dict, base Name) map[int]rune {
	out := make(map[int]rune, 256)
	table := standardEncoding
	if isSymbolFont(base) {
		// A symbol font's codes mean whatever its own face says, so no
		// base encoding is assumed.
		table = [256]rune{}
	}
	switch enc := doc.Resolve(dict["Encoding"]).(type) {
	case Name:
		table = encodingByName(enc, table)
	case Dict:
		if n, ok := doc.Resolve(enc["BaseEncoding"]).(Name); ok {
			table = encodingByName(n, table)
		}
		for i, r := range table {
			if r != 0 {
				out[i] = r
			}
		}
		// /Differences is [code /name /name … code /name …].
		if diffs, ok := doc.Resolve(enc["Differences"]).(Array); ok {
			code := 0
			for _, el := range diffs {
				switch v := doc.Resolve(el).(type) {
				case Name:
					if r, ok := glyphRune(v); ok {
						out[code] = r
					}
					code++
				default:
					if n, ok := numOf(v); ok {
						code = int(n)
					}
				}
			}
		}
		return out
	}
	for i, r := range table {
		if r != 0 {
			out[i] = r
		}
	}
	return out
}

func encodingByName(n Name, def [256]rune) [256]rune {
	switch n {
	case "WinAnsiEncoding":
		return winAnsiEncoding
	case "MacRomanEncoding":
		return macRomanEncoding
	case "StandardEncoding", "PDFDocEncoding":
		return standardEncoding
	}
	return def
}

// isSymbolFont reports the standard-14 faces whose codes are not text.
func isSymbolFont(base Name) bool {
	s := string(base)
	if i := indexByte(s, '+'); i >= 0 && i+1 < len(s) { // strip a subset tag
		s = s[i+1:]
	}
	return s == "Symbol" || s == "ZapfDingbats"
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// glyphRune maps a PostScript glyph name to a rune: the names that are
// literally "uniXXXX", the single-letter names, and the common Latin set.
func glyphRune(n Name) (rune, bool) {
	s := string(n)
	if len(s) == 7 && s[:3] == "uni" {
		if v, err := strconv.ParseUint(s[3:], 16, 32); err == nil {
			return rune(v), true
		}
	}
	if len(s) >= 2 && s[0] == 'u' {
		if v, err := strconv.ParseUint(s[1:], 16, 32); err == nil && len(s) >= 5 {
			return rune(v), true
		}
	}
	if r, ok := glyphNames[s]; ok {
		return r, true
	}
	if len(s) == 1 {
		return rune(s[0]), true
	}
	// "gXX" / "cidXX" name their glyph by index, which says nothing about
	// text; report nothing rather than a wrong character.
	return 0, false
}

// toUnicode reads a /ToUnicode CMap: the "bfchar" and "bfrange" mappings
// that give a code its text.
func (rd *Renderer) toUnicode(dict Dict) map[int]rune {
	s, ok := rd.doc.Resolve(dict["ToUnicode"]).(*Stream)
	if !ok {
		return nil
	}
	data, err := s.Decode()
	if err != nil {
		return nil
	}
	out := map[int]rune{}
	l := &lexer{b: data}
	var pending []Object
	for {
		l.skipSpace()
		if l.pos >= len(data) {
			break
		}
		c := data[l.pos]
		if c == '<' || c == '[' || c == '(' || c == '/' || (c >= '0' && c <= '9') || c == '-' || c == '.' {
			o, err := l.object()
			if err != nil {
				l.pos++
				continue
			}
			pending = append(pending, o)
			continue
		}
		switch l.token() {
		case "":
			return out
		case "endbfchar":
			// pairs: <src> <dst>
			for i := 0; i+1 < len(pending); i += 2 {
				src, ok1 := pending[i].(String)
				dst, ok2 := pending[i+1].(String)
				if ok1 && ok2 {
					out[codeOf(src)] = firstRune(dst)
				}
			}
			pending = nil
		case "endbfrange":
			// triples: <lo> <hi> <dst> | <lo> <hi> [<dst> …]
			for i := 0; i+2 < len(pending); i += 3 {
				lo, ok1 := pending[i].(String)
				hi, ok2 := pending[i+1].(String)
				if !ok1 || !ok2 {
					continue
				}
				l0, h0 := codeOf(lo), codeOf(hi)
				if h0 < l0 || h0-l0 > 65535 {
					continue
				}
				switch dst := pending[i+2].(type) {
				case String:
					r := firstRune(dst)
					for c := l0; c <= h0; c++ {
						out[c] = r + rune(c-l0)
					}
				case Array:
					for j, el := range dst {
						if s, ok := el.(String); ok && l0+j <= h0 {
							out[l0+j] = firstRune(s)
						}
					}
				}
			}
			pending = nil
		case "endcidrange", "endcidchar", "endcodespacerange":
			pending = nil
		default:
			pending = nil
		}
	}
	return out
}

// codeOf reads a CMap code (big-endian bytes).
func codeOf(s String) int {
	v := 0
	for _, b := range s {
		v = v<<8 | int(b)
	}
	return v
}

// firstRune reads a UTF-16BE destination string's first character.
func firstRune(s String) rune {
	if len(s) >= 4 {
		hi := rune(s[0])<<8 | rune(s[1])
		lo := rune(s[2])<<8 | rune(s[3])
		if hi >= 0xD800 && hi <= 0xDBFF && lo >= 0xDC00 && lo <= 0xDFFF {
			return 0x10000 + (hi-0xD800)<<10 + (lo - 0xDC00)
		}
	}
	if len(s) >= 2 {
		return rune(s[0])<<8 | rune(s[1])
	}
	if len(s) == 1 {
		return rune(s[0])
	}
	return 0
}
