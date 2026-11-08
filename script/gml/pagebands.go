package gml

import "github.com/sushvibez/gfx/ui"

// A Page's and an ApplicationWindow's bands: `menuBar` (a window's only),
// `header` and `footer` sit above and below the content, full width, and
// the page's own children lay out in the content area between them — so
// `anchors.fill: parent` fills what the bands leave, as in Qt.

// isBandedType reports the types with header/footer bands.
func isBandedType(t string) bool { return t == "Page" || t == "ApplicationWindow" }

// pageBands are a page's band attributes, in top-to-bottom order.
var pageBands = []string{"menuBar", "header", "footer"}

// buildBanded builds a Page or ApplicationWindow that has bands: a column
// of the bands around the page itself, built without them as the content.
// handled is false for one without bands.
func buildBanded(n *node, env *Env, doc *docState) (el ui.Element, handled bool, err error) {
	bands := map[string]*node{}
	for _, a := range n.attrs {
		for _, b := range pageBands {
			if a.name != b || (b == "menuBar" && n.typeName != "ApplicationWindow") {
				continue
			}
			if a.elem != nil {
				bands[b] = a.elem
			} else if c := doc.componentValue(a.expr, env); c != nil {
				bands[b] = c
			}
		}
	}
	if len(bands) == 0 {
		return nil, false, nil
	}
	// The content: the page itself without its bands or its own size (the
	// column holds that), filling what the bands leave.
	content := *n
	content.attrs = nil
	var sizeAttrs []attr
	for _, a := range n.attrs {
		switch a.name {
		case "menuBar", "header", "footer":
			continue
		case "width", "height", "implicitWidth", "implicitHeight":
			sizeAttrs = append(sizeAttrs, a)
			continue
		}
		content.attrs = append(content.attrs, a)
	}
	inner, err := buildBox(&content, env, doc)
	if err != nil {
		return nil, true, err
	}
	if d, ok := inner.(*ui.DivEl); ok {
		d.Grow()
	}
	col := ui.Div().Col()
	for _, a := range sizeAttrs {
		f, err := attrFloat(a, env)
		if err != nil || f <= 0 {
			continue
		}
		switch a.name {
		case "width", "implicitWidth":
			col.W(f)
		case "height", "implicitHeight":
			col.H(f)
		}
	}
	if len(sizeAttrs) == 0 {
		col.Grow()
	}
	band := func(name string) error {
		bn := bands[name]
		if bn == nil {
			return nil
		}
		b, err := build(bn, env, doc)
		if err != nil {
			return err
		}
		col.Children(b)
		return nil
	}
	for _, name := range []string{"menuBar", "header"} {
		if err := band(name); err != nil {
			return nil, true, err
		}
	}
	col.Children(inner)
	if err := band("footer"); err != nil {
		return nil, true, err
	}
	return col, true, nil
}
