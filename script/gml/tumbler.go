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
	"math"
	"time"

	"github.com/sushvibez/gfx/core"

	"github.com/sushvibez/gfx/app"

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
	// The wheel shows a fixed number of slots in its own height, and never
	// paints outside it — a long label (a month name in a narrow column)
	// is cut, not spilled over the neighbouring wheel.
	box := ui.Div().ItemsCenter().JustifyCenter().Clip()
	if w > 0 {
		box.W(w)
	}
	if h > 0 {
		box.H(h)
	}
	if spacing > 0 {
		box.Gap(spacing)
	}
	if count == 0 {
		return box, nil
	}
	// A model shorter than the window cannot wrap, and the index stays in
	// range even after the model shrinks under it (a month change dropping
	// February's 29th).
	if count < visible {
		wrap = false
	}
	cur := refInt(idxRef)
	if cur >= count || cur < 0 {
		cur = min(max(cur, 0), count-1)
		_ = idxRef.set(cur)
	}
	delegate, err := delegateChild(n)
	if err != nil {
		// No delegate: QML's default renders the model's string value.
		delegate = nil
	}
	viewKey := doc.key(n)
	for _, slot := range tumblerWindow(cur, count, visible, wrap) {
		if slot.index < 0 {
			// Off the ends of a non-wrapping wheel: an empty slot keeps the
			// centred item centred.
			box.Children(ui.Div().Grow())
			continue
		}
		i := slot.index
		el, err := tumblerItem(delegate, viewKey, i, slot.displacement, rows, env, doc)
		if err != nil {
			return nil, err
		}
		pick := i
		box.Children(ui.Div().ItemsCenter().JustifyCenter().Grow().
			OnClick(func() { _ = idxRef.set(pick) }).Children(el))
	}
	// spin is the wheel's CONTINUOUS position: the index is where it points
	// (rounded), and a flick tweens it so the wheel rolls to a stop instead
	// of jumping. Every other input keeps it in step.
	spinRef := doc.floatProp(doc.key(n)+"|spin", float32(cur))
	settle := func(next int) int {
		if wrap {
			return ((next % count) + count) % count
		}
		return min(max(next, 0), count-1)
	}
	// While a flick is in flight the animation owns the wheel: follow the
	// continuous position. Once it stops, the index is the truth again.
	if p := spinProp(spinRef); p != nil && core.TweenRunning(p) {
		if at := settle(int(math.Round(float64(p.Get())))); at != cur {
			cur = at
			_ = idxRef.set(at)
		}
	}
	// step moves the wheel by whole items, wrapping or stopping at the ends
	// the way the model allows.
	step := func(from, by int) {
		next := settle(from + by)
		_ = idxRef.set(next)
		_ = spinRef.set(float32(next)) // the continuous position follows
	}
	// Arrow keys move the wheel, as they do in QML.
	box.Focusable().OnKey(func(e app.KeyEvent) {
		switch {
		case !e.Down:
		case e.Key == app.KeyUp:
			step(refInt(idxRef), -1)
		case e.Key == app.KeyDown:
			step(refInt(idxRef), 1)
		}
	})
	// Dragging spins it: the wheel follows the pointer a row at a time, from
	// the item that was centred when the press landed — so a drag of half a
	// row does nothing and letting go leaves it snapped, as a detented wheel
	// does. Dragging DOWN brings earlier items down into view.
	rowH := h / float32(visible)
	if rowH <= 0 {
		// No stated height: the box the wheel was given last frame.
		if pe := doc.prevGeom(doc.keyID(n)); pe != nil {
			rowH = ui.Bounds(pe).H / float32(visible)
		}
	}
	if rowH > 0 && count > 1 {
		startRef := doc.intProp(doc.key(n)+"|dragFrom", 0)
		box.OnDrag(func(_, dy float32, phase ui.DragPhase) {
			if phase == ui.DragStart {
				core.StopTween(spinProp(spinRef)) // a press catches a spinning wheel
				_ = startRef.set(refInt(idxRef))
				return
			}
			step(refInt(startRef), -int(math.Round(float64(dy/rowH))))
		})
		// Letting go while still moving rolls the wheel on and settles it,
		// slowing at the same rate a flicked list does.
		box.OnFling(func(_, vy float32) {
			p := spinProp(spinRef)
			if p == nil {
				return
			}
			rows := -flingDistance(vy) / rowH
			target := settle(refInt(idxRef) + int(math.Round(float64(rows))))
			p.Set(float32(refInt(idxRef)))
			core.Tween(p, float32(target), flingDuration(vy), core.EaseOutQuad)
		})
	}
	return box, nil
}

// wheelSlot is one visible position on the wheel: which model index it shows
// (-1 for an empty slot past the end of a non-wrapping wheel) and how far it
// sits from the centred item, which the delegates fade and shrink by.
type wheelSlot struct {
	index        int
	displacement float32
}

// tumblerWindow is the visible window: `visible` slots centred on cur,
// wrapping around the model when the wheel wraps.
func tumblerWindow(cur, count, visible int, wrap bool) []wheelSlot {
	half := visible / 2
	slots := make([]wheelSlot, 0, visible)
	for d := -half; d <= half; d++ {
		i := cur + d
		switch {
		case wrap:
			i = ((i % count) + count) % count
		case i < 0 || i >= count:
			i = -1
		}
		slots = append(slots, wheelSlot{index: i, displacement: float32(d)})
	}
	return slots
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
