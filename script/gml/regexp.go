package gml

import (
	"fmt"
	"regexp"
	"strings"
)

// jsRegExp is a JS RegExp value: a regex literal (/ab+/i) or RegExp(src,
// flags). Go's RE2 syntax covers what markup uses (classes, quantifiers,
// groups, alternation); lookaround and backreferences fail to compile,
// with the error naming the pattern.
type jsRegExp struct {
	re     *regexp.Regexp
	source string
	flags  string
}

// newRegExp compiles a JS pattern with its flags: i, m and s map onto RE2's
// own; g (global) changes what replace, match and exec do; the rest are
// accepted.
func newRegExp(source, flags string) (*jsRegExp, error) {
	prefix := ""
	for _, f := range "ims" {
		if strings.ContainsRune(flags, f) {
			prefix += string(f)
		}
	}
	pat := source
	if prefix != "" {
		pat = "(?" + prefix + ")" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("regex /%s/: %v", source, err)
	}
	return &jsRegExp{re: re, source: source, flags: flags}, nil
}

// String is the pattern source, which is what a validator's
// regularExpression is read as.
func (r *jsRegExp) String() string { return r.source }

func (r *jsRegExp) global() bool { return strings.ContainsRune(r.flags, 'g') }

// member answers the RegExp properties.
func (r *jsRegExp) member(name string) (any, bool) {
	switch name {
	case "source":
		return r.source, true
	case "flags":
		return r.flags, true
	case "global":
		return r.global(), true
	case "ignoreCase":
		return strings.ContainsRune(r.flags, 'i'), true
	case "multiline":
		return strings.ContainsRune(r.flags, 'm'), true
	}
	return nil, false
}

// call answers r.test(s) and r.exec(s): exec gives the match and its
// groups as an array (with index), or null.
func (r *jsRegExp) call(name string, args []any) (any, bool) {
	s := ""
	if len(args) > 0 {
		s = str(args[0])
	}
	switch name {
	case "test":
		return r.re.MatchString(s), true
	case "exec":
		return matchArray(r.re, s), true
	case "toString":
		return "/" + r.source + "/" + r.flags, true
	}
	return nil, false
}

// matchArray is a JS match result: [whole, group1, …] or nil.
func matchArray(re *regexp.Regexp, s string) any {
	m := re.FindStringSubmatchIndex(s)
	if m == nil {
		return nil
	}
	out := make([]any, 0, len(m)/2)
	for i := 0; i < len(m); i += 2 {
		if m[i] < 0 {
			out = append(out, nil)
			continue
		}
		out = append(out, s[m[i]:m[i+1]])
	}
	return out
}

// regexStringMethod handles the String methods that take a RegExp:
// replace (the first match, every match with g), replaceAll, match (the
// first match's array, or every match with g), search and split. A
// replacement's $1..$9 and $& name groups, as in JS; a function
// replacement gets the match and its groups.
func regexStringMethod(recv, name string, r *jsRegExp, args []any) (any, bool, error) {
	switch name {
	case "replace", "replaceAll":
		var repl any = ""
		if len(args) > 1 {
			repl = args[1]
		}
		all := name == "replaceAll" || r.global()
		done := false
		var ferr error
		out := r.re.ReplaceAllStringFunc(recv, func(m string) string {
			if done {
				return m
			}
			if !all {
				done = true
			}
			groups := r.re.FindStringSubmatch(m)
			if isCallable(repl) {
				fargs := make([]any, len(groups))
				for i, g := range groups {
					fargs[i] = g
				}
				v, err := callAny(repl, fargs)
				if err != nil && ferr == nil {
					ferr = err
				}
				return str(v)
			}
			return expandJSRepl(str(repl), groups)
		})
		return out, true, ferr
	case "match":
		if r.global() {
			ms := r.re.FindAllString(recv, -1)
			if ms == nil {
				return nil, true, nil
			}
			out := make([]any, len(ms))
			for i, m := range ms {
				out[i] = m
			}
			return out, true, nil
		}
		return matchArray(r.re, recv), true, nil
	case "search":
		if loc := r.re.FindStringIndex(recv); loc != nil {
			return float64(len([]rune(recv[:loc[0]]))), true, nil
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
