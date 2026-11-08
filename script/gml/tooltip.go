package gml

import (
	"time"

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

// attachTooltip floats the item's tooltip above it while it is wanted and
// its delay has passed (ui.ToolTipState). The element is returned
// unchanged when the tip is not showing.
func (doc *docState) attachTooltip(n *node, env *Env, el ui.Element, t tooltipSpec) ui.Element {
	box, ok := el.(*ui.DivEl)
	if !ok {
		return el // only a box can carry the overlay (and be hovered)
	}
	if doc.tipStates == nil {
		doc.tipStates = map[string]*ui.ToolTipState{}
	}
	key := doc.key(n)
	st := doc.tipStates[key]
	if st == nil {
		st = &ui.ToolTipState{}
		doc.tipStates[key] = st
	}
	// The item's own hover state, tracked HERE rather than read off the
	// control's own slot: a styled control keys it one way and a plain box
	// another, and a tooltip can hang off either.
	st.Track(box)
	hovered := st.Hovered()
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
	show, waiting := st.Due(want, ui.ToolTipOpts{
		Delay:   time.Duration(t.delay) * time.Millisecond,
		Timeout: time.Duration(t.timeout) * time.Millisecond,
	})
	// A tip waiting for its delay (or counting down its timeout) waits on
	// the CLOCK, not on a property — so the document has to keep
	// rebuilding while it waits, or it would be handed its cached tree for
	// ever and the wait would never end.
	if waiting || (show && t.timeout == 0) {
		doc.timed++
	}
	if !show {
		return el
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
	return ui.AttachTip(box, unwrapModal(panel))
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
