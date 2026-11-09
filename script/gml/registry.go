package gml

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/sushvibez/gfx/ui"
)

// The element registry (doc.elems: element key → the ui element built
// for it) persists across frames. A frame's build used to refill it from
// nothing — a cache hit replayed every registration under it, so a
// settled page re-registered its whole tree every frame — and now it is
// edited only where the tree changed:
//
//   - a fresh build registers its elements (registerElem);
//   - a cache hit leaves its elements where they are (markLive);
//   - a cache entry that is replaced or dropped is retired, and after the
//     build its registrations leave the registry (settleRegistry) unless
//     the key was registered again or the entry holding them was reused;
//     an entry whose registrations went while it stayed cached is marked
//     gone and replays them if it is hit again;
//   - registrations made outside every cached build, and top-level
//     entries (the root, delegate rows built during layout), are redone or
//     reused every frame, and last frame's that were not leave at the next
//     frame (nextRegistryFrame);
//   - invalidateCache drops every entry at once: the next frame clears the
//     registry and every hit replays, which is the old refill for a frame.
//
// A key registered by two elements in one build (gml has a few such
// collisions) holds the one built last; the old refill held the one its
// replay walk visited last, so the two can differ there.
//
// prevElems is "the registry as the frame found it": every write saves
// the value it replaces in the frame's overlay first, and a read takes
// the overlay's value when the key has one.

// regState is the registry's bookkeeping.
type regState struct {
	frame   uint32 // the registry frame (overlay generation)
	build   uint32 // the top-level build within it
	settle  uint32 // eviction passes
	ovEl    []ui.Element
	ovFrame []uint32
	// atBuild/atFrame: the build and frame that last registered each key
	// (a registration of the element it already held included): an
	// element registered again is never evicted as a leftover.
	atBuild, atFrame []uint32
	retired          []*subtreeEntry
	// loose: registrations outside every cached build, this frame and
	// last; looseEnts: entries built or reused with nothing enclosing
	// them, this frame and last.
	loose, looseLast         []elemReg
	looseEnts, looseEntsLast []*subtreeEntry
	// sweepAll: invalidateCache dropped every entry; replayAll: this frame
	// rebuilds the registry from nothing (every hit replays).
	sweepAll, replayAll bool
}

// prevReg reads the registry as it was when the frame began.
type prevReg struct{ doc *docState }

func (p *prevReg) get(k keyID) ui.Element {
	r := &p.doc.reg
	if int(k) < len(r.ovFrame) && r.ovFrame[k] == r.frame {
		return r.ovEl[k]
	}
	return p.doc.elems.get(k)
}

func (p *prevReg) get2(k keyID) (ui.Element, bool) {
	el := p.get(k)
	return el, el != nil
}

// keep saves k's current value in the overlay before its first change
// this frame.
func (doc *docState) keep(k keyID) {
	r := &doc.reg
	if int(k) >= len(r.ovFrame) {
		n := int(k) + 1 + len(r.ovFrame)/2
		el := make([]ui.Element, n)
		copy(el, r.ovEl)
		fr := make([]uint32, n)
		copy(fr, r.ovFrame)
		r.ovEl, r.ovFrame = el, fr
	}
	if r.ovFrame[k] != r.frame {
		r.ovEl[k] = doc.elems.get(k)
		r.ovFrame[k] = r.frame
	}
}

func (doc *docState) regSet(k keyID, el ui.Element) {
	r := &doc.reg
	if int(k) >= len(r.atBuild) {
		n := int(k) + 1 + len(r.atBuild)/2
		b := make([]uint32, n)
		copy(b, r.atBuild)
		f := make([]uint32, n)
		copy(f, r.atFrame)
		r.atBuild, r.atFrame = b, f
	}
	r.atBuild[k], r.atFrame[k] = r.build, r.frame
	doc.traceReg("set", k, el)
	if doc.elems.get(k) == el {
		return
	}
	doc.keep(k)
	doc.elems.set(k, el)
}

func (doc *docState) regDel(k keyID) {
	doc.traceReg("del", k, doc.elems.get(k))
	if doc.elems.get(k) == nil {
		return
	}
	doc.keep(k)
	doc.elems.del(k)
}

// markLive records that e was built or reused by this build (and, with
// nothing enclosing it, that it is a top-level entry this frame).
func (doc *docState) markLive(e *subtreeEntry) {
	if regTraceEnt != "" && doc.keyStr(doc.keyID(e.node)) == regTraceEnt {
		fmt.Fprintf(os.Stderr, "gml: reg live %s %p build %d frame %d caps %d patching %d\n", regTraceEnt, e, doc.reg.build, doc.reg.frame, len(doc.capStack), doc.patching)
	}
	e.liveBuild, e.liveFrame = doc.reg.build, doc.reg.frame
	if len(doc.capStack) == 0 && doc.patching == 0 && e.looseFrame != doc.reg.frame {
		e.looseFrame = doc.reg.frame
		doc.reg.looseEnts = append(doc.reg.looseEnts, e)
	}
}

// retire queues an entry that left the cache for eviction.
func (doc *docState) retire(e *subtreeEntry) {
	if e != nil {
		doc.reg.retired = append(doc.reg.retired, e)
	}
}

// beginBuild starts a top-level build (View): what it builds or reuses
// is live for this build's eviction pass.
func (doc *docState) beginBuild() {
	doc.reg.build++
	if regTraceEnt != "" {
		fmt.Fprintln(os.Stderr, "gml: reg build", doc.reg.build, "frame", doc.reg.frame)
	}
}

// settleRegistry evicts the registrations of the entries retired so far.
// Run after each top-level build, and at the frame's end for what a
// layout pass rebuilt (delegate rows).
func (doc *docState) settleRegistry() {
	r := &doc.reg
	if len(r.retired) == 0 {
		return
	}
	r.settle++
	for _, e := range r.retired {
		if regTrace != "" {
			evictPath = []string{"retired " + doc.keyStr(doc.keyID(e.node))}
		}
		doc.evict(e, true)
	}
	clear(r.retired)
	r.retired = r.retired[:0]
}

// evict removes e's registrations, and those of the subtrees under it
// this frame did not build or reuse, where the registry still holds them
// and no build this frame registered the key again. The retired entry
// itself is out of the tree by definition (replaced or dropped); below it,
// an entry live this frame is in the tree and so is everything under it
// (a hit marks only the entry it hit). Liveness is by frame, not build: a
// frame can build twice (completion), and a subtree the first build hit is
// served by an ancestor's hit in the second without being visited.
func (doc *docState) evict(e *subtreeEntry, root bool) {
	r := &doc.reg
	if e.walked == r.settle || (!root && e.liveFrame == r.frame) {
		return
	}
	e.walked = r.settle
	for _, g := range e.regs {
		if doc.elems.get(g.key) == g.el && !doc.registeredIn(g.key, 0, r.frame) {
			if regTrace != "" && strings.Contains(doc.keyStr(g.key), regTrace) {
				fmt.Fprintln(os.Stderr, "gml: reg evict path:", strings.Join(evictPath, " > "))
			}
			doc.regDel(g.key)
		}
	}
	e.gone = true
	for _, sub := range e.subs {
		if regTrace != "" {
			evictPath = append(evictPath, fmt.Sprintf("%s(live=%d cached=%v)", doc.keyStr(doc.keyID(sub.node)), sub.liveFrame, doc.subtrees.get(doc.keyID(sub.node)) == sub))
		}
		doc.evict(sub, false)
		if regTrace != "" {
			evictPath = evictPath[:len(evictPath)-1]
		}
	}
}

var evictPath []string

// nextRegistryFrame closes the frame that just ended — evictions owed to
// it, loose registrations and top-level entries it did not redo — and
// opens the next one's overlay.
func (doc *docState) nextRegistryFrame() {
	r := &doc.reg
	doc.settleRegistry()
	// Loose registrations from the frame before that the frame just ended
	// did not redo: still the old element, so nothing replaced it.
	for _, g := range r.looseLast {
		if doc.elems.get(g.key) == g.el && !doc.registeredIn(g.key, 0, r.frame) {
			doc.regDel(g.key)
		}
	}
	r.looseLast, r.loose = r.loose, r.looseLast[:0]
	// Top-level entries the frame just ended did not build or reuse.
	r.settle++
	for _, e := range r.looseEntsLast {
		if e.liveFrame != r.frame {
			if regTrace != "" {
				evictPath = []string{fmt.Sprintf("top-level %s %p liveFrame=%d frame=%d", doc.keyStr(doc.keyID(e.node)), e, e.liveFrame, r.frame)}
			}
			doc.evict(e, true)
		}
	}
	clear(r.looseEntsLast)
	r.looseEntsLast, r.looseEnts = r.looseEnts, r.looseEntsLast[:0]
	r.frame++
	r.replayAll = false
	if r.sweepAll {
		// Every cached entry was dropped at once (invalidateCache), and
		// what they had registered is not tracked by anything any more:
		// start this frame's registry from nothing, the old way, keeping
		// last frame's as the overlay; every hit replays.
		r.sweepAll, r.replayAll = false, true
		for k, el := range doc.elems.v {
			if el != nil {
				doc.keep(keyID(k))
			}
		}
		doc.elems.clear()
		clear(r.looseLast)
		r.looseLast = r.looseLast[:0]
		clear(r.retired)
		r.retired = r.retired[:0]
	}
}

// regVerify (GFX_REG_VERIFY=1) checks, after every top-level build, that
// the persistent registry holds every element the old per-frame refill
// would have: the loose registrations plus a replay of every top-level
// entry's tree. It reports the first missing or different key, and with
// GFX_REG_VERIFY=all also the first stale extra.
var regVerify = os.Getenv("GFX_REG_VERIFY")

func (doc *docState) verifyRegistry() {
	if regVerify == "" {
		return
	}
	want := map[keyID]ui.Element{}
	owner := map[keyID]*subtreeEntry{}
	parent := map[*subtreeEntry]*subtreeEntry{}
	seen := map[*subtreeEntry]bool{}
	var walk func(e *subtreeEntry)
	walk = func(e *subtreeEntry) {
		if seen[e] {
			return
		}
		seen[e] = true
		for _, g := range e.regs {
			want[g.key] = g.el
			owner[g.key] = e
		}
		for _, s := range e.subs {
			if _, ok := parent[s]; !ok {
				parent[s] = e
			}
			walk(s)
		}
	}
	for _, g := range doc.reg.loose {
		want[g.key] = g.el
	}
	for _, e := range doc.reg.looseEnts {
		if e.liveBuild == doc.reg.build {
			walk(e)
		}
	}
	for k, el := range want {
		if got := doc.elems.get(k); got != el {
			fmt.Fprintf(os.Stderr, "gml: registry: %s holds %T, the refill would hold %T\n", doc.keyStr(k), got, el)
			return
		}
	}
	if regVerify == "all" {
		for k, el := range doc.elems.v {
			if el != nil {
				if _, ok := want[keyID(k)]; !ok {
					fmt.Fprintf(os.Stderr, "gml: registry: %s is registered but not in the tree\n", doc.keyStr(keyID(k)))
					return
				}
			}
		}
	}
}

// vals is the registry as the frame found it, as a slice by key (tests
// and diagnostics; a fresh slice).
func (p *prevReg) vals() []ui.Element {
	r := &p.doc.reg
	n := max(len(p.doc.elems.v), len(r.ovFrame))
	out := make([]ui.Element, n)
	for k := range out {
		out[k] = p.get(keyID(k))
	}
	return out
}

// registeredIn reports whether k was registered in the given build (or,
// with build 0, the given frame).
func (doc *docState) registeredIn(k keyID, build, frame uint32) bool {
	r := &doc.reg
	if int(k) >= len(r.atBuild) {
		return false
	}
	if build != 0 {
		return r.atBuild[k] == build
	}
	return r.atFrame[k] == frame
}

// regTrace (GFX_REG_TRACE=substring) logs every registry write and delete
// for keys containing it, with the caller.
var regTrace = os.Getenv("GFX_REG_TRACE")

var regTraceEnt = os.Getenv("GFX_REG_TRACE_ENT")

func (doc *docState) traceReg(op string, k keyID, el ui.Element) {
	if regTrace == "" || !strings.Contains(doc.keyStr(k), regTrace) {
		return
	}
	pc := make([]uintptr, 6)
	n := runtime.Callers(3, pc)
	frames := runtime.CallersFrames(pc[:n])
	var who []string
	for {
		f, more := frames.Next()
		who = append(who, f.Function[strings.LastIndex(f.Function, ".")+1:])
		if !more {
			break
		}
	}
	fmt.Fprintf(os.Stderr, "gml: reg %s %s %T@%p frame=%d build=%d via %s\n", op, doc.keyStr(k), el, el, doc.reg.frame, doc.reg.build, strings.Join(who, "<"))
}
