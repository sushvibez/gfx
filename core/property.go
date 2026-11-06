package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
)

// Property is a reactive value: the Go equivalent of a QML property.
//
//	var width, height Property[float32]   // zero value is ready to use
//	height.Set(100)
//	width.Bind(func() float32 { return height.Get() * 2 })
//	width.Get()                           // 200, and re-derives when height changes
//
// Dependencies are tracked automatically: every Property read during a
// binding's evaluation becomes a dependency, re-captured on each evaluation,
// so conditional bindings track exactly what they last touched.
//
// Invalidation is eager but evaluation is lazy: Set marks dependent bindings
// dirty and schedules them; they re-evaluate either on the next Get (pull,
// keeps reads consistent) or in the next FlushBindings (push, once per
// frame, in dependency order). A diamond of bindings therefore evaluates
// each node once, with no glitches.
//
// Properties belong to the event-loop goroutine; see Signal for the
// threading model.
type Property[T comparable] struct {
	value   T
	binding *binding
	subs    depSet     // bindings whose closures read this property
	changed *Signal[T] // lazily allocated
}

// Get returns the current value, re-evaluating a dirty binding first. Inside
// a binding closure, Get also registers this property as a dependency; inside
// a TrackReads scope (and outside any binding) it records into the read-set.
func (p *Property[T]) Get() T {
	if p.binding != nil && p.binding.dirty {
		p.binding.eval()
	}
	if current != nil && current != p.binding {
		current.addSource(&p.subs)
	} else if current == nil && tracker != nil {
		tracker.record(&p.subs)
	}
	return p.value
}

// Set assigns an imperative value. Like QML, an imperative Set on a bound
// property removes the binding.
func (p *Property[T]) Set(v T) {
	p.unbind()
	p.store(v)
}

// Bind makes the property derive its value from fn, re-evaluating whenever
// any property fn reads changes. The binding is evaluated once immediately.
func (p *Property[T]) Bind(fn func() T) {
	p.unbind()
	b := &binding{ownerSubs: &p.subs}
	b.run = func() { p.store(fn()) }
	p.binding = b
	b.eval()
}

// Changed returns the signal emitted with the new value whenever the
// property's value actually changes (by Set or by binding re-evaluation).
func (p *Property[T]) Changed() *Signal[T] {
	if p.changed == nil {
		p.changed = &Signal[T]{}
	}
	return p.changed
}

func (p *Property[T]) unbind() {
	if p.binding != nil {
		p.binding.drop()
		p.binding = nil
	}
}

// store writes the value without touching the binding, notifying only on
// real change.
func (p *Property[T]) store(v T) {
	if p.value == v {
		return
	}
	p.value = v
	revision++
	p.subs.stamp = revision
	p.subs.invalidate()
	if p.changed != nil {
		p.changed.Emit(v)
	}
}

// revision counts every property value change process-wide. A frame loop can
// read Revision() to cheaply tell whether any reactive state changed since the
// last frame and skip recomputing an otherwise-identical view. It is advisory
// (a conservative over-count is fine) and lives on the event-loop goroutine.
var revision uint64

// Revision returns a monotonically increasing counter bumped on every real
// property change. Equal values across two frames mean no property changed.
func Revision() uint64 { return revision }

// BumpRevision marks reactive state as changed for observers of Revision —
// for mutations that live outside Property values (e.g. a script-side model
// row edit) but must still invalidate revision-keyed caches.
func BumpRevision() { revision++ }

// current is the binding being evaluated right now. Package-level state is
// correct here because all properties live on the event-loop goroutine.
var current *binding

// dirtyQueue holds invalidated bindings awaiting FlushBindings. A binding
// pulled earlier by a Get simply flushes as a no-op.
var dirtyQueue []*binding

// binding connects a closure to the property it computes. It is untyped so
// depSet can hold bindings of heterogeneous properties.
type binding struct {
	run        func() // recompute and store the owner property's value
	ownerSubs  *depSet
	sources    []*depSet // depSets we are registered with, re-captured per eval
	dirty      bool
	evaluating bool
	dropped    bool
	flushGen   uint64 // loop detection: evals within the current flush
	flushEvals int
}

// maxFlushEvals bounds how often one binding may re-evaluate within a single
// FlushBindings before it is declared a loop. Legitimate dependency chains
// evaluate each binding once; oscillation means mutually-dependent bindings.
const maxFlushEvals = 64

func (b *binding) eval() {
	if b.evaluating {
		// Direct cycle (a binding reading its own property). Keep the
		// previous value, like QML's "binding loop detected" warning.
		fmt.Fprintln(os.Stderr, "core: binding loop detected; keeping previous value")
		return
	}
	if inFlush {
		if b.flushGen != flushGen {
			b.flushGen, b.flushEvals = flushGen, 0
		}
		b.flushEvals++
		if b.flushEvals > maxFlushEvals {
			if b.flushEvals == maxFlushEvals+1 {
				fmt.Fprintln(os.Stderr, "core: binding loop detected (oscillating bindings); keeping previous value")
			}
			b.dirty = false // stop the ping-pong; next external Set retries
			return
		}
	}
	b.evaluating = true
	b.clearSources()
	prev := current
	current = b
	b.run()
	current = prev
	b.evaluating = false
	b.dirty = false
}

func (b *binding) addSource(d *depSet) {
	d.add(b)
	b.sources = append(b.sources, d)
}

func (b *binding) clearSources() {
	for _, d := range b.sources {
		d.remove(b)
	}
	b.sources = b.sources[:0]
}

// drop permanently detaches the binding (property was re-bound or Set).
func (b *binding) drop() {
	b.clearSources()
	b.dropped = true
}

// invalidate marks the binding dirty and cascades to everything that depends
// on the property it computes, so an entire chain dirties in one pass and
// evaluates once at flush.
func (b *binding) invalidate() {
	if b.dirty || b.dropped {
		return
	}
	b.dirty = true
	dirtyQueue = append(dirtyQueue, b)
	b.ownerSubs.invalidate()
}

// flushGen and inFlush scope the loop detector: only flush-driven evaluation
// can livelock, so Get-pulls between frames never count toward the limit.
var (
	flushGen uint64
	inFlush  bool
)

// FlushBindings evaluates all dirty bindings. The event loop calls this once
// per frame before layout and paint; tests may call it directly. Evaluation
// order is dependency order: a binding whose sources are themselves dirty
// pulls them first through Get. Oscillating binding loops are detected,
// reported to stderr, and broken by keeping the previous value.
func FlushBindings() {
	flushGen++
	inFlush = true
	defer func() { inFlush = false }()
	for len(dirtyQueue) > 0 {
		q := dirtyQueue
		dirtyQueue = nil
		for _, b := range q {
			if b.dirty && !b.dropped {
				b.eval()
			}
		}
	}
}

// ---- read-set tracking (per-subtree dirty checks) -----------------------

// tracker is the active TrackReads recorder; nil when tracking is off.
// Package-level like `current` — all properties live on the loop goroutine.
var tracker *ReadSet

// ReadSet records which properties a computation read, so a cached result
// can later be checked for staleness in O(reads) without re-running it.
// Produced by TrackReads; check with Stale.
type ReadSet struct {
	deps      []*depSet // identity of each property read (its dep set)
	snap      uint64    // revision the computation ran at
	untracked bool      // read something outside the property system
	// seen indexes deps once the set is big enough that a linear dedupe
	// scan costs more than a map.
	seen map[*depSet]struct{}
	// subs are the read-sets of NESTED computations (a subtree's children,
	// built or served from cache under this one): a parent is stale when
	// any of them is. They are held by reference — copying a few hundred
	// deps up every level of the tree was a fifth of every frame — and
	// Stale memoizes per revision, so the whole tree is checked once a
	// frame however many lookups ask.
	subs      []*ReadSet
	memoRev   uint64
	memoStale bool
	memoValid bool
}

// readSetIndexAt is the size past which a read-set indexes its deps.
const readSetIndexAt = 16

// TrackReads runs fn recording every property read (outside binding
// evaluation — a binding's internal reads surface through its own property's
// write stamp instead). Scopes nest: an inner set's reads also land in the
// outer set, so a parent's cache subsumes its children's dependencies.
func TrackReads(fn func()) *ReadSet {
	rs := &ReadSet{}
	prev := tracker
	tracker = rs
	fn()
	tracker = prev
	rs.snap = revision
	// A computation depends on everything its NESTED computations read: a
	// parent subtree embeds its children's results, so a child that goes
	// stale must make the parent stale too. Without this a cached parent
	// kept serving a stale child (an animated opacity stuck at 0.65 while
	// its own entry said 0.3).
	if prev != nil {
		// Linked, not copied: the parent reaches the child's reads through
		// subs, and its OWN deps stay its own (see OwnStale).
		prev.adopt(rs)
	}
	return rs
}

// OwnStale reports whether something the computation itself read has
// changed — its nested computations aside. A parent whose own reads are
// fresh while a child's are stale can keep its result and have just that
// child recomputed (see script/gml's subtree patching).
func (rs *ReadSet) OwnStale() bool {
	if rs.untracked {
		return true
	}
	for _, d := range rs.deps {
		if d.stamp > rs.snap {
			return true
		}
	}
	return false
}

// ReplaceSub swaps one nested read-set for another (a child recomputed in
// place) and forgets the memoized answer, which was computed over the old
// one.
func (rs *ReadSet) ReplaceSub(old, repl *ReadSet) bool {
	for i, s := range rs.subs {
		if s == old {
			rs.subs[i] = repl
			rs.memoValid = false
			if repl.untracked {
				rs.untracked = true
			}
			return true
		}
	}
	return false
}

// Invalidate forgets the memoized staleness (after the set's subs changed).
func (rs *ReadSet) Invalidate() { rs.memoValid = false }

// Summary describes a read-set for tracing: deps, nested sets, untracked.
func (rs *ReadSet) Summary() string {
	return fmt.Sprintf("%d deps, %d subs, untracked=%v", len(rs.deps), len(rs.subs), rs.untracked)
}

// EachSub visits the nested read-sets in the order they were adopted.
func (rs *ReadSet) EachSub(fn func(sub *ReadSet)) {
	for _, s := range rs.subs {
		fn(s)
	}
}

// Untracked reports whether the computation read state the property system
// cannot stamp (see MarkUntrackedRead) — always stale, and worth counting
// apart from a set that is stale because something it read changed.
func (rs *ReadSet) Untracked() bool { return rs.untracked }

// ReplayReads records everything rs read into the active read-set — what a
// cache HIT owes its enclosing computation, which never ran the reads
// itself but depends on them all the same.
func ReplayReads(rs *ReadSet) {
	if rs != nil && tracker != nil {
		tracker.adopt(rs)
	}
}

// adopt makes sub one of rs's nested sets. An untracked child poisons the
// parent at once, so Untracked stays a flag read.
func (rs *ReadSet) adopt(sub *ReadSet) {
	if sub.untracked {
		rs.untracked = true
	}
	rs.subs = append(rs.subs, sub)
}

// MarkUntrackedRead poisons the active read-set: the computation read state
// the property system can't stamp (layout geometry, scroll offsets, model
// rows), so its cached result can never be proven fresh.
func MarkUntrackedRead() {
	if tracker != nil {
		tracker.untracked = true
		if untrackedTrace {
			_, file, line, _ := runtime.Caller(1)
			untrackedSites[fmt.Sprintf("%s:%d", filepath.Base(file), line)]++
		}
	}
}

// untrackedTrace counts the call sites that poison read-sets
// (GFX_UNTRACKED_TRACE=1) — how you find which read keeps a subtree
// cache from ever hitting.
var (
	untrackedTrace = os.Getenv("GFX_UNTRACKED_TRACE") != ""
	untrackedSites = map[string]int{}
)

// UntrackedSites returns and clears the counted call sites (see
// GFX_UNTRACKED_TRACE); nil unless tracing.
func UntrackedSites() map[string]int {
	if !untrackedTrace {
		return nil
	}
	out := untrackedSites
	untrackedSites = map[string]int{}
	return out
}

func (rs *ReadSet) record(d *depSet) {
	// Dedupe: repeat reads of the same property are common (one expression,
	// several references). A scan is cheapest while the set is small; a
	// map takes over once it is not (see seen).
	if rs.seen != nil {
		if _, ok := rs.seen[d]; ok {
			return
		}
		rs.seen[d] = struct{}{}
		rs.deps = append(rs.deps, d)
		return
	}
	if slices.Contains(rs.deps, d) {
		return
	}
	rs.deps = append(rs.deps, d)
	if len(rs.deps) >= readSetIndexAt {
		rs.seen = make(map[*depSet]struct{}, 2*len(rs.deps))
		for _, x := range rs.deps {
			rs.seen[x] = struct{}{}
		}
	}
}

// Stale reports whether any recorded property changed since the set was
// captured (or the computation read untracked state, which is always stale).
func (rs *ReadSet) Stale() bool {
	if rs.untracked {
		return true
	}
	// Memoized per revision: nothing a set depends on can change without
	// a property write, and every write bumps the revision — so between
	// writes the answer stands, and a stale set stays stale for good.
	if rs.memoValid && rs.memoRev == revision {
		return rs.memoStale
	}
	stale := rs.staleNow()
	rs.memoRev, rs.memoStale, rs.memoValid = revision, stale, true
	return stale
}

func (rs *ReadSet) staleNow() bool {
	for _, sub := range rs.subs {
		if sub.Stale() {
			return true
		}
	}
	for _, d := range rs.deps {
		if d.stamp > rs.snap {
			return true
		}
	}
	return false
}

// depSet is the set of bindings depending on one property.
type depSet struct {
	bindings []*binding
	stamp    uint64 // revision of the owning property's last real change
}

func (d *depSet) add(b *binding) {
	if slices.Contains(d.bindings, b) {
		return
	}
	d.bindings = append(d.bindings, b)
}

func (d *depSet) remove(b *binding) {
	for i, x := range d.bindings {
		if x == b {
			last := len(d.bindings) - 1
			d.bindings[i] = d.bindings[last]
			d.bindings[last] = nil
			d.bindings = d.bindings[:last]
			return
		}
	}
}

func (d *depSet) invalidate() {
	for _, b := range d.bindings {
		b.invalidate()
	}
}
