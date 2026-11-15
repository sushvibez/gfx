package gml

import (
	"fmt"
	"testing"
)

// jsValue evaluates a JS expression as a property of a document.
func jsValue(t *testing.T, expr string) any {
	t.Helper()
	d, err := LoadString("Item { property var result: "+expr+" }", NewEnv())
	if err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	return d.Property("result")
}

// Language-only expressions against what Node prints for them. A page's
// script must never panic the host, whatever indices it passes.
func TestJSParity(t *testing.T) {
	for _, c := range []struct {
		expr string
		want any
	}{
		// String cuts: every bound clamped, by each method's own rules.
		{`"abc".slice(0,-9)`, ""},
		{`"abc".slice(-2)`, "bc"},
		{`"abc".slice(-9, 2)`, "ab"},
		{`"abc".slice(2, 1)`, ""},
		{`"abc".slice(1e30)`, ""},
		{`"abc".slice(0/0, 2)`, "ab"},
		{`"abc".substring(2,0)`, "ab"},
		{`"abc".substring(-5, 99)`, "abc"},
		{`"abc".substring(1)`, "bc"},
		{`"abc".substr(-2, 1)`, "b"},
		{`"abc".substr(1, -4)`, ""},
		{`"abc".substr(1, 1e30)`, "bc"},
		{`"".slice(-1, 1)`, ""},
		// Searching: a position argument, and the same unit as charAt.
		{`"abc".indexOf("a",1)`, -1.0},
		{`"abcabc".indexOf("c",3)`, 5.0},
		{`"abc".indexOf("c",-7)`, 2.0},
		{`"éx".indexOf("x")`, 1.0},
		{`"éxéx".lastIndexOf("x")`, 3.0},
		{`"abcabc".lastIndexOf("a", 2)`, 0.0},
		{`"abc".lastIndexOf("z")`, -1.0},
		// Strict equality does not coerce between kinds.
		{`1 === "1"`, false},
		{`1 !== "1"`, true},
		{`1 == "1"`, true},
		{`1 === 1.0`, true},
		{`"a" === "a"`, true},
		{`true === 1`, false},
		{`true == 1`, true},
		{`null === 0`, false},
		{`0 === false`, false},
		// Bitwise operators are 32-bit; shift counts wrap at 32.
		{`1 << 32`, 1.0},
		{`1 << 31`, -2147483648.0},
		{`1 << -1`, -2147483648.0},
		{`-8 >> 1`, -4.0},
		{`5.9 | 0`, 5.0},
		{`4294967297 | 0`, 1.0},
		{`2147483648 | 0`, -2147483648.0},
		{`6 & 3`, 2.0},
		{`6 ^ 3`, 5.0},
		{`-1 >>> 0`, 4294967295.0},
		{`-8 >>> 28`, 15.0},
		{`~5`, -6.0},
		{`~4294967295`, 0.0},
		{`1 + 2 ^ 4`, 7.0},
		// JSON: what JSON cannot write is null.
		{`JSON.stringify(0/0)`, "null"},
		{`JSON.stringify([1, 1/0])`, "[1,null]"},
		{`JSON.stringify("a")`, `"a"`},
		{`JSON.stringify()`, nil},
	} {
		if got := jsValue(t, c.expr); got != c.want {
			t.Errorf("%s = %#v, want %#v", c.expr, got, c.want)
		}
	}
}

// A page cannot ask the host for an enormous string.
func TestJSStringSizeIsBounded(t *testing.T) {
	for _, expr := range []string{`"abcdefgh".repeat(1e9)`, `"a".padStart(2e9, "b")`, `"a".padEnd(2e9)`} {
		d, err := LoadString("Item { property var result: "+expr+" }", NewEnv())
		if err == nil {
			if s, ok := d.Property("result").(string); ok && len(s) > maxStringBytes {
				t.Errorf("%s built %d bytes", expr, len(s))
			}
		}
	}
	if got := jsValue(t, `"ab".repeat(3)`); got != "ababab" {
		t.Errorf(`"ab".repeat(3) = %v`, got)
	}
}

func FuzzStringCuts(f *testing.F) {
	f.Add("abc", 0.0, -9.0)
	f.Add("é𝄞x", 1e30, -1e30)
	f.Fuzz(func(t *testing.T, s string, a, b float64) {
		n := len([]rune(s))
		for _, name := range []string{"slice", "substring", "substr"} {
			start, end := sliceBounds(name, n, []any{a, b})
			if start < 0 || start > end || end > n {
				t.Fatalf("%s(%v, %v) on %d characters cut [%d:%d]", name, a, b, n, start, end)
			}
		}
		if i := runeIndex(s, "x", int(max(min(a, 1e9), -1e9))); i < -1 || i > n {
			t.Fatalf("runeIndex %d of %d", i, n)
		}
		_ = fmt.Sprint(runeLastIndex(s, "x", int(max(min(b, 1e9), -1e9))))
	})
}
