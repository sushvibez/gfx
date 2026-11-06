package gml

import (
	"github.com/sushvibez/gfx/core"

	"fmt"

	"github.com/sushvibez/gfx/ui"
)

// QML data models: ListModel/ListElement define rows of named roles, and
// ListView/Repeater bind a delegate against each row. A delegate sees the
// row's roles as plain identifiers (QML's implicit role scope) plus the
// explicit `model.role` form and the `index` of the row — matching Qt Quick.

// listModel is a static table of rows; each row maps role name to value.
// (Runtime append/remove — QML's ListModel methods — is not modeled yet;
// models are data definitions refreshed on hot reload.)
type listModel struct {
	rows []map[string]any
	// ver stamps every mutation as a reactive read/write, so a subtree that
	// READ the model (a view's rows) is proven stale exactly when the model
	// changes — rows live outside the property system, and marking the read
	// untracked instead would rebuild every view with a model every frame.
	ver core.Property[int]
	// seeded is the row set the ListElement declarations last evaluated to.
	// Re-declaring the model (a file component rebuilding, a hot reload)
	// re-seeds only when the declarations themselves changed, so rows a
	// handler appended at runtime survive the rebuild.
	seeded []map[string]any
}

// call dispatches QML ListModel's runtime-mutation methods. Mutations
// take effect on the next rebuild (bindings re-evaluate against rows).
func (m *listModel) call(name string, argExprs []expr, env *Env, line int) (any, error) {
	args, err := evalArgs(argExprs, env)
	if err != nil {
		return nil, err
	}
	return m.invoke(name, args, line)
}

// invoke is call on already-evaluated arguments — what a method bound BARE
// into a handler's scope calls (a ListModel's own Component.onCompleted
// says `append({...})`, not `model.append({...})`).
func (m *listModel) invoke(name string, args []any, line int) (any, error) {
	// Every mutation below bumps the frame revision; the version property
	// carries the same news to the per-subtree read-sets (see ver).
	defer func() {
		if name != "get" {
			m.ver.Set(m.ver.Get() + 1)
		}
	}()
	asRow := func(v any) map[string]any {
		if r, ok := v.(map[string]any); ok {
			return r
		}
		return map[string]any{"modelData": v}
	}
	switch name {
	case "append":
		if len(args) == 1 {
			m.rows = append(m.rows, asRow(args[0]))
		}
		core.BumpRevision()
		return nil, nil
	case "insert":
		if len(args) == 2 {
			i, _ := toInt(args[0])
			if i < 0 {
				i = 0
			}
			if i > len(m.rows) {
				i = len(m.rows)
			}
			m.rows = append(m.rows[:i], append([]map[string]any{asRow(args[1])}, m.rows[i:]...)...)
		}
		core.BumpRevision()
		return nil, nil
	case "remove":
		if len(args) >= 1 {
			i, _ := toInt(args[0])
			n := 1
			if len(args) >= 2 {
				n, _ = toInt(args[1])
			}
			if i >= 0 && i < len(m.rows) {
				end := min(i+n, len(m.rows))
				m.rows = append(m.rows[:i], m.rows[end:]...)
			}
		}
		core.BumpRevision()
		return nil, nil
	case "clear":
		m.rows = nil
		core.BumpRevision()
		return nil, nil
	case "set":
		if len(args) == 2 {
			if i, _ := toInt(args[0]); i >= 0 && i < len(m.rows) {
				m.rows[i] = asRow(args[1])
			}
		}
		core.BumpRevision()
		return nil, nil
	case "get":
		if len(args) == 1 {
			if i, _ := toInt(args[0]); i >= 0 && i < len(m.rows) {
				return m.rows[i], nil
			}
		}
		return nil, nil
	}
	return nil, fmt.Errorf("gml:%d: ListModel has no method %q", line, name)
}

// nodeID returns the value of a node's `id` attribute, if any.
func nodeID(n *node) string {
	for _, a := range n.attrs {
		if a.name == "id" {
			if id, ok := a.expr.(*identExpr); ok {
				return id.name
			}
		}
	}
	return ""
}

// registerModel evaluates a ListModel's ListElement rows once and binds the
// model into env under its id. Called from the declaration pass so a
// ListView may reference a model declared anywhere in the document.
func registerModel(n *node, env *Env, doc *docState) error {
	id := nodeID(n)
	if id == "" {
		return fmt.Errorf("gml:%d: ListModel needs an id", n.line)
	}
	rows, err := listModelRows(n, env)
	if err != nil {
		return err
	}
	key := doc.idKey(id)
	m := doc.models[key]
	if m != nil {
		if !sameRows(m.seeded, rows) {
			m.rows, m.seeded = rows, rows
		}
	} else {
		m = &listModel{rows: rows, seeded: rows}
		doc.models[key] = m
	}
	env.vars[id] = m
	// Also under the un-shadowable synthetic name: a later declaration pass
	// re-points the id at the element OBJECT for that id (a page reads
	// `dyn.count` through it), and a model lookup by id would then miss the
	// model itself.
	env.vars[inlineModelVar(n)] = m
	doc.queueModelCompleted(n, m, env)
	return nil
}

// registerInlineModel is registerModel for a model declared inline with no
// id (`model: ListModel { Component.onCompleted: ... }`): nothing can name
// it, but it still needs ONE instance across rebuilds, so rows a handler
// appended are not thrown away. It is keyed by position and reachable from
// resolveModel through the env.
func registerInlineModel(n *node, env *Env, doc *docState) *listModel {
	rows, err := listModelRows(n, env)
	if err != nil {
		logf("gml:%d: %v", n.line, err)
		return nil
	}
	key := doc.key(n)
	m := doc.models[key]
	if m != nil {
		if !sameRows(m.seeded, rows) {
			m.rows, m.seeded = rows, rows
		}
	} else {
		m = &listModel{rows: rows, seeded: rows}
		doc.models[key] = m
	}
	env.vars[inlineModelVar(n)] = m
	doc.queueModelCompleted(n, m, env)
	return m
}

// inlineModelVar is the env name an id-less inline model is reachable
// under: not a QML name (it starts with a character markup cannot write),
// so it cannot collide with anything a page declares.
func inlineModelVar(n *node) string { return "@model:" + elemKey(n) }

// queueModelCompleted runs a ListModel's own Component.onCompleted, which
// is where QML fills a model imperatively (`for (...) append({...})` — the
// gridview page's dynamic grid, and any model built from a loop). A model
// is DECLARED, never built, so nothing else collects its lifecycle
// handlers; and inside them the model's own methods resolve BARE, as they
// do on any QML object.
func (doc *docState) queueModelCompleted(n *node, m *listModel, env *Env) {
	if doc.completed {
		return
	}
	_, tasks := extractComponentHandlers(n, env)
	if len(tasks) == 0 {
		return
	}
	scope := env.snapshot().child()
	for _, name := range []string{"append", "insert", "remove", "clear", "set", "get", "move", "sync"} {
		method := name
		scope.vars[name] = jsFunc(func(args []any) (any, error) {
			return m.invoke(method, args, n.line)
		})
	}
	for i := range tasks {
		tasks[i].env = scope
		tasks[i].self = "" // the model is not in the id registry
	}
	doc.onCompleted = append(doc.onCompleted, tasks...)
}

// sameRows reports whether two declared row sets carry the same values.
func sameRows(a, b []map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for k, v := range a[i] {
			other, ok := b[i][k]
			if !ok || fmt.Sprint(v) != fmt.Sprint(other) {
				return false
			}
		}
	}
	return true
}

// listModelRows evaluates a ListModel node's ListElement rows.
func listModelRows(n *node, env *Env) ([]map[string]any, error) {
	var rows []map[string]any
	for _, c := range n.children {
		if c.typeName != "ListElement" {
			return nil, fmt.Errorf("gml:%d: ListModel contains only ListElement, got %q", c.line, c.typeName)
		}
		row := map[string]any{}
		for _, a := range c.attrs {
			if a.name == "id" || a.expr == nil {
				continue
			}
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, fmt.Errorf("gml:%d: ListElement %s: %v", a.line, a.name, err)
			}
			row[a.name] = v
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// tracked returns the model's rows AS A READ: the version property is
// touched so the reading subtree records a dependency on the model (see
// listModel.ver).
func (m *listModel) tracked() ([]map[string]any, int) {
	m.ver.Get()
	return m.rows, len(m.rows)
}

// modelFromEnv returns the registered listModel bound under name, if any.
func modelFromEnv(env *Env, name string) *listModel {
	if name == "" {
		return nil
	}
	v, ok := env.lookup(name)
	if !ok {
		return nil
	}
	if r, isRef := v.(Ref); isRef {
		v = r.get()
	}
	m, _ := v.(*listModel)
	return m
}

// modelObject returns the model OBJECT a `model:` attribute names, when it
// is one: pages read it back off the view (`view.model.get(i).label`).
func modelObject(a attr, env *Env) any {
	if a.elem != nil {
		if id := nodeID(a.elem); id != "" {
			if v, ok := env.lookup(id); ok {
				return v
			}
		}
		// An inline model with no id is still readable through the view
		// (`view.model.get(i)`): the registered instance when it has one,
		// so a read sees rows a handler appended.
		if m := modelFromEnv(env, inlineModelVar(a.elem)); m != nil {
			return m
		}
		rows, err := listModelRows(a.elem, env)
		if err != nil {
			return nil
		}
		return &listModel{rows: rows, seeded: rows}
	}
	if a.expr == nil {
		return nil
	}
	v, err := resolveAny(a.expr, env)
	if err != nil {
		return nil
	}
	if r, ok := v.(Ref); ok {
		v = r.get()
	}
	if m, ok := v.(*listModel); ok {
		return m
	}
	return nil
}

// resolveModel reads a `model:`/`count:` attribute: an inline ListModel
// element, an integer count (rows nil, bare index only), a JS array, or a
// ListModel reference (rows + role binding).
func resolveModel(a attr, env *Env) (rows []map[string]any, count int, err error) {
	// Inline element-valued model: `model: ListModel { ... }`.
	if a.elem != nil {
		if a.elem.typeName != "ListModel" {
			return nil, 0, fmt.Errorf("gml:%d: model wants a ListModel element, got %s", a.line, a.elem.typeName)
		}
		// The REGISTERED instance when there is one (see registerModel /
		// registerInlineModel): its rows include whatever a handler
		// appended, where the declarations alone are only the seed.
		// The synthetic name first: an id can be shadowed by the element
		// object registered for it, the synthetic one cannot.
		for _, name := range []string{inlineModelVar(a.elem), nodeID(a.elem)} {
			if m := modelFromEnv(env, name); m != nil {
				rows, n := m.tracked()
				return rows, n, nil
			}
		}
		rows, err := listModelRows(a.elem, env)
		return rows, len(rows), err
	}
	if a.expr == nil {
		return nil, 0, nil
	}
	v, err := resolveAny(a.expr, env)
	if err != nil {
		return nil, 0, err
	}
	// A bare identifier bound to a declared `property var` resolves to the
	// Ref itself (resolveAny intentionally doesn't dereference — callers
	// like resolveRef want the Ref); model: needs the underlying value.
	if r, ok := v.(Ref); ok {
		v = r.get()
	}
	if m, ok := v.(*listModel); ok {
		rows, n := m.tracked()
		return rows, n, nil
	}
	// A plain JS array: object elements expose their keys as roles AND the
	// whole row object as `modelData` (QML: modelData is always available,
	// object rows or not — a delegate that reads `modelData.foo` instead of
	// the bare `foo` role, or is written for a primitive-array model and
	// reused against an object one, still resolves); primitive elements are
	// reachable only via `modelData`.
	// An object-backed model: a Go backend (or any element) exposing rows.
	// QML's C++ models are objects too — a QSqlQueryModel bound straight to
	// a ListView — so accept anything carrying a `rows` (or `table`) array.
	if arr, ok := objectRows(v); ok {
		v = arr
	}
	if arr, ok := v.([]any); ok {
		rows := make([]map[string]any, len(arr))
		for i, item := range arr {
			if row, ok := item.(map[string]any); ok {
				merged := make(map[string]any, len(row)+1)
				for k, rv := range row {
					merged[k] = rv
				}
				if _, has := merged["modelData"]; !has {
					merged["modelData"] = row
				}
				rows[i] = merged
			} else {
				rows[i] = map[string]any{"modelData": item}
			}
		}
		return rows, len(rows), nil
	}
	if v == nil {
		// `model: null` — or a `property var` holding the model that the
		// backend has not filled yet. QML shows nothing; it is not an error.
		return nil, 0, nil
	}
	n, err := toInt(v)
	if err != nil {
		return nil, 0, fmt.Errorf("gml:%d: model wants a count or a ListModel: %v", a.line, err)
	}
	return nil, n, nil
}

// bindRow injects row i's scope into env (index, model, and each role as a
// bare identifier) and returns a restore func that undoes exactly those
// bindings — so delegate scope never leaks into siblings or declared props.
//
// vals, when non-nil, binds every one of those names as a Ref reading
// through the row's token instead of as a plain value, which is what lets
// a delegate's subtrees be cached at all (see rowtoken.go). Nil keeps the
// plain binding, for rows that cannot be hashed and for handler runs,
// which rebind a row outside any build.
func bindRow(env *Env, row map[string]any, i int, vals *rowVals) func() {
	type prev struct {
		v  any
		ok bool
	}
	keys := []string{"index", "model"}
	for k := range row {
		keys = append(keys, k)
	}
	snap := make(map[string]prev, len(keys))
	for _, k := range keys {
		v, ok := env.vars[k]
		snap[k] = prev{v, ok}
	}
	if vals != nil {
		env.vars["index"] = rowValRef{vals, "index"}
		if row != nil {
			env.vars["model"] = rowValRef{vals, "model"}
			for k := range row {
				env.vars[k] = rowValRef{vals, k}
			}
		} else {
			delete(env.vars, "model")
		}
	} else {
		env.vars["index"] = float64(i)
		if row != nil {
			env.vars["model"] = row
			for k, v := range row {
				env.vars[k] = v
			}
		} else {
			delete(env.vars, "model")
		}
	}
	return func() {
		for k, p := range snap {
			if p.ok {
				env.vars[k] = p.v
			} else {
				delete(env.vars, k)
			}
		}
	}
}

// rowBind is one delegate row's scope, kept so handlers built inside the
// row can re-establish it when they run (possibly frames later).
type rowBind struct {
	row map[string]any
	idx int
	// ids are the per-row bindings this scope made (the delegate's ids and
	// its declared properties), so a handler can be run inside them long
	// after the row itself was built.
	ids map[string]any
}

// handlerRunner captures the row-scope stack active at build time and
// returns a runner that re-binds it (outermost first) around handler
// statements. Outside any delegate it is a plain runStmts.
func (doc *docState) handlerRunner(env *Env) func(stmts []stmt, what string) {
	// `parent` is bound only while an element's own attributes are being
	// built, and a handler runs LATER — so capture what it meant here. QML
	// handlers reach their surroundings through it
	// (`onClicked: parent.parent.originMode = 1`), which failed as
	// "unknown identifier parent" and left the page inert.
	outer, hasOuter := env.lookup("parent")
	// Binds the captured parent for the handler's run (no map for one
	// name: a handler was built for every box on every build).
	bindParent := func() func() {
		if !hasOuter {
			return noRestore
		}
		prev, had := env.vars["parent"]
		env.vars["parent"] = outer
		return func() {
			if had {
				env.vars["parent"] = prev
			} else {
				delete(env.vars, "parent")
			}
		}
	}
	if len(doc.rowScope) == 0 {
		return func(stmts []stmt, what string) {
			restore := bindParent()
			err := runStmts(stmts, env)
			restore()
			if err != nil {
				logf("gml: %s: %v", what, err)
			}
		}
	}
	scope := make([]rowBind, len(doc.rowScope))
	copy(scope, doc.rowScope)
	return func(stmts []stmt, what string) {
		restores := []func(){bindParent()}
		for _, rb := range scope {
			restores = append(restores, bindRow(env, rb.row, rb.idx, nil))
			restores = append(restores, bindVars(env, rb.ids))
		}
		err := runStmts(stmts, env)
		for i := len(restores) - 1; i >= 0; i-- {
			restores[i]()
		}
		if err != nil {
			logf("gml: %s: %v", what, err)
		}
	}
}

// bindVars installs vars into env and returns the undo. Used to put a
// delegate's own ids and properties back in scope while its handler runs.
func bindVars(env *Env, vars map[string]any) func() {
	if len(vars) == 0 {
		return func() {}
	}
	type prev struct {
		v  any
		ok bool
	}
	snap := make(map[string]prev, len(vars))
	for k, v := range vars {
		old, ok := env.vars[k]
		snap[k] = prev{old, ok}
		env.vars[k] = v
	}
	return func() {
		for k, p := range snap {
			if p.ok {
				env.vars[k] = p.v
			} else {
				delete(env.vars, k)
			}
		}
	}
}

// buildRepeater expands a Repeater inline: it builds its single delegate
// child once per model row and returns the elements for the parent to splice
// into its own children (Repeater has no box of its own, like QML).
func buildRepeater(n *node, env *Env, doc *docState) ([]ui.Element, error) {
	return buildRepeaterRows(n, env, doc, nil)
}

// buildRepeaterRows expands a Repeater, building each row through `each`
// when one is given. `each` runs INSIDE the row scope, so a caller that
// needs the delegate's own per-row expressions (an anchored container
// parsing the row's x/y) sees the row's values.
func buildRepeaterRows(n *node, env *Env, doc *docState, each func(*node) (ui.Element, error)) ([]ui.Element, error) {
	var modelAttr *attr
	for i := range n.attrs {
		switch n.attrs[i].name {
		case "id":
		case "model", "count":
			modelAttr = &n.attrs[i]
		case "delegate":
			// consumed by delegateChild (element-valued)
		case "width", "height", "anchors.fill", "visible":
			// the Repeater has no box of its own; sizing hints are no-ops
		default:
			return nil, fmt.Errorf("gml:%d: Repeater has no attribute %q", n.attrs[i].line, n.attrs[i].name)
		}
	}
	if modelAttr == nil {
		return nil, fmt.Errorf("gml:%d: Repeater needs a model", n.line)
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	rows, count, err := resolveModel(*modelAttr, env)
	if err != nil {
		return nil, err
	}
	// A Repeater's id exposes its count (`for (i = 0; i < rep.count; i++)`).
	doc.exposeIDProp(n, "count", float64(count))
	viewKey := doc.key(n)
	out := make([]ui.Element, 0, count)
	for i := 0; i < count; i++ {
		var row map[string]any
		if rows != nil {
			row = rows[i]
		}
		restore := enterRow(viewKey, delegate, env, doc, row, i)
		var el ui.Element
		var err error
		if each != nil {
			el, err = each(delegate)
		} else {
			el, err = build(delegate, env, doc)
		}
		restore()
		if err != nil {
			return nil, err
		}
		out = append(out, el)
	}
	return out, nil
}

// delegateChild returns the single visual delegate child of a model view,
// ignoring any non-visual children (none today, but keeps the rule explicit).
func delegateChild(n *node) (*node, error) {
	// QML form: `delegate: <Element> { ... }` (element-valued attribute),
	// optionally wrapped in a Component. The LAST one wins, as attributes
	// do everywhere else: a styled control carries its style's delegate
	// first and the use site's after it.
	for i := len(n.attrs) - 1; i >= 0; i-- {
		if a := n.attrs[i]; a.name == "delegate" && a.elem != nil {
			return unwrapComponent(a.elem), nil
		}
	}
	// Fallback: the single visual child is the delegate.
	var delegate *node
	for _, c := range n.children {
		if isNonVisual(c) {
			continue
		}
		if delegate != nil {
			return nil, fmt.Errorf("gml:%d: %s needs exactly one delegate child", n.line, n.typeName)
		}
		delegate = c
	}
	if delegate == nil {
		return nil, fmt.Errorf("gml:%d: %s needs a delegate child", n.line, n.typeName)
	}
	return delegate, nil
}

// unwrapComponent returns a Component's single child (QML wraps delegates in
// Component { ... }); other elements pass through unchanged.
func unwrapComponent(n *node) *node {
	if n.typeName == "Component" && len(n.children) == 1 {
		return n.children[0]
	}
	return n
}

// isNonVisual reports nodes that declare data/behavior rather than render
// (skipped when collecting a parent's children).
func isNonVisual(n *node) bool {
	if isNativeType(n.typeName) {
		return true // Go-registered backend objects render nothing
	}
	switch n.typeName {
	case "ListModel", "TableModel", "ItemSelectionModel", "Timer",
		// Behaviors and standalone animations have no visual presence. Behavior
		// on <prop> drives a real tween (see collectBehaviors); the standalone
		// animation elements are still accepted as no-ops in immediate mode.
		"Behavior", "NumberAnimation", "PropertyAnimation", "ColorAnimation",
		"RotationAnimation", "SequentialAnimation", "ParallelAnimation", "PauseAnimation",
		"PropertyAction", "ScriptAction", "SpringAnimation", "SmoothedAnimation",
		// The *Animator variants (render-thread animations) are the same
		// declarations here.
		"NumberAnimator", "RotationAnimator", "OpacityAnimator",
		"ScaleAnimator", "XAnimator", "YAnimator", "UniformAnimator",
		// Component declarations don't render in place (instantiated by Loader/
		// delegates); Connections wires signals.
		"Component", "Connections",
		// Grouping/attached helpers with no visual presence.
		"ButtonGroup", "ActionGroup", "Settings", "FontLoader",
		"SystemPalette", "Shortcut",
		// Binding writes a property while its `when` holds; FrameAnimation
		// runs a handler each frame. Both are declarations, not visuals.
		"Binding", "FrameAnimation":
		return true
	}
	// A particle part declared OUTSIDE its ParticleSystem, bound to it by
	// `system:` — QML lets an Emitter or an ImageParticle live anywhere in
	// the tree, and the system draws them (see registerParticlePart). It
	// has no visual presence of its own.
	if isParticlePart(n.typeName) {
		return true
	}
	// QtMultimedia's non-visual half: the player, the capture session and
	// the device list (VideoOutput is the visual one). PdfDocument is the
	// same shape: a model its viewer item renders.
	if isMediaType(n.typeName) || n.typeName == "PdfDocument" {
		return true
	}
	return false
}

// scrollMember serves the read-only QML Flickable members a binding can
// ask a scroll state for: the content offsets and the visibleArea
// sub-object (position and size as 0..1 fractions of the content).
func scrollMember(s *ui.ScrollState, name string) (any, bool) {
	// Scroll offsets and layout sizes live outside the property system;
	// the state's token stands in for them (see scrollTok).
	scrollTok(s).Get()
	frac := func(pos, view, content float32) (float64, float64) {
		if content <= 0 {
			return 0, 1
		}
		return float64(pos / content), float64(min(view/content, 1))
	}
	yPos, hRatio := frac(s.Y, s.ViewH(), s.ContentH())
	xPos, wRatio := frac(s.X, s.ViewW(), s.ContentW())
	switch name {
	case "contentY":
		return float64(s.Y), true
	case "contentX":
		return float64(s.X), true
	case "contentHeight":
		return float64(s.ContentH()), true
	case "contentWidth":
		return float64(s.ContentW()), true
	case "visibleArea":
		return map[string]any{
			"yPosition": yPos, "heightRatio": hRatio,
			"xPosition": xPos, "widthRatio": wRatio,
		}, true
	case "atYBeginning":
		return s.Y <= 0, true
	case "atYEnd":
		return s.Y >= s.MaxY(), true
	case "atXBeginning":
		return s.X <= 0, true
	case "atXEnd":
		return s.X >= s.MaxX(), true
	case "originX", "originY":
		return float64(0), true
	// A flick's motion is not modeled (scrolling is immediate here), so the
	// "is it moving" family reads false rather than failing the binding.
	case "moving", "dragging", "flicking",
		"movingVertically", "movingHorizontally",
		"draggingVertically", "draggingHorizontally",
		"flickingVertically", "flickingHorizontally":
		return false, true
	}
	return nil, false
}

// objectRows reads the row array out of an object-backed model: an
// elemObject (a native backend's instance, or an id'd element) or a plain
// object with a `rows` or `table` property.
func objectRows(v any) ([]any, bool) {
	get := func(name string) (any, bool) {
		switch t := v.(type) {
		case *elemObject:
			return t.member(name)
		case map[string]any:
			x, ok := t[name]
			if r, isRef := x.(Ref); isRef {
				return r.get(), ok
			}
			return x, ok
		}
		return nil, false
	}
	for _, name := range []string{"rows", "table"} {
		if x, ok := get(name); ok {
			if arr, isArr := x.([]any); isArr {
				return arr, true
			}
		}
	}
	return nil, false
}
