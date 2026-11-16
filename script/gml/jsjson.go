package gml

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// jsonNS is the JavaScript JSON global over this engine's values (float64,
// string, bool, []any, map[string]any, Null, and nil for undefined).
//
// stringify(value, replacer, indent) follows JS: undefined and functions
// are left out of an object and written as null in an array, a value's
// toJSON is asked first, a replacer is a function of (key, value) or a
// list of the keys to keep, a structure that contains itself is an error,
// and indent is a count of spaces (at most 10) or a string.
//
// One thing is not JS's: an object here is a Go map, which has no order,
// so keys are written sorted, where JS writes them in the order they were
// added.
func jsonNS() map[string]any {
	return map[string]any{
		"stringify": jsFunc(func(a []any) (any, error) {
			if len(a) == 0 {
				return nil, nil
			}
			w := jsonWriter{}
			if len(a) > 1 {
				switch r := a[1].(type) {
				case []any:
					// A list of keys: only those members of any object.
					w.keep = map[string]bool{}
					for _, k := range r {
						switch k.(type) {
						case string, float64, int, float32:
							w.keep[str(k)] = true
						}
					}
				default:
					if isCallable(r) {
						w.replacer = r
					}
				}
			}
			if len(a) > 2 {
				switch g := a[2].(type) {
				case float64, int, float32:
					f, _ := toFloat64(g)
					if f == f && f >= 1 {
						w.gap = strings.Repeat(" ", int(math.Min(f, 10)))
					}
				case string:
					w.gap = g
					if rs := []rune(g); len(rs) > 10 {
						w.gap = string(rs[:10])
					}
				}
			}
			v, ok, err := w.resolve("", a[0])
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, nil // undefined, a function: no text at all
			}
			if err := w.write(v, ""); err != nil {
				return nil, err
			}
			return w.b.String(), nil
		}),
		"parse": jsFunc(func(a []any) (any, error) {
			if len(a) == 0 {
				return nil, fmt.Errorf("JSON.parse needs a string")
			}
			var v any
			// The decoder's own message is what a page's catch shows.
			if err := json.Unmarshal([]byte(str(a[0])), &v); err != nil {
				return nil, err
			}
			v = jsonNulls(v)
			if len(a) > 1 && isCallable(a[1]) {
				return jsonRevive(a[1], "", v)
			}
			return v, nil
		}),
	}
}

// jsonNulls is a decoded JSON value with its nulls as Null: the decoder
// writes nil, which is undefined here.
func jsonNulls(v any) any {
	switch t := v.(type) {
	case nil:
		return Null
	case []any:
		for i, e := range t {
			if e == nil {
				t[i] = Null
			} else {
				jsonNulls(e)
			}
		}
	case map[string]any:
		for k, e := range t {
			if e == nil {
				t[k] = Null
			} else {
				jsonNulls(e)
			}
		}
	}
	return v
}

// jsonRevive is JSON.parse's reviver pass: members first, then the value
// that holds them; a member the reviver answers undefined for is removed.
func jsonRevive(fn any, key string, v any) (any, error) {
	switch t := v.(type) {
	case []any:
		for i, e := range t {
			r, err := jsonRevive(fn, strconv.Itoa(i), e)
			if err != nil {
				return nil, err
			}
			t[i] = r
		}
	case map[string]any:
		for _, k := range sortedKeys(t) {
			r, err := jsonRevive(fn, k, t[k])
			if err != nil {
				return nil, err
			}
			if r == nil {
				delete(t, k)
			} else {
				t[k] = r
			}
		}
	}
	return callAny(fn, []any{key, v})
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// jsonWriter is one JSON.stringify call.
type jsonWriter struct {
	b        strings.Builder
	replacer any             // a function of (key, value), or nil
	keep     map[string]bool // a replacer list: the keys to write, or nil
	gap      string          // one level of indentation ("" for compact)
	// open holds the maps and slices being written, outermost first, so a
	// value that contains itself is found instead of recursed into for ever.
	open []uintptr
}

// resolve is the value that will be written for one property — the root,
// a member or an item — after its toJSON and the replacer have had their
// say, and whether there is anything to write at all (undefined and
// functions have no JSON form).
func (w *jsonWriter) resolve(key string, v any) (any, bool, error) {
	switch t := v.(type) {
	case Ref:
		v = t.get()
	case *varBox:
		v = t.v
	}
	if m, ok := v.(map[string]any); ok {
		if fn, ok := m["toJSON"]; ok && isCallable(fn) {
			r, err := callAny(fn, []any{key})
			if err != nil {
				return nil, false, err
			}
			v = r
		}
	}
	if w.replacer != nil {
		r, err := callAny(w.replacer, []any{key, v})
		if err != nil {
			return nil, false, err
		}
		v = r
	}
	return v, v != nil && !isCallable(v), nil
}

// write writes a resolved value. indent is the indentation of the line
// the value starts on.
func (w *jsonWriter) write(v any, indent string) error {
	switch t := v.(type) {
	case nullValue:
		w.b.WriteString("null")
	case bool:
		w.b.WriteString(strconv.FormatBool(t))
	case string:
		jsonQuote(&w.b, t)
	case colorVal:
		jsonQuote(&w.b, string(t))
	case float64:
		w.number(t)
	case float32:
		w.number(float64(t))
	case int:
		w.number(float64(t))
	case int64:
		w.number(float64(t))
	case []any:
		return w.array(t, indent)
	case map[string]any:
		return w.object(t, indent)
	case *jsMatch:
		return w.array(t.items, indent)
	case *jsRegExp:
		w.b.WriteString("{}")
	default:
		// A host's own Go value (a struct, a []string): as encoding/json
		// writes it.
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("JSON.stringify: %v", err)
		}
		w.b.Write(b)
	}
	return nil
}

// number writes a number the way JS prints one; what JSON cannot say
// (NaN, an infinity) is null.
func (w *jsonWriter) number(f float64) {
	if f != f || math.IsInf(f, 0) {
		w.b.WriteString("null")
		return
	}
	if f == 0 {
		w.b.WriteByte('0') // -0 is 0
		return
	}
	format := byte('f')
	if abs := math.Abs(f); abs < 1e-6 || abs >= 1e21 {
		format = 'e'
	}
	var buf [32]byte
	b := strconv.AppendFloat(buf[:0], f, format, -1, 64)
	if format == 'e' {
		// e-07 is written e-7.
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	w.b.Write(b)
}

// enter records a container being written; it is an error if the
// container is already open.
func (w *jsonWriter) enter(v any) error {
	p := reflect.ValueOf(v).Pointer()
	for _, o := range w.open {
		if o == p {
			return fmt.Errorf("JSON.stringify: the value contains itself (a cyclic structure)")
		}
	}
	w.open = append(w.open, p)
	return nil
}

func (w *jsonWriter) array(a []any, indent string) error {
	if len(a) == 0 {
		w.b.WriteString("[]")
		return nil
	}
	if err := w.enter(a); err != nil {
		return err
	}
	inner := indent + w.gap
	w.b.WriteByte('[')
	for i, e := range a {
		if i > 0 {
			w.b.WriteByte(',')
		}
		w.newline(inner)
		v, ok, err := w.resolve(strconv.Itoa(i), e)
		if err != nil {
			return err
		}
		if !ok {
			w.b.WriteString("null") // undefined keeps its place
		} else if err := w.write(v, inner); err != nil {
			return err
		}
	}
	w.newline(indent)
	w.b.WriteByte(']')
	w.open = w.open[:len(w.open)-1]
	return nil
}

func (w *jsonWriter) object(m map[string]any, indent string) error {
	if len(m) == 0 {
		w.b.WriteString("{}")
		return nil
	}
	if err := w.enter(m); err != nil {
		return err
	}
	inner := indent + w.gap
	w.b.WriteByte('{')
	wrote := false
	for _, k := range sortedKeys(m) {
		if w.keep != nil && !w.keep[k] {
			continue
		}
		v, ok, err := w.resolve(k, m[k])
		if err != nil {
			return err
		}
		if !ok {
			continue // undefined, a function: the member is left out
		}
		if wrote {
			w.b.WriteByte(',')
		}
		w.newline(inner)
		jsonQuote(&w.b, k)
		w.b.WriteByte(':')
		if w.gap != "" {
			w.b.WriteByte(' ')
		}
		if err := w.write(v, inner); err != nil {
			return err
		}
		wrote = true
	}
	if wrote {
		w.newline(indent)
	}
	w.b.WriteByte('}')
	w.open = w.open[:len(w.open)-1]
	return nil
}

// newline starts a line at the given indentation when indenting at all.
func (w *jsonWriter) newline(indent string) {
	if w.gap != "" {
		w.b.WriteByte('\n')
		w.b.WriteString(indent)
	}
}

// jsonQuote writes s as a JSON string the way JS does: the two-character
// escapes, \u00XX for the other control characters, everything else as it
// is (encoding/json also escapes <, > and &, which JS does not).
func jsonQuote(b *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				b.WriteString(s[start:i])
				b.WriteString(`�`)
				i++
				start = i
				continue
			}
			i += size
			continue
		}
		if c >= 0x20 && c != '"' && c != '\\' {
			i++
			continue
		}
		b.WriteString(s[start:i])
		switch c {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			b.WriteString(`\u00`)
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		}
		i++
		start = i
	}
	b.WriteString(s[start:])
	b.WriteByte('"')
}
