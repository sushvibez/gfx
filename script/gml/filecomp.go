package gml

// QML-style file components: an element whose type has no builtin meaning
// resolves to <baseDir>/<Type>.gml — the file's root element is the
// component template. Use-site attributes override same-named property
// declarations (the QML customization point) or append as plain
// attributes (x, y, width, ...).
//
// v1 limits (single-instance chrome components): declared properties are
// document-global like every gml property, so two instances of a component
// share (collide on) its property names; component files are parsed once
// and do not hot-reload independently of the main document.

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/sushvibez/gfx/ui"
)

// fileComponent returns the cached parsed root of the component file for
// name — <baseDir>/<name>.gml, <baseDir>/<name>.qml, or a module type
// registered by an import — or nil when the name is not component-shaped
// or no file exists.
func (doc *docState) fileComponent(name string) *node {
	if name == "" || !unicode.IsUpper(rune(name[0])) || doc.baseDir == "" {
		return nil
	}
	if doc.fileComps == nil {
		doc.fileComps = map[string]*node{}
	}
	if n, ok := doc.fileComps[name]; ok {
		return n // nil = negative cache
	}
	paths := []string{
		filepath.Join(doc.baseDir, name+".gml"),
		filepath.Join(doc.baseDir, name+".qml"),
	}
	if p, ok := doc.moduleTypes[name]; ok {
		paths = append(paths, p)
	}
	var src []byte
	var err error
	var found string
	for _, p := range paths {
		if src, err = os.ReadFile(p); err == nil {
			found = p
			break
		}
	}
	if found == "" {
		doc.fileComps[name] = nil
		return nil
	}
	root, err := Parse(string(src))
	if err != nil {
		logf("gml: component %s: %v", name, err)
		doc.fileComps[name] = nil
		return nil
	}
	doc.fileComps[name] = root
	return root
}

// fileComponentAt parses (and caches) a component file addressed by a
// RELATIVE PATH — QML's `Loader { source: "screens/Foo.qml" }` form, as
// opposed to fileComponent's type-name lookup. Resolved against the
// document's baseDir; cached under the resolved path.
func (doc *docState) fileComponentAt(relPath string) *node {
	if relPath == "" || doc.baseDir == "" {
		return nil
	}
	path := filepath.Join(doc.baseDir, relPath)
	if doc.fileComps == nil {
		doc.fileComps = map[string]*node{}
	}
	if n, ok := doc.fileComps[path]; ok {
		return n // nil = negative cache
	}
	src, err := os.ReadFile(path)
	if err != nil {
		logf("gml: Loader source %q: %v", relPath, err)
		doc.fileComps[path] = nil
		return nil
	}
	root, err := Parse(string(src))
	if err != nil {
		logf("gml: Loader source %s: %v", relPath, err)
		doc.fileComps[path] = nil
		return nil
	}
	// Note: the loaded file's own imports/assets resolve against the
	// DOCUMENT's baseDir (like every file component) — Loader sources in
	// subdirectories should reference assets/imports relative to the
	// root document.
	doc.fileComps[path] = root
	return root
}

// useSiteAttr freezes a plain use-site attribute to the value it has in the
// USE SITE's scope, the scope QML evaluates it in — `Label { text: "" +
// root.selectedYear }` means the enclosing document's root, even though the
// style file the attribute lands on has its own `id: root` shadowing it (the
// same rule buildFileComponent applies to declared-property overrides).
// Re-done on every build, so the binding stays live.
//
// Only plain data survives the freeze. An attribute that evaluates to an
// object (`anchors.fill: parent`) or that fails to resolve out here keeps
// its expression, for the instance scope to resolve.
func useSiteAttr(a attr, env *Env) attr {
	a.fromUse = true
	if a.expr == nil || a.elem != nil || a.stmt != nil || strings.HasPrefix(a.name, "on") {
		return a
	}
	v, err := a.expr.eval(env)
	if err != nil {
		return a
	}
	switch v.(type) {
	case float64, float32, int, string, colorVal, bool, []any, map[string]any, nil:
		out := a
		out.expr = &litExpr{v: v}
		return out
	}
	return a
}

// prepareComponentImports loads a component file's own imports once.
func prepareComponentImports(root *node, env *Env, doc *docState) {
	if len(root.imports) > 0 {
		doc.resolveImports(root, env)
	}
}

// buildFileComponent instantiates a component template with the use-site
// node's attribute overrides applied.
// shareInstance gives `to` — the use site's view of a component instance —
// the instance's own functions and declared properties. Functions are the
// component's code, so they always win; properties fill in where the use
// site has none of its own, leaving its seeded interaction state alone.
func shareInstance(to, inst *elemObject) {
	if inst.methods != nil {
		if to.methods == nil {
			to.methods = map[string]any{}
		}
		for k, v := range inst.methods {
			to.methods[k] = v
		}
	}
	if inst.props != nil {
		if to.props == nil {
			to.props = map[string]any{}
		}
		for k, v := range inst.props {
			if _, ok := to.props[k]; !ok {
				to.setProp(k, v)
			}
		}
	}
	if to.input == nil {
		to.input = inst.input
	}
	if to.scroll == nil {
		to.scroll = inst.scroll
	}
}

// noteInstanceXY resolves the instance root's own x/y HERE, inside the
// component's scope, and records them for the use site.
//
// A component that places itself (`x: (parent.width - width) / 2 - cameraX
// * parallaxFactor` — a parallax layer centring on its scene) writes that
// binding in terms of its own declared properties, which exist only in this
// scope. The enclosing container positions the instance, and reading the
// binding from out there cannot see them.
func noteInstanceXY(key string, inst *node, env *Env, doc *docState) {
	var vals [2]float32
	var have [2]bool
	restoreGeom := shadowSelfGeometry(inst, env, doc)
	for _, a := range inst.attrs {
		i := 0
		switch a.name {
		case "x":
		case "y":
			i = 1
		default:
			continue
		}
		if f, err := attrFloat(a, env); err == nil {
			vals[i], have[i] = f, true
		}
	}
	restoreGeom()
	if !have[0] && !have[1] {
		return
	}
	if doc.instXY == nil {
		doc.instXY = map[string]instPos{}
	}
	doc.instXY[key] = instPos{
		x: vals[0], y: vals[1], hasX: have[0], hasY: have[1],
	}
}

// instPos is a file-component instance's resolved own position.
type instPos struct {
	x, y       float32
	hasX, hasY bool
}

func buildFileComponent(use *node, comp *node, env *Env, doc *docState) (ui.Element, error) {
	inst := *comp // shallow copy; slices replaced below where modified
	// What the USE SITE imposes and the style file cannot see: a width
	// (anchored to both sides, or a layout stretching it), which decides
	// whether a wrapping Label inside the style breaks its lines at all.
	inst.imposedWidth = use.imposedWidth || textWidthImposed(use)

	// Split use-site attrs: declared-property overrides vs plain attrs.
	overrides := map[string]expr{}
	declared := map[string]bool{}
	for _, d := range comp.props {
		declared[d.name] = true
	}
	useID := nodeID(use)
	var extra []attr
	for _, a := range use.attrs {
		if a.name == "id" {
			continue
		}
		if declared[a.name] {
			overrides[a.name] = a.expr
		} else {
			extra = append(extra, a)
		}
	}
	if len(overrides) > 0 {
		props := make([]propDecl, len(comp.props))
		copy(props, comp.props)
		for i := range props {
			e, ok := overrides[props[i].name]
			if !ok {
				continue
			}
			if props[i].typ == "alias" {
				continue // the def IS the alias target; written below
			}
			// A use-site binding is evaluated in the USE SITE's scope, not
			// the component's (QML lexical scoping): `AnalogClock { active:
			// root.fullSize }` means the enclosing document's root, even
			// though the component file has its own `id: root`. Evaluate it
			// here, outside the instance scope, and pass the value in.
			// Re-done on every build, so the binding stays live.
			if v, err := e.eval(env); err == nil {
				props[i].def = &litExpr{v: v}
			} else {
				props[i].def = e // unresolved here: retry inside the scope
			}
		}
		inst.props = props
	}
	// The use site's CHILDREN belong to the instance: QML puts them in the
	// component's default property, which for an Item-rooted component is
	// its children (`SceneLayer { Canvas { ... } }` — the layer holds the
	// canvas). They come after the component's own children, so they paint
	// on top, as declaration order does in QML.
	if len(use.children) > 0 {
		kids := make([]*node, 0, len(comp.children)+len(use.children))
		kids = append(kids, comp.children...)
		kids = append(kids, use.children...)
		inst.children = kids
		// `default property alias contentData: contentContainer.data`
		// redirects them INTO a nested element (a drawer's panel content
		// area), which is where QML puts them.
		if id := defaultChildTarget(comp); id != "" {
			if root, ok := appendChildrenInto(comp, id, use.children); ok {
				inst.children = root.children
			}
		}
	}
	if len(extra) > 0 {
		attrs := make([]attr, 0, len(comp.attrs)+len(extra))
		attrs = append(attrs, comp.attrs...)
		for _, a := range extra {
			attrs = append(attrs, useSiteAttr(a, env)) // later attrs win
		}
		inst.attrs = attrs
	}
	// What the use site handed in was evaluated HERE, in the use site's
	// scope, and baked into the instance as literals — so the instance's
	// subtree never read the inputs behind them. Its cache entry has to
	// depend on the VALUES instead: they are folded into a token the
	// instance's build reads (buildFresh), stamped when they change. Without
	// it a `Canvas1 { width: parent.width }` built while its parent was
	// still 0 wide was served at 0 for ever once nothing else in it moved.
	inst.instTok = doc.instToken(use, &inst, prevPrefixOf(doc), overrides, extra)
	prepareComponentImports(comp, env, doc)
	// Inside this component's tree, ITS import list decides which builtin
	// types a style module shadows.
	prevFile := doc.curFile
	doc.curFile = comp
	defer func() { doc.curFile = prevFile }()
	templated := false
	outerPrefix := ""
	var useSiteObjs []*elemObject
	if _, ok := isTemplateType(&inst, comp); ok {
		// Scope every state key (geometry, hover, checked) to THIS use site:
		// the component file's nodes are shared by all instantiations, so a
		// bare elemKey would make two styled controls share one slot — and
		// the delegates size themselves off root.height, which is that
		// element's previous-frame geometry.
		outerPrefix = doc.rowPrefix
		doc.rowPrefix = outerPrefix + "tpl:" + use.typeName + ":" + elemKey(use) + "/"
		prevPrefix := outerPrefix
		defer func() { doc.rowPrefix = prevPrefix }()
		// A Templates-based style control: bind the control contract
		// (text, enabled, palette, ...) before declaring properties, so
		// defaults reading control state resolve. The id shadow is undone
		// after this instantiation's build (ids are component-scoped).
		// Per-use-site key: two instances of the same styled control must
		// not share one checked/value slot.
		// The page's own view of this element (declared at load time, which
		// its bindings and handlers close over).
		var outer *elemObject
		if useID != "" {
			outer = doc.elemObjs.get(doc.idIn(prevPrefix, useID))
		}
		restoreSeed := seedTemplateScope(&inst, env, doc, doc.key(&inst), outer)
		defer restoreSeed()
		// The style's delegates are attribute values, so declare their ids
		// now — before the instance's Component.onCompleted is collected,
		// which is where a style pokes its own canvas
		// (`Component.onCompleted: dialCanvas.requestPaint()`).
		for _, a := range inst.attrs {
			switch a.name {
			case "background", "contentItem", "indicator", "handle":
				if a.elem != nil {
					restore := declareRowScope(a.elem, env, doc, nil, "", -1)
					defer restore()
				}
			}
		}
		templated = true
		// Every object the page already knows this id by, BEFORE the alias
		// below re-points the map entries at the style's own scope: a
		// handler on the page captured one of these, and what the control
		// publishes (a popup's open()) has to reach it.
		if useID != "" {
			seen := map[*elemObject]bool{}
			for _, o := range []*elemObject{
				doc.elemObjs.get(doc.intern(useID)), doc.elemObjs.get(doc.idIn(outerPrefix, useID)),
			} {
				if o != nil && !seen[o] {
					seen[o] = true
					useSiteObjs = append(useSiteObjs, o)
				}
			}
			if o, ok := env.vars[useID].(*elemObject); ok && o != nil && !seen[o] {
				useSiteObjs = append(useSiteObjs, o)
			}
		}
		// The use-site id (Button { id: pressBtn }) aliases the control's
		// scope object, so pressBtn.down/checked/text resolve.
		if useID != "" {
			if obj := doc.elemObjs.get(doc.intern(nodeID(&inst))); obj != nil {
				doc.elemObjs.set(doc.intern(useID), obj)
				env.vars[useID] = obj
				// The page may already hold its own view of this id — the
				// row/instance scope it was declared in, which its own
				// bindings and handlers close over. Share the control's
				// STATE refs with it, or the page and the control keep
				// separate values for the same property (a date picker
				// setting `yearTumbler.currentIndex` and a wheel that never
				// turned).
				shareControlState(obj, doc.elemObjs.get(doc.idIn(prevPrefix, useID)))
			}
		}
	}
	if err := declareProps(&inst, env, doc); err != nil {
		return nil, err
	}
	// The component's own functions (function deptColor(d) {...}) — the main
	// document's load-time declareFuncs never sees this separate tree. After
	// declareProps so the root id's elemObject exists for method attachment.
	declareFuncs(&inst, env)
	// Per-instance scope (non-templated): declared properties and internal
	// ids are component-scoped in QML — N use-sites are N instances, each
	// with its own values (a red Chamfer and a dark Chamfer must not share
	// one doc-global `fill` slot). Reuses the delegate row-scope machinery,
	// keyed by the use-site, layered over the doc-global declarations above
	// (which still own ListModels, functions and onChanged wiring).
	if !templated {
		prevPrefix := doc.rowPrefix
		doc.rowPrefix = prevPrefix + "fc:" + use.typeName + ":" + elemKey(use) + "/"
		restoreScope := declareRowScope(&inst, env, doc, nil, "", -1)
		// Re-close this instance's functions over the instance scope: a
		// setter called from a handler (setDay writing dayTumbler.currentIndex)
		// must reach THIS instance's state, not the doc-global slot the
		// pre-instance pass closed over.
		declareFuncsScoped(&inst, env, doc.scopeSnapshot(env, "funcs"))
		// The use-site id names THIS instance: `HudControlsPanel { id: ctl }`
		// then `ctl.pitch` elsewhere must read the instance's own property,
		// not the doc-global slot of the same name. Alias the use-site id to
		// the instance root's scope object (outlives this build, so reads
		// from the surrounding document resolve on later frames too).
		// A component may declare an id its USE SITE also carries —
		// `BreathingCircles { id: breathSection }` around an inner
		// `Item { id: breathSection }`, which the corpus does twice. In QML
		// the component's own scope wins INSIDE the component, so the
		// aliasing below must leave that binding (and that object) alone:
		// pointing the name at the instance root instead cost the inner
		// Item's own properties (`breathSection.time` from its Canvas read
		// a root that has no `time`).
		shadowed := useID != "" && declaresIDInside(&inst, useID)
		if rootID := nodeID(&inst); useID != "" && rootID != "" && !shadowed {
			if obj := doc.elemObjs.get(doc.here(rootID)); obj != nil {
				// Every object that names the use site becomes THIS one: the
				// binding in env is undone when the enclosing scope unwinds,
				// so a handler that fires later (a click, frames on) would
				// otherwise resolve the id to an object carrying none of the
				// component's own state or functions — `leftDrawer.open()`
				// failed as `no member "open"` from the very button written
				// to call it.
				for _, key := range []string{doc.rowPrefix, prevPrefix, ""} {
					prev := doc.elemObjs.get(doc.idIn(key, useID))
					if prev == obj {
						continue
					}
					if prev != nil {
						// The instance's own functions and properties are
						// shared INTO the use-site's object rather than
						// replacing it: a handler holds whichever object was
						// bound when it was built, and both have to answer.
						shareInstance(prev, obj)
					}
					if prev == nil {
						doc.elemObjs.set(doc.idIn(key, useID), obj)
					}
				}
				env.vars[useID] = obj
			}
		}
		// A shadowed use-site id still has to answer from OUTSIDE the
		// component (the page wrote `BreathingCircles { id: breathSection }`
		// to read it by), so the outer keys point at the instance root —
		// only this instance's own binding is left as the component
		// declared it.
		if shadowed {
			inner := doc.elemObjs.get(doc.here(useID))
			if obj := doc.elemObjs.get(doc.here(nodeID(&inst))); obj != nil {
				for _, key := range []string{prevPrefix, ""} {
					prev := doc.elemObjs.get(doc.idIn(key, useID))
					switch {
					case prev == nil:
						doc.elemObjs.set(doc.idIn(key, useID), obj)
					case prev != inner && prev != obj:
						// The page's own view of the use site answers with
						// the instance's properties, as it does for an
						// unshadowed id — only the component's OWN object
						// (and its binding) is left alone.
						shareInstance(prev, obj)
					}
				}
			}
		}
		// The instance root's x/y are read relative to whatever the use
		// site sits in, as QML's are: the component file was parsed on its
		// own, so nothing else can know its parent. Without this `root.x`
		// inside a component reports window coordinates, and anything
		// positioned by it (a glass card pulling its captured background
		// back by -root.x) lands offset by the enclosing item's origin.
		if rootID := nodeID(&inst); rootID != "" {
			// Re-taken every build: the parent object is rebuilt with the
			// frame, and a stale one carries a stale (or missing) element,
			// which reads the same as having no parent at all.
			if obj := doc.elemObjs.get(doc.here(rootID)); obj != nil {
				if p, ok := env.lookup("parent"); ok {
					if po, ok := p.(*elemObject); ok {
						obj.parentObj = po
					}
				}
			}
		}
		// Use-site values for ALIAS properties: an alias forwards to an
		// element inside this instance, which only exists once the scope
		// above is bound, so the write happens here rather than with the
		// ordinary declared-property overrides.
		for _, d := range inst.props {
			if d.typ != "alias" {
				continue
			}
			e, ok := overrides[d.name]
			if !ok {
				continue
			}
			v, err := e.eval(env)
			if err != nil {
				continue
			}
			if err := (aliasRef{target: d.def, env: env}).set(v); err != nil {
				logf("gml:%d: alias %s: %v", d.line, d.name, err)
			}
		}
		noteInstanceXY(prevPrefix+elemKey(use), &inst, env, doc)
		// A slot, for the reason the templated path below is one: the
		// instance's own subtree patches in place, so the use site gets
		// back the same element it already finished. declareRowScope
		// pushed this instance's scope onto the bind stack, so the slot
		// captures it and a later patch rebuilds under it.
		el, err := doc.slot(env, func() (ui.Element, error) { return build(&inst, env, doc) })
		restoreScope()
		doc.rowPrefix = prevPrefix
		return el, err
	}
	// NOT doc.retryPendingProps() here: a property default referencing a
	// sibling ELEMENT (readonly property int listenPort: listenPortSpin.value)
	// can only resolve once that sibling has actually been built, which
	// happens in the build(&inst, ...) call below — retrying now would
	// fail again and permanently drop the pending default. The document's
	// View() retries once more after the whole tree finishes building.
	if templated {
		// Declared properties are document-global by name, so a second
		// instance would inherit the first's values (a flat Button reusing
		// a solid one's __bgColor). Re-evaluate this file's declarations
		// against the freshly seeded scope on every instantiation — with
		// the control's own geometry in scope, since a style's derived
		// sizes read it bare (`readonly property real _dialSize:
		// Math.min(width, height)`).
		restoreGeom := shadowSelfGeometry(&inst, env, doc)
		for _, d := range inst.props {
			if d.typ == "alias" {
				continue
			}
			ref, ok := doc.props[doc.propKey(d.name)]
			if !ok {
				continue
			}
			if v, err := d.def.eval(env); err == nil {
				_ = ref.set(v)
			}
		}
		restoreGeom()
	}
	// While a STYLED control builds, remember which id the page knows it by
	// — anything the control publishes imperatively (a popup's open()) has
	// to reach the page's own object for that id, not only the style's
	// `root`. Restored after, since instantiations nest.
	if templated && useID != "" {
		prev := doc.useSite
		if obj := doc.elemObjs.get(doc.here(nodeID(&inst))); obj != nil {
			useSiteObjs = append(useSiteObjs, obj)
		}
		doc.useSite = useSite{id: useID, objs: useSiteObjs}
		defer func() { doc.useSite = prev }()
	}
	// The instance is a SLOT of the use site, so a change deep inside it
	// can be redone without rebuilding the use site (see docState.patch).
	//
	// This is what the use site's own build costs: resolving the
	// component, merging the use-site attributes into a derived node,
	// declaring the instance's properties, binding its scope. On the
	// buttons page that work was 58 % of a frame, run twice — once for
	// the card, once for the styled button inside it — every frame the
	// pulse animation ticked, to end up with the same elements.
	//
	// It works because the instance almost always comes back as the
	// SAME element: its own subtree patches in place, and patch's fast
	// path then has nothing to swap. The use site's element is that
	// element, already finished (layout hints, tooltip wrapper,
	// registration) by the build that made it, and none of that has to
	// be redone. When the instance does come back as a new element the
	// patch bails out and the use site rebuilds as before — the finish
	// step is what would be missing, and it is not re-applicable.
	return doc.slot(env, func() (ui.Element, error) { return build(&inst, env, doc) })
}

// defaultChildTarget returns the id a component's `default property alias`
// forwards its children to (`default property alias contentData:
// panel.data`), or "" when the component has no such alias — then a use
// site's children belong to the component's root, as usual.
func defaultChildTarget(comp *node) string {
	for _, d := range comp.props {
		if !d.isDflt || d.typ != "alias" {
			continue
		}
		m, ok := d.def.(*memberExpr)
		if !ok {
			continue
		}
		switch m.name {
		case "data", "children", "contentData", "contentChildren":
		default:
			continue
		}
		if base, ok := m.base.(*identExpr); ok {
			return base.name
		}
	}
	return ""
}

// appendChildrenInto returns a copy of the tree with kids appended to the
// element carrying id (copying only the path down to it, so the component's
// shared node tree is left alone and other instances are unaffected).
func appendChildrenInto(n *node, id string, kids []*node) (*node, bool) {
	if nodeID(n) == id {
		cp := *n
		cp.children = append(append([]*node{}, n.children...), kids...)
		return &cp, true
	}
	for i, c := range n.children {
		if nc, ok := appendChildrenInto(c, id, kids); ok {
			cp := *n
			cp.children = append([]*node{}, n.children...)
			cp.children[i] = nc
			return &cp, true
		}
	}
	return n, false
}

// shareControlState copies a styled control's live state refs onto another
// view of the same element, so reads and writes through either land in the
// same property. Only Refs travel: plain contract values (text, palette)
// belong to the control alone, and copying them would shadow whatever the
// page declared under the same name.
func shareControlState(from, to *elemObject) {
	if from == nil || to == nil || from == to || from.props == nil {
		return
	}
	if to.props == nil {
		to.props = map[string]any{}
	}
	for name, v := range from.props {
		if _, isRef := v.(Ref); !isRef {
			continue
		}
		to.setProp(name, v)
	}
}

// declaresIDInside reports whether a component's tree declares the given id
// on something OTHER than its root — an id the component owns, which must
// keep its meaning inside the component even when the use site carries the
// same name (QML scopes a component file's ids to that file).
func declaresIDInside(root *node, id string) bool {
	found := false
	walkRowNodes(root, func(n *node) {
		if n != root && nodeID(n) == id {
			found = true
		}
	})
	return found
}
