package gml

// The evaluated document tree: a small exported view of a parsed document
// for hosts that define their own element vocabulary (the cad feature
// tree, config documents) without depending on gml's internal AST. Attrs
// arrive EVALUATED against a fresh env — right for data documents whose
// attributes are literals; hosts that need live bindings use the dedicated
// builders (ui, Scene3D) instead.

import "fmt"

// TreeNode is one element of an evaluated document.
type TreeNode struct {
	Type     string
	Line     int
	Attrs    []TreeAttr
	Children []*TreeNode
}

// TreeAttr is an evaluated attribute: numbers arrive as float64, strings,
// bools, and lists as []any.
type TreeAttr struct {
	Name  string
	Value any
}

// Attr returns an attribute's value (nil, false when absent).
func (n *TreeNode) Attr(name string) (any, bool) {
	for _, a := range n.Attrs {
		if a.Name == name {
			return a.Value, true
		}
	}
	return nil, false
}

// Float returns a numeric attribute (def when absent or non-numeric).
func (n *TreeNode) Float(name string, def float64) float64 {
	if v, ok := n.Attr(name); ok {
		if f, err := toFloat64(v); err == nil {
			return f
		}
	}
	return def
}

// String returns a string attribute (def when absent).
func (n *TreeNode) String(name, def string) string {
	if v, ok := n.Attr(name); ok {
		return str(v)
	}
	return def
}

// Bool returns a bool attribute (def when absent).
func (n *TreeNode) Bool(name string, def bool) bool {
	if v, ok := n.Attr(name); ok {
		if b, err := toBool(v); err == nil {
			return b
		}
	}
	return def
}

// ParseDocumentTree parses gml source and evaluates every attribute against
// a fresh environment, returning the exported tree. Handler and
// element-valued attributes are skipped (data documents don't carry them).
func ParseDocumentTree(src string) (*TreeNode, error) {
	root, err := Parse(src)
	if err != nil {
		return nil, err
	}
	env := NewEnv()
	return evalTree(root, env)
}

func evalTree(n *node, env *Env) (*TreeNode, error) {
	out := &TreeNode{Type: n.typeName, Line: n.line}
	for _, a := range n.attrs {
		if a.expr == nil {
			continue // handlers / element-valued attrs
		}
		if id, ok := a.expr.(*identExpr); ok && a.name == "id" {
			out.Attrs = append(out.Attrs, TreeAttr{Name: "id", Value: id.name})
			continue
		}
		v, err := a.expr.eval(env)
		if err != nil {
			return nil, fmt.Errorf("gml:%d: %s: %w", a.line, a.name, err)
		}
		out.Attrs = append(out.Attrs, TreeAttr{Name: a.name, Value: v})
	}
	for _, c := range n.children {
		child, err := evalTree(c, env)
		if err != nil {
			return nil, err
		}
		out.Children = append(out.Children, child)
	}
	return out, nil
}
