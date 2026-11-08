package gml

// Qt Quick Controls' Tumbler: a vertical wheel of items centred on
// currentIndex. QML's Tumbler is a spinnable Flickable; here the visible
// window is drawn directly — `visibleItemCount` items centred on the
// current one — and selection is by clicking an item or with the arrow
// keys, by dragging the wheel, or by flicking it — a release that is still
// moving coasts on and settles on an item.
//
// The delegate sees the same scope a view's delegate does (modelData,
// index, the instance's own ids and properties) plus the attached
// `Tumbler.displacement`: 0 for the centred item, growing by one per step
// away from it, negated above centre, which is what these delegates scale
// font size, colour and opacity by.

import (
	"fmt"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// buildTumbler renders a Tumbler element.
func buildTumbler(n *node, env *Env, doc *docState) (ui.Element, error) {
	return buildTumblerWheel(n, env, doc, nil)
}

// buildTumblerWheel is buildTumbler with the index ref supplied: a STYLED
// tumbler already owns one (the control's contract state, which the page
// binds to), and the wheel has to turn that same ref rather than a second
// copy of its own.
func buildTumblerWheel(n *node, env *Env, doc *docState, idx Ref) (ui.Element, error) {
	var modelAttr *attr
	var w, h, spacing float32
	visible := 3
	wrap := true // QML: true unless the model is shorter than the window
	idxRef := idx
	for i := range n.attrs {
		a := n.attrs[i]
		switch a.name {
		case "model":
			modelAttr = &n.attrs[i]
		case "delegate":
			// read by delegateChild
		case "currentIndex":
			if idx == nil {
				idxRef, _ = intBinding(doc, n, a, env)
			}
		case "visibleItemCount":
			if v, err := attrFloat(a, env); err == nil && v >= 1 {
				visible = int(v)
			}
		case "wrap":
			if b, err := attrBool(a, env); err == nil {
				wrap = b
			}
		case "width", "implicitWidth":
			if f, err := attrFloat(a, env); err == nil {
				w = f
			}
		case "height", "implicitHeight":
			if f, err := attrFloat(a, env); err == nil {
				h = f
			}
		case "spacing":
			if f, err := attrFloat(a, env); err == nil {
				spacing = f
			}
		case "id", "clip", "z", "opacity", "visible", "enabled", "focus",
			"activeFocusOnTab", "wheelEnabled", "background", "contentItem",
			"anchors.fill", "anchors.centerIn", "anchors.horizontalCenter",
			"anchors.verticalCenter", "anchors.margins", "objectName":
			// layout/chrome — accepted, applied below or ignored
		case "onCurrentIndexChanged", "onMoved":
			// index-change handlers: bindings re-evaluate every frame, so a
			// handler mirroring the index has nothing to do here.
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: Tumbler has no attribute %q", a.line, a.name)
		}
	}
	var rows []map[string]any
	count := 0
	if modelAttr != nil {
		var err error
		rows, count, err = resolveModel(*modelAttr, env)
		if err != nil {
			return nil, err
		}
	}
	if idxRef == nil {
		// A STYLED tumbler's wheel is handed the control's ref by
		// buildTemplateWheel — but only when that build re-established the
		// control's scope. It does not have to have: the wheel is a
		// subtree of its own and can be rebuilt alone, and then the scope
		// object is not in hand. Adopt the page's own ref by id first,
		// which is the same rule templateStateScope follows, or the wheel
		// turns a second, private index the page never sees — measured as
		// a styled Tumbler that took one drag and then ignored every
		// later one.
		if id := nodeID(n); id != "" {
			if obj := doc.elemObjs.get(doc.intern(id)); obj != nil {
				idxRef, _ = obj.props["currentIndex"].(Ref)
			}
		}
	}
	if idxRef == nil {
		// Same slot seedControlState creates at declare time, so a property
		// derived from this wheel (selectedDay: dayTumbler.currentIndex + 1)
		// and a setter writing it back share one ref with the wheel.
		idxRef = doc.floatProp(doc.key(n)+".currentIndex", 0)
	}
	doc.exposeIDProp(n, "currentIndex", idxRef)
	// The rest of the wheel state the QML contract exposes on its id (the
	// delegates read visibleItemCount to scale their depth fade).
	doc.exposeIDProp(n, "count", float64(count))
	doc.exposeIDProp(n, "visibleItemCount", float64(visible))
	doc.exposeIDProp(n, "wrap", wrap)
	doc.exposeIDProp(n, "moving", false)
	// The wheel itself — slots, clicks, keys, drag and fling — is
	// ui.Tumbler's; the rows are the delegate's, in each row's scope.
	if doc.tumblers == nil {
		doc.tumblers = map[string]*ui.TumblerState{}
	}
	viewKey := doc.key(n)
	st := doc.tumblers[viewKey]
	if st == nil {
		st = &ui.TumblerState{}
		doc.tumblers[viewKey] = st
	}
	doc.exposeIDProp(n, "moving", st.Moving())
	delegate, err := delegateChild(n)
	if err != nil {
		delegate = nil // QML's default renders the model's string value
	}
	var buildErr error
	wheel := ui.Tumbler(st, refInt(idxRef), func(i int) { _ = idxRef.set(i) },
		ui.TumblerOpts{Count: count, Visible: visible, Wrap: wrap, W: w, H: h, Spacing: spacing},
		func(i int, displacement float32) ui.Element {
			el, err := tumblerItem(delegate, viewKey, i, displacement, rows, env, doc)
			if err != nil {
				buildErr = err
				return ui.Div()
			}
			return el
		})
	if buildErr != nil {
		return nil, buildErr
	}
	return wheel, nil
}

// tumblerItem builds one wheel slot: the delegate in row i's scope with
// Tumbler.displacement bound, or the model value as text when the Tumbler
// declares no delegate.
func tumblerItem(delegate *node, viewKey string, i int, displacement float32,
	rows []map[string]any, env *Env, doc *docState) (ui.Element, error) {

	var row map[string]any
	if rows != nil {
		row = rows[i]
	} else {
		// An integer model ("model: 51"): QML gives the delegate the index
		// as its modelData.
		row = map[string]any{"modelData": float64(i), "index": float64(i)}
	}
	if delegate == nil {
		return ui.Text(str(row["modelData"]), ui.RGB(0x1a1a1a)), nil
	}
	restore := enterRow(viewKey, delegate, env, doc, row, i)
	prev, had := env.vars["Tumbler"]
	env.vars["Tumbler"] = map[string]any{
		"displacement": float64(displacement),
		"tumbler":      doc.elemObjs.get(doc.intern(viewKey)),
	}
	el, err := build(delegate, env, doc)
	if had {
		env.vars["Tumbler"] = prev
	} else {
		delete(env.vars, "Tumbler")
	}
	restore()
	return el, err
}

// spinProp is the animatable property behind the wheel's continuous
// position (nil when the ref is not a real float property).
func spinProp(ref Ref) *core.Property[float32] {
	pr, _, ok := propOf[float32](ref)
	if !ok {
		return nil
	}
	return pr.p
}

// flingDeceleration is how fast a flicked wheel slows, in pixels per second
// squared — the same rate a flicked list uses.
const flingDeceleration = 1500

// flingDistance is how far a release at this speed travels before stopping:
// v² / 2a, signed like the velocity.
func flingDistance(v float32) float32 {
	d := v * v / (2 * flingDeceleration)
	if v < 0 {
		return -d
	}
	return d
}

// flingDuration is how long that takes: v / a.
func flingDuration(v float32) time.Duration {
	if v < 0 {
		v = -v
	}
	return time.Duration(float64(v) / flingDeceleration * float64(time.Second))
}
