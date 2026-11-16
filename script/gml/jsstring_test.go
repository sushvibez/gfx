package gml

import (
	"math"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// A JS string is counted in UTF-16 code units: a character outside the
// Basic Multilingual Plane (an emoji) is two of them. Every position —
// length, charAt, charCodeAt, indexOf, slice, a regex match's index — is
// in that unit, as Node prints it.
func TestJSStringUTF16(t *testing.T) {
	for _, c := range []struct {
		expr string
		want any
	}{
		{`"😀".length`, 2.0},
		{`"a😀b".length`, 4.0},
		{`"é".length`, 1.0},
		{`"日本語".length`, 3.0},
		{`"".length`, 0.0},
		{`"a😀b".charAt(0)`, "a"},
		{`"a😀b".charAt(3)`, "b"},
		{`"a😀b".charAt(4)`, ""},
		{`"a😀b"[3]`, "b"},
		{`"a😀b"[4] === undefined`, true},
		{`"日本語".charAt(1)`, "本"},
		{`"a😀b".charCodeAt(0)`, 97.0},
		{`"a😀b".charCodeAt(1)`, 55357.0}, // 0xD83D, the high surrogate
		{`"a😀b".charCodeAt(2)`, 56832.0}, // 0xDE00, the low one
		{`"a😀b".charCodeAt(3)`, 98.0},
		{`"A".charCodeAt()`, 65.0},
		{`"é".charCodeAt(0)`, 233.0},
		{`"a😀b".codePointAt(1)`, 128512.0},
		{`"a😀b".codePointAt(2)`, 56832.0},
		{`"a😀b".codePointAt(3)`, 98.0},
		{`"a😀b".codePointAt(9) === undefined`, true},
		{`"a😀b".indexOf("b")`, 3.0},
		{`"a😀b".indexOf("😀")`, 1.0},
		{`"😀😀".indexOf("😀", 1)`, 2.0},
		{`"😀😀".indexOf("😀", 2)`, 2.0},
		{`"😀😀".indexOf("😀", 3)`, -1.0},
		{`"a😀b".lastIndexOf("b")`, 3.0},
		{`"😀😀".lastIndexOf("😀")`, 2.0},
		{`"😀😀".lastIndexOf("😀", 1)`, 0.0},
		{`"a😀b".includes("b", 3)`, true},
		{`"a😀b".includes("b", 4)`, false},
		{`"a😀b".startsWith("b", 3)`, true},
		{`"a😀b".endsWith("😀", 3)`, true},
		{`"a😀b".slice(3)`, "b"},
		{`"a😀b".slice(1, 3)`, "😀"},
		{`"a😀b".slice(0, 1)`, "a"},
		{`"a😀b".slice(-1)`, "b"},
		{`"a😀b".slice(-3, -1)`, "😀"},
		{`"a😀b".substring(1, 3)`, "😀"},
		{`"a😀b".substr(1, 2)`, "😀"},
		{`"日本語".slice(1, 2)`, "本"},
		{`"😀".padStart(4, "x")`, "xx😀"},
		{`"😀".padEnd(3, "x")`, "😀x"},
		{`"x".padStart(5, "😀")`, "😀😀x"},
		{`"abc".at(-1)`, "c"},
		{`"abc".at(0)`, "a"},
		{`"abc".at(5) === undefined`, true},
		{`String.fromCharCode(0xD83D, 0xDE00)`, "😀"},
		{`String.fromCharCode(65, 66)`, "AB"},
		{`String.fromCharCode(0x30A0)`, "゠"},
		{`String.fromCodePoint(128512, 97)`, "😀a"},
		{`"a😀b".search(/b/)`, 3.0},
		{`/b/.exec("a😀b").index`, 3.0},
		// Iteration is by code point, as in JS.
		{`[..."a😀"].length`, 2.0},
		{`(function() { var n = 0; for (var c of "a😀b") { n++ } return n })()`, 3.0},
	} {
		if got := jsRaw(t, c.expr); got != c.want {
			t.Errorf("%s = %#v, want %#v", c.expr, got, c.want)
		}
	}
	if got, _ := jsRaw(t, `"abc".charCodeAt(9)`).(float64); !math.IsNaN(got) {
		t.Errorf(`"abc".charCodeAt(9) = %v, want NaN`, got)
	}
	// What cannot be said in a Go string: half of a surrogate pair. A cut
	// through a pair holds U+FFFD for the half, and the positions around
	// it still count as JS counts them.
	for _, c := range []struct {
		expr string
		want any
	}{
		{`"a😀b".charAt(1)`, "�"},
		{`"a😀b".slice(0, 2)`, "a�"},
		{`"a😀b".slice(2)`, "�b"},
		{`"a😀b".slice(2).length`, 2.0},
	} {
		if got := jsRaw(t, c.expr); got != c.want {
			t.Errorf("%s = %#v, want %#v", c.expr, got, c.want)
		}
	}
}

// The unit arithmetic against the standard library, over every cut of
// strings mixing one-, two-, three- and four-byte characters.
func TestStringUnitOffsets(t *testing.T) {
	for _, s := range []string{"", "abc", "éa", "日本語", "a😀b", "😀😀", "x😀é日😀"} {
		units := utf16.Encode([]rune(s))
		if got := unitLen(s); got != len(units) {
			t.Errorf("unitLen(%q) = %d, want %d", s, got, len(units))
		}
		for i := 0; i <= len(units); i++ {
			for j := i; j <= len(units); j++ {
				want := string(utf16.Decode(units[i:j]))
				if got := unitSlice(s, i, j); got != want {
					t.Errorf("unitSlice(%q, %d, %d) = %q, want %q", s, i, j, got, want)
				}
			}
			if i < len(units) {
				if u, ok := unitAt(s, i); !ok || u != units[i] {
					t.Errorf("unitAt(%q, %d) = %#x, %v, want %#x", s, i, u, ok, units[i])
				}
			}
		}
		if _, ok := unitAt(s, len(units)); ok {
			t.Errorf("unitAt(%q, %d) is past the end", s, len(units))
		}
	}
}

// Whatever indices a page passes, a cut is valid UTF-8 and never panics.
func FuzzStringUnits(f *testing.F) {
	f.Add("a😀b", 1, 3)
	f.Add("日本語", -2, 9)
	f.Add("\xff\xfe😀", 0, 2)
	f.Fuzz(func(t *testing.T, s string, i, j int) {
		if !utf8.ValidString(s) {
			return
		}
		n := unitLen(s)
		i, j = max(0, min(i, n)), max(0, min(j, n))
		if i > j {
			i, j = j, i
		}
		if out := unitSlice(s, i, j); !utf8.ValidString(out) || unitLen(out) != j-i {
			t.Errorf("unitSlice(%q, %d, %d) = %q", s, i, j, out)
		}
	})
}
