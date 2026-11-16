package gml

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// jsRegExp is a JS RegExp value: a regex literal (/ab+/i) or RegExp(src,
// flags). Go's RE2 syntax covers what markup uses (classes, quantifiers,
// groups, alternation); lookaround and backreferences fail to compile,
// with the error naming the pattern.
//
// Matching is by code point, which is what JS does WITH the u flag: `.`
// is a whole character and a class holds whole characters, emoji
// included. A pattern without u behaves the same here, where JS would see
// the two UTF-16 units of such a character; that difference is not
// modelled. Positions that come out (index, lastIndex) are UTF-16 units,
// as every string position is (jsstring.go).
type jsRegExp struct {
	re     *regexp.Regexp
	source string
	flags  string
	// lastIndex is where the next exec or test starts, in UTF-16 units. It
	// is read and moved only by a global (g) or sticky (y) regex, but can
	// be assigned on any.
	lastIndex int
	// The matches of the last input a scan from its start makes, kept so
	// that the usual loop — exec until null — costs one scan of the input
	// and not one per call (see findFrom).
	scanned bool
	scanIn  string
	scan    [][]int
}

// newRegExp compiles a JS pattern with its flags: i, m and s map onto RE2's
// own; g (global) and y (sticky) make exec, test, match and replace use
// lastIndex; u is accepted (patterns match by code point here anyway, see
// jsRegExp). Any other flag is an error: d and v change what a match means
// or returns in JS, and nothing here implements them, so accepting them
// would run a different expression from the one written. So is a flag
// given twice, as in JS.
func newRegExp(source, flags string) (*jsRegExp, error) {
	for i, f := range flags {
		if !strings.ContainsRune("gimsuy", f) {
			return nil, fmt.Errorf("regex /%s/%s: flag %q is not supported", source, flags, string(f))
		}
		if strings.ContainsRune(flags[:i], f) {
			return nil, fmt.Errorf("regex /%s/%s: flag %q given twice", source, flags, string(f))
		}
	}
	prefix := ""
	for _, f := range "ims" {
		if strings.ContainsRune(flags, f) {
			prefix += string(f)
		}
	}
	pat := goPattern(source)
	if prefix != "" {
		pat = "(?" + prefix + ")" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("regex /%s/: %v", source, err)
	}
	return &jsRegExp{re: re, source: source, flags: flags}, nil
}

// goPattern rewrites the escapes JS has and RE2 does not: \uXXXX and
// \u{X…} become \x{…}. Everything else is passed through for RE2 to
// accept or refuse.
func goPattern(src string) string {
	if !strings.Contains(src, `\u`) {
		return src
	}
	var b strings.Builder
	for i := 0; i < len(src); i++ {
		if src[i] != '\\' || i+1 >= len(src) {
			b.WriteByte(src[i])
			continue
		}
		if src[i+1] != 'u' {
			b.WriteByte(src[i]) // an escape of something else: both characters
			b.WriteByte(src[i+1])
			i++
			continue
		}
		rest := src[i+2:]
		if strings.HasPrefix(rest, "{") {
			if end := strings.IndexByte(rest, '}'); end > 1 && isHexString(rest[1:end]) {
				b.WriteString(`\x{` + rest[1:end] + `}`)
				i += 2 + end
				continue
			}
		} else if len(rest) >= 4 && isHexString(rest[:4]) {
			b.WriteString(`\x{` + rest[:4] + `}`)
			i += 5
			continue
		}
		b.WriteString(`\u`) // not an escape this understands: RE2 will say so
		i++
	}
	return b.String()
}

func isHexString(s string) bool {
	for _, c := range s {
		if !isHex(c) {
			return false
		}
	}
	return s != ""
}

// fresh is a new RegExp with the same pattern: what evaluating a literal
// gives, every time (regexExpr).
func (r *jsRegExp) fresh() *jsRegExp {
	return &jsRegExp{re: r.re, source: r.source, flags: r.flags}
}

// regexExpr is a regex literal with the g or y flag: each evaluation is a
// RegExp of its own, starting at lastIndex 0.
type regexExpr struct{ re *jsRegExp }

func (x *regexExpr) eval(*Env) (any, error) { return x.re.fresh(), nil }

// String is the pattern source, which is what a validator's
// regularExpression is read as.
func (r *jsRegExp) String() string { return r.source }

func (r *jsRegExp) global() bool { return strings.ContainsRune(r.flags, 'g') }
func (r *jsRegExp) sticky() bool { return strings.ContainsRune(r.flags, 'y') }

// member answers the RegExp properties.
func (r *jsRegExp) member(name string) (any, bool) {
	switch name {
	case "source":
		return r.source, true
	case "flags":
		return r.flags, true
	case "global":
		return r.global(), true
	case "sticky":
		return r.sticky(), true
	case "ignoreCase":
		return strings.ContainsRune(r.flags, 'i'), true
	case "multiline":
		return strings.ContainsRune(r.flags, 'm'), true
	case "dotAll":
		return strings.ContainsRune(r.flags, 's'), true
	case "unicode":
		return strings.ContainsRune(r.flags, 'u'), true
	case "lastIndex":
		return float64(r.lastIndex), true
	}
	return nil, false
}

// setMember assigns a RegExp property; lastIndex is the writable one.
func (r *jsRegExp) setMember(name string, v any) bool {
	if name != "lastIndex" {
		return false
	}
	r.lastIndex = max(0, argInt([]any{v}, 0))
	return true
}

// call answers r.test(s) and r.exec(s): exec gives the match and its
// groups as an array (with index, input and groups), or null.
func (r *jsRegExp) call(name string, args []any) (any, bool) {
	s := ""
	if len(args) > 0 {
		s = str(args[0])
	}
	switch name {
	case "test":
		return r.exec(s) != nil, true
	case "exec":
		if m := r.exec(s); m != nil {
			return m, true
		}
		return Null, true
	case "toString":
		return "/" + r.source + "/" + r.flags, true
	}
	return nil, false
}

// exec is one match of s, or nil. A global or sticky regex starts at
// lastIndex and leaves it at the end of the match — or at 0 when there is
// none, so the next call starts over; any other regex matches from the
// start and leaves lastIndex alone.
func (r *jsRegExp) exec(s string) *jsMatch {
	if !r.global() && !r.sticky() {
		return r.matchOf(s, r.re.FindStringSubmatchIndex(s))
	}
	var loc []int
	if r.lastIndex <= len(s) { // a cheap bound: units never outnumber bytes
		// A position inside a surrogate pair is the character's start,
		// as it is for JS with the u flag.
		if pos, _ := unitOffset(s, r.lastIndex); pos < len(s) || r.lastIndex == unitLen(s) {
			loc = r.findFrom(s, pos)
		}
	}
	if loc == nil {
		r.lastIndex = 0
		return nil
	}
	r.lastIndex = unitLen(s[:loc[1]])
	return r.matchOf(s, loc)
}

// findFrom is the leftmost match of s starting at or after byte pos — at
// exactly pos for a sticky regex — as submatch offsets, or nil.
//
// RE2 can only search a string from its start, and searching the tail
// s[pos:] instead would get `^`, `$` and `\b` wrong: the cut would look
// like the start of the text. So the matches of a scan of the whole
// string are found once and kept, and the answer is read out of them.
// That is exact wherever pos is not inside one of those matches — which
// covers the loop every page writes, exec until null, where pos is always
// the end of the match before. Only a lastIndex set by hand into the
// middle of a match searches the tail.
func (r *jsRegExp) findFrom(s string, pos int) []int {
	var loc []int
	if pos == 0 {
		loc = r.re.FindStringSubmatchIndex(s)
	} else {
		if !r.scanned || r.scanIn != s {
			r.scanned, r.scanIn = true, s
			r.scan = r.re.FindAllStringSubmatchIndex(s, -1)
		}
		k := sort.Search(len(r.scan), func(i int) bool { return r.scan[i][0] >= pos })
		switch {
		case k > 0 && r.scan[k-1][1] > pos:
			if tail := r.re.FindStringSubmatchIndex(s[pos:]); tail != nil {
				loc = make([]int, len(tail))
				for i, off := range tail {
					loc[i] = off
					if off >= 0 {
						loc[i] = off + pos
					}
				}
			}
		case k < len(r.scan):
			loc = r.scan[k]
		}
	}
	if loc != nil && r.sticky() && loc[0] != pos {
		return nil
	}
	return loc
}

// jsMatch is what exec and match return: the array [whole, group1, …]
// with the match's index, the input, and the named groups. It is used as
// an array everywhere one is (arrayOf).
type jsMatch struct {
	items  []any
	index  int // UTF-16 units
	input  string
	groups any // name → text, or undefined when the pattern names no group
}

// matchOf builds the result for submatch offsets loc (nil for none).
func (r *jsRegExp) matchOf(s string, loc []int) *jsMatch {
	if loc == nil {
		return nil
	}
	m := &jsMatch{items: make([]any, 0, len(loc)/2), index: unitLen(s[:loc[0]]), input: s}
	for i := 0; i < len(loc); i += 2 {
		if loc[i] < 0 {
			m.items = append(m.items, nil) // a group that took no part: undefined
			continue
		}
		m.items = append(m.items, s[loc[i]:loc[i+1]])
	}
	var groups map[string]any
	for i, name := range r.re.SubexpNames() {
		if name == "" {
			continue
		}
		if groups == nil {
			groups = map[string]any{}
		}
		groups[name] = m.items[i]
	}
	if groups != nil {
		m.groups = groups
	}
	return m
}

func (m *jsMatch) member(name string) (any, bool) {
	switch name {
	case "length":
		return float64(len(m.items)), true
	case "index":
		return float64(m.index), true
	case "input":
		return m.input, true
	case "groups":
		return m.groups, true
	}
	return nil, false
}

// arrayOf is v as an array, when it is one: a plain array, or a match
// result.
func arrayOf(v any) ([]any, bool) {
	switch t := v.(type) {
	case []any:
		return t, true
	case *jsMatch:
		return t.items, true
	}
	return nil, false
}

// regexStringMethod handles the String methods that take a RegExp:
// replace (the first match, every match with g), replaceAll, match (the
// first match's array, or every match with g), matchAll, search and
// split. A replacement's $1..$9 and $& name groups, as in JS; a function
// replacement gets the match, its groups, where it matched and the whole
// string.
func regexStringMethod(recv, name string, r *jsRegExp, args []any) (any, bool, error) {
	switch name {
	case "replace", "replaceAll":
		var repl any = ""
		if len(args) > 1 {
			repl = args[1]
		}
		all := name == "replaceAll" || r.global()
		if all {
			r.lastIndex = 0 // a global replace starts from, and ends at, 0
		}
		var locs [][]int
		if all {
			locs = r.re.FindAllStringSubmatchIndex(recv, -1)
		} else if loc := r.re.FindStringSubmatchIndex(recv); loc != nil {
			locs = [][]int{loc}
		}
		if len(locs) == 0 {
			return recv, true, nil
		}
		var out strings.Builder
		last := 0
		for _, loc := range locs {
			out.WriteString(recv[last:loc[0]])
			last = loc[1]
			groups := make([]string, len(loc)/2)
			for i := range groups {
				if loc[2*i] >= 0 {
					groups[i] = recv[loc[2*i]:loc[2*i+1]]
				}
			}
			if !isCallable(repl) {
				out.WriteString(expandJSRepl(str(repl), groups))
				continue
			}
			fargs := make([]any, 0, len(groups)+2)
			for i, g := range groups {
				if loc[2*i] < 0 {
					fargs = append(fargs, nil)
				} else {
					fargs = append(fargs, g)
				}
			}
			fargs = append(fargs, float64(unitLen(recv[:loc[0]])), recv)
			v, err := callAny(repl, fargs)
			if err != nil {
				return nil, true, err
			}
			out.WriteString(str(v))
		}
		out.WriteString(recv[last:])
		return out.String(), true, nil
	case "match":
		if !r.global() {
			if m := r.exec(recv); m != nil {
				return m, true, nil
			}
			return Null, true, nil
		}
		r.lastIndex = 0
		ms := r.re.FindAllString(recv, -1)
		if ms == nil {
			return Null, true, nil
		}
		out := make([]any, len(ms))
		for i, m := range ms {
			out[i] = m
		}
		return out, true, nil
	case "matchAll":
		// Every match with its details. JS gives an iterator; an array
		// does for the ways one is used (for-of, spread).
		if !r.global() {
			return nil, true, fmt.Errorf("matchAll: the regex needs the g flag")
		}
		locs := r.re.FindAllStringSubmatchIndex(recv, -1)
		out := make([]any, len(locs))
		for i, loc := range locs {
			out[i] = r.matchOf(recv, loc)
		}
		return out, true, nil
	case "search":
		if loc := r.re.FindStringIndex(recv); loc != nil {
			return float64(unitLen(recv[:loc[0]])), true, nil
		}
		return float64(-1), true, nil
	case "split":
		parts := r.re.Split(recv, -1)
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = p
		}
		return out, true, nil
	}
	return nil, false, nil
}

// isCallable reports a value a replace callback can be.
func isCallable(v any) bool {
	switch v.(type) {
	case *userFunc, jsFunc, func([]any) (any, error):
		return true
	}
	return false
}

// expandJSRepl expands a JS replacement string: $& the match, $1..$9 the
// groups, $$ a dollar.
func expandJSRepl(repl string, groups []string) string {
	if !strings.Contains(repl, "$") {
		return repl
	}
	var b strings.Builder
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		if c != '$' || i+1 >= len(repl) {
			b.WriteByte(c)
			continue
		}
		n := repl[i+1]
		switch {
		case n == '$':
			b.WriteByte('$')
			i++
		case n == '&':
			b.WriteString(groups[0])
			i++
		case n >= '1' && n <= '9' && int(n-'0') < len(groups):
			b.WriteString(groups[n-'0'])
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
