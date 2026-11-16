package gml

import (
	"strings"
	"testing"
)

// callRun loads an Item whose body is the given declarations and calls its
// function run().
func callRun(t *testing.T, decls string) (any, error) {
	t.Helper()
	d, err := LoadString("Item {\n"+decls+"\n}", NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v\n%s", err, decls)
	}
	fn, ok := d.env.Get("run")
	if !ok {
		t.Fatal("run() not declared")
	}
	return callValue(fn, nil, d.env, 0, "run")
}

// var belongs to the function, let and const to the block; a closure made
// in a loop keeps the let of its own iteration and shares a var.
func TestJSScopes(t *testing.T) {
	for _, c := range []struct {
		body string
		want any
	}{
		// var: visible in the whole function, from its start.
		{`if (true) { var a = 1 } return a`, 1.0},
		{`for (var i = 0; i < 3; i++) { } return i`, 3.0},
		{`{ { var deep = 4 } } return deep`, 4.0},
		{`var r = (x === undefined); var x = 1; return r`, true},
		{`var r = typeof x; var x = 1; return r`, "undefined"},
		{`var x = 1; var x; return x`, 1.0},
		{`for (var i = 0; i < 3; i++) { var acc; if (i === 0) acc = 5 } return acc`, 5.0},
		{`try { var t = 1 } catch (e) { } return t`, 1.0},
		{`switch (1) { case 1: var s = 2 } return s`, 2.0},
		// let and const: the block they are declared in, and no further.
		{`{ let a = 1 } return typeof a`, "undefined"},
		{`if (true) { const c = 3 } return typeof c`, "undefined"},
		{`for (let i = 0; i < 3; i++) { } return typeof i`, "undefined"},
		{`let a = 1; { let a = 2 } return a`, 1.0},
		{`let a = 1; { a = 2 } return a`, 2.0},
		{`const a = 1; { const a = 2; { const a = 3 } } return a`, 1.0},
		{`let a = 1; if (true) { let a = a0() } return a; function a0() { return 9 }`, 1.0},
		{`let u; return u === undefined`, true},
		// A const binding cannot be assigned; what it holds can change.
		{`const o = {a: 1}; o.a = 2; return o.a`, 2.0},
		{`const xs = []; xs.push(1); xs.push(2); return xs.length`, 2.0},
		{`const xs = [1, 2]; xs[0] = 9; return xs[0]`, 9.0},
		// Closures in loops.
		{`var fs = []; for (let i = 0; i < 3; i++) { fs.push(() => i) } return fs.map(f => f()).join(",")`, "0,1,2"},
		{`var fs = []; for (var i = 0; i < 3; i++) { fs.push(() => i) } return fs.map(f => f()).join(",")`, "3,3,3"},
		{`var fs = []; for (var i = 0; i < 3; i++) { let j = i * 2; fs.push(() => j) } return fs.map(f => f()).join(",")`, "0,2,4"},
		{`var fs = []; for (var i = 0; i < 3; i++) { var j = i * 2; fs.push(() => j) } return fs.map(f => f()).join(",")`, "4,4,4"},
		{`var fs = []; for (let x of [1, 2, 3]) { fs.push(() => x) } return fs.map(f => f()).join(",")`, "1,2,3"},
		{`var fs = []; for (const x of [1, 2, 3]) { fs.push(() => x) } return fs.map(f => f()).join(",")`, "1,2,3"},
		{`var fs = []; for (var x of [1, 2, 3]) { fs.push(() => x) } return fs.map(f => f()).join(",")`, "3,3,3"},
		{`var fs = []; for (let i = 0; i < 3; i++) { fs.push(() => i); i++ } return fs.map(f => f()).join(",")`, "1,3"},
		{`var fs = []; for (let i = 0; i < 3; i++) { fs.push(function() { return i }) } return fs.map(f => f()).join(",")`, "0,1,2"},
		{`var n = 0; for (let i = 0; i < 5; i++) { n += i } return n`, 10.0},
		// Loop variables without a declaration, and with var, outlive the loop.
		{`var x; for (x of [1, 2]) { } return x`, 2.0},
		{`for (var y of [1, 2]) { } return y`, 2.0},
		// for-in: the keys of an object (sorted here: a map has no order),
		// the indices of an array, as strings.
		{`var ks = []; for (var k in {b: 1, a: 2}) { ks.push(k) } return ks.join(",")`, "a,b"},
		{`var ks = []; for (const i in [5, 6]) { ks.push(typeof i + i) } return ks.join(",")`, "string0,string1"},
		// Function declarations are hoisted to the top of their block.
		{`return helper(); function helper() { return 7 }`, 7.0},
		{`var r; { r = h(); function h() { return 1 } } return r`, 1.0},
	} {
		got, err := callRun(t, "function run() { "+c.body+" }")
		if err != nil {
			t.Errorf("%s\n error: %v", c.body, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s\n got %#v, want %#v", c.body, got, c.want)
		}
	}
}

// Reading or writing a let or const before its declaration has run is an
// error, even where an outer variable of the same name exists; so is
// assigning to a const.
func TestJSDeadZoneAndConst(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{`var r = a; let a = 1; return r`, "before initialization"},
		{`a = 2; let a = 1`, "before initialization"},
		{`var r = c; const c = 1; return r`, "before initialization"},
		{`{ var r = b; let b = 1 }`, "before initialization"},
		{`let a = 1; { var r = a; let a = 2 }`, "before initialization"},
		{`var r = g(); let a = 1; return r; function g() { return a }`, "before initialization"},
		{`const c = 1; c = 2`, "constant"},
		{`const c = 1; c++`, "constant"},
		{`const c = 1; c += 1`, "constant"},
		{`const c = 1; { c = 2 }`, "constant"},
		{`const c = 1; (() => { c = 2 })()`, "constant"},
		{`for (const x of [1, 2]) { x = 5 }`, "constant"},
	} {
		_, err := callRun(t, "function run() { "+c.body+" }")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s\n error %v, want one mentioning %q", c.body, err, c.want)
		}
	}
	// The dead zone ends at the declaration, and a caught error leaves the
	// rest of the function working.
	got, err := callRun(t, `function run() {
        var r
        try { r = a } catch (e) { r = "dead" }
        let a = 1
        return r + a + g()
        function g() { return a }
    }`)
	if err != nil || got != "dead11" {
		t.Errorf("dead zone then use: %#v, %v", got, err)
	}
}

// Declaring a name twice in one scope is refused when the page loads, as
// a SyntaxError is in JS — unless both declarations are var.
func TestJSRedeclaration(t *testing.T) {
	for _, body := range []string{
		`let a = 1; let a = 2`,
		`const a = 1; let a`,
		`let a; const a = 1`,
		`var a; let a`,
		`let a; var a`,
		`let a; { var a }`,
		`let f; function f() { }`,
		`{ let b; let b }`,
		`switch (1) { case 1: let s; break; case 2: let s }`,
		`for (let i = 0, i = 1; ;) { }`,
		`const c`,
	} {
		if _, err := LoadString("Item { function run() { "+body+" } }", NewEnv()); err == nil {
			t.Errorf("%s: loaded", body)
		}
	}
	if _, err := LoadString("Item { function run(p) { let p = 1 } }", NewEnv()); err == nil {
		t.Error("a let named like a parameter loaded")
	}
	for _, body := range []string{
		`var a; var a = 2`,
		`let a; { let a }`,
		`let a; (function() { let a })()`,
		`for (let i = 0; i < 1; i++) { let i = 5 }`,
		`{ let b } { let b }`,
		`var f; function f() { }`,
	} {
		if _, err := LoadString("Item { function run() { "+body+" } }", NewEnv()); err != nil {
			t.Errorf("%s: %v", body, err)
		}
	}
	if _, err := LoadString("Item { function run(p) { var p = 1; return p } }", NewEnv()); err != nil {
		t.Errorf("a var named like a parameter: %v", err)
	}
}

// A handler's var is the handler's: it does not leak into the document,
// and the next run starts without it.
func TestJSHandlerScope(t *testing.T) {
	if got := clickRun(t, `if (true) { var a = "x" }
        let b = "y"
        { let b = "z" }
        out = a + b + typeof leaked`); got != "xyundefined" {
		t.Errorf("out = %q, want xyundefined", got)
	}
	// Block bindings are function scopes too.
	if got := jsRaw(t, `{ if (true) { var v = 2 } let w = 3; return v * w }`); got != 6.0 {
		t.Errorf("block binding = %#v, want 6", got)
	}
}

// Format keeps the declaration kinds: printing a let as a var would change
// what the code means.
func TestFormatKeepsDeclarationKinds(t *testing.T) {
	src := "Item {\n    function f() {\n        let a = 1\n        const b = 2\n        var c\n        for (const x of xs) {\n            a += x\n        }\n        for (var k in o) {\n            c = k\n        }\n    }\n}\n"
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"let a = 1", "const b = 2", "var c\n", "for (const x of xs)", "for (var k in o)"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatted source lost %q:\n%s", want, out)
		}
	}
}
