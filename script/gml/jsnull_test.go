package gml

import (
	"testing"
)

// jsRaw evaluates a JS expression as a property of a document and returns
// the value as the engine holds it — Null not yet turned into the nil a
// host reads (Document.Property does that).
func jsRaw(t *testing.T, expr string) any {
	t.Helper()
	d, err := LoadString("Item { property var result: "+expr+" }", NewEnv())
	if err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	return d.doc.props["result"].get()
}

// null and undefined are two values: equal under ==, not under ===, and
// they coerce differently (null is 0 as a number, undefined is "not given").
func TestJSNullAndUndefined(t *testing.T) {
	const fns = `(function() {
		function dflt(a, b = 5) { return b }
		function isUndef(a) { return a === undefined }
		function isNull(a) { return a === null }
		function nothing() { }
		var unset
		return [%s]
	})()[0]`
	for _, c := range []struct {
		expr string
		want any
	}{
		// Equality.
		{`null === undefined`, false},
		{`null !== undefined`, true},
		{`null == undefined`, true},
		{`null != undefined`, false},
		{`null === null`, true},
		{`undefined === undefined`, true},
		{`null == 0`, false},
		{`null == false`, false},
		{`null == ""`, false},
		{`undefined == 0`, false},
		// typeof.
		{`typeof null`, "object"},
		{`typeof undefined`, "undefined"},
		{`typeof ({}).missing`, "undefined"},
		{`typeof notDeclaredAnywhere`, "undefined"},
		// What is absent is undefined, never null.
		{`({}).missing === undefined`, true},
		{`({}).missing === null`, false},
		{`({a: null}).a === null`, true},
		{`({a: null}).a === undefined`, false},
		{`[1][5] === undefined`, true},
		{`[1][5] === null`, false},
		{`[null][0] === null`, true},
		{`"abc"[9] === undefined`, true},
		// Coercion in arguments: null is 0, undefined is "not given".
		{`"abc".slice(0, null)`, ""},
		{`"abc".slice(0, undefined)`, "abc"},
		{`"abc".slice(0)`, "abc"},
		{`"abc".slice(null, 2)`, "ab"},
		{`"abc".substring(1, null)`, "a"},
		{`"abc".substring(1, undefined)`, "bc"},
		{`"abc".substr(1, null)`, ""},
		{`"abc".substr(1, undefined)`, "bc"},
		{`"abc".endsWith("c", undefined)`, true},
		{`"abc".endsWith("c", null)`, false},
		{`"abc".endsWith("", null)`, true},
		{`[1, 2, 3].slice(0, null).length`, 0.0},
		{`[1, 2, 3].slice(0, undefined).length`, 3.0},
		// Arithmetic and logic.
		{`null + 1`, 1.0},
		{`null * 5`, 0.0},
		{`null >= 0`, true},
		{`null > 0`, false},
		{`!null`, true},
		{`!undefined`, true},
		{`null ? 1 : 2`, 2.0},
		{`null || "d"`, "d"},
		// ?? and ?. treat both alike.
		{`null ?? "d"`, "d"},
		{`undefined ?? "d"`, "d"},
		{`0 ?? "d"`, 0.0},
		{`"" ?? "d"`, ""},
		{`(null)?.x === undefined`, true},
		{`(undefined)?.x === undefined`, true},
		{`({a: null}).a?.b === undefined`, true},
		{`({a: null}).a?.b.c() === undefined`, true},
		// JSON.
		{`JSON.stringify(null)`, "null"},
		{`JSON.stringify(undefined) === undefined`, true},
		{`JSON.stringify({a: null, b: undefined})`, `{"a":null}`},
		{`JSON.stringify([null, undefined, 1])`, `[null,null,1]`},
		{`JSON.parse("null") === null`, true},
		{`JSON.parse('{"a":null}').a === null`, true},
		{`JSON.parse('{"a":null}').b === undefined`, true},
		{`JSON.parse('[null]')[0] === null`, true},
	} {
		if got := jsRaw(t, c.expr); got != c.want {
			t.Errorf("%s = %#v, want %#v", c.expr, got, c.want)
		}
	}
	// Omitted, explicit undefined and explicit null arguments.
	for _, c := range []struct {
		expr string
		want any
	}{
		{`dflt(1)`, 5.0},
		{`dflt(1, undefined)`, 5.0},
		{`dflt(1, null) === null`, true},
		{`dflt(1, 0)`, 0.0},
		{`isUndef()`, true},
		{`isUndef(undefined)`, true},
		{`isUndef(null)`, false},
		{`isNull()`, false},
		{`isNull(undefined)`, false},
		{`isNull(null)`, true},
		{`nothing() === undefined`, true},
		{`nothing() === null`, false},
		{`unset === undefined`, true},
		{`unset === null`, false},
		{`unset == null`, true},
		{`typeof unset`, "undefined"},
	} {
		if got := jsRaw(t, sprintf1(fns, c.expr)); got != c.want {
			t.Errorf("%s = %#v, want %#v", c.expr, got, c.want)
		}
	}
}

// sprintf1 substitutes the one %s of a template (the templates here hold
// JS's own % operators nowhere, but keep fmt's verbs out of the cases).
func sprintf1(tmpl, s string) string {
	for i := 0; i+1 < len(tmpl); i++ {
		if tmpl[i] == '%' && tmpl[i+1] == 's' {
			return tmpl[:i] + s + tmpl[i+2:]
		}
	}
	return tmpl
}

// A host's Go nil keeps working: it is undefined, loosely equal to null,
// falsy, and what a host reads back for a null is nil again.
func TestJSNullAtTheHostBoundary(t *testing.T) {
	env := NewEnv()
	env.Set("h", nil)
	env.Set("n", Null)
	env.Set("row", map[string]any{"a": nil, "b": Null})
	var got []any
	env.Func("take", func(args []any) (any, error) { got = args; return nil, nil })
	for _, c := range []struct{ src, want string }{
		{`h == null`, "true"},
		{`h === undefined`, "true"},
		{`h === null`, "false"},
		{`typeof h`, "undefined"},
		{`h ? 1 : 2`, "2"},
		{`h ?? "d"`, "d"},
		{`n === null`, "true"},
		{`n == undefined`, "true"},
		{`typeof n`, "object"},
		{`row.a == null`, "true"},
		{`row.b === null`, "true"},
		{`take(null, undefined, 1) === undefined`, "true"},
	} {
		if s := evalStr(t, env, c.src); s != c.want {
			t.Errorf("%s = %q, want %q", c.src, s, c.want)
		}
	}
	// A host function is handed nil for null, as it always was.
	if len(got) != 3 || got[0] != nil || got[1] != nil || got[2] != 1.0 {
		t.Errorf("host function received %#v, want [nil nil 1]", got)
	}
	d, err := LoadString(`Item {
    property var a: null
    property var b
    property Item it
    property var aIsNull: a === null
    property var bIsUndefined: b === undefined
    property var itIsNull: it === null
}`, NewEnv())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "it"} {
		if v := d.Property(name); v != nil {
			t.Errorf("Property(%q) = %#v, want nil", name, v)
		}
	}
	for _, name := range []string{"aIsNull", "bIsUndefined", "itIsNull"} {
		if v := d.Property(name); v != true {
			t.Errorf("%s = %#v, want true", name, v)
		}
	}
}
