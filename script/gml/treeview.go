package gml

// Qt 6 TreeView: hierarchical data flattened to visible rows (Qt's own
// TreeModelAdaptor does the same), one delegate instance per visible row
// with the Qt-injected tree properties: treeView, isTreeNode, expanded,
// hasChildren, depth, row, column, plus the node's own roles. The model is
// a nested tree — a plain JS array of {role..., children: [...]} objects,
// or an object (typically a Go-registered backend) exposing that array as
// its `tree` member. Expand/collapse state persists per node path in
// docState (boolProp), so toggling survives frames and reloads; a settable
// per-view currentRow drives `selected`, and the view's id exposes
// toggleExpanded/expand/collapse/index plus a selectionModel stub, so the
// corpus' delegate TapHandlers work unmodified.

import (
	"fmt"
	"strconv"

	"github.com/sushvibez/gfx/ui"
)

// treeNode is one flattened visible row.
type treeNode struct {
	roles       map[string]any
	path        string // stable identity: child-index chain ("0/2/1")
	depth       int
	hasChildren bool
	expanded    bool
}

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

// flattenTree walks the nested node array depth-first, appending visible
// rows; children recurse only under an expanded node.
func (doc *docState) flattenTree(nodes []any, viewKey, parentPath string, depth int, out []treeNode) []treeNode {
	for i, item := range nodes {
		node, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path := parentPath + strconv.Itoa(i)
		children, _ := node["children"].([]any)
		roles := make(map[string]any, len(node))
		for k, v := range node {
			if k != "children" {
				roles[k] = v
			}
		}
		expRef := doc.boolProp(viewKey+"|exp:"+path, false)
		expanded, _ := toBool(expRef.get())
		out = append(out, treeNode{
			roles: roles, path: path, depth: depth,
			hasChildren: len(children) > 0, expanded: expanded,
		})
		if expanded && len(children) > 0 {
			out = doc.flattenTree(children, viewKey, path+"/", depth+1, out)
		}
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
	visible := doc.flattenTree(tree, viewKey, "", 0, nil)
	curRef := doc.intProp(viewKey+"|currentRow", -1)
	cur := refInt(curRef)

	// The view's API, used from delegate handlers: toggleExpanded(row) etc.
	// operate on THIS build's row→path mapping; a toggle sets the node's
	// persistent boolProp, whose revision bump triggers the rebuild.
	paths := make([]string, len(visible))
	for i, tn := range visible {
		paths[i] = tn.path
	}
	setExpanded := func(args []any, to bool, toggle bool) (any, error) {
		if len(args) < 1 {
			return nil, nil
		}
		i, err := toInt(args[0])
		if err != nil || i < 0 || i >= len(paths) {
			return nil, nil
		}
		ref := doc.boolProp(viewKey+"|exp:"+paths[i], false)
		if toggle {
			b, _ := toBool(ref.get())
			to = !b
		}
		return nil, ref.set(to)
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

	col := ui.Div().Col()
	for i, tn := range visible {
		row := map[string]any{
			"treeView": view, "isTreeNode": true,
			"expanded": tn.expanded, "hasChildren": tn.hasChildren,
			"depth": float64(tn.depth), "row": float64(i), "column": float64(0),
			"current": i == cur, "selected": i == cur,
		}
		for k, v := range tn.roles {
			row[k] = v
		}
		restore := enterRow(viewKey, delegate, env, doc, row, i)
		el, err := build(delegate, env, doc)
		restore()
		if err != nil {
			el = ui.Text("gml: "+err.Error(), ui.RGB(0xffb4b4))
		}
		col.Children(el)
	}
	scrollKey := doc.rowPrefix + fmt.Sprintf("__tree%d", n.line)
	scrollState := doc.scrolls[scrollKey]
	if scrollState == nil {
		scrollState = &ui.ScrollState{}
		doc.scrolls[scrollKey] = scrollState
	}
	s := ui.Scroll(scrollState).AutoHideBar().Children(col)
	if height > 0 {
		s.H(height)
	}
	return s, nil
}
