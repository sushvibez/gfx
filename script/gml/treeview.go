package gml

// Qt 6 TreeView, over ui.TreeView (the Go API for the same): hierarchical
// data flattened to visible rows (Qt's own TreeModelAdaptor does the
// same), one delegate instance per visible row with the Qt-injected tree
// properties: treeView, isTreeNode, expanded, hasChildren, depth, row,
// column, plus the node's own roles. The model is a nested tree — a plain
// JS array of {role..., children: [...]} objects, or an object (typically a
// Go-registered backend) exposing that array as its `tree` member.
// Expand/collapse state persists per node path in the view's
// ui.TreeState, so toggling survives frames; a settable per-view
// currentRow drives `selected`, and the view's id exposes
// toggleExpanded/expand/collapse/index plus a selectionModel stub, so the
// corpus' delegate TapHandlers work unmodified.

import (
	"fmt"

	"github.com/sushvibez/gfx/ui"
)

// resolveTreeModel reads a TreeView's model: attribute to the nested tree
// array: either the array itself or an object exposing it as `tree`.
func resolveTreeModel(a attr, env *Env) ([]any, error) {
	v, err := resolveAny(a.expr, env)
	if err != nil {
		return nil, err
	}
	if r, ok := v.(Ref); ok {
		v = r.get()
	}
	if o, ok := v.(*elemObject); ok {
		if t, found := o.member("tree"); found {
			v = t
		}
	}
	if arr, ok := v.([]any); ok {
		return arr, nil
	}
	return nil, fmt.Errorf("gml:%d: TreeView model wants a tree (array of nodes with children), got %T", a.line, v)
}

// treeNodes converts the model's nested node array to ui.TreeNodes, each
// node's roles (all but children) as its Data.
func treeNodes(nodes []any) []ui.TreeNode {
	var out []ui.TreeNode
	for _, item := range nodes {
		node, ok := item.(map[string]any)
		if !ok {
			continue
		}
		roles := make(map[string]any, len(node))
		for k, v := range node {
			if k != "children" {
				roles[k] = v
			}
		}
		tn := ui.TreeNode{Data: roles}
		for _, k := range []string{"display", "name", "text", "title"} {
			if v, ok := roles[k]; ok {
				tn.Label = str(v)
				break
			}
		}
		children, _ := node["children"].([]any)
		tn.Children = treeNodes(children)
		out = append(out, tn)
	}
	return out
}

// buildTreeView renders the visible rows and wires the view's expand /
// selection API onto its id.
func buildTreeView(n *node, env *Env, doc *docState) (ui.Element, error) {
	var modelAttr *attr
	var height float32
	for i := range n.attrs {
		a := &n.attrs[i]
		switch a.name {
		case "id":
		case "model":
			modelAttr = a
		case "height":
			f, err := attrFloat(*a, env)
			if err != nil {
				return nil, err
			}
			height = f
		case "delegate":
			// consumed by delegateChild (element-valued)
		case "width", "clip", "boundsBehavior", "interactive", "reuseItems",
			"selectionBehavior", "selectionMode", "selectionModel",
			"rowSpacing", "columnSpacing":
			// accepted, not modeled (selection rendering is via currentRow)
		default:
			return nil, fmt.Errorf("gml:%d: TreeView has no attribute %q", a.line, a.name)
		}
	}
	if modelAttr == nil {
		return nil, fmt.Errorf("gml:%d: TreeView needs a model", n.line)
	}
	tree, err := resolveTreeModel(*modelAttr, env)
	if err != nil {
		return nil, err
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	viewKey := doc.key(n)
	if doc.trees == nil {
		doc.trees = map[string]*ui.TreeState{}
	}
	st := doc.trees[viewKey]
	if st == nil {
		st = &ui.TreeState{}
		doc.trees[viewKey] = st
	}
	roots := treeNodes(tree)
	visible := st.Rows(roots)
	curRef := doc.intProp(viewKey+"|currentRow", -1)
	cur := refInt(curRef)
	st.SetCurrent(cur)

	// The view's API, used from delegate handlers: toggleExpanded(row) etc.
	// operate on THIS build's row→path mapping.
	paths := make([]string, len(visible))
	for i, r := range visible {
		paths[i] = r.Path
	}
	setExpanded := func(args []any, to bool, toggle bool) (any, error) {
		if len(args) < 1 {
			return nil, nil
		}
		i, err := toInt(args[0])
		if err != nil || i < 0 || i >= len(paths) {
			return nil, nil
		}
		if toggle {
			st.Toggle(paths[i])
		} else {
			st.SetExpanded(paths[i], to)
		}
		return nil, nil
	}
	view := doc.elemObjs.get(doc.intern(nodeID(n)))
	if view == nil {
		view = &elemObject{}
		if id := nodeID(n); id != "" {
			doc.elemObjs.set(doc.intern(id), view)
		}
	}
	if view.methods == nil {
		view.methods = map[string]any{}
	}
	view.methods["toggleExpanded"] = jsFunc(func(a []any) (any, error) { return setExpanded(a, false, true) })
	view.methods["expand"] = jsFunc(func(a []any) (any, error) { return setExpanded(a, true, false) })
	view.methods["collapse"] = jsFunc(func(a []any) (any, error) { return setExpanded(a, false, false) })
	view.methods["index"] = jsFunc(func(a []any) (any, error) {
		if len(a) >= 1 {
			if i, err := toInt(a[0]); err == nil {
				return float64(i), nil
			}
		}
		return float64(-1), nil
	})
	doc.exposeIDProp(n, "rows", float64(len(visible)))
	doc.exposeIDProp(n, "currentRow", curRef)
	// selectionModel stub: setCurrentIndex(index, mode) selects the row
	// (index is what view.index(row, 0) returned — the row itself).
	doc.exposeIDProp(n, "selectionModel", &elemObject{methods: map[string]any{
		"setCurrentIndex": jsFunc(func(a []any) (any, error) {
			if len(a) >= 1 {
				if i, err := toInt(a[0]); err == nil {
					return nil, curRef.set(i)
				}
			}
			return nil, nil
		}),
		"clear": jsFunc(func([]any) (any, error) { return nil, curRef.set(-1) }),
	}})

	// The rows are ui.TreeView's; each is the delegate in the row's scope.
	// The keyboard moves gml's own currentRow (which the next build reads back).
	onCurrent := func(i int) { _ = curRef.set(i) }
	return ui.TreeView(st, roots, ui.TreeOpts{H: height, OnCurrent: onCurrent, Row: func(r ui.TreeRow, current bool) ui.Element {
		row := map[string]any{
			"treeView": view, "isTreeNode": true,
			"expanded": r.Expanded, "hasChildren": r.HasChildren,
			"depth": float64(r.Depth), "row": float64(r.Index), "column": float64(0),
			"current": current, "selected": current,
		}
		if roles, ok := r.Node.Data.(map[string]any); ok {
			for k, v := range roles {
				row[k] = v
			}
		}
		restore := enterRow(viewKey, delegate, env, doc, row, r.Index)
		defer restore()
		el, err := build(delegate, env, doc)
		if err != nil {
			return ui.Text("gml: "+err.Error(), ui.RGB(0xffb4b4))
		}
		return el
	}}), nil
}
