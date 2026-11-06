package gml

import (
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// QML's attached ToolTip: any Control can carry one, and the idiom is to
// hang it off the control's own hover state —
//
//	Button {
//	    text: "Instant ToolTip"
//	    ToolTip.visible: hovered
//	    ToolTip.text: "This appears immediately!"
//	    ToolTip.delay: 0
//	    ToolTip.timeout: 3000
//	}
//
// The tip itself is a ToolTip, so it draws through the application's own
// ToolTip style when there is one; here it is floated above the control,
// centred on it, exactly as that style places it.
//
// The delay and timeout are real: Qt shows the tip only once the pointer
// has rested for `delay`, and takes it away again after `timeout`. Ignore
// them and a page of tooltips with 0/500/1500ms delays shows all three at
// once, where Qt shows the ones whose time has come.

// tooltipSpec is what an item's ToolTip.* attributes asked for.
type tooltipSpec struct {
	text    string
	visible expr
	delay   float32 // ms the pointer must rest before it shows
	timeout float32 // ms after which it goes away again (0 = stays)
}

// tooltipOf reads an item's attached ToolTip attributes.
func tooltipOf(n *node, env *Env) (tooltipSpec, bool) {
	var t tooltipSpec
	found := false
	for _, a := range n.attrs {
		switch a.name {
		case "ToolTip.text":
			if v, err := a.expr.eval(env); err == nil {
				t.text, found = str(v), true
			}
		case "ToolTip.visible":
			t.visible, found = a.expr, true
		case "ToolTip.delay":
			if f, err := attrFloat(a, env); err == nil {
				t.delay = f
			}
		case "ToolTip.timeout":
			if f, err := attrFloat(a, env); err == nil {
				t.timeout = f
			}
		}
	}
	return t, found && t.text != ""
}

// attachTooltip floats the item's tooltip above it while the pointer rests
// on it. The element is returned unchanged when the tip is not showing.
func (doc *docState) attachTooltip(n *node, env *Env, el ui.Element, t tooltipSpec) ui.Element {
	box, ok := el.(*ui.DivEl)
	if !ok {
		return el // only a box can carry the overlay (and be hovered)
	}
	key := doc.key(n)
	// The item's own live hover state. Wired HERE rather than read off the
	// control's own slot: a styled control keys it one way and a plain box
	// another, and a tooltip can hang off either.
	hover := doc.boolProp(key+"|tipHover", false)
	hovered, _ := toBool(hover.get())
	box.OnHoverChanged(func(v bool) {
		if now, _ := toBool(hover.get()); now != v {
			_ = hover.set(v)
		}
	})
	// `ToolTip.visible: hovered` reads the control's state by bare name.
	want := hovered
	if t.visible != nil {
		scope := env.child()
		scope.vars["hovered"] = hovered
		scope.vars["containsMouse"] = hovered
		if v, err := t.visible.eval(scope); err == nil {
			want, _ = toBool(v)
		}
	}
	if !want {
		delete(doc.tipSince, key)
		return el
	}
	// How long the pointer has rested here, on the frame loop's clock (the
	// same one Timers and animations follow, so a headless capture sees the
	// delay elapse just as a window does).
	now := core.AnimClock()
	if doc.tipSince == nil {
		doc.tipSince = map[string]time.Duration{}
	}
	since, held := doc.tipSince[key]
	if !held {
		doc.tipSince[key] = now
		since = now
	}
	rested := now - since
	// A tip waiting for its delay (or counting down its timeout) is waiting
	// on the CLOCK, not on a property — so the document has to keep
	// rebuilding while it waits, or it would be handed its cached tree for
	// ever and the wait would never end.
	if rested < time.Duration(t.delay+t.timeout)*time.Millisecond || t.timeout == 0 {
		doc.timed++
	}
	if rested < time.Duration(t.delay)*time.Millisecond {
		return el // not yet
	}
	if t.timeout > 0 && rested > time.Duration(t.delay+t.timeout)*time.Millisecond {
		return el // shown its while
	}
	// A ToolTip element, so the app's own ToolTip style draws it.
	tip := &node{typeName: "ToolTip", line: n.line, attrs: []attr{
		{name: "text", expr: &litExpr{v: t.text}, line: n.line},
		{name: "visible", expr: &litExpr{v: true}, line: n.line},
	}}
	panel, err := build(tip, env, doc)
	if err != nil || panel == nil {
		logf("gml: tooltip: %v", err)
		return el
	}
	// Qt's ToolTip style places it centred above its parent, 4px clear.
	box.Children(ui.Overlay(unwrapModal(panel), 0, 0).AnchorAbove(box, 4).SnapToWindow(2))
	return box
}

// unwrapModal takes the panel out of the floating wrapper a popup build
// returns: the tooltip is placed by its own overlay, anchored to the item
// it belongs to, rather than centred in the window like a dialog.
func unwrapModal(el ui.Element) ui.Element {
	for _, ch := range ui.Children(el) {
		switch el.(type) {
		case *ui.ModalEl, *ui.OverlayEl:
			return ch
		}
	}
	return el
}
