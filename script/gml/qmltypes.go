package gml

// Small QML types that are declarations rather than widgets: Binding (write
// a property when a condition holds), FrameAnimation (a per-frame tick),
// DropArea (a drop target), and ScrollBar as a standalone element. They live
// here rather than in build.go's element switch because each is a handful of
// lines with no shared machinery.

import (
	"fmt"

	"github.com/sushvibez/gfx/ui"
)

// applyBinding runs a QML `Binding { target: item; property: "width";
// value: expr; when: cond }`: while `when` holds (true by default), the
// named property of the target object is written from `value`. Re-run every
// build, so it behaves like the declarative binding it stands in for.
// `restoreMode` is accepted and ignored: we never restore the old value,
// because nothing here tracks what it was before the binding took over.
func (doc *docState) applyBinding(n *node, env *Env) {
	var targetExpr, valueExpr expr
	prop := ""
	when := true
	for _, a := range n.attrs {
		switch a.name {
		case "target":
			targetExpr = a.expr
		case "property":
			if v, err := a.expr.eval(env); err == nil {
				prop = str(v)
			}
		case "value":
			valueExpr = a.expr
		case "when":
			if b, err := attrBool(a, env); err == nil {
				when = b
			}
		}
	}
	if !when || valueExpr == nil {
		return
	}
	v, err := valueExpr.eval(env)
	if err != nil {
		logf("gml:%d: Binding value: %v", n.line, err)
		return
	}
	// `Binding { target: x; property: "p" }` and the shorthand
	// `Binding { x.p: value }` (any dotted attribute) both write through a
	// resolved property reference.
	if targetExpr != nil && prop != "" {
		ref, err := resolveRef(&memberExpr{base: targetExpr, name: prop, line: n.line}, env)
		if err != nil {
			logf("gml:%d: Binding target: %v", n.line, err)
			return
		}
		if err := ref.set(v); err != nil {
			logf("gml:%d: Binding: %v", n.line, err)
		}
		return
	}
	logf("gml:%d: Binding needs a target and a property name", n.line)
}

// frameAnim is a QML FrameAnimation: a handler run once per frame, with
// frameTime/elapsedTime in scope (seconds), driven by the same clamped
// wall-clock delta as Timer.
type frameAnim struct {
	node    *node
	env     *Env
	elapsed float64
}

// refreshFrameAnimation registers or updates a FrameAnimation node during
// the build pass; advanceFrameAnims runs the handlers.
func (doc *docState) refreshFrameAnimation(n *node, env *Env) {
	key := nodeID(n)
	if key == "" {
		key = fmt.Sprintf("__frameanim%d", n.line)
	}
	f := doc.frameAnims[key]
	if f == nil {
		f = &frameAnim{}
		doc.frameAnims[key] = f
	}
	// Snapshot the scope, like Timer: the handler runs from the frame tick.
	f.node, f.env = n, doc.scopeSnapshot(env, "frameanim:"+key)
}

// advanceFrameAnims runs every registered FrameAnimation's onTriggered for
// this frame's delta (milliseconds), skipping the ones not running.
func (doc *docState) advanceFrameAnims(dtMs float64) {
	for _, f := range doc.frameAnims {
		if f.node == nil {
			continue
		}
		running, paused := true, false
		var handler []stmt
		for _, a := range f.node.attrs {
			switch a.name {
			case "running":
				if b, err := attrBool(a, f.env); err == nil {
					running = b
				}
			case "paused":
				if b, err := attrBool(a, f.env); err == nil {
					paused = b
				}
			case "onTriggered":
				handler = a.stmt
			}
		}
		if !running || paused || handler == nil {
			continue
		}
		f.elapsed += dtMs / 1000
		scope := f.env.child()
		scope.vars["frameTime"] = dtMs / 1000
		scope.vars["elapsedTime"] = f.elapsed
		scope.vars["currentFrame"] = float64(int(f.elapsed * 60))
		if err := runStmts(handler, scope); err != nil {
			logf("gml: FrameAnimation onTriggered: %v", err)
		}
	}
}

// buildDropArea renders a QML DropArea: an invisible target that accepts a
// dragged payload. `onDropped` runs with `drop` in scope (its `text` and
// `source` are the payload); containsDrag is not modeled (the ui layer
// reports the drop, not the hover).
func buildDropArea(n *node, env *Env, doc *docState) (ui.Element, error) {
	var w, h float32
	var onDropped []stmt
	for _, a := range n.attrs {
		switch a.name {
		case "id", "keys", "enabled", "onEntered", "onExited", "onPositionChanged",
			"containsDrag", "drag.source":
			// accepted, not modeled
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			h = f
		case "onDropped", "onDrop":
			onDropped = a.stmt
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: DropArea has no attribute %q", a.line, a.name)
		}
	}
	// A DropArea has no visuals of its own; its children (a highlight
	// rectangle, usually) render inside it.
	inner := ui.Div()
	if w > 0 || h > 0 {
		inner.Size(w, h)
	} else {
		inner.Grow()
	}
	kids, err := buildChildren(n, env, doc, false)
	if err != nil {
		return nil, err
	}
	inner.Children(kids...)
	if onDropped == nil {
		return inner, nil
	}
	run := doc.handlerRunner(env)
	stmts := onDropped
	return ui.DropZone(inner, func(payload any) {
		env.vars["drop"] = map[string]any{"text": str(payload), "source": payload}
		run(stmts, "onDropped")
		delete(env.vars, "drop")
	}), nil
}

// buildScrollBar renders a standalone QtQuick.Controls ScrollBar element.
// The scroll views draw their own bars from the retained scroll state, so
// this is the indicator's placeholder: it occupies no space and paints
// nothing, which is what an attached `ScrollBar.vertical` already does.
func buildScrollBar(n *node, env *Env, doc *docState) (ui.Element, error) {
	return ui.Div(), nil
}
