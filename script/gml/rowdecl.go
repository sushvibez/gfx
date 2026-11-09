package gml

import (
	"os"

	"github.com/sushvibez/gfx/core"
)

// A delegate row declares its instance scope (declareRowScope: an
// elemObject per id, a ref per property, its live defaults re-evaluated)
// every time it builds — and a row is built every frame its view is, even
// when the row's own subtree then comes from the cache. On a settled list
// that walk was a quarter of the frame.
//
// So the declaration is cached like a subtree: its reads are tracked into
// a per-row read-set, and the names it bound are recorded. A later build of
// the same row (same delegate node, same env) whose declaration read
// nothing that changed binds the recorded names again and skips the walk:
// every elemObject and property ref it would touch persists in docState,
// and a live default that would re-evaluate to something new is exactly a
// read that changed. The read-set is adopted by the enclosing build, as a
// cache hit's is.

// rowKV is one name a row's declaration bound.
type rowKV struct {
	name string
	v    any
}

// rowDecl is a row's cached declaration.
type rowDecl struct {
	delegate *node
	env      *Env
	rs       *core.ReadSet
	binds    []rowKV
}

// noRowDeclCache (GFX_NO_ROW_DECL_CACHE=1) declares every row in full, every
// build: the ground truth a suspected stale row scope is compared against.
var noRowDeclCache = os.Getenv("GFX_NO_ROW_DECL_CACHE") != ""

// declareRowScopeCached is declareRowScope, served from the row's cached
// declaration when nothing it read has changed. Untracked rows (whose
// values cannot be hashed into a token) always declare.
func (doc *docState) declareRowScopeCached(delegate *node, env *Env, row map[string]any, viewKey string, i int, tracked bool) func() {
	key := doc.rowPrefix
	if tracked && !noRowDeclCache {
		if d := doc.rowDecls[key]; d != nil && d.delegate == delegate && d.env == env && !d.rs.Untracked() && !d.rs.Stale() {
			doc.rowDeclHits++
			core.ReplayReads(d.rs)
			return doc.replayRowDecl(d, env)
		}
	}
	var binds []rowKV
	prev := doc.declRecord
	if tracked {
		doc.declRecord = &binds
	} else {
		doc.declRecord = nil
	}
	var restore func()
	rs := core.TrackReads(func() { restore = declareRowScope(delegate, env, doc, row, viewKey, i) })
	doc.declRecord = prev
	if tracked && !noRowDeclCache {
		if doc.rowDecls == nil {
			doc.rowDecls = map[string]*rowDecl{}
		}
		doc.rowDecls[key] = &rowDecl{delegate: delegate, env: env, rs: rs, binds: binds}
	}
	return restore
}

// replayRowDecl binds a cached declaration's names as declareRowScope
// bound them: into env (undone by the returned func), the row's handler
// scope, and the patch binds.
func (doc *docState) replayRowDecl(d *rowDecl, env *Env) func() {
	var mine map[string]any
	if n := len(doc.rowScope); n > 0 {
		if doc.rowScope[n-1].ids == nil {
			doc.rowScope[n-1].ids = make(map[string]any, len(d.binds))
		}
		mine = doc.rowScope[n-1].ids
	}
	var bound map[string]any
	if !noPatch {
		bound = make(map[string]any, len(d.binds))
	}
	popBinds := doc.pushBinds(bound, true)
	type saved struct {
		name string
		v    any
		ok   bool
	}
	undo := make([]saved, 0, len(d.binds))
	for _, kv := range d.binds {
		prev, ok := env.vars[kv.name]
		undo = append(undo, saved{kv.name, prev, ok})
		env.vars[kv.name] = kv.v
		if bound != nil {
			bound[kv.name] = kv.v
		}
		if mine != nil {
			mine[kv.name] = kv.v
		}
	}
	return func() {
		popBinds()
		for i := len(undo) - 1; i >= 0; i-- {
			s := undo[i]
			if s.ok {
				env.vars[s.name] = s.v
			} else {
				delete(env.vars, s.name)
			}
		}
	}
}
