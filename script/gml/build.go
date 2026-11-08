package gml

import (
	"fmt"
	"github.com/sushvibez/gfx/md/mdrender"
	"math"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/core/settings"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// The element vocabulary follows QML / Qt Quick Controls: Rectangle, Item,
// Row, Column, Text/Label, Button, CheckBox, Slider, TextField,
// ScrollView, ListView, plus ListModel/ListElement data models and Repeater
// for data-driven content. Properties are QML-spelled (width, height,
// color, radius, spacing, border.width, ...). Layout is flexbox rather than
// anchors — Row/Column with spacing/padding are the native idiom; anchors
// are not supported. Extensions beyond QML are noted inline.

// keyCount is one cache key's repeat count, saved so a delegate row can
// put it back when the row ends (see build).
type keyCount struct {
	key  keyID
	prev int
}

// build turns a parsed node into a ui element against env. doc carries
// per-id retained state (inputs, scrolls, declared properties) that
// survives both frames and hot reloads.
// build constructs the element for n, reusing a cached subtree when nothing
// it read has changed. The whole-document gate (Document.View) handles the
// idle case; this per-subtree cache makes the REBUILD case proportional to
// the change: a single property write re-evaluates only the subtrees whose
// recorded read-set contains it. Entries guard on node+env identity, so
// multiply-instantiated component nodes and per-build envs (delegates,
// Loader files) never cache — they rebuild exactly as before.
func build(n *node, env *Env, doc *docState) (ui.Element, error) {
	// Delegate rows used to be excluded from the cache outright, because
	// their scope was bound into env as plain values that no read-set
	// could see. It is bound through a token now (rowtoken.go) and the
	// interior of a view caches like anything else — except for a row
	// whose values could not be hashed, which keeps the old behaviour and
	// is what rowUntracked counts. Per-row cache entries do cost map
	// space, which is what the old comment here worried about; a row
	// rebuilt in full every frame costs more.
	// An effect CAPTURE builds the source's markup a second time for its
	// own offscreen copy; served from the cache it would get the source's
	// very element, which then sits in the tree twice and is laid out
	// twice (the source read its x back as the copy's).
	key := doc.keyID(n)
	if doc.rowUntracked > 0 || noRowCache && doc.rowDepth > 0 ||
		len(doc.capturing) > 0 || noSubtreeCache ||
		(cacheSkip != "" && strings.Contains(doc.keyStr(key), cacheSkip)) {
		el, err := buildFresh(n, env, doc)
		doc.lastEntry = nil
		return el, err
	}
	// The same node can build more than once in a frame under one key — a
	// style's content container is built as the delegate and again with
	// the page's children placed into it — and they are two different
	// subtrees. Number the repeats so each has its own cache slot; build
	// order is stable frame to frame, so the numbering is too. (The node
	// identity check used to hide this: two derived pointers never matched.)
	if doc.patching > 0 {
		doc.seenLog = append(doc.seenLog, key) // undone if the patch fails
	}
	// Inside a delegate row the counts are restored when the row ends, so
	// the numbering restarts for every build of it. It has to: a row is
	// built by a CALLBACK the ui layer runs during layout, after the
	// frame's build phase, and as many times as the layout runs — where
	// the frame reset (beginFrame) cannot reach it. Left to accumulate,
	// the suffix climbed past #80 on a seven-row list, every build missed
	// a cache key nothing had ever stored, and the subtree map grew for
	// as long as the window was open.
	if doc.rowDepth > 0 {
		doc.rowSeen = append(doc.rowSeen, keyCount{key, doc.keySeen.get(key)})
	}
	if seen := doc.keySeen.get(key); seen > 0 {
		doc.keySeen.set(key, seen+1)
		key = doc.intern(doc.keyStr(key) + "#" + strconv.Itoa(seen))
	} else {
		doc.keySeen.set(key, 1)
	}
	// No node-identity test: every node reaching here is a freshly derived
	// pointer (overrides merged, sizes stripped, handlers consumed, a
	// component's `inst := *comp`), so identity never held and the cache
	// never hit. What the derivation depends on is tracked instead — an
	// override write stamps its token, a moved item stamps its geometry
	// token, and a markup change drops the caches (invalidateCache).
	if buildStats {
		// Why a subtree did not hit, by reason (GFX_BUILD_STATS).
		switch e, ok := doc.subtrees.get2(key); {
		case !ok:
			doc.missAbsent++
		case e.env != env:
			doc.missEnv++
		case e.rs.Untracked():
			doc.missUntracked++
		case e.rs.Stale():
			doc.missStamped++
		}
	}
	if buildTrace {
		switch e, ok := doc.subtrees.get2(key); {
		case !ok:
			fmt.Fprintf(os.Stderr, "gml: fresh %s (absent)\n", doc.keyStr(key))
		case e.env != env:
			fmt.Fprintf(os.Stderr, "gml: fresh %s (env)\n", doc.keyStr(key))
		case e.rs.Untracked():
			fmt.Fprintf(os.Stderr, "gml: fresh %s (untracked)\n", doc.keyStr(key))
		case e.rs.Stale():
			fmt.Fprintf(os.Stderr, "gml: fresh %s (stamped)\n", doc.keyStr(key))
		default:
			fmt.Fprintf(os.Stderr, "gml: hit   %s\n", doc.keyStr(key))
		}
	}
	if e, ok := doc.subtrees.get2(key); ok && e.env == env {
		// Served as it is when nothing it read changed — or when only
		// CHILDREN changed and each can be redone in place (patch): the
		// chain of ancestors above an animated leaf keeps its elements
		// and its scope work, and only the leaf rebuilds.
		hit := !e.rs.Stale()
		if !hit && !noPatch && len(e.slots) > 0 && !e.rs.OwnStale() {
			hit = doc.patch(e, env)
		} else if !hit && buildTrace {
			fmt.Fprintf(os.Stderr, "gml: no patch %s: slots=%d ownStale=%v\n", doc.keyStr(key), len(e.slots), e.rs.OwnStale())
		}
		if hit {
			doc.cacheHits++
			core.ReplayReads(e.rs) // the parent depends on what this subtree read
			// Replay the subtree's element registrations (geometry refs read
			// them) into this frame's registry, and hand the entry to the
			// enclosing capture as one of its subtrees.
			doc.replayRegs(e)
			doc.linkSub(e)
			doc.lastEntry = e
			return e.el, nil
		}
	}
	ent := &subtreeEntry{node: n, env: env}
	doc.capStack = append(doc.capStack, ent)
	var el ui.Element
	var err error
	ent.rs = core.TrackReads(func() { el, err = buildFresh(n, env, doc) })
	doc.capStack = doc.capStack[:len(doc.capStack)-1]
	if err != nil || el == nil {
		doc.subtrees.del(key)
		doc.lastEntry = nil
		return el, err
	}
	ent.el = el
	doc.subtrees.set(key, ent)
	doc.linkSub(ent)
	doc.lastEntry = ent
	return el, nil
}

// linkSub records e as a subtree of the build in progress, so a later
// reuse of THAT build can replay e's registrations too. Held by reference:
// copying every registration into every enclosing entry was a quarter of
// a frame's allocations.
func (doc *docState) linkSub(e *subtreeEntry) {
	if n := len(doc.capStack); n > 0 {
		top := doc.capStack[n-1]
		top.subs = append(top.subs, e)
	}
}

// replayRegs re-registers a reused subtree's elements: its own, then its
// subtrees' recursively.
func (doc *docState) replayRegs(e *subtreeEntry) {
	for _, r := range e.regs {
		doc.elems.set(r.key, r.el)
		doc.stampFocus(r.key, r.el) // a reused element still takes focus requests and reports
	}
	for _, sub := range e.subs {
		doc.replayRegs(sub)
	}
}

// registerElem records an element for next frame's geometry refs and into
// every in-flight subtree capture (so reuse can replay it).
// noteIDKey records which registry key an id'd element was built under, so
// the next frame's geometry reads through that id find the right element
// even when it lives inside a component instance (prefixed) while the id
// itself was declared document-wide.
func (doc *docState) noteIDKey(n *node, key keyID) {
	id := nodeID(n)
	if id == "" || len(doc.capturing) > 0 {
		return
	}
	doc.idKeys.set(doc.intern(id), key)
	if doc.rowPrefix != "" {
		doc.idKeys.set(doc.here(id), key)
	}
}

func (doc *docState) registerElem(key keyID, el ui.Element) {
	// A copy built for an effect's capture must not claim the original's
	// geometry: it is drawn wherever the effect sits, and every binding
	// reading the source's x/y would then follow the copy — which moves the
	// copy, which moves the reads, frame after frame.
	if len(doc.capturing) > 0 {
		// Nor may the copy be RECORDED for replay: a cache hit of the
		// effect's subtree would re-register it under the source's key,
		// and the source would read the copy's geometry from then on.
		return
	}
	doc.elems.set(key, el)
	doc.stampFocus(key, el)
	// Into the innermost build only; outer ones reach it through subs.
	if n := len(doc.capStack); n > 0 {
		top := doc.capStack[n-1]
		top.regs = append(top.regs, elemReg{key, el})
	}
}

// attachedNoOpPrefixes are attached-property namespaces any item may carry
// that we accept and ignore: hover tooltips, accessibility metadata, and
// container hints handled elsewhere or not modeled.
var attachedNoOpPrefixes = [...]string{
	"Material.", "Universal.", // the controls' theme: read by themeOf
	"ToolTip.", "Accessible.", "Drag.", "StackView.", "SwipeView.",
	"ScrollBar.", "ScrollIndicator.", "ListView.", "GridView.",
	"Keys.forwardTo", "EnterKey.",
}

// stripAttachedNoOps removes attached attributes we accept and ignore,
// returning the same node when there are none.
func stripAttachedNoOps(n *node) *node {
	has := false
	for _, a := range n.attrs {
		if isAttachedNoOp(a.name) {
			has = true
			break
		}
	}
	if !has {
		return n
	}
	cp := *n
	cp.attrs = nil
	for _, a := range n.attrs {
		if !isAttachedNoOp(a.name) {
			cp.attrs = append(cp.attrs, a)
		}
	}
	return &cp
}

func isAttachedNoOp(name string) bool {
	for _, p := range attachedNoOpPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// applyBackgroundDelegate copies a Controls background delegate's fill and
// frame onto the container box: QML resizes the delegate to the control, so
// a Rectangle background is the box's own paint. Anything richer than
// color/radius/border/opacity is ignored (the delegate is not rendered as a
// separate element, which would need an anchor container here).
// backgroundLook reads the fill, frame and corner radius out of a
// background delegate, for the elements that paint their own box rather
// than being a plain container (text fields).
func backgroundLook(bg *node, env *Env) (fill, frame *ui.RGBA, radius float32) {
	radius = -1
	for _, a := range bg.attrs {
		if a.expr == nil {
			continue
		}
		switch a.name {
		case "color":
			if c, err := attrColor(a, env); err == nil {
				fill = &c
			}
		case "radius":
			if f, err := attrFloat(a, env); err == nil {
				radius = f
			}
		case "border.color":
			if c, err := attrColor(a, env); err == nil {
				frame = &c
			}
		}
	}
	return fill, frame, radius
}

func applyBackgroundDelegate(bg *node, d *ui.DivEl, env *Env) {
	var bw float32
	var bc ui.RGBA
	hasBC := false
	for _, a := range bg.attrs {
		if a.expr == nil {
			continue
		}
		switch a.name {
		case "color":
			if c, err := attrColor(a, env); err == nil {
				d.Bg(c)
			}
		case "radius":
			if f, err := attrFloat(a, env); err == nil {
				d.Rounded(f)
			}
		case "opacity":
			if f, err := attrFloat(a, env); err == nil {
				d.Opacity(f)
			}
		case "border.width":
			if f, err := attrFloat(a, env); err == nil {
				bw = f
			}
		case "border.color":
			if c, err := attrColor(a, env); err == nil {
				bc, hasBC = c, true
			}
		}
	}
	if bw > 0 && hasBC {
		d.Border(bw, bc)
	}
}

// foldGroupProperties rewrites QML group-property blocks — a child element
// whose type name starts lowercase, which no QML type ever does:
//
//	anchors { fill: parent; margins: 4 }   ==   anchors.fill: parent
//	                                           anchors.margins: 4
//
// into dotted attributes on the parent, and drops the pseudo-children.
// Returns the same node when there are none.
func foldGroupProperties(n *node) *node {
	has := false
	for _, c := range n.children {
		if isGroupBlock(c) {
			has = true
			break
		}
	}
	if !has {
		return n
	}
	cp := *n
	cp.attrs = append([]attr{}, n.attrs...)
	cp.children = nil
	for _, c := range n.children {
		if !isGroupBlock(c) {
			cp.children = append(cp.children, c)
			continue
		}
		for _, a := range c.attrs {
			a.name = c.typeName + "." + a.name
			cp.attrs = append(cp.attrs, a)
		}
	}
	return &cp
}

// isGroupBlock reports a group-property block: a child "element" whose type
// name begins with a lowercase letter (QML types are always capitalized).
func isGroupBlock(c *node) bool {
	if c.typeName == "" || len(c.children) > 0 || len(c.props) > 0 {
		return false
	}
	r := rune(c.typeName[0])
	return unicode.IsLower(r)
}

// shadowOwnAttrs binds an element's own attribute values under their bare
// names for the duration of its build, so a sibling attribute can read them
// the way QML's item scope allows (`cellHeight: cellWidth`). Values that
// fail to evaluate (they may reference the attr being shadowed) are left
// alone. The returned func restores the previous bindings.
func shadowOwnAttrs(n *node, env *Env, names ...string) func() {
	var undo []scopeSaved
	for _, name := range names {
		for _, a := range n.attrs {
			if a.name != name || a.expr == nil {
				continue
			}
			v, err := a.expr.eval(env)
			if err != nil {
				break
			}
			undo = append(undo, saveBinding(env, name))
			sv := &undo[len(undo)-1]
			env.vars[name] = v
			shadowDerived(env, sv)
			// DERIVED: the value was just computed from this item's own
			// attribute expression, in this scope, and whatever that
			// expression read was recorded as it was read. A sibling
			// attribute reading it back (`cellHeight: cellWidth`) learns
			// nothing new, so it must not poison the read-set.
			markDerived(env, sv, nil)
			break
		}
	}
	return func() { restoreBindings(env, undo) }
}

// shadowSelfGeometry binds an element's own previous-frame width/height
// into scope, QML-style (`radius: width / 2`, `property real s:
// Math.min(width, height)` — a bare geometry read means THIS item). Done
// in place so env identity (which subtree caching guards on) is unchanged;
// the returned func restores the previous bindings. Zero on the first
// frame: the geometry signature forces a settle rebuild once real bounds
// exist, and the reads are plain values, so caching stays correct.
// declaredSize reads a node's own size attribute (the first of names that
// evaluates), for the frame-one fallback above. Bindings that read the
// element's own geometry are skipped: they would recurse.
func declaredSize(n *node, env *Env, names ...string) float32 {
	for _, name := range names {
		for _, a := range n.attrs {
			if a.name != name || a.expr == nil {
				continue
			}
			if _, isLit := a.expr.(*litExpr); !isLit {
				// Only a literal is safe here: an expression could read the
				// very geometry we are standing in for.
				continue
			}
			if f, err := attrFloat(a, env); err == nil && f > 0 {
				return f
			}
		}
	}
	return 0
}

// bindGeom binds one geometry name in env for the duration of an
// evaluation, returning the undo. A zero size binds nothing: an item that
// states no width should not be told it is 0 wide.
func bindGeom(env *Env, name string, v float32) func() {
	if v <= 0 {
		return func() {}
	}
	prev, had := env.vars[name]
	env.vars[name] = float64(v)
	return func() {
		if had {
			env.vars[name] = prev
		} else {
			delete(env.vars, name)
		}
	}
}

// sizeAttr evaluates one size attribute of n in the current scope (0 when
// absent or unevaluable). Unlike declaredSize it accepts any expression, so
// it must only be used for attributes that cannot stand in for the geometry
// being resolved.
func sizeAttr(n *node, env *Env, name string) float32 {
	for _, a := range n.attrs {
		if a.name != name || a.expr == nil {
			continue
		}
		if f, err := attrFloat(a, env); err == nil && f > 0 {
			return f
		}
	}
	return 0
}

func shadowSelfGeometry(n *node, env *Env, doc *docState) func() {
	if n.noSelfReads {
		// Nothing in this subtree reads a bare width/height/opacity/
		// visible (see selfreads.go): no scope to shadow, no geometry
		// dependency to record.
		return noRestore
	}
	var b ui.Rect
	registered := false
	var geoTok *core.Property[uint64]
	if pe, ok := doc.prevElems.get2(doc.keyID(n)); ok {
		b = ui.Bounds(pe)
		registered = true
		// The item's OWN previous-frame geometry: record the dependency on
		// its geometry token here, inside the item's build, so the subtree
		// is stale exactly when the item moved — and the bare `width` /
		// `height` reads below can then be derived rather than untracked
		// (they carry the token, so a read outside this build's read-set —
		// a property binding's — records it too).
		geoTok = doc.geoToken(doc.keyID(n), pe)
		geoTok.Get()
	}
	// Frame one has no previous element, but the markup may state the size
	// itself — and `x: parent.width - width` has to mean something on the
	// first frame, or the item lands outside its parent and is clipped away
	// (a spin box's stepper, a handle at the end of a groove).
	if b.W == 0 {
		b.W = declaredSize(n, env, "width", "implicitWidth")
	}
	if b.H == 0 {
		b.H = declaredSize(n, env, "height", "implicitHeight")
	}
	undo := takeUndo(8)
	var bound map[string]any
	if !noPatch {
		bound = map[string]any{}
	}
	popBinds := doc.pushBinds(bound, false)
	if env.derivedNames == nil {
		env.derivedNames = map[string]bool{}
	}
	// width/height are derived (see above) only when they came from the
	// registered element whose token was just recorded. A declared or
	// layout-preferred size evaluated elsewhere — a Layout.preferredWidth
	// the parent read off geometry in ITS read-set — stays a plain value,
	// so reading it keeps the subtree untracked: a layer served from cache
	// at (80,50) where it belonged at (-70,-25) is what the alternative
	// looked like.
	derivedGeom := registered
	if env.derivedTok == nil {
		env.derivedTok = map[string]*core.Property[uint64]{}
	}
	var scrollTokFor *core.Property[uint64]
	bind := func(name string, v any) {
		prev, had := env.vars[name]
		undo = append(undo, scopeSaved{name: name, v: prev, ok: had})
		sv := &undo[len(undo)-1]
		env.vars[name] = v
		if bound != nil {
			bound[name] = v
		}
		if len(env.derivedNames) > 0 && env.derivedNames[name] {
			sv.wasDerived = true
			sv.wasTok = env.derivedTok[name]
			delete(env.derivedNames, name)
			if sv.wasTok != nil {
				delete(env.derivedTok, name)
			}
		}
		// Which token a read of this name depends on, and whether it is
		// derived at all. Worked out inline rather than through a
		// closure: a closure here is allocated on every bind of every
		// item, which was a tenth of the page's allocations.
		var tok *core.Property[uint64]
		derived := false
		switch name {
		case "width", "height":
			tok, derived = geoTok, derivedGeom
		case "opacity", "visible":
			// Evaluated from the item's own attributes, in this scope:
			// whatever they read was recorded as they were computed.
			derived = true
		case "contentX", "contentY", "contentWidth", "contentHeight", "visibleArea",
			"atXBeginning", "atXEnd", "atYBeginning", "atYEnd",
			"moving", "movingVertically", "movingHorizontally", "dragging", "flicking":
			// The scroll state's own values, carrying the state's token
			// (see scrollTok).
			tok, derived = scrollTokFor, true
		}
		if !derived {
			return
		}
		markDerived(env, sv, tok)
	}
	// A layout item's size IS the preference it states, and one axis is
	// routinely written in terms of the other (`Layout.preferredWidth: 10;
	// Layout.preferredHeight: width` — a round dot). Width is bound before
	// the height is evaluated so that read resolves; without it the height
	// is 0 and the dot stretches into a bar down its whole row.
	if b.W == 0 {
		b.W = sizeAttr(n, env, "Layout.preferredWidth")
	}
	bind("width", float64(b.W))
	if b.H == 0 {
		b.H = sizeAttr(n, env, "Layout.preferredHeight")
	}
	bind("height", float64(b.H))
	// The item's own `opacity`, which QML reads bare — and `visible` reads
	// it (`visible: opacity > 0.01` gates a fading overlay on its own
	// animated opacity), so it goes into scope first.
	opacity := 1.0
	for _, a := range n.attrs {
		if a.name == "opacity" && a.expr != nil {
			if v, err := a.expr.eval(env); err == nil {
				if f, err := toFloat64(v); err == nil {
					opacity = f
				}
			}
		}
	}
	bind("opacity", opacity)
	// The item's own `visible`, likewise (`opacity: visible ? 1 : 0`). It
	// is the declared value, not "is it on screen".
	visible := true
	for _, a := range n.attrs {
		if a.name == "visible" && a.expr != nil {
			if b, err := truthy(a.expr, env); err == nil {
				visible = b
			}
		}
	}
	bind("visible", visible)
	// A scrollable item's own scroll state is in ITS scope too, bare, the
	// way QML reads it (`property int currentPage: Math.round(contentX /
	// width)` declared on the Flickable itself).
	if isScrollType(n.typeName) {
		if id := nodeID(n); id != "" {
			st := doc.scrollFor(doc.idKey(id))
			scrollTokFor = scrollTok(st)
			for _, name := range scrollBareNames {
				if v, ok := scrollMember(st, name); ok {
					bind(name, v)
				}
			}
		}
	}
	return func() {
		popBinds()
		for i := len(undo) - 1; i >= 0; i-- {
			u := undo[i]
			if u.ok {
				env.vars[u.name] = u.v
			} else {
				delete(env.vars, u.name)
			}
			if u.wasDerived || u.nowDerived {
				restoreDerived(env, &u)
			}
		}
		putUndo(undo)
	}
}

// noRestore is the undo of a scope that bound nothing.
func noRestore() {}

// scopeSaved is one env binding a scope shadowed, kept to restore it.
type scopeSaved struct {
	name string
	v    any
	ok   bool
	// What the name's DERIVED status was before this scope shadowed it
	// (see Env.derived). Restoring the value without it left the outer
	// binding in place but no longer marked derived, so every read of it
	// from then on poisoned the reader's read-set — which is what an
	// item nested inside another item did to `opacity` and `visible`.
	wasDerived bool
	wasTok     *core.Property[uint64]
	// nowDerived: the shadowing binding marked the name derived itself,
	// so the restore has to take that mark away again.
	nowDerived bool
}

// saveBinding records everything about a name a shadowing scope has to
// put back: the value, whether there was one, and its DERIVED status
// (see Env.derived). Restoring the value alone left the outer binding in
// place but no longer marked derived, so every read of it after the
// inner scope unwound poisoned the reader's read-set — an item nested
// inside another item did exactly that to `opacity` and `visible`.
func saveBinding(env *Env, name string) scopeSaved {
	prev, ok := env.vars[name]
	sv := scopeSaved{name: name, v: prev, ok: ok}
	// The map is empty for most scopes: check the length rather than
	// hashing the name to find nothing, and consult the token map only
	// for the few names that are derived.
	if len(env.derivedNames) > 0 && env.derivedNames[name] {
		sv.wasDerived = true
		if len(env.derivedTok) > 0 {
			sv.wasTok = env.derivedTok[name]
		}
	}
	return sv
}

// shadowDerived drops the derived marks a shadowing binding must not
// inherit — using what saveBinding already looked up, so the common
// "was not derived" case costs nothing.
func shadowDerived(env *Env, sv *scopeSaved) {
	if !sv.wasDerived {
		return
	}
	delete(env.derivedNames, sv.name)
	if sv.wasTok != nil {
		delete(env.derivedTok, sv.name)
	}
}

// markDerived marks the name a binding just made as derived, recording
// on the undo entry that the restore has to undo it.
func markDerived(env *Env, sv *scopeSaved, tok *core.Property[uint64]) {
	if env.derivedNames == nil {
		env.derivedNames = map[string]bool{}
	}
	env.derivedNames[sv.name] = true
	sv.nowDerived = true
	if tok == nil {
		return
	}
	if env.derivedTok == nil {
		env.derivedTok = map[string]*core.Property[uint64]{}
	}
	env.derivedTok[sv.name] = tok
}

// restoreBindings undoes a list of saveBinding records, innermost first.
// shadowSelfGeometry inlines this loop instead: it runs for every item
// on the page and the call is not free there.
func restoreBindings(env *Env, undo []scopeSaved) {
	for i := len(undo) - 1; i >= 0; i-- {
		s := undo[i]
		if s.ok {
			env.vars[s.name] = s.v
		} else {
			delete(env.vars, s.name)
		}
		if s.wasDerived || s.nowDerived {
			restoreDerived(env, &s)
		}
	}
}

// restoreDerived puts one name's derived marks back — the rare branch of
// a restore, kept out of line so the common one is two bool tests.
func restoreDerived(env *Env, s *scopeSaved) {
	if s.wasDerived {
		env.derivedNames[s.name] = true
		if s.wasTok != nil {
			env.derivedTok[s.name] = s.wasTok
		}
		return
	}
	delete(env.derivedNames, s.name)
	if len(env.derivedTok) > 0 {
		delete(env.derivedTok, s.name)
	}
}

// undoPool recycles the undo lists of shadowControlScope and
// shadowSelfGeometry: one per control or item per build, of forty-odd
// entries for a control — the largest single allocation of a rebuild.
var undoPool = sync.Pool{New: func() any { s := make([]scopeSaved, 0, 48); return &s }}

func takeUndo(n int) []scopeSaved {
	p := undoPool.Get().(*[]scopeSaved)
	s := (*p)[:0]
	if cap(s) < n {
		s = make([]scopeSaved, 0, n)
	}
	return s
}

func putUndo(s []scopeSaved) {
	for i := range s {
		s[i] = scopeSaved{} // drop the references
	}
	s = s[:0]
	undoPool.Put(&s)
}

// scrollBareNames are the Flickable properties visible bare inside the
// item's own scope.
var scrollBareNames = []string{
	"contentX", "contentY", "contentWidth", "contentHeight", "visibleArea",
	"atXBeginning", "atXEnd", "atYBeginning", "atYEnd",
	"moving", "movingVertically", "movingHorizontally", "dragging", "flicking",
}

// withAttrOverrides returns n with the attributes assigned through its id
// applied — an imperative `bar.x = 40`, and every frame of an animation
// that drives one. They are appended, so they win over the markup's own.
func (doc *docState) withAttrOverrides(n *node) *node {
	key := doc.keyID(n)
	ov := doc.attrOv.get(key)
	if len(ov) == 0 {
		return n
	}
	// The merged node is kept on the node and refreshed in place: an
	// animation writes the same attribute every frame, and copying the
	// node and its attributes for it was a twentieth of the frame's
	// allocation. The override set rarely changes; when it does (a new
	// name, or a copy that is not this node's) the merge is redone.
	if m := n.ovNode; m != nil && n.ovFrom == attrsOf(n) && len(m.attrs) == len(n.attrs)+len(ov) {
		ok := true
		for i := len(n.attrs); i < len(m.attrs); i++ {
			a := &m.attrs[i]
			v, has := doc.attrOverride(key, a.name)
			if !has {
				ok = false
				break
			}
			a.expr.(*litExpr).v = v
		}
		if ok {
			return m
		}
	}
	extra := make([]attr, 0, len(ov))
	for name := range ov {
		// Through attrOverride, so the READ is recorded: an animation
		// writing this attribute then invalidates the subtrees that
		// actually read it instead of the whole document (see
		// setAttrOverride).
		v, _ := doc.attrOverride(key, name)
		extra = append(extra, attr{name: name, expr: &litExpr{v: v}})
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].name < extra[j].name })
	m := n.merged(extra)
	n.ovNode, n.ovFrom = m, attrsOf(n)
	return m
}

// useSite is the id a page uses for a styled control, together with every
// object that id already named there. The objects are held as POINTERS
// because the style instantiation re-points the id's map entries at its own
// scope: what the page's handlers captured is one of these.
type useSite struct {
	id   string
	objs []*elemObject
}

// noteTrigger remembers where the element that is handling a click sits.
// A `menu.popup()` with no coordinates opens at its trigger — QML opens it
// at the pointer, and the element under the pointer is the one whose
// handler is running.
func (doc *docState) noteTrigger(el ui.Element) {
	if el != nil {
		doc.trigger = ui.Bounds(el)
	}
}

// attrOverride reads an attribute override for an element key.
func (doc *docState) attrOverride(key keyID, name string) (any, bool) {
	m := doc.attrOv.get(key)
	if m == nil {
		return nil, false
	}
	v, ok := m[name]
	if ok {
		if t := doc.attrOvT.get(key)[name]; t != nil {
			t.Get() // record the read; see setAttrOverride
		}
	}
	return v, ok
}

// setAttrOverride records an attribute assigned through an id.
func (doc *docState) setAttrOverride(key keyID, name string, v any) {
	m := doc.attrOv.get(key)
	if m == nil {
		m = map[string]any{}
		doc.attrOv.set(key, m)
	}
	if old, had := m[name]; had && str(old) == str(v) {
		return
	}
	m[name] = v
	// `item.focus = true` from a handler moves focus there, as Qt's does.
	if name == "focus" {
		if b, _ := toBool(v); b {
			doc.requestFocus(key)
		}
	}
	switch name {
	case "x", "y", "width", "height":
		// Geometry is read from the laid-out tree, which lags this write
		// by a frame: until then the write reads back as written.
		if doc.geomWritten == nil {
			doc.geomWritten = map[geomKey]int{}
		}
		doc.geomWritten[geomKey{key, name}] = doc.frameNo
	}
	// Every override carries a TRACKING TOKEN: a property whose only job
	// is to be read while a subtree builds (see attrOverride) and written
	// here. Without one, nothing in the property system knew about this
	// map, so the only way to make a write visible was to drop every
	// cached subtree — and since an animator writes an override on every
	// frame, an animated page rebuilt its whole document 60 times a
	// second (measured on the buttons page: 234 subtree builds a frame,
	// not one of them reused). With the token, the write marks exactly
	// the subtrees that read this attribute.
	//
	// A NEW override is still a structural change — a node that had no
	// override now has one, and the trees built before it cannot know —
	// so the first write for a (key, name) drops the caches as before.
	tm := doc.attrOvT.get(key)
	if tm == nil {
		tm = map[string]*core.Property[string]{}
		doc.attrOvT.set(key, tm)
	}
	t := tm[name]
	if t == nil {
		t = &core.Property[string]{}
		tm[name] = t
		doc.invalidateCache()
	}
	t.Set(str(v)) // stamps the read-sets that read this override
	core.BumpRevision()
}

// transformOriginFrac maps QML's transformOrigin to fractions of the item's
// box (Center is the default).
func transformOriginFrac(name string) (float32, float32) {
	switch {
	case strings.Contains(name, "TopLeft"):
		return 0, 0
	case strings.Contains(name, "TopRight"):
		return 1, 0
	case strings.Contains(name, "BottomLeft"):
		return 0, 1
	case strings.Contains(name, "BottomRight"):
		return 1, 1
	case strings.Contains(name, "Top"):
		return 0.5, 0
	case strings.Contains(name, "Bottom"):
		return 0.5, 1
	case strings.Contains(name, "Left"):
		return 0, 0.5
	case strings.Contains(name, "Right"):
		return 1, 0.5
	}
	return 0.5, 0.5
}

// hasFlexAttrs reports whether a box declares gml's flex-container
// extensions, which mean "lay these children out" even in a QML document.
func hasFlexAttrs(n *node) bool {
	for _, a := range n.attrs {
		switch a.name {
		case "justify", "items", "spacing", "gap", "row", "col", "wrap":
			return true
		}
	}
	return false
}

// hasAttr reports whether the node declares the named attribute.
func hasAttr(n *node, name string) bool {
	for _, a := range n.attrs {
		if a.name == name {
			return true
		}
	}
	return false
}

func buildFresh(n *node, env *Env, doc *docState) (built ui.Element, buildErr error) {
	// Accessible.role/name/description tag what the item built (access.go).
	// (The node as declared: n is stripped of its attached attributes below.)
	declared := n
	defer func() {
		if buildErr == nil {
			applyAccessible(declared, env, built)
		}
	}()
	doc.freshBuilds++ // dirty-tracking effectiveness counter (tests, profiling)
	// An item that states a theme (Material.theme, a palette role) draws the
	// controls inside it in that theme (theme.go).
	if hasThemeAttrs(n) {
		sc := doc.themeScope(n)
		sc.Push(themeOf(n, env))
		defer sc.Pop()
	}
	if n.instTok != nil {
		n.instTok.Get() // the use-site values this instance was built from
	}
	// visible: false collapses the element (QML hides it, no space); strip the
	// attr either way so no builder needs to special-case it.
	n2, isHidden := applyVisible(n, env)
	// Publish the resolved value as id.visible: siblings bind against it
	// (`anchors.leftMargin: avatar.visible ? 6 : 0`), and QML's `visible`
	// is a readable property, not just a build-time switch.
	if id := nodeID(n); id != "" {
		doc.exposeIDProp(n, "visible", !isHidden)
		// `opacity` is readable the same way (`layer.enabled: root.opacity
		// > 0`), and defaults to 1 like QML's.
		opacity := 1.0
		for _, a := range n.attrs {
			if a.name == "opacity" && a.expr != nil {
				if v, err := a.expr.eval(env); err == nil {
					if f, err := toFloat64(v); err == nil {
						opacity = f
					}
				}
			}
		}
		doc.exposeIDProp(n, "opacity", opacity)
	}
	if isHidden {
		// QML's `visible: false` hides an item; it does not remove it. One
		// that something refers to by id keeps its geometry, so a sibling
		// can anchor to it and an effect can sample it (`anchors.fill:
		// sourceImage` over a hidden Image is the whole Qt5Compat
		// effect-chain idiom, and it was collapsing to nothing here). In a
		// positioner or a layout a hidden item really does take no space,
		// which the empty box gives.
		if nodeID(n) == "" || doc.inLayout {
			return ui.Div(), nil
		}
		el, err := buildElement(n2, env, doc)
		if err != nil || el == nil {
			return ui.Div(), nil
		}
		doc.registerElem(doc.keyID(n), el)
		doc.noteIDKey(n, doc.keyID(n))
		return ui.Hidden(el), nil
	}
	n = n2
	// Signal and property-change on-handlers are consumed by the signal /
	// change mechanisms, not the element builder — strip them before dispatch.
	if len(n.signals) > 0 || len(n.props) > 0 {
		n = stripConsumedHandlers(n)
	}
	// Component.onCompleted / onDestruction: lifecycle handlers on any element.
	// Collect onCompleted once (run after the first build); strip both so the
	// element builder doesn't see them as unknown attributes.
	n = doc.collectCompleted(n, env)
	// QtQuick.Layouts attached properties (Layout.fillWidth, Layout.alignment,
	// ...). Strip them so the element builder doesn't see unknown attributes;
	// fillWidth/fillHeight map to flexbox grow and preferredWidth/Height to
	// fixed sizes, applied after the build.
	statedFillW, statedFillH := hasAttr(n, "Layout.fillWidth"), hasAttr(n, "Layout.fillHeight")
	n, layoutFill, prefW, prefH, minW, minH, layoutAlign, layoutMar := extractLayoutAttrs(n, env)
	// A layout hint that gives the item a width is one of the things that
	// decides whether a wrapping Text breaks its lines (textWidthImposed);
	// the attrs themselves were just stripped, so record it on the node the
	// builders see. extractLayoutAttrs already returned a copy when it
	// stripped anything, so this never touches the shared tree.
	if (layoutFill.w || prefW > 0) && !n.imposedWidth {
		n.imposedWidth = true
	}
	// An item's attached ToolTip (ToolTip.text / .visible / .delay), read
	// before the attached-property strip below takes it away.
	tip, hasTip := tooltipOf(n, env)
	// Attached properties of things we don't model (ToolTip.text on any
	// item, Accessible.*, ...): strip them so no builder has to list them.
	n = stripAttachedNoOps(n)
	// Stray anchors.* attributes: the child's parent wasn't an anchor
	// container (e.g. anchors.verticalCenter inside a Row), so no earlier
	// pass stripped them. QML positioners generally ignore anchors set on
	// their children too — accept as a no-op rather than rejecting the
	// element outright.
	anchorAlign, hasAnchorAlign := crossAnchorAlign(n, doc.layoutRow)
	n = stripStrayAnchors(n)
	// Attached TableView.* attributes: TableView.editDelegate is consumed by
	// buildTableView (in-place cell editing); the rest are accepted no-ops.
	n = stripAttachedPrefix(n, "TableView.")
	// Apply active-state property overrides targeting this element by id.
	if doc.state != nil {
		if id := nodeID(n); id != "" {
			// Its anchors are the anchor pass's (buildAnchoredChild merges the
			// state there, before they are parsed and stripped): an
			// AnchorChanges must not come back here as attributes.
			if ov := withoutAnchors(doc.state.overridesFor(id)); len(ov) > 0 {
				n = n.merged(ov)
			}
		}
	}
	// Attribute values assigned through this element's id (see attrOv).
	n = doc.withAttrOverrides(n)
	// An item's OWN states may target the item itself — the usual shape for
	// a single morphing rectangle — and that has to be merged here, before
	// its attributes are applied (the states scope buildBox sets up governs
	// its children).
	if id := nodeID(n); id != "" && hasAttr(n, "states") {
		if ctx, err := computeStateCtx(n, env, doc); err == nil && ctx != nil {
			if ov := ctx.overrides[id]; len(ov) > 0 {
				n = n.merged(ov)
			}
		}
	}
	// Bare self-geometry (QML: `radius: width / 2` resolves width against
	// the element itself): shadow the element's previous-frame bounds into
	// the scope for this build, restored after (in-place, so env identity —
	// which subtree caching guards on — is unchanged). Zero-sized on the
	// first frame; the geometry signature forces a settle rebuild once real
	// bounds exist, and reads mark the subtree untracked (plain values), so
	// caching stays correct.
	defer shadowSelfGeometry(n, env, doc)()
	// The enclosing layout's shape decides how THIS item's Layout.fill*
	// reads, but not its descendants': a card in a GridLayout cell is a
	// cell, the fields inside the card are not. Capture the flags for the
	// hints applied below and clear the grid one for the subtree, or a
	// control's fixed size gets flex-distributed down inside every cell.
	inGrid := doc.layoutGrid
	inRow := doc.layoutRow
	inLayout := doc.inLayout
	inPositioner := doc.inPositioner
	doc.layoutGrid, doc.inLayout = false, false
	// An item managed by a LAYOUT is sized by its preferred size, and an
	// explicit width or height is only the last of the three candidates:
	// Layout.preferred* wins, then the item's own IMPLICIT size, then
	// width/height. A control therefore keeps its implicit size in a
	// layout however wide the markup says it is — Qt renders `GlowButton {
	// width: 180; height: 50 }` at the style's implicit 150x40 — while a
	// Rectangle, whose implicit size is nothing, does get the width it
	// states (verified against Qt both ways).
	//
	// The attributes are dropped BEFORE the element builds, so whatever
	// builds it falls back to its own implicit size; clearing the size
	// afterwards left a styled control with a 0x0 box and no background.
	if (inLayout || inGrid) && !noImplicitSizeType(rootTypeOf(n, doc)) {
		n = stripSizeAttrs(n)
	}
	el, err := buildElement(n, env, doc)
	doc.layoutGrid, doc.inLayout = inGrid, inLayout
	if err != nil {
		if doc.lenient {
			// .qml compat: Qt warns and renders the rest of the page when
			// an element fails (missing C++ types, unsupported members).
			// The failed element becomes an empty placeholder.
			if len(doc.capturing) == 0 {
				// Inside an effect capture the same markup is being built a
				// second time; anything wrong with it was already reported
				// where it really lives.
				logf("gml: %v (element skipped)", err)
			}
			return ui.Div(), nil
		}
		return nil, err
	}
	// The item's attached ToolTip, floated above it while the pointer rests
	// on it (see attachTooltip).
	if hasTip {
		el = doc.attachTooltip(n, env, el, tip)
	}
	// A stated preference is a size, not a suggestion: Qt hands the item its
	// preferred size and lets the row overflow rather than squeezing the
	// label out of a button. It is a floor as well as a size, so the
	// crowding pass below leaves it alone.
	//
	// Unless the item also asked to FILL that axis: then the layout's own
	// size wins and the preference is only a hint for the slack. Markup
	// states both together (`Layout.fillWidth: true; Layout.preferredWidth:
	// root.width - 10`) and Qt hands such an item the width the layout has,
	// which may be less; treating the preference as a size there overflows
	// the container by the difference.
	// A LAYOUT inside a layout FILLS by default: Qt's Layout.fillWidth /
	// fillHeight default to false for an item and TRUE for a layout, so a
	// nested RowLayout takes the surplus its own children can use. The
	// corpus' deploy-preview tiles are exactly that — a RowLayout with
	// `Layout.preferredHeight: 72` whose tiles fill, which Qt grows to 194
	// while ours sat at the stated 72 with the panel empty beneath it.
	// (What a filling nested layout may then take is still capped by its
	// content; see ui.DivEl.layoutMax.)
	if (inLayout || inGrid) && qtLayoutType(rootTypeOf(n, doc)) {
		if !statedFillW && layoutChildFills(n, env, "Layout.fillWidth") {
			layoutFill.w = true
		}
		if !statedFillH && layoutChildFills(n, env, "Layout.fillHeight") {
			layoutFill.h = true
		}
	}
	// A FILLING item's preference is not its size — but it IS its weight.
	// Qt's layout engine divides the space among the items that fill in
	// proportion to their preferred sizes (the stretch factor defaults to
	// the size hint): a column holding a `Layout.preferredHeight: 72` tile
	// strip and a bare `Item { Layout.fillHeight: true }` gives the strip
	// 608 of 615 and the spacer 7, where an equal split (ours) left the
	// strip at half the height and the panel empty beneath it. Probed
	// against Qt three ways: 72+0 -> 608/7, 72+24 -> 461/154 (exactly 3:1),
	// no preferences -> 308/307.
	fillWeightW, fillWeightH := float32(0), float32(0)
	if prefW > 0 && layoutFill.w {
		fillWeightW, prefW = prefW, 0
	}
	if prefH > 0 && layoutFill.h {
		fillWeightH, prefH = prefH, 0
	}
	if prefW > 0 || prefH > 0 {
		switch e := el.(type) {
		case *ui.DivEl:
			if prefW > 0 {
				e.W(prefW).MinW(prefW)
			}
			if prefH > 0 {
				e.H(prefH).MinH(prefH)
			}
		case *ui.TextEl:
			// A Label is a layout item too: a table of them states its
			// column widths this way, and without this every column
			// collapsed to the width of its text.
			if prefW > 0 {
				e.W(prefW)
			}
			if prefH > 0 {
				e.H(prefH)
			}
		case *ui.Canvas:
			// A Canvas is a layout item as well: the corpus' easing plot
			// is `Layout.fillWidth` with a preferred height, and without
			// this it sat at the 300x150 an HTML canvas defaults to.
			if prefW > 0 {
				e.W(prefW)
			}
			if prefH > 0 {
				e.H(prefH)
			}
		}
	}
	// Layout minimums are floors, so the item can still stretch or grow.
	if minW > 0 || minH > 0 {
		if e, ok := el.(*ui.DivEl); ok {
			if minW > 0 {
				e.MinW(minW)
			}
			if minH > 0 {
				e.MinH(minH)
			}
		}
	}
	// A bare Item or Rectangle has NO implicit size in QML: it does not
	// grow to hold its children the way a positioner or a layout does. As
	// an item OF A LAYOUT that matters — it contributes nothing to the
	// preferred size of the row, column or track it sits in, so a grid of
	// such cards comes out in equal halves however much text each holds,
	// and their size comes from their Layout.fill*/minimum*/preferred*
	// hints alone. Outside a layout the distinction is invisible (an
	// anchored or explicitly sized item is unaffected either way), so this
	// stays where Qt's rule is observable.
	if inLayout || inGrid {
		switch rootTypeOf(n, doc) {
		case "Rectangle", "Item", "MouseArea", "FocusScope":
			if d, ok := el.(*ui.DivEl); ok && !hasAttr(n, "width") && !hasAttr(n, "height") &&
				!hasAttr(n, "implicitWidth") && !hasAttr(n, "implicitHeight") {
				d.NoImplicitSize()
			}
		}
	}
	// Layout.fillWidth/fillHeight: on the enclosing layout's MAIN axis the
	// item shares the leftover space (flex grow); on the CROSS axis it
	// stretches to the line, which the layout does for auto-sized items —
	// so drop the fixed size on that axis instead of growing, or a
	// `Layout.fillWidth` inside a ColumnLayout would stretch the item
	// VERTICALLY and squash its siblings.
	if layoutFill.w || layoutFill.h {
		mainFill, crossFill := layoutFill.h, layoutFill.w
		mainWeight := fillWeightH
		if inRow {
			mainFill, crossFill = layoutFill.w, layoutFill.h
			mainWeight = fillWeightW
		}
		if inGrid {
			// A grid places children in cells itself, so each axis stands
			// on its own: fillWidth fills the cell's width, fillHeight its
			// height. The grid reads them back as Grow/CrossFill.
			mainFill, crossFill = layoutFill.w, layoutFill.h
			mainWeight = fillWeightW
		}
		grow := func(set func(float32)) {
			if mainWeight > 0 {
				set(mainWeight)
				return
			}
			// No stated preference: the item's own CONTENT is its weight,
			// which is what Qt's stretch factor defaults to (-1 in the
			// element itself, meaning "weight by the size hint"). A ROW of
			// filling columns therefore divides the width in proportion to
			// what each column wants — probed against Qt, four labelled
			// columns of implicit width 45.5/22.6/79.4/32.7 came out
			// 210/105/365/150. ui reads a negative factor as exactly that.
			//
			// Widths only. A column's preferred HEIGHT is height-for-width
			// — our first pass measures a card before its width is settled,
			// so weighting by that measurement collapsed cards whose
			// content had not resolved yet (the canvas page's gradient card
			// lost everything below its title).
			if inRow {
				set(-1)
				return
			}
			set(1)
		}
		switch e := el.(type) {
		case *ui.DivEl:
			if mainFill {
				grow(func(f float32) { e.GrowBy(f) })
			}
			if crossFill {
				if inGrid {
					// A grid cell keeps its size and the grid stretches
					// it; zeroing the axis here would lose it.
				} else if doc.layoutRow {
					e.H(0)
				} else {
					e.W(0)
				}
				// A RowLayout only stretches the items that asked (the
				// others keep their height and sit level with them).
				e.CrossFill()
			}
		case *ui.LazyImageEl:
			if mainFill {
				grow(func(f float32) { e.GrowBy(f) })
			}
		case *ui.Canvas:
			if mainFill {
				grow(func(f float32) { e.GrowBy(f) })
			}
			if crossFill {
				// Drop the size on the filled axis (a canvas states 300x150
				// by default, as in HTML) so the layout can stretch it, the
				// way an auto-sized box is stretched.
				if doc.layoutRow {
					e.H(0)
				} else {
					e.W(0)
				}
				e.CrossFill()
			}
		case *ui.ScrollEl:
			// A ScrollView is as much a layout item as a box is — without
			// this its Layout.fillHeight did nothing and it sat at its
			// content height inside a column that had room to spare.
			if mainFill {
				grow(func(f float32) { e.GrowBy(f) })
			}
		case *ui.ListEl:
			// So is a ListView, and its Layout.fillHeight reached nothing
			// at all: the navigation display's flight plan sat at the 200px
			// a bare list defaults to, centred in a slot twice that, which
			// put its first row 80px below Qt's and cut the last one off.
			if mainFill {
				grow(func(f float32) { e.GrowBy(f) })
			}
		case *ui.GridEl:
			if mainFill {
				grow(func(f float32) { e.GrowBy(f) })
			}
		case *ui.Paragraph:
			// A wrapping Label with Layout.fillWidth takes the leftover
			// width of its row and wraps to THAT, which is how a card's
			// description column is written. Without it the label
			// measured against the whole row and ran off the card.
			if mainFill {
				e.Grow()
			}
			if crossFill {
				e.CrossFill()
			}
		case *ui.TextEl:
			if mainFill {
				grow(func(f float32) { e.GrowBy(f) })
			}
			if crossFill {
				e.CrossFill()
			}
		}
	}
	// Layout.alignment: the item sits where it says in the space the layout
	// gives it, and is not stretched to fill it — `Layout.alignment:
	// Qt.AlignHCenter` on a button in a card is how the corpus centres one.
	if hasAnchorAlign && inPositioner && layoutAlign == 0 {
		// A positioner sets only the main axis, so a child anchored across
		// it (`Label { anchors.horizontalCenter: parent.horizontalCenter }`
		// inside a Column) is centred by that anchor — the stray-anchor
		// pass drops the attribute, but not what it meant.
		switch e := el.(type) {
		case *ui.DivEl:
			e.AlignSelf(anchorAlign)
		case *ui.TextEl:
			e.AlignSelf(anchorAlign)
		case *ui.Paragraph:
			e.AlignSelf(anchorAlign)
		}
	}
	if layoutAlign != 0 && (inLayout || inGrid) {
		if a, ok := crossAlign(layoutAlign, inRow); ok {
			switch e := el.(type) {
			case *ui.DivEl:
				e.AlignSelf(a)
			case *ui.TextEl:
				e.AlignSelf(a)
			case *ui.Paragraph:
				e.AlignSelf(a)
			}
		}
	}
	// An item in a layout gives way when the row runs short, rather than
	// overflowing it (Qt shrinks toward Layout.minimumWidth); one that
	// states its own size keeps it.
	if inLayout && !hasAttr(n, "width") && !hasAttr(n, "height") {
		if d, ok := el.(*ui.DivEl); ok {
			d.Shrink()
			// ...but not below the size it states for itself. A control's
			// implicit size is its preferred size in Qt's layouts, which
			// overflow rather than squeeze — a zoom ComboBox stating
			// implicitWidth 90 keeps 90 and its label fits. A stated
			// Layout.preferred* wins over the implicit size, so that axis
			// is already floored above.
			// ...unless it asked to FILL that axis: a filling control is
			// one Qt shrinks (a slider in a row gets narrower, it does not
			// push the row past its card), and a floor there would make
			// the row demand its implicit width.
			fw, fh := ui.FixedSize(d)
			if prefW == 0 && fw > 0 && !layoutFill.w {
				d.MinW(fw)
			}
			if prefH == 0 && fh > 0 && !layoutFill.h {
				d.MinH(fh)
			}
		}
	}
	// Layout.*Margin: space around the item inside its cell. A padded
	// wrapper carries it, so the item itself keeps its own size and hints.
	if layoutMar.set && (inLayout || inGrid) {
		wrap := ui.Div().Padding(layoutMar.t, layoutMar.r, layoutMar.b, layoutMar.l)
		if d, ok := el.(*ui.DivEl); ok {
			// The wrapper stands in for the item in its cell, so it has to
			// carry what the layout reads off it.
			if d.GrowFactor() > 0 {
				wrap.Grow()
			}
			if d.CrossFills() {
				wrap.CrossFill()
			}
			if a, ok := d.SelfAlign(); ok {
				wrap.AlignSelf(a)
			}
		}
		el = wrap.Children(el)
	}
	// `layer.enabled` with a blur effect: the item renders into its own
	// texture and is composited blurred (see layer.go).
	el = wrapLayer(n, el, env, doc)
	// Record for next frame's geometry refs (parent / id.width).
	doc.registerElem(doc.keyID(n), el)
	doc.noteIDKey(n, doc.keyID(n))
	return el, nil
}

// materializeElemDefault converts an element-valued property default (the
// parser keeps the element node) into its runtime value: FontLoader
// instantiates into its {name, source, status} object; unmodeled element
// types stay null so dependent bindings fail soft, as before.
func materializeElemDefault(v any, env *Env, doc *docState) any {
	nd, ok := v.(*node)
	if !ok {
		return v
	}
	if nd.typeName == "FontLoader" {
		return instantiateFontLoader(nd, env, doc)
	}
	return nil
}

// stripAttachedPrefix returns a copy of n without attributes under the
// given attached-property prefix (a no-op copy only when one is present).
func stripAttachedPrefix(n *node, prefix string) *node {
	has := false
	for _, a := range n.attrs {
		if strings.HasPrefix(a.name, prefix) {
			has = true
			break
		}
	}
	if !has {
		return n
	}
	cp := *n
	cp.attrs = nil
	for _, a := range n.attrs {
		if !strings.HasPrefix(a.name, prefix) {
			cp.attrs = append(cp.attrs, a)
		}
	}
	return &cp
}

// styleShadowed lists the control types a Qt style module may reimplement;
// when an imported module provides one, its QML wins over the builtin
// (QML import shadowing). Kept to the kinds our Templates layer renders
// well so other styled controls keep the sturdier builtins.
func styleShadowed(name string) bool {
	switch name {
	// Kinds our Templates layer composes faithfully: their style files use
	// background / contentItem / indicator / handle / up.indicator /
	// down.indicator delegates and read only control state we expose, and
	// the interaction is a click (or, for Slider, a bare overlay slider
	// that supplies press and drag; for SpinBox, a step per indicator).
	// Tumbler is the one still on its builtin: its style needs the
	// keyboard/flick routing the composed box cannot do yet.
	case "Button", "Label", "CheckBox", "Switch", "RadioButton", "ToolButton",
		"Slider", "RangeSlider", "TextField", "TextArea", "ComboBox",
		"ProgressBar", "Dial", "SpinBox", "Tumbler", "BusyIndicator",
		// Floating kinds: their style states the panel (background, and a
		// Dialog's header/footer bands) while the popup itself still owns
		// its open state and signals. Unshadowed, every dialog in an app
		// with its own style drew OUR chrome over ITS page.
		"Popup", "Dialog", "Menu", "MenuItem", "ToolTip":
		return true
	}
	return false
}

// shadowsType reports whether the file currently building imports a module
// that provides its own version of a builtin type name (QML import
// shadowing, which is per file — a page's `import qmlsnippetsstyle` does
// not restyle the component files it instantiates).
func (doc *docState) shadowsType(name string) bool {
	if doc.moduleTypesBy == nil {
		return false
	}
	// An app-wide Controls style applies everywhere, imports aside.
	if doc.styleModule != "" && doc.moduleTypesBy[doc.styleModule][name] != "" {
		return true
	}
	if doc.curFile == nil {
		return false
	}
	for _, imp := range doc.curFile.imports {
		if doc.moduleTypesBy[imp][name] != "" {
			return true
		}
	}
	return false
}

// isControlsContainer reports the Qt Quick Controls containers that take
// their implicit size from their content (their declared children become
// the contentItem), rather than staying empty like a bare Item.
func isControlsContainer(t string) bool {
	switch t {
	case "Pane", "Frame", "GroupBox", "Page", "Control", "Container", "ToolBar", "MenuBar":
		return true
	}
	return false
}

func buildElement(n *node, env *Env, doc *docState) (ui.Element, error) {
	// A Go-implemented visual element (gml.RegisterPaintedType) — checked
	// before the builtins so an app can also replace one.
	if isPaintedType(n.typeName) {
		return buildPainted(n, env, doc)
	}
	if styleShadowed(n.typeName) && doc.shadowsType(n.typeName) {
		if comp := doc.fileComponent(n.typeName); comp != nil {
			return buildFileComponent(n, comp, env, doc)
		}
	}
	switch n.typeName {
	case "Rectangle", "Item", "Row", "Column", "MouseArea", "RowLayout", "ColumnLayout", "Flow",
		// Container-ish Controls and windows behave as plain boxes here:
		// Qt Quick Controls containers (Pane/Frame/GroupBox/Page/Control) and
		// the window roots (ApplicationWindow/Window) just lay out their
		// children; a Page's or window's header/footer/menuBar band it
		// (pagebands.go), and its title bar is accepted as a no-op.
		"Pane", "Frame", "GroupBox", "Page", "Control", "Container", "FocusScope",
		"ApplicationWindow", "Window", "ToolBar",
		"ScrollIndicator":
		if isBandedType(n.typeName) {
			if el, ok, err := buildBanded(n, env, doc); ok {
				return el, err // header/footer/menuBar around the content (pagebands.go)
			}
		}
		return buildBox(n, env, doc)
	case "MenuBar":
		// A row of menu titles; clicking one drops its Menu (see menu.go).
		return buildMenuBar(n, env, doc)
	case "Menu":
		// A menu opened on its own — by popup(), or by a `visible:` binding.
		return buildMenu(n, env, doc)
	case "SplitView":
		// Panes along one axis with a handle between them (see splitview.go).
		return buildSplitView(n, env, doc)
	case "PageIndicator":
		// One dot per page, the current one highlighted.
		return buildPageIndicator(n, env, doc)
	case "MonthGrid":
		// Six weeks of day cells for one month.
		return buildMonthGrid(n, env, doc)
	case "DayOfWeekRow":
		// The weekday-name header above a MonthGrid.
		return buildDayOfWeekRow(n, env, doc)
	case "Tumbler":
		// Spinning wheel of model items centred on currentIndex.
		return buildTumbler(n, env, doc)
	case "TabBar":
		// Tabbed selector: tracks a currentIndex its TabButtons drive.
		return buildTabBar(n, env, doc)
	case "PathView":
		// Delegates spaced along a Path (no flick — display layout only).
		return buildPathView(n, env, doc)
	case "PdfMultiPageView", "PdfPageView", "PdfScrollablePageView":
		// The PDF page area (see pdf.go: no renderer, so no pages).
		return buildPdfView(n, env, doc, n.typeName)
	case "Map":
		// QtLocation: projected overlays, no basemap (see map.go).
		return buildMap(n, env, doc)
	case "VideoOutput":
		// The video frame area (see media.go: no decoder, so it is black).
		return buildVideoOutput(n, env, doc)
	case "View3D":
		// QtQuick3D: a scene declared in markup (see scene3d.go).
		return buildView3D(n, env, doc)
	case "Viewport3D":
		// Embedded live 3D view (a host-registered render.View3D).
		return buildViewport3D(n, env, doc)
	case "StackLayout", "SwipeView", "StackView":
		// Navigation containers: show only the current page (currentIndex /
		// pushed stack), not all children stacked.
		return buildStack(n, env, doc)
	case "Popup", "Dialog", "Drawer", "ToolTip":
		// Floating overlays: rendered above everything when open (see buildPopup).
		return buildPopup(n, env, doc)
	case "Grid", "GridLayout":
		return buildGrid(n, env, doc)
	case "Image", "BorderImage", "AnimatedImage":
		return buildImage(n, env, doc)
	case "Canvas":
		return buildCanvas(n, env, doc)
	case "Shape":
		// QtQuick.Shapes: declarative vector paths (see shape.go).
		return buildShape(n, env, doc)
	case "Text", "Label":
		return buildText(n, env, doc)
	case "ItemDelegate", "SwipeDelegate":
		// A delegate is a ROW, not a button: no fill of its own, its label
		// leading, and it takes the width its view gives it. (Rendered as a
		// button, a drawer's menu of ItemDelegates came out as a stack of
		// pills.)
		return buildDelegateRow(n, env, doc)
	case "Button", "ToolButton", "DelayButton", "RoundButton",
		"TabButton", "MenuItem", "MenuBarItem":
		return buildButton(n, env, doc)
	case "ToolSeparator", "MenuSeparator":
		return buildSeparator(n, env)
	case "DropArea":
		return buildDropArea(n, env, doc)
	case "GraphsView":
		return buildGraphsView(n, env, doc)
	case "Bars3D", "Scatter3D", "Surface3D":
		return buildChart3D(n, env, doc)
	case "ParticleSystem":
		return buildParticleSystem(n, env, doc)
	case "ShaderEffectSource":
		return buildShaderEffectSource(n, env, doc)
	case "ShaderEffect",
		// Qt5Compat.GraphicalEffects / QtQuick.Effects: each wraps a
		// `source` item and post-processes it. We cannot run their shaders,
		// so they render the source unshaded rather than dropping it.
		"FastBlur", "GaussianBlur", "RecursiveBlur", "MaskedBlur",
		"RadialBlur", "DirectionalBlur", "ZoomBlur", "Glow", "DropShadow",
		"InnerShadow", "ColorOverlay", "Colorize", "Desaturate", "Blend",
		"BrightnessContrast", "HueSaturation", "LevelAdjust", "Displace",
		"OpacityMask", "ThresholdMask", "GammaAdjust", "MultiEffect":
		return buildShaderEffect(n, env, doc)
	case "FileDialog", "FolderDialog":
		// Qt hands these to the desktop; ours is an in-app browser panel.
		return buildFileDialog(n, env, doc)
	case "ScrollBar":
		return buildScrollBar(n, env, doc)
	case "Dial":
		return buildDial(n, env, doc)
	case "CheckBox":
		return buildCheckbox(n, env, doc)
	case "Switch":
		return buildSwitch(n, env, doc)
	case "RadioButton":
		return buildRadioButton(n, env, doc)
	case "ProgressBar":
		return buildProgressBar(n, env, doc)
	case "BusyIndicator":
		return buildBusyIndicator(n, env, doc)
	case "SpinBox":
		return buildSpinBox(n, env, doc)
	case "ComboBox":
		return buildComboBox(n, env, doc)
	case "Slider":
		return buildSlider(n, env, doc)
	case "RangeSlider":
		// One track, two handles bounding a range (see rangeslider.go).
		return buildRangeSlider(n, env, doc)
	case "TextField", "TextInput", "TextEdit", "TextArea":
		return buildTextField(n, env, doc)
	case "ScrollView", "Flickable":
		return buildScroll(n, env, doc)
	case "ListView":
		return buildList(n, env, doc)
	case "GridView":
		return buildGridView(n, env, doc)
	case "TableView":
		return buildTableView(n, env, doc)
	case "HorizontalHeaderView":
		return buildHeaderView(n, env, doc)
	case "TreeView":
		return buildTreeView(n, env, doc)
	case "Loader":
		return buildLoader(n, env, doc)
	case "Spacer":
		// extension: a gap. With an explicit width/height it's a FIXED gap;
		// otherwise it grows to fill the free main-axis space.
		sp := ui.Div()
		fixed := false
		for _, a := range n.attrs {
			switch a.name {
			case "width":
				if f, err := attrFloat(a, env); err == nil {
					sp.W(f)
					fixed = true
				}
			case "height":
				if f, err := attrFloat(a, env); err == nil {
					sp.H(f)
					fixed = true
				}
			}
		}
		if !fixed {
			sp.Grow()
		}
		return sp, nil
	case "Repeater":
		// A bare Repeater (not inside a box's child list) wraps its rows in
		// a Column; inside a Row/Column it is expanded inline by buildChildren.
		els, err := buildRepeater(n, env, doc)
		if err != nil {
			return nil, err
		}
		return ui.Div().Col().Children(els...), nil
	}
	if kind, ok := isTemplateType(n, n); ok {
		// QtQuick.Templates control (a style file's root, e.g. T.Button).
		return buildTemplateControl(kind, n, env, doc)
	}
	// A NESTED Templates control: the alias lives on the file's root node,
	// so a `T.ItemDelegate` used as a style's delegate — built later, on its
	// own, when a ComboBox opens its popup — has to resolve it through the
	// file being built. Without this the delegate fell through as an unknown
	// dotted type and measured nothing, so an open popup was a 4px sliver.
	if doc.curFile != nil && doc.curFile != n {
		if kind, ok := isTemplateType(n, doc.curFile); ok {
			return buildTemplateControl(kind, n, env, doc)
		}
	}
	if comp, ok := doc.components[n.typeName]; ok {
		// Inline `component Name: ...` declaration used as an element type.
		return buildFileComponent(n, comp, env, doc)
	}
	if comp := doc.fileComponent(n.typeName); comp != nil {
		return buildFileComponent(n, comp, env, doc)
	}
	return nil, fmt.Errorf("gml:%d: unknown element %q", n.line, n.typeName)
}

// propKey scopes a declared property to the instance being built. The
// document's own properties keep their bare names (rowPrefix is empty
// there); a styled control's style file declares ITS properties per
// instantiation, or five dials would share one `_dialSize` and every
// canvas would paint the last one's arc.
func (doc *docState) propKey(name string) string { return doc.rowPrefix + name }

// docState retains markup-owned state across frames and hot reloads,
// keyed by id / property name.
type docState struct {
	// mdImages are the pictures Markdown text in this document names,
	// relative to the document's directory (markdown.go).
	mdImages *mdrender.Images
	// useSite names the id a page knows a STYLED control by, while that
	// control builds: what it publishes imperatively has to reach the
	// page's object for that id too (see exposeIDMethodsIn).
	useSite useSite
	// trigger is where the element handling the current click sits, for a
	// popup() that opens at its trigger.
	trigger ui.Rect
	// timed counts the things in this document waiting on the clock rather
	// than on a property (a tooltip's delay), so the frame memoization
	// keeps rebuilding while any of them waits.
	timed int
	// tipStates is each item's attached ToolTip: its hover and since when
	// the tip has been wanted (Qt shows it only after `ToolTip.delay`).
	tipStates map[string]*ui.ToolTipState
	// groundDark is how dark the page is, measured from the outermost fill
	// of the last tree built (noteGround) — the palette a popup with no
	// background of its own has to match.
	groundDark bool
	inputs     map[string]*ui.InputState
	// inputSeed is the last bound `text:` value each retained input was
	// seeded with, so a binding that changes underneath an unfocused
	// field refreshes it (QML: the text property setter) while typing
	// in a focused field is never overwritten by a stale binding.
	inputSeed map[string]string
	scrolls   map[string]*ui.ScrollState
	lists     map[string]*ui.ListState
	// layers are the offscreen targets of layered (blurred / frosted)
	// items, by element key: a GPU texture must outlive the per-frame
	// element tree that samples it.
	layers map[string]*ui.LayerState
	// scrollEdges remembers each view's last atYEnd/atYBeginning value, so
	// the change handler fires on the flip and not every frame.
	scrollEdges map[string]bool
	// scenes are the declared View3D scenes, by element key: retained
	// render targets plus the frame the last build described.
	scenes map[string]*qview3D
	// shaderFX are the ShaderEffects that run their own shaders, by element
	// key: the parsed .qsb pair and the pipeline built from it (see
	// shaderfx.go).
	shaderFX map[string]*shaderFXState
	// maps is each Map's retained view (centre and zoom), by element key;
	// mapSeeded remembers which ones took their declared centre already.
	maps      map[string]*mapState
	mapSeeded map[string]bool
	// attrOv holds attribute values assigned through an element's id — an
	// imperative `box.width = 100` or a `property alias` written from a use
	// site. They override the markup on every later build.
	attrOv col[map[string]any]
	// attrOvT holds each override\'s tracking token (see setAttrOverride).
	attrOvT col[map[string]*core.Property[string]]
	// pdfs is each PdfDocument's retained state, by element key.
	pdfs map[string]*pdfState
	// media is each MediaPlayer's retained state, by element key.
	media  map[string]*mediaState
	props  map[string]Ref
	models map[string]*listModel
	images map[string]*ui.ImageCell
	// rasterSizes caches each raster source's header size, for sourceSize.
	rasterSizes map[string][2]float64
	// idKeys maps an id to the registry key its element was last built
	// under. A component file's ids are declared once, document-wide, while
	// its elements are registered per instantiation (prefixed), so this is
	// how an id'd view finds the element it actually belongs to.
	idKeys        ccol[keyID]
	timers        map[string]*gmlTimer
	frameAnims    map[string]*frameAnim
	baseDir       string                            // directory of the source .gml, for resolving Image sources
	settings      *settingsState                    // Settings files loaded, changed (settings.go)
	settingsStore settings.Store                    // where Settings values persist; nil = settings.DefaultStore()
	animPlayers   map[string]*animPlayer            // AnimatedImage players, by element (animatedimage.go)
	dragStates    map[string]*dragTargetState       // MouseArea drags, by element (dragtarget.go)
	buttonGroups  map[string]*buttonGroup           // exclusive button groups, named or automatic (buttongroup.go)
	menuBars      map[string]*ui.MenuBarState       // each MenuBar's open menu and menu states (menu.go)
	menus         map[string]*ui.MenuState          // each standalone Menu's highlight and submenus
	drawers       map[string]*ui.DrawerState        // each Drawer's slide position (drawer.go)
	tumblers      map[string]*ui.TumblerState       // each Tumbler's wheel position (tumbler.go)
	delays        map[string]*ui.DelayState         // each DelayButton's hold progress
	splits        map[string]*ui.SplitState         // each SplitView's dragged handles
	tables        map[string]*ui.TableState         // each TableView's selection and edited cell
	trees         map[string]*ui.TreeState          // each TreeView's expanded nodes and current row
	themeScopes   map[string]*ui.ThemeScope         // each item's own controls theme (theme.go)
	pageTheme     ui.ThemeScope                     // the page's base theme, from its ground (theme.go)
	activities    map[string]*ui.Activity           // each indeterminate ProgressBar's / BusyIndicator's loop
	activitySeen  map[string]bool                   // ... built this frame (the rest are stopped)
	popupStates   map[string]*ui.PopupState         // popup open/closing state and enter/exit values (popuptransitions.go)
	groupOfButton map[string]string                 // button id -> the ButtonGroup that lists it
	stateSeen     map[string]string                 // each item's last active state, to notice a change (state.go)
	stateAnims    map[string][]animSpec             // the transition running since that change
	stateToks     map[string]*core.Property[uint64] // bumped on each change (stateCtx.tok)
	stateCtxs     map[string]*stateCtx              // each item's state scope, updated in place (computeStateCtx)
	stateShown    map[string]float32                // last value shown per id|prop under a state scope (stateTween)
	focusReq      map[keyID]bool                    // forceActiveFocus requests, applied on the next build (focus.go)
	focusRefs     map[keyID]Ref                     // tracked activeFocus, by element (focus.go)
	focusReports  map[keyID]func(bool)              // each tracked element's focus report, made once (focus.go)
	readsFocus    map[*node]bool                    // items whose own attributes read activeFocus (focus.go)
	inputKeys     map[*ui.InputState]keyID          // the element key a text field's state was built under (focus.go)
	inputMasks    map[*ui.InputState]string         // the inputMask each text field has (buildTextField)
	focusObjs     map[keyID]any                     // what activeFocusItem names, by element (focus.go)
	handlerStates map[string]*inputHandlerState     // Wheel/Pinch/PointHandler state (inputhandlers.go)
	pointerSeen   map[string]*[2]float32            // MouseArea: the pointer's last window position (mousearea.go)
	geomWritten   map[geomKey]int                   // frame an x/y/width/height override was written (setAttrOverride)
	lastView      time.Time                         // wall clock of the previous frame, for timers
	lastClock     time.Duration                     // ... and the frame loop's animation clock
	listSeq       int

	// Element geometry for parent/id.width refs. elems collects this frame's
	// elements by key; prevElems is last frame's (already laid out, so its
	// bounds are valid); elemObjs are the id-keyed live views whose geometry
	// reads prevElems.
	elems     ccol[ui.Element]
	prevElems ccol[ui.Element]
	elemObjs  ccol[*elemObject]

	// state holds the active states/transitions scope for the subtree
	// currently being built (nil outside any stateful Item). anims and
	// animTarget retain the per-target tweened values that transitions drive,
	// keyed "id|prop", surviving frames and hot reloads.
	state      *stateCtx
	anims      map[string]*core.Property[float32]
	animTarget map[string]float32

	// animators holds standalone running animations (NumberAnimation{running},
	// keyed by id/line), ticked each frame like timers.
	animators map[string]*gmlAnimator

	// rowScope is the stack of model-row bindings active while building a
	// Repeater/view delegate. Handlers (onClicked, …) capture it so that
	// `index`/`modelData`/role identifiers still resolve when the handler
	// runs — bindRow's build-time scope is long gone by then.
	rowScope []rowBind
	hpRefs   ccol[hoverPress]             // per element key (see hoverPressRefs)
	instToks ccol[*core.Property[uint64]] // component instances' use-site tokens (see instToken)
	rowToks  ccol[*core.Property[uint64]] // delegate rows' tokens (see rowToken)
	// Subtree patching (see noteSlot, patch): the entry build() last
	// produced, the ancestors' scope bindings in force, and a count.
	lastEntry *subtreeEntry
	bindStack []bindLayer
	patched   int
	patching  int     // patch() depth
	captures  int     // effect captures built so far (see slot)
	seenLog   []keyID // keys counted in keySeen during a patch (undone on failure)
	// The key intern table (see keyid.go).
	keyIDs  map[string]keyID
	keyStrs []string
	hereIDs map[string]map[string]keyID // [prefix][id] → id of prefix+id
	// parentObjs are the per-key `parent` objects (see parentObject).
	parentObjs ccol[*elemObject]
	// scopeSnaps memoizes Env.snapshot per site (see scopeSnapshot).
	scopeSnaps  map[string]*scopeSnap
	invalidates int // invalidateCache count, part of the scope generation

	// components maps a `Component { id: x; <child> }` id to its single child,
	// so Loader { sourceComponent: x } can instantiate it.
	components map[string]*node

	// svgs caches parsed vector Image sources (nil = known-bad, logged once).
	svgs map[string]*render.SVG

	// fileComps caches QML-style file components resolved from baseDir
	// (<Type>.gml; nil = known-missing).
	fileComps map[string]*node
	// fileDirs is the directory each loaded component file came from, so a
	// Loader source inside it resolves against its own directory.
	fileDirs    map[*node]string
	fileDialogs map[string]*fileDialogState
	painted     map[string]*paintedInst
	nodesByID   map[string]*node
	capturing   map[string]bool
	particles   map[string]*liveParticleSystem
	// particleParts holds the emitters/painters/affectors declared OUTSIDE
	// their ParticleSystem, keyed by the id their `system:` names
	// (collected at load time; see collectParticleParts).
	particleParts map[string][]*node
	// partsVer is bumped when one registers, and READ while a system
	// builds: the per-subtree cache reuses a subtree whose read-set is
	// unchanged, so without it a system that built before its siblings
	// registered never rebuilt to see them.
	partsVer   core.Property[int]
	seriesData map[string]*seriesData

	// lenient (set for .qml documents) logs-and-skips children whose build
	// fails instead of failing the whole document — Qt's runtime warns and
	// renders, and third-party QML should degrade the same way. Authored
	// .gml stays strict.
	lenient bool

	// propsRaw holds each declared property's unwrapped Ref (props holds
	// the assignment-tracking wrapper); assigned records QML "broken
	// bindings": once a handler assigns a property imperatively, its
	// default expression stops re-evaluating.
	propsRaw map[string]Ref
	assigned map[string]bool

	// pendingProps queues property defaults whose first evaluation failed
	// (forward references to ids/properties declared later in the file);
	// retryPendingProps re-evaluates them once declaration completes.
	pendingProps []pendingProp
	// declParent is the item being descended into during the declaration
	// walk, recorded on each id so `someId.parent` resolves.
	declParent *elemObject
	// declParentKey is that item's element-registry key, which stays valid
	// after the declaration pass (the object's element does not).
	declParentKey keyID
	// propRetries counts consecutive frames an INSTANCE-scoped property
	// default has failed to resolve. A default naming an element declared
	// later in the document cannot resolve on the first frame but resolves
	// on the next one, so only a default that keeps failing is a real
	// problem worth logging.
	propRetries map[string]int

	// Module imports (module.go): modules already resolved, and module
	// component types mapped to their .qml files.
	modulesSeen map[string]bool
	moduleTypes map[string]string
	// moduleTypesBy is moduleTypes split per module name, and curFile is
	// the file whose tree is building — together they answer "does THIS
	// file's import list shadow this builtin type?", since QML imports are
	// per file (a page importing a style module does not restyle the
	// separate .qml files it instantiates).
	// instXY holds a file-component instance's own x/y, resolved inside the
	// component's scope (see noteInstanceXY) for the enclosing anchor
	// container to position it by.
	instXY map[string]instPos
	// inPositioner says the element being built is a child of a positioner
	// or a layout — something that places it — rather than of an anchor
	// container. A positioner leaves the CROSS axis free, so a child's
	// cross-axis anchor (`anchors.horizontalCenter` inside a Column) is
	// the only thing that decides where it sits across the line.
	inPositioner bool
	// layoutRow is the direction of the layout currently building its
	// children: it decides whether Layout.fillWidth means "grow" (row) or
	// "stretch the cross axis" (column). layoutGrid overrides it for grid
	// cells, which size themselves.
	layoutRow  bool
	layoutGrid bool
	// inLayout: the children being built belong to a QtQuick.Layouts
	// layout, whose items share the space and compress when it runs short
	// (a plain Row just overflows).
	inLayout      bool
	moduleTypesBy map[string]map[string]string
	curFile       *node
	// styleModule is the application-wide Controls style from
	// qtquickcontrols2.conf (`Style=qmlsnippetsstyle`), which restyles every
	// file, unlike a per-file import. styleChecked guards the one lookup.
	styleModule  string
	styleChecked bool

	// nativeInsts holds live Go-registered type instances by element id
	// (native.go); they persist across per-frame rebuilds.
	nativeInsts map[string]*nativeInst

	// connections maps a target id → signal handler name (e.g. "onClicked") →
	// the full handler declaration (params + body), from
	// Connections { target: x; function onClicked(a, b){} }.
	connections map[string]map[string]funcDecl

	// synthRefs holds engine-owned persistent property refs that have no
	// `property` declaration in markup — e.g. a TabBar's implicit currentIndex.
	// Keyed "elemKey|prop" so the backing value survives frames and reloads.
	synthRefs map[string]Ref

	// navStacks holds StackView runtime stacks (push/pop/replace), keyed by the
	// StackView's elemKey so navigation state persists across frames.
	navStacks map[string]*navStack

	// slides holds SwipeView page-change transitions (tweened 1→0 progress),
	// keyed by the view's elemKey.
	slides map[string]*slideState

	// Frame memoization (dirty-tracking): the engine rebuilds the whole tree
	// each frame, so when no observable input changed it can return the previous
	// tree untouched. cachedEl is that tree; obsRev/geoSig are the signatures it
	// was built against — a property change (core.Revision), a scroll offset
	// change, or a layout-geometry change since then forces a rebuild.
	cachedEl ui.Element
	obsRev   uint64
	geoSig   uint64
	geoKnown bool // geoSig was measured for the cached tree (see View)
	haveSigs bool

	// Per-subtree memoization (per-binding dirty-tracking): when the whole-
	// document gate above DOES rebuild, subtrees whose recorded property
	// read-set is unchanged are reused instead of re-evaluated, so a single
	// property change re-runs only its dependent subtrees. Keyed like
	// doc.elems; entries guard on node+env identity (a component node
	// instantiated twice, or a per-build env, simply never caches).
	// capStack collects each in-flight build's element registrations so a
	// reused subtree can replay them into doc.elems without being walked.
	subtrees                                        ccol[*subtreeEntry]
	geoTok                                          ccol[*core.Property[uint64]]          // geometry tokens by element key (see geoToken)
	frameNo, stampedAt                              int                                   // View() counts frames; stampTokens runs once per frame
	keySeen                                         col[int]                              // per-frame count of builds per cache key (see build)
	tplObjs                                         map[string]*elemObject                // styled controls' shadow objects, kept across builds (see seedTemplateScope)
	propTok                                         map[propTokKey]*core.Property[string] // exposed-value tokens by key+name (see elemObject.setProp)
	rowDepth                                        int                                   // delegate row scopes we are inside (see build)
	rowUntracked                                    int                                   // ... of which could not be hashed (rowtoken.go)
	rowSeen                                         []keyCount                            // keySeen counts to restore when a row ends (see build)
	cacheHits                                       int                                   // subtree cache hits this build (GFX_BUILD_STATS)
	missAbsent, missEnv, missUntracked, missStamped int                                   // GFX_BUILD_STATS miss reasons
	capStack                                        []*subtreeEntry
	freshBuilds                                     int // buildFresh call count (per-subtree cache effectiveness)

	// propReads memoizes each live declared-property default's read-set so
	// rebindProps skips bindings whose inputs are unchanged.
	propReads map[string]*core.ReadSet

	// wired tracks one-shot signal wiring on persistent refs (Slider
	// onValueChanged etc.) so per-frame rebuilds don't stack handlers.
	wired map[string]bool

	// builds counts the builds this document has done. The completion
	// handlers wait for the second one, where last frame's geometry is
	// available — QML sizes its item tree during instantiation, so a
	// handler reads geometry as freely as anything else there, and ours
	// has none until a tree has been laid out. collected says the first
	// build has queued its handlers, so the second must not queue them
	// again.
	builds    int
	collected bool

	// Component.onCompleted handlers: collected during the first build and run
	// once, after the tree is constructed (QML component-completion semantics).
	completed   bool
	onCompleted []completedTask

	// curBehaviors/curKey carry the Behavior-on-<prop> specs for the element
	// currently in boxAttrs, so numAttr can tween its own properties on change.
	curBehaviors map[string]behaviorSpec
	curKey       string

	// tableModels holds declared TableModel objects by id (tableview.go);
	// tableInfo/tableNodes let a HorizontalHeaderView resolve its syncView's
	// columns whichever of the two builds first.
	tableModels map[string]*tableModel
	tableInfo   map[string]tableInfo
	tableNodes  map[string]*node

	// outerID is the id a style instantiation shadowed (`root`), with what
	// it meant outside — so a delegate the use site handed in can be built
	// in its own scope (see docState.buildUseSite).
	outerID outerBinding

	// rowPrefix scopes element state to the model row currently being built
	// ("" outside any delegate). Delegates instantiate once per row, so
	// everything keyed by element identity — ids, declared properties,
	// hover/press state, geometry, behaviors — must fork per row or all rows
	// share one instance. Set by enterRow (model.go), consumed by doc.key
	// and the row-scope declaration pass. Nested delegates accumulate.
	rowPrefix string
}

// subtreeEntry is one cached subtree build: the element, the property
// read-set it was built against, and the doc.elems registrations made while
// building it (self + descendants), replayed on reuse.
type subtreeEntry struct {
	node *node
	env  *Env
	el   ui.Element
	rs   *core.ReadSet
	regs []elemReg       // registrations made directly by this build
	subs []*subtreeEntry // nested subtrees (built or reused under it)
	// slots are the children this build placed through a path that can
	// be re-run on its own (see noteSlot / patch): the entry each one
	// produced, the element it ended up as inside el, and how to redo it.
	slots []*slot
}

// slot is one child placed by a subtree build: the entry the placement
// produced, the element it ended up as inside the parent, the reads the
// PLACEMENT itself made (anchors parsed off the child, the wrapper it was
// given — nested under the parent's read-set as one unit, so a child whose
// geometry moves stales the slot rather than the parent's own reads), and
// how to redo it.
type slot struct {
	sub     *subtreeEntry
	placed  ui.Element
	prs     *core.ReadSet
	rebuild func() (ui.Element, error)
	ctx     buildCtx
}

// buildCtx is what a child's build reads from the document state and the
// env beyond its own node: the scope its ancestors bound around it. It is
// captured when the child is placed and re-established to rebuild the
// child alone — the parent keeps its element and its own scope work.
type buildCtx struct {
	rowPrefix           string
	useSite             useSite
	outerID             outerBinding
	curFile             *node
	layoutRow, inLayout bool
	inPositioner        bool
	layoutGrid          bool
	state               *stateCtx
	curBehaviors        map[string]behaviorSpec
	curKey              string
	declParent          *elemObject
	declParentKey       keyID
	binds               []bindLayer // ancestors' scope bindings, outermost first
	// overrides are names whose LIVE value at capture time differed from
	// what the bindings say: an ancestor re-pointed a name after binding
	// it (a use-site id at its control's scope object).
	overrides map[string]any
	parent    any
	hasParent bool
}

// captureCtx snapshots the build context for a slot.
func (doc *docState) captureCtx(env *Env) buildCtx {
	c := buildCtx{
		rowPrefix: doc.rowPrefix, useSite: doc.useSite, outerID: doc.outerID, curFile: doc.curFile,
		layoutRow: doc.layoutRow, inLayout: doc.inLayout, inPositioner: doc.inPositioner,
		layoutGrid: doc.layoutGrid, state: doc.state, curBehaviors: doc.curBehaviors, curKey: doc.curKey,
		declParent: doc.declParent, declParentKey: doc.declParentKey,
	}
	if len(doc.bindStack) > 0 {
		c.binds = append([]bindLayer(nil), doc.bindStack...)
		for _, l := range c.binds {
			if !l.ids {
				continue // contract values and geometry are never re-pointed
			}
			for name, bound := range l.vars {
				if live, ok := env.vars[name]; ok && !identical(live, bound) {
					if c.overrides == nil {
						c.overrides = map[string]any{}
					}
					c.overrides[name] = live
				}
			}
		}
	}
	c.parent, c.hasParent = env.lookup("parent")
	return c
}

// identical reports whether two scope values are the same value: equal
// scalars, the same pointer or Ref, the same map or slice (by identity;
// maps and slices are not comparable with ==).
func identical(a, b any) (same bool) {
	// A value holding an uncomparable field (a Ref wrapping a struct with
	// a func) panics under ==; that is "not the same value" here.
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	if ta == nil {
		return true
	}
	switch ta.Kind() {
	case reflect.Map, reflect.Slice, reflect.Func:
		return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
	}
	return a == b
}

// refreshObj re-points a captured scope object at last frame's element,
// the way beginFrame does for the registry's objects: a parent object or
// a control's scope object made for an earlier frame would otherwise read
// stale geometry.
func (doc *docState) refreshObj(v any) {
	o, ok := v.(*elemObject)
	if !ok || o == nil || o.doc != doc || o.key == 0 {
		return
	}
	key := o.key
	if k, ok := doc.idKeys.get2(key); ok {
		key = k
	}
	if el := doc.prevElems.get(key); el != nil {
		o.el = el
	}
}

// withCtx runs fn with ctx in force: the document fields, the ancestors'
// bindings (bound into env outermost first, so inner ones win, exactly
// as they were), and `parent`. Everything is restored after.
func (doc *docState) withCtx(ctx buildCtx, env *Env, fn func()) {
	saved := doc.captureCtx(env)
	apply := func(c *buildCtx) {
		doc.rowPrefix, doc.useSite, doc.outerID, doc.curFile = c.rowPrefix, c.useSite, c.outerID, c.curFile
		doc.layoutRow, doc.inLayout, doc.inPositioner = c.layoutRow, c.inLayout, c.inPositioner
		doc.layoutGrid, doc.state, doc.curBehaviors, doc.curKey = c.layoutGrid, c.state, c.curBehaviors, c.curKey
		doc.declParent, doc.declParentKey = c.declParent, c.declParentKey
	}
	apply(&ctx)
	// The names are bound straight into env and undone from one pooled
	// list, outermost layer first so inner ones win, exactly as they were.
	undo := takeUndo(64)
	// Deliberately does NOT touch the derived marks (see Env.derived).
	// Carrying them through was tried and measured: it changed no page's
	// rebuild count and cost a fifth of the buttons page's frame, because
	// this runs for every name every ancestor bound on every slot a patch
	// redoes. A re-established binding keeps whatever status the live
	// scope gives it, as it always has.
	bind := func(name string, v any) {
		prev, ok := env.vars[name]
		undo = append(undo, scopeSaved{name: name, v: prev, ok: ok})
		env.vars[name] = v
	}
	for _, l := range ctx.binds {
		for name, v := range l.vars {
			bind(name, v)
		}
	}
	for name, v := range ctx.overrides {
		doc.refreshObj(v)
		bind(name, v)
	}
	if ctx.hasParent {
		doc.refreshObj(ctx.parent)
		bind("parent", ctx.parent)
	}
	fn()
	for i := len(undo) - 1; i >= 0; i-- {
		u := undo[i]
		if u.ok {
			env.vars[u.name] = u.v
		} else {
			delete(env.vars, u.name)
		}
	}
	putUndo(undo)
	apply(&saved)
}

// pushBinds records a set of names an ancestor bound into env for the
// duration of its build (an instance's ids and properties, a control's
// contract, an item's own geometry), so a child placed under it can be
// rebuilt alone with the same names in scope. Returns the pop.
//
// ids marks a layer that binds IDS (an instance's, a style root's): those
// are the names a later write may re-point (a use-site id at its control's
// scope object), so a slot compares them against the live env when it is
// captured. A control's contract and an item's geometry never are.
func (doc *docState) pushBinds(m map[string]any, ids bool) func() {
	if noPatch || m == nil {
		return noRestore // nothing records slots, so nothing reads the stack
	}
	doc.bindStack = append(doc.bindStack, bindLayer{vars: m, ids: ids})
	return func() { doc.bindStack = doc.bindStack[:len(doc.bindStack)-1] }
}

// bindLayer is one ancestor's set of bound names (see pushBinds).
type bindLayer struct {
	vars map[string]any
	ids  bool
}

// slot runs one child placement — a build and whatever the parent wraps
// around it — as a unit the parent can later redo on its own (see patch):
// its reads are tracked as one nested set, and the element it produced,
// the entry it came from and the context it ran in are recorded on the
// subtree being built. Where nothing is being built (a delegate row, an
// effect capture), it is just the placement.
func (doc *docState) slot(env *Env, place func() (ui.Element, error)) (ui.Element, error) {
	var placed ui.Element
	var err error
	n := len(doc.capStack)
	if n == 0 || noPatch {
		return place()
	}
	// The context is captured BEFORE the placement runs: it is what the
	// parent had in force when it placed the child, not whatever the
	// child's own build left behind.
	ctx := doc.captureCtx(env)
	doc.lastEntry = nil
	captures := doc.captures
	prs := core.TrackReads(func() { placed, err = place() })
	sub := doc.lastEntry
	doc.lastEntry = nil
	if err != nil || placed == nil || sub == nil {
		return placed, err
	}
	if doc.captures != captures {
		// The placement built an effect's copy of another item. The copy
		// reads that item's geometry through the item's own tokens but is
		// laid out inside the effect, so a patched copy could sit where an
		// earlier frame put it; this chain rebuilds as before.
		return placed, err
	}
	if sub.el != placed {
		inside := false
		for _, c := range ui.Children(placed) {
			if c == sub.el {
				inside = true
			}
		}
		if !inside {
			return placed, err
		}
	}
	top := doc.capStack[n-1]
	top.slots = append(top.slots, &slot{sub: sub, placed: placed, prs: prs, rebuild: place, ctx: ctx})
	if buildTrace {
		fmt.Fprintf(os.Stderr, "gml: slot %s under %s\n", doc.keyStr(doc.keyID(sub.node)), doc.keyStr(doc.keyID(top.node)))
	}
	return placed, err
}

// patch recomputes the stale children of a cached subtree in place and
// reports whether it could: every stale nested entry has to be one of the
// entry's slots, and each rebuild has to land where the old child was.
func (doc *docState) patch(e *subtreeEntry, env *Env) bool {
	// Every stale nested read-set must belong to a slot's entry: a nested
	// computation that is not a placed child (a property binding tracked
	// inside the build, a delegate built by a control's own path) means
	// the parent has to run again. Decided BEFORE anything is rebuilt, so
	// a bail-out costs nothing and leaves no half-done state.
	stale := 0
	ok := true
	e.rs.EachSub(func(sub *core.ReadSet) {
		if !ok || !sub.Stale() {
			return
		}
		stale++
		for _, sl := range e.slots {
			if sl.prs == sub {
				return
			}
		}
		ok = false
		if buildTrace {
			fmt.Fprintf(os.Stderr, "gml: patch %s: a stale nested computation is not a placed child (%s)\n", doc.keyStr(doc.keyID(e.node)), sub.Summary())
		}
	})
	if !ok || stale == 0 {
		return false
	}
	// Rebuild each stale slot alone: no enclosing capture (the new entry
	// is linked into e by hand), the slot's context in force. The repeat
	// counter is restored if this fails: the children rebuilt here would
	// otherwise be numbered as repeats when the parent rebuilds them again.
	savedStack := doc.capStack
	doc.capStack = nil
	logStart := len(doc.seenLog)
	doc.patching++
	failed := true
	defer func() {
		doc.capStack = savedStack
		doc.patching--
		if failed {
			for _, k := range doc.seenLog[logStart:] {
				doc.keySeen.set(k, doc.keySeen.get(k)-1)
			}
		}
		doc.seenLog = doc.seenLog[:logStart]
	}()
	for _, sl := range e.slots {
		if !sl.prs.Stale() {
			continue
		}
		var placed ui.Element
		var err error
		var prs *core.ReadSet
		doc.lastEntry = nil
		doc.withCtx(sl.ctx, env, func() {
			prs = core.TrackReads(func() { placed, err = sl.rebuild() })
		})
		repl := doc.lastEntry
		doc.lastEntry = nil
		if err != nil || placed == nil || repl == nil {
			if buildTrace {
				fmt.Fprintf(os.Stderr, "gml: patch %s: rebuild of %s failed (err=%v)\n", doc.keyStr(doc.keyID(e.node)), doc.keyStr(doc.keyID(sl.sub.node)), err)
			}
			return false
		}
		if repl == sl.sub {
			// The child patched ITSELF in place: same entry, same element,
			// fresh again. Only its placement wrapper (an anchored child's)
			// can be new, and that is swapped.
			if placed != sl.placed {
				if !ui.ReplaceChild(e.el, sl.placed, placed) {
					return false
				}
				sl.placed = placed
			}
			if !e.rs.ReplaceSub(sl.prs, prs) {
				return false
			}
			sl.prs = prs
			continue
		}
		if sl.placed == e.el {
			// The child IS this entry's element (a use site whose element
			// is its instance) and came back as a NEW element: the entry's
			// own build finished the old one (layout hints, registration),
			// and that work is not redone here. Rebuild the whole entry.
			return false
		}
		if !ui.ReplaceChild(e.el, sl.placed, placed) {
			if buildTrace {
				fmt.Fprintf(os.Stderr, "gml: patch %s: old child element not found under the parent\n", doc.keyStr(doc.keyID(e.node)))
			}
			return false
		}
		for i, sub := range e.subs {
			if sub == sl.sub {
				e.subs[i] = repl
			}
		}
		if !e.rs.ReplaceSub(sl.prs, prs) {
			return false
		}
		sl.sub, sl.placed, sl.prs = repl, placed, prs
		doc.patched++
	}
	e.rs.Invalidate()
	if e.rs.Stale() {
		if buildTrace {
			fmt.Fprintf(os.Stderr, "gml: patch %s: still stale after patching\n", doc.keyStr(doc.keyID(e.node)))
		}
		return false
	}
	if buildTrace {
		fmt.Fprintf(os.Stderr, "gml: patched %s\n", doc.keyStr(doc.keyID(e.node)))
	}
	failed = false
	return true
}

// noRowCache puts delegate rows back outside the subtree cache
// (GFX_NO_ROW_CACHE=1) — what the engine did before rows carried a token,
// and the ground truth a suspected stale row is compared against.
var noRowCache = os.Getenv("GFX_NO_ROW_CACHE") != ""

// noPatch disables subtree patching (GFX_NO_PATCH=1): every stale
// ancestor rebuilds, as before. On by default: every corpus page verifies
// against a from-scratch build (GFX_PATCH_VERIFY=1) up to the verifier's
// own artifacts, which a build without patching shows too.
var noPatch = os.Getenv("GFX_NO_PATCH") != ""

type elemReg struct {
	key keyID
	el  ui.Element
}

// key returns n's state key scoped to the model row currently being built:
// the plain elemKey at top level, and rowPrefix+elemKey inside a delegate so
// each row owns its state (QML instantiates a delegate per row; ids and
// per-element state are component-scoped, never shared across rows).
func (doc *docState) key(n *node) string {
	if doc.rowPrefix == "" {
		return elemKey(n)
	}
	// Cached per node for the prefix it was last asked under: the key is
	// asked for several times per node per build.
	if n.kprefix == doc.rowPrefix && n.kfull != "" {
		return n.kfull
	}
	n.kprefix, n.kfull = doc.rowPrefix, doc.rowPrefix+elemKey(n)
	return n.kfull
}

// behaviorSpec is a `Behavior on <prop>` animation: how a property change is
// tweened (duration + easing) instead of snapping.
type behaviorSpec struct {
	dur    time.Duration
	easing core.Easing
}

// completedTask is a Component.onCompleted handler captured with the env scope
// it was declared in.
type completedTask struct {
	stmts []stmt
	env   *Env
	// self is the id-registry key of the element the handler was declared
	// on, when it has one: QML resolves a bare name in a handler against
	// its own object first (`Component.onCompleted: openDatabase()` calls
	// the enclosing element's method). It carries the row prefix, since a
	// component instance's ids are scoped.
	self string
	// node is the element the handler was declared on, so its OWN control
	// state and derived properties can be bound by bare name too — QML's
	// item scope, which an id lookup alone does not cover.
	node *node
}

// collectCompleted queues an element's Component.onCompleted handlers,
// keying them by the id registry entry for THIS instance so a bare call in
// the handler resolves the element's own methods. Returns the node with the
// lifecycle attributes stripped.
func (doc *docState) collectCompleted(n *node, env *Env) *node {
	out, tasks := extractComponentHandlers(n, env)
	if len(tasks) == 0 || doc.collected || doc.completed {
		return out
	}
	for i := range tasks {
		if tasks[i].self != "" {
			tasks[i].self = doc.rowPrefix + tasks[i].self
		}
		// QML resolves a bare name in a handler against the element's OWN
		// properties — a ComboBox reading `currentValue` in its
		// onCompleted, with or without an id. Bind its state and derived
		// values live, so they read at run time (after the control builds).
		//
		// The scope is FLATTENED here: the handler runs after this build
		// returns, by which point per-instance bindings (a styled control's
		// delegate ids, a component instance's own scope) have been
		// restored — the live chain would no longer resolve them.
		tasks[i].env = selfPropScope(n, tasks[i].env.snapshot(), doc)
	}
	doc.onCompleted = append(doc.onCompleted, tasks...)
	return out
}

// selfPropScope binds an element's own state and derived properties by bare
// name, for a handler on an element with no id. Values stay live: state is
// the control's ref, and a derived value is read through deriveRef when the
// handler actually runs (after the control has built).
func selfPropScope(n *node, env *Env, doc *docState) *Env {
	kind := bareTypeName(n.typeName)
	names := stateAttrs[kind]
	derive := comboDeriveNames(kind)
	if len(names) == 0 && len(derive) == 0 && kind != "Canvas" {
		return env
	}
	scope := env.child()
	// A Canvas resolves its own repaint API bare, wherever it is declared
	// (`Component.onCompleted: requestPaint()`).
	seedCanvasMethods(n, scope, &elemObject{props: map[string]any{}})
	for name, kind := range names {
		key := doc.key(n) + "." + name
		if kind == "bool" {
			scope.vars[name] = doc.boolProp(key, false)
		} else {
			scope.vars[name] = doc.floatProp(key, 0)
		}
	}
	if len(derive) > 0 {
		fn := comboDerive(n, env, doc)
		for _, name := range derive {
			scope.vars[name] = deriveRef{fn: fn, name: name}
		}
	}
	return scope
}

// comboDeriveNames are the read-only properties a ComboBox exposes bare in
// its own handlers.
func comboDeriveNames(kind string) []string {
	if kind != "ComboBox" {
		return nil
	}
	return []string{"currentText", "displayText", "currentValue", "count", "editText"}
}

// bareTypeName drops a Templates namespace ("T.ComboBox" -> "ComboBox"), so
// a styled control is recognized as the control it is.
func bareTypeName(t string) string {
	if i := strings.LastIndexByte(t, '.'); i >= 0 {
		return t[i+1:]
	}
	return t
}

// deriveRef reads one computed control property on demand. It is read-only:
// a write is ignored, as QML's read-only properties are.
type deriveRef struct {
	fn   func(string) (any, bool)
	name string
}

func (r deriveRef) get() any {
	if v, ok := r.fn(r.name); ok {
		return v
	}
	return nil
}
func (r deriveRef) set(any) error                         { return nil }
func (r deriveRef) animate(to any, d time.Duration) error { return nil }
func (r deriveRef) onChange(func())                       {}

// selfScope returns the scope a completed-handler runs in: the captured
// env, plus the declaring element's own methods and properties bound by
// bare name. Resolved at RUN time, because the object (a Go backend's
// instance, say) is created during the build that follows collection.
func (doc *docState) selfScope(t completedTask) *Env {
	// The handler's scope was SNAPSHOT when the build queued it, so the id
	// objects in it are that build's. A file component instantiates fresh
	// objects on every build, and only the live ones are re-pointed at
	// last frame's elements (see beginFrame) — so a handler holding the
	// old ones read every geometry as zero, which is how the blueprint
	// viewer's `fitToView()` came to divide by a zero width.
	doc.refreshScopeGeometry(t.env)
	if t.self == "" {
		return t.env
	}
	// The id registry is authoritative: a file component's declared
	// property of the same name is document-global here, and would
	// otherwise shadow the element the handler belongs to.
	eo := doc.elemObjs.get(doc.intern(t.self))
	if eo == nil {
		// (A missing id in the registry is a typed nil, so look it up
		// directly rather than through an `any`.)
		if v, ok := t.env.lookup(t.self); ok {
			eo, _ = v.(*elemObject)
		}
	}
	if eo == nil || (len(eo.methods) == 0 && len(eo.props) == 0 && eo.input == nil) {
		return t.env
	}
	scope := t.env.child()
	for name, fn := range eo.methods {
		scope.vars[name] = fn
	}
	for name, v := range eo.props {
		if _, taken := scope.vars[name]; !taken {
			scope.vars[name] = v
		}
	}
	// A text field's own content is in its handlers' scope, bare — QML's
	// item scope (`Component.onCompleted: parseInput(text)` on a TextEdit).
	if in := eo.input; in != nil {
		if _, taken := scope.vars["text"]; !taken {
			scope.vars["text"] = propRef[string]{&in.Text,
				func(v any) (string, error) { return str(v), nil }, nil}
		}
	}
	return scope
}

// trackedRef wraps a declared property so imperative sets mark its
// default binding broken (QML semantics); binding re-evaluation writes
// through the raw ref underneath instead.
type trackedRef struct {
	Ref
	doc  *docState
	name string
}

func (t trackedRef) set(v any) error {
	t.doc.assigned[t.name] = true
	return t.Ref.set(v)
}

// unwrap exposes the wrapped raw ref so widgets that need the concrete
// core.Property underneath (CheckBox, Slider) can reach it.
func (t trackedRef) unwrap() Ref { return t.Ref }

// propOf digs through decorator refs (trackedRef) to the concrete
// propRef[T] a widget must bind against, and returns the declared-property
// name whose binding a user-driven write should break ("" when the ref is
// not a document property). Widgets write straight to the core.Property,
// so they cannot go through trackedRef.set — they mark the break in their
// own interaction callback instead.
func propOf[T comparable](r Ref) (propRef[T], string, bool) {
	broke := ""
	for r != nil {
		if pr, ok := r.(propRef[T]); ok {
			return pr, broke, true
		}
		if t, ok := r.(trackedRef); ok {
			broke = t.name
		}
		u, ok := r.(interface{ unwrap() Ref })
		if !ok {
			break
		}
		r = u.unwrap()
	}
	var zero propRef[T]
	return zero, "", false
}

// rebindProps re-evaluates every root-document declared property whose
// binding is still live (never imperatively assigned) — QML bindings.
// Runs once per frame before the cache check; a changed value bumps the
// revision, so dependent chains settle over successive frames. (File
// components re-bind through declareProps at their per-build call.)
// Each default's read-set is recorded so unchanged bindings are skipped —
// an idle document does O(props) stamp compares instead of O(props) evals.
func rebindProps(n *node, env *Env, doc *docState) {
	// A default may read this item's own geometry (`property real s:
	// Math.min(width, height)`), so bind it for the re-evaluation pass.
	defer shadowSelfGeometry(n, env, doc)()
	for _, d := range n.props {
		key := doc.propKey(d.name)
		if d.typ == "alias" || doc.assigned[key] {
			continue
		}
		if _, isElem := d.def.(*elemExpr); isElem {
			continue // element defaults (FontLoader) are one-shot
		}
		raw, has := doc.propsRaw[key]
		if !has {
			continue
		}
		if rs, ok := doc.propReads[key]; ok && !rs.Stale() {
			continue
		}
		var v any
		var err error
		rs := core.TrackReads(func() { v, err = d.def.eval(env) })
		if err != nil {
			continue
		}
		doc.propReads[key] = rs
		if str(raw.get()) != str(v) {
			_ = raw.set(v)
		}
	}
	for _, c := range n.children {
		rebindProps(c, env, doc)
	}
}

func newDocState() *docState {
	return &docState{
		inputs:      map[string]*ui.InputState{},
		inputSeed:   map[string]string{},
		scrolls:     map[string]*ui.ScrollState{},
		lists:       map[string]*ui.ListState{},
		layers:      map[string]*ui.LayerState{},
		scrollEdges: map[string]bool{},
		props:       map[string]Ref{},
		propsRaw:    map[string]Ref{},
		assigned:    map[string]bool{},
		models:      map[string]*listModel{},
		images:      map[string]*ui.ImageCell{},
		timers:      map[string]*gmlTimer{},
		frameAnims:  map[string]*frameAnim{},
		anims:       map[string]*core.Property[float32]{},
		animTarget:  map[string]float32{},
		animators:   map[string]*gmlAnimator{},
		components:  map[string]*node{},
		connections: map[string]map[string]funcDecl{},
		synthRefs:   map[string]Ref{},
		navStacks:   map[string]*navStack{},
		slides:      map[string]*slideState{},
		svgs:        map[string]*render.SVG{},
		tableModels: map[string]*tableModel{},
		tableInfo:   map[string]tableInfo{},
		tableNodes:  map[string]*node{},
		propReads:   map[string]*core.ReadSet{},
	}
}

// wireOnce reports whether key's one-shot wiring should happen now (first
// call true, later calls false).
func (doc *docState) wireOnce(key string) bool {
	if doc.wired == nil {
		doc.wired = map[string]bool{}
	}
	if doc.wired[key] {
		return false
	}
	doc.wired[key] = true
	return true
}

// intProp returns a persistent engine-owned int property ref for key, creating
// it (with the given default) on first use. Used for implicit control state
// like TabBar.currentIndex that markup never declares but binds against.
func (doc *docState) intProp(key string, def int) Ref {
	if r, ok := doc.synthRefs[key]; ok {
		return r
	}
	r, _ := newDeclaredProp("int", def)
	doc.synthRefs[key] = r
	return r
}

// registerConnections records Connections { target: id; function onSig(){} }
// handlers so the target element's signal also runs them.
func (doc *docState) registerConnections(n *node) {
	if n.typeName == "Connections" {
		target := connectionsTargetID(n)
		if target != "" {
			if doc.connections[target] == nil {
				doc.connections[target] = map[string]funcDecl{}
			}
			for _, fn := range n.funcs {
				doc.connections[target][fn.name] = fn
			}
			// The Qt 5 form, `onClicked: { … }`: a handler attribute with
			// no parameter list (a declaration of the function form wins).
			for _, a := range n.attrs {
				if len(a.name) > 2 && strings.HasPrefix(a.name, "on") && a.stmt != nil {
					if _, has := doc.connections[target][a.name]; !has {
						doc.connections[target][a.name] = funcDecl{name: a.name, body: a.stmt, line: a.line}
					}
				}
			}
		}
	}
	for _, c := range n.children {
		doc.registerConnections(c)
	}
}

// connectionsTargetID resolves a Connections { target: ... } expression to
// an element id: a bare identifier (target: pipeline), or the trailing
// member of a dotted chain (target: root.pipeline) — QML property lookup
// falls back to sibling ids, so `root.pipeline` and a bare `pipeline` id
// name the same element when pipeline is declared with `id: pipeline`.
func connectionsTargetID(n *node) string {
	for _, a := range n.attrs {
		if a.name != "target" {
			continue
		}
		switch e := a.expr.(type) {
		case *identExpr:
			return e.name
		case *memberExpr:
			for {
				if m, ok := e.base.(*memberExpr); ok {
					e = m
					continue
				}
				return e.name
			}
		}
	}
	return ""
}

// connectedHandler returns the combined handler for an element id's signal
// (e.g. "onClicked") from any Connections targeting it, or nil.
func (doc *docState) connectedHandler(id, signal string, env *Env) func() {
	if id == "" {
		return nil
	}
	fd, ok := doc.connections[id][signal]
	if !ok {
		return nil
	}
	return func() {
		if _, err := callUserFunc(&userFunc{params: fd.params, body: fd.body, env: env}, nil); err != nil {
			logf("gml: Connections %s.%s: %v", id, signal, err)
		}
	}
}

// registerComponents walks the tree recording `Component { id: x; child }`
// declarations so Loader { sourceComponent: x } can instantiate them.
func (doc *docState) registerComponents(n *node) {
	if n.typeName == "Component" {
		if id := nodeID(n); id != "" && len(n.children) == 1 {
			doc.components[id] = n.children[0]
		}
	}
	for _, c := range n.children {
		doc.registerComponents(c)
	}
	// Components can also appear as element-valued attributes (e.g. a delegate).
	for _, a := range n.attrs {
		if a.elem != nil {
			doc.registerComponents(a.elem)
		}
	}
}

// elemKey identifies an element across frames: its id, or a stable
// position-based key when it has none.
func elemKey(n *node) string {
	if n.ekey != "" {
		return n.ekey
	}
	if id := nodeID(n); id != "" {
		n.ekey = id
	} else {
		n.ekey = "__el" + strconv.Itoa(n.line)
	}
	return n.ekey
}

// observableSignature folds every reactive input a binding can read (other
// than layout geometry) into one value: the process-wide property revision
// (which also covers TextField text, a core.Property) plus scroll offsets,
// which are plain floats outside the property system. Two equal signatures
// across frames mean no such input changed.
func (doc *docState) observableSignature() uint64 {
	sig := core.Revision()
	for k, s := range doc.scrolls {
		sig += hashStr(k) ^ uint64(math.Float32bits(s.Y)) ^ uint64(math.Float32bits(s.X))<<32
	}
	// Something in the document is WAITING on the clock rather than on a
	// property — a tooltip counting down its delay. Fold the clock in while
	// that is true, so the cached tree is not handed back for ever and the
	// wait can actually end.
	if doc.timed > 0 {
		sig += uint64(core.AnimClock())
	}
	return sig
}

// geometrySignature folds the laid-out bounds of every tracked element into one
// value. It changes when layout moves anything — a window resize or a binding
// that reads sibling/parent geometry settling over a frame — which the property
// revision can't see, so it's compared separately to force a rebuild. The
// elements are reused (not rebuilt) on skipped frames, so their bounds reflect
// the latest layout.
func (doc *docState) geometrySignature() uint64 {
	var sig uint64
	for k, el := range doc.elems.v {
		if el == nil {
			continue
		}
		b := ui.Bounds(el)
		h := uint64(math.Float32bits(b.X)) | uint64(math.Float32bits(b.Y))<<32
		h ^= uint64(math.Float32bits(b.W)) | uint64(math.Float32bits(b.H))<<32
		sig += (uint64(k)+1)*0x9e3779b97f4a7c15 ^ h
	}
	return sig
}

// anyLaidOut reports whether any registered element has real bounds — i.e.
// whether a tree has been laid out at all. geometrySignature cannot answer
// that: it folds each key's name in, so it is non-zero the moment anything
// is registered, bounds or not.
func (doc *docState) anyLaidOut() bool {
	for _, el := range doc.elems.v {
		if el == nil {
			continue
		}
		if b := ui.Bounds(el); b.W > 0 && b.H > 0 {
			return true
		}
	}
	return false
}

// hashStr is a small FNV-1a over a string, for folding map keys into a
// signature so two different elements with identical bounds don't cancel.
func hashStr(s string) uint64 {
	h := uint64(1469598103934665603)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// invalidateCache forces the next View() to rebuild everything (hot reload,
// native-signal handlers): the whole-document gate and every subtree cache.
func (doc *docState) invalidateCache() {
	doc.invalidates++
	doc.haveSigs = false
	doc.cachedEl = nil
	doc.subtrees.clear()
	clear(doc.propReads)
	clear(doc.tplObjs) // the markup may have changed what a control exposes
}

// instToken is a component instance's use-site token: a property carrying
// a hash of every value the use site passed in as a literal (attributes
// and declared-property overrides, evaluated in the use site's scope). Set
// here, read by the instance's build, so the instance's cache entry goes
// stale exactly when one of those values changes (see buildFileComponent).
func (doc *docState) instToken(use, inst *node, prefix string, overrides map[string]expr, extra []attr) *core.Property[uint64] {
	key := doc.idIn(prefix, "inst:"+elemKey(use))
	t := doc.instToks.get(key)
	if t == nil {
		t = &core.Property[uint64]{}
		doc.instToks.set(key, t)
	}
	h := uint64(1469598103934665603)
	mix := func(v uint64) { h ^= v; h *= 1099511628211 }
	for _, a := range inst.attrs {
		if !a.fromUse {
			continue
		}
		if l, ok := a.expr.(*litExpr); ok {
			mix(hashStr(a.name))
			mix(tokenHash(l.v, 0))
		}
	}
	for _, d := range inst.props {
		if _, ok := overrides[d.name]; !ok {
			continue
		}
		if l, ok := d.def.(*litExpr); ok {
			mix(hashStr(d.name))
			mix(tokenHash(l.v, 0))
		}
	}
	t.Set(h) // a no-op when unchanged, so an idle instance stays fresh
	return t
}

// prevPrefixOf is the row prefix in force where a use site is built (the
// instance's own prefix is pushed later).
func prevPrefixOf(doc *docState) string { return doc.rowPrefix }

// scopeSnap is one memoized scope snapshot: the copy, the env it was
// copied from, and the scope generation it was copied at.
type scopeSnap struct {
	env   *Env
	src   *Env
	gen   scopeGen
	deriv []string // the derived names copied, refreshed on every hit
}

// scopeGen is the document's scope generation: the set of objects and
// refs a scope can bind changes only when a declaration pass CREATES one
// (they are never removed, short of an invalidation), so a snapshot taken
// at the same generation from the same env sees the same bindings.
type scopeGen struct{ objs, refs, inval int }

func (doc *docState) scopeGeneration() scopeGen {
	return scopeGen{doc.elemObjs.count(), len(doc.synthRefs), doc.invalidates}
}

// transientScopeNames are the bindings a build makes that are NOT
// persistent objects: remade or recomputed every build, so a memoized
// snapshot refreshes them from the live chain on every hit.
var transientScopeNames = []string{"parent", "width", "height", "opacity", "visible"}

// scopeSnapshot is env.snapshot() memoized per site — the site being what
// captures it (an animator, a timer, a component's functions), under the
// row prefix that names the instance.
//
// Every rebuild of a component instance snapshotted its scope for each
// animator, timer and function in it: on the buttons page that was 6% of
// the frame in map copies, for snapshots identical to last frame's. What
// a scope binds is persistent — id objects and property refs live in the
// document's maps — so a copy stays valid until a declaration pass
// creates something new (scopeGeneration), and only the few values a
// build recomputes (the item's own geometry, `parent`, a control's
// contract) have to be copied again. Inside a delegate row the snapshot
// is always fresh: a row's model values are bound per row and are not
// tracked here.
func (doc *docState) scopeSnapshot(env *Env, site string) *Env {
	if doc.rowDepth > 0 {
		return env.snapshot()
	}
	if doc.scopeSnaps == nil {
		doc.scopeSnaps = map[string]*scopeSnap{}
	}
	key := doc.rowPrefix + "|" + site
	gen := doc.scopeGeneration()
	if sn := doc.scopeSnaps[key]; sn != nil && sn.src == env && sn.gen == gen {
		// Copying a name into the snapshot's own vars has to carry its
		// DERIVED status with it, or the value lands in a scope that
		// calls it a plain injected one and every read of it poisons
		// the reader's read-set (see Env.derived). The transient names
		// are exactly the ones shadowSelfGeometry marks derived —
		// width, height, opacity, visible — so hardcoding "not derived"
		// for them undid that mark on every memo hit.
		refresh := func(name string) {
			v, ok := env.lookup(name)
			if !ok {
				delete(sn.env.vars, name)
				delete(sn.env.derivedNames, name)
				delete(sn.env.derivedTok, name)
				return
			}
			sn.env.vars[name] = v
			derived, tok := derivedIn(env, name)
			if !derived {
				delete(sn.env.derivedNames, name)
				delete(sn.env.derivedTok, name)
				return
			}
			if sn.env.derivedNames == nil {
				sn.env.derivedNames = map[string]bool{}
			}
			sn.env.derivedNames[name] = true
			if tok == nil {
				delete(sn.env.derivedTok, name)
				return
			}
			if sn.env.derivedTok == nil {
				sn.env.derivedTok = map[string]*core.Property[uint64]{}
			}
			sn.env.derivedTok[name] = tok
		}
		for _, name := range transientScopeNames {
			refresh(name)
		}
		for _, name := range sn.deriv {
			refresh(name)
		}
		return sn.env
	}
	snap := env.snapshot()
	sn := &scopeSnap{env: snap, src: env, gen: gen}
	for c := env; c != nil; c = c.parent {
		// The frozen builtins root is KEPT as the snapshot's parent
		// rather than copied (see Env.snapshot), so its hundred-odd
		// constant namespaces are already in scope and already derived.
		// Refreshing them would copy each one into the snapshot's own
		// vars, where nothing marks it derived — and then every `Math.`
		// or `Easing.` in the document poisoned the read-set of
		// whatever was reading it.
		if c.frozen {
			continue
		}
	names:
		for name := range c.derivedNames {
			for _, have := range sn.deriv {
				if have == name {
					continue names
				}
			}
			sn.deriv = append(sn.deriv, name)
		}
	}
	doc.scopeSnaps[key] = sn
	return snap
}

// derivedIn reports whether name, as the live chain resolves it, is a
// derived value, and the token a read of it depends on (see Env.derived).
// It stops at the scope that owns the name, which is the one a lookup
// would have found.
func derivedIn(env *Env, name string) (bool, *core.Property[uint64]) {
	for c := env; c != nil; c = c.parent {
		if _, ok := c.vars[name]; ok {
			return c.isDerived(name), c.derivedTok[name]
		}
	}
	return false, nil
}

// beginFrame rotates the per-frame element registry (last frame's elements
// become the geometry source) and points each id'd live view at its
// previous-frame element.
func (doc *docState) beginFrame() {
	doc.timed = 0 // recounted by this build
	doc.sweepActivities()
	doc.keySeen.clear()
	// Rotate the two registries and clear the older one: it keeps its
	// slots, so the frame neither allocates a registry nor regrows it.
	older := doc.prevElems
	doc.prevElems = doc.elems
	older.clear()
	doc.elems = older
	// The registry is keyed the same way elements are — the instance/row
	// prefix included — so a delegate inside a styled control or a delegate
	// row finds ITS element, not the document-global one of the same id.
	for i, eo := range doc.elemObjs.v {
		if eo == nil {
			continue
		}
		key := keyID(i)
		// A view knows the registry key of its own element, which carries
		// the instance/row prefix even when the id it is filed under does
		// not (a component instance's root: `root.width` inside it).
		if k, ok := doc.idKeys.get2(key); ok {
			key = k // the instance this id was last built in
		} else if eo.key != 0 {
			key = eo.key
		}
		eo.el = doc.prevElems.get(key)
	}
	// Stamp every GEOMETRY TOKEN whose item moved since last frame (see
	// geoToken): a Set with an unchanged hash is a no-op, so an idle page
	// stamps nothing and its subtrees stay provably fresh. Scroll states
	// likewise (see scrollTok).
	doc.stampTokens(&doc.prevElems)
}

// stampTokens writes every geometry token from the given element registry
// and every scroll token from its state (see geoToken, scrollTok).
func (doc *docState) stampTokens(elems *ccol[ui.Element]) {
	// Once per frame: View stamps early (ahead of the property bindings)
	// and beginFrame would stamp the same registry again a moment later.
	if doc.stampedAt == doc.frameNo && doc.frameNo != 0 {
		return
	}
	doc.stampedAt = doc.frameNo
	for i, t := range doc.geoTok.v {
		if t != nil {
			t.Set(geomHash(elems.get(keyID(i))))
		}
	}
	// An id object's element can come from somewhere other than its own
	// registry key (a queued handler's refreshed scope, an effect capture
	// re-pointing a source): stamp from what the OBJECT holds as well, so
	// a reader is never served for ever against a key nothing registers.
	for i, eo := range doc.elemObjs.v {
		if eo == nil {
			continue
		}
		key := keyID(i)
		if k, ok := doc.idKeys.get2(key); ok {
			key = k
		} else if eo.key != 0 {
			key = eo.key
		}
		if t := doc.geoTok.get(key); t != nil && eo.el != nil {
			t.Set(geomHash(eo.el))
		}
	}
	stampScrollTokens()
}

// geoToken is the geometry token for an element registry key: a property
// carrying a hash of the item's laid-out rect (and implicit size), written
// by beginFrame when the rect changes. A geometry read Gets it, so the
// read-set of the subtree that read `root.height` depends on exactly that
// item and is stale only when it moved — instead of always, which is what
// an untracked read meant: two thirds of the corpus' styled controls read
// their own height for a radius or a handle size, and the subtree cache
// could never serve one of them. Created on first read, seeded from the
// element the reader saw, so the frame that changes it marks the reader.
func (doc *docState) geoToken(key keyID, el ui.Element) *core.Property[uint64] {
	t := doc.geoTok.get(key)
	if t == nil {
		t = &core.Property[uint64]{}
		t.Set(geomHash(el))
		doc.geoTok.set(key, t)
	}
	return t
}

// refreshScopeGeometry re-points the id objects in a captured scope at last
// frame's elements, the way beginFrame does for the live registry. Objects
// are shared by pointer, so this is the same fix reaching the copies a
// queued handler kept — and it must overwrite an element that is merely
// UNLAID-OUT, not only a nil one: a queued handler holds the objects of
// the build that queued it, whose elements were built and registered but
// never given bounds (the tree that got laid out is the one the NEXT
// build replaced them with).
func (doc *docState) refreshScopeGeometry(env *Env) {
	for e := env; e != nil; e = e.parent {
		for name, v := range e.vars {
			obj, ok := v.(*elemObject)
			if !ok || obj == nil {
				continue
			}
			if obj.el != nil {
				if b := ui.Bounds(obj.el); b.W > 0 || b.H > 0 {
					continue
				}
			}
			key := obj.key
			if key == 0 {
				key = doc.intern(name)
			}
			if k, ok := doc.idKeys.get2(key); ok {
				key = k
			} else if k, ok := doc.idKeys.get2(doc.intern(name)); ok {
				key = k
			}
			if el := doc.prevElems.get(key); el != nil {
				if b := ui.Bounds(el); b.W > 0 || b.H > 0 {
					obj.el = el
				}
			}
		}
	}
}

// merged returns a shallow copy of n with extra override attributes appended.
// Builders process attributes in order and later ones win, so the appended
// state overrides take effect over the element's base values.
func (n *node) merged(extra []attr) *node {
	cp := *n
	// An override REPLACES the attribute it names: appended after it, both
	// were applied in turn (the last winning), and a tweened property saw
	// its target flip between the two on every build, restarting the tween.
	names := make(map[string]bool, len(extra))
	for i := range extra {
		names[extra[i].name] = true
	}
	cp.attrs = make([]attr, 0, len(n.attrs)+len(extra))
	for _, a := range n.attrs {
		if !names[a.name] {
			cp.attrs = append(cp.attrs, a)
		}
	}
	cp.attrs = append(cp.attrs, extra...)
	cp.ovNode, cp.stripped = nil, nil
	if cp.noSelfReads {
		for i := range extra {
			if attrMentions(&extra[i], selfScopeNames) {
				cp.noSelfReads = false
				break
			}
		}
	}
	return &cp
}

// listStateFor returns the retained scroll state for a ListView, keyed by
// its id attribute (or a stable position-based key when none is given) so
// the scroll position survives per-frame rebuilds.
func listStateFor(n *node, doc *docState) *ui.ListState {
	key := ""
	for _, a := range n.attrs {
		if a.name == "id" {
			if id, ok := a.expr.(*identExpr); ok {
				key = id.name
			}
		}
	}
	if key == "" {
		key = fmt.Sprintf("__list%d", n.line) // line number is stable per doc
	}
	key = doc.rowPrefix + key // per-row scroll state for a list inside a delegate
	if st := doc.lists[key]; st != nil {
		return st
	}
	st := &ui.ListState{}
	doc.lists[key] = st
	return st
}

// declareProps walks the tree and registers `property` declarations into
// env. Defaults evaluate once; redeclaration after a hot reload keeps the
// current value, so state survives edits.
func declareProps(n *node, env *Env, doc *docState) error {
	// ListModel rows are data, not state: (re)evaluate them each load so a
	// referencing ListView/Repeater can resolve the model regardless of
	// declaration order.
	if n.typeName == "ListModel" {
		return registerModel(n, env, doc)
	}
	// A model declared inline as an attribute value (`model: ListModel { id:
	// tabModel; ... }`) is still a document-wide id: the page's own labels
	// read `tabModel.count` and `tabModel.get(i)`.
	for _, a := range n.attrs {
		if a.elem == nil || a.elem.typeName != "ListModel" {
			continue
		}
		if nodeID(a.elem) != "" {
			if err := registerModel(a.elem, env, doc); err != nil {
				return err
			}
			continue
		}
		// No id: nothing can name it, but it still needs ONE instance
		// across rebuilds so a Component.onCompleted that fills it is not
		// undone (see registerInlineModel).
		registerInlineModel(a.elem, env, doc)
	}
	if n.typeName == "TableModel" {
		return registerTableModel(n, env, doc)
	}
	// FontLoader as a child element: instantiate once and bind its object
	// (name/source/status) to its id for font.family bindings.
	if n.typeName == "FontLoader" {
		if id := nodeID(n); id != "" {
			if _, done := env.vars[id].(map[string]any); !done {
				env.vars[id] = instantiateFontLoader(n, env, doc)
			}
		}
	}
	// ItemSelectionModel: bind its id to a live selection object up front so
	// forward reads (selModel.hasSelection driving an enabled: binding)
	// resolve before the owning TableView builds; the table keeps the props
	// current and attaches methods when it builds (tableview.go).
	if n.typeName == "ItemSelectionModel" {
		if id := nodeID(n); id != "" {
			obj := doc.elemObjs.get(doc.intern(id))
			if obj == nil {
				obj = &elemObject{}
				doc.elemObjs.set(doc.intern(id), obj)
			}
			if obj.props == nil {
				obj.props = map[string]any{}
			}
			if _, ok := obj.props["hasSelection"]; !ok {
				obj.setProp("hasSelection", false)
				obj.setProp("selectedIndexes", []any{})
			}
			env.vars[id] = obj
		}
	}
	// Register TableView nodes by id so a HorizontalHeaderView declared
	// before its syncView can still resolve the table's model and widths.
	// (Nil-guarded: module.go's singleton scratch docState is a bare literal.)
	if n.typeName == "TableView" {
		if id := nodeID(n); id != "" {
			if doc.tableNodes == nil {
				doc.tableNodes = map[string]*node{}
			}
			doc.tableNodes[id] = n
		}
	}
	// A `Component { id: x; <child> }` binds its id to a componentRef so it can
	// be instantiated on demand (Loader.sourceComponent, StackView.push) rather
	// than rendered in place. Other id'd elements get a live elemObject (reused
	// across reloads) exposing their declared properties as `id.prop` and their
	// geometry as `id.width` etc. Properties are also global by bare name.
	var idObj *elemObject
	if n.typeName == "Component" {
		if id := nodeID(n); id != "" && len(n.children) == 1 {
			env.vars[id] = &componentRef{node: n.children[0]}
		}
	} else if id := nodeID(n); id != "" {
		// Remember the node behind each id: the effect elements
		// (ShaderEffectSource) build another element's subtree by name.
		if doc.nodesByID == nil {
			doc.nodesByID = map[string]*node{}
		}
		doc.nodesByID[id] = n
		idObj = doc.elemObjs.get(doc.intern(id))
		if idObj == nil {
			idObj = &elemObject{props: map[string]any{}}
			doc.elemObjs.set(doc.intern(id), idObj)
		}
		// Where this element sits in the tree, so `someId.parent.<prop>`
		// resolves (QML reads a sibling's parent to reach the item they
		// share). Both are stable across frames: the node is the markup and
		// the env is the document scope.
		idObj.node, idObj.nodeEnv = n, env
		idObj.doc, idObj.key = doc, doc.keyID(n)
		idObj.parentObj = doc.declParent
		idObj.parentKey = doc.declParentKey
		// Bind in env unless a specialized object (input/scroll/model) already
		// claimed this id at a later build phase.
		if _, taken := env.vars[id]; !taken {
			env.vars[id] = idObj
		}
		// Pre-seed live hover/press state so an element's OWN attributes
		// (color: x.containsMouse ? ...) resolve on the very first build —
		// wireHoverPress installs the same refs (same keys) at build time,
		// which is too late for the attribute pass. Mirrors declareRowScope's
		// per-row phase-1 seeding.
		//
		// The keys carry the row prefix, like wireHoverPress's do. Keyed by
		// the bare id they were shared by every instance of a style file —
		// each one's root is `id: root` — so two styled Buttons side by side
		// fought over one `root.pressed`: the held one wrote true, its
		// neighbour wrote false a moment later, and neither ever darkened.
		if idObj.props != nil {
			if _, has := idObj.props["containsMouse"]; !has {
				ikey := doc.keyStr(idObj.key)
				hover := doc.boolProp(ikey+".containsMouse", false)
				idObj.setProp("containsMouse", hover)
				idObj.setProp("hovered", hover)
				idObj.setProp("pressed", doc.boolProp(ikey+".pressed", false))
				// Keyboard focus, tracked from the element's own layout
				// (focus.go); a text input claims its id with its own state.
				fr, _ := doc.trackFocus(idObj.key, idObj)
				idObj.setProp("activeFocus", fr)
			}
		}
		seedControlState(n, env, doc)
		seedDerivedProps(n, env, doc, idObj)
		// A text field's state exists from declare time, so a sibling
		// attribute or property default reading field.text / .activeFocus
		// resolves before the field itself builds.
		if isTextInput(n.typeName) && idObj.input == nil {
			idObj.input = doc.inputFor(id, env)
		}
		// Same for a Flickable/ScrollView's scroll state, so
		// `flick.contentY` / `flick.visibleArea.yPosition` resolve.
		if isScrollType(n.typeName) && idObj.scroll == nil {
			idObj.scroll = doc.scrollFor(doc.idKey(id))
		}
		seedCanvasMethods(n, env, idObj)
		doc.seedSettingsMethods(n, env, idObj)
		seedSystemPalette(n, env, idObj)
	}
	if len(n.props) > 0 {
		// Defaults see this item's own width/height (QML self-geometry).
		restoreGeom := shadowSelfGeometry(n, env, doc)
		defer restoreGeom()
	}
	for _, d := range n.props {
		key := doc.propKey(d.name)
		ref, ok := doc.props[key]
		if !ok {
			if d.typ == "alias" {
				// The target is kept unevaluated and resolved lazily per access.
				ref = aliasRef{target: d.def, env: env}
			} else {
				def, err := d.def.eval(env)
				if err != nil {
					// Forward reference (an id or property declared later in
					// the file): register with the type's zero value now and
					// retry the default once declaration completes.
					def = zeroFor(d.typ)
					doc.pendingProps = append(doc.pendingProps,
						pendingProp{name: key, def: d.def, env: env, line: d.line})
				}
				def = materializeElemDefault(def, env, doc)
				raw, err := newDeclaredProp(d.typ, def)
				if err != nil {
					return fmt.Errorf("gml:%d: property %s: %v", d.line, d.name, err)
				}
				doc.propsRaw[key] = raw
				// Wrap so imperative assignments mark the binding broken
				// (QML: `prop = x` in a handler replaces the binding).
				ref = trackedRef{Ref: raw, doc: doc, name: key}
			}
			doc.props[key] = ref
			if n.typeName == "Settings" {
				doc.bindSetting(n, d.name, ref, env)
			}
			// Wire an on<Prop>Changed handler once, when the property is first
			// created (refs persist across hot reloads).
			if h := handlerFor(n, "on"+title(d.name)+"Changed"); h != nil {
				name := d.name
				// Run through the row-scope-aware runner: a handler declared
				// inside a delegate has to see its row when it fires, which
				// can be frames later. (The document's own scope is
				// long-lived, so this keeps the live env — a per-instance
				// property is wired in declareRowScope instead, against a
				// snapshot of the instance.)
				run := doc.handlerRunner(env)
				ref.onChange(func() { run(h, "on"+title(name)+"Changed") })
			}
		} else if raw, has := doc.propsRaw[key]; has && !doc.assigned[key] {
			// QML live binding: the default expression re-evaluates while no
			// imperative assignment has broken it (`itemsSlide: itemsOpen ?
			// 1 : 0` must follow itemsOpen). Writes go through the RAW ref —
			// a binding update must not mark the binding broken. Element
			// defaults (FontLoader) are one-shot: never re-instantiated.
			if _, isElem := d.def.(*elemExpr); !isElem {
				if v, err := d.def.eval(env); err == nil {
					if str(raw.get()) != str(v) {
						_ = raw.set(v)
					}
				}
			}
		}
		env.vars[d.name] = ref
		if idObj != nil {
			idObj.setProp(d.name, ref)
		}
	}
	for _, c := range n.children {
		// A delegate host's children are per-row component scopes: their ids
		// and properties are declared per instance by declareRowScope when
		// rows build, never document-globally (QML delegate ids aren't
		// visible outside the component). ListModel children are still data
		// owned by the document.
		if isDelegateHost(n.typeName) && c.typeName != "ListModel" {
			continue
		}
		// A child's property default may read the item it sits in
		// (`property real prog: parent.progress`), which QML resolves
		// lexically — and this node's own properties were declared just
		// above, so they are there to read.
		prevParent, hadParent := env.vars["parent"]
		pobj := parentObject(n, env, doc)
		env.vars["parent"] = pobj
		prevDecl, prevDeclKey := doc.declParent, doc.declParentKey
		doc.declParent, doc.declParentKey = pobj, doc.keyID(n)
		err := declareProps(c, env, doc)
		doc.declParent, doc.declParentKey = prevDecl, prevDeclKey
		if hadParent {
			env.vars["parent"] = prevParent
		} else {
			delete(env.vars, "parent")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// stateAttrs names the attributes that are a control's OWN live state (the
// Q_PROPERTYs a real Controls type writes itself), with the binder that
// creates the backing property.
var stateAttrs = map[string]map[string]string{
	"Slider":         {"value": "real"},
	"CheckBox":       {"checked": "bool"},
	"CheckDelegate":  {"checked": "bool"},
	"Switch":         {"checked": "bool"},
	"SwitchDelegate": {"checked": "bool"},
	"RadioButton":    {"checked": "bool"},
	"SpinBox":        {"value": "int"},
	"ComboBox":       {"currentIndex": "int"},
	"TabBar":         {"currentIndex": "int"},
	"Tumbler":        {"currentIndex": "int"},
}

// seedInteractionState publishes the interaction members every Control
// carries — pressed/down/hovered, and checked for the checkable ones — on
// the control's own id, so a binding that reads its own state resolves from
// the first frame (`text: btn.down ? "Pressed!" : "Press Me"`). They are the
// same doc.boolProp slots the element wires when it builds, so the value
// tracks the pointer; only the DECLARATION has to come first.
func seedInteractionState(n *node, doc *docState) {
	if !isControlKind(n.typeName) || nodeID(n) == "" {
		return
	}
	key := doc.key(n)
	press := doc.boolProp(key+".pressed", false)
	hover := doc.boolProp(key+".containsMouse", false)
	obj := doc.elemObjs.get(doc.here(nodeID(n)))
	has := func(name string) bool {
		if obj == nil || obj.props == nil {
			return false
		}
		_, ok := obj.props[name]
		return ok
	}
	for name, ref := range map[string]Ref{
		"pressed": press, "down": press, "hovered": hover,
	} {
		if !has(name) {
			doc.exposeIDProp(n, name, ref)
		}
	}
	// `checked` only when nothing else owns it: a declared value, or a kind
	// whose state table seeds it, must create the slot with ITS value —
	// creating it here first would pin it to false.
	if _, owned := stateAttrs[n.typeName]["checked"]; !owned && !has("checked") && !hasAttr(n, "checked") {
		doc.exposeIDProp(n, "checked", doc.boolProp(key+".checked", false))
	}
}

// isControlKind reports the Qt Quick Controls that carry interaction state.
func isControlKind(t string) bool {
	switch t {
	case "Button", "ToolButton", "DelayButton", "RoundButton", "TabButton",
		"MenuItem", "MenuBarItem", "ItemDelegate", "SwipeDelegate",
		"CheckBox", "CheckDelegate", "Switch", "SwitchDelegate", "RadioButton",
		"RadioDelegate", "Slider", "RangeSlider", "Dial", "SpinBox",
		"ComboBox", "TextField", "TextArea":
		return true
	}
	return false
}

// seedCanvasMethods gives a Canvas id its repaint API. We repaint every
// frame, so the request is a no-op — but it must RESOLVE, both qualified
// (myCanvas.requestPaint()) and bare inside the Canvas's own handlers
// (QML's item scope).
func seedCanvasMethods(n *node, env *Env, obj *elemObject) {
	if n.typeName != "Canvas" || obj == nil {
		return
	}
	if obj.methods == nil {
		obj.methods = map[string]any{}
	}
	repaint := jsFunc(func([]any) (any, error) { return nil, nil })
	obj.methods["requestPaint"] = repaint
	obj.methods["markDirty"] = repaint
	if _, taken := env.vars["requestPaint"]; !taken {
		env.vars["requestPaint"] = repaint
	}
}

// seedDerivedProps gives a control's READ-ONLY derived properties a value at
// declare time, so a sibling that reads one before the control builds (a card
// declaring its preview rectangle above the ComboBox it previews) resolves on
// the first frame. The control overwrites them with the real values when it
// builds.
func seedDerivedProps(n *node, env *Env, doc *docState, obj *elemObject) {
	if obj == nil {
		return
	}
	if n.typeName == "ComboBox" {
		obj.derive = comboDerive(n, env, doc)
	}
}

// comboDerive computes a ComboBox's read-only properties on demand — from its
// model, roles and current index — so they have their REAL values (and real
// types: currentValue may be a number or a colour) before the box has built.
func comboDerive(n *node, env *Env, doc *docState) func(string) (any, bool) {
	return func(name string) (any, bool) {
		switch name {
		case "currentText", "displayText", "currentValue", "count", "editText":
		default:
			return nil, false
		}
		var modelAttr *attr
		textRole, valueRole := "", ""
		for i := range n.attrs {
			switch n.attrs[i].name {
			case "model":
				modelAttr = &n.attrs[i]
			case "textRole":
				textRole, _ = attrString(n.attrs[i], env)
			case "valueRole":
				valueRole, _ = attrString(n.attrs[i], env)
			}
		}
		if modelAttr == nil {
			return nil, false
		}
		rows, count, err := resolveModel(*modelAttr, env)
		if err != nil {
			return nil, false
		}
		var items []any
		switch {
		case rows != nil:
			for _, row := range rows {
				items = append(items, comboRowText(row, textRole))
			}
		case modelAttr.expr != nil:
			if v, verr := modelAttr.expr.eval(env); verr == nil {
				if arr, ok := v.([]any); ok {
					items = arr
				}
			}
		}
		if name == "count" {
			if len(items) > 0 {
				return float64(len(items)), true
			}
			return float64(count), true
		}
		cur := refInt(doc.floatProp(doc.key(n)+".currentIndex", 0))
		if cur < 0 || cur >= len(items) {
			return nil, false
		}
		if name != "currentValue" {
			return str(items[cur]), true
		}
		if valueRole != "" && cur < len(rows) {
			if v, ok := rows[cur][valueRole]; ok {
				return v, true
			}
		}
		return items[cur], true
	}
}

// seedControlState creates an id'd control's own state property at declare
// time, so reads that appear BEFORE the control in the file resolve on the
// very first build (`Label { text: "A: " + sl.value }` above the Slider).
// Only literal-valued (or absent) attributes are seeded: a bound attribute
// IS the property it names, which the control's own build resolves — and
// seeding it here would fork the state in two.
func seedControlState(n *node, env *Env, doc *docState) {
	seedInteractionState(n, doc)
	if n.typeName == "RangeSlider" {
		seedRangeHandles(n, env, doc)
	}
	attrs, ok := stateAttrs[n.typeName]
	if !ok {
		return
	}
	for name, kind := range attrs {
		a, has := attr{}, false
		for _, at := range n.attrs {
			if at.name == name {
				a, has = at, true
				break
			}
		}
		if has {
			if _, lit := a.expr.(*litExpr); !lit {
				continue // bound to a property: not the control's own state
			}
			switch kind {
			case "bool":
				boolBinding(doc, n, a, env)
			default:
				floatBinding(doc, n, a, env) // intBinding shares this backing
			}
			continue
		}
		// No attribute at all: the control still has state at its zero value.
		key := doc.key(n) + "." + name
		if kind == "bool" {
			doc.exposeIDProp(n, name, doc.boolProp(key, false))
		} else {
			doc.exposeIDProp(n, name, doc.floatProp(key, 0))
		}
	}
}

// seedRangeHandles publishes a RangeSlider's two handle sub-objects at
// declare time, each holding the live value ref the control (builtin or
// styled) then uses: a page reads them through its own id
// (`priceRange.first.value`), often from a Label declared above the slider.
func seedRangeHandles(n *node, env *Env, doc *docState) {
	if nodeID(n) == "" {
		return
	}
	from, to := float32(0), float32(1)
	for _, a := range n.attrs {
		switch a.name {
		case "from":
			if f, err := attrFloat(a, env); err == nil {
				from = f
			}
		case "to":
			if f, err := attrFloat(a, env); err == nil {
				to = f
			}
		}
	}
	seed := map[string]float32{"first": from, "second": to}
	for _, a := range n.attrs {
		for _, side := range []string{"first", "second"} {
			if a.name == side+".value" {
				if f, err := attrFloat(a, env); err == nil {
					seed[side] = f
				}
			}
		}
	}
	key := doc.key(n)
	for _, side := range []string{"first", "second"} {
		ref := doc.floatProp(key+"."+side+".value", seed[side])
		v, _ := toFloat32(ref.get())
		pos := float64(0)
		if to != from {
			pos = float64(min(max((v-from)/(to-from), 0), 1))
		}
		doc.exposeIDProp(n, side, map[string]any{
			"value": ref, "position": pos, "visualPosition": pos,
			"pressed": false, "hovered": false,
		})
	}
}

// declareFuncs registers `function` declarations into env as callable
// userFuncs closed over the document scope. Re-registered each load (code,
// not state), like declareProps.
func declareFuncs(n *node, env *Env) { declareFuncsScoped(n, env, env) }

// declareFuncsScoped registers the same declarations into env but closes the
// functions over `closure` instead. A file component's functions belong to
// the INSTANCE: they must keep resolving the instance's own ids and
// properties when they run later (from a deferred onCompleted, or a click),
// long after the build that bound that scope has restored it — so the
// instance passes a snapshot of its scope as the closure.
func declareFuncsScoped(n *node, env, closure *Env) {
	for _, f := range n.funcs {
		fn := &userFunc{params: f.params, defaults: f.defaults, body: f.body, env: closure}
		env.vars[f.name] = fn
		// Also reachable as a member of the declaring element's id
		// (root.deptColor(...) — QML functions are methods of their item).
		if id := nodeID(n); id != "" {
			if obj, ok := env.vars[id].(*elemObject); ok {
				if obj.methods == nil {
					obj.methods = map[string]any{}
				}
				obj.methods[f.name] = fn
			}
		}
	}
	for _, c := range n.children {
		declareFuncsScoped(c, env, closure)
	}
}

// declareSignals registers each `signal name(params)` as a callable: emitting
// it runs the same element's `on<Name>` handler with the params bound. A
// signal is therefore a userFunc whose body is its handler — emit = call.
func declareSignals(n *node, env *Env, doc *docState) {
	id := nodeID(n)
	for _, s := range n.signals {
		handler := "on" + title(s.name)
		own := &userFunc{params: s.params, body: handlerFor(n, handler), env: env}
		fd, connected := doc.connections[id][handler]
		if id == "" || !connected {
			env.vars[s.name] = own
			doc.signalMethod(id, s.name, own)
			continue
		}
		// A Connections { target: <this id> } handler runs too, after the
		// element's own, with the signal's arguments. The Qt 5 form
		// (`onMoved: …`) declares no parameters and sees the signal's
		// parameter names, as in QML.
		params := fd.params
		if len(params) == 0 {
			params = s.params
		}
		conn := &userFunc{params: params, body: fd.body, env: env}
		env.vars[s.name] = jsFunc(func(args []any) (any, error) {
			if _, err := callUserFunc(own, args); err != nil {
				return nil, err
			}
			return callUserFunc(conn, args)
		})
		doc.signalMethod(id, s.name, env.vars[s.name])
	}
	for _, c := range n.children {
		declareSignals(c, env, doc)
	}
}

// applyVisible evaluates a node's `visible` binding (if any), strips the attr
// so element builders never see it, and reports whether the element is hidden
// (visible:false → rendered as an empty zero-size box, like QML). Returns the
// same node unchanged when there is no visible attribute.
func applyVisible(n *node, env *Env) (*node, bool) {
	// Overlay types manage their own open/visible state in buildPopup (visible
	// is the open flag, not a collapse), so don't strip/hide them here.
	switch n.typeName {
	case "Popup", "Dialog", "Drawer", "Menu", "ToolTip":
		return n, false
	}
	has := false
	for _, a := range n.attrs {
		if a.name == "visible" {
			has = true
			break
		}
	}
	if !has {
		return n, false
	}
	hidden := false
	cp := *n
	cp.attrs = nil
	for _, a := range n.attrs {
		if a.name == "visible" {
			if a.expr != nil {
				if v, err := a.expr.eval(env); err == nil {
					if b, err := toBool(v); err == nil && !b {
						hidden = true
					}
				}
			}
			continue
		}
		cp.attrs = append(cp.attrs, a)
	}
	return &cp, hidden
}

// layoutHints is which axes an item asked to fill (QtQuick.Layouts'
// Layout.fillWidth / Layout.fillHeight, or SplitView's equivalents).
type layoutHints struct{ w, h bool }

// attachedSizeAttr reports an attached size hint an ancestor container owns
// — QtQuick.Layouts' `Layout.*` and SplitView's identically-shaped
// `SplitView.*` — and returns the bare hint name ("fillWidth").
func attachedSizeAttr(name string) (string, bool) {
	for _, pre := range [...]string{"Layout.", "SplitView."} {
		if strings.HasPrefix(name, pre) {
			return name[len(pre):], true
		}
	}
	return "", false
}

// extractLayoutAttrs removes attached size hints (any attr named "Layout.*"
// or "SplitView.*") from a node so element builders don't reject them.
// fillWidth/fillHeight map to flexbox grow; preferredWidth/preferredHeight
// (and minimum sizes, as an approximation) become fixed sizes. Other hints
// (alignment, spans, margins) are accepted as no-ops. Returns the same node
// unchanged when there are none.
func extractLayoutAttrs(n *node, env *Env) (*node, layoutHints, float32, float32, float32, float32, int, layoutMargins) {
	has := false
	for _, a := range n.attrs {
		if _, ok := attachedSizeAttr(a.name); ok {
			has = true
			break
		}
	}
	if !has {
		return n, layoutHints{}, 0, 0, 0, 0, 0, layoutMargins{}
	}
	var fill layoutHints
	var minW, minH float32
	var prefW, prefH float32
	var align int
	var mar layoutMargins
	cp := *n
	cp.attrs = nil
	for _, a := range n.attrs {
		hint, ok := attachedSizeAttr(a.name)
		if !ok {
			cp.attrs = append(cp.attrs, a)
			continue
		}
		switch hint {
		case "fillWidth":
			if b, err := attrBool(a, env); err == nil && b {
				fill.w = true
			}
		case "fillHeight":
			if b, err := attrBool(a, env); err == nil && b {
				fill.h = true
			}
		case "preferredWidth", "preferredHeight":
			// Evaluated below, in an order that lets one axis be written in
			// terms of the other.
		case "minimumWidth":
			// A MINIMUM is a floor, not a size: an item that also fills its
			// axis must still stretch past it (a card with
			// `Layout.minimumHeight: 80` in a row of 320-tall cards is 320).
			if f, err := attrFloat(a, env); err == nil && f > minW {
				minW = f
			}
		case "minimumHeight":
			if f, err := attrFloat(a, env); err == nil && f > minH {
				minH = f
			}
		case "alignment":
			// Qt::AlignmentFlag bits: where the item sits in the space the
			// layout gives it, instead of filling it.
			if f, err := attrFloat(a, env); err == nil {
				align = int(f)
			}
		case "margins", "leftMargin", "rightMargin", "topMargin", "bottomMargin":
			// A layout item's own margins: space around it INSIDE its
			// cell, which the corpus uses to indent a nested group (a
			// checkbox list under its "Select All").
			if f, err := attrFloat(a, env); err == nil {
				switch hint {
				case "margins":
					mar = layoutMargins{t: f, r: f, b: f, l: f, set: true}
				case "leftMargin":
					mar.l, mar.set = f, true
				case "rightMargin":
					mar.r, mar.set = f, true
				case "topMargin":
					mar.t, mar.set = f, true
				case "bottomMargin":
					mar.b, mar.set = f, true
				}
			}
		}
		// all other hints: accepted, no-op
	}
	// One axis is routinely stated in terms of the other (`preferredWidth:
	// 10; preferredHeight: width` — a round dot), so bind each as it is
	// resolved and let the second read it. QML resolves this lazily, in
	// either order; here width goes first and height gets a second chance.
	prefW = sizeAttr(n, env, "Layout.preferredWidth")
	restoreW := bindGeom(env, "width", prefW)
	prefH = sizeAttr(n, env, "Layout.preferredHeight")
	restoreW()
	if prefW == 0 && prefH > 0 {
		restoreH := bindGeom(env, "height", prefH)
		prefW = sizeAttr(n, env, "Layout.preferredWidth")
		restoreH()
	}
	// A minimum on an axis the item does NOT fill still needs to size it,
	// since nothing else will (QML's implicit size is content-based here).
	if minW > 0 && !fill.w && prefW == 0 {
		prefW = minW
	}
	if minH > 0 && !fill.h && prefH == 0 {
		prefH = minH
	}
	return &cp, fill, prefW, prefH, minW, minH, align, mar
}

// layoutMargins is a layout item's Layout.*Margin set.
type layoutMargins struct {
	t, r, b, l float32
	set        bool
}

// crossAnchorAlign reads a child's cross-axis anchor as an alignment: in a
// column that is a horizontal anchor, in a row a vertical one. Anchors to
// anything other than the parent are not alignments and are left alone.
func crossAnchorAlign(n *node, row bool) (ui.Align, bool) {
	center, start, end := "anchors.horizontalCenter", "anchors.left", "anchors.right"
	if row {
		center, start, end = "anchors.verticalCenter", "anchors.top", "anchors.bottom"
	}
	toParent := func(name string) bool {
		for _, a := range n.attrs {
			if a.name != name {
				continue
			}
			// `parent.horizontalCenter` — an anchor to a SIBLING is not an
			// alignment, and guessing one would move the item further from
			// where Qt puts it than leaving it alone does.
			m, ok := a.expr.(*memberExpr)
			if !ok {
				return false
			}
			id, ok := m.base.(*identExpr)
			return ok && id.name == "parent"
		}
		return false
	}
	switch {
	case toParent(center):
		return ui.AlignCenter, true
	case toParent(end):
		return ui.AlignEnd, true
	case toParent(start):
		return ui.AlignStart, true
	}
	return ui.AlignStart, false
}

// stripSizeAttrs returns a copy of n without its width/height attributes.
func stripSizeAttrs(n *node) *node {
	has := false
	for _, a := range n.attrs {
		if a.name == "width" || a.name == "height" {
			has = true
			break
		}
	}
	if !has {
		return n
	}
	cp := *n
	cp.attrs = nil
	for _, a := range n.attrs {
		if a.name == "width" || a.name == "height" {
			continue
		}
		cp.attrs = append(cp.attrs, a)
	}
	return &cp
}

// rootTypeOf is the QML type an element really IS: for a file component
// (`MultiplaneScene { }`) that is the type of its own root element, which
// is what decides whether it has an implicit size — the use-site name says
// nothing about it.
func rootTypeOf(n *node, doc *docState) string {
	if comp := doc.fileComponent(n.typeName); comp != nil {
		return comp.typeName
	}
	return n.typeName
}

// layoutChildFills reports whether one of a layout's own items asks to fill
// the named axis — so the layout has something that can USE space beyond
// its preferred size, which is when Qt's default (fill true for a layout
// item) actually shows. A Repeater's delegate counts: that is how the
// corpus' tile strips are written.
func layoutChildFills(n *node, env *Env, attr string) bool {
	for _, c := range n.children {
		kids := []*node{c}
		if c.typeName == "Repeater" || c.typeName == "Component" {
			kids = append(kids, c.children...)
			for _, a := range c.attrs {
				if a.elem != nil {
					kids = append(kids, a.elem)
				}
			}
		}
		for _, k := range kids {
			for _, a := range k.attrs {
				if a.name != attr {
					continue
				}
				if b, err := attrBool(a, env); err == nil && b {
					return true
				}
			}
		}
	}
	return false
}

// qtLayoutType reports the QtQuick.Layouts types — the ones whose fill
// hints default to true when they are themselves items of a layout.
func qtLayoutType(t string) bool {
	switch t {
	case "RowLayout", "ColumnLayout", "GridLayout", "StackLayout":
		return true
	}
	return false
}

// noImplicitSizeType reports the QML types that have no implicit size of
// their own: what they contain does not size them, so an explicit width or
// height is all a layout has to go on.
func noImplicitSizeType(t string) bool {
	switch t {
	case "Item", "Rectangle", "MouseArea", "FocusScope", "Canvas",
		"Shape", "ShaderEffect", "ShaderEffectSource", "Flickable",
		"ListView", "GridView", "PathView", "TableView", "TreeView",
		"View3D", "Viewport3D", "Map", "VideoOutput", "GraphsView",
		"ParticleSystem", "MonthGrid", "DayOfWeekRow", "Tumbler":
		return true
	}
	return false
}

// crossAlign turns Qt::AlignmentFlag bits into the alignment across a
// layout's own axis: in a column that is the horizontal bit, in a row the
// vertical one. 0 (and a flag naming only the main axis) means "no
// preference", which leaves the layout's own rule in charge.
func crossAlign(align int, row bool) (ui.Align, bool) {
	if row {
		switch {
		case align&0x0020 != 0: // AlignTop
			return ui.AlignStart, true
		case align&0x0040 != 0: // AlignBottom
			return ui.AlignEnd, true
		case align&0x0080 != 0: // AlignVCenter
			return ui.AlignCenter, true
		}
		return ui.AlignStart, false
	}
	switch {
	case align&0x0001 != 0: // AlignLeft
		return ui.AlignStart, true
	case align&0x0002 != 0: // AlignRight
		return ui.AlignEnd, true
	case align&0x0004 != 0: // AlignHCenter
		return ui.AlignCenter, true
	}
	return ui.AlignStart, false
}

// extractComponentHandlers pulls Component.onCompleted / onDestruction off a
// node. onCompleted handlers are returned as tasks (with their env scope);
// onDestruction is accepted and ignored (no destruction phase in immediate
// mode). When the node has neither, the same node is returned unchanged so
// callers can cheaply detect "nothing to do" by pointer identity.
func extractComponentHandlers(n *node, env *Env) (*node, []completedTask) {
	has := false
	for _, a := range n.attrs {
		if a.name == "Component.onCompleted" || a.name == "Component.onDestruction" {
			has = true
			break
		}
	}
	if !has {
		return n, nil
	}
	var tasks []completedTask
	cp := *n
	cp.attrs = nil
	for _, a := range n.attrs {
		switch a.name {
		case "Component.onCompleted":
			tasks = append(tasks, completedTask{stmts: a.stmt, env: env,
				self: nodeID(n), node: n})
			// (the caller prefixes self with the instance scope; see
			// docState.collectCompleted)
		case "Component.onDestruction":
			// accepted, no-op
		default:
			cp.attrs = append(cp.attrs, a)
		}
	}
	return &cp, tasks
}

// buildLoader implements QML's Loader: it instantiates content from
// `sourceComponent: <Component id>`, an inline `Component { }` / element child,
// when `active` (default true). An inactive or empty Loader is an empty box.
func buildLoader(n *node, env *Env, doc *docState) (ui.Element, error) {
	active := true
	var content *node
	var sourceFile *node
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "active":
			if b, err := attrBool(a, env); err == nil {
				active = b
			}
		case "sourceComponent":
			// Usually an id referring to a Component { } declaration, but
			// any expression yielding one counts: switching views with a
			// ternary (`selected === 0 ? circlesComp : gridComp`) is the
			// whole point of a Loader, and only the bare id used to work.
			// It can also be declared inline, as the attribute's value.
			if a.elem != nil {
				content = unwrapComponent(a.elem)
			} else if c := doc.componentValue(a.expr, env); c != nil {
				content = c
			}
		case "source":
			// A relative file URL: parse + cache the component file and
			// instantiate it as a proper file component (its own declared
			// properties/functions register per instantiation).
			if a.expr != nil {
				if v, err := a.expr.eval(env); err == nil {
					if c := doc.fileComponentAt(str(v)); c != nil {
						sourceFile = c
					}
				}
			}
		case "asynchronous", "visible", "anchors.fill", "anchors.centerIn":
			// accepted, no-op
		default:
			// tolerate unknown attrs
		}
	}
	// An inline child (Component { } or a bare element) overrides sourceComponent.
	for _, c := range n.children {
		if c.typeName == "Component" {
			content = unwrapComponent(c)
		} else if !isNonVisual(c) {
			content = c
		}
	}
	// The Loader's own contract, which pages read and their onStatusChanged
	// handlers compare against: `status === Loader.Ready && item`. `item`
	// is a plain object — enough to be truthy and to take a write — since
	// the loaded element is rebuilt every frame here.
	loaded := content != nil || sourceFile != nil
	status := float64(0) // Loader.Null
	if loaded && active {
		status = 1 // Loader.Ready
	}
	doc.exposeIDProp(n, "status", status)
	if loaded && active {
		doc.exposeIDProp(n, "item", map[string]any{"opacity": float64(1)})
	} else {
		doc.exposeIDProp(n, "item", nil)
	}
	if !active {
		return ui.Div(), nil
	}
	if content != nil {
		return build(content, env, doc)
	}
	if sourceFile != nil {
		// Instantiate through the file-component path (declares the file's
		// own properties/functions) with a bare synthetic use-site so the
		// Loader's own attrs don't leak onto the instance.
		use := &node{typeName: sourceFile.typeName, line: n.line}
		return buildFileComponent(use, sourceFile, env, doc)
	}
	return ui.Div(), nil
}

// componentValue resolves an expression to the node a Component { }
// declaration wraps: a bare id (the common case, resolved without
// evaluating), or any expression whose value is that component.
func (doc *docState) componentValue(e expr, env *Env) *node {
	if e == nil {
		return nil
	}
	if id, ok := e.(*identExpr); ok {
		if c := doc.components[id.name]; c != nil {
			return c
		}
	}
	v, err := e.eval(env)
	if err != nil {
		return nil
	}
	switch c := v.(type) {
	case *componentRef:
		return c.node
	case componentRef:
		return c.node
	case *node:
		return c
	}
	return nil
}

// buildPopup implements QML's floating overlays (Popup/Dialog/Drawer/Menu/
// ToolTip): when open (visible:/open: truthy) the content is built and floated
// above everything via ui.Modal, with a dim scrim (modal popups) that dismisses
// on an outside click by writing the controlling property false. When closed it
// occupies no space. An explicit `x:`/`y:` positions the popup at that point
// (QML popup coordinates) instead of centering it — how Menus open at their
// trigger.
// popupState is a floating overlay's own state: whether it is open, the
// signals it emits, and where it wants to sit. Shared by the builtin popup
// builder and the styled one — a Dialog behaves the same whether its looks
// come from a style file or from us.
type popupState struct {
	open       Ref  // the property backing visible/open
	shown      bool // ... as it reads now
	modal      bool
	title      string
	buttons    float64 // standardButtons bitmask (Dialog)
	px, py     float32
	positioned bool
	fire       func(name string) // run one of the popup's own handlers
}

// popupOrigin is the window position of the item a popup is declared in
// — the frame its x/y are stated in. Zero at the window root, or when the
// parent has not been laid out yet.
//
// A STYLED popup builds with `parent` already pointing at itself (the
// control scope every T.* delegate reads), so that one is skipped for the
// object it hangs off — read as its own parent, a menu walked down the
// window by its own y every frame, because last frame's box was where the
// y had already put it.
func popupOrigin(n *node, env *Env) (x, y float32) {
	p, ok := env.vars["parent"].(*elemObject)
	if !ok || p == nil {
		return 0, 0
	}
	if p.node == n || (p.doc != nil && p.key == p.doc.keyID(n)) {
		p = p.parentObj
	}
	if p == nil || p.el == nil {
		return 0, 0
	}
	pr := ui.Bounds(p.el)
	return pr.X, pr.Y
}

// popupStateOf reads a popup node's overlay attributes and installs its
// imperative API on its id. QML opens a dialog by CALLING it
// (`infoDialog.open()` from a button's onClicked), not by binding a
// property: a Dialog with no `visible:` attribute would otherwise have no
// way to ever show.
func popupStateOf(n *node, env *Env, doc *docState) *popupState {
	st := &popupState{
		open:  doc.boolProp(doc.key(n)+"|open", false),
		modal: n.typeName == "Dialog" || n.typeName == "Menu", // sensible defaults
	}
	handlers := map[string][]stmt{}
	// A popup's x/y are in its PARENT's coordinates — QML places
	// `Menu { y: fileToolBtn.height }` under the button it is declared in,
	// wherever that button sits — and the overlay wants window coordinates.
	// The parent's laid-out box is last frame's; a popup opens a frame after
	// its trigger anyway.
	orgX, orgY := popupOrigin(n, env)
	for _, a := range n.attrs {
		switch a.name {
		case "visible", "open", "opened":
			// A declared binding still wins: a popup whose page drives it
			// with a property keeps that property as its state.
			if r, err := resolveRef(a.expr, env); err == nil {
				st.open = r
			} else if b, err := attrBool(a, env); err == nil && b {
				_ = st.open.set(true)
			}
		case "modal", "dim":
			if b, err := attrBool(a, env); err == nil {
				st.modal = b
			}
		case "title":
			if v, err := a.expr.eval(env); err == nil {
				st.title = str(v)
			}
		case "standardButtons":
			if f, err := attrFloat(a, env); err == nil {
				st.buttons = float64(f)
			}
		case "onAccepted", "onRejected", "onOpened", "onClosed":
			handlers[a.name] = a.stmt
		case "x":
			if f, err := attrFloat(a, env); err == nil {
				st.px, st.positioned = f+orgX, true
			}
		case "y":
			if f, err := attrFloat(a, env); err == nil {
				st.py, st.positioned = f+orgY, true
			}
		}
	}
	if st.positioned {
		// One coordinate stated, the other defaulted: the default is 0 in
		// the parent's frame too.
		if !hasAttr(n, "x") {
			st.px = orgX
		}
		if !hasAttr(n, "y") {
			st.py = orgY
		}
	}
	run := doc.handlerRunner(env)
	st.fire = func(name string) {
		if body := handlers[name]; body != nil {
			run(body, name)
		}
	}
	// open()/close()/accept()/reject() on the popup's id, with the signal
	// each one emits: Qt's Dialog runs onOpened when it shows, and
	// accept()/reject() close it and run onAccepted/onRejected.
	ref := st.open
	fire := st.fire
	openFn := jsFunc(func([]any) (any, error) {
		err := ref.set(true)
		fire("onOpened")
		return nil, err
	})
	doc.exposeIDMethodsIn(n, env, map[string]any{
		"open": openFn,
		// A Menu is opened with popup() as often as with open() — the
		// corpus' own menus all do — optionally at a point, which QML takes
		// in the menu's parent coordinates.
		"popup": jsFunc(func(args []any) (any, error) {
			switch {
			case len(args) >= 2:
				if x, err := toFloat32(args[0]); err == nil {
					if y, err := toFloat32(args[1]); err == nil {
						st.px, st.py, st.positioned = x+orgX, y+orgY, true
					}
				}
			case doc.trigger.H > 0:
				// No point given: at the trigger, as QML opens it at the
				// pointer — which is over the trigger.
				st.px, st.py, st.positioned = doc.trigger.X, doc.trigger.Y+doc.trigger.H, true
			}
			return openFn(nil)
		}),
		"close": jsFunc(func([]any) (any, error) {
			err := ref.set(false)
			fire("onClosed")
			return nil, err
		}),
		"accept": jsFunc(func([]any) (any, error) {
			err := ref.set(false)
			fire("onAccepted")
			fire("onClosed")
			return nil, err
		}),
		"reject": jsFunc(func([]any) (any, error) {
			err := ref.set(false)
			fire("onRejected")
			fire("onClosed")
			return nil, err
		}),
	})
	st.shown, _ = toBool(st.open.get())
	doc.exposeIDProp(n, "opened", st.shown)
	doc.exposeIDProp(n, "visible", st.shown)
	return st
}

// dismiss closes the popup and runs its onClosed — what a click on the
// scrim or an Escape does.
func (st *popupState) dismiss() {
	_ = st.open.set(false)
	st.fire("onClosed")
}

// float wraps a built panel as the floating overlay itself: centred, or at
// the popup's own x/y (QML popup coordinates — how a Menu opens at its
// trigger), dimmed behind when modal.
func (st *popupState) float(panel ui.Element, dimA float32) ui.Element {
	dim := ui.RGBA{}
	if st.modal {
		dim = ui.RGBA{A: dimA}
	}
	dismiss := st.dismiss
	if st.positioned {
		return ui.PositionedModal(panel, st.px, st.py, dim, dismiss)
	}
	return ui.Modal(panel, dim, dismiss)
}

func buildPopup(n *node, env *Env, doc *docState) (ui.Element, error) {
	st := popupStateOf(n, env, doc)
	if n.typeName == "Drawer" {
		return buildDrawer(n, env, doc, st) // slides from its edge (drawer.go)
	}
	// enter/exit transitions (popuptransitions.go): a closing popup with an
	// exit transition stays drawn until it is done.
	pst, draw := doc.popupTransition(n, env, st.shown)
	if !draw {
		return ui.Div(), nil // closed → no flow space, nothing floating
	}
	// A ToolTip is its text: there is nothing else to compose, and `text:`
	// is not an attribute a box builder understands.
	if n.typeName == "ToolTip" && len(n.children) == 0 {
		if txt := popupText(n, env); txt != "" {
			return ui.PositionedModal(tooltipPanel(txt), st.px, st.py, ui.RGBA{}, nil), nil
		}
	}
	// Build the content panel from the node's own styling + children. Strip the
	// overlay-control attrs so buildBox doesn't reject them.
	panel, err := buildBox(stripPopupAttrs(n), env, doc)
	if err != nil {
		return nil, err
	}
	// A panel with no background gets a default popup skin so it reads as a
	// card, in the palette of the page it floats over.
	if pd, ok := panel.(*ui.DivEl); ok {
		if doc.groundDark {
			pd.DarkPopupSkin()
		} else {
			pd.PopupSkin()
		}
	}
	if pd, ok := panel.(*ui.DivEl); ok {
		pst.Apply(pd)
	}
	// A Dialog is a titled panel with a button box under it, and those
	// buttons are the only way its accepted()/rejected() ever fire.
	if n.typeName == "Dialog" && (st.title != "" || st.buttons != 0) {
		panel = ui.DialogFrame(panel, st.closeFn(), st.dialogOpts(doc.groundDark))
	}
	if n.typeName == "ToolTip" { // tooltips never dim / aren't modal
		return ui.PositionedModal(panel, st.px, st.py, ui.RGBA{}, nil), nil
	}
	return st.float(panel, 0.45), nil
}

// noteGround remembers how dark the page is, from the outermost fill in the
// tree just built. A popup with no background of its own takes its skin from
// this: the content inside such a popup is written for the PAGE's palette (a
// dark app's dialog holds light text), so floating it on a light card would
// leave that text invisible.
func (doc *docState) noteGround(root ui.Element) {
	var find func(ui.Element, int) (ui.RGBA, bool)
	find = func(e ui.Element, depth int) (ui.RGBA, bool) {
		if e == nil || depth > 4 {
			return ui.RGBA{}, false
		}
		if c, ok := ui.FillColor(e); ok && c.A > 0.5 {
			return c, true
		}
		for _, ch := range ui.Children(e) {
			if c, ok := find(ch, depth+1); ok {
				return c, true
			}
		}
		return ui.RGBA{}, false
	}
	if c, ok := find(root, 0); ok {
		// Rec. 709 luma, the same measure the renderer's soft masks use.
		doc.groundDark = 0.2126*c.R+0.7152*c.G+0.0722*c.B < 0.4
	}
}

// tooltipPanel is an unstyled ToolTip: Qt's Basic look, a small dark panel
// holding the text, which is all a ToolTip is.
func tooltipPanel(text string) ui.Element { return ui.ToolTip(text) }

// popupText reads a popup's own `text:` (a ToolTip's label).
func popupText(n *node, env *Env) string {
	for _, a := range n.attrs {
		if a.name == "text" && a.expr != nil {
			if v, err := a.expr.eval(env); err == nil {
				return str(v)
			}
		}
	}
	return ""
}

// dialogOpts reads a popup's dialog side into ui.DialogOpts: the signals a
// button emits run the Dialog's own handlers.
func (st *popupState) dialogOpts(dark bool) ui.DialogOpts {
	fire := st.fire
	return ui.DialogOpts{
		Title: st.title, Buttons: ui.StandardButton(int64(st.buttons)), Dark: dark,
		OnAccepted: func() { fire("onAccepted") },
		OnRejected: func() { fire("onRejected") },
		OnClosed:   func() { fire("onClosed") },
	}
}

// closeFn closes the popup without a signal (the dialog's buttons emit
// their own).
func (st *popupState) closeFn() func() {
	ref := st.open
	return func() { _ = ref.set(false) }
}

// stripPopupAttrs returns a copy of n without the overlay-control attributes
// (visible/open/modal/closePolicy/...) that buildBox doesn't understand.
func stripPopupAttrs(n *node) *node {
	cp := *n
	cp.typeName = "Item" // build as a plain box
	cp.attrs = nil
	for _, a := range n.attrs {
		switch a.name {
		case "visible", "open", "opened", "modal", "dim", "closePolicy",
			"x", "y", "anchors.centerIn", "parent", "focus", "title",
			// A Dialog's button box and its signals: buildPopup draws the
			// buttons and runs the handlers, so the content box never sees
			// them (and used to refuse the whole dialog over them).
			"standardButtons", "onAccepted", "onRejected", "onOpened", "onClosed",
			"enter", "exit":
			// overlay/positioning control — handled by buildPopup
		default:
			cp.attrs = append(cp.attrs, a)
		}
	}
	return &cp
}

// evalGradientElem reads a QML `Gradient { GradientStop { position; color } }`
// element into a render.Ramp (up to 4 positioned stops; more collapse to the
// first/last four evenly) and an angle. QML's default Gradient runs
// top→bottom (vertical); orientation: Gradient.Horizontal runs left→right.
// Coincident stops give a hard color split.
// evalGradientElem reads a Gradient element: the four-stop ramp a quad can
// carry, EVERY stop (for the banded path when there are more than four),
// and the gradient's angle.
func evalGradientElem(g *node, env *Env) (render.Ramp, []render.GradientStop, float32, error) {
	if g.typeName != "Gradient" {
		return render.Ramp{}, nil, 0, fmt.Errorf("gml:%d: expected Gradient, got %q", g.line, g.typeName)
	}
	ang := float32(3.14159265 / 2) // vertical (top→bottom) by default
	for _, a := range g.attrs {
		if a.name == "orientation" {
			if v, err := a.expr.eval(env); err == nil {
				if s := str(v); s == "Gradient.Horizontal" || s == "Horizontal" || s == "1" {
					ang = 0
				}
			}
		}
	}
	type stop struct {
		pos float32
		col ui.RGBA
	}
	var stops []stop
	for _, c := range g.children {
		if c.typeName != "GradientStop" {
			continue
		}
		var s stop
		for _, a := range c.attrs {
			switch a.name {
			case "position":
				f, _ := attrFloat(a, env)
				s.pos = f
			case "color":
				v, err := a.expr.eval(env)
				if err != nil {
					return render.Ramp{}, nil, 0, err
				}
				col, err := parseColor(str(v))
				if err != nil {
					return render.Ramp{}, nil, 0, err
				}
				s.col = col
			}
		}
		stops = append(stops, s)
	}
	if len(stops) == 0 {
		return render.Ramp2(ui.RGBA{A: 1}, ui.RGBA{A: 1}), nil, ang, nil
	}
	// Keep declaration order (stops are authored ascending in QML); more than
	// 4 collapse to the first three plus the last.
	sort.SliceStable(stops, func(i, j int) bool { return stops[i].pos < stops[j].pos })
	if len(stops) == 1 {
		stops = append(stops, stops[0])
	}
	var ramp render.Ramp
	ramp.Count = min(len(stops), 4)
	for i, s := range stops[:ramp.Count] {
		ramp.Colors[i] = s.col
		ramp.Stops[i] = s.pos
	}
	out := make([]render.GradientStop, len(stops))
	for i, s := range stops {
		out[i] = render.GradientStop{Pos: s.pos, Col: s.col}
	}
	return ramp, out, ang, nil
}

// handlerFor returns the statements of a node's named on-handler, or nil.
func handlerFor(n *node, attrName string) []stmt {
	for _, a := range n.attrs {
		if a.name == attrName {
			return a.stmt
		}
	}
	return nil
}

// stripConsumedHandlers removes a node's signal on-handlers and
// on<Prop>Changed handlers before its element builder runs (they belong to
// the signal / change mechanisms, not the element's built-in attributes).
func stripConsumedHandlers(n *node) *node {
	if len(n.signals) == 0 && len(n.props) == 0 {
		return n // nothing to strip
	}
	skip := map[string]bool{}
	for _, s := range n.signals {
		skip["on"+title(s.name)] = true
	}
	for _, p := range n.props {
		skip["on"+title(p.name)+"Changed"] = true
	}
	cp := *n
	cp.attrs = nil
	for _, a := range n.attrs {
		if !skip[a.name] {
			cp.attrs = append(cp.attrs, a)
		}
	}
	return &cp
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// selfGeomScope resolves bare `width`/`height` identifiers used in a
// node's OTHER attributes (e.g. `radius: width / 2`) against that same
// node's own `width:`/`height:` attribute values — real QML property
// lookup starts at `this`, so a bare geometry name always means the
// element's own size, never a parent's or child's. Returns env
// unchanged when the node sets neither.
func selfGeomScope(n *node, env *Env) *Env {
	var w, h expr
	for _, a := range n.attrs {
		switch a.name {
		case "width":
			w = a.expr
		case "height":
			h = a.expr
		}
	}
	if w == nil && h == nil {
		return env
	}
	scope := env.child()
	// These are the item's OWN attribute expressions evaluated in place —
	// derived from tracked inputs, not injected state — so reading them
	// back must not poison the subtree's read-set (see Env.derived).
	scope.derived = true
	if w != nil {
		if v, err := w.eval(env); err == nil {
			scope.vars["width"] = v
		}
	}
	if h != nil {
		if v, err := h.eval(env); err == nil {
			scope.vars["height"] = v
		}
	}
	return scope
}

// boxAttrs applies the shared QML-ish attribute set for container/visual
// elements onto a Div.
func boxAttrs(n *node, d *ui.DivEl, env *Env, doc *docState) error {
	env = selfGeomScope(n, env)
	if n.typeName == "MouseArea" {
		env = doc.mouseAreaScope(n, env)
	}
	env = doc.focusScope(n, env)
	if n.typeName == "FocusScope" {
		d.FocusScope() // focus sent to it goes inside (focus.go)
	}
	var borderW float32
	var borderDashed bool
	var borderC ui.RGBA
	var onClicked, keysPressed, keysReleased []stmt
	var keysSpecific map[float64][]stmt // Keys.onLeftPressed and friends, by Qt key code
	var onEntered, onExited, onPressed, onReleased []stmt
	var gestures mouseGestures // MouseArea's double-click, hold, wheel, position (mousearea.go)
	var drag dragAttrs         // MouseArea drag.target and its limits (dragtarget.go)
	// rotation/scale apply about transformOrigin, which may be declared
	// after them, so they are collected and applied at the end.
	var rotateDeg, scaleBy float32
	var originName string
	hasBorder := false
	id := nodeID(n)
	// Behavior on <prop> declarations on this element tween its own property
	// changes. Scoped to this element while its attrs are applied.
	prevBeh, prevKey := doc.curBehaviors, doc.curKey
	var navs []keyNav // KeyNavigation.* (focus.go)
	doc.curBehaviors, doc.curKey = collectBehaviors(n, env), doc.key(n)
	defer func() { doc.curBehaviors, doc.curKey = prevBeh, prevKey }()
	for _, a := range n.attrs {
		if doc.readKeyNav(a, env, &navs) {
			continue
		}
		switch a.name {
		case "drag.target", "drag.axis", "drag.threshold",
			"drag.minimumX", "drag.maximumX", "drag.minimumY", "drag.maximumY",
			"drag.handler", "drag.xEnabled", "drag.yEnabled",
			"drag.onTranslationChanged", "drag.onActiveChanged":
			drag.read(a)
		case "id": // accepted everywhere, no state for plain boxes
		case "state", "states", "transitions": // handled by computeStateCtx
		case "implicitWidth", "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			// A size expression reading not-yet-settled geometry can come
			// out negative (`Math.min(width, height - 20)` on frame one);
			// QML clamps item sizes at zero, so do the same rather than
			// laying out an inverted box.
			d.W(max(doc.numAttr(id, "width", f), 0))
		case "implicitHeight", "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			d.H(max(doc.numAttr(id, "height", f), 0))
		case "background":
			// A Controls container's background delegate (`ToolBar {
			// background: Rectangle { color: ...; radius: ... } }`). The
			// delegate always fills the control, so its fill and frame
			// apply to this box directly.
			if a.elem != nil {
				applyBackgroundDelegate(a.elem, d, env)
			}
		case "color":
			c, err := attrColor(a, env)
			if err != nil {
				return err
			}
			d.Bg(c)
		case "radius":
			f, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			d.Rounded(doc.numAttr(id, "radius", f))
		case "border.width":
			f, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			borderW, hasBorder = f, true
		case "border.color":
			c, err := attrColor(a, env)
			if err != nil {
				return err
			}
			borderC, hasBorder = c, true
		case "border.style":
			if v, err := a.expr.eval(env); err == nil {
				borderDashed = str(v) == "dashed" || str(v) == "dotted"
			}
		case "spacing":
			f, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			d.Gap(f)
		case "padding":
			f, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			d.P(f)
		case "justify": // extension (flexbox)
			al, err := attrAlign(a, env)
			if err != nil {
				return err
			}
			d.Justify(al)
		case "items": // extension (flexbox)
			al, err := attrAlign(a, env)
			if err != nil {
				return err
			}
			d.Items(al)
		case "grow": // extension (flexbox)
			b, err := attrBool(a, env)
			if err != nil {
				return err
			}
			if b {
				d.Grow()
			}
		case "hoverColor": // extension
			c, err := attrColor(a, env)
			if err != nil {
				return err
			}
			d.HoverBg(c)
		case "pressColor": // extension
			c, err := attrColor(a, env)
			if err != nil {
				return err
			}
			d.PressBg(c)
		case "shadow": // extension: [blur, ox, oy, color]
			v, err := a.expr.eval(env)
			if err != nil {
				return err
			}
			list, ok := v.([]any)
			if !ok || len(list) != 4 {
				return fmt.Errorf("gml:%d: shadow wants [blur, ox, oy, color]", a.line)
			}
			blur, _ := toFloat32(list[0])
			ox, _ := toFloat32(list[1])
			oy, _ := toFloat32(list[2])
			c, err := parseColor(str(list[3]))
			if err != nil {
				return err
			}
			d.Shadow(blur, ox, oy, c)
		case "opacity":
			f, err := attrFloat(a, env)
			if err != nil {
				return err
			}
			d.Opacity(doc.numAttr(id, "opacity", f))
		case "gradient":
			// QML form: gradient: Gradient { GradientStop { position; color } ... }
			if a.elem != nil {
				ramp, stops, ang, err := evalGradientElem(a.elem, env)
				if err != nil {
					return err
				}
				// More than four stops cannot ride on a quad: those are
				// drawn band by band (QML's Gradient is axis-aligned, so a
				// band per segment is exact).
				if len(stops) > 4 {
					d.GradientStopsN(stops, ang)
				} else {
					d.GradientRamp(ramp, ang)
				}
				continue
			}
			// Extension form: [color0, color1, angleDegrees].
			v, err := a.expr.eval(env)
			if err != nil {
				return err
			}
			list, ok := v.([]any)
			if !ok || len(list) != 3 {
				return fmt.Errorf("gml:%d: gradient wants [color0, color1, angleDegrees]", a.line)
			}
			c0, err := parseColor(str(list[0]))
			if err != nil {
				return err
			}
			c1, err := parseColor(str(list[1]))
			if err != nil {
				return err
			}
			ang, _ := toFloat32(list[2])
			d.Gradient(c0, c1, ang*3.14159265/180)
		case "onClicked":
			onClicked = a.stmt
		case "Keys.onPressed":
			keysPressed = a.stmt
		case "Keys.onReleased":
			keysReleased = a.stmt
		case "Keys.onLeftPressed", "Keys.onRightPressed", "Keys.onUpPressed", "Keys.onDownPressed",
			"Keys.onEscapePressed", "Keys.onSpacePressed", "Keys.onTabPressed", "Keys.onBacktabPressed",
			"Keys.onDeletePressed", "Keys.onReturnPressed", "Keys.onEnterPressed",
			"Keys.onDigit0Pressed", "Keys.onDigit1Pressed", "Keys.onDigit2Pressed", "Keys.onDigit3Pressed",
			"Keys.onDigit4Pressed", "Keys.onDigit5Pressed", "Keys.onDigit6Pressed", "Keys.onDigit7Pressed",
			"Keys.onDigit8Pressed", "Keys.onDigit9Pressed":
			if keysSpecific == nil {
				keysSpecific = map[float64][]stmt{}
			}
			keysSpecific[keySignalCode(a.name)] = a.stmt
		case "focus":
			if b, err := attrBool(a, env); err == nil && b {
				d.Focus()
			}
		case "activeFocusOnTab": // a plain item joins the Tab chain
			if b, err := attrBool(a, env); err == nil && b {
				d.Focusable()
			}
		case "onEntered":
			onEntered = append(onEntered, a.stmt...)
		case "onExited":
			onExited = append(onExited, a.stmt...)
		case "onContainsMouseChanged", "onHoveredChanged":
			// containsMouse flips on both transitions, so the handler runs
			// on either.
			onEntered = append(onEntered, a.stmt...)
			onExited = append(onExited, a.stmt...)
		case "onPressed":
			onPressed = a.stmt
		case "onReleased":
			onReleased = a.stmt
		case "onDoubleClicked":
			gestures.doubleClicked = a.stmt
		case "onPressAndHold":
			gestures.pressAndHold = a.stmt
		case "onWheel":
			gestures.wheel = a.stmt
		case "onPositionChanged":
			gestures.positionChanged = a.stmt
		case "cursorShape":
			if v, err := a.expr.eval(env); err == nil {
				d.Cursor(cursorFromQt(v))
			}
		case "orientation":
			// SplitView/Tumbler-style containers: Qt.Vertical stacks, Qt.
			// Horizontal lays out in a row (the enum is 1/2 as in Qt).
			if v, err := a.expr.eval(env); err == nil {
				if f, err := toFloat64(v); err == nil && int(f) == 2 {
					d.Col()
				} else {
					d.Row()
				}
			}
		case "rotation":
			// QML rotates an item about its transformOrigin (default
			// Center) for PAINTING only — layout keeps the unrotated box.
			if f, err := attrFloat(a, env); err == nil && f != 0 {
				rotateDeg = f
			}
		case "scale":
			if f, err := attrFloat(a, env); err == nil && f != 1 {
				scaleBy = f
			}
		case "transformOrigin":
			if v, err := a.expr.eval(env); err == nil {
				originName = str(v)
			}
		case "transform":
			// A transform LIST (Translate/Rotation/Scale elements). QML
			// applies it right-to-left, which is the order ui composes in.
			for _, t := range readTransformList(a, env, doc) {
				d.Transform(t)
			}
		case "clip":
			// QML clips a box's children to its own rect (a card cutting
			// off the decorations that bleed past its edge).
			if b, err := attrBool(a, env); err == nil && b {
				d.Clip()
			}
		case "enabled", "z", "antialiasing", "smooth",
			"visible", "objectName",
			// `parent: someItem` re-parents an item in QML. Ours is laid
			// out where it was declared; a style that re-parents its
			// selection band onto the background means the control's box
			// either way.
			"parent",
			"layer.enabled", "layer.effect", "layer.samples",
			// manual x/y on in-flow children: positioning is layout-owned.
			"x", "y", "baselineOffset",
			// geometry/focus change signals: our items have no per-property
			// signal objects, and every binding re-evaluates each frame, so
			// a handler that only mirrors geometry has nothing to do.
			"onXChanged", "onYChanged", "onWidthChanged", "onHeightChanged",
			"onVisibleChanged", "onOpacityChanged", "onActiveFocusChanged",
			"onScaleChanged", "onRotationChanged", "onEnabledChanged",
			// window/control chrome we don't model:
			"handle", "contentItem",
			"title", "flags", "modality", "visibility", "spacing.bottom",
			"hoverEnabled", "focusPolicy", "padding.left", "padding.right",
			"padding.top", "padding.bottom", "leftPadding", "rightPadding",
			"topPadding", "bottomPadding",
			// MouseArea extras not modeled (drag.* is dragtarget.go):
			"preventStealing", "onActiveChanged", "onGrabChanged", "onCanceled",
			"drag.filterChildren", "drag.smoothed",
			"acceptedButtons", "propagateComposedEvents",
			// Axis limits of the handlers not rewritten to drag.* (a
			// DragHandler's are: pointerHandlerAsMouseArea):
			"xAxis.minimum", "xAxis.maximum", "xAxis.enabled",
			"yAxis.minimum", "yAxis.maximum", "yAxis.enabled",
			// Input Handler (TapHandler/HoverHandler, hoisted here) extras:
			"gesturePolicy", "grabPermissions", "acceptedDevices",
			"margin", "longPressThreshold", "target", "point.id",
			// PinchHandler's and PointHandler's reports (a DragHandler's
			// onTranslationChanged is rewritten to drag.*):
			"onTranslationChanged", "onCentroidChanged":
			// accepted, not modeled (kept so real .qml files load)
		default:
			return fmt.Errorf("gml:%d: %s has no attribute %q", a.line, n.typeName, a.name)
		}
	}
	// QML's rotation/scale act about transformOrigin (Center by default).
	if rotateDeg != 0 || scaleBy != 0 {
		ox, oy := transformOriginFrac(originName)
		if rotateDeg != 0 {
			d.Transform(ui.Transform{Kind: ui.XformRotate, Angle: rotateDeg,
				Rel: ox == 0.5 && oy == 0.5, OriginFracX: ox, OriginFracY: oy})
		}
		if scaleBy != 0 && scaleBy != 1 {
			d.Transform(ui.Transform{Kind: ui.XformScale, SX: scaleBy, SY: scaleBy,
				Rel: ox == 0.5 && oy == 0.5, OriginFracX: ox, OriginFracY: oy})
		}
	}
	if hasBorder {
		if borderW == 0 {
			borderW = 1
		}
		d.Border(borderW, borderC)
		if borderDashed {
			d.BorderDashed()
		}
	}
	// MouseArea/Rectangle onClicked + any Connections targeting this id.
	connected := doc.connectedHandler(id, "onClicked", env)
	if onClicked != nil || connected != nil {
		run := doc.handlerRunner(env)
		d.OnClick(func() {
			doc.noteTrigger(d)
			if onClicked != nil {
				run(onClicked, "onClicked")
			}
			if connected != nil {
				connected()
			}
		})
	}
	for _, kn := range navs {
		d.KeyNav(kn.dir, kn.id)
	}
	// Keys.onPressed / Keys.onReleased → focusable raw key handler.
	if kh := keyHandlerWith(keysSpecific, keysPressed, keysReleased, env); kh != nil {
		d.OnKeyAccept(kh)
	}
	doc.wireShortcuts(n, d, env)
	doc.wireHoverPress(n, d, env, onEntered, onExited, onPressed, onReleased)
	if gestures.any() || n.typeName == "MouseArea" {
		gestures.hoverEnabled = attrIsTrue(n, "hoverEnabled", env)
		_, pressRef := doc.hoverPressRefs(doc.keyID(n))
		doc.wireMouseGestures(n, d, env, &gestures, pressRef)
	}
	doc.wireDragTarget(n, d, env, &drag)
	return nil
}

// wireHoverPress gives any box live containsMouse/pressed state (a real
// MouseArea's Q_PROPERTY, exposed as id.containsMouse / id.pressed) and fires
// onEntered/onExited/onPressed/onReleased on the actual state transition —
// the element tree is rebuilt every frame, so the transition edge is tracked
// on the persistent Ref (doc.boolProp), not on the transient *ui.DivEl.
func (doc *docState) wireHoverPress(n *node, d *ui.DivEl, env *Env, onEntered, onExited, onPressed, onReleased []stmt) {
	if nodeID(n) == "" && onEntered == nil && onExited == nil && onPressed == nil && onReleased == nil {
		// Nothing can read this box's state (no id to read it by) and
		// nothing reacts to it: no refs, no callbacks. Most boxes.
		return
	}
	key := doc.keyID(n)
	hoverRef, pressRef := doc.hoverPressRefs(key)
	run := doc.handlerRunner(env)
	doc.exposeIDProp(n, "containsMouse", hoverRef)
	doc.exposeIDProp(n, "hovered", hoverRef)
	doc.exposeIDProp(n, "pressed", pressRef)
	d.OnHoverChanged(func(v bool) {
		old, _ := toBool(hoverRef.get())
		if v == old {
			return
		}
		_ = hoverRef.set(v)
		var stmts []stmt
		if v {
			stmts = onEntered
		} else {
			stmts = onExited
		}
		if stmts != nil {
			run(stmts, map[bool]string{true: "onEntered", false: "onExited"}[v])
		}
	})
	d.OnPressChanged(func(v bool) {
		old, _ := toBool(pressRef.get())
		if v == old {
			return
		}
		_ = pressRef.set(v)
		var stmts []stmt
		if v {
			stmts = onPressed
		} else {
			stmts = onReleased
		}
		if stmts != nil {
			run(stmts, map[bool]string{true: "onPressed", false: "onReleased"}[v])
		}
	})
}

// hoverPressRefs returns an element key's containsMouse and pressed refs —
// the same doc.boolProp slots as `<key>.containsMouse` / `<key>.pressed`,
// found without building those strings on every rebuild.
func (doc *docState) hoverPressRefs(key keyID) (hover, press Ref) {
	if hp, ok := doc.hpRefs.get2(key); ok {
		return hp.hover, hp.press
	}
	ks := doc.keyStr(key)
	hp := hoverPress{doc.boolProp(ks+".containsMouse", false), doc.boolProp(ks+".pressed", false)}
	doc.hpRefs.set(key, hp)
	return hp.hover, hp.press
}

type hoverPress struct{ hover, press Ref }

// cursorFromQt maps a Qt.XxxCursor enum value to the engine's cursor set
// (CursorDefault/Pointer/Text only exist today); unrecognized shapes fall
// back to the default arrow rather than rejecting the element.
func cursorFromQt(v any) app.Cursor {
	f, _ := toFloat64(v)
	switch f {
	case qtCursorPointingHand:
		return app.CursorPointer
	case qtCursorIBeam:
		return app.CursorText
	default:
		return app.CursorDefault
	}
}

// isPositioner reports types that lay out their children (Row/Column/Grid and
// the Qt Quick Layouts); anchors inside these are ignored, as in QML.
// isLayoutType reports the QtQuick.Layouts containers, whose items share
// the available space — unlike Row/Column, which simply place items.
func isLayoutType(t string) bool {
	switch t {
	case "RowLayout", "ColumnLayout", "GridLayout":
		return true
	}
	return false
}

// layoutDefaultSpacing is QtQuick.Layouts' default `spacing`.
const layoutDefaultSpacing = 5

func isPositioner(t string) bool {
	switch t {
	case "Row", "Column", "Grid", "RowLayout", "ColumnLayout", "GridLayout":
		return true
	}
	return false
}

// buildGrid lays children out in a grid of `columns` (default 4, QML's
// default), filling left-to-right then top-to-bottom, with optional row /
// column spacing. Modeled as a Column of Rows.
func buildGrid(n *node, env *Env, doc *docState) (ui.Element, error) {
	columns := 4
	rows := 0
	hasColumns := false
	var spacing, rowSpacing, colSpacing, padding float32
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "columns":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			if f >= 1 {
				columns, hasColumns = int(f), true
			}
		case "rows":
			// With no explicit columns, rows determines them (QML: items flow
			// to fill rows first, so columns = ceil(n/rows)).
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			if f >= 1 {
				rows = int(f)
			}
		case "spacing":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			spacing = f
		case "rowSpacing":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			rowSpacing = f
		case "columnSpacing":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			colSpacing = f
		case "padding":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			padding = f
		case "width", "height", "anchors.fill", "flow", "visible":
			// Explicit sizing/positioning hints; the grid already sizes
			// from its parent's layout slot (Layout.fillWidth etc.), so
			// these are accepted as no-ops rather than rejecting the
			// element outright.
		default:
			return nil, fmt.Errorf("gml:%d: %s has no attribute %q", a.line, n.typeName, a.name)
		}
	}
	if rowSpacing == 0 {
		rowSpacing = spacing
	}
	if colSpacing == 0 {
		colSpacing = spacing
	}
	// A grid places children in cells itself, so an item's Layout.fill*
	// means "fill your cell" on BOTH axes rather than the row/column
	// grow-vs-stretch split a linear layout wants.
	prevGrid := doc.layoutGrid
	doc.layoutGrid = true
	kids, err := buildChildren(n, env, doc, true)
	doc.layoutGrid = prevGrid
	if err != nil {
		return nil, err
	}
	// Layout.columnSpan / rowSpan, in child order (Repeater rows expand
	// inline, so the spans line up with the built elements).
	spans := gridSpans(n, env, len(kids))
	// rows without columns: derive the column count so the items fill that
	// many rows (QML Grid).
	if !hasColumns && rows > 0 {
		columns = (len(kids) + rows - 1) / rows
		if columns < 1 {
			columns = 1
		}
	}
	g := ui.Grid(columns, 0).GapXY(colSpacing, rowSpacing)
	if n.typeName == "Grid" {
		// QML's plain Grid is a POSITIONER: it places items at their own
		// size in a grid of cells and never resizes them. Only GridLayout
		// shares space out.
		g.Positioned()
	}
	for i, el := range kids {
		span := [2]int{1, 1}
		if i < len(spans) {
			span = spans[i]
		}
		g.Cell(el, span[0], span[1])
	}
	if padding > 0 {
		return ui.Div().P(padding).Grow().Children(g), nil
	}
	return g, nil
}

// gridSpans reads each visual child's Layout.columnSpan / Layout.rowSpan.
func gridSpans(n *node, env *Env, kids int) [][2]int {
	out := make([][2]int, 0, kids)
	var walk func(*node)
	walk = func(p *node) {
		for _, c := range p.children {
			switch {
			case isBareMouseArea(c) || isPointerHandler(c) || isNonVisual(c):
				continue
			case c.typeName == "Repeater":
				// Rows expand inline; each carries the delegate's spans.
				walk(c)
				continue
			}
			span := [2]int{1, 1}
			for _, a := range c.attrs {
				switch a.name {
				case "Layout.columnSpan":
					if f, err := attrFloat(a, env); err == nil && f >= 1 {
						span[0] = int(f)
					}
				case "Layout.rowSpan":
					if f, err := attrFloat(a, env); err == nil && f >= 1 {
						span[1] = int(f)
					}
				}
			}
			out = append(out, span)
		}
	}
	walk(n)
	for len(out) < kids {
		out = append(out, [2]int{1, 1})
	}
	return out
}

// parentObject is the `parent` scope object a container exposes to its
// children: its previous-frame geometry (width/height/x/y) plus its own
// declared properties, so `parent.bSize` resolves the way QML resolves a
// property read on the parent item.
func parentObject(n *node, env *Env, doc *docState) *elemObject {
	// doc/key make the object's geometry reads TRACKABLE (see trackGeom):
	// a parent object is made per build, so a token has to be the
	// document's, keyed like the element.
	key := doc.keyID(n)
	if len(n.props) == 0 {
		// One object per key, re-pointed at the frame's element: a
		// handler that captured `parent` then reads the current frame's
		// geometry through it, and a rebuild no longer allocates one for
		// every box. (A node with properties of its own copies them per
		// build below, and keeps a fresh object.)
		pe := doc.parentObjs.get(key)
		if pe == nil {
			pe = &elemObject{doc: doc, key: key}
			doc.parentObjs.set(key, pe)
		}
		pe.el, pe.node, pe.nodeEnv = doc.prevElems.get(key), n, env
		pe.parentObj, pe.natW, pe.natH = nil, 0, 0
		pe.parentKey = ownerKey(n, doc)
		pe.props, pe.methods = nil, nil
		if id := nodeID(n); id != "" {
			if obj := doc.elemObjs.get(doc.here(id)); obj != nil {
				pe.props, pe.methods = obj.props, obj.methods
			}
		}
		return pe
	}
	pe := &elemObject{el: doc.prevElems.get(key), node: n, nodeEnv: env, doc: doc, key: key, parentKey: ownerKey(n, doc)}
	if id := nodeID(n); id != "" {
		if obj := doc.elemObjs.get(doc.here(id)); obj != nil {
			// Share the live maps: same refs, so writes stay visible.
			pe.props, pe.methods = obj.props, obj.methods
			return pe
		}
	}
	if len(n.props) == 0 {
		return pe
	}
	pe.props = make(map[string]any, len(n.props))
	for _, d := range n.props {
		// Refs stay refs (writes through parent.prop must be visible); a
		// plain value is a delegate's `required property` filled from the
		// model row, which `parent.shortName` reads just as directly.
		if v, ok := env.lookup(d.name); ok {
			pe.setProp(d.name, v)
		}
	}
	return pe
}

func buildBox(n *node, env *Env, doc *docState) (ui.Element, error) {
	// QML visible: a hidden box occupies no space and hits nothing, but
	// stays in the document (re-evaluated every build, so bindings can
	// show it again). Popup types handle visible/open themselves.
	for _, a := range n.attrs {
		if a.name != "visible" {
			continue
		}
		if v, err := attrBool(a, env); err == nil && !v {
			return ui.Div(), nil
		}
	}
	d := ui.Div()
	switch n.typeName {
	case "Row", "RowLayout", "ToolBar", "TabBar", "Flow":
		d.Row()
		if n.typeName == "Flow" {
			d.Wrap() // a Flow is a row that starts a new line when it runs out
		}
	default: // Column, Rectangle, Item, MouseArea, ColumnLayout, Pane, ...
		d.Col()
	}
	// QtQuick.Layouts space their items by 5px unless told otherwise (the
	// Row/Column positioners pack theirs). An icon and its label in a bare
	// RowLayout are 5px apart in Qt and were touching here.
	if isLayoutType(n.typeName) {
		d.Gap(layoutDefaultSpacing)
		// Laid out by Qt's rules rather than plain flex (see SpreadCells):
		// a column shares its leftover across its items, a row packs left,
		// and both are laid out in the space they REQUIRE.
		d.SpreadCells()
		// A RowLayout centres the items that keep their own height (a label
		// beside a slider sits level with it) while the ones that fill the
		// row still fill it.
		if n.typeName == "RowLayout" {
			d.CrossCenter()
		}
	}
	// Themed Controls containers default to the Qt light-theme background
	// (an explicit color: attribute below overrides it).
	switch n.typeName {
	case "Pane", "Frame", "GroupBox", "Page", "Control",
		"ApplicationWindow", "Window":
		d.Bg(ui.RGB(0xf0f0f0))
	case "ToolBar":
		d.Bg(ui.RGB(0xe4e4e4))
	}
	if err := boxAttrs(n, d, env, doc); err != nil {
		return nil, err
	}
	// A bare MouseArea child (no geometry, no children) means "cover this
	// box": hoist its handlers/cursor onto the box itself. Laid out as a
	// child it would be zero-size — painted nowhere and hit-tested nowhere.
	// Input Handlers (TapHandler/HoverHandler) attach to their parent item
	// by definition — same hoist, with their signal names mapped.
	for _, c := range n.children {
		if isBareMouseArea(c) {
			if err := boxAttrs(c, d, env, doc); err != nil {
				return nil, err
			}
		} else if isPointerHandler(c) {
			// Inside a handler `parent` is the item it sits in — this box
			// — where this scope's `parent` is one level further out.
			var err error
			withVar(env, "parent", parentObject(n, env, doc), func() {
				if isWiredHandler(c) {
					doc.wireInputHandler(c, n, d, env)
					return
				}
				err = boxAttrs(pointerHandlerAsMouseArea(c, n, doc), d, env, doc)
			})
			if err != nil {
				return nil, err
			}
		}
	}
	// A non-positioner container (Item/Rectangle/MouseArea/...) with anchored
	// or x/y children is an absolute anchor layout rather than a stack.
	// QML places every child of an Item/Rectangle at (0,0) unless it is
	// anchored or given x/y — an Item is not a layout. gml's own dialect
	// treats a box as a flex container instead (that is what `justify:`,
	// `items:` and `spacing:` are for, and the gpui parity suite is written
	// that way), so the QML rule applies to QML documents: .qml files, where
	// matching Qt is the whole point.
	anchorMode := !isPositioner(n.typeName) &&
		(hasAnchorChildren(n, doc) || (doc.lenient && !hasFlexAttrs(n)))
	if anchorMode {
		d.AnchorMode()
		if isControlsContainer(n.typeName) {
			d.AnchorFit()
		}
	}
	// If this box declares states, its active overrides/transitions scope
	// applies to its children; restore the parent scope afterwards.
	prev := doc.state
	ctx, err := computeStateCtx(n, env, doc)
	if err != nil {
		return nil, err
	}
	if ctx != nil {
		doc.state = ctx
	}
	// Expose this container as `parent` to its children (geometry from its
	// previous-frame element); restore the enclosing parent afterwards.
	prevParent, hadParent := env.vars["parent"]
	pobj := parentObject(n, env, doc)
	// `parent.parent` has to keep walking up: QML handlers reach the card
	// they sit in that way (`parent.parent.originMode = 1`). The enclosing
	// parent is whatever `parent` meant one level out.
	if po, ok := prevParent.(*elemObject); ok && hadParent {
		pobj.parentObj = po
	}
	env.vars["parent"] = pobj

	var els []ui.Element
	if anchorMode {
		els, err = buildAnchorChildren(n, env, doc)
	} else {
		els, err = buildChildren(n, env, doc, d.IsRow())
	}

	// Items a ParentChange moves under this one, built as its own children.
	// An empty plain box places them by their x/y, as Qt's Item would.
	if moved := doc.movedInto(n); len(moved) > 0 && err == nil && !anchorMode && len(els) == 0 && !isPositioner(n.typeName) && !isLayoutType(n.typeName) {
		anchorMode = true
		d.AnchorMode()
	}
	// Items a ParentChange moves under this one, built as its own children.
	if err == nil {
		for _, mv := range doc.movedInto(n) {
			var el ui.Element
			var e error
			if anchorMode {
				el, e = buildAnchoredChild(mv, env, doc)
			} else {
				el, e = build(mv, env, doc)
			}
			if e != nil {
				err = e
				break
			}
			els = append(els, el)
		}
	}
	if hadParent {
		env.vars["parent"] = prevParent
	} else {
		delete(env.vars, "parent")
	}
	doc.state = prev
	if err != nil {
		return nil, err
	}
	d.Children(els...)
	return d, nil
}

// refreshNonVisual services a declaration-only child: the elements that
// render nothing but still do something each frame (timers, bindings,
// animations, Go-backed objects). A non-visual node never reaches
// buildFresh, so its Component.onCompleted is collected here too —
// `DatabaseManager { Component.onCompleted: openDatabase() }` is how a
// backend gets started.
func (doc *docState) refreshNonVisual(c *node, env *Env) {
	doc.collectCompleted(c, env)
	switch c.typeName {
	case "Timer":
		doc.refreshTimer(c, env)
	case "Binding":
		doc.applyBinding(c, env)
	case "FrameAnimation":
		doc.refreshFrameAnimation(c, env)
	case "SystemPalette":
		if id := nodeID(c); id != "" {
			seedSystemPalette(c, env, doc.elemObjs.get(doc.intern(id)))
		}
	}
	if isAnimation(c.typeName) {
		doc.refreshAnimator(c, env)
	}
	if isNativeType(c.typeName) {
		doc.syncNative(c, env)
	}
	switch c.typeName {
	case "MediaPlayer":
		doc.refreshMediaPlayer(c, env)
	case "CaptureSession":
		doc.refreshCaptureSession(c, env)
	case "MediaDevices":
		doc.refreshMediaDevices(c, env)
	case "PdfDocument":
		doc.refreshPdfDocument(c, env)
	}
}

// hiddenChild reports a child a LAYOUT or POSITIONER must leave out
// entirely: QML gives an item with `visible: false` no slot and no spacing
// either (`Layout.fillHeight` neighbours take the whole surplus), where an
// empty element in its place still eats a gap — the diorama's collapsed
// maths panel cost its scene 12 pixels of height that way.
//
// Only a layout skips it. Everywhere else an invisible item still EXISTS,
// with geometry: the shaders page hides the rounded square that its three
// MultiEffects take as their source and anchor to, and dropping it left
// the effects sourceless and filling the whole card. Overlay types keep
// their own visible/open handling.
func hiddenChild(c *node, env *Env, doc *docState) bool {
	if isOverlayType(c.typeName) {
		return false
	}
	c = doc.withAttrOverrides(c)
	for i := len(c.attrs) - 1; i >= 0; i-- {
		if c.attrs[i].name != "visible" {
			continue
		}
		v, err := attrBool(c.attrs[i], env)
		return err == nil && !v
	}
	return false
}

// buildChildren builds a node's child elements, expanding Repeaters inline
// (QML semantics: a Repeater injects its rows into the parent) and skipping
// non-visual declarations like ListModel.
func buildChildren(parent *node, env *Env, doc *docState, row bool) ([]ui.Element, error) {
	// Tell the children which way this layout runs, so an item's
	// Layout.fillWidth/fillHeight lands on the right axis.
	prevRow, prevIn, prevPos := doc.layoutRow, doc.inLayout, doc.inPositioner
	doc.layoutRow, doc.inLayout = row, isLayoutType(parent.typeName)
	doc.inPositioner = true
	defer func() {
		doc.layoutRow, doc.inLayout, doc.inPositioner = prevRow, prevIn, prevPos
	}()
	// A Layout or a positioner (Row/Column/Grid/Flow) gives its children
	// slots; a ScrollView's content column is one of these too, via its own
	// children. Anything else just stacks them.
	laysOutChildren := isLayoutType(parent.typeName) || isPositioner(parent.typeName)
	var out []ui.Element
	for _, c := range parent.children {
		if doc.state.movedAway(c, parent) {
			continue // a ParentChange has it under another item
		}
		switch {
		case isBareMouseArea(c) || isPointerHandler(c):
			// Hoisted onto the parent box by buildBox — not a visual child.
			continue
		case isNonVisual(c):
			doc.refreshNonVisual(c, env)
			continue
		case laysOutChildren && hiddenChild(c, env, doc):
			continue
		case c.typeName == "Repeater":
			els, err := buildRepeater(c, env, doc)
			if err != nil {
				if doc.lenient {
					if len(doc.capturing) == 0 {
						// Inside an effect capture the same markup is being built a
						// second time; anything wrong with it was already reported
						// where it really lives.
						logf("gml: %v (element skipped)", err)
					}
					continue
				}
				return nil, err
			}
			out = append(out, els...)
		default:
			el, err := doc.slot(env, func() (ui.Element, error) { return build(c, env, doc) })
			if err != nil {
				if doc.lenient {
					if len(doc.capturing) == 0 {
						// Inside an effect capture the same markup is being built a
						// second time; anything wrong with it was already reported
						// where it really lives.
						logf("gml: %v (element skipped)", err)
					}
					continue
				}
				return nil, err
			}
			out = append(out, el)
		}
	}
	return out, nil
}

// buildText renders a Text/Label item, carrying its painting transforms
// (`transform:`, `rotation`, `scale`) on a wrapper around the finished
// text. A transform on a Text used to be rejected outright, which SKIPS the
// element: a rotated caption simply vanished.
func buildText(n *node, env *Env, doc *docState) (ui.Element, error) {
	el, err := buildTextItem(n, env, doc)
	if err != nil || el == nil {
		return el, err
	}
	return withTextTransforms(n, el, env, doc), nil
}

// withTextTransforms wraps el in a box carrying the item's transforms. The
// wrapper takes the text's own size, so layout is untouched — QML transforms
// affect painting only.
func withTextTransforms(n *node, el ui.Element, env *Env, doc *docState) ui.Element {
	var xforms []ui.Transform
	var rotateDeg, scaleBy float32
	var originName string
	for _, a := range n.attrs {
		switch a.name {
		case "transform":
			xforms = append(xforms, readTransformList(a, env, doc)...)
		case "rotation":
			if f, err := attrFloat(a, env); err == nil {
				rotateDeg = f
			}
		case "scale":
			if f, err := attrFloat(a, env); err == nil && f != 1 {
				scaleBy = f
			}
		case "transformOrigin":
			if a.expr != nil {
				if v, err := a.expr.eval(env); err == nil {
					originName = str(v)
				}
			}
		}
	}
	if rotateDeg != 0 || scaleBy != 0 {
		ox, oy := transformOriginFrac(originName)
		rel := ox == 0.5 && oy == 0.5
		if rotateDeg != 0 {
			xforms = append(xforms, ui.Transform{Kind: ui.XformRotate, Angle: rotateDeg,
				Rel: rel, OriginFracX: ox, OriginFracY: oy})
		}
		if scaleBy != 0 && scaleBy != 1 {
			xforms = append(xforms, ui.Transform{Kind: ui.XformScale, SX: scaleBy, SY: scaleBy,
				Rel: rel, OriginFracX: ox, OriginFracY: oy})
		}
	}
	if len(xforms) == 0 {
		return el
	}
	d := ui.Div()
	for _, t := range xforms {
		d.Transform(t)
	}
	return d.Children(el)
}

func buildTextItem(n *node, env *Env, doc *docState) (ui.Element, error) {
	var s string
	color := ui.RGB(0xffffff)
	var sizePx float64
	var w, h float32
	var bold, underline, strike, truncate, wrap, italic bool
	var tracking float32
	opacity := float32(1)
	glow := ui.RGBA{}
	var maxLines int
	var family, textFormat string
	var onLink []stmt // onLinkActivated, run with `link` bound
	// renderType: 0 the UI default, +1 QtRendering, -1 NativeRendering.
	var renderType int8
	halign, valign := ui.AlignStart, ui.AlignStart
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "text":
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			s = str(v)
		case "color":
			c, err := attrColor(a, env)
			if err != nil {
				return nil, err
			}
			color = c
		case "font.pixelSize", "font.pointSize":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			sizePx = float64(f)
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			h = f
		case "horizontalAlignment":
			al, err := textAlign(a, env, true)
			if err != nil {
				return nil, err
			}
			halign = al
		case "verticalAlignment":
			al, err := textAlign(a, env, false)
			if err != nil {
				return nil, err
			}
			valign = al
		case "font.bold":
			b, err := attrBool(a, env)
			if err != nil {
				return nil, err
			}
			bold = b
		case "font.underline":
			b, err := attrBool(a, env)
			if err != nil {
				return nil, err
			}
			underline = b
		case "font.strikeout":
			b, err := attrBool(a, env)
			if err != nil {
				return nil, err
			}
			strike = b
		case "renderType":
			// Qt's Text.renderType, and it means here what it means
			// there: QtRendering is a distance field (crisp rotated,
			// at a size that slides, or with a synthetic weight),
			// NativeRendering the hinted coverage atlas. The default
			// is whatever the UI is set to.
			if v, err := a.expr.eval(env); err == nil {
				if f, ferr := toFloat64(v); ferr == nil {
					if f == qtRenderingText {
						renderType = 1
					} else {
						renderType = -1
					}
				}
			}
		case "elide":
			truncate = true
		case "maximumLineCount":
			if f, err := attrFloat(a, env); err == nil {
				maxLines = int(f)
				wrap = true
			}
		case "font.weight":
			// Numeric QML weight (Normal=400, DemiBold=600, Bold=700…) or the
			// enum value; >= 600 renders bold (synthetic, single-weight face).
			if v, err := a.expr.eval(env); err == nil {
				if f, ferr := toFloat64(v); ferr == nil && f >= 600 {
					bold = true
				}
			}
		case "wrapMode":
			// Any wrapping mode (WordWrap/Wrap/WrapAnywhere) makes a sized Text
			// wrap to its width; NoWrap / 0 keeps it single-line.
			if v, err := a.expr.eval(env); err == nil {
				if f, ferr := toFloat64(v); ferr == nil {
					wrap = f != 0
				} else if s, ok := v.(string); ok {
					wrap = s != "" && s != "NoWrap"
				} else {
					wrap = true
				}
			}
		case "font.family":
			if v, err := a.expr.eval(env); err == nil {
				family = str(v)
			}
		case "font.italic":
			if b, err := attrBool(a, env); err == nil {
				italic = b
			}
		case "font":
			// A whole-font assignment (`font: root.font`): take the parts
			// we model from the object.
			if v, err := a.expr.eval(env); err == nil {
				if m, ok := v.(map[string]any); ok {
					if f, err := toFloat64(m["pixelSize"]); err == nil && f > 0 {
						sizePx = f
					} else if f, err := toFloat64(m["pointSize"]); err == nil && f > 0 {
						sizePx = f
					}
					if b, err := toBool(m["bold"]); err == nil && b {
						bold = true
					}
					if b, err := toBool(m["italic"]); err == nil && b {
						italic = true
					}
				}
			}
		case "font.letterSpacing":
			if f, err := attrFloat(a, env); err == nil {
				tracking = f
			}
		case "glow": // extension: soft text-shadow halo color
			if c, err := attrColor(a, env); err == nil {
				glow = c
			}
		case "textFormat":
			if v, err := a.expr.eval(env); err == nil {
				textFormat = str(v)
			}
		case "onLinkActivated":
			onLink = a.stmt
		case "opacity":
			if f, err := attrFloat(a, env); err == nil {
				opacity = f
			}
		case "transform", "rotation", "scale", "transformOrigin":
			// Painting transforms, applied to the finished text by
			// buildText (they leave layout alone, as QML's do).
		case "lineHeight", "lineHeightMode", "style", "styleColor",
			"font.wordSpacing",
			"font.capitalization", "font.styleName", "minimumPixelSize",
			"minimumPointSize", "fontSizeMode", "clip", "padding", "leftPadding",
			"rightPadding", "topPadding", "bottomPadding", "antialiasing", "smooth",
			"textInteractionFlags", "renderTypeQuality", "linkColor",
			"baseUrl", "font.preferShaping", "font.hintingPreference":
			// accepted, not modeled (plain-text rendering)
		default:
			return nil, fmt.Errorf("gml:%d: Text has no attribute %q", a.line, a.name)
		}
	}
	// QML opacity on a Text item scales what it paints: with plain-text
	// rendering that is exactly the glyph alpha (and the glow halo's).
	if opacity < 1 {
		if opacity < 0 {
			opacity = 0
		}
		color.A *= opacity
		glow.A *= opacity
	}
	// QML fonts INHERIT: a control's font.pixelSize reaches the label inside
	// its contentItem, which is why a `font.pixelSize: 11` Button has 11px
	// text even though the style's Label sets its own default size. The
	// composition binds the control's font group as a bare `font`, so an
	// unsized text takes its size from there.
	if sizePx == 0 {
		if v, ok := env.lookup("font"); ok {
			if fm, ok := v.(map[string]any); ok {
				if f, err := toFloat64(fm["pixelSize"]); err == nil && f > 0 {
					sizePx = f
				}
			}
		}
	}
	// RichText renders as styled spans. AutoText — which is QML's DEFAULT,
	// so an unset textFormat means it — sniffs the string for markup the
	// way Qt::mightBeRichText does, rather than treating every "<" as a tag.
	// The enum arrives as a name or as its number (Text.RichText evaluates
	// to 1), so classify rather than string-match: anything but PlainText
	// renders markup when the string carries any.
	plain := strings.Contains(textFormat, "Plain") || textFormat == "0"
	// MarkdownText (3) is Markdown, laid out by the md module: headings,
	// lists, tables, code — not the HTML subset RichText reads.
	if strings.Contains(textFormat, "Markdown") || textFormat == "3" {
		return markdownWithLinks(buildMarkdown(s, color, sizePx, w, doc.markdownImages()), onLink, env, doc), nil
	}
	rich := strings.Contains(textFormat, "Rich") || strings.Contains(textFormat, "Styled") ||
		textFormat == "1" || textFormat == "4"
	auto := !plain && !rich
	if (rich || (auto && looksRich(s))) && !plain {
		return buildRichText(s, color, sizePx)
	}
	// Hard line breaks: a text carrying "\n" is several lines, whether or
	// not it wraps (QML's Text honours the escape).
	if strings.Contains(s, "\n") && !(wrap && w > 0) {
		lines := strings.Split(s, "\n")
		col := ui.Div().Col()
		if w > 0 {
			col.W(w)
		}
		for _, ln := range lines {
			t := ui.Text(ln, color)
			if sizePx > 0 {
				t.SizePx(sizePx)
			}
			if bold {
				t.Bold()
			}
			if italic {
				t.Italic()
			}
			if underline {
				t.Underline()
			}
			if strike {
				t.StrikeThrough()
			}
			if family != "" {
				t.FontFamily(family)
			}
			if glow.A > 0 {
				t.Glow(glow)
			}
			t.HAlign(halign)
			if w > 0 {
				t.W(w)
			}
			col.Children(t)
		}
		switch halign {
		case ui.AlignCenter:
			col.ItemsCenter()
		case ui.AlignEnd:
			col.Items(ui.AlignEnd)
		}
		return col, nil
	}
	// QML breaks lines only once a width is IMPOSED on the text. A
	// `wrapMode` with nothing imposing a width keeps its text on one line
	// and lets it overflow — and its own width is that line, which is what
	// anything sized off the label reads. Ours wrapped to whatever box its
	// container happened to give it, so a plain child of an item broke
	// lines Qt does not break (and, before the container width reached it,
	// measured 0).
	if wrap && w == 0 && !textWidthImposed(n) {
		wrap = false
	}
	// A wrapping Text becomes a multi-line Paragraph. Without an explicit
	// width it wraps to the width its container gives it (QML's
	// height-for-width: `Label { wrapMode: Text.WordWrap; Layout.fillWidth:
	// true }` is how every description line in a card is written).
	if wrap {
		p := ui.Wrapped(s, color, w)
		if sizePx > 0 {
			p.SizePx(sizePx)
		}
		// The family and weight decide where the lines BREAK, so a
		// wrapping label that lost them wrapped on the wrong advances —
		// the corpus' card captions came out one line where Qt takes two.
		if family != "" {
			p.FontFamily(family)
		}
		if bold {
			p.Bold()
		}
		if italic {
			p.Italic()
		}
		if halign == ui.AlignCenter {
			p.Center()
		}
		if maxLines > 0 {
			p.Lines(maxLines)
		}
		return p, nil
	}
	t := ui.Text(s, color)
	if sizePx > 0 {
		t.SizePx(sizePx)
	}
	if w > 0 {
		t.W(w)
	}
	if h > 0 {
		t.H(h)
	}
	t.HAlign(halign).VAlign(valign)
	if bold {
		t.Bold()
	}
	if underline {
		t.Underline()
	}
	if strike {
		t.StrikeThrough()
	}
	if truncate {
		t.Truncate()
	}
	if italic {
		t.Italic()
	}
	if family != "" {
		t.FontFamily(family)
	}
	if tracking != 0 {
		t.Tracking(tracking)
	}
	if glow.A > 0 {
		t.Glow(glow)
	}
	if renderType != 0 {
		t.DistanceField(renderType > 0)
	}
	return t, nil
}

// looksRich reports whether a string carries HTML the way Qt's
// mightBeRichText does: an opening tag it would style, not any stray "<".
func looksRich(s string) bool {
	i := strings.IndexByte(s, '<')
	if i < 0 {
		return false
	}
	for _, tag := range []string{
		"<b>", "<i>", "<u>", "<s>", "<br", "<p>", "<p ", "<font", "<span",
		"<a ", "<h1", "<h2", "<h3", "<h4", "<ul", "<ol", "<li", "<pre",
		"<code", "<em>", "<strong", "<div", "<img", "<table", "<center",
	} {
		if strings.Contains(strings.ToLower(s), tag) {
			return true
		}
	}
	return false
}

// textAlign maps a QML alignment flag (number from the enum namespace, or a
// string) to a ui.Align. horizontal picks Left/HCenter/Right; vertical picks
// Top/VCenter/Bottom.
func textAlign(a attr, env *Env, horizontal bool) (ui.Align, error) {
	v, err := a.expr.eval(env)
	if err != nil {
		return ui.AlignStart, err
	}
	// Qt::AlignmentFlag bits: HCenter=4, Right=2, VCenter=128, Bottom=64.
	if f, err := toFloat64(v); err == nil {
		bit := int(f)
		if horizontal {
			switch {
			case bit&4 != 0:
				return ui.AlignCenter, nil
			case bit&2 != 0:
				return ui.AlignEnd, nil
			}
			return ui.AlignStart, nil
		}
		switch {
		case bit&128 != 0:
			return ui.AlignCenter, nil
		case bit&64 != 0:
			return ui.AlignEnd, nil
		}
		return ui.AlignStart, nil
	}
	switch str(v) {
	case "center", "AlignHCenter", "AlignVCenter":
		return ui.AlignCenter, nil
	case "right", "AlignRight", "bottom", "AlignBottom":
		return ui.AlignEnd, nil
	}
	return ui.AlignStart, nil
}

// buildButton is the Qt-Quick-Controls-flavored button: text + onClicked,
// styled with the framework's defaults unless overridden.
func buildButton(n *node, env *Env, doc *docState) (ui.Element, error) {
	var delayMs float32 // a DelayButton's hold time
	var onActivated []stmt
	var o ui.ButtonOpts
	var label string
	var onClicked []stmt
	var checkedRef Ref
	iconSrc := ""
	var iconColor ui.RGBA
	hasIconColor := false
	iconW, iconH := float32(18), float32(18)
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "text":
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			label = str(v)
		case "icon.source":
			if v, err := a.expr.eval(env); err == nil {
				iconSrc = str(v)
			}
		case "icon.name":
			// Freedesktop-style name: resolve through the builtin icon
			// registry (works headless and on js).
			if v, err := a.expr.eval(env); err == nil && iconSrc == "" {
				iconSrc = "icon:" + str(v)
			}
		case "icon.color":
			if c, err := attrColor(a, env); err == nil {
				iconColor, hasIconColor = c, true
			}
		case "icon.width":
			// Style helpers scale by window geometry, which is 0 before the
			// first layout — keep the default until a real size arrives.
			if f, err := attrFloat(a, env); err == nil && f > 0 {
				iconW = f
			}
		case "icon.height":
			if f, err := attrFloat(a, env); err == nil && f > 0 {
				iconH = f
			}
		case "textColor", "palette.buttonText": // textColor is an extension
			c, err := attrColor(a, env)
			if err != nil {
				return nil, err
			}
			o.TextColor = c
		case "width", "height", "radius":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			switch a.name {
			case "width":
				o.Width = f
			case "height":
				o.Height = f
			case "radius":
				o.Radius = f
			}
		case "color", "palette.button":
			c, err := attrColor(a, env)
			if err != nil {
				return nil, err
			}
			o.Color = c
		case "delay":
			if v, err := attrFloat(a, env); err == nil {
				delayMs = v
			}
		case "onActivated":
			onActivated = a.stmt
		case "onClicked":
			onClicked = a.stmt
		case "flat", "highlighted", "enabled":
			if b, err := attrBool(a, env); err == nil {
				switch a.name {
				case "flat":
					o.Flat = b
				case "highlighted":
					o.Highlighted = b
				case "enabled":
					o.Disabled = !b
				}
			}
		case "checked":
			checkedRef, _ = boolBinding(doc, n, a, env)
		case "checkable", "down", "autoRepeat",
			"display", "focusPolicy", "hoverEnabled", "spacing",
			"padding", "leftPadding", "rightPadding", "topPadding", "bottomPadding",
			"font.pixelSize", "font.pointSize", "font.bold", "font.family",
			"palette.highlight",
			"implicitWidth", "implicitHeight", "action":
			// accepted, not modeled (default chrome)
		default:
			return nil, fmt.Errorf("gml:%d: Button has no attribute %q", a.line, a.name)
		}
	}
	if checkedRef != nil && attrIsTrue(n, "checkable", env) {
		if pr, _, ok := propOf[bool](checkedRef); ok {
			o.Checked = pr.p
		}
	}
	// Icon (icon.source / icon.name): SVGs render tinted vectors; raster
	// sources go through the image cell. Sits left of the label like Qt.
	if iconSrc != "" {
		lower := strings.ToLower(iconSrc)
		if strings.HasSuffix(lower, ".svg") || strings.HasPrefix(iconSrc, "icon:") {
			if svg := doc.svgFor(iconSrc); svg != nil {
				ic := ui.SVG(svg, iconW, iconH)
				if hasIconColor {
					ic.Tint(iconColor)
				}
				o.Icon = ic
			}
		} else if cell := doc.imageCell(iconSrc); cell != nil {
			o.Icon = ui.LazyImage(cell, iconW, iconH)
		}
	}
	// Own onClicked + any Connections targeting this button's id.
	var d *ui.DivEl
	connected := doc.connectedHandler(nodeID(n), "onClicked", env)
	if onClicked != nil || connected != nil {
		run := doc.handlerRunner(env)
		o.OnClick = func() {
			doc.noteTrigger(d)
			if onClicked != nil {
				run(onClicked, "onClicked")
			}
			if connected != nil {
				connected()
			}
		}
	}
	if n.typeName == "DelayButton" {
		d = doc.delayButton(n, env, label, o, delayMs, onActivated)
	} else {
		d = ui.Button(label, o)
	}
	out := d

	// A button's children: the Menu a ToolButton opens (`onClicked:
	// fileMenu.open()` needs the menu BUILT for its id to carry open()),
	// timers, connections. Popups float in their own overlay, so they hang
	// off a zero-size holder beside the button rather than inside its
	// centred row, where an empty placeholder would shift the label by a gap.
	var popups []ui.Element
	if len(n.children) > 0 {
		prevParent, hadParent := env.vars["parent"]
		env.vars["parent"] = parentObject(n, env, doc)
		for _, c := range n.children {
			switch {
			case isNonVisual(c):
				doc.refreshNonVisual(c, env)
			case isPopupKind(c.typeName):
				el, err := build(c, env, doc)
				if err != nil {
					logf("gml: %s child %s: %v (skipped)", n.typeName, c.typeName, err)
					continue
				}
				popups = append(popups, el)
			}
		}
		if hadParent {
			env.vars["parent"] = prevParent
		} else {
			delete(env.vars, "parent")
		}
	}
	if len(popups) == 0 {
		return out, nil
	}
	return ui.Div().Children(out, ui.Div().W(0).H(0).Children(popups...)), nil
}

func buildCheckbox(n *node, env *Env, doc *docState) (ui.Element, error) {
	var label string
	var ref, stateRef Ref
	var onToggled []stmt
	tristate := false
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "text":
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			label = str(v)
		case "checked":
			ref, _ = boolBinding(doc, n, a, env)
		case "checkState":
			stateRef, _ = intBinding(doc, n, a, env)
		case "tristate":
			if b, err := attrBool(a, env); err == nil {
				tristate = b
			}
		case "onToggled", "onClicked", "onCheckStateChanged":
			onToggled = append(onToggled, a.stmt...)
		case "scale", "opacity", "visible", "enabled", "hoverEnabled",
			"ButtonGroup.group", "autoExclusive", "font.pixelSize",
			"font.pointSize", "font.bold", "spacing", "leftPadding", "rightPadding",
			"topPadding", "bottomPadding", "indicator", "contentItem", "background",
			"checkable", "onCheckedChanged":
			// group membership is read by buttonGroupFor; the rest accepted
		default:
			if strings.HasPrefix(a.name, "palette.") {
				continue // theme roles: our controls carry their own palette
			}
			return nil, fmt.Errorf("gml:%d: CheckBox has no attribute %q", a.line, a.name)
		}
	}
	pr, broke, ok := propOf[bool](ref)
	if !ok && ref == nil {
		// No `checked` attribute: the control still owns checked state.
		own := doc.boolProp(doc.key(n)+".checked", false)
		doc.exposeIDProp(n, "checked", own)
		pr, broke, ok = propOf[bool](own)
	}
	if !ok {
		return nil, fmt.Errorf("gml:%d: CheckBox checked wants a bool property", n.line)
	}
	run := doc.handlerRunner(env)
	fire := func() {
		if broke != "" {
			doc.assigned[broke] = true
		}
		if onToggled != nil {
			run(onToggled, "onToggled")
		}
	}
	// A tristate box (Qt.Unchecked, PartiallyChecked, Checked): checkState
	// is its state, and checked follows it (true only when Checked).
	if tristate || stateRef != nil {
		if stateRef == nil {
			seed := ui.Unchecked
			if pr.p.Get() {
				seed = ui.Checked
			}
			stateRef = doc.intProp(doc.key(n)+".checkState", seed)
			doc.exposeIDProp(n, "checkState", stateRef)
		}
		sp, _, ok := propOf[int](stateRef)
		if !ok {
			return nil, fmt.Errorf("gml:%d: CheckBox checkState wants an int property", n.line)
		}
		if tristate {
			return ui.TriCheckbox(sp.p, label, func(v int) {
				pr.p.Set(v == ui.Checked)
				fire()
			}), nil
		}
		// checkState bound but not tristate: a two-state box over it.
		pr.p.Set(sp.p.Get() == ui.Checked)
		return ui.Checkbox(pr.p, label, func(on bool) {
			if on {
				sp.p.Set(ui.Checked)
			} else {
				sp.p.Set(ui.Unchecked)
			}
			fire()
		}), nil
	}
	// checkState reads off checked for a two-state box.
	p := pr.p
	doc.exposeIDProp(n, "checkState", computedRef{fn: func() any {
		if p.Get() {
			return float64(ui.Checked)
		}
		return float64(ui.Unchecked)
	}})
	bg := doc.buttonGroupFor(n, env, false)
	if bg != nil {
		doc.join(bg, n, p)
	}
	return ui.Checkbox(p, label, func(on bool) {
		if bg != nil && bg.g.Exclusive {
			if !on {
				p.Set(true) // the checked member of an exclusive group stays checked
				return
			}
			bg.g.Check(p)
		}
		fire()
		doc.clicked(bg, p)
	}), nil
}

func buildSlider(n *node, env *Env, doc *docState) (ui.Element, error) {
	w := float32(200)
	from, to := float32(0), float32(1)
	var ref Ref
	var onMoved, onValueChanged []stmt
	var step, h float32
	var snap, vertical bool
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "width", "implicitWidth":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "value":
			ref, _ = floatBinding(doc, n, a, env)
		case "onMoved":
			onMoved = a.stmt
		case "onValueChanged":
			onValueChanged = a.stmt
		case "from":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			from = f
		case "to":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			to = f
		case "stepSize":
			if f, err := attrFloat(a, env); err == nil {
				step = f
			}
		case "snapMode":
			// SnapAlways holds the value to a step while dragging;
			// SnapOnRelease and the default NoSnap let it move freely
			// (stepSize then governs only keys and the wheel).
			if v, err := a.expr.eval(env); err == nil {
				snap = str(v) == "SnapAlways"
			}
		case "orientation":
			if f, err := attrFloat(a, env); err == nil {
				vertical = int(f) == 2 // Qt.Vertical
			}
		case "height", "implicitHeight":
			// A vertical slider's LENGTH is its height.
			if f, err := attrFloat(a, env); err == nil {
				h = f
			}
		case "live", "enabled":
			// accepted, not modeled
		default:
			return nil, fmt.Errorf("gml:%d: Slider has no attribute %q", a.line, a.name)
		}
	}
	ref, pr, broke, writeBack, ok := realValueProp(doc, n, ref)
	if !ok {
		return nil, fmt.Errorf("gml:%d: Slider value wants a real property", n.line)
	}
	s := ui.Slider(pr.p, w).Range(from, to).Step(step, snap)
	if vertical {
		s.Vertical().H(h)
	}
	if onValueChanged != nil && doc.wireOnce(doc.key(n)+"|onValueChanged") {
		// Any value change (user or programmatic), QML's valueChanged; wired
		// once on the persistent ref so per-frame rebuilds don't stack.
		run := doc.handlerRunner(env)
		stmts := onValueChanged
		p := pr.p
		ref.onChange(func() {
			env.vars["value"] = float64(p.Get())
			run(stmts, "onValueChanged")
			delete(env.vars, "value")
		})
	}
	if onMoved != nil || broke != "" || writeBack != nil {
		run := doc.handlerRunner(env)
		// The handler reads `value` bare (QML signal scope): bind it live.
		p := pr.p
		s.OnMoved(func() {
			if writeBack != nil {
				writeBack(p.Get())
			}
			if broke != "" {
				// Dragging is an imperative write: it breaks the bound
				// property's own default binding (QML semantics).
				doc.assigned[broke] = true
			}
			if onMoved == nil {
				return
			}
			env.vars["value"] = float64(p.Get())
			run(onMoved, "onMoved")
			delete(env.vars, "value")
		})
	}
	return s, nil
}

// boolBinding resolves a control's bool attribute: a bound property (Ref, which
// can be toggled) or a plain literal (display-only, ref nil). QML allows both
// `checked: someProp` and `checked: true`.
// boolBinding resolves a control's bool attribute: an explicitly bound
// external property, or the control's OWN persistent state — a real QML
// control (CheckBox.checked, Switch.checked, ...) always has live
// Q_PROPERTY state of its own, whether or not the app binds it
// externally. The own-state case is created once (via doc.boolProp,
// seeded from the markup literal) and exposed as id.attr so other
// bindings can read it — matching intProp's existing TabBar.currentIndex
// pattern, just for bool.
func boolBinding(doc *docState, n *node, a attr, env *Env) (Ref, bool) {
	if r, err := resolveRef(a.expr, env); err == nil {
		// The control's state is that property; still publish it as
		// id.attr, since QML lets other bindings read control.checked
		// whether the app bound it externally or not.
		doc.exposeIDProp(n, a.name, r)
		b, _ := toBool(r.get())
		return r, b
	}
	def := false
	if a.expr != nil {
		if v, err := a.expr.eval(env); err == nil {
			def, _ = toBool(v)
		}
	}
	ref := doc.boolProp(doc.key(n)+"."+a.name, def)
	doc.exposeIDProp(n, a.name, ref)
	b, _ := toBool(ref.get())
	return ref, b
}

// floatBinding is boolBinding's real-number counterpart (Slider.value,
// SpinBox.value's fractional path).
func floatBinding(doc *docState, n *node, a attr, env *Env) (Ref, float32) {
	if r, err := resolveRef(a.expr, env); err == nil {
		doc.exposeIDProp(n, a.name, r) // readable as id.attr, as in boolBinding
		f, _ := toFloat32(r.get())
		return r, f
	}
	def := float32(0)
	if a.expr != nil {
		if v, err := a.expr.eval(env); err == nil {
			def, _ = toFloat32(v)
		}
	}
	ref := doc.floatProp(doc.key(n)+"."+a.name, def)
	doc.exposeIDProp(n, a.name, ref)
	f, _ := toFloat32(ref.get())
	return ref, f
}

// intBinding resolves a control's int attribute: a bound property (Ref) or
// its own persistent state seeded from a plain literal.
func intBinding(doc *docState, n *node, a attr, env *Env) (Ref, int) {
	if r, err := resolveRef(a.expr, env); err == nil {
		doc.exposeIDProp(n, a.name, r) // readable as id.attr, as in boolBinding
		if f, err := toFloat64(r.get()); err == nil {
			return r, int(f)
		}
		return r, 0
	}
	ref, f := floatBinding(doc, n, a, env)
	return ref, int(f)
}

// idPropRef returns the ref an element's id ALREADY publishes for name —
// the slot the declare pass (seedControlState) created before any binding
// could read it. A control has to take its own state from that ref rather
// than deriving a key again: the declare pass and the build pass do not
// always agree on an element's prefix (an instance of a file component gets
// one only while building), and two keys mean two forked properties — the
// control moves one while every binding reads the other, so a TabBar's
// highlight walks and the StackLayout it drives never changes page.
func (doc *docState) idPropRef(n *node, name string) Ref {
	id := nodeID(n)
	if id == "" {
		return nil
	}
	for _, key := range []keyID{doc.here(id), doc.intern(id)} {
		if obj := doc.elemObjs.get(key); obj != nil {
			if r, ok := obj.props[name].(Ref); ok {
				return r
			}
		}
	}
	return nil
}

// exposeIDProp makes a control's own state readable as id.attr from
// anywhere else in the document (elemObject props, like a declared
// property would be) — a no-op when the element has no id. Inside a
// delegate the id (and so the elemObject) is scoped to the current row,
// matching declareRowScope's per-row binding of the same key.
// seedNatural records an item's own natural size on its id object, for the
// reads that happen before the first layout: elemObject.member falls back
// to it for width/height and implicitWidth/implicitHeight (frame one has no
// element yet, and a handler that divides by a zero there poisons the
// property it writes).
func (doc *docState) seedNatural(n *node, w, h float32) {
	id := nodeID(n)
	if id == "" {
		return
	}
	obj := doc.elemObjs.get(doc.here(id))
	if obj == nil {
		obj = &elemObject{}
		doc.elemObjs.set(doc.here(id), obj)
	}
	obj.natW, obj.natH = w, h
}

func (doc *docState) exposeIDProp(n *node, attr string, v any) {
	id := nodeID(n)
	if id == "" {
		return
	}
	key := doc.keyID(n) // == rowPrefix+id for an id'd node, cached on it
	obj := doc.elemObjs.get(key)
	if obj == nil {
		obj = &elemObject{}
		doc.elemObjs.set(key, obj)
	}
	obj.setProp(attr, v)
	// An element inside a file component or a styled control has TWO
	// objects for its id: the one the outer declare pass made under the bare
	// name, and this one under the instance's prefix. A handler carries
	// whichever it captured, and one that resolves the bare object would
	// find no property there — so `colorGridView.currentIndex = index`
	// wrote an attribute override nothing reads, and the grid never moved
	// its selection. Instances are one per use site, so both objects mean
	// the same element; a delegate ROW is not (one id, many rows), so its
	// per-row state stays where it is.
	if id != "" && isInstancePrefix(doc.rowPrefix) {
		if base := doc.elemObjs.get(doc.intern(id)); base != nil && base != obj {
			base.setProp(attr, v)
		}
	}
}

// isInstancePrefix reports whether a state prefix names ONE element of a
// file-component instance — the case where the id has two objects for the
// same element (the outer declare pass's and the instance's) and both must
// answer alike.
//
// Not row prefixes (one id, many rows — see enterRow's "#<index>/"), and
// not a STYLE instance's: every style file calls its root `root`, as do
// many pages, so publishing a styled control's own state under that bare
// name would have unrelated elements answering for each other.
func isInstancePrefix(prefix string) bool {
	if prefix == "" || strings.Contains(prefix, "#") {
		return false
	}
	segs := strings.Split(strings.TrimSuffix(prefix, "/"), "/")
	return strings.HasPrefix(segs[len(segs)-1], "fc:")
}

// boolProp is intProp's bool counterpart: a persistent engine-owned
// property for implicit control state markup expresses as a literal.
func (doc *docState) boolProp(key string, def bool) Ref {
	if r, ok := doc.synthRefs[key]; ok {
		return r
	}
	r, _ := newDeclaredProp("bool", def)
	doc.synthRefs[key] = r
	return r
}

// strProp is intProp's string counterpart, for implicit item state markup
// expresses as a literal (an item's `state` name).
func (doc *docState) strProp(key string, def string) Ref {
	if r, ok := doc.synthRefs[key]; ok {
		return r
	}
	r, _ := newDeclaredProp("string", def)
	doc.synthRefs[key] = r
	return r
}

// varProp is intProp's `property var` counterpart: state that is an array
// or object (a file dialog's selection), written through the property
// system so a change invalidates the build cache.
func (doc *docState) varProp(key string, def any) Ref {
	if r, ok := doc.synthRefs[key]; ok {
		return r
	}
	r, _ := newDeclaredProp("var", def)
	doc.synthRefs[key] = r
	return r
}

// floatProp is intProp's real-number counterpart.
func (doc *docState) floatProp(key string, def float32) Ref {
	if r, ok := doc.synthRefs[key]; ok {
		return r
	}
	r, _ := newDeclaredProp("real", def)
	doc.synthRefs[key] = r
	return r
}

// withLabel wraps a control indicator with a trailing text label (QML
// CheckBox/Switch/RadioButton style); returns the indicator alone if empty.
func withLabel(indicator ui.Element, label string) ui.Element {
	if label == "" {
		return indicator
	}
	return ui.Div().Row().ItemsCenter().Gap(8).Children(indicator, ui.Text(label, ui.CurrentTheme().Text)) // dark on a light page, light on a dark one
}

// buildSwitch is a Qt Quick Controls Switch: a pill track with a knob bound to
// a bool property; clicking toggles it.
func buildSwitch(n *node, env *Env, doc *docState) (ui.Element, error) {
	var ref Ref
	var label string
	var onToggled []stmt
	on := false
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "text":
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			label = str(v)
		case "checked":
			ref, on = boolBinding(doc, n, a, env)
		case "onToggled", "onClicked":
			onToggled = a.stmt
		case "enabled", "display", "focusPolicy", "hoverEnabled", "scale", "opacity", "visible",
			"font.pixelSize", "font.pointSize", "font.bold", "font.family",
			"leftPadding", "rightPadding", "topPadding", "bottomPadding", "spacing",
			"ButtonGroup.group", "indicator", "contentItem":
		default:
			if strings.HasPrefix(a.name, "palette.") {
				continue
			}
			return nil, fmt.Errorf("gml:%d: Switch has no attribute %q", a.line, a.name)
		}
	}
	_ = on
	if ref == nil {
		// No `checked` attribute: the control owns its checked state.
		ref = doc.boolProp(doc.key(n)+".checked", false)
		doc.exposeIDProp(n, "checked", ref)
	}
	pr, broke, ok := propOf[bool](ref)
	if !ok {
		return nil, fmt.Errorf("gml:%d: Switch checked wants a bool property", n.line)
	}
	run := doc.handlerRunner(env)
	track := ui.SwitchOpts{Disabled: !enabledAttr(n, env)}.Build(pr.p, "", func(bool) {
		if broke != "" {
			doc.assigned[broke] = true
		}
		if onToggled != nil {
			run(onToggled, "onToggled")
		}
	})
	return withLabel(track, label), nil
}

// buildRadioButton is a Qt Quick Controls RadioButton: a ring with a center
// dot when checked, bound to a bool property.
func buildRadioButton(n *node, env *Env, doc *docState) (ui.Element, error) {
	var ref Ref
	var label string
	var onClicked []stmt
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "text":
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			label = str(v)
		case "checked":
			ref, _ = boolBinding(doc, n, a, env)
		case "onClicked", "onToggled":
			onClicked = append(onClicked, a.stmt...)
		case "enabled", "display", "focusPolicy", "autoExclusive", "hoverEnabled",
			"scale", "opacity", "visible", "ButtonGroup.group", "font.pixelSize",
			"font.pointSize", "font.bold", "spacing", "indicator", "contentItem",
			"checkable", "onCheckedChanged":
			// group membership is read by buttonGroupFor; the rest accepted
		default:
			if strings.HasPrefix(a.name, "palette.") {
				continue
			}
			return nil, fmt.Errorf("gml:%d: RadioButton has no attribute %q", a.line, a.name)
		}
	}
	if ref == nil {
		// No `checked` attribute: the control owns its checked state.
		ref = doc.boolProp(doc.key(n)+".checked", false)
		doc.exposeIDProp(n, "checked", ref)
	}
	pr, broke, ok := propOf[bool](ref)
	if !ok {
		return nil, fmt.Errorf("gml:%d: RadioButton checked wants a bool property", n.line)
	}
	enabled := true
	if e := attrExpr(n, "enabled"); e != nil {
		if v, err := e.eval(env); err == nil {
			enabled, _ = toBool(v)
		}
	}
	bg := doc.buttonGroupFor(n, env, true)
	var group *ui.ButtonGroup
	if bg != nil {
		doc.join(bg, n, pr.p)
		group = bg.g
	}
	if !enabled {
		ring := ui.RadioRing(pr.p, nil)
		ring.OnClick(nil)
		return withLabel(ring, label), nil
	}
	run := doc.handlerRunner(env)
	ring := ui.RadioRing(pr.p, group, func(bool) {
		if broke != "" {
			doc.assigned[broke] = true // a click breaks the bound default
		}
		if onClicked != nil {
			run(onClicked, "onClicked")
		}
		doc.clicked(bg, pr.p)
	})
	return withLabel(ring, label), nil
}

// buildProgressBar is a Qt Quick Controls ProgressBar: a track with a filled
// portion for value in [from,to] (default [0,1]); an indeterminate one sweeps
// a segment along the track (ui.ProgressBar).
func buildProgressBar(n *node, env *Env, doc *docState) (ui.Element, error) {
	var value, from, to float32 = 0, 0, 1
	w := float32(200)
	indeterminate := false
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "value":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			value = f
		case "from":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			from = f
		case "to":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			to = f
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "indeterminate":
			b, err := attrBool(a, env)
			if err != nil {
				return nil, err
			}
			indeterminate = b
		case "height", "enabled":
		default:
			return nil, fmt.Errorf("gml:%d: ProgressBar has no attribute %q", a.line, a.name)
		}
	}
	o := ui.ProgressOpts{From: from, To: to, Width: w}
	if to == from {
		o.From, o.To = 0, 1 // an empty range shows nothing done
		value = 0
	}
	act := doc.activity(n)
	act.Run(indeterminate)
	if indeterminate {
		o.Indeterminate = act
	}
	return ui.ProgressBar(value, o), nil
}

// buildBusyIndicator is a Qt Quick Controls BusyIndicator: a ring with a dot
// going round while `running` (ui.BusyIndicator).
func buildBusyIndicator(n *node, env *Env, doc *docState) (ui.Element, error) {
	size := float32(32)
	running := true
	for _, a := range n.attrs {
		switch a.name {
		case "running":
			if b, err := attrBool(a, env); err == nil {
				running = b
			}
		case "id", "enabled":
		case "width", "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			if f > 0 {
				size = f
			}
		case "opacity", "visible", "scale", "implicitWidth", "implicitHeight",
			"padding", "contentItem", "background", "z":
			// accepted, not modeled
		default:
			return nil, fmt.Errorf("gml:%d: BusyIndicator has no attribute %q", a.line, a.name)
		}
	}
	act := doc.activity(n)
	act.Run(running)
	return ui.BusyIndicator(act, size), nil
}

// buildSeparator is a thin divider line: ToolSeparator (vertical, for a
// toolbar row) or MenuSeparator (horizontal full-width).
func buildSeparator(n *node, env *Env) (ui.Element, error) {
	for _, a := range n.attrs {
		switch a.name {
		case "id", "orientation", "enabled", "padding", "spacing", "width", "height",
			"leftPadding", "rightPadding", "topPadding", "bottomPadding":
		default:
			return nil, fmt.Errorf("gml:%d: %s has no attribute %q", a.line, n.typeName, a.name)
		}
	}
	return ui.Separator(n.typeName != "MenuSeparator"), nil
}

// buildDial is a Qt Quick Controls Dial, rendered as a ring with a notch at the
// value angle (value in [from,to], default [0,1]).
func buildDial(n *node, env *Env, doc *docState) (ui.Element, error) {
	var value, from, to float32 = 0, 0, 1
	size := float32(64)
	var ref Ref
	var step float32
	var onMoved []stmt
	enabled := true
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "value":
			// Own live state, exposed as id.value like the other controls.
			ref, value = floatBinding(doc, n, a, env)
		case "stepSize":
			if f, err := attrFloat(a, env); err == nil {
				step = f
			}
		case "enabled":
			if b, err := attrBool(a, env); err == nil {
				enabled = b
			}
		case "onMoved":
			onMoved = a.stmt
		case "from":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			from = f
		case "to":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			to = f
		case "width", "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			if f > 0 {
				size = f
			}
		case "wrap", "snapMode", "live", "inputMode",
			"suffix", "prefix", "opacity", "scale", "visible", "hoverEnabled",
			"font.pixelSize", "font.pointSize", "background", "handle",
			// style extensions the corpus sets on its Dial:
			"progressColor", "trackColor", "handleColor", "startAngle",
			"endAngle", "onValueChanged", "z",
			"trackWidth", "showTicks", "tickCount", "valueDecimals",
			"valueSuffix", "label", "labelColor", "implicitWidth",
			"implicitHeight", "inputMode2":
			// accepted, not modeled
		default:
			return nil, fmt.Errorf("gml:%d: Dial has no attribute %q", a.line, a.name)
		}
	}
	// A Qt Dial is a rotary track with the value drawn as an arc plus a
	// handle: sweep -140°..140° from 12 o'clock, like Quick Controls.
	// Turning it is ui.Dial's, laid over the drawing (dialInteraction).
	ref, pr, broke, writeBack, ok := realValueProp(doc, n, ref)
	if ok && ref != nil {
		value = pr.p.Get()
	}
	if !ok || !enabled {
		pos := float32(0)
		if to != from {
			pos = (value - from) / (to - from)
		}
		return ui.DialFace(min(max(pos, 0), 1), size, ui.CurrentTheme()), nil
	}
	o := ui.DialOpts{Size: size, Step: step, Snap: snapAlways(n, env)}
	if onMoved != nil || broke != "" || writeBack != nil {
		run := doc.handlerRunner(env)
		p := pr.p
		o.OnMoved = func() {
			if writeBack != nil {
				writeBack(p.Get())
			}
			if broke != "" {
				doc.assigned[broke] = true // turning it breaks the bound default
			}
			if onMoved != nil {
				env.vars["value"] = float64(p.Get())
				run(onMoved, "onMoved")
				delete(env.vars, "value")
			}
		}
	}
	return ui.DialControl(pr.p, from, to, o), nil
}

// buildSpinBox is a Qt Quick Controls SpinBox: a value box flanked by - / +
// buttons bound to an int property in [from,to], stepping by stepSize.
func buildSpinBox(n *node, env *Env, doc *docState) (ui.Element, error) {
	var ref Ref
	o := ui.SpinOpts{From: 0, To: 99, Step: 1}
	var onModified []stmt
	var fromText, toText any
	editable, enabled := false, true
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "value":
			ref, _ = intBinding(doc, n, a, env)
		case "from", "to", "stepSize", "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			switch a.name {
			case "from":
				o.From = int(f)
			case "to":
				o.To = int(f)
			case "stepSize":
				o.Step = int(f)
			case "width":
				o.Width = max(f-72, 32) // the two 32 px buttons and their gaps take the rest
			}
		case "wrap", "editable", "enabled":
			b, err := attrBool(a, env)
			if err != nil {
				return nil, err
			}
			switch a.name {
			case "wrap":
				o.Wrap = b
			case "editable":
				editable = b
			case "enabled":
				enabled = b
			}
		case "textFromValue", "valueFromText":
			if a.expr != nil {
				if v, err := a.expr.eval(env); err == nil {
					if a.name == "textFromValue" {
						toText = v
					} else {
						fromText = v
					}
				}
			}
		case "onValueModified":
			onModified = a.stmt
		case "scale", "opacity", "visible", "validator", "inputMethodHints", "font.pixelSize",
			"font.pointSize", "up.indicator", "down.indicator", "contentItem", "background",
			"onValueChanged", "focusPolicy", "wheelEnabled", "live":
			// accepted, not modelled (a validator: valueFromText decides)
		default:
			return nil, fmt.Errorf("gml:%d: SpinBox has no attribute %q", a.line, a.name)
		}
	}
	if ref == nil {
		ref = doc.intProp(doc.key(n)+".value", o.From)
		doc.exposeIDProp(n, "value", ref)
	}
	pr, broke, ok := propOf[int](ref)
	var writeBack func(int)
	if !ok {
		// A value that is not an int property (a literal, a real): mirror it
		// through the box's own int state, re-seeded every build and
		// written back on a user change (QML converts silently).
		cur, _ := toInt(ref.get())
		mirror := doc.intProp(doc.key(n)+".value~int", cur)
		mp, _, mok := propOf[int](mirror)
		if !mok {
			return nil, fmt.Errorf("gml:%d: SpinBox value wants a number", n.line)
		}
		mp.p.Set(cur)
		pr = mp
		target := ref
		writeBack = func(v int) { _ = target.set(float64(v)) }
		if t, isTracked := ref.(trackedRef); isTracked {
			broke = t.name
		}
	}
	if toText != nil {
		fn := toText
		o.Format = func(v int) string {
			out, err := callAny(fn, []any{float64(v), nil})
			if err != nil {
				return fmt.Sprint(v)
			}
			return str(out)
		}
	}
	if fromText != nil {
		fn := fromText
		o.Parse = func(s string) (int, bool) {
			out, err := callAny(fn, []any{s, nil})
			if err != nil {
				return 0, false
			}
			v, err := toInt(out)
			return v, err == nil
		}
	}
	if editable {
		o.Input = doc.inputFor(doc.key(n)+".edit", env)
	}
	// displayText, as the box shows the value.
	format := o.Format
	p := pr.p
	doc.exposeIDProp(n, "displayText", computedRef{fn: func() any {
		if format != nil {
			return format(p.Get())
		}
		return fmt.Sprint(p.Get())
	}})
	o.Disabled = !enabled
	run := doc.handlerRunner(env)
	o.OnModified = func() {
		if writeBack != nil {
			writeBack(p.Get())
		}
		if broke != "" {
			doc.assigned[broke] = true
		}
		if onModified != nil {
			run(onModified, "onValueModified")
		}
	}
	return ui.SpinBox(p, o), nil
}

// buildComboBox is a Qt Quick Controls ComboBox: a box showing the current
// model entry; clicking cycles currentIndex (no drop-down popup yet). model is
// a JS array (or count); currentIndex may bind to an int property.
func buildComboBox(n *node, env *Env, doc *docState) (ui.Element, error) {
	var items []any
	var idxRef Ref
	var modelAttr *attr
	var onActivated []stmt
	var onAccepted []stmt
	editable, enabled := false, true
	curIdx := 0
	w := float32(140)
	textRole, valueRole := "", ""
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "model":
			modelAttr = &a
		case "currentIndex":
			idxRef, curIdx = intBinding(doc, n, a, env)
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "valueRole":
			if v, err := a.expr.eval(env); err == nil {
				valueRole = str(v)
			}
		case "textRole":
			if v, err := a.expr.eval(env); err == nil {
				textRole = str(v)
			}
		case "onActivated", "onCurrentIndexChanged", "onCurrentTextChanged":
			onActivated = append(onActivated, a.stmt...)
		case "onAccepted":
			onAccepted = a.stmt
		case "editable", "enabled":
			if b, err := attrBool(a, env); err == nil {
				if a.name == "editable" {
					editable = b
				} else {
					enabled = b
				}
			}
		case "onEditTextChanged", "displayText", "currentValue",
			"font.pixelSize", "font.pointSize", "implicitHeight", "implicitWidth",
			"validator", "editText", "inputMethodHints", "popup", "delegate",
			"indicator", "background", "contentItem", "opacity", "visible",
			"flat", "wheelEnabled", "selectTextByMouse":
		default:
			return nil, fmt.Errorf("gml:%d: ComboBox has no attribute %q", a.line, a.name)
		}
	}
	// The model resolves after the attribute loop so a later textRole
	// still applies to ListModel rows.
	var modelRows []map[string]any
	if modelAttr != nil {
		rows, count, err := resolveModel(*modelAttr, env)
		if err != nil {
			return nil, err
		}
		modelRows = rows
		if count > 10000 {
			count = 10000 // NaN/pathological counts must not hang the build
		}
		if rows != nil {
			for _, row := range rows {
				items = append(items, comboRowText(row, textRole))
			}
		} else if modelAttr.expr != nil {
			if v, verr := modelAttr.expr.eval(env); verr == nil {
				if arr, ok := v.([]any); ok {
					items = arr
				}
			}
		}
		if len(items) == 0 {
			for i := 0; i < count; i++ {
				items = append(items, fmt.Sprintf("%d", i))
			}
		}
	}
	// The control owns currentIndex when nothing binds it; read it before
	// the current text is worked out from it.
	if idxRef == nil {
		idxRef = doc.intProp(doc.key(n)+".currentIndex", curIdx)
		doc.exposeIDProp(n, "currentIndex", idxRef)
		curIdx, _ = toInt(idxRef.get())
	}
	text := ""
	if len(items) > 0 {
		if curIdx < 0 {
			curIdx = 0
		}
		if curIdx >= len(items) {
			curIdx = len(items) - 1
		}
		text = str(items[curIdx])
	}
	// QML ComboBox.currentText / count are read-only properties other
	// bindings use (`label.text: colorCombo.currentText`).
	doc.exposeIDProp(n, "currentText", text)
	doc.exposeIDProp(n, "displayText", text)
	// An editable ComboBox's field text. We do not edit it in place yet, so
	// it IS the current item — but a page that reports it (`"Current
	// value: " + combo.editText`) must still read something.
	doc.exposeIDProp(n, "editText", text)
	doc.exposeIDProp(n, "count", float64(len(items)))
	// currentValue: the valueRole field of the current row (QML's typed
	// counterpart to currentText), or the row itself without a role.
	if curIdx >= 0 && curIdx < len(items) {
		value := items[curIdx]
		// A ListModel's rows carry the roles; `items` holds only the display
		// strings, so read the value role from the row itself.
		if curIdx < len(modelRows) {
			if v, ok := modelRows[curIdx][valueRole]; ok && valueRole != "" {
				value = v
			}
		}
		doc.exposeIDProp(n, "currentValue", value)
	}
	ip, broke, ok := propOf[int](idxRef)
	var writeBack func(int)
	if !ok {
		cur, _ := toInt(idxRef.get())
		mirror := doc.intProp(doc.key(n)+".currentIndex~int", cur)
		mp, _, mok := propOf[int](mirror)
		if !mok {
			return nil, fmt.Errorf("gml:%d: ComboBox currentIndex wants a number", n.line)
		}
		mp.p.Set(cur)
		ip = mp
		target := idxRef
		writeBack = func(v int) { _ = target.set(float64(v)) }
		if t, isTracked := idxRef.(trackedRef); isTracked {
			broke = t.name
		}
	}
	labels := make([]string, len(items))
	for i, it := range items {
		labels[i] = str(it)
	}
	// find(text) and indexOfValue(value), QML's lookups.
	findFn := jsFunc(func(args []any) (any, error) {
		if len(args) > 0 {
			for i, l := range labels {
				if l == str(args[0]) {
					return float64(i), nil
				}
			}
		}
		return float64(-1), nil
	})
	doc.exposeIDProp(n, "find", findFn)
	rows := modelRows
	doc.exposeIDProp(n, "indexOfValue", jsFunc(func(args []any) (any, error) {
		if len(args) > 0 {
			for i := range labels {
				v := any(labels[i])
				if i < len(rows) && valueRole != "" {
					v = rows[i][valueRole]
				}
				if str(v) == str(args[0]) {
					return float64(i), nil
				}
			}
		}
		return float64(-1), nil
	}))
	o := ui.ComboOpts{Items: labels, Width: w, Disabled: !enabled}
	if editable {
		in := doc.inputFor(doc.key(n)+".edit", env)
		o.Input = in
		// editText is what is typed, which is the current item until then.
		doc.exposeIDProp(n, "editText", computedRef{fn: func() any { return in.Text.Get() }})
	}
	run := doc.handlerRunner(env)
	o.OnActivated = func(i int) {
		if writeBack != nil {
			writeBack(i)
		}
		if broke != "" {
			doc.assigned[broke] = true
		}
		if onActivated != nil {
			runEvent(run, env, onActivated, "onActivated", "index", float64(i))
		}
	}
	if onAccepted != nil {
		// The handler reads the box's own editText and find() by bare name.
		o.OnAccepted = func(text string) {
			withVar(env, "editText", text, func() {
				withVar(env, "find", findFn, func() { run(onAccepted, "onAccepted") })
			})
		}
	}
	open := doc.boolProp(doc.key(n)+".open", false)
	op, _, _ := propOf[bool](open)
	return ui.ComboBox(ip.p, op.p, o), nil
}

// comboRowText picks a ListModel row's display string: the ComboBox's
// textRole, the conventional "text" role, or the first value.
func comboRowText(row map[string]any, textRole string) string {
	if textRole != "" {
		if v, ok := row[textRole]; ok {
			return str(v)
		}
	}
	if v, ok := row["text"]; ok {
		return str(v)
	}
	for _, v := range row {
		return str(v)
	}
	return ""
}

// isTextInput reports the QML text-entry types, which own a live
// ui.InputState instead of a plain element object.
func isTextInput(typeName string) bool {
	switch typeName {
	case "TextField", "TextInput", "TextEdit", "TextArea":
		return true
	}
	return false
}

// wireScrollEdge runs a view's scroll-edge handlers when the edge flag
// actually flips. QML fires onAtYEndChanged as the view reaches the bottom,
// which is how these pages append the next batch of rows; the flag is
// derived from the scroll offset, so the flip is detected by remembering
// last frame's value.
func wireScrollEdge(n *node, env *Env, doc *docState, st *ui.ListState, handlers []attr) {
	if len(handlers) == 0 {
		return
	}
	core.MarkUntrackedRead() // scroll offsets live outside the property system
	atEnd := st.MaxY() > 0 && st.Offset() >= st.MaxY()-1
	atBeginning := st.Offset() <= 0
	run := doc.handlerRunner(env)
	for _, a := range handlers {
		now, name := atEnd, a.name
		if name == "onAtYBeginningChanged" {
			now = atBeginning
		}
		key := doc.key(n) + "|" + name
		was, seen := doc.scrollEdges[key]
		doc.scrollEdges[key] = now
		if seen && was != now {
			stmts := a.stmt
			run(stmts, name)
		}
	}
}

// delegateHeight reads a delegate's own height for a virtualized view: the
// explicit height, else the implicit one, evaluated with the first row bound
// so a binding on a model role resolves. Zero when it cannot be read (the
// caller keeps its default).
func delegateHeight(delegate *node, rows []map[string]any, env *Env, doc *docState, viewKey string) float32 {
	return delegateExtent(delegate, rows, env, doc, viewKey, "height", "implicitHeight")
}

// delegateExtent is a delegate's size as its first row states it: the
// first of names it gives a positive value for.
func delegateExtent(delegate *node, rows []map[string]any, env *Env, doc *docState, viewKey string, names ...string) float32 {
	var row map[string]any
	if len(rows) > 0 {
		row = rows[0]
	}
	restore := enterRow(viewKey, delegate, env, doc, row, 0)
	defer restore()
	restoreGeom := shadowSelfGeometry(delegate, env, doc)
	defer restoreGeom()
	for _, name := range names {
		for _, a := range delegate.attrs {
			if a.name != name {
				continue
			}
			if f, err := attrFloat(a, env); err == nil && f > 0 {
				return f
			}
		}
	}
	return 0
}

// exposeListMethods wires ListView's imperative view API onto its id — and
// bare into the item's own scope, where QML calls it from the view's
// handlers (`Component.onCompleted: positionViewAtEnd()`). Positioning is a
// scroll-offset write, since rows here are uniform height.
func exposeListMethods(n *node, env *Env, doc *docState, st *ui.ListState, count int, rowHeight float32) {
	at := func(i int) {
		core.MarkUntrackedRead()
		st.SetOffset(float32(i) * rowHeight)
		core.BumpRevision()
	}
	methods := map[string]any{
		"positionViewAtEnd":       jsFunc(func([]any) (any, error) { at(count); return nil, nil }),
		"positionViewAtBeginning": jsFunc(func([]any) (any, error) { at(0); return nil, nil }),
		"positionViewAtIndex": jsFunc(func(args []any) (any, error) {
			i := 0
			if len(args) > 0 {
				if f, err := toFloat64(args[0]); err == nil {
					i = int(f)
				}
			}
			at(i)
			return nil, nil
		}),
		// The list re-lays out every frame, so these have nothing to do.
		"forceLayout":      jsFunc(func([]any) (any, error) { return nil, nil }),
		"returnToBounds":   jsFunc(func([]any) (any, error) { return nil, nil }),
		"cancelFlick":      jsFunc(func([]any) (any, error) { return nil, nil }),
		"itemAtIndex":      jsFunc(func([]any) (any, error) { return nil, nil }),
		"indexAt":          jsFunc(func([]any) (any, error) { return float64(-1), nil }),
		"itemAt":           jsFunc(func([]any) (any, error) { return nil, nil }),
		"forceActiveFocus": jsFunc(func([]any) (any, error) { return nil, nil }),
	}
	if id := nodeID(n); id != "" {
		obj := doc.elemObjs.get(doc.here(id))
		if obj == nil {
			obj = &elemObject{props: map[string]any{}}
			doc.elemObjs.set(doc.here(id), obj)
		}
		if obj.methods == nil {
			obj.methods = map[string]any{}
		}
		for k, v := range methods {
			obj.methods[k] = v
		}
	}
	for k, v := range methods {
		if _, taken := env.vars[k]; !taken {
			env.vars[k] = v
		}
	}
}

// isScrollType reports the QML types backed by a ui.ScrollState.
func isScrollType(typeName string) bool {
	switch typeName {
	case "ScrollView", "Flickable":
		return true
	}
	return false
}

// scrollFor returns the persistent scroll state for an id, creating it on
// first use.
func (doc *docState) scrollFor(id string) *ui.ScrollState {
	if st := doc.scrolls[id]; st != nil {
		return st
	}
	st := &ui.ScrollState{}
	doc.scrolls[id] = st
	return st
}

// inputFor returns the persistent text state for an id, creating it on
// first use. A host-registered state (env.Input) under the same id wins, so
// Go code can drive the field's text.
func (doc *docState) inputFor(id string, env *Env) *ui.InputState {
	if st := doc.inputs[id]; st != nil {
		return st
	}
	st, _ := env.vars[id].(*ui.InputState)
	if st == nil {
		st = ui.NewInput("")
	}
	doc.inputs[id] = st
	return st
}

func buildTextField(n *node, env *Env, doc *docState) (ui.Element, error) {
	w := float32(200)
	var sizePx float64
	var state *ui.InputState
	var idName string
	var bg, border, textCol *ui.RGBA
	var radius float32 = -1
	// TextInput and TextEdit are QML's RAW text items: no frame, no fill.
	// (TextField and TextArea are Controls, and keep the field look.) A
	// style's contentItem is a TextInput for exactly this reason — its
	// background delegate draws the box.
	if n.typeName == "TextInput" || n.typeName == "TextEdit" {
		clear := ui.RGBA{}
		bg, radius = &clear, 0
	}
	var placeholder, initialText string
	var hasInitial, wantFocus bool
	var navs []keyNav // KeyNavigation.* (focus.go)
	var onAccepted []stmt
	var mask string // inputMask
	// What the field draws instead of its value (echoMode), how long it may
	// get, and what it will accept at all.
	var echo rune
	var hasEcho bool
	var maxLen int
	var accept func(string) bool
	// TextArea and TextEdit are multi-line (ui.TextArea): wrapped, Enter
	// for a new line. Without a height they show about four lines.
	area := n.typeName == "TextArea" || n.typeName == "TextEdit"
	areaH := float32(0)
	for _, a := range n.attrs {
		if doc.readKeyNav(a, env, &navs) {
			continue
		}
		if area && (a.name == "height" || a.name == "implicitHeight") {
			if f, err := attrFloat(a, env); err == nil && f > 0 {
				areaH = f
			}
			continue
		}
		switch a.name {
		case "onAccepted",
			// Enter on a field is its accept, however it is spelled.
			"Keys.onReturnPressed", "Keys.onEnterPressed":
			onAccepted = append(onAccepted, a.stmt...)
		case "visible":
			if b, err := attrBool(a, env); err == nil && !b {
				return ui.Div(), nil // hidden: no space, no hits
			}
		case "focus":
			if b, err := attrBool(a, env); err == nil && b {
				wantFocus = true
			}
		case "id":
			id, ok := a.expr.(*identExpr)
			if !ok {
				return nil, fmt.Errorf("gml:%d: id wants a bare name", a.line)
			}
			// A host-registered state (env.Input) under this id lets Go
			// code drive the field's text (e.g. prefilled inline editors).
			st := doc.inputFor(id.name, env)
			env.vars[id.name] = st
			state = st
			idName = id.name
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "background":
			// The field's background delegate. `background: null` is how a
			// QML field is made to sit flush inside its own container, so a
			// null one leaves no fill and no frame at all.
			if a.elem != nil {
				bg, border, radius = backgroundLook(a.elem, env)
			} else {
				clear := ui.RGBA{}
				bg, border = &clear, nil
				radius = 0
			}
		case "color":
			c, err := attrColor(a, env)
			if err != nil {
				return nil, err
			}
			if doc.lenient {
				// QML: `color` on a text item is the TEXT colour; the box
				// comes from a background delegate. (gml's own dialect
				// treats it as the field's skin, with textColor for text —
				// examples and the cad panels are written that way.)
				textCol = &c
			} else {
				bg = &c
			}
		case "borderColor", "border.color":
			c, err := attrColor(a, env)
			if err != nil {
				return nil, err
			}
			border = &c
		case "textColor", "color.text":
			c, err := attrColor(a, env)
			if err != nil {
				return nil, err
			}
			textCol = &c
		case "radius":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			radius = f
		case "placeholderText":
			if v, err := a.expr.eval(env); err == nil {
				placeholder = str(v)
			}
		case "text":
			if v, err := a.expr.eval(env); err == nil {
				initialText = str(v)
				hasInitial = true
			}
		case "font":
			// A whole-font assignment (`font: root.font`, how a style's
			// contentItem inherits the control's font): only the size
			// reaches the input element.
			if v, err := a.expr.eval(env); err == nil {
				if m, ok := v.(map[string]any); ok {
					if f, err := toFloat64(m["pixelSize"]); err == nil && f > 0 {
						sizePx = f
					}
				}
			}
		case "echoMode":
			// TextInput.Password / NoEcho / PasswordEchoOnEdit: what the
			// field DRAWS. Its value is untouched.
			if v, err := a.expr.eval(env); err == nil {
				switch str(v) {
				case "Password", "PasswordEchoOnEdit":
					echo, hasEcho = '\u2022', true
				case "NoEcho":
					echo, hasEcho = 0, true
				}
			}
		case "maximumLength":
			if f, err := attrFloat(a, env); err == nil {
				maxLen = int(f)
			}
		case "inputMask":
			if v, err := a.expr.eval(env); err == nil {
				mask = str(v)
			}
		case "validator":
			// `validator: IntValidator { bottom: 0; top: 999 }` — a real
			// QValidator refuses a keystroke that would make the field
			// invalid, so this becomes a predicate on the candidate value.
			accept = validatorFunc(a, env)
		case "readOnly", "enabled", "selectByMouse", "wrapMode",
			"inputMethodHints", "font.pixelSize",
			"font.pointSize", "font.family", "font.bold", "font.italic",
			"font.underline", "font.letterSpacing", "horizontalAlignment",
			"verticalAlignment", "padding", "leftPadding", "rightPadding",
			"topPadding", "bottomPadding", "activeFocusOnTab",
			"clip", "textFormat", "selectedTextColor", "selectionColor",
			"onTextEdited", "onEditingFinished", "onTextChanged",
			// sizing/appearance the input element owns itself:
			"height", "implicitHeight", "implicitWidth", "opacity",
			"z", "scale", "cursorVisible", "persistentSelection",
			"onActiveFocusChanged", "onFocusChanged":
			// accepted, not modeled
		default:
			return nil, fmt.Errorf("gml:%d: TextField has no attribute %q", a.line, a.name)
		}
	}
	// QML TextFields need no id; without one, key the retained input state by
	// the node's source position so its text survives per-frame rebuilds.
	// Prefixed: a style file's contentItem is one node shared by every
	// instantiation, so a bare line number would give five spin boxes
	// one text. An id-named field keys by its id instead.
	key := doc.rowPrefix + fmt.Sprintf("__input%d", n.line)
	if idName != "" {
		key = "__id:" + idName
	}
	if state == nil {
		state = doc.inputs[key]
	}
	if state == nil {
		state = ui.NewInput(initialText)
		doc.inputs[key] = state
	} else if hasInitial && doc.inputSeed[key] != initialText && !state.HasFocus() {
		// The bound text changed since the field was last seeded (or an
		// id-registered state meets its binding for the first time):
		// QML's text setter. A focused field keeps what is being typed.
		state.SetText(initialText)
	}
	if hasInitial {
		doc.inputSeed[key] = initialText
	}
	state.MaxLen(maxLen)
	state.Accept(accept)
	if mask != doc.inputMasks[state] { // Qt's input mask (ui.InputState.SetMask)
		state.SetMask(mask)
		if doc.inputMasks == nil {
			doc.inputMasks = map[*ui.InputState]string{}
		}
		doc.inputMasks[state] = mask
	}
	// onAccepted fires on Enter with `text` bound to the field's content
	// (QML exposes the field's text property; a plain scope binding keeps
	// delegate row scope working too via handlerRunner).
	if onAccepted != nil {
		run := doc.handlerRunner(env)
		state.OnSubmit(func(txt string) {
			prev, had := env.vars["text"]
			env.vars["text"] = txt
			run(onAccepted, "onAccepted")
			if had {
				env.vars["text"] = prev
			} else {
				delete(env.vars, "text")
			}
		})
	} else {
		state.OnSubmit(nil)
	}
	in := ui.Input(state, w)
	if doc.inputKeys == nil {
		doc.inputKeys = map[*ui.InputState]keyID{}
	}
	doc.inputKeys[state] = doc.keyID(n) // KeyNavigation to it (focus.go)
	doc.trackFocus(doc.keyID(n), state) // so activeFocusItem can name it
	if area {
		if areaH <= 0 {
			areaH = 4*20 + 12
		}
		in = ui.TextArea(state, w, areaH)
	}
	if hasEcho {
		in.Echo(echo)
	}
	if sizePx > 0 {
		in.SizePx(sizePx)
	}
	if bg != nil {
		in.Bg(*bg)
	}
	if border != nil {
		in.Border(1, *border)
	}
	if textCol != nil {
		in.TextColor(*textCol)
	}
	if radius >= 0 {
		in.Rounded(radius)
	}
	if placeholder != "" {
		in.Placeholder(placeholder, ui.RGB(0xb0b0b0))
	}
	if wantFocus {
		in.Focus()
	}
	for _, kn := range navs {
		in.KeyNav(kn.dir, kn.id)
	}
	return in, nil
}

func buildScroll(n *node, env *Env, doc *docState) (ui.Element, error) {
	var state *ui.ScrollState
	interactive := true // QML's default: a Flickable flicks
	contentSized := false
	var width, height, padding, spacing float32
	var contentW, contentH float32
	var barColor *ui.RGBA
	for _, a := range n.attrs {
		switch a.name {
		case "scrollbarColor":
			c, err := attrColor(a, env)
			if err != nil {
				return nil, err
			}
			barColor = &c
		case "id":
			id, ok := a.expr.(*identExpr)
			if !ok {
				return nil, fmt.Errorf("gml:%d: id wants a bare name", a.line)
			}
			state = doc.scrollFor(doc.idKey(id.name))
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			width = f
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			height = f
		case "padding":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			padding = f
		case "spacing":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			spacing = f
		case "interactive":
			// QML's switch for "does this pan by dragging at all".
			if b, err := attrBool(a, env); err == nil {
				interactive = b
			}
		case "contentWidth":
			// QML's -1 means "adapt to the content": the content keeps its
			// own width instead of being stretched to the viewport, which
			// a page whose content sizes ITSELF depends on.
			if f, err := attrFloat(a, env); err == nil {
				if f < 0 {
					contentSized = true
				} else {
					contentW = f
				}
			}
		case "contentHeight":
			if f, err := attrFloat(a, env); err == nil && f > 0 {
				contentH = f
			}
		case "clip", "boundsBehavior",
			"ScrollBar.vertical", "ScrollBar.horizontal",
			// flick physics and gesture limits: a flick decelerates at one
			// fixed rate here (Qt's default), so these tune nothing but
			// must not fail the element.
			"maximumFlickVelocity", "flickDeceleration", "flickableDirection",
			"pressDelay", "rebound", "synchronousDrag",
			"contentX", "contentY", "onContentXChanged", "onContentYChanged",
			"onMovementEnded", "onFlickEnded", "topMargin", "bottomMargin",
			"leftMargin", "rightMargin", "originX", "originY":
			// accepted, no-op
		default:
			return nil, fmt.Errorf("gml:%d: %s has no attribute %q", a.line, n.typeName, a.name)
		}
	}
	if state == nil {
		// Flickable / id-less ScrollView: retain by a stable position key so
		// the scroll offset survives per-frame rebuilds.
		state = doc.scrollFor(doc.rowPrefix + fmt.Sprintf("__scroll%d", n.line))
	}
	// A QML view has no scrollbar of its own: one is attached with
	// `ScrollBar.vertical`, and Qt's own is only visible while the view
	// moves. Ours showed a permanent line down the side of every page.
	s := ui.Scroll(state).AutoHideBar()
	// QML's Flickable (and the ScrollView built on one) pans by dragging
	// and carries the motion on when the pointer lets go, unless the
	// markup switches interaction off.
	if interactive {
		s.Flickable()
	}
	if contentSized {
		s.ContentSized()
	}
	if width > 0 {
		s.W(width)
	}
	if height > 0 {
		s.H(height)
	}
	if padding > 0 {
		s.P(padding)
	}
	if spacing > 0 {
		s.Gap(spacing)
	}
	if barColor != nil {
		s.BarColor(*barColor)
	}
	// What a child means by `parent` is the CONTENT ITEM, not the view:
	// it is as wide as contentWidth (a ScrollView's is availableWidth, the
	// viewport minus padding), and graphics3d's control panel sizes its
	// whole column off it (`ColumnLayout { width: parent.width }`). Left as
	// the enclosing box, every control in that panel came out as wide as
	// the PANEL — 320 where Qt gives 270 — so each one overflowed the clip
	// and the slider handles sat too far right.
	prevParent, hadParent := env.vars["parent"]
	pobj := parentObject(n, env, doc)
	if po, ok := prevParent.(*elemObject); ok && hadParent {
		pobj.parentObj = po
	}
	pobj.ovW, pobj.ovH = contentW, contentH
	if padding > 0 {
		if b := ui.Bounds(pobj.el); pobj.ovW == 0 && b.W > 0 {
			pobj.ovW = b.W - 2*padding
		}
		if b := ui.Bounds(pobj.el); pobj.ovH == 0 && b.H > 0 {
			pobj.ovH = b.H - 2*padding
		}
	}
	env.vars["parent"] = pobj
	els, err := buildChildren(n, env, doc, false)
	if hadParent {
		env.vars["parent"] = prevParent
	} else {
		delete(env.vars, "parent")
	}
	if err != nil {
		return nil, err
	}
	s.Children(els...)
	return s, nil
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// anchorSized reports whether a node's own anchors decide its size, so the
// element must not pin a fallback of its own (an anchored view fills what it
// is anchored to).
func anchorSized(n *node) bool {
	if hasAttr(n, "anchors.fill") || hasAttr(n, "anchors.centerIn") {
		return true
	}
	return (hasAttr(n, "anchors.left") && hasAttr(n, "anchors.right")) ||
		(hasAttr(n, "anchors.top") && hasAttr(n, "anchors.bottom"))
}

// buildGridView renders a QML-style GridView: a model/count + a delegate laid
// out in uniform cellWidth×cellHeight cells, wrapping into rows by the view
// width, inside a vertical scroll. Columns are derived from width/cellWidth
// (immediate-mode: width must be known via the width attribute or it defaults).
func buildGridView(n *node, env *Env, doc *docState) (ui.Element, error) {
	var width, height, cellW, cellH float32 = 300, 300, 100, 100
	var hasW, hasH bool
	count := 0
	var rows []map[string]any
	var gridHighlight *node
	gridInitialCur, gridHasCur := -1, false
	// `cellHeight: cellWidth` — a bare read of the view's OWN attribute
	// (QML item scope). Bind the ones the corpus actually cross-references
	// before evaluating the rest.
	defer shadowOwnAttrs(n, env, "cellWidth", "cellHeight")()
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			width, hasW = f, true
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			height, hasH = f, true
		case "cellWidth":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			cellW = f
		case "cellHeight":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			cellH = f
		case "count", "model":
			r, c, err := resolveModel(a, env)
			if err != nil {
				return nil, err
			}
			rows, count = r, c
		case "highlight":
			if a.elem != nil {
				gridHighlight = unwrapComponent(a.elem)
			}
		case "currentIndex":
			if a.expr != nil {
				if v, err := a.expr.eval(env); err == nil {
					if i, err := toInt(v); err == nil {
						gridInitialCur, gridHasCur = i, true
					}
				}
			}
		case "delegate":
			// consumed by delegateChild (element-valued)
		case "flow", "clip", "cacheBuffer", "snapMode", "boundsBehavior",
			"focus", "interactive", "keyNavigationEnabled",
			"highlightFollowsCurrentItem", "highlightMoveDuration",
			// view transitions (add/remove/move animations) aren't modeled:
			"add", "remove", "displaced", "populate", "addDisplaced",
			"removeDisplaced", "moveDisplaced", "verticalLayoutDirection":
			// accepted, not modeled
		default:
			return nil, fmt.Errorf("gml:%d: GridView has no attribute %q", a.line, a.name)
		}
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	// Delegates size themselves off the view (width: grid.cellWidth).
	doc.exposeIDProp(n, "cellWidth", float64(cellW))
	doc.exposeIDProp(n, "cellHeight", float64(cellH))
	doc.exposeIDProp(n, "count", float64(count))
	// -1 is "nothing current", as in a ListView (see buildList).
	gseed := 0
	if gridHasCur {
		gseed = gridInitialCur
	}
	gridCurRef := doc.intProp(doc.key(n)+"|currentIndex", gseed)
	doc.exposeIDProp(n, "currentIndex", gridCurRef)
	viewKey := doc.key(n)
	scrollKey := doc.rowPrefix + fmt.Sprintf("__grid%d", n.line)
	scrollState := doc.scrolls[scrollKey]
	if scrollState == nil {
		scrollState = &ui.ScrollState{}
		doc.scrolls[scrollKey] = scrollState
	}
	// The grid itself is ui.GridView's; each cell is the delegate in its
	// row's scope, and the current one has the highlight behind it.
	o := ui.GridViewOpts{Count: count, CellW: cellW, CellH: cellH, Current: -1}
	if ci, err := toInt(gridCurRef.get()); err == nil {
		o.Current = ci
	}
	if gridHighlight != nil {
		o.Highlight = func(int) ui.Element {
			hl, err := build(gridHighlight, env, doc)
			if err != nil {
				return ui.Div()
			}
			return hl
		}
	}
	s := ui.GridView(scrollState, o, func(i int) ui.Element {
		var row map[string]any
		if rows != nil {
			row = rows[i]
		}
		restore := enterRow(viewKey, delegate, env, doc, row, i)
		defer restore()
		el, err := build(delegate, env, doc)
		if err != nil {
			return ui.Text("gml: "+err.Error(), ui.RGB(0xffb4b4))
		}
		return el
	})
	// A view that states no size takes the one its anchors or its layout
	// cell give it; only an explicit width/height pins it.
	if hasH && height > 0 {
		s.H(height)
	} else if !anchorSized(n) {
		s.H(height)
	}
	if hasW && width > 0 {
		s.W(width)
	}
	return s, nil
}

// buildList renders a QML-style ListView: a count, a row height, and a
// single delegate child rebuilt per visible row with `index` available to
// its expressions (the QML delegate/index idiom). Virtualized via ui.List.
func buildList(n *node, env *Env, doc *docState) (ui.Element, error) {
	var height, rowHeight float32 = 200, 32
	var rowSpacing float32
	var bottomUp bool   // verticalLayoutDirection: ListView.BottomToTop
	var horizontal bool // orientation: ListView.Horizontal
	var headerNode, footerNode *node
	// Sections: a header row is inserted wherever the section role's value
	// changes (QML groups by CONSECUTIVE runs, not by a GROUP BY).
	var sectionRole string
	var sectionDelegate *node
	count := 0
	var rows []map[string]any
	var highlight *node
	initialCur, hasCur := -1, false
	var scrollEdge []attr // onAtYEndChanged / onAtYBeginningChanged
	for _, a := range n.attrs {
		switch a.name {
		case "id": // accepted; list scroll state is keyed below
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			height = f
		case "rowHeight":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			rowHeight = f
		case "count", "model":
			r, c, err := resolveModel(a, env)
			if err != nil {
				return nil, err
			}
			rows, count = r, c
		case "highlight":
			// The current item's backdrop component, layered behind it.
			if a.elem != nil {
				highlight = unwrapComponent(a.elem)
			}
		case "currentIndex":
			// The attr seeds the persistent ref once; afterwards clicks own it.
			if a.expr != nil {
				if v, err := a.expr.eval(env); err == nil {
					if i, err := toInt(v); err == nil {
						initialCur, hasCur = i, true
					}
				}
			}
		case "delegate":
			// consumed by delegateChild (element-valued)
		case "onAtYEndChanged", "onAtYBeginningChanged":
			// A scroll-edge handler: this is the infinite-scroll hook, so it
			// runs when the flag actually flips (see wireScrollEdge below).
			scrollEdge = append(scrollEdge, a)
		case "spacing":
			// QML's ListView.spacing is the gap BETWEEN rows: the pitch
			// grows by it while each delegate keeps its own height.
			if f, err := attrFloat(a, env); err == nil {
				rowSpacing = f
			}
		case "verticalLayoutDirection":
			// ListView.BottomToTop: index 0 at the bottom, later rows
			// stacking above it — how every chat log in the corpus is
			// written (the model appends and the newest message shows at
			// the bottom).
			if v, err := a.expr.eval(env); err == nil {
				if f, err := toFloat32(v); err == nil {
					bottomUp = f == 1
				}
			}
		case "header", "footer":
			// Built above / below the rows, inside the list's height
			// (listHeaderFooter).
			var comp *node
			if a.elem != nil {
				comp = unwrapComponent(a.elem)
			} else {
				comp = doc.componentValue(a.expr, env) // header: someComponentId
			}
			if a.name == "header" {
				headerNode = comp
			} else {
				footerNode = comp
			}
		case "orientation":
			// ListView.Horizontal (Qt.Horizontal, 1): a row that scrolls
			// sideways (buildHorizontalList).
			if v, err := a.expr.eval(env); err == nil {
				if f, err := toFloat32(v); err == nil {
					horizontal = f == 1
				}
			}
		case "width", "clip", "cacheBuffer", "boundsBehavior", "snapMode",
			"highlightRangeMode", "highlightMoveDuration",
			"highlightFollowsCurrentItem", "preferredHighlightBegin", "preferredHighlightEnd",
			"add", "remove", "move", "displaced", "populate",
			"focus", "interactive", "keyNavigationEnabled", "reuseItems",
			"section.criteria":
			// accepted, not modeled (FullString is the only criteria the
			// corpus uses, and it is what grouping by value already does)
		case "section.property":
			if v, err := a.expr.eval(env); err == nil {
				sectionRole = str(v)
			}
		case "section.delegate":
			if a.elem != nil {
				sectionDelegate = unwrapComponent(a.elem)
			}
		default:
			return nil, fmt.Errorf("gml:%d: ListView has no attribute %q", a.line, a.name)
		}
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	// QML has no rowHeight: the DELEGATE's height is the row height. Read it
	// from the delegate (in the first row's scope, so a binding on a role
	// resolves); `rowHeight:` stays as a gml extension that wins when given.
	if !hasAttr(n, "rowHeight") {
		if h := delegateHeight(delegate, rows, env, doc, doc.key(n)); h > 0 {
			rowHeight = h
		}
	}
	doc.exposeIDProp(n, "count", float64(count))
	// currentIndex: persistent, settable (delegates assign it in onClicked);
	// the delegate's ListView.isCurrentItem reads it live (rowscope.go) and
	// the highlight component below renders behind the current row.
	// `currentIndex: -1` means NOTHING is current: no row reads
	// ListView.isCurrentItem as true and the highlight component is not
	// drawn at all (the corpus' contact list opens with no selection).
	// Only an absent currentIndex falls back to the first row.
	seed := 0
	if hasCur {
		seed = initialCur
	}
	curRef := doc.intProp(doc.key(n)+"|currentIndex", seed)
	doc.exposeIDProp(n, "currentIndex", curRef)
	if horizontal {
		return buildHorizontalList(n, env, doc, delegate, rows, count, rowSpacing, height, curRef, highlight, headerNode, footerNode), nil
	}
	st := listStateFor(n, doc)
	exposeListMethods(n, env, doc, st, count, rowHeight)
	wireScrollEdge(n, env, doc, st, scrollEdge)
	// The row callback runs lazily (during layout, for visible rows only),
	// after this build call has returned and doc.rowPrefix has been
	// restored — capture the row-scoped view key now, and FLATTEN the
	// scope: a file component's per-instance ids and properties are
	// restored when its build returns, so the live chain would resolve the
	// document-global objects instead (see Env.snapshot).
	viewKey := doc.key(n)
	// Memoized per view, not copied afresh: a row's cache entry guards on
	// env IDENTITY, so a new snapshot every build of the list invalidated
	// every row in it — 91 rebuilt subtrees a frame on the chat page,
	// which is the whole of both its lists. What a scope binds is
	// persistent (see scopeSnapshot), so the same copy stays valid until
	// a declaration pass creates something new.
	rowEnv := doc.scopeSnapshot(env, "rows|"+viewKey)
	// A sectioned list has more rows than the model does: one header per
	// run of equal section values, each with its own height.
	plan := sectionPlan(rows, sectionRole, sectionDelegate != nil)
	if len(plan) > 0 {
		secH := rowHeight
		if h := delegateHeight(sectionDelegate, rows, env, doc, doc.key(n)+"|sect"); h > 0 {
			secH = h
		}
		heights := make([]float32, len(plan))
		for i, sl := range plan {
			if sl.header {
				heights[i] = secH
			} else {
				heights[i] = rowHeight
			}
		}
		return listHeaderFooter(doc, env, headerNode, footerNode, height, listDirection(ui.List(st, rowHeight, len(plan), func(i int) ui.Element {
			sl := plan[i]
			env := rowEnv
			if sl.header {
				restore := enterRow(viewKey+"|sect", sectionDelegate, env, doc,
					map[string]any{"section": sl.section}, i)
				defer restore()
				env.vars["section"] = sl.section
				el, err := build(sectionDelegate, env, doc)
				if err != nil {
					return ui.Text("gml: "+err.Error(), ui.RGB(0xffb4b4))
				}
				return el
			}
			var row map[string]any
			if rows != nil {
				row = rows[sl.index]
			}
			restore := enterRow(viewKey, delegate, env, doc, row, sl.index)
			defer restore()
			el, err := build(delegate, env, doc)
			if err != nil {
				return ui.Text("gml: "+err.Error(), ui.RGB(0xffb4b4))
			}
			return el
		}).Heights(heights).Spacing(rowSpacing).H(height).AutoHideBar(), bottomUp)), nil
	}
	return listHeaderFooter(doc, env, headerNode, footerNode, height, listDirection(ui.List(st, rowHeight, count, func(i int) ui.Element {
		var row map[string]any
		if rows != nil {
			row = rows[i]
		}
		env := rowEnv
		restore := enterRow(viewKey, delegate, env, doc, row, i)
		defer restore()
		el, err := build(delegate, env, doc)
		if err != nil {
			return ui.Text("gml: "+err.Error(), ui.RGB(0xffb4b4))
		}
		// Current row: layer the highlight component behind the delegate
		// (both fill the row box, painted in order).
		if highlight != nil {
			if ci, err := toInt(curRef.get()); err == nil && ci == i {
				if hl, err := build(highlight, env, doc); err == nil {
					return ui.Div().AnchorMode().Children(
						ui.Anchored(hl, "", ui.Anchors{Fill: "parent"}),
						ui.Anchored(el, "", ui.Anchors{Fill: "parent"}),
					)
				}
			}
		}
		return el
	}).Spacing(rowSpacing).H(height).AutoHideBar(), bottomUp)), nil
}

// listDirection applies a ListView's row policy to the built list: each row
// is as tall as its own delegate builds itself (QML has no uniform row
// height — a chat bubble is as tall as its message), and BottomToTop puts
// index 0 at the bottom (see ui.ListEl.BottomUp / Measured).
func listDirection(l *ui.ListEl, bottomUp bool) *ui.ListEl {
	if bottomUp {
		l.BottomUp()
	}
	return l.Measured()
}

// listSlot is one row of a sectioned list: a section header, or the model
// row at index.
type listSlot struct {
	header  bool
	section string
	index   int
}

// sectionPlan expands a model into header-and-item rows, one header per run
// of equal section values. Returns nil when the list has no sections, so
// the plain (uniform-height) path stays untouched.
func sectionPlan(rows []map[string]any, role string, hasDelegate bool) []listSlot {
	if role == "" || !hasDelegate || len(rows) == 0 {
		return nil
	}
	var plan []listSlot
	last := ""
	for i, row := range rows {
		sect := str(row[role])
		if i == 0 || sect != last {
			plan = append(plan, listSlot{header: true, section: sect})
			last = sect
		}
		plan = append(plan, listSlot{index: i})
	}
	return plan
}

// pendingProp is a property default awaiting re-evaluation (forward refs).
type pendingProp struct {
	name string
	def  expr
	env  *Env
	line int
	// ref is the property this default belongs to when it is NOT the
	// document-global slot: a delegate row's or a component instance's own
	// ref. Nil means look the name up in doc.props.
	ref Ref
}

// zeroFor returns a declared-property type's zero value.
func zeroFor(typ string) any {
	switch typ {
	case "int", "real", "double":
		return float64(0)
	case "bool":
		return false
	case "string", "color":
		return ""
	}
	return nil
}

// retryPendingProps re-evaluates queued property defaults until a fixpoint
// (forward references usually resolve in one extra pass), then gives up:
// unresolvable defaults keep their zero value, log once, and are dropped.
// Call this only once no further build can add resolving state (i.e. after
// View()'s first build) — a default referencing a control's own live state
// (msgIdSpin.value, ma.containsMouse) can't resolve until that control has
// actually been built at least once. Before that point, use
// retryPendingPropsSoft so unresolved entries stay queued.
func (doc *docState) retryPendingProps() {
	doc.retryPendingPropsSoft()
	for _, p := range doc.pendingProps {
		_, err := p.def.eval(p.env)
		if err == nil {
			continue
		}
		if p.ref == nil {
			logf("gml:%d: property %s: default unresolved: %v", p.line, p.name, err)
			continue
		}
		// Instance-scoped: re-queued (and re-evaluated) by its own scope
		// every frame, so give the forward reference a few frames before
		// calling it a mistake, and say so only once.
		if doc.propRetries == nil {
			doc.propRetries = map[string]int{}
		}
		key := fmt.Sprintf("%s:%d", p.name, p.line)
		doc.propRetries[key]++
		if doc.propRetries[key] == 3 {
			logf("gml:%d: property %s: default unresolved: %v", p.line, p.name, err)
		}
	}
	doc.pendingProps = nil
}

// retryPendingPropsSoft is retryPendingProps' non-final pass: it resolves
// what it can and leaves the rest queued (no logging, no discarding) for a
// later retryPendingProps/retryPendingPropsSoft call once more state exists.
func (doc *docState) retryPendingPropsSoft() {
	for pass := 0; pass < 4 && len(doc.pendingProps) > 0; pass++ {
		var still []pendingProp
		for _, p := range doc.pendingProps {
			v, err := p.def.eval(p.env)
			if err != nil {
				still = append(still, p)
				continue
			}
			// Write through the RAW ref: resolving a default is the binding
			// working, not an imperative assignment — going through the
			// tracked ref would mark the binding broken and freeze the
			// property at this first resolved value.
			ref, ok := p.ref, p.ref != nil
			if !ok {
				ref, ok = doc.propsRaw[p.name]
				if !ok {
					ref, ok = doc.props[p.name]
				}
			}
			if ok {
				if err := ref.set(v); err != nil {
					logf("gml:%d: property %s: %v", p.line, p.name, err)
				}
			}
		}
		if len(still) == len(doc.pendingProps) {
			doc.pendingProps = still
			break // no progress
		}
		doc.pendingProps = still
	}
}

// validatorFunc turns a QML `validator:` into the predicate an input state
// vets each candidate value with. QValidator has three verdicts and only
// Invalid is refused: a partially-typed number ("-", "1.") is Intermediate,
// which a field must accept or the user could never type the rest of it.
func validatorFunc(a attr, env *Env) func(string) bool {
	v := a.elem
	if v == nil {
		return nil
	}
	num := func(name string) (float64, bool) {
		for _, at := range v.attrs {
			if at.name != name || at.expr == nil {
				continue
			}
			if f, err := attrFloat(at, env); err == nil {
				return float64(f), true
			}
		}
		return 0, false
	}
	switch v.typeName {
	case "IntValidator", "DoubleValidator":
		decimals := v.typeName == "DoubleValidator"
		lo, hasLo := num("bottom")
		hi, hasHi := num("top")
		return func(text string) bool {
			if text == "" {
				return true
			}
			// The prefixes a number is typed through, in order.
			for i, r := range text {
				switch {
				case r >= '0' && r <= '9':
				case (r == '-' || r == '+') && i == 0:
				case r == '.' && decimals:
				default:
					return false
				}
			}
			f, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return true // still intermediate ("-", "1.")
			}
			if hasLo && f < lo {
				// A number still being typed can be under the floor
				// ("1" on the way to "12" with bottom 10), but a
				// NEGATIVE one under a non-negative floor never will be.
				return !(hasLo && lo >= 0 && f < 0)
			}
			return !hasHi || f <= hi
		}
	case "RegularExpressionValidator", "RegExpValidator":
		pattern := ""
		for _, at := range v.attrs {
			if at.name == "regularExpression" || at.name == "regExp" {
				if val, err := at.expr.eval(env); err == nil {
					pattern = str(val)
				}
			}
		}
		if pattern == "" {
			return nil
		}
		// QML anchors a validator's pattern at both ends, and the field
		// has to accept every PREFIX of a match as intermediate.
		re, err := regexp.Compile("^(?:" + pattern + ")$")
		if err != nil {
			return nil
		}
		return func(text string) bool {
			if text == "" {
				return true
			}
			if re.MatchString(text) {
				return true
			}
			// Intermediate: some longer string starting with this could
			// still match. Approximated by asking whether the pattern
			// matches the text with its own anchors relaxed at the end.
			if open, err := regexp.Compile("^(?:" + pattern + ")"); err == nil {
				if loc := open.FindStringIndex(text); loc != nil && loc[1] == len(text) {
					return true
				}
			}
			return false
		}
	}
	return nil
}

// buildDelegateRow renders an unstyled ItemDelegate / SwipeDelegate: a list
// ROW. It has no fill of its own (only a wash under the pointer), its label
// leads at Qt's 12px inset, and its height is the label's plus Qt's padding
// — measured from the corpus' own reference, where a menu of these renders
// as plain rows over the panel behind them.
//
// Not a Button: rendered as one, a drawer's menu came out as a stack of
// pills, and a list of them as a wall of colour.
func buildDelegateRow(n *node, env *Env, doc *docState) (ui.Element, error) {
	label := ""
	var onClicked []stmt
	var w, h, fontPx float32
	textCol := ui.RGB(0x111111)
	if doc.groundDark {
		textCol = ui.RGB(0xd8d8e0)
	}
	for _, a := range n.attrs {
		switch a.name {
		case "text":
			if v, err := a.expr.eval(env); err == nil {
				label = str(v)
			}
		case "onClicked", "onTriggered", "onPressed":
			onClicked = append(onClicked, a.stmt...)
		case "width", "implicitWidth":
			if f, err := attrFloat(a, env); err == nil {
				w = f
			}
		case "height", "implicitHeight":
			if f, err := attrFloat(a, env); err == nil {
				h = f
			}
		case "font.pixelSize", "font.pointSize":
			if f, err := attrFloat(a, env); err == nil {
				fontPx = f
			}
		case "color":
			if c, err := attrColor(a, env); err == nil {
				textCol = c
			}
		default:
			// The rest of the delegate contract (highlighted, checkable,
			// padding, anchors, Layout.*): accepted, and its own children
			// build below.
		}
	}
	if fontPx <= 0 {
		fontPx = 13
	}
	if h <= 0 {
		// Qt's ItemDelegate is its label plus its padding: 46 for a 16px
		// label in the corpus' own render.
		h = fontPx + 30
	}
	row := ui.Div().Row().ItemsCenter().H(h).Padding(0, 12, 0, 12).
		HoverBg(ui.RGBA{R: 1, G: 1, B: 1, A: 0.06})
	if w > 0 {
		row.W(w)
	} else {
		row.Grow().CrossFill()
	}
	if label != "" {
		t := ui.Text(label, textCol)
		t.SizePx(float64(fontPx))
		row.Children(t)
	}
	// A delegate's own children (an icon, a Row of labels) come after.
	for _, c := range n.children {
		if isNonVisual(c) {
			doc.refreshNonVisual(c, env)
			continue
		}
		el, err := build(c, env, doc)
		if err != nil {
			return nil, err
		}
		row.Children(el)
	}
	if len(onClicked) > 0 {
		run := doc.handlerRunner(env)
		stmts := onClicked
		row.OnClick(func() { run(stmts, "onClicked") })
	}
	return row, nil
}

// textWidthImposed reports whether something in the markup gives a Text a
// width to wrap at: an explicit one (handled by the caller), anchors that
// pin both sides, or a layout told to stretch it. QML's own rule — a
// wrapping Text with no imposed width does not wrap at all.
//
// `anchors.centerIn` is NOT one of them: it positions without sizing.
func textWidthImposed(n *node) bool {
	if hasAttr(n, "anchors.fill") ||
		(hasAttr(n, "anchors.left") && hasAttr(n, "anchors.right")) {
		return true
	}
	for _, a := range n.attrs {
		switch a.name {
		case "Layout.fillWidth", "Layout.preferredWidth", "Layout.maximumWidth":
			return true
		}
	}
	// A styled control is built from its STYLE's node, which carries none
	// of the use site's anchors or layout hints — the instantiation records
	// what the use site imposed (see node.imposedWidth).
	return n.imposedWidth
}

// signalMethod makes a declared signal callable through its element's id,
// `knob.moved(3)`, as well as by its bare name inside the element.
func (doc *docState) signalMethod(id, name string, fn any) {
	if id == "" {
		return
	}
	obj := doc.elemObjs.get(doc.intern(id))
	if obj == nil {
		return
	}
	if obj.methods == nil {
		obj.methods = map[string]any{}
	}
	obj.methods[name] = fn
}

// buildHorizontalList is a ListView with orientation: ListView.Horizontal:
// the delegates side by side, each at its own width, in a ui.List that
// scrolls sideways (Horizontal): only the delegates in view are built, the
// wheel scrolls it, and its header and footer scroll with the rows.
func buildHorizontalList(n *node, env *Env, doc *docState, delegate *node, rows []map[string]any,
	count int, spacing, height float32, curRef Ref, highlight, header, footer *node) ui.Element {
	viewKey := doc.key(n)
	rowEnv := doc.scopeSnapshot(env, "rows|"+viewKey)
	st := listStateFor(n, doc)
	// The width estimate for rows not built yet; each built row's own
	// width is learned (Measured), as a vertical list learns heights.
	est := delegateExtent(delegate, rows, env, doc, viewKey, "width", "implicitWidth")
	if est <= 0 {
		est = 80
	}
	l := ui.List(st, est, count, func(i int) ui.Element {
		var row map[string]any
		if rows != nil {
			row = rows[i]
		}
		restore := enterRow(viewKey, delegate, rowEnv, doc, row, i)
		defer restore()
		el, err := build(delegate, rowEnv, doc)
		if err != nil {
			return ui.Text("gml: "+err.Error(), ui.RGB(0xffb4b4))
		}
		if highlight != nil {
			if ci, err := toInt(curRef.get()); err == nil && ci == i {
				if hl, err := build(highlight, rowEnv, doc); err == nil {
					return ui.Div().AnchorMode().Children(
						ui.Anchored(hl, "", ui.Anchors{Fill: "parent"}),
						ui.Anchored(el, "", ui.Anchors{Fill: "parent"}),
					)
				}
			}
		}
		return el
	}).Horizontal().Measured().Spacing(spacing).H(height).AutoHideBar()
	for _, a := range n.attrs {
		if a.name == "width" {
			if w, err := attrFloat(a, env); err == nil && w > 0 {
				l.W(w)
			}
		}
	}
	return listHeaderFooter(doc, env, header, footer, height, l)
}

// listHeaderFooter builds a ListView's header and footer components into
// the list, before its first row and after its last (above and below a
// vertical list, left and right of a horizontal one), scrolling with them.
func listHeaderFooter(doc *docState, env *Env, header, footer *node, _ float32, l *ui.ListEl) ui.Element {
	// Part of the list's content (ui.ListEl.Header/Footer), so they scroll
	// away with the rows, as Qt's do.
	if header != nil {
		if el, err := build(header, env, doc); err == nil {
			l.Header(el)
		} else {
			logf("gml: ListView header: %v", err)
		}
	}
	if footer != nil {
		if el, err := build(footer, env, doc); err == nil {
			l.Footer(el)
		} else {
			logf("gml: ListView footer: %v", err)
		}
	}
	return l
}

// realValueProp resolves a value control's (Slider, Dial) value to the
// real property its ui widget drives. ref is the value attribute's
// binding, or nil when there is none: the control then owns value state,
// readable as id.value. A binding to a non-real property (`value:
// someIntProp`) is mirrored through the control's own real state —
// re-seeded from the property every build, written back by writeBack on
// a move (QML converts between value types silently). broke names the
// declared property a user move breaks the binding of ("" for none).
func realValueProp(doc *docState, n *node, ref Ref) (Ref, propRef[float32], string, func(float32), bool) {
	pr, broke, ok := propOf[float32](ref)
	var writeBack func(float32)
	if ok {
		return ref, pr, broke, nil, true
	}
	if ref == nil {
		ref = doc.floatProp(doc.key(n)+".value", 0)
		doc.exposeIDProp(n, "value", ref)
		pr, broke, ok = propOf[float32](ref)
		return ref, pr, broke, nil, ok
	}
	cur, _ := toFloat32(ref.get())
	mirror := doc.floatProp(doc.key(n)+".value~real", cur)
	if mp, _, mok := propOf[float32](mirror); mok {
		mp.p.Set(cur)
		pr, ok = mp, true
	}
	target := ref
	writeBack = func(v float32) { _ = target.set(v) }
	if t, isTracked := ref.(trackedRef); isTracked {
		broke = t.name
	}
	return ref, pr, broke, writeBack, ok
}

// geomKey names one geometry attribute of one element.
type geomKey struct {
	key  keyID
	name string
}

// pendingGeom is a geometry override written since the tree was last laid
// out: `item.x = 230` read back before the next frame has placed the item.
// It is pending through the frame after the write (that frame's build
// still reads the old layout); from then on the laid-out value is the
// truth, so anchors and layouts keep the last word.
func (doc *docState) pendingGeom(key keyID, name string) (float64, bool) {
	stamp, ok := doc.geomWritten[geomKey{key, name}]
	if !ok || doc.frameNo > stamp+1 {
		return 0, false
	}
	v, ok := doc.attrOverride(key, name)
	if !ok {
		return 0, false
	}
	f, err := toFloat64(v)
	return f, err == nil
}

// ownerKey is the element key of the item n was declared inside (0 when it
// has none): what n's x/y are relative to, for a parent object that is
// read outside the build that set its parentObj (a handler's target).
func ownerKey(n *node, doc *docState) keyID {
	if n.owner == nil {
		return 0
	}
	return doc.keyID(n.owner)
}

// enabledAttr is an element's `enabled` (true when absent or unreadable).
func enabledAttr(n *node, env *Env) bool {
	if e := attrExpr(n, "enabled"); e != nil {
		if v, err := e.eval(env); err == nil {
			b, _ := toBool(v)
			return b
		}
	}
	return true
}
