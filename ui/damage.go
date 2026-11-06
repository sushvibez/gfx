package ui

import (
	"math"
	"unsafe"
)

// Damage-driven painting.
//
// A frame used to repaint every pixel of the window whatever had changed,
// so one pulsing ring cost the whole page: every element painted, every
// batch uploaded, every pixel filled. Qt repaints the rectangle the ring
// occupies and nothing else. This is that.
//
// After layout, diff walks the tree once. Every element is keyed by its
// PATH from the root (child index by child index), which is stable across
// the per-frame rebuilds a retained-view UI does, and compared against last
// frame's entry at that path: the same element pointer at the same rect is
// unchanged, and so is a fresh pointer whose PAINT SIGNATURE — a hash of
// everything its paint reads from itself — matches. That second test is
// what makes the pass work at all: a gml page rebuilds the ancestor chain
// of anything that animates and a Go view rebuilds everything, so pointer
// identity alone would find the whole window changed every frame.
//
// The damage is the union of the old and new paint bounds of every element
// that changed (or says it is dirty, or was removed). Painting then skips a
// subtree that is clean AND lies wholly outside the damage; everything
// touching it repaints, so the cleared region is fully recovered. The
// shell loads the previous frame under the pass and clamps every scissor
// to the damage rect, so the GPU fills only what moved.
//
// Anything the walk cannot reason about forces a FULL frame: input events
// (hover and press are read off the mouse at paint time), overlays, a
// drag, a focus ring, an expose, a resize, the first frame.

// dmgInfo is what culling needs to know about an element this frame.
type dmgInfo struct {
	sub   Rect
	clean bool
}

// dmgRec is one element's entry in the per-frame damage map.
type dmgRec struct {
	rect Rect    // the element's own paint bounds
	sub  Rect    // those of its whole subtree
	sig  uint64  // its paint signature (0 when the type has none)
	el   Element // the element itself, for pointer identity
}

// damageState is the UI's frame-to-frame damage tracking.
type damageState struct {
	on         bool
	prev, cur  map[uint64]dmgRec
	info       map[Element]dmgInfo // this frame, by element: subtree bounds and cleanliness
	rect       Rect                // this frame's damage (valid when !full && any)
	any        bool
	full       bool
	forceFull  bool // set by input, resize, expose, first frame
	prepared   bool // diff already ran for the tree about to paint
	frames     int
	culled     int // subtrees skipped this frame (stats)
	painted    int // paint calls that ran this frame (stats)
	lastCulled int
	lastPaint  int
}

// EnableDamage turns damage-driven painting on or off. Off, every frame
// paints everything, as before.
func (u *UI) EnableDamage(on bool) {
	if u.dmg == nil {
		u.dmg = &damageState{prev: map[uint64]dmgRec{}, cur: map[uint64]dmgRec{}, info: map[Element]dmgInfo{}}
	}
	u.dmg.on = on
	if !on {
		u.dmg.forceFull = true
	}
}

// ForceFullRedraw makes the next frame paint everything (a resize, an
// expose, anything the diff cannot see).
func (u *UI) ForceFullRedraw() {
	if u.dmg != nil {
		u.dmg.forceFull = true
	}
}

// PrepareDamage runs the frame's diff against the laid-out tree, ahead of
// painting, so the caller can read Damage before recording anything. Frame
// runs it itself when the caller did not.
func (u *UI) PrepareDamage() {
	if u.dmg == nil || !u.dmg.on || u.root == nil {
		return
	}
	u.diffTree()
	u.dmg.prepared = true
}

// Damage reports this frame's damage rect and whether the whole window is
// damaged (in which case the rect is meaningless). Before the first
// PrepareDamage/Frame, or with damage off, it is full.
func (u *UI) Damage() (r Rect, full bool) {
	if u.dmg == nil || !u.dmg.on || !u.dmg.prepared {
		return Rect{}, true
	}
	if u.dmg.full || !u.dmg.any {
		// Nothing changed at all: report an EMPTY rect, not a full one —
		// the shell then loads the previous frame and paints nothing.
		return Rect{}, u.dmg.full
	}
	return u.dmg.rect, false
}

// DamageStats reports how many paint subtrees the last frame skipped and
// how many painted (for GFX_FPS).
func (u *UI) DamageStats() (culled, painted int) {
	if u.dmg == nil {
		return 0, 0
	}
	return u.dmg.lastCulled, u.dmg.lastPaint
}

// diffTree walks the laid-out tree, decides what changed since last frame
// and accumulates the damage.
func (u *UI) diffTree() {
	st := u.dmg
	st.any, st.full = false, false
	st.rect = Rect{}
	clear(st.cur)
	clear(st.info)
	full := st.forceFull || st.frames == 0 || len(u.ctx.overlays) > 0 || u.dragging
	if el := u.focusedEl(); el != nil {
		// The focus ring is painted after the tree, outside any element's
		// bounds accounting.
		if _, isInput := el.(*InputEl); !isInput {
			full = true
		}
	}
	st.forceFull = false
	st.frames++
	u.walkDamage(u.root, 1)
	// Whatever was in the tree last frame and is not now: its pixels are
	// stale wherever it painted.
	for k, rec := range st.prev {
		if _, ok := st.cur[k]; !ok {
			st.addDamage(rec.sub)
		}
	}
	st.prev, st.cur = st.cur, st.prev
	st.full = full
	st.lastCulled, st.lastPaint = st.culled, st.painted
	st.culled, st.painted = 0, 0
}

// walkDamage visits e under path key k and returns its subtree's paint
// bounds and whether the subtree is clean.
func (u *UI) walkDamage(e Element, k uint64) (sub Rect, clean bool) {
	st := u.dmg
	r := paintBoundsOf(e)
	sub = r
	sig, hasSig := uint64(0), false
	if ps, ok := e.(interface{ paintSig() uint64 }); ok {
		sig, hasSig = ps.paintSig(), true
	}
	prev, had := st.prev[k]
	same := had && prev.rect == r && (prev.el == e || (hasSig && prev.sig == sig))
	if d, ok := e.(interface{ dirty(*Context) bool }); ok && d.dirty(&u.ctx) {
		same = false
	}
	clean = same
	// A transformed subtree paints somewhere its layout rects do not say;
	// its bounds are inflated (see DivEl.paintBounds) and nothing inside
	// it is ever culled (see Context.culled), so it is one unit here.
	for i, c := range Children(e) {
		if c == nil {
			continue
		}
		cs, cc := u.walkDamage(c, k*1000003+uint64(i)+1)
		sub = unionRect(sub, cs)
		clean = clean && cc
	}
	if !same {
		if had {
			// Where it WAS: a moved element leaves its old pixels behind.
			st.addDamage(prev.rect)
		}
		st.addDamage(r)
	}
	st.cur[k] = dmgRec{rect: r, sub: sub, sig: sig, el: e}
	st.info[e] = dmgInfo{sub: sub, clean: clean}
	return sub, clean
}

func (st *damageState) addDamage(r Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	if !st.any {
		st.rect, st.any = r, true
		return
	}
	st.rect = unionRect(st.rect, r)
}

// culled reports whether e's whole subtree can be skipped this frame: it
// is clean and lies outside the damage. Called at the top of every paint.
func (c *Context) culled(e Element) bool {
	st := c.dmg
	if st == nil {
		return false
	}
	if st.full || c.dmgLock > 0 {
		st.painted++
		return false
	}
	in, ok := st.info[e]
	if !ok || !in.clean || (st.any && intersects(in.sub, st.rect)) {
		st.painted++
		return false
	}
	st.culled++
	return true
}

func paintBoundsOf(e Element) Rect {
	var r Rect
	if pb, ok := e.(interface{ paintBounds() Rect }); ok {
		r = pb.paintBounds()
	} else {
		r = e.bounds()
	}
	// A pixel of antialiasing on every side.
	return Rect{r.X - 1, r.Y - 1, r.W + 2, r.H + 2}
}

func unionRect(a, b Rect) Rect {
	if a.W <= 0 || a.H <= 0 {
		return b
	}
	if b.W <= 0 || b.H <= 0 {
		return a
	}
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

func intersects(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func inflate(r Rect, by float32) Rect {
	return Rect{r.X - by, r.Y - by, r.W + 2*by, r.H + 2*by}
}

// sigHasher folds paint-relevant values into a signature (FNV-1a).
type sigHasher uint64

func newSig(tag uint64) sigHasher { return sigHasher(14695981039346656037 ^ tag) }

func (h *sigHasher) u64(v uint64) {
	// One multiply-xorshift step per word (a splitmix-style mix): the
	// byte-wise FNV loop it replaced was six percent of a frame on its own,
	// and a signature only has to be well spread, not standard.
	x := uint64(*h) ^ v
	x *= 0x9e3779b97f4a7c15
	x ^= x >> 32
	*h = sigHasher(x)
}

func (h *sigHasher) f32(v float32) { h.u64(uint64(math.Float32bits(v))) }
func (h *sigHasher) f64(v float64) { h.u64(math.Float64bits(v)) }
func (h *sigHasher) b(v bool) {
	if v {
		h.u64(2)
	} else {
		h.u64(1)
	}
}
func (h *sigHasher) i(v int)              { h.u64(uint64(v)) }
func (h *sigHasher) col(c RGBA)           { h.f32(c.R); h.f32(c.G); h.f32(c.B); h.f32(c.A) }
func (h *sigHasher) ptr(p unsafe.Pointer) { h.u64(uint64(uintptr(p))) }
func (h *sigHasher) str(s string) {
	x := uint64(*h)
	for i := 0; i < len(s); i++ {
		x ^= uint64(s[i])
		x *= 1099511628211
	}
	*h = sigHasher(x)
}

// SubtreeSig folds a subtree's paint signatures and rects into one hash,
// and reports whether anything in it is dirty (time-driven). An offscreen
// layer re-renders only when this changes: the global property revision
// it used to watch moves for any property anywhere, so an unrelated write
// re-rendered every frosted panel on the page every frame.
func SubtreeSig(e Element) (sig uint64, dirty bool) {
	h := newSig(0x5b5)
	var walk func(Element)
	walk = func(e Element) {
		if e == nil {
			return
		}
		r := e.bounds()
		h.f32(r.X)
		h.f32(r.Y)
		h.f32(r.W)
		h.f32(r.H)
		if ps, ok := e.(interface{ paintSig() uint64 }); ok {
			h.u64(ps.paintSig())
		} else {
			h.ptr(elemPtr(e))
		}
		if d, ok := e.(interface{ dirty(*Context) bool }); ok && d.dirty(nil) {
			dirty = true
		}
		for _, c := range Children(e) {
			walk(c)
		}
	}
	walk(e)
	return uint64(h), dirty
}

// elemPtr is an element's identity as a pointer, for types without a
// paint signature.
func elemPtr(e Element) unsafe.Pointer {
	type iface struct{ typ, data unsafe.Pointer }
	return (*iface)(unsafe.Pointer(&e)).data
}
