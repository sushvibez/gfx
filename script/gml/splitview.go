package gml

// Qt Quick Controls' SplitView, over ui.SplitView (the Go API for the
// same): panes laid out along one axis with a handle between each pair.
// The attached properties size the panes — SplitView.preferredWidth/Height,
// fillWidth/fillHeight and the minimum/maximum pair — and the `handle:`
// delegate draws the divider, reading SplitHandle.pressed / .hovered.
// Dragging a handle resizes the panes either side of it, clamped by both
// panes' minimum/maximum.

import (
	"fmt"

	"github.com/sushvibez/gfx/ui"
)

// buildSplitView renders a SplitView.
func buildSplitView(n *node, env *Env, doc *docState) (ui.Element, error) {
	vertical := false
	var w, h float32
	grow := false
	var handle *node
	for _, a := range n.attrs {
		switch a.name {
		case "orientation":
			if v, err := a.expr.eval(env); err == nil {
				if f, err := toFloat64(v); err == nil {
					vertical = int(f) == 2 // Qt.Vertical
				} else {
					vertical = containsFold(str(v), "Vertical")
				}
			}
		case "handle":
			handle = a.elem
		case "width":
			if f, err := attrFloat(a, env); err == nil {
				w = f
			}
		case "height":
			if f, err := attrFloat(a, env); err == nil {
				h = f
			}
		case "anchors.fill":
			grow = true
		case "id", "clip", "z", "opacity", "visible", "enabled", "spacing",
			"anchors.margins", "anchors.centerIn", "objectName", "padding",
			"resizing", "background", "contentItem":
			// accepted, applied below or not modeled
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: SplitView has no attribute %q", a.line, a.name)
		}
	}
	var panes []ui.SplitPane
	for _, c := range n.children {
		if isNonVisual(c) {
			doc.refreshNonVisual(c, env)
			continue
		}
		pane, sp := splitPane(c, env)
		// Panes are built FRESH, never served from the subtree cache: the
		// split sizes them afterwards by mutating the element it got back,
		// and a reused element carries last frame's layout state into that
		// — the nested IDE card's editor column came back a third shorter.
		// A parent that mutates what build returns must not share it
		// across frames.
		el, err := buildFresh(pane, env, doc)
		if err != nil {
			if doc.lenient {
				logf("gml: %v (pane skipped)", err)
				continue
			}
			return nil, err
		}
		sp.El = el
		panes = append(panes, sp)
	}
	if doc.splits == nil {
		doc.splits = map[string]*ui.SplitState{}
	}
	key := doc.key(n)
	st := doc.splits[key]
	if st == nil {
		st = &ui.SplitState{}
		doc.splits[key] = st
	}
	o := ui.SplitOpts{Vertical: vertical}
	if handle != nil {
		o.Handle = func(_ int, hovered, pressed bool) ui.Element {
			return buildSplitHandle(handle, env, doc, hovered, pressed)
		}
	}
	split := ui.SplitView(st, panes, o)
	if w > 0 || h > 0 {
		split.Size(w, h)
	}
	if grow {
		split.Grow()
	}
	return split, nil
}

// splitPane strips the SplitView.* attached properties off a pane and
// returns them.
func splitPane(n *node, env *Env) (*node, ui.SplitPane) {
	var sp ui.SplitPane
	cp := *n
	cp.attrs = nil
	for _, a := range n.attrs {
		switch a.name {
		case "SplitView.preferredWidth", "SplitView.preferredHeight":
			if f, err := attrFloat(a, env); err == nil {
				sp.Preferred = f
			}
		case "SplitView.minimumWidth", "SplitView.minimumHeight":
			if f, err := attrFloat(a, env); err == nil {
				sp.Minimum = f
			}
		case "SplitView.fillWidth", "SplitView.fillHeight":
			if b, err := attrBool(a, env); err == nil {
				sp.Fill = b
			}
		case "SplitView.maximumWidth", "SplitView.maximumHeight":
			if f, err := attrFloat(a, env); err == nil {
				sp.Maximum = f
			}
		default:
			cp.attrs = append(cp.attrs, a)
		}
	}
	return &cp, sp
}

// buildSplitHandle builds the `handle:` delegate with the handle's own
// attached state bound (`color: SplitHandle.pressed ? … : SplitHandle.hovered
// ? … : …`). nil (ui.SplitView's own bar) when it does not build.
func buildSplitHandle(handle *node, env *Env, doc *docState, hovered, pressed bool) ui.Element {
	prev, had := env.vars["SplitHandle"]
	env.vars["SplitHandle"] = map[string]any{"pressed": pressed, "hovered": hovered}
	defer func() {
		if had {
			env.vars["SplitHandle"] = prev
		} else {
			delete(env.vars, "SplitHandle")
		}
	}()
	el, err := build(stripAnchorAttrs(handle), env, doc)
	if err != nil {
		return nil
	}
	return el
}

// setBool writes a bool Ref only when it changes, so a state reported
// every paint does not rebuild the page every frame.
func setBool(r Ref, v bool) {
	if cur, _ := toBool(r.get()); cur != v {
		_ = r.set(v)
	}
}
