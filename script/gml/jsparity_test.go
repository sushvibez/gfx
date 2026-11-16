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
		{`"abc".includes("a",1)`, false},
		{`"abc".includes("c",2)`, true},
		{`"abc".startsWith("b",1)`, true},
		{`"abc".startsWith("a",1)`, false},
		{`"abc".endsWith("b",2)`, true},
		{`"abc".endsWith("c",2)`, false},
		{`"abc".endsWith("c")`, true},
		{`"7".padStart(3, "0")`, "007"},
		{`"7".padEnd(4, "ab")`, "7aba"},
		{`"x".padStart(4, "é")`, "éééx"},
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
		n := unitLen(s) // positions are UTF-16 units (jsstring.go)
		for _, name := range []string{"slice", "substring", "substr"} {
			start, end := sliceBounds(name, n, []any{a, b})
			if start < 0 || start > end || end > n {
				t.Fatalf("%s(%v, %v) on %d units cut [%d:%d]", name, a, b, n, start, end)
			}
			_ = unitSlice(s, start, end)
		}
		if i := unitIndex(s, "x", int(max(min(a, 1e9), -1e9))); i < -1 || i > n {
			t.Fatalf("unitIndex %d of %d", i, n)
		}
		_ = fmt.Sprint(unitLastIndex(s, "x", int(max(min(b, 1e9), -1e9))))
	})
}

// A function that calls itself without end is an error from the page, not
// a stack overflow that takes the host down.
func TestJSRunawayRecursionIsAnError(t *testing.T) {
	d, err := LoadString(`Item {
    function f(n) { return f(n + 1) }
    function fact(n) { return n <= 1 ? 1 : n * fact(n - 1) }
    property var deep: fact(170)
    property var result: f(0)
}`, NewEnv())
	if err == nil {
		if got := d.Property("result"); got != nil {
			t.Errorf("runaway recursion produced %v", got)
		}
		if got, _ := d.Property("deep").(float64); got < 7e306 || got > 8e306 {
			t.Errorf("fact(170) = %v: ordinary recursion no longer works", d.Property("deep"))
		}
	}
	if n := callDepth.Load(); n != 0 {
		t.Errorf("the call depth is %d after the calls returned", n)
	}
}

// Flags nothing here implements are refused, not silently ignored.
func TestRegExpFlags(t *testing.T) {
	for _, ok := range []string{"", "g", "gi", "ims", "u", "gimsu", "y", "giy"} {
		if _, err := newRegExp("a+", ok); err != nil {
			t.Errorf("flags %q: %v", ok, err)
		}
	}
	for _, bad := range []string{"d", "v", "gg", "x", "gid", "yy"} {
		if _, err := newRegExp("a+", bad); err == nil {
			t.Errorf("flags %q were accepted", bad)
		}
	}
}

// A negative repeat count is an error (a RangeError in JS), and padding
// with long characters stays under the byte limit.
func TestJSStringErrors(t *testing.T) {
	if d, err := LoadString(`Item { property var result: "a".repeat(-1) }`, NewEnv()); err == nil && d.Property("result") != nil {
		t.Errorf(`"a".repeat(-1) = %#v, want an error`, d.Property("result"))
	}
	// Four bytes a character: half the limit in characters is twice it in bytes.
	expr := fmt.Sprintf(`"a".padEnd(%d, "\u{1D11E}")`, maxStringBytes/2)
	if d, err := LoadString("Item { property var result: "+expr+" }", NewEnv()); err == nil {
		if s, ok := d.Property("result").(string); ok && len(s) > maxStringBytes {
			t.Errorf("padEnd built %d bytes, over the limit of %d", len(s), maxStringBytes)
		}
	}
}
