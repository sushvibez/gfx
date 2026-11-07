package gml

// Qt Quick Controls' SplitView: panes laid out along one axis with a handle
// between each pair. The attached properties size the panes —
// SplitView.preferredWidth/Height, fillWidth/fillHeight and the
// minimum/maximum pair — and the `handle:` delegate draws the divider (its
// SplitHandle.pressed / .hovered state reads as "not interacting" here).
//
// Dragging a handle resizes the panes either side of it: each handle owns a
// persistent offset, added to the pane before it and taken off the pane
// after, clamped by both panes' minimum/maximum. The panes' own sizes still
// come from the markup — the offset is what the user has done to them.

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
	box := ui.Div()
	if !vertical {
		box.Row()
	}
	if w > 0 || h > 0 {
		box.Size(w, h)
	}
	if grow {
		box.Grow()
	}
	// The panes, and what the user has dragged: offset[i] belongs to the
	// handle after pane i, and moves the boundary between panes i and i+1.
	type paneBuild struct {
		el    ui.Element
		hints splitHints
	}
	var panes []paneBuild
	for _, c := range n.children {
		if isNonVisual(c) {
			doc.refreshNonVisual(c, env)
			continue
		}
		pane, hints := splitPane(c, env)
		// Panes are built FRESH, never served from the subtree cache: the
		// split sizes them afterwards by mutating the element it got back
		// (applySplitHints), and a reused element carries last frame's
		// layout state into that — the nested IDE card's editor column came
		// back a third shorter. A parent that mutates what build returns
		// must not share it across frames.
		el, err := buildFresh(pane, env, doc)
		if err != nil {
			if doc.lenient {
				logf("gml: %v (pane skipped)", err)
				continue
			}
			return nil, err
		}
		panes = append(panes, paneBuild{el, hints})
	}
	key := doc.key(n)
	for i := range panes {
		if i > 0 {
			gap := i - 1
			before, after := &panes[gap].hints, &panes[i].hints
			off := doc.floatProp(fmt.Sprintf("%s|split:%d", key, gap), 0)
			hover := doc.boolProp(fmt.Sprintf("%s|split:%d.hovered", key, gap), false)
			press := doc.boolProp(fmt.Sprintf("%s|split:%d.pressed", key, gap), false)
			hel := buildSplitHandle(handle, env, doc, vertical, hover, press)
			if hel != nil {
				box.Children(splitDragger(hel, off, before, after, vertical, press))
			}
		}
		applySplitHints(panes[i].el, panes[i].hints, vertical,
			splitOffsetFor(doc, key, i, len(panes)))
		box.Children(panes[i].el)
	}
	return box, nil
}

// splitOffsetFor is how much the user has dragged pane i: the handle after
// it gives, the handle before it takes.
func splitOffsetFor(doc *docState, key string, i, n int) float32 {
	var d float32
	if i < n-1 {
		if r, ok := doc.synthRefs[fmt.Sprintf("%s|split:%d", key, i)]; ok {
			f, _ := toFloat32(r.get())
			d += f
		}
	}
	if i > 0 {
		if r, ok := doc.synthRefs[fmt.Sprintf("%s|split:%d", key, i-1)]; ok {
			f, _ := toFloat32(r.get())
			d -= f
		}
	}
	return d
}

// splitDragger wraps a handle so dragging it moves the boundary. The offset
// is clamped so neither pane goes under its stated minimum or over its
// maximum (Qt refuses to drag a handle past a pane's minimumWidth or
// maximumWidth) — and the offset is absolute, so
// the drag follows the pointer rather than accumulating per event.
// pressed is the handle's SplitHandle.pressed: true from the press to the
// release, wherever the pointer goes meanwhile.
func splitDragger(handle ui.Element, off Ref, before, after *splitHints, vertical bool, pressed Ref) ui.Element {
	minBefore, minAfter := before.minimum, after.minimum
	maxBefore, maxAfter := before.maximum, after.maximum
	prefBefore, prefAfter := before.preferred, after.preferred
	start := float32(0)
	// The drag goes on the handle ITSELF: wrapped in a box, the handle lost
	// the size the split gave it and drew as a hairline.
	d, ok := handle.(*ui.DivEl)
	if !ok {
		return handle
	}
	d.OnDrag(func(dx, dy float32, phase ui.DragPhase) {
		d := dx
		if vertical {
			d = dy
		}
		switch phase {
		case ui.DragStart:
			f, _ := toFloat32(off.get())
			start = f
			setBool(pressed, true)
			return
		case ui.DragEnd:
			setBool(pressed, false)
		}
		want := start + d
		// A pane with no preferred size has no floor to compute against;
		// then only the other pane's minimum limits the drag.
		if prefBefore > 0 && minBefore > 0 {
			want = max(want, minBefore-prefBefore)
		}
		if prefAfter > 0 && minAfter > 0 {
			want = min(want, prefAfter-minAfter)
		}
		// Nor past a maximum: the pane before grows by the offset, the
		// pane after by its negative.
		if prefBefore > 0 && maxBefore > 0 {
			want = min(want, maxBefore-prefBefore)
		}
		if prefAfter > 0 && maxAfter > 0 {
			want = max(want, prefAfter-maxAfter)
		}
		_ = off.set(want)
	})
	return d
}

// splitHints is what a pane asked for along the split axis.
type splitHints struct {
	preferred float32
	minimum   float32
	maximum   float32 // 0: none
	fill      bool
}

// splitPane strips the SplitView.* attached properties off a pane and
// returns them.
func splitPane(n *node, env *Env) (*node, splitHints) {
	var hints splitHints
	cp := *n
	cp.attrs = nil
	for _, a := range n.attrs {
		switch a.name {
		case "SplitView.preferredWidth", "SplitView.preferredHeight":
			if f, err := attrFloat(a, env); err == nil {
				hints.preferred = f
			}
		case "SplitView.minimumWidth", "SplitView.minimumHeight":
			if f, err := attrFloat(a, env); err == nil {
				hints.minimum = f
			}
		case "SplitView.fillWidth", "SplitView.fillHeight":
			if b, err := attrBool(a, env); err == nil {
				hints.fill = b
			}
		case "SplitView.maximumWidth", "SplitView.maximumHeight":
			if f, err := attrFloat(a, env); err == nil {
				hints.maximum = f
			}
		default:
			cp.attrs = append(cp.attrs, a)
		}
	}
	return &cp, hints
}

// applySplitHints sizes a pane along the split axis, including whatever the
// user has dragged onto it.
func applySplitHints(el ui.Element, hints splitHints, vertical bool, dragged float32) {
	d, ok := el.(*ui.DivEl)
	if !ok {
		return
	}
	switch {
	case hints.fill:
		d.Grow()
	case hints.preferred > 0:
		size := max(hints.preferred+dragged, 1)
		if hints.maximum > 0 {
			size = min(size, hints.maximum)
		}
		if vertical {
			d.H(size)
		} else {
			d.W(size)
		}
	}
	if hints.minimum > 0 {
		if vertical {
			d.MinH(hints.minimum)
		} else {
			d.MinW(hints.minimum)
		}
	}
	// A pane with no hint at all shares what is left, as Qt's does.
	if !hints.fill && hints.preferred == 0 {
		d.Grow()
	}
}

// buildSplitHandle draws the divider between two panes: the `handle:`
// delegate when the markup gives one, else a thin neutral bar. hovered and
// pressed are the handle's SplitHandle.hovered / .pressed, kept across
// builds (splitDragger sets pressed).
func buildSplitHandle(handle *node, env *Env, doc *docState, vertical bool, hovered, pressed Ref) ui.Element {
	watch := func(el ui.Element) ui.Element {
		if d, ok := el.(*ui.DivEl); ok {
			d.OnHoverChanged(func(on bool) { setBool(hovered, on) })
		}
		return el
	}
	if handle != nil {
		// The handle's own attached state, read by the delegate
		// (`color: SplitHandle.pressed ? … : SplitHandle.hovered ? … : …`).
		h, _ := toBool(hovered.get())
		p, _ := toBool(pressed.get())
		prev, had := env.vars["SplitHandle"]
		env.vars["SplitHandle"] = map[string]any{"pressed": p, "hovered": h}
		defer func() {
			if had {
				env.vars["SplitHandle"] = prev
			} else {
				delete(env.vars, "SplitHandle")
			}
		}()
		if el, err := build(stripAnchorAttrs(handle), env, doc); err == nil && el != nil {
			if d, ok := el.(*ui.DivEl); ok {
				// The delegate sizes its own thickness; it stretches along
				// the other axis like Qt's does.
				if vertical {
					d.W(0)
				} else {
					d.H(0)
				}
			}
			return watch(el)
		}
	}
	bar := ui.Div().Bg(ui.RGBA{R: 0.35, G: 0.36, B: 0.4, A: 1})
	if vertical {
		return watch(bar.H(4))
	}
	return watch(bar.W(4))
}

// setBool writes a bool Ref only when it changes, so a hover reported
// every paint does not rebuild the page every frame.
func setBool(r Ref, v bool) {
	if cur, _ := toBool(r.get()); cur != v {
		_ = r.set(v)
	}
}
