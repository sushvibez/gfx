package gml

import "testing"

// A global or sticky RegExp remembers where its last match ended
// (lastIndex), and an exec result is an array that also says where it
// matched, in what, and its named groups — against what Node prints.
func TestRegExpLastIndexAndExecResult(t *testing.T) {
	const pre = `(function() {
		var re = /a(\d)/g
		var s = "a1 a2 a3"
		var out = []
		var m
		%s
	})()`
	for _, c := range []struct {
		body string
		want any
	}{
		// exec walks the matches, one per call, and starts over after null.
		{`m = re.exec(s); return m[0] + m[1] + "@" + m.index + "/" + re.lastIndex`, "a11@0/2"},
		{`re.exec(s); m = re.exec(s); return m[0] + "@" + m.index + "/" + re.lastIndex`, "a2@3/5"},
		{`re.exec(s); re.exec(s); re.exec(s); m = re.exec(s); return (m === null) + "/" + re.lastIndex`, "true/0"},
		{`while ((m = re.exec(s)) !== null) { out.push(m[1]) } return out.join(",")`, "1,2,3"},
		{`m = re.exec(s); return m.input === s`, true},
		{`m = re.exec(s); return m.length`, 2.0},
		{`m = re.exec(s); return m.groups === undefined`, true},
		{`m = re.exec(s); return JSON.stringify(m)`, `["a1","1"]`},
		{`m = re.exec(s); return m.join("|")`, "a1|1"},
		{`m = re.exec(s); return [...m].length`, 2.0},
		{`m = re.exec(s); for (var g of m) { out.push(g) } return out.join("")`, "a11"},
		{`return typeof re.exec(s)`, "object"},
		{`return re.lastIndex`, 0.0},
		// test advances a global regex too.
		{`var r = /a/g; return [r.test("aa"), r.test("aa"), r.test("aa")].join(",")`, "true,true,false"},
		// lastIndex can be set.
		{`re.lastIndex = 2; m = re.exec(s); return m[0] + "@" + m.index`, "a2@3"},
		{`re.lastIndex = 99; m = re.exec(s); return (m === null) + "/" + re.lastIndex`, "true/0"},
		{`var r = /aa/g; r.lastIndex = 1; return r.exec("aaa").index`, 1.0},
		{`var r = /^a/g; r.lastIndex = 1; return r.exec("aa") === null`, true},
		{`var r = /\bb/g; r.lastIndex = 1; return r.exec("ab b") .index`, 3.0},
		// A regex without g or y does not use lastIndex at all.
		{`var r = /a/; r.lastIndex = 5; m = r.exec("a"); return m.index + "/" + r.lastIndex`, "0/5"},
		// Sticky: the match has to start exactly at lastIndex.
		{`return /foo/y.test("barfoo")`, false},
		{`var r = /foo/y; r.lastIndex = 3; return r.test("barfoo") + "/" + r.lastIndex`, "true/6"},
		{`var r = /foo/y; r.lastIndex = 2; return r.test("barfoo") + "/" + r.lastIndex`, "false/0"},
		{`var r = /a/y; return [r.test("aab"), r.test("aab"), r.test("aab")].join(",")`, "true,true,false"},
		{`return /a/y.sticky + "," + /a/g.sticky + "," + /a/u.unicode + "," + /a/gy.flags`, "true,false,true,gy"},
		// A literal is a new RegExp every time it is evaluated.
		{`function t(x) { return /a/g.test(x) } return [t("a"), t("a"), t("a")].join(",")`, "true,true,true"},
		// Named groups, and groups that took no part.
		{`m = /(?<y>\d{4})-(?<mo>\d\d)/.exec("on 2024-05"); return m.groups.y + "/" + m.groups.mo + "@" + m.index`, "2024/05@3"},
		{`return /(a)|(b)/.exec("b")[1] === undefined`, true},
		{`return /(?<x>a)|(?<z>b)/.exec("b").groups.x === undefined`, true},
		// The String methods.
		{`return "abc".match(/x/) === null`, true},
		{`return "abc".match(/x/g) === null`, true},
		{`return "a1b2".match(/\d/).index`, 1.0},
		{`return "a1b2".match(/\d/).input`, "a1b2"},
		{`return "a1b2".match(/\d/g).join(",")`, "1,2"},
		{`re.lastIndex = 4; "a1 a2".match(re); return re.lastIndex`, 0.0},
		{`return [..."a1b2".matchAll(/\d/g)].map(x => x.index).join(",")`, "1,3"},
		{`return [..."a1b2".matchAll(/(\d)/g)][1][1]`, "2"},
		{`return "abc".replace(/b/, (x, off, str) => off + str)`, "a1abcc"},
		{`return "a1".replace(/(\d)/, (x, g1, off) => g1 + off)`, "a11"},
		{`re.lastIndex = 3; return "a1 a2".replace(re, "#") + re.lastIndex`, "# #0"},
		// Positions are UTF-16 units, like every other string position.
		{`var r = /b/g; m = r.exec("a😀b"); return m.index + "/" + r.lastIndex`, "3/4"},
		{`var r = /./g; r.lastIndex = 3; return r.exec("a😀b")[0]`, "b"},
	} {
		if got := jsRaw(t, sprintf1(pre, c.body)); got != c.want {
			t.Errorf("%s\n got %#v, want %#v", c.body, got, c.want)
		}
	}
}

// What `u` means here. Go's regexp matches by code point, so a pattern
// always behaves as JS's does WITH the u flag: `.` is a whole character,
// an emoji included, and a class or a quantifier applies to characters,
// not to halves of them. Without u, JS would see two code units; that is
// the one place a pattern can differ, and it is not modelled. \uXXXX and
// \u{...} escapes are understood either way.
func TestRegExpUnicode(t *testing.T) {
	for _, c := range []struct {
		expr string
		want any
	}{
		{`/^.$/u.test("😀")`, true},
		{`/^.$/.test("😀")`, true}, // JS without u: false (two units)
		{`/é/.test("café")`, true},
		{`/\u{1F600}/u.test("😀")`, true},
		{`/[A-C]+/.exec("xABCx")[0]`, "ABC"},
		{`/\\u0041/.test("A")`, false}, // an escaped backslash, then text
		{`/\\u0041/.test("\\u0041")`, true},
		{`/😀+/u.exec("a😀😀b")[0]`, "😀😀"},
		{`/[😀]/u.test("😀")`, true},
		{`/É/i.test("é")`, true},
		{`"a😀b".replace(/./gu, "x")`, "xxx"},
		{`"a😀b".split(/(?:)/u).length`, 3.0},
	} {
		if got := jsRaw(t, c.expr); got != c.want {
			t.Errorf("%s = %#v, want %#v", c.expr, got, c.want)
		}
	}
}
