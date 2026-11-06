package gml

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/sushvibez/gfx/core"
)

// A delegate row's scope, made visible to the read-set machinery.
//
// QML binds a model row into the delegate's scope as plain identifiers —
// `index`, `model`, and every role by name. Plain values are exactly what
// a read-set cannot see: nothing stamps when they change, so a subtree
// that read one can never be proven fresh. The engine's answer used to be
// to give up on the whole interior of a view — `build` skipped the
// subtree cache entirely while `rowDepth > 0` — which meant a ListView,
// GridView, Repeater or PathView rebuilt every element of every row on
// every frame, however little had changed. On the corpus that was the
// single biggest remaining cost: indicators rebuilt 228 subtrees a frame,
// chatui 184, navdisplay 152.
//
// The fix is the one listModel already uses for its rows (see
// listModel.ver): put a token in front of the data. Each row gets a
// core.Property carrying a hash of its contents and its index, and the
// row's names are bound as Refs that read the token before handing back
// the value. A read now records a dependency; the token stamps exactly
// when the row's data or position changes; and the interior of a view
// caches like anything else.
//
// It is all-or-nothing per row, deliberately. If any value in the row is
// something rowHash cannot follow exactly — a live model, an element
// object, a function, a structure deeper than it walks — the row is bound
// the old way and its subtrees stay untracked. A hash that might miss a
// change would serve a stale row, which is far worse than rebuilding one.

// rowVals is one bound row: the token, the hash it carries, and where the
// values come from. Reads go through get, so every one of them records
// the token.
type rowVals struct {
	tok *core.Property[uint64]
	h   uint64
	row map[string]any
	idx float64
	// over holds values a delegate ASSIGNED to a row name. Created on the
	// first write, which nearly never happens; until then a read costs a
	// map lookup in the row itself and no copy of it.
	over map[string]any
}

func (s *rowVals) get(name string) any {
	s.tok.Get()
	if s.over != nil {
		if v, ok := s.over[name]; ok {
			return v
		}
	}
	switch name {
	case "index":
		return s.idx
	case "model":
		if s.row == nil {
			return nil
		}
		return s.row
	}
	return s.row[name]
}

// set records a write to a row name. It does NOT write through to the
// model — no more than the plain binding it replaces did, where the
// assignment landed in the scope and was gone by the next build — but it
// does stamp the token, so the row rebuilds instead of being served the
// element it had before the write. The next bind re-seeds the value from
// the model and stamps again; a write therefore costs one extra frame and
// then settles.
func (s *rowVals) set(name string, v any) {
	if s.over == nil {
		s.over = map[string]any{}
	}
	s.over[name] = v
	s.h = s.h*1099511628211 ^ hashStr(name) ^ tokenHash(v, 0)
	s.tok.Set(s.h)
}

// rowValRef is one name of a bound row, as a Ref — which is what keeps
// the read tracked (evalIdent hands back a Ref's value without marking
// the read-set untracked).
type rowValRef struct {
	s    *rowVals
	name string
}

func (r rowValRef) get() any           { return r.s.get(r.name) }
func (r rowValRef) set(v any) error    { r.s.set(r.name, v); return nil }
func (r rowValRef) onChange(fn func()) { r.s.tok.Changed().Connect(func(uint64) { fn() }) }
func (r rowValRef) animate(any, time.Duration) error {
	return fmt.Errorf("a model role is not animatable")
}

// rowToken returns the token for row i of the view keyed viewKey, stamped
// with a hash of the row's contents and its index — a no-op when
// unchanged, so an untouched row stays fresh frame after frame.
//
// ok is false when the row holds something rowHash cannot follow; the
// caller then binds the row the old way.
func (doc *docState) rowToken(viewKey string, row map[string]any, i int) (*rowVals, bool) {
	h, ok := rowHash(row, i)
	if !ok {
		return nil, false
	}
	key := doc.intern(viewKey + "#" + strconv.Itoa(i) + "/row")
	t := doc.rowToks.get(key)
	if t == nil {
		t = &core.Property[uint64]{}
		doc.rowToks.set(key, t)
	}
	t.Set(h)
	return &rowVals{tok: t, h: h, row: row, idx: float64(i)}, true
}

// rowHash folds a row and its index into one number, or reports that it
// cannot. Keys are walked in order so the hash does not depend on map
// iteration.
//
// What it refuses is the point of it: anything whose CONTENTS it would
// not follow — a Ref or an element object (live state behind a pointer),
// a function, a model, a type it does not know — and anything nested
// deeper than it walks. Refusing costs a rebuild; guessing costs a wrong
// frame.
func rowHash(row map[string]any, i int) (uint64, bool) {
	h := uint64(1469598103934665603)
	mix := func(v uint64) { h ^= v; h *= 1099511628211 }
	mix(uint64(i) + 0x9e3779b9)
	if row == nil {
		return h, true
	}
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, ok := rowValueHash(row[k], 0)
		if !ok {
			return 0, false
		}
		mix(hashStr(k))
		mix(v)
	}
	return h, true
}

// rowValueHash hashes one row value exactly, or reports that it cannot.
const rowHashDepth = 4

func rowValueHash(v any, depth int) (uint64, bool) {
	if depth > rowHashDepth {
		return 0, false
	}
	const prime = 1099511628211
	switch t := v.(type) {
	case nil:
		return 0x9e37, true
	case bool:
		if t {
			return 0x51ed, true
		}
		return 0x2f1b, true
	case string:
		return hashStr(t), true
	case colorVal:
		return hashStr(string(t)), true
	case float64:
		return hashStr(str(t)), true
	case float32:
		return hashStr(str(float64(t))), true
	case int:
		return uint64(t) * prime, true
	case int64:
		return uint64(t) * prime, true
	case map[string]any:
		h := uint64(14695981039346656037)
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			x, ok := rowValueHash(t[k], depth+1)
			if !ok {
				return 0, false
			}
			h ^= hashStr(k)
			h *= prime
			h ^= x
			h *= prime
		}
		return h, true
	case []any:
		h := uint64(14695981039346656037) ^ uint64(len(t))
		for _, x := range t {
			y, ok := rowValueHash(x, depth+1)
			if !ok {
				return 0, false
			}
			h ^= y
			h *= prime
		}
		return h, true
	}
	// A Ref, an element object, a function, a live model, or a type this
	// does not know: not followable, so the row is not trackable.
	return 0, false
}
