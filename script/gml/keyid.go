package gml

// Integer element keys.
//
// Every piece of per-element state the document keeps is filed under the
// element's KEY: the row/instance prefix plus its id or line
// ("fc:SpecializedButtonsCard:__el110/tpl:PulseButton:__el68/pulseCircle").
// Those strings are long, and a rebuild hashes them again and again —
// registry, objects, refs, tokens, overrides, subtree cache — which was
// about forty percent of an animated frame. A keyID is the same key as a
// small integer: interned once per document, equal exactly when the
// strings are equal, and cached on the node for the prefix it was asked
// under. The string stays reachable (keyStr) for logging, tests and the
// state that is still filed by string.

type keyID uint32

// intern returns the id of a key string, allocating one on first sight.
// Id 0 is never a key.
func (doc *docState) intern(s string) keyID {
	if id, ok := doc.keyIDs[s]; ok {
		return id
	}
	if doc.keyIDs == nil {
		doc.keyIDs = map[string]keyID{}
		doc.keyStrs = []string{""}
	}
	id := keyID(len(doc.keyStrs))
	doc.keyIDs[s] = id
	doc.keyStrs = append(doc.keyStrs, s)
	return id
}

// keyStr is the string a keyID was interned from.
func (doc *docState) keyStr(id keyID) string {
	if int(id) < len(doc.keyStrs) {
		return doc.keyStrs[id]
	}
	return ""
}

// keyID is doc.key(n) as an id, cached on the node for the document and
// prefix it was asked under (a style file's nodes are shared by every
// instance of the control, each under its own prefix).
func (doc *docState) keyID(n *node) keyID {
	if n.kidDoc == doc && n.kidPrefix == doc.rowPrefix && n.kid != 0 {
		return n.kid
	}
	id := doc.intern(doc.key(n))
	n.kidDoc, n.kidPrefix, n.kid = doc, doc.rowPrefix, id
	return id
}

// here is the id of `rowPrefix + id` — the key an id'd element has in the
// instance being built — found through a two-level cache, so the prefix
// and the id are never concatenated on a rebuild.
func (doc *docState) here(id string) keyID { return doc.idIn(doc.rowPrefix, id) }

// idIn is the id of `prefix + id` through the same cache.
func (doc *docState) idIn(prefix, id string) keyID {
	if doc.hereIDs == nil {
		doc.hereIDs = map[string]map[string]keyID{}
	}
	m := doc.hereIDs[prefix]
	if m == nil {
		m = map[string]keyID{}
		doc.hereIDs[prefix] = m
	}
	if k, ok := m[id]; ok {
		return k
	}
	k := doc.intern(prefix + id)
	m[id] = k
	return k
}

// idKey is `rowPrefix + id` as a string, without building it each time.
func (doc *docState) idKey(id string) string { return doc.keyStr(doc.here(id)) }

// col is per-element state as a COLUMN: a slice indexed by keyID instead
// of a map keyed by it. Ids are dense (interned in order of first sight),
// so the slice is compact; a read past the end is the zero value, a write
// grows it. What used to be a map lookup per element per frame — the
// registry, the id objects, the tokens — is an index.
type col[T any] struct {
	v []T
	n int // entries ever set from zero (a size for scopeGeneration)
}

func (c *col[T]) get(k keyID) T {
	if int(k) < len(c.v) {
		return c.v[k]
	}
	var zero T
	return zero
}

func (c *col[T]) set(k keyID, v T) {
	if int(k) >= len(c.v) {
		grown := make([]T, int(k)+1+len(c.v)/2)
		copy(grown, c.v)
		c.v = grown
		c.n++
	}
	c.v[k] = v
}

// del zeroes the slot.
func (c *col[T]) del(k keyID) {
	if int(k) < len(c.v) {
		var zero T
		c.v[k] = zero
	}
}

func (c *col[T]) clear() { clear(c.v) }

// count is the number of distinct ids ever written (grows only).
func (c *col[T]) count() int { return c.n }

// ccol is a col whose zero value is detectable (pointers, interfaces,
// comparable structs), so a lookup can report presence like a map's.
type ccol[T comparable] struct{ col[T] }

func (c *ccol[T]) get2(k keyID) (T, bool) {
	var zero T
	if int(k) < len(c.v) && c.v[k] != zero {
		return c.v[k], true
	}
	return zero, false
}

func (c *ccol[T]) set(k keyID, v T) {
	var zero T
	if int(k) >= len(c.v) || c.v[k] == zero {
		c.n++
	}
	if int(k) >= len(c.v) {
		grown := make([]T, int(k)+1+len(c.v)/2)
		copy(grown, c.v)
		c.v = grown
	}
	c.v[k] = v
}
