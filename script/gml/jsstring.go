package gml

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// String positions.
//
// A JS string is a sequence of UTF-16 code units, and every position a
// page can see is counted in them: length, charAt, charCodeAt, indexOf,
// slice, a regex match's index. A string here is a Go string, UTF-8. For
// everything in the Basic Multilingual Plane the two counts agree with
// counting characters; a character outside it (an emoji) is ONE character
// and TWO units, and counting characters put every position after it one
// short of what the page computed.
//
// So positions are units, worked out by walking the UTF-8: no []rune or
// []uint16 copy is made, and a string that is all ASCII — nearly every
// string a page indexes — is cut by byte offset directly, which is cheaper
// than the []rune conversion each operation used to make.
//
// One thing a Go string cannot hold is half of a surrogate pair. A cut
// that goes through a pair has U+FFFD where the half would be: its length
// and the positions around it are still JS's, but the two halves of a
// split character do not join back into it.

// isASCII reports whether s has no multi-byte character, so that unit,
// character and byte offsets are all the same thing.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// unitLen is the length of s in UTF-16 code units.
func unitLen(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x80 || c >= 0xC0 { // not a continuation byte: a character starts
			n++
			if c >= 0xF0 { // four bytes: outside the BMP, a surrogate pair
				n++
			}
		}
	}
	return n
}

// unitOffset is the byte offset of unit u of s. mid reports that u is the
// second unit of the surrogate pair whose character starts at that offset.
// A u past the end gives len(s).
func unitOffset(s string, u int) (off int, mid bool) {
	i := 0
	for u > 0 && i < len(s) {
		c := s[i]
		switch {
		case c < 0x80:
			i++
		case c < 0xE0:
			i += 2
		case c < 0xF0:
			i += 3
		default:
			if u == 1 {
				return i, true
			}
			i += 4
			u--
		}
		u--
	}
	return min(i, len(s)), false
}

// unitSlice is units [start, end) of s, 0 <= start <= end <= unitLen(s).
// It is a substring of s (no copy) unless a bound falls inside a surrogate
// pair, where the half is U+FFFD.
func unitSlice(s string, start, end int) string {
	if start >= end {
		return ""
	}
	if isASCII(s) {
		return s[min(start, len(s)):min(end, len(s))]
	}
	from, midFrom := unitOffset(s, start)
	n, midTo := unitOffset(s[from:], end-start+boolInt(midFrom))
	to := from + n
	if !midFrom && !midTo {
		return s[from:to]
	}
	var b strings.Builder
	if midFrom {
		b.WriteRune(utf8.RuneError)
		from += 4
	}
	if from < to {
		b.WriteString(s[from:to])
	}
	if midTo {
		b.WriteRune(utf8.RuneError)
	}
	return b.String()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// unitAt is unit i of s, and whether there is one.
func unitAt(s string, i int) (uint16, bool) {
	if i < 0 {
		return 0, false
	}
	off, mid := unitOffset(s, i)
	if off >= len(s) {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(s[off:])
	if r >= 0x10000 {
		hi, lo := utf16.EncodeRune(r)
		if mid {
			return uint16(lo), true
		}
		return uint16(hi), true
	}
	return uint16(r), true
}

// unitChar is the one-unit string at position i (charAt, s[i]), and
// whether there is one. Half of a surrogate pair is U+FFFD.
func unitChar(s string, i int) (string, bool) {
	if i < 0 {
		return "", false
	}
	off, mid := unitOffset(s, i)
	if off >= len(s) {
		return "", false
	}
	_, size := utf8.DecodeRuneInString(s[off:])
	if mid || size == 4 {
		return string(utf8.RuneError), true
	}
	return s[off : off+size], true
}

// unitIndex is where sub first occurs in s at or after unit from, in
// units, or -1.
func unitIndex(s, sub string, from int) int {
	if from < 0 {
		from = 0
	}
	off, mid := unitOffset(s, from)
	if mid { // inside a pair: the search goes on from the next character
		off = min(off+4, len(s))
	}
	i := strings.Index(s[off:], sub)
	if i < 0 {
		return -1
	}
	return unitLen(s[:off+i])
}

// unitLastIndex is where sub last occurs in s starting at or before unit
// from, in units, or -1.
func unitLastIndex(s, sub string, from int) int {
	if from < 0 {
		from = 0
	}
	off, _ := unitOffset(s, from)
	i := strings.LastIndex(s[:min(off+len(sub), len(s))], sub)
	if i < 0 {
		return -1
	}
	return unitLen(s[:i])
}

// fromUnits is the string a sequence of UTF-16 code units spells
// (String.fromCharCode): pairs join into their character, a half alone is
// U+FFFD.
func fromUnits(units []uint16) string {
	return string(utf16.Decode(units))
}
