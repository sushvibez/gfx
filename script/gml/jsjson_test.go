package gml

import (
	"strings"
	"testing"
)

// JSON.stringify and JSON.parse against what Node prints. Keys come out
// sorted here (an object is a Go map), so the cases use sorted keys.
func TestJSONStringify(t *testing.T) {
	for _, c := range []struct {
		expr string
		want any
	}{
		// What has no JSON form: left out of an object, null in an array,
		// nothing at all at the top.
		{`JSON.stringify({a: 1, b: undefined, c: function() {}, d: () => 1})`, `{"a":1}`},
		{`JSON.stringify([1, undefined, function() {}, null])`, `[1,null,null,null]`},
		{`JSON.stringify(function() {}) === undefined`, true},
		{`JSON.stringify() === undefined`, true},
		{`JSON.stringify({})`, `{}`},
		{`JSON.stringify([])`, `[]`},
		{`JSON.stringify({a: {}})`, `{"a":{}}`},
		// Scalars.
		{`JSON.stringify("a\"b\\c\n")`, `"a\"b\\c\n"`},
		{`JSON.stringify("<a href='x'>&</a>")`, `"<a href='x'>&</a>"`},
		{`JSON.stringify("\u0001é")`, `"\u0001é"`},
		{`JSON.stringify(true)`, `true`},
		{`JSON.stringify(1.5)`, `1.5`},
		{`JSON.stringify(-0)`, `0`},
		{`JSON.stringify(1e21)`, `1e+21`},
		{`JSON.stringify(0.0000001)`, `1e-7`},
		{`JSON.stringify(123456789012)`, `123456789012`},
		{`JSON.stringify([0/0, 1/0, -1/0])`, `[null,null,null]`},
		// A replacer function sees the root under "" and every member after.
		{`JSON.stringify({a: 1, b: 2}, (k, v) => typeof v === "number" ? v * 2 : v)`, `{"a":2,"b":4}`},
		{`JSON.stringify({a: 1, b: "x"}, (k, v) => typeof v === "string" ? undefined : v)`, `{"a":1}`},
		{`JSON.stringify([1, 2], (k, v) => k === "0" ? 9 : v)`, `[9,2]`},
		{`JSON.stringify({a: 1}, (k, v) => k === "" ? [v.a] : v)`, `[1]`},
		// A replacer list keeps those keys, at every depth, and no others.
		{`JSON.stringify({a: 1, b: {a: 2, c: 3}, c: 4}, ["a", "b"])`, `{"a":1,"b":{"a":2}}`},
		{`JSON.stringify([{a: 1, z: 2}], ["z"])`, `[{"z":2}]`},
		// Indentation: a count of spaces (at most 10) or a string.
		{`JSON.stringify({a: 1, b: [1, 2], c: {}, d: []}, null, 2)`,
			"{\n  \"a\": 1,\n  \"b\": [\n    1,\n    2\n  ],\n  \"c\": {},\n  \"d\": []\n}"},
		{`JSON.stringify([1], null, "\t")`, "[\n\t1\n]"},
		{`JSON.stringify([1], null, 99)`, "[\n          1\n]"},
		{`JSON.stringify([1], null, "abcdefghijklmnop")`, "[\nabcdefghij1\n]"},
		{`JSON.stringify([1], null, 0)`, `[1]`},
		{`JSON.stringify({a: undefined}, null, 2)`, `{}`},
		// toJSON is asked first.
		{`JSON.stringify({a: {toJSON: () => "t"}})`, `{"a":"t"}`},
		{`JSON.stringify(new Date(0)).length`, 26.0},
		// The same object twice is fine; only containing itself is not.
		{`(function() { var o = {k: 1}; return JSON.stringify([o, o]) })()`, `[{"k":1},{"k":1}]`},
		// parse.
		{`JSON.parse('{"a":[1,2,{"b":null}]}').a[2].b === null`, true},
		{`JSON.parse("[1,2,3]", (k, v) => typeof v === "number" ? v + 1 : v).join(",")`, "2,3,4"},
		{`JSON.stringify(JSON.parse('{"a":1,"b":2}', (k, v) => k === "a" ? undefined : v))`, `{"b":2}`},
		{`JSON.stringify(JSON.parse(' {"x": [true, false, null, "s", 1.5]} '))`, `{"x":[true,false,null,"s",1.5]}`},
	} {
		if got := jsRaw(t, c.expr); got != c.want {
			t.Errorf("%s\n got %#v\nwant %#v", c.expr, got, c.want)
		}
	}
}

// A structure that contains itself is an error a page can catch, not a
// stack overflow; so is text that is not JSON.
func TestJSONErrors(t *testing.T) {
	for _, c := range []struct{ handler, want string }{
		{`var o = {}; o.self = o
          try { JSON.stringify(o); out = "no error" } catch (e) { out = e.message }`, "cyclic"},
		{`var a = [1]; a[0] = a
          try { JSON.stringify(a); out = "no error" } catch (e) { out = e.message }`, "cyclic"},
		{`var o = {a: {b: {}}}; o.a.b.up = o.a
          try { JSON.stringify(o); out = "no error" } catch (e) { out = e.message }`, "cyclic"},
		{`try { JSON.parse("{not json"); out = "no error" } catch (e) { out = e.message }`, "invalid character"},
		{`try { JSON.parse("[1,]"); out = "no error" } catch (e) { out = e.message }`, "invalid character"},
	} {
		if got := clickRun(t, c.handler); !strings.Contains(got, c.want) {
			t.Errorf("%s\n out = %q, want it to mention %q", c.handler, got, c.want)
		}
	}
}

// A host's own Go values still serialize, as encoding/json writes them,
// and a host's nil in a map is undefined: the member is left out.
func TestJSONHostValues(t *testing.T) {
	env := NewEnv()
	env.Set("names", []string{"a", "b"})
	env.Set("row", map[string]any{"a": nil, "b": float64(1), "c": Null})
	for _, c := range []struct{ src, want string }{
		{`JSON.stringify(names)`, `["a","b"]`},
		{`JSON.stringify(row)`, `{"b":1,"c":null}`},
	} {
		if got := evalStr(t, env, c.src); got != c.want {
			t.Errorf("%s = %s, want %s", c.src, got, c.want)
		}
	}
}
