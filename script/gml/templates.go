package gml

// Minimal QtQuick.Templates: enough of the T.* control contract for
// Qt style modules to render. A style file's root (`T.Button { ... }`)
// composes its background and contentItem delegates inside an
// anchor-layout container, with the control's state (text, flat,
// enabled, palette, ...) exposed through the file's id object so the
// delegates' bindings (root.text, root.palette.button) resolve.
// Interaction state (pressed/hovered) is static false in v1 — normal
// visual state renders; hover/press tinting needs live element state.

import (
	"strconv"
	"strings"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// templatesAlias returns the namespace alias a file binds to
// QtQuick.Templates ("" when the module isn't imported).
func templatesAlias(root *node) string {
	for a, m := range root.nsAliases {
		if m == "QtQuick.Templates" {
			return a
		}
	}
	return ""
}

// isTemplateType reports whether n is a namespaced Templates control
// within a file whose root declared the alias, returning the bare kind.
func isTemplateType(n *node, fileRoot *node) (string, bool) {
	alias := templatesAlias(fileRoot)
	if alias == "" || !strings.HasPrefix(n.typeName, alias+".") {
		return "", false
	}
	return strings.TrimPrefix(n.typeName, alias+"."), true
}

// templateDefaults are the control-contract properties every T.* type
// exposes to its delegates.
func templateDefaults() map[string]any {
	return map[string]any{
		"text": "", "enabled": true, "flat": false, "highlighted": false,
		"checked": false, "checkable": false, "pressed": false,
		"hovered": false, "down": false, "hoverEnabled": true,
		"value": 0.0, "from": 0.0, "to": 1.0, "position": 0.0,
		"visualPosition": 0.0, "checkState": 0.0, "mirrored": false,
		"color":         "#1a1a1a",
		"indeterminate": false, "orientation": 1.0, "stepSize": 0.0,
		"activeFocus": false, "focus": false, "placeholderText": "",
		// `root.popup.visible` (a ComboBox style's background reads it) and
		// `root.font` (a contentItem copies the control's whole font).
		"popup": map[string]any{"visible": false},
		"font": map[string]any{
			"family": "", "pixelSize": 0.0, "pointSize": 0.0,
			"bold": false, "italic": false,
		},
		"displayText": "", "currentText": "", "editText": "", "currentIndex": 0.0,
		"count": 0.0, "textRole": "", "valueRole": "", "editable": false,
		"currentValue": nil,
		// RangeSlider's two handles: each is a sub-object with its own
		// value/position (the style and the page both read them).
		"first": map[string]any{
			"value": 0.0, "position": 0.0, "visualPosition": 0.0,
			"pressed": false, "hovered": false,
		},
		"second": map[string]any{
			"value": 1.0, "position": 1.0, "visualPosition": 1.0,
			"pressed": false, "hovered": false,
		},
		// SpinBox's two indicator sub-objects, whose `pressed` the style
		// colours the +/- buttons by, and the locale its contentItem hands
		// to textFromValue. (`down` above is the button-pressed flag every
		// control has; SpinBox's stepper is `down.indicator`, so the
		// sub-object has to carry both meanings.)
		"up": map[string]any{
			"pressed": false, "hovered": false, "indicator": nil,
		},
		"locale": map[string]any{"name": "C"}, "validator": nil,
		"wrap": false, "inputMethodHints": 0.0,
		// Geometry a style combines into its implicit size / content rect.
		// The real numbers are filled in per instance (see seedTemplateScope
		// and buildTemplateControl); these keep the expressions evaluating.
		"leftInset": 0.0, "rightInset": 0.0, "topInset": 0.0, "bottomInset": 0.0,
		"leftPadding": 0.0, "rightPadding": 0.0, "topPadding": 0.0,
		"bottomPadding": 0.0, "padding": 0.0, "spacing": 0.0,
		"availableWidth": 0.0, "availableHeight": 0.0,
		"implicitBackgroundWidth": 0.0, "implicitBackgroundHeight": 0.0,
		"implicitContentWidth": 0.0, "implicitContentHeight": 0.0,
		"visibleItemCount": 3.0, "model": nil,
		"icon": map[string]any{
			"color": "#1a1a1a", "name": "", "source": "",
			"width": 0.0, "height": 0.0, "cache": true,
		},
		// The palette holds COLOURS, not strings: a style reads
		// `Qt.rgba(root.palette.button.r, ..., 0.2)` for its hover wash,
		// and a plain string has no .r (see colorVal).
		"palette": map[string]any{
			"button": colorVal("#e0e0e0"), "buttonText": colorVal("#1a1a1a"),
			"window": colorVal("#f0f0f0"), "windowText": colorVal("#1a1a1a"),
			"base": colorVal("#ffffff"), "text": colorVal("#1a1a1a"),
			"highlight": colorVal("#308cc6"), "highlightedText": colorVal("#ffffff"),
		},
	}
}

// seedTemplateScope pre-binds the control contract into the file's id
// object BEFORE property declaration, so declared props whose defaults
// read control state (readonly property color __bg: root.enabled ? ...)
// evaluate correctly. Control attributes (text:, flat:, palette.button:)
// override the defaults.
//
// The id is SHADOWED for the instantiation and restored by the returned
// func: QML ids are component-scoped, and style files conventionally use
// `id: root` — which pages also use, so binding through the taken-check
// (or merging into doc.elemObjs) made every styled control inside an
// `id: root` page resolve the PAGE's object and lose the contract.
func seedTemplateScope(inst *node, env *Env, doc *docState, stateKey string, outer *elemObject) func() {
	id := nodeID(inst)
	if id == "" {
		return func() {}
	}
	// The scope object carries the control's previous-frame element, so a
	// delegate sizing itself off root.width / root.height sees real bounds
	// (zero on the first frame, settled by the geometry signature).
	// doc/key give the object a durable identity across builds, so the
	// plain contract values it exposes carry tokens (see elemObject.setProp)
	// instead of poisoning — or, worse, silently satisfying — every
	// delegate that reads them.
	// The object is KEPT across builds (doc.tplObjs): the ~70 contract
	// values it carries are the same every frame, and rebuilding them —
	// a fresh defaults map, four nested maps, seventy stamped writes — was
	// the largest single cost of an animated page's frame. A kept object
	// takes only the writes whose value changed.
	obj := doc.tplObjs[stateKey]
	fresh := obj == nil
	if fresh {
		obj = &elemObject{props: map[string]any{}, doc: doc, key: doc.intern(stateKey)}
		if doc.tplObjs == nil {
			doc.tplObjs = map[string]*elemObject{}
		}
		doc.tplObjs[stateKey] = obj
	}
	obj.el = doc.prevElems.get(doc.intern(stateKey))
	idk := doc.intern(id)
	prevObj, hadObj := doc.elemObjs.get2(idk)
	prevVar, hadVar := env.vars[id]
	// What `root` (or whatever the style calls itself) meant OUTSIDE this
	// instantiation: a delegate the use site handed in reads its own page's
	// ids, and pages use `id: root` as often as style files do.
	doc.outerID = outerBinding{id: id, val: prevVar, had: hadVar, env: env}
	restore := func() {
		if hadObj {
			doc.elemObjs.set(idk, prevObj)
		} else {
			doc.elemObjs.del(idk)
		}
		if hadVar {
			env.vars[id] = prevVar
		} else {
			delete(env.vars, id)
		}
	}
	doc.elemObjs.set(idk, obj)
	// A kept object's props ARE the defaults (plus everything since); the
	// nested contract maps are its own and are updated in place below.
	defaults := obj.props
	if fresh {
		defaults = templateDefaults()
	}
	if strings.HasSuffix(inst.typeName, ".SpinBox") {
		// A SpinBox's `down` is the lower stepper sub-object, not a
		// button's pressed flag: its style reads `root.down.pressed`.
		defaults["down"] = map[string]any{
			"pressed": false, "hovered": false, "indicator": nil,
		}
		// ... and its contentItem displays textFromValue(value, locale).
		if obj.methods == nil {
			obj.methods = map[string]any{}
		}
		obj.methods["textFromValue"] = jsFunc(func(a []any) (any, error) {
			if len(a) == 0 {
				return "", nil
			}
			f, err := toFloat64(a[0])
			if err != nil {
				return str(a[0]), nil
			}
			return strconv.FormatFloat(f, 'f', -1, 64), nil
		})
		obj.methods["valueFromText"] = jsFunc(func(a []any) (any, error) {
			if len(a) == 0 {
				return float64(0), nil
			}
			f, err := strconv.ParseFloat(strings.TrimSpace(str(a[0])), 64)
			if err != nil {
				return float64(0), nil
			}
			return f, nil
		})
	}
	pal := defaults["palette"].(map[string]any)
	icon := defaults["icon"].(map[string]any)
	font := defaults["font"].(map[string]any)
	// Evaluate the instance's overrides BEFORE installing defaults: nested
	// controls (a style Button's contentItem Label) reference the OUTER
	// control's state in their attrs (text: root.text) — those must read
	// the current scope, not this instance's fresh defaults.
	overrides := map[string]any{}
	// State the control owns itself, created BEFORE its attributes are
	// evaluated: a use-site attribute may read it. `highlighted: checked`
	// on a checkable Button is the QML idiom for "look active while I am
	// checked" — item scope, where a bare name resolves against the item's
	// own properties first — and with nothing bound for `checked` there
	// every such button drew unhighlighted (the particles page's three
	// affector buttons all looked selected).
	attrEnv := templateStateScope(inst, env, doc, stateKey, outer, overrides)
	for _, a := range inst.attrs {
		switch {
		case a.name == "id" || a.expr == nil:
		case a.name == "checked" || a.name == "value" || a.name == "currentIndex":
			// Already resolved into the state above.
		case strings.HasPrefix(a.name, "palette."):
			if v, err := a.expr.eval(attrEnv); err == nil {
				// A palette entry stays a COLOUR whatever it was written as.
				if s, ok := plainColor(v).(string); ok {
					v = colorVal(s)
				}
				pal[strings.TrimPrefix(a.name, "palette.")] = v
			}
		case strings.HasPrefix(a.name, "first."), strings.HasPrefix(a.name, "second."):
			// `first.value: 0.2` — the handle sub-objects.
			part := defaults[a.name[:strings.IndexByte(a.name, '.')]].(map[string]any)
			if v, err := a.expr.eval(attrEnv); err == nil {
				part[a.name[strings.IndexByte(a.name, '.')+1:]] = v
			}
		case strings.HasPrefix(a.name, "font."):
			if v, err := a.expr.eval(attrEnv); err == nil {
				font[strings.TrimPrefix(a.name, "font.")] = v
			}
		case strings.HasPrefix(a.name, "icon."):
			if v, err := a.expr.eval(attrEnv); err == nil {
				icon[strings.TrimPrefix(a.name, "icon.")] = v
			}
		default:
			if _, isCtl := defaults[a.name]; !isCtl {
				continue
			}
			// A property-bound contract value keeps its Ref, so the
			// control can WRITE it back (a styled CheckBox toggling
			// `checked: someProp`); elemObject.member derefs on read.
			if r, err := resolveRef(a.expr, attrEnv); err == nil {
				overrides[a.name] = r
				continue
			}
			if v, err := a.expr.eval(attrEnv); err == nil {
				overrides[a.name] = v
			}
		}
	}
	if fresh {
		for k, v := range defaults {
			obj.setProp(k, v)
		}
	}
	// The implicit sizes a Control derives from its parts, which its style
	// then combines (`implicitWidth: Math.max(implicitBackgroundWidth +
	// leftInset + rightInset, implicitContentWidth + leftPadding + ...)`).
	// Without them a Tumbler's style computes 0 and falls back to a
	// button-sized box.
	for name, delegate := range map[string]string{"Background": "background", "Content": "contentItem"} {
		dw, dh := delegateImplicitSize(inst, delegate, env)
		obj.setProp("implicit"+name+"Width", float64(dw))
		obj.setProp("implicit"+name+"Height", float64(dh))
	}
	for k, v := range overrides {
		obj.setProp(k, v)
	}
	// A text control's live editing state belongs to the USE SITE, not to
	// this style instance: the page reads `nameField.text` through the
	// object its own id names. Adopt it, the way the contract values above
	// adopt the page's refs — with two states the user typed into the
	// style's and every binding read the page's, so a summary line bound
	// to the field stayed empty however much was typed.
	if outer != nil && outer.input != nil {
		obj.input = outer.input
	}
	// Same for the interaction state every Control carries: the page's
	// bindings read `pressBtn.down` through the object its own id names, so
	// the styled control has to move THAT ref rather than one of its own.
	// Forked, a held button never reported itself pressed (its label stayed
	// "Press Me" where Qt's read "Pressed!").
	if outer != nil {
		for _, name := range []string{"pressed", "down", "hovered", "containsMouse"} {
			r, ok := outer.props[name].(Ref)
			if !ok {
				continue
			}
			// A SpinBox's `down` is its lower STEPPER (a sub-object the
			// style reads as `down.indicator`), not a pressed flag: leave
			// any sub-object alone.
			if _, sub := obj.props[name].(map[string]any); sub {
				continue
			}
			obj.setProp(name, r)
		}
	}
	// QML keeps `checked` and `checkState` in step — setting either moves
	// the other (Qt.Unchecked 0, PartiallyChecked 1, Checked 2). It matters
	// for the tri-state parent idiom: a "Select All" box states only
	// checkState, computed from its children, while the style draws its
	// tick from `checked || checkState === Qt.PartiallyChecked`. Unlinked,
	// a parent whose children are ALL checked rendered empty.
	linkCheckState(obj, hasAttr(inst, "checkState"))
	// Paddings and insets the style states plainly (`leftPadding: 40`).
	for _, a := range inst.attrs {
		switch a.name {
		case "padding", "leftPadding", "rightPadding", "topPadding", "bottomPadding",
			"leftInset", "rightInset", "topInset", "bottomInset", "spacing":
			if f, err := attrFloat(a, env); err == nil {
				obj.setProp(a.name, float64(f))
				if a.name == "padding" {
					for _, side := range []string{"leftPadding", "rightPadding", "topPadding", "bottomPadding"} {
						obj.setProp(side, float64(f))
					}
				}
			}
		}
	}
	// A RangeSlider's two handles carry their own live value, the way the
	// control's own `value` does — the style reads their positions and a
	// drag writes them back.
	if strings.HasSuffix(inst.typeName, ".RangeSlider") {
		for _, side := range []string{"first", "second"} {
			part, _ := obj.props[side].(map[string]any)
			if part == nil {
				continue
			}
			f, _ := toFloat32(part["value"])
			if outer != nil {
				if sub, ok := outer.props[side].(map[string]any); ok {
					if r, ok := sub["value"].(Ref); ok {
						part["value"] = r
						continue
					}
				}
			}
			part["value"] = doc.floatProp(stateKey+"|"+side+".value", f)
		}
		// The page reads and writes the handles through its own id
		// (`slider.first.value`, `onMoved: … first.value …`), so give the
		// use site the very same sub-objects.
		if outer != nil {
			for _, side := range []string{"first", "second"} {
				if part, ok := obj.props[side].(map[string]any); ok {
					outer.props[side] = part
				}
			}
		}
	}
	// position / visualPosition must exist BEFORE the style's own declared
	// properties are re-evaluated: a Dial style computes its progress angle
	// from position in a `readonly property`, and with position still at its
	// default that angle equals the arc's start — the progress arc collapses
	// to a point.
	syncPositions(obj)
	// The control's content rect, from what the style states about its own
	// parts: a declared property may compute from it before anything is
	// laid out (`__delegateHeight: availableHeight / visibleItemCount`).
	// buildTemplateControl refines these once the real size is known.
	availW := implicitSide(obj, "Width") - numProp(obj, "leftPadding") - numProp(obj, "rightPadding")
	availH := implicitSide(obj, "Height") - numProp(obj, "topPadding") - numProp(obj, "bottomPadding")
	obj.setProp("availableWidth", availW)
	obj.setProp("availableHeight", availH)
	env.vars[id] = obj
	popRoot := doc.pushBinds(map[string]any{id: obj}, true)
	// A style file reads the control's contract by BARE name inside the
	// control's own bindings, as QML's item scope does — and that scope has
	// to be in place for the declaration pass, since a `readonly property`
	// default may compute from it.
	restoreScope := shadowControlScope(obj, env, doc)
	return func() {
		restoreScope()
		popRoot()
		restore()
	}
}

// implicitSide is the control's implicit width or height: the larger of its
// background's and its content's.
func implicitSide(obj *elemObject, side string) float64 {
	return max(numProp(obj, "implicitBackground"+side), numProp(obj, "implicitContent"+side))
}

// numProp reads a numeric contract value off the scope (0 when absent).
func numProp(obj *elemObject, name string) float64 {
	v, ok := obj.props[name]
	if !ok {
		return 0
	}
	if r, isRef := v.(Ref); isRef {
		v = r.get()
	}
	f, err := toFloat64(v)
	if err != nil {
		return 0
	}
	return f
}

// delegateImplicitSize reads a delegate's own implicitWidth/implicitHeight
// (a style states them as plain numbers), for the control's implicit size.
func delegateImplicitSize(inst *node, attrName string, env *Env) (w, h float32) {
	for _, a := range inst.attrs {
		if a.name != attrName || a.elem == nil {
			continue
		}
		w = attrFloatOr(a.elem, "implicitWidth", env, 0)
		h = attrFloatOr(a.elem, "implicitHeight", env, 0)
	}
	return w, h
}

// outerBinding is the id binding a style instantiation shadowed, kept so a
// use-site delegate can be built in the scope it was written in.
type outerBinding struct {
	id  string
	val any
	had bool
	env *Env
}

// buildUseSite runs fn with the shadowed id restored to what it meant at
// the use site — for building a delegate the page handed to a styled
// control. A no-op when nothing is shadowed.
func (doc *docState) buildUseSite(fn func()) {
	b := doc.outerID
	if b.id == "" || b.env == nil {
		fn()
		return
	}
	cur, hadCur := b.env.vars[b.id]
	if b.had {
		b.env.vars[b.id] = b.val
	} else {
		delete(b.env.vars, b.id)
	}
	fn()
	if hadCur {
		b.env.vars[b.id] = cur
	} else {
		delete(b.env.vars, b.id)
	}
}

// buildTemplateControl renders a T.* control by composing its delegates.
func buildTemplateControl(kind string, n *node, env *Env, doc *docState) (ui.Element, error) {
	var background, contentItem, indicator, handle *node
	// SpinBox's stepper buttons and RangeSlider's two handles are delegates
	// of their own sub-objects.
	var upInd, downInd, firstHandle, secondHandle *node
	// A popup's own bands (T.Dialog's header and footer) and the scrim it
	// dims the page with (Overlay.modal).
	var header, footer, overlayModal, menuDelegate *node
	var padL, padR, padT, padB float32 // the control's content insets
	var w, h float32
	// wStated/hStated: the markup gave an explicit width/height, which
	// wins over any implicit one (see the attrs loop below).
	var wStated, hStated bool
	var clickStmts []stmt
	textVal := ""
	var scope *elemObject
	if id := nodeID(n); id != "" {
		obj := doc.elemObjs.get(doc.intern(id))
		if obj == nil {
			// The id binding is only in the registry while the control's
			// own build is in flight (seedTemplateScope restores it after).
			// A subtree of the control can be rebuilt on its own — a patch
			// redoing one slot — and then it is not, so fall back to the
			// object's durable home. Without this the rebuilt part saw no
			// control at all and made its own state: a styled Tumbler
			// spun a private currentIndex the page never heard about, and
			// took one drag and then no more.
			obj = doc.tplObjs[doc.keyStr(doc.keyID(n))]
		}
		if obj != nil {
			scope = obj
			if v, ok := obj.member("text"); ok {
				textVal = str(v)
			}
		}
	}
	// Live hovered/pressed state (a real T.Control always has its own,
	// whether or not a style delegate reads it): install Refs into the
	// control's scope BEFORE the delegates build, so background/contentItem
	// bindings (root.pressed ? ... : ...) resolve them each frame. State lags
	// one frame like every other rebuilt-per-frame binding here.
	// The control's OWN attributes read its contract by bare name, the way
	// QML's item scope does (`implicitWidth: Math.max(implicitBackgroundWidth
	// + leftInset, ...)` in a style file) — the delegates get the same scope
	// further down.
	restoreSelf := shadowControlScope(scope, env, doc)
	defer restoreSelf()
	key := doc.key(n)
	// The refs the scope already carries are the USE SITE's (adopted in
	// seedTemplateScope); only a control whose id published none needs its
	// own.
	hoverRef := scopeRef(scope, "hovered", func() Ref { return doc.boolProp(key+".hovered", false) })
	pressRef := scopeRef(scope, "pressed", func() Ref { return doc.boolProp(key+".pressed", false) })
	doc.exposeIDProp(n, "hovered", hoverRef)
	doc.exposeIDProp(n, "pressed", pressRef)
	doc.exposeIDProp(n, "down", pressRef)
	for _, a := range n.attrs {
		switch a.name {
		case "background":
			background = a.elem
		case "contentItem":
			contentItem = a.elem
		case "indicator":
			indicator = a.elem
		case "handle":
			handle = a.elem
		case "header":
			header = a.elem
		case "footer":
			footer = a.elem
		case "Overlay.modal", "Overlay.modeless":
			overlayModal = a.elem
		case "delegate":
			menuDelegate = a.elem
		case "up.indicator":
			upInd = a.elem
		case "down.indicator":
			downInd = a.elem
		case "first.handle":
			firstHandle = a.elem
		case "second.handle":
			secondHandle = a.elem
		case "implicitWidth", "width":
			// The USE SITE's implicitWidth beats the style's: the attrs
			// arrive style-first, so the last one wins. (A page states
			// `implicitWidth: 34` on a transport button and Qt gives it
			// 34, where the style's own 140 would make it a pill.) An
			// explicit `width` still beats any implicit one, as in QML.
			if f, err := attrFloat(a, env); err == nil {
				if a.name == "width" {
					w, wStated = f, true
				} else if !wStated {
					w = f
				}
			}
		case "implicitHeight", "height":
			if f, err := attrFloat(a, env); err == nil {
				if a.name == "height" {
					h, hStated = f, true
				} else if !hStated {
					h = f
				}
			}
		case "padding", "leftPadding", "rightPadding", "topPadding", "bottomPadding":
			// The control's content rect is inset by these (a spin box
			// keeps its value clear of the two steppers).
			if f, err := attrFloat(a, env); err == nil {
				switch a.name {
				case "padding":
					padL, padR, padT, padB = f, f, f, f
				case "leftPadding":
					padL = f
				case "rightPadding":
					padR = f
				case "topPadding":
					padT = f
				case "bottomPadding":
					padB = f
				}
			}
		case "onClicked", "onToggled", "onPressed",
			// A MenuItem's own signal is onTriggered, and an Action's too.
			"onTriggered":
			clickStmts = append(clickStmts, a.stmt...)
		default:
			// The control contract, palette.*, font.*, id, states, and
			// anything else style files set: tolerated (state was seeded).
		}
	}
	if w == 0 {
		w = 120
	}
	if h == 0 {
		h = 36
	}
	// The handler runner is captured HERE, before the control shadows
	// `parent` for its own delegates: the page wrote `onClicked:
	// parent.parent.originMode = 1` against ITS surroundings, not the
	// control's.
	runHandler := doc.handlerRunner(env)

	// T.Label: no delegates — render the text directly.
	if kind == "Label" || kind == "ToolTip" {
		return buildTemplateLabel(n, env, doc)
	}
	// The control's content rect, which a style computes its own geometry
	// from (`readonly property real __delegateHeight: availableHeight /
	// visibleItemCount`).
	if scope != nil {
		scope.setProp("availableWidth", float64(w-padL-padR))
		scope.setProp("availableHeight", float64(h-padT-padB))
	}

	// A Slider/ProgressBar delegate positions itself from position /
	// visualPosition: derive them from the live value before the delegates
	// build (QML keeps them in sync with value/from/to).
	syncPositions(scope)

	// The delegates are attribute VALUES, so no earlier pass walked them:
	// register their ids (and hover/press state) now, or a cross-delegate
	// reference (`anchors.left: indicatorRect.right`) has nothing to
	// resolve. Also expose the control itself as their `parent`.
	for _, delegate := range []*node{background, indicator, handle, contentItem, upInd, downInd,
		firstHandle, secondHandle} {
		if delegate == nil {
			continue
		}
		// declareRowScope (not declareProps): delegate ids and state must be
		// keyed per instantiation, matching the element registry that
		// geometry reads (`width: background.width / 2`).
		restore := declareRowScope(delegate, env, doc, nil, "", -1)
		defer restore()
	}
	// The control also exposes its delegates as properties — a style reads
	// `root.background.width` to place its handle — so give each slot an
	// object over its node, with last frame's element for the geometry.
	if scope != nil && scope.props != nil {
		for name, delegate := range map[string]*node{
			"background": background, "indicator": indicator,
			"handle": handle, "contentItem": contentItem,
		} {
			if delegate == nil {
				continue
			}
			key := doc.key(delegate)
			// x/y read relative to the control, as QML's do.
			var org ui.Rect
			if ce, ok := doc.prevElems.get2(doc.keyID(n)); ok {
				org = ui.Bounds(ce)
			}
			scope.props[name] = &elemObject{
				props: map[string]any{}, node: delegate, nodeEnv: env,
				doc: doc, key: doc.intern(key), el: doc.prevElems.get(doc.intern(key)),
				orgX: org.X, orgY: org.Y,
			}
		}
	}
	// A delegate resolves the control's own properties by BARE name too
	// (`source: icon.source`, `text: text`): QML's scope chain puts the
	// control between the delegate and the file. Shadow them for the
	// delegate builds only.
	restoreScope := shadowControlScope(scope, env, doc)
	defer restoreScope()
	prevParent, hadParent := env.vars["parent"]
	pobj := parentObject(n, env, doc)
	// The control's size is known here (implicitWidth/Height), so a
	// delegate positioning itself off `parent.width` lands correctly on the
	// very first frame instead of settling a frame later.
	pobj.natW, pobj.natH = w, h
	// The item the control sits in stays reachable (`parent.parent`, and a
	// popup's origin — see popupOrigin).
	if po, ok := prevParent.(*elemObject); ok && hadParent {
		pobj.parentObj = po
	}
	env.vars["parent"] = pobj
	defer func() {
		if hadParent {
			env.vars["parent"] = prevParent
		} else {
			delete(env.vars, "parent")
		}
	}()

	// A styled ComboBox shows its current item through the contentItem and
	// opens a popup list of the model on click — the interaction our
	// builtin combo does by cycling.
	var comboPopup ui.Element
	var comboOpen Ref
	if kind == "ComboBox" && scope != nil {
		items, textRole := comboItems(n, env)
		idxRef, _ := scope.props["currentIndex"].(Ref)
		cur := 0
		if idxRef != nil {
			if f, err := toFloat32(idxRef.get()); err == nil {
				cur = int(f)
			}
		}
		if cur < 0 {
			cur = 0
		}
		if cur >= len(items) && len(items) > 0 {
			cur = len(items) - 1
		}
		display := ""
		if cur < len(items) {
			display = comboItemText(items[cur], textRole)
		}
		scope.setProp("displayText", display)
		scope.setProp("currentText", display)
		// An editable combo's field text: the current item, since the field
		// is not edited in place here.
		scope.setProp("editText", display)
		scope.setProp("count", float64(len(items)))
		// currentValue: the valueRole field of the current row (QML's
		// model-value counterpart to currentText), or the row itself.
		if cur < len(items) {
			value := items[cur]
			if role := str(scope.props["valueRole"]); role != "" {
				if row, ok := items[cur].(map[string]any); ok {
					if v, ok := row[role]; ok {
						value = v
					}
				}
			}
			scope.setProp("currentValue", value)
		}
		openRef := doc.boolProp(doc.key(n)+"|popup", false)
		// `root.popup.visible` — the style's background reads it.
		scope.setProp("popup", map[string]any{"visible": openRef.get()})
		if isOpen, _ := toBool(openRef.get()); isOpen && len(items) > 0 {
			comboPopup = comboPopupList(n, items, textRole, cur, w, idxRef, openRef, env, doc)
		}
		comboOpen = openRef
	}

	// A disabled control does not respond to anything: QML gives it no
	// input at all (and blocks what is behind it). Read it BEFORE the
	// interactive parts are attached, so a disabled slider keeps its value
	// however hard it is dragged.
	enabled := true
	if scope != nil {
		if v, ok := scope.member("enabled"); ok {
			enabled, _ = toBool(v)
		}
	}

	// A styled POPUP is not a box in the flow: it floats, it owns its open
	// state, and its style file supplies the panel around it (background,
	// header, footer). Everything below composes an in-flow control, so a
	// popup takes its own path.
	if isPopupKind(kind) {
		return buildTemplatePopup(kind, n, env, doc, scope, templatePopupParts{
			background: background, contentItem: contentItem, header: header,
			footer: footer, overlayModal: overlayModal, delegate: menuDelegate,
			w: w, h: h, padL: padL, padR: padR, padT: padT, padB: padB,
		})
	}

	// A styled text field draws its frame through the background delegate,
	// but the text, caret, selection and key handling are the input
	// element's: create it first so the delegate can read activeFocus, then
	// overlay it transparently over the composed frame.
	var input ui.Element
	if isTextInput(kind) && scope != nil {
		// The page reads its field through the state the DECLARE pass put
		// on the id (`nameField.text`, and the summary Label bound to it),
		// so take THAT state rather than starting a second copy: with two,
		// the user typed into one and every binding read the other, which
		// left the page's summary permanently empty.
		st, fresh := scope.input, false
		if st == nil {
			key := doc.key(n) + "|input"
			fresh = doc.inputs[key] == nil
			st = doc.inputFor(key, env)
		}
		if fresh {
			// Seed once from the markup's `text:` (QML's initial value);
			// after that the field owns its content.
			if v, ok := scope.member("text"); ok && str(v) != "" {
				st.SetText(str(v))
			}
		}
		// The contract values a page reads off the field, from the live
		// state: the control's scope props are consulted BEFORE its input,
		// so leaving the seeded empty `text` in place shadowed everything
		// the user typed — a summary Label bound to the field never moved.
		scope.setProp("text", st.Text.Get())
		scope.setProp("displayText", st.Text.Get())
		scope.setProp("length", float64(len([]rune(st.Text.Get()))))
		scope.setProp("activeFocus", st.HasFocus())
		scope.setProp("focus", st.HasFocus())
		in := ui.Input(st, w).Bg(ui.RGBA{}).Border(0, ui.RGBA{})
		in.ReadOnly(!enabled || boolAttrOf(n, "readOnly", env))
		// What the field draws instead of its value, how long it may get,
		// and what it accepts at all — all stated on the control, not on
		// the style's contentItem.
		if echo, hasEcho := echoModeOf(n, env); hasEcho {
			in.Echo(echo)
		}
		st.MaxLen(intAttrOf(n, "maximumLength", env))
		st.Accept(validatorOf(n, env))
		if v, ok := scope.member("color"); ok {
			if c, err := parseColor(str(v)); err == nil {
				in.TextColor(c)
			}
		}
		if v, ok := scope.member("placeholderText"); ok && str(v) != "" {
			in.Placeholder(str(v), ui.RGB(0xb0b0b0))
		}
		input = in
	}

	// Compose: a fixed-size anchor container with the delegates stacked in
	// QML's own order — background, then the indicator/handle, then the
	// content on top. Each delegate carries its own anchors / x / y.
	// A control paints outside its own box when its content asks for more
	// room than the style's implicit size (a Switch whose label is longer
	// than the 170px the style states): Qt clips at the card, not at the
	// control.
	box := ui.Div().Size(w, h).AnchorMode()
	// The stepper buttons come after the background and before the content,
	// and each carries a click that steps the value (a SpinBox is its
	// indicators — without the wiring the style would draw two dead
	// rectangles).
	step := spinStepper(scope)
	for _, delegate := range []*node{background, indicator, handle, upInd, downInd,
		firstHandle, secondHandle, contentItem} {
		if delegate == nil {
			continue
		}
		if kind == "Tumbler" && delegate == contentItem {
			// A TumblerView is a path-laid view we do not model; the wheel
			// below replaces it.
			continue
		}
		// Each delegate is placed as a SLOT (see docState.slot): the whole
		// placement — its geometry shadow, its anchors, its wrapper — is
		// one unit the control can redo alone when only that delegate's
		// subtree changed, keeping the control's own scope work.
		delegate := delegate
		place := func() (ui.Element, error) {
			// The delegate's own x/y/anchors bindings read the DELEGATE's
			// geometry by bare name (`x: root.checked ? background.width - width
			// : 0` — `width` is the handle's, not the control's), so shadow it
			// here too: the control's build already put ITS geometry in scope.
			restoreGeom := shadowSelfGeometry(delegate, env, doc)
			anch, err := parseAnchors(delegate, env)
			if err != nil {
				// An unresolved anchor/position binding must NOT fall back to
				// filling the control: a handle placed by x/y would suddenly be
				// control-sized, and its children size off it.
				anch = ui.Anchors{}
				if !hasSizeAttrs(delegate) {
					anch = ui.Anchors{Fill: "parent"}
				}
			}
			// A delegate that positions itself nowhere fills the control, as
			// Qt resizes background/contentItem to the control's content rect.
			if anch == (ui.Anchors{}) && !hasSizeAttrs(delegate) {
				anch = ui.Anchors{Fill: "parent"}
			}
			el, err := build(stripAnchorAttrs(delegate), env, doc)
			restoreGeom()
			if err != nil {
				return nil, err
			}
			// A text delegate that fills the control carries its own padding
			// and vertical alignment (`leftPadding: 12; verticalAlignment:
			// Text.AlignVCenter`), which a text leaf can't apply to a rect it
			// was handed — wrap it so the padding and centering happen.
			if anch.Fill != "" && isTextDelegate(delegate) {
				left := attrFloatOr(delegate, "leftPadding", env, 0)
				right := attrFloatOr(delegate, "rightPadding", env, 0)
				wrap := ui.Div().Row().ItemsCenter().Children(el)
				if left > 0 || right > 0 {
					// Each side keeps its OWN padding: a ComboBox's content is
					// `leftPadding: 12; rightPadding: 30` (room for the arrow),
					// and taking the larger of the two for both indented the
					// text by 30 where Qt starts it at 12.
					wrap = ui.Div().Row().ItemsCenter().Padding(0, right, 0, left).Children(el)
				}
				// ... and its HORIZONTAL alignment, which the row otherwise
				// leaves at the leading edge: the corpus' specialized buttons
				// are `contentItem: Text { horizontalAlignment: AlignHCenter }`
				// filling the control, and every label sat against the pill's
				// left edge where Qt centres it.
				if centeredText(delegate, env) {
					wrap.JustifyCenter()
				} else if rightAlignedText(delegate, env) {
					wrap.Justify(ui.AlignEnd)
				}
				el = wrap
			}
			if delegate == contentItem {
				// The content sits in the control's content rect, and a
				// TextInput contentItem that asks to be centred is centred
				// (our input element draws left-aligned by default).
				if anch.Fill != "" {
					anch.LeftMargin, anch.RightMargin = padL, padR
					anch.TopMargin, anch.BottomMargin = padT, padB
				}
				if in, ok := el.(*ui.InputEl); ok && centeredText(delegate, env) {
					in.Center()
				}
			}
			if step != nil && (delegate == upInd || delegate == downInd) {
				dir := 1
				if delegate == downInd {
					dir = -1
				}
				if d, ok := el.(*ui.DivEl); ok {
					d.OnClick(func() { step(dir) })
				}
			}
			return ui.Anchored(el, nodeID(delegate), anch), nil
		}
		el, err := doc.slot(env, place)
		if err != nil {
			logf("gml: %v (delegate skipped)", err)
			continue
		}
		box.Children(el)
	}
	// A Tumbler's contentItem is a TumblerView: a wheel of delegates along a
	// vertical path, which is what our own tumbler already lays out. Build
	// that from the control's model/delegate/visibleItemCount and turn the
	// control's own currentIndex, so the page's binding sees the picks.
	if kind == "Tumbler" {
		wheel, err := buildTemplateWheel(n, env, doc, scope, w-padL-padR, h-padT-padB)
		if err != nil {
			logf("gml: %v (tumbler wheel skipped)", err)
		} else if wheel != nil {
			box.Children(ui.Anchored(wheel, "", ui.Anchors{
				Fill: "parent", LeftMargin: padL, RightMargin: padR,
				TopMargin: padT, BottomMargin: padB,
			}))
		}
	}
	// The style's own children (a Tumbler's selection band, re-parented onto
	// the background in QML) are laid out in the control's box: the
	// background fills it, so the band lands where the markup means.
	for _, c := range n.children {
		if isNonVisual(c) {
			doc.refreshNonVisual(c, env)
			continue
		}
		if isPopupKind(c.typeName) {
			// The Menu a ToolButton opens: its x/y are POPUP coordinates
			// (where it floats, in the control's frame), not a place in
			// the control's box — stripped as anchors they left every
			// `Menu { y: btn.height }` centred in the window.
			el, err := build(c, env, doc)
			if err != nil {
				logf("gml: %v (popup child skipped)", err)
				continue
			}
			box.Children(el)
			continue
		}
		c := c
		el, err := doc.slot(env, func() (ui.Element, error) {
			restoreGeom := shadowSelfGeometry(c, env, doc)
			anch, err := parseAnchors(c, env)
			restoreGeom()
			if err != nil {
				return nil, err
			}
			el, err := build(stripAnchorAttrs(c), env, doc)
			if err != nil {
				return nil, err
			}
			return ui.Anchored(el, nodeID(c), anch), nil
		})
		if err != nil {
			logf("gml: %v (style child skipped)", err)
			continue
		}
		box.Children(el)
	}
	if background == nil && contentItem == nil && indicator == nil && handle == nil &&
		textVal != "" && !isTextInput(kind) {
		// Style-less fallback: a plain labeled box. A text input never wants
		// it — its own input element carries the text, so the fallback would
		// draw the content twice under a box the markup asked not to have
		// (`background: null`).
		box = ui.Div().Size(w, h).Bg(ui.RGB(0xe0e0e0)).Rounded(4).
			JustifyCenter().ItemsCenter().Children(ui.Text(textVal, ui.RGB(0x1a1a1a)))
	}
	// A styled Slider draws through its groove/handle delegates but still
	// has to DRAG: overlay a bare slider (paints nothing, handles press and
	// drag) bound to the control's value.
	// A styled RangeSlider draws through its groove and two handle
	// delegates, and drags through a bare overlay bound to both values.
	if kind == "RangeSlider" && scope != nil && enabled {
		lo, hi := handleProp(scope, "first"), handleProp(scope, "second")
		if lo != nil && hi != nil {
			from, to := float32(0), float32(1)
			if v, ok := scope.member("from"); ok {
				from, _ = toFloat32(v)
			}
			if v, ok := scope.member("to"); ok {
				to, _ = toFloat32(v)
			}
			rs := ui.RangeSlider(lo, hi, w).Range(from, to).Bare()
			// stepSize/snapMode are the control's, stated on the page.
			rstep, _ := toFloat32(scopeNum(scope, "stepSize"))
			rs.Step(rstep, snapAlways(n, env))
			if numProp(scope, "orientation") == 2 {
				rs.Vertical()
			}
			box.Children(ui.Anchored(rs, "", ui.Anchors{Fill: "parent"}))
		}
	}
	// A Dial turns to the pointer's angle: a press anywhere on it sets the
	// value, and it keeps following while held (Qt's Dial). The style draws
	// the ring and the handle; this supplies the turning.
	if kind == "Dial" && scope != nil && enabled {
		if ref, ok := scope.props["value"].(Ref); ok {
			if pr, _, ok := propOf[float32](ref); ok {
				from, to := float32(0), float32(1)
				if v, ok := scope.member("from"); ok {
					from, _ = toFloat32(v)
				}
				if v, ok := scope.member("to"); ok {
					to, _ = toFloat32(v)
				}
				dl := ui.Dial(pr.p, from, to)
				dstep, _ := toFloat32(scopeNum(scope, "stepSize"))
				dl.Step(dstep, snapAlways(n, env))
				box.Children(ui.Anchored(dl, "", ui.Anchors{Fill: "parent"}))
			}
		}
	}
	if kind == "Slider" && scope != nil && enabled {
		if ref, ok := scope.props["value"].(Ref); ok {
			if pr, _, ok := propOf[float32](ref); ok {
				from, to := float32(0), float32(1)
				if v, ok := scope.member("from"); ok {
					from, _ = toFloat32(v)
				}
				if v, ok := scope.member("to"); ok {
					to, _ = toFloat32(v)
				}
				sl := ui.Slider(pr.p, w).Range(from, to).Bare()
				// stepSize/snapMode/orientation are the control's, stated
				// on the page — the style only draws the groove.
				step, _ := toFloat32(scopeNum(scope, "stepSize"))
				sl.Step(step, snapAlways(n, env))
				if o, _ := toFloat32(scopeNum(scope, "orientation")); int(o) == 2 {
					sl.Vertical() // Qt.Vertical: zero at the bottom
				}
				box.Children(ui.Anchored(sl, "", ui.Anchors{Fill: "parent"}))
			}
		}
	}

	if input != nil {
		box.Children(ui.Anchored(input, "", ui.Anchors{Fill: "parent"}))
	}
	if comboOpen != nil {
		open := comboOpen
		box.OnClick(func() {
			b, _ := toBool(open.get())
			_ = open.set(!b)
		})
		if comboPopup != nil {
			// The popup floats (measure 0, out of flow), so it can ride
			// along as a child of the control's box.
			box.Children(comboPopup)
		}
	}

	// A checkable control toggles its own state on click, then runs the
	// page's handler — the interaction a styled CheckBox/Switch needs.
	var toggle Ref
	if isCheckableKind(kind) && scope != nil {
		toggle, _ = scope.props["checked"].(Ref)
	}
	if !enabled {
		clickStmts, toggle = nil, nil
	}
	if len(clickStmts) > 0 || toggle != nil {
		stmts := clickStmts
		run := runHandler
		box.OnClick(func() {
			doc.noteTrigger(box)
			if toggle != nil {
				b, _ := toBool(toggle.get())
				_ = toggle.set(!b)
			}
			if len(stmts) > 0 {
				run(stmts, "onClicked")
			}
		})
	}
	box.OnHoverChanged(func(v bool) { _ = hoverRef.set(v) })
	box.OnPressChanged(func(v bool) { _ = pressRef.set(v) })
	return box, nil
}

// buildTemplateLabel renders T.Label. A styled Label is still a Text item —
// it wraps, aligns, elides and honours hard line breaks — so the ordinary
// Text builder does the work; the simple path below is the fallback for a
// style that decorates its label with attributes Text does not know.
func buildTemplateLabel(n *node, env *Env, doc *docState) (ui.Element, error) {
	if el, err := buildText(n, env, doc); err == nil {
		return el, nil
	}
	text := ""
	color := ui.RGB(0x1a1a1a)
	size := 0.0
	bold := false
	for _, a := range n.attrs {
		if a.expr == nil {
			continue
		}
		switch a.name {
		case "text":
			if v, err := a.expr.eval(env); err == nil {
				text = str(v)
			}
		case "color":
			if c, err := attrColor(a, env); err == nil {
				color = c
			}
		case "font.pixelSize", "font.pointSize":
			if f, err := attrFloat(a, env); err == nil {
				size = float64(f)
			}
		case "font.bold":
			if b, err := attrBool(a, env); err == nil {
				bold = b
			}
		}
	}
	t := ui.Text(text, color)
	if size > 0 {
		t.SizePx(size)
	}
	if bold {
		t.Bold()
	}
	return t, nil
}

// templateStateScope creates the live state a control owns (checked,
// value, currentIndex) and returns the scope its own attributes evaluate
// in: env plus those names, so `highlighted: checked` resolves the way
// QML's item scope does. The refs land in overrides, which the caller
// installs on the instance object.
func templateStateScope(inst *node, env *Env, doc *docState, stateKey string,
	outer *elemObject, overrides map[string]any) *Env {
	attrOf := func(name string) *attr {
		for i := range inst.attrs {
			if inst.attrs[i].name == name && inst.attrs[i].expr != nil {
				return &inst.attrs[i]
			}
		}
		return nil
	}
	scope := env
	bind := func(name string, v any) {
		if scope == env {
			scope = env.child()
			scope.derived = true // contract values, computed from the use site
		}
		scope.vars[name] = v
	}
	for name, kind := range map[string]string{
		"checked": "bool", "value": "real", "currentIndex": "int",
	} {
		// A property-bound contract value keeps its Ref, so the control can
		// WRITE it back (a styled CheckBox toggling `checked: someProp`).
		if a := attrOf(name); a != nil {
			if r, err := resolveRef(a.expr, env); err == nil {
				overrides[name] = r
				bind(name, r)
				continue
			}
		}
		// The page may already own this state: the use-site element got its
		// own ref at declare time, and its bindings and handlers have been
		// closing over that one since. Adopt it rather than starting a
		// second copy the two sides then disagree about.
		if outer != nil {
			if r, ok := outer.props[name].(Ref); ok {
				overrides[name] = r
				bind(name, r)
				continue
			}
		}
		// Otherwise the literal the markup gave seeds a fresh instance ref.
		var seed any
		if a := attrOf(name); a != nil {
			if v, err := a.expr.eval(env); err == nil {
				seed = v
			}
		}
		key := stateKey + "|" + name
		var ref Ref
		switch kind {
		case "bool":
			b, _ := toBool(seed)
			ref = doc.boolProp(key, b)
		case "int":
			f, _ := toFloat32(seed)
			ref = doc.intProp(key, int(f))
		default:
			f, _ := toFloat32(seed)
			ref = doc.floatProp(key, f)
		}
		overrides[name] = ref
		bind(name, ref)
	}
	return scope
}

// linkCheckState mirrors a checkable control's two views of one state. The
// stated side wins: a box that computes checkState owns it (and its
// `checked` follows), and any other box derives checkState from `checked`.
func linkCheckState(obj *elemObject, statesCheckState bool) {
	if obj == nil || obj.props == nil {
		return
	}
	if _, has := obj.props["checkState"]; !has {
		return
	}
	if statesCheckState {
		v, _ := obj.member("checkState")
		f, _ := toFloat64(v)
		obj.setProp("checked", f == 2) // Qt.Checked
		return
	}
	v, _ := obj.member("checked")
	if b, _ := toBool(v); b {
		obj.setProp("checkState", float64(2))
	} else {
		obj.setProp("checkState", float64(0))
	}
}

// isCheckableKind reports the T.* kinds whose click toggles `checked`.
func isCheckableKind(kind string) bool {
	switch kind {
	case "CheckBox", "Switch", "RadioButton", "CheckDelegate", "SwitchDelegate",
		"RadioDelegate", "ToolButton", "Button":
		return true
	}
	return false
}

// syncPositions keeps a control's derived position/visualPosition in step
// with its live value (QML: position = (value - from) / (to - from), and
// visualPosition mirrors it unless the control is mirrored). Delegates
// place their handle and fill from these.
func syncPositions(scope *elemObject) {
	if scope == nil || scope.props == nil {
		return
	}
	num := func(name string) float32 {
		v, ok := scope.member(name)
		if !ok {
			return 0
		}
		f, _ := toFloat32(v)
		return f
	}
	from, to := num("from"), num("to")
	pos := float32(0)
	if to != from {
		pos = (num("value") - from) / (to - from)
	}
	pos = min(max(pos, 0), 1)
	scope.setProp("position", float64(pos))
	// QML: visualPosition mirrors position for a horizontal control and
	// INVERTS it for a vertical one (a vertical slider's zero is at the
	// bottom), which is what the style's fill and handle offsets assume.
	vis := pos
	if num("orientation") == 2 { // Qt.Vertical
		vis = 1 - pos
	}
	scope.setProp("visualPosition", float64(vis))
	// A RangeSlider's handles each have their own pair, from their own
	// value on the same range.
	for _, side := range []string{"first", "second"} {
		part, ok := scope.props[side].(map[string]any)
		if !ok {
			continue
		}
		pv := part["value"]
		if r, ok := pv.(Ref); ok {
			pv = r.get() // the handle's live value, once it has one
		}
		v, _ := toFloat32(pv)
		p := float32(0)
		if to != from {
			p = (v - from) / (to - from)
		}
		p = min(max(p, 0), 1)
		part["position"] = float64(p)
		if num("orientation") == 2 {
			p = 1 - p
		}
		part["visualPosition"] = float64(p)
	}
}

// hasSizeAttrs reports whether a delegate sizes or positions itself
// explicitly (so it must not be stretched to fill the control).
func hasSizeAttrs(n *node) bool {
	for _, a := range n.attrs {
		switch a.name {
		// An IMPLICIT size is a hint for a delegate that has nothing else
		// to size it — Qt still resizes background/contentItem to the
		// control's content rect, so it must not stop the delegate from
		// filling. Only an explicit size or position does that.
		case "width", "height", "x", "y":
			return true
		}
	}
	return false
}

// derivedContract lists the contract names a style may read back without
// the read counting as mutable (see installTemplateProps / Env.derived).
var derivedContract = map[string]bool{
	"leftInset": true, "rightInset": true, "topInset": true, "bottomInset": true,
	"leftPadding": true, "rightPadding": true, "topPadding": true, "bottomPadding": true,
	"padding": true, "spacing": true, "availableWidth": true, "availableHeight": true,
	"implicitBackgroundWidth": true, "implicitBackgroundHeight": true,
	"implicitContentWidth": true, "implicitContentHeight": true,
	"palette": true, "font": true, "icon": true, "locale": true,
}

// shadowControlScope binds a control's contract properties under their bare
// names while its delegates build, restoring the previous bindings after.
func shadowControlScope(scope *elemObject, env *Env, doc *docState) func() {
	if scope == nil || scope.props == nil {
		return func() {}
	}
	undo := takeUndo(len(scope.props))
	popBinds := doc.pushBinds(scope.props, false)
	if env.derivedNames == nil {
		env.derivedNames = map[string]bool{}
	}
	for name, v := range scope.props {
		undo = append(undo, saveBinding(env, name))
		sv := &undo[len(undo)-1]
		env.vars[name] = v
		shadowDerived(env, sv)
		// The GEOMETRY of the contract (paddings, insets, implicit sizes)
		// is computed from the use site and may be read back without
		// poisoning the style's read-sets. The STATE placeholders (pressed,
		// hovered, checked...) are seeded as plain values and replaced by
		// live Refs once the control has built — a reader of the
		// placeholder must stay untracked or it never sees the Ref.
		if derivedContract[name] {
			markDerived(env, sv, nil)
		}
	}
	return func() {
		popBinds()
		restoreBindings(env, undo)
		putUndo(undo)
	}
}

// comboItems resolves a ComboBox's model rows (an array, a ListModel, or a
// plain count) and its textRole, from the use-site attributes.
func comboItems(n *node, env *Env) ([]any, string) {
	var items []any
	textRole := ""
	for _, a := range n.attrs {
		switch a.name {
		case "textRole":
			if v, err := a.expr.eval(env); err == nil {
				textRole = str(v)
			}
		case "model":
			rows, count, err := resolveModel(a, env)
			if err != nil {
				continue
			}
			if rows != nil {
				for _, r := range rows {
					items = append(items, r)
				}
				continue
			}
			for i := 0; i < count && i < 10000; i++ {
				items = append(items, float64(i))
			}
		}
	}
	return items, textRole
}

// comboItemText renders one model row as the combo's display string: the
// textRole, the conventional "text" role, or the value itself.
func comboItemText(item any, textRole string) string {
	if row, ok := item.(map[string]any); ok {
		if textRole != "" {
			if v, ok := row[textRole]; ok {
				return str(v)
			}
		}
		if v, ok := row["text"]; ok {
			return str(v)
		}
		if v, ok := row["modelData"]; ok {
			return str(v)
		}
		for _, v := range row {
			return str(v)
		}
		return ""
	}
	return str(item)
}

// comboPopupList builds the open ComboBox's item list as a floating panel
// under the control: the style's own delegate per row when it has one,
// otherwise a plain labeled row. Choosing a row sets currentIndex and
// closes; clicking outside just closes.
func comboPopupList(n *node, items []any, textRole string, cur int, w float32,
	idxRef, openRef Ref, env *Env, doc *docState) ui.Element {
	delegate := attachedElem(n, "delegate")
	panel := ui.Div().Col().W(w).Bg(ui.RGB(0x2a2d35)).Rounded(8).P(2)
	for i, item := range items {
		label := comboItemText(item, textRole)
		var row ui.Element
		if delegate != nil {
			// Per-row scope: the style's delegate reads modelData/index/
			// highlighted (and `model[textRole]` for object rows).
			// The items are model ROWS already ({"modelData": "Apple"} for a
			// string array, a role map for an object one), so the row's own
			// keys go into the scope as they are — wrapping them again put
			// the whole map in `modelData`, and every row read as
			// "map[modelData:Apple]".
			cell := map[string]any{"index": float64(i), "highlighted": i == cur}
			if row, ok := item.(map[string]any); ok {
				for k, v := range row {
					cell[k] = v
				}
			} else {
				cell["modelData"] = item
			}
			restore := enterRow(doc.key(n)+"|popup", delegate, env, doc, cell, i)
			el, err := build(delegate, env, doc)
			restore()
			if err == nil {
				row = el
			}
		}
		if row == nil {
			c := ui.RGB(0xe8e8f0)
			if i == cur {
				c = ui.RGB(0x00d1a9)
			}
			row = ui.Div().H(32).ItemsCenter().P(10).Children(ui.Text(label, c))
		}
		idx, ir, or := i, idxRef, openRef
		item := ui.Div().W(w - 4).Children(row)
		item.OnClick(func() {
			if ir != nil {
				_ = ir.set(idx)
			}
			_ = or.set(false)
		})
		panel.Children(item)
	}
	// Float it just under the control (previous frame's bounds).
	var b ui.Rect
	if pe, ok := doc.prevElems.get2(doc.keyID(n)); ok {
		b = ui.Bounds(pe)
	}
	ref := openRef
	return ui.PositionedModal(panel, b.X, b.Y+b.H+2, ui.RGBA{}, func() { _ = ref.set(false) })
}

// isTextDelegate reports a delegate that renders as a text leaf, which has
// no way to position itself inside a larger rect.
func isTextDelegate(n *node) bool {
	switch n.typeName {
	case "Text", "Label":
		return true
	}
	return false
}

// attrFloatOr reads a numeric attribute of a node, or def when it is absent
// or unevaluable.
func attrFloatOr(n *node, name string, env *Env, def float32) float32 {
	for _, a := range n.attrs {
		if a.name != name || a.expr == nil {
			continue
		}
		if f, err := attrFloat(a, env); err == nil {
			return f
		}
	}
	return def
}

// spinStepper returns the value-stepping callback a SpinBox's indicators
// need: value += dir * stepSize, clamped to [from, to]. Nil when the scope
// holds no writable value (nothing to step).
func spinStepper(scope *elemObject) func(dir int) {
	if scope == nil {
		return nil
	}
	ref, _ := scope.props["value"].(Ref)
	if ref == nil {
		return nil
	}
	num := func(name string, def float64) float64 {
		v, ok := scope.member(name)
		if !ok {
			return def
		}
		f, err := toFloat64(v)
		if err != nil {
			return def
		}
		return f
	}
	return func(dir int) {
		step := num("stepSize", 1)
		if step == 0 {
			step = 1
		}
		from, to := num("from", 0), num("to", 99)
		v := num("value", 0) + float64(dir)*step
		if v < from {
			v = from
		}
		if v > to {
			v = to
		}
		if err := ref.set(v); err != nil {
			logf("gml: SpinBox step: %v", err)
		}
	}
}

// centeredText reports whether a delegate asks for horizontally centred
// text (`horizontalAlignment: Qt.AlignHCenter` / Text.AlignHCenter).
// rightAlignedText reports `horizontalAlignment: Text.AlignRight`.
func rightAlignedText(n *node, env *Env) bool {
	for _, a := range n.attrs {
		if a.name != "horizontalAlignment" || a.expr == nil {
			continue
		}
		v, err := a.expr.eval(env)
		if err != nil {
			continue
		}
		if f, err := toFloat64(v); err == nil {
			return int(f)&0x0002 != 0 // Qt::AlignRight
		}
		return containsFold(str(v), "Right")
	}
	return false
}

func centeredText(n *node, env *Env) bool {
	for _, a := range n.attrs {
		if a.name != "horizontalAlignment" || a.expr == nil {
			continue
		}
		v, err := a.expr.eval(env)
		if err != nil {
			continue
		}
		if f, err := toFloat64(v); err == nil {
			return int(f)&0x0084 != 0 // Qt::AlignHCenter (0x0084)
		}
		return containsFold(str(v), "HCenter") || containsFold(str(v), "Center")
	}
	return false
}

// buildTemplateWheel builds a styled Tumbler's spinning wheel from the
// control's own model, delegate and visibleItemCount, turning the control's
// currentIndex ref. The style's TumblerView (a path-laid view we do not
// model) is replaced by it.
func buildTemplateWheel(n *node, env *Env, doc *docState, scope *elemObject, w, h float32) (ui.Element, error) {
	wheel := &node{typeName: "Tumbler", line: n.line}
	for _, a := range n.attrs {
		switch a.name {
		case "id", "model", "delegate", "visibleItemCount", "wrap", "spacing":
			wheel.attrs = append(wheel.attrs, a)
		}
	}
	if w > 0 && h > 0 {
		wheel.attrs = append(wheel.attrs,
			attr{name: "width", expr: &litExpr{float64(w)}, line: n.line},
			attr{name: "height", expr: &litExpr{float64(h)}, line: n.line})
	}
	var idx Ref
	if scope != nil {
		idx, _ = scope.props["currentIndex"].(Ref)
	}
	// A delegate the PAGE supplied reads the page's own ids; one from the
	// style file reads the style's. Build in whichever scope wrote it.
	useSite := false
	for _, a := range wheel.attrs {
		if a.name == "delegate" && a.fromUse {
			useSite = true
		}
	}
	var el ui.Element
	var err error
	run := func() { el, err = buildTumblerWheel(wheel, env, doc, idx) }
	if useSite {
		doc.buildUseSite(run)
	} else {
		run()
	}
	return el, err
}

// handleProp is the animatable property behind one of a RangeSlider's two
// handle values (nil when the sub-object holds no live ref).
func handleProp(scope *elemObject, side string) *core.Property[float32] {
	part, ok := scope.props[side].(map[string]any)
	if !ok {
		return nil
	}
	ref, ok := part["value"].(Ref)
	if !ok {
		return nil
	}
	return spinProp(ref)
}

// boolAttrOf reads a plain boolean attribute (false when absent).
func boolAttrOf(n *node, name string, env *Env) bool {
	for _, a := range n.attrs {
		if a.name == name && a.expr != nil {
			if b, err := attrBool(a, env); err == nil {
				return b
			}
		}
	}
	return false
}

// scopeRef returns the Ref a control's scope already holds for name, or the
// one make() creates when it holds none.
func scopeRef(scope *elemObject, name string, make func() Ref) Ref {
	if scope != nil && scope.props != nil {
		if r, ok := scope.props[name].(Ref); ok {
			return r
		}
	}
	return make()
}

// scopeNum reads a numeric contract value off a control's scope (0 when it
// has none).
func scopeNum(scope *elemObject, name string) float64 {
	if scope == nil {
		return 0
	}
	v, ok := scope.member(name)
	if !ok {
		return 0
	}
	f, _ := toFloat64(v)
	return f
}

// snapAlways reports whether a slider holds its value to stepSize while
// being dragged (QML snapMode: Slider.SnapAlways).
func snapAlways(n *node, env *Env) bool {
	for _, a := range n.attrs {
		if a.name == "snapMode" && a.expr != nil {
			if v, err := a.expr.eval(env); err == nil {
				return str(v) == "SnapAlways"
			}
		}
	}
	return false
}

// echoModeOf reads a text control's `echoMode` (QML TextInput.Password /
// NoEcho / PasswordEchoOnEdit) as the mask its field draws.
func echoModeOf(n *node, env *Env) (rune, bool) {
	for _, a := range n.attrs {
		if a.name != "echoMode" || a.expr == nil {
			continue
		}
		if v, err := a.expr.eval(env); err == nil {
			switch str(v) {
			case "Password", "PasswordEchoOnEdit":
				return '•', true
			case "NoEcho":
				return 0, true
			}
		}
	}
	return 0, false
}

// intAttrOf reads a plain integer attribute (0 when absent).
func intAttrOf(n *node, name string, env *Env) int {
	for _, a := range n.attrs {
		if a.name == name && a.expr != nil {
			if f, err := attrFloat(a, env); err == nil {
				return int(f)
			}
		}
	}
	return 0
}

// validatorOf reads a text control's `validator:` element as the predicate
// its field vets each keystroke with.
func validatorOf(n *node, env *Env) func(string) bool {
	for _, a := range n.attrs {
		if a.name == "validator" {
			return validatorFunc(a, env)
		}
	}
	return nil
}
