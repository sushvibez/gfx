package gml

import (
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// Per-instance delegate scope. QML instantiates a delegate component once per
// model row: ids declared inside it are visible only within that instance,
// and its `property` declarations hold per-row state (an accordion's
// `expanded`, a row's computed `filteredTopics`). This engine builds the
// delegate template per row instead, so instance identity must be synthesized:
// enterRow prefixes every state key with "<viewKey>#<row>/" and runs a
// per-row declaration pass over the delegate subtree, giving each row its own
// persistent elemObjects and property refs (they live in docState maps, so
// row state survives per-frame rebuilds and hot reloads, exactly like
// document-level declared properties).

// enterRow binds row i's scope for building `delegate` under the view keyed
// viewKey (doc.key of the ListView/Repeater/GridView/PathView — already
// row-scoped itself when views nest). It returns a restore func undoing the
// env bindings and the row prefix; docState-held row state persists.
func enterRow(viewKey string, delegate *node, env *Env, doc *docState, row map[string]any, i int) func() {
	// A trackable row binds its names through a token, so everything
	// built under it can be cached (rowtoken.go); one that cannot be
	// hashed falls back to the plain binding and stays uncacheable.
	vals, tracked := doc.rowToken(viewKey, row, i)
	restoreRow := bindRow(env, row, i, vals)
	prevPrefix := doc.rowPrefix
	doc.rowPrefix = viewKey + "#" + strconv.Itoa(i) + "/"
	doc.rowDepth++
	if !tracked {
		doc.rowUntracked++
	}
	seenMark := len(doc.rowSeen)
	// Push the row onto the handler-scope stack so handlers built inside the
	// delegate (onClicked reading modelData) re-bind this row when they fire,
	// possibly frames later (handlerRunner snapshots the stack).
	doc.rowScope = append(doc.rowScope, rowBind{row: row, idx: i})
	restoreScope := declareRowScope(delegate, env, doc, row, viewKey, i)
	return func() {
		restoreScope()
		doc.rowScope = doc.rowScope[:len(doc.rowScope)-1]
		doc.rowPrefix = prevPrefix
		// Put the repeat counts back, so the next build of this row
		// numbers its subtrees exactly as this one did and finds them.
		for i := len(doc.rowSeen) - 1; i >= seenMark; i-- {
			doc.keySeen.set(doc.rowSeen[i].key, doc.rowSeen[i].prev)
		}
		doc.rowSeen = doc.rowSeen[:seenMark]
		doc.rowDepth--
		if !tracked {
			doc.rowUntracked--
		}
		restoreRow()
	}
}

// declareRowScope walks the delegate subtree (not descending into nested
// views' delegates — those get their own row scopes) and binds this row's
// instance scope into env: per-row elemObjects for every id (phase 1, so a
// later property default or an EARLIER sibling's binding can reference any
// id in the instance), then per-row refs for every `property` declaration
// (phase 2, defaults evaluated once per row with the row's modelData/roles
// in scope). Returns a restore func undoing exactly the env bindings made.
func declareRowScope(delegate *node, env *Env, doc *docState, row map[string]any, viewKey string, rowIdx int) func() {
	type saved struct {
		name string
		v    any
		ok   bool
	}
	var undo []saved
	// on<Prop>Changed handlers, wired once the whole scope is bound.
	type changedWiring struct {
		raw  Ref
		body []stmt
		name string
	}
	var onChanged []changedWiring
	// Every binding this row makes is also recorded on the row-scope stack:
	// a handler inside a delegate runs LATER (on a click, frames after the
	// row was built and its bindings undone), and it has to see the row's
	// own ids and properties — `contactListView.currentIndex =
	// contactDelegate.index` reads the delegate's id from inside itself.
	var mine map[string]any
	if n := len(doc.rowScope); n > 0 {
		if doc.rowScope[n-1].ids == nil {
			doc.rowScope[n-1].ids = map[string]any{}
		}
		mine = doc.rowScope[n-1].ids
	}
	var bound map[string]any
	if !noPatch {
		bound = map[string]any{}
	}
	popBinds := doc.pushBinds(bound, true)
	bind := func(name string, v any) {
		prev, ok := env.vars[name]
		undo = append(undo, saved{name, prev, ok})
		env.vars[name] = v
		if bound != nil {
			bound[name] = v
		}
		if mine != nil {
			mine[name] = v
		}
	}

	// View attached properties, computed once per row against the view's
	// currentIndex ref (created by the view's builder before rows build).
	// Immediate-mode: a currentIndex change bumps the revision and rows
	// re-derive on the next layout. Bound BOTH on each id'd element (for
	// qualified dl.ListView.isCurrentItem) and as row-scope names (for the
	// bare ListView.isCurrentItem QML actually writes — shadowing the enum
	// namespaces for the row's duration).
	// Bound as a Ref rather than a computed map, so that reading
	// `ListView.isCurrentItem` records the dependency on the view's
	// currentIndex in the READER's read-set. Computed here and bound as
	// a plain map, the read was untracked — and since a poisoned nested
	// set poisons every set above it, two such reads on the chat page
	// were keeping fifteen subtrees a frame uncacheable.
	curRef := doc.synthRefs[viewKey+"|currentIndex"]
	attached := rowAttachedRef{ref: curRef, row: rowIdx}
	bind("ListView", attached)
	bind("GridView", attached)
	bind("PathView", attached)

	// Phase 1: per-row elemObjects for ids, with live hover/press state
	// pre-seeded — wireHoverPress installs the same refs (same doc.boolProp
	// keys) when the node builds, but a FORWARD reference from an earlier
	// sibling's binding (color: chapterMouse.containsMouse ? ...) evaluates
	// before that, and must find the member on the row's very first frame.
	walkRowNodesParent(delegate, nil, func(n, parentNode *node) {
		id := nodeID(n)
		if id == "" {
			return
		}
		// Don't shadow ids claimed by specialized objects (text inputs,
		// models, components) — only plain element views fork per row.
		switch env.vars[id].(type) {
		case nil, *elemObject:
		default:
			return
		}
		// A native (host-registered) object owns its id: instantiating it
		// binds the backend under that name when the declaration is
		// serviced, and a plain per-instance object here would shadow it —
		// `SettingsManager { id: settings }` inside a card, then
		// settings.allKeys() from its onCompleted.
		if isNativeType(n.typeName) {
			return
		}
		key := doc.idKey(id)
		kid := doc.here(id)
		obj := doc.elemObjs.get(kid)
		if obj == nil {
			obj = &elemObject{props: map[string]any{}}
			doc.elemObjs.set(kid, obj)
		}
		if obj.props == nil {
			obj.props = map[string]any{}
		}
		// Functions declared on this element (declareFuncs attaches them to
		// the unprefixed doc-level object) are code, not state — share them
		// into the instance object so id.fn(...) keeps resolving.
		if obj.methods == nil {
			if base := doc.elemObjs.get(doc.intern(id)); base != nil && base.methods != nil {
				obj.methods = base.methods
			}
		}
		hover := doc.boolProp(key+".containsMouse", false)
		obj.setProp("containsMouse", hover)
		obj.setProp("hovered", hover)
		obj.setProp("pressed", doc.boolProp(key+".pressed", false))
		fr, _ := doc.trackFocus(doc.intern(key), obj) // keyboard focus (focus.go)
		obj.setProp("activeFocus", fr)
		// Text fields own live input state; attach it so this instance's
		// siblings can read field.text / .activeFocus before it builds.
		if isTextInput(n.typeName) && obj.input == nil {
			obj.input = doc.inputFor(id, env)
		}
		if isScrollType(n.typeName) && obj.scroll == nil {
			obj.scroll = doc.scrollFor(key)
		}
		obj.setProp("ListView", attached)
		obj.setProp("GridView", attached)
		// Where this node sits, so `someId.parent.<prop>` resolves inside
		// the instance too (declareProps does the same document-globally).
		obj.node, obj.nodeEnv = n, env
		obj.doc, obj.key = doc, doc.keyID(n)
		if parentNode != nil {
			obj.parentObj = parentObject(parentNode, env, doc)
			obj.parentKey = doc.keyID(parentNode)
		}
		// Controls own live state (Slider.value, CheckBox.checked): seed it
		// per instance too, so a sibling reading id.value before the control
		// builds resolves on the row's first frame.
		seedControlState(n, env, doc)
		seedDerivedProps(n, env, doc, obj)
		seedCanvasMethods(n, env, obj)
		bind(id, obj)
	})

	// Defaults whose first evaluation hit a forward reference; retried once
	// the whole instance scope is declared (see below).
	var deferred []deferredDefault

	// Phase 2: per-row property refs. A `required property` is a delegate
	// slot the view fills from the model context (QML's required-property
	// idiom: modelData, index, or a role name) — it aliases the row value
	// bindRow already put in env, so only the qualified id.prop access needs
	// wiring. Every other declaration is ordinary per-row state, shadowing
	// any same-named role (QML scoping: own properties win over the model's
	// implicit role scope).
	walkRowNodes(delegate, func(n *node) {
		var obj *elemObject
		if id := nodeID(n); id != "" {
			obj = doc.elemObjs.get(doc.here(id)) // phase 1 created it (or skipped a specialized id)
		}
		if len(n.props) > 0 {
			// Defaults read this item's own geometry (QML self-geometry),
			// e.g. `property real bSize: Math.min(width, height - 20)`.
			restoreGeom := shadowSelfGeometry(n, env, doc)
			defer restoreGeom()
		}
		for _, d := range n.props {
			if d.typ == "alias" {
				// An alias forwards to another element's property IN THIS
				// instance (`property alias canvasWidth: canvas.width`), so
				// it resolves against the row/instance scope bound above.
				aenv := doc.scopeSnapshot(env, "alias:"+d.name)
				if obj != nil {
					obj.props[d.name] = aliasRef{target: d.def, env: aenv}
				}
				bind(d.name, aliasRef{target: d.def, env: aenv})
				continue
			}
			if d.required && isRowBinding(d.name, row) {
				if obj != nil {
					obj.props[d.name] = env.vars[d.name]
				}
				continue
			}
			rkey := doc.rowPrefix + "p:" + d.name
			raw, ok := doc.synthRefs[rkey]
			if !ok {
				def, err := d.def.eval(env)
				if err != nil {
					// A forward reference: the default reads a property or id
					// declared LATER in this instance (`property real ratio:
					// adiCanvas.pitch / 90`). Register the zero value now and
					// retry after the whole scope is declared.
					def = zeroFor(d.typ)
					deferred = append(deferred, deferredDefault{decl: d, key: rkey})
				}
				raw, err = newDeclaredProp(d.typ, def)
				if err != nil {
					logf("gml:%d: property %s: %v", d.line, d.name, err)
					continue
				}
				doc.synthRefs[rkey] = raw
				// The property's own on<Prop>Changed handler, wired once
				// with the ref — per INSTANCE, so a component that reacts
				// to its own property reacts on the instance whose property
				// moved. Wired only on the doc-global ref, a drawer's
				// `onOpenedChanged: position = ...` never ran for the
				// instance the user opened: `opened` went true and the panel
				// stayed put.
				if h := handlerFor(n, "on"+title(d.name)+"Changed"); h != nil {
					// Wired at the END of this scope, not here: the handler
					// reads the instance's OTHER properties, which are not
					// bound yet (a drawer's `onOpenedChanged: position =
					// ...` would have written the document's `position`
					// instead of the instance's, and the panel never moved).
					onChanged = append(onChanged, changedWiring{raw: raw, body: h, name: d.name})
				}
			} else if !doc.assigned[rkey] {
				// QML live binding, per instance: the default re-evaluates
				// every build while no imperative assignment has broken it
				// (a file component's use-site override `pitch: ctl.pitch`
				// must follow the control it names). Writes go through the
				// RAW ref so a binding update is not itself a break.
				if _, isElem := d.def.(*elemExpr); !isElem {
					if v, err := d.def.eval(env); err == nil {
						if !sameValue(raw.get(), v) {
							_ = raw.set(v)
						}
					}
				}
			}
			// Wrap so imperative writes (a Slider dragging this property,
			// `root.x = 1` in a handler) mark the binding broken.
			ref := Ref(trackedRef{Ref: raw, doc: doc, name: rkey})
			bind(d.name, ref)
			if obj != nil {
				obj.props[d.name] = ref
			}
		}
	})

	// The on<Prop>Changed handlers, now that every id and property in this
	// scope is bound: each keeps a snapshot of the WHOLE instance scope, so
	// it still resolves the instance's own state whenever it fires — long
	// after this scope has been unwound.
	if len(onChanged) > 0 {
		henv := env.snapshot()
		run := doc.handlerRunner(henv)
		for _, w := range onChanged {
			body, name := w.body, w.name
			w.raw.onChange(func() { run(body, "on"+title(name)+"Changed") })
		}
	}

	// Retry the forward-referencing defaults now that every id and property
	// in this scope is bound. One that STILL fails usually names an element
	// declared later in the document (a card bound to `controls.pitch` with
	// the controls panel below it), which no amount of retrying inside this
	// scope can resolve: queue it for the document's own retry pass, which
	// runs after the whole tree has built and logs what is left over — so a
	// genuine typo stays visible without every forward reference warning on
	// the first frame.
	for _, dd := range deferred {
		ref := doc.synthRefs[dd.key]
		if ref == nil {
			continue
		}
		v, err := dd.decl.def.eval(env)
		if err != nil {
			doc.pendingProps = append(doc.pendingProps, pendingProp{
				name: dd.decl.name, def: dd.decl.def, env: env.snapshot(),
				line: dd.decl.line, ref: ref,
			})
			continue
		}
		_ = ref.set(v)
		delete(doc.propRetries, fmt.Sprintf("%s:%d", dd.decl.name, dd.decl.line))
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

// walkRowNodes visits every node of one delegate instance's scope: the
// subtree minus nested views' delegate subtrees (a nested Repeater/ListView
// child is a separate per-row component with its own scope; only its
// ListModel children still belong to the outer document's data pass).
func walkRowNodes(n *node, visit func(*node)) {
	walkRowNodesParent(n, nil, func(c, _ *node) { visit(c) })
}

// walkRowNodesParent is walkRowNodes with each node's parent, for scope that
// depends on where the node sits (`someId.parent.<prop>`).
func walkRowNodesParent(n, parent *node, visit func(n, parent *node)) {
	visit(n, parent)
	if isDelegateHost(n.typeName) {
		return
	}
	// Element-valued attributes belong to the same instance: a
	// MapQuickItem's `sourceItem: Column { ... }` declares ids the row's
	// bindings read (`text: wpLabel.implicitWidth`).
	for _, a := range n.attrs {
		if a.elem != nil {
			walkRowNodesParent(a.elem, n, visit)
		}
	}
	for _, c := range n.children {
		walkRowNodesParent(c, n, visit)
	}
}

// isDelegateHost reports view types that instantiate their children per model
// row (their subtrees are row scopes of their own).
func isDelegateHost(t string) bool {
	switch t {
	case "Repeater", "ListView", "GridView", "PathView":
		return true
	}
	return false
}

// isRowBinding reports whether name is bound by the current row scope
// (bindRow): the implicit index/model/modelData, or one of the row's roles.
func isRowBinding(name string, row map[string]any) bool {
	if name == "index" || name == "model" {
		return true
	}
	if row == nil {
		return false
	}
	if name == "modelData" {
		return true
	}
	_, ok := row[name]
	return ok
}

// deferredDefault is a property default that could not evaluate on its
// first pass (forward reference), retried after the scope is complete.
type deferredDefault struct {
	decl propDecl
	key  string
}

// viewAttached is the view-attached property map a row scope binds under
// ListView/GridView/PathView. Two shared maps, one per isCurrentItem value:
// the contents are constants, and it was built for every instance scope on
// every build (a file component's, a styled control's — not just rows).
// Nothing writes into it; it is read as a namespace.
// rowAttachedRef is a row's `ListView` / `GridView` / `PathView`
// attached object, resolved on read: it takes the view's currentIndex
// through its Ref at that moment, so the read lands in the read-set of
// whatever is doing the reading.
type rowAttachedRef struct {
	ref Ref
	row int
}

func (r rowAttachedRef) get() any {
	current := false
	if r.ref != nil {
		if ci, err := toInt(r.ref.get()); err == nil {
			current = ci == r.row
		}
	}
	return viewAttached(current)
}

func (r rowAttachedRef) set(any) error {
	return fmt.Errorf("a view's attached properties are read-only")
}
func (r rowAttachedRef) animate(any, time.Duration) error {
	return fmt.Errorf("a view's attached properties are not animatable")
}
func (r rowAttachedRef) onChange(func()) {}

func viewAttached(current bool) map[string]any {
	if current {
		return viewAttachedCurrent
	}
	return viewAttachedOther
}

func makeViewAttached(current bool) map[string]any {
	return map[string]any{"isCurrentItem": current, "delayRemove": false,
		"section": "", "onPath": true,
		"NoHighlightRange": float64(0), "ApplyRange": float64(1), "StrictlyEnforceRange": float64(2),
		// The enum members too: this map SHADOWS the ListView namespace
		// for the row's duration, and a view built inside a row still
		// states `verticalLayoutDirection: ListView.BottomToTop`.
		"Horizontal": float64(1), "Vertical": float64(2),
		"TopToBottom": float64(0), "BottomToTop": float64(1),
		"LeftToRight": float64(0), "RightToLeft": float64(1)}
}

var (
	viewAttachedCurrent = makeViewAttached(true)
	viewAttachedOther   = makeViewAttached(false)
)

// sameValue reports whether a re-evaluated default is the value the
// property already holds, so the write (and the dirty marks it spreads)
// can be skipped. It is the comparison `str(a) != str(b)` used to make —
// numbers compare by value whatever their Go type, scalars by value — but
// without formatting: on a `var` property holding an object or an array,
// fmt's "%v" sorted the map's keys and printed every member, every row,
// every frame, and was a tenth of the chat and indicators pages' frames.
// Objects compare by identity first and by reflect.DeepEqual after, which
// is what the formatted comparison amounted to.
func sameValue(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil || str(b) == ""
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64, int, float32:
		if fb, ok := toFloat(b); ok {
			fa, _ := toFloat(a)
			return fa == fb
		}
		return false
	case colorVal:
		y, ok := b.(colorVal)
		return ok && x == y
	}
	if b == nil {
		return str(a) == ""
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return str(a) == str(b)
	}
	switch ta.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Func:
		if reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer() {
			return true
		}
	}
	return reflect.DeepEqual(a, b)
}

// toFloat is the numeric view of a scalar, for comparisons.
func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case float32:
		return float64(t), true
	}
	return 0, false
}
