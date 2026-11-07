package gml

import (
	"fmt"
	"strings"

	"github.com/sushvibez/gfx/ui"
)

// QML anchors on the gml side: detect anchored children, parse their
// anchors.* / x / y attributes into a ui.Anchors, and wrap each child in a
// ui.AnchorEl so the parent (in AnchorMode) can position it. Anchor
// references (parent, sibling ids, target.edge) are read structurally from
// the expression AST — they are layout identifiers, not values, so they are
// never evaluated.

// hasAnchorChildren reports whether any direct child uses anchors or manual
// x/y positioning, which turns the container into an anchor layout. Overlay
// children (Popup/Menu/...) don't count: their x/y positions the floating
// panel in window coordinates, not the child in the parent.
func hasAnchorChildren(n *node, doc *docState) bool {
	for _, c := range n.children {
		if isOverlayType(c.typeName) {
			continue
		}
		// A Repeater is not the child that gets positioned — its rows are,
		// and they carry the delegate's x/y (`x: posX * field.width`).
		if c.typeName == "Repeater" {
			if d, err := delegateChild(c); err == nil {
				c = d
			}
		}
		for _, a := range anchorAttrsOf(c, doc) {
			if a.name == "x" || a.name == "y" || strings.HasPrefix(a.name, "anchors.") {
				return true
			}
		}
	}
	return false
}

// isOverlayType reports the floating-overlay elements buildPopup handles.
func isOverlayType(t string) bool {
	switch t {
	case "Popup", "Dialog", "Drawer", "Menu", "ToolTip":
		return true
	}
	return false
}

// anchorAttrsOf returns the attributes that position a child in its parent.
// For a file-component use site (`Panel { }`) those live on the COMPONENT's
// root element (`Item { anchors.fill: parent }`) — the use site itself
// often carries none, and reading only it would lay the instance out in
// flow at its natural (zero) size.
func anchorAttrsOf(c *node, doc *docState) []attr {
	attrs, _ := anchorAttrsOfSrc(c, doc)
	return attrs
}

// anchorAttrsOfSrc is anchorAttrsOf, also reporting whether the attributes
// are the USE SITE's own (true) or a file component's root's (false).
func anchorAttrsOfSrc(c *node, doc *docState) ([]attr, bool) {
	for _, a := range c.attrs {
		if a.name == "x" || a.name == "y" || strings.HasPrefix(a.name, "anchors.") {
			return c.attrs, true
		}
	}
	if doc == nil {
		return c.attrs, true
	}
	if comp := doc.fileComponent(c.typeName); comp != nil {
		for _, a := range comp.attrs {
			// x/y count as much as anchors do: a component whose root
			// places itself (`x: (parent.width - width) / 2`, a parallax
			// layer centring itself on its scene) is positioned by that
			// binding, and reading only its anchors left it in flow at the
			// parent's origin.
			if a.name == "x" || a.name == "y" || strings.HasPrefix(a.name, "anchors.") {
				return comp.attrs, false
			}
		}
	}
	return c.attrs, true
}

// isBareMouseArea reports a MouseArea with no geometry and no children —
// the author means "cover the parent" (QML's anchors.fill: parent idiom,
// commonly left implicit). buildBox hoists its handlers onto the parent
// box; child builders skip it.
func isBareMouseArea(n *node) bool {
	if n.typeName != "MouseArea" || len(n.children) != 0 {
		return false
	}
	for _, a := range n.attrs {
		switch a.name {
		case "x", "y", "width", "height", "implicitWidth", "implicitHeight":
			return false
		}
		if strings.HasPrefix(a.name, "anchors.") {
			return false
		}
	}
	return true
}

// isPointerHandler reports Qt Quick Input Handlers (TapHandler,
// HoverHandler, ...) — non-visual attachments that always cover their
// parent item. buildBox hoists them onto the parent box via
// pointerHandlerAsMouseArea: TapHandler, HoverHandler and DragHandler
// work; the rest attach nothing (pinch, wheel and point gestures are not
// modeled) but must not render as unknown elements.
func isPointerHandler(n *node) bool {
	switch n.typeName {
	case "TapHandler", "HoverHandler", "WheelHandler", "DragHandler",
		"PinchHandler", "PointHandler", "PointerHandler":
		return true
	}
	return false
}

// pointerHandlerAsMouseArea rewrites an Input Handler node as the
// equivalent bare MouseArea so boxAttrs can hoist its handlers onto box,
// the item it sits in. A DragHandler becomes drag.* attributes
// (dragtarget.go), its target defaulting to box itself.
func pointerHandlerAsMouseArea(n, box *node, doc *docState) *node {
	cp := *n
	cp.typeName = "MouseArea"
	cp.attrs = nil
	dragH := n.typeName == "DragHandler"
	if dragH {
		cp.attrs = append(cp.attrs, attr{name: "drag.handler", line: n.line},
			attr{name: "drag.target", line: n.line, expr: &boxObjExpr{box: box, doc: doc}})
	}
	for _, a := range n.attrs {
		if dragH {
			if to, ok := dragHandlerAttrs[a.name]; ok {
				a.name = to // a later drag.target (target:) wins over the default
			}
		}
		switch a.name {
		case "onTapped", "onSingleTapped":
			a.name = "onClicked"
		case "onDoubleTapped":
			a.name = "onDoubleClicked"
		case "onLongPressed":
			a.name = "onPressAndHold"
		}
		cp.attrs = append(cp.attrs, a)
	}
	return &cp
}

// stripStrayAnchors removes any remaining anchors.* attributes from a node
// (idempotent with stripAnchorAttrs — a no-op when there are none).
func stripStrayAnchors(n *node) *node {
	has := false
	for _, a := range n.attrs {
		if strings.HasPrefix(a.name, "anchors.") {
			has = true
			break
		}
	}
	if !has {
		return n
	}
	return stripAnchorAttrs(n)
}

// stripAnchorAttrs returns a copy of n without its anchor/x/y attributes, so
// the element's own builder doesn't see (and reject) them.
func stripAnchorAttrs(n *node) *node {
	strip := false
	for _, a := range n.attrs {
		if a.name == "x" || a.name == "y" || strings.HasPrefix(a.name, "anchors.") {
			strip = true
			break
		}
	}
	if !strip {
		return n // nothing to strip: no copy
	}
	if n.stripped != nil && n.stripFrom == attrsOf(n) {
		return n.stripped // attributes are immutable once parsed: the copy holds
	}
	cp := *n
	cp.attrs = nil
	cp.stripped, cp.ovNode = nil, nil
	for _, a := range n.attrs {
		if a.name == "x" || a.name == "y" || strings.HasPrefix(a.name, "anchors.") {
			continue
		}
		cp.attrs = append(cp.attrs, a)
	}
	n.stripped, n.stripFrom = &cp, attrsOf(n)
	return &cp
}

func edgeByName(s string) (ui.Edge, bool) {
	switch s {
	case "left":
		return ui.EdgeLeft, true
	case "right":
		return ui.EdgeRight, true
	case "top":
		return ui.EdgeTop, true
	case "bottom":
		return ui.EdgeBottom, true
	case "horizontalCenter":
		return ui.EdgeHCenter, true
	case "verticalCenter":
		return ui.EdgeVCenter, true
	}
	return 0, false
}

// anchorItem reads a whole-item anchor target (anchors.fill / centerIn): a
// bare id such as `parent` or `box`.
func anchorItem(e expr) (string, bool) {
	if id, ok := e.(*identExpr); ok {
		return id.name, true
	}
	return "", false
}

// anchorEdge reads a target.edge anchor link, e.g. `parent.left` or
// `box.right`. A conditional picks a branch (QML idiom: `anchors.left:
// sent ? undefined : parent.left`), and `undefined`/`null` means "not
// anchored on this edge" — a nil link with no error.
func anchorEdge(e expr, env *Env) (*ui.AnchorLink, error) {
	for {
		c, ok := e.(*condExpr)
		if !ok {
			break
		}
		v, err := c.c.eval(env)
		if err != nil {
			return nil, err
		}
		if b, _ := toBool(v); b {
			e = c.a
		} else {
			e = c.b
		}
	}
	if id, ok := e.(*identExpr); ok && (id.name == "undefined" || id.name == "null") {
		return nil, nil
	}
	if l, ok := e.(*litExpr); ok && l.v == nil {
		return nil, nil
	}
	m, ok := e.(*memberExpr)
	if !ok {
		return nil, fmt.Errorf("anchor wants target.edge (e.g. parent.left)")
	}
	id, ok := m.base.(*identExpr)
	if !ok {
		return nil, fmt.Errorf("anchor target must be an id")
	}
	ed, ok := edgeByName(m.name)
	if !ok {
		return nil, fmt.Errorf("unknown anchor edge %q", m.name)
	}
	return &ui.AnchorLink{Target: id.name, Edge: ed}, nil
}

// parseAnchors collects a node's anchor spec from its attributes.
func parseAnchors(n *node, env *Env) (ui.Anchors, error) {
	var a ui.Anchors
	for _, at := range n.attrs {
		switch at.name {
		case "anchors.fill":
			id, ok := anchorItem(at.expr)
			if !ok {
				return a, fmt.Errorf("gml:%d: anchors.fill wants an item id", at.line)
			}
			a.Fill = id
		case "anchors.centerIn":
			id, ok := anchorItem(at.expr)
			if !ok {
				return a, fmt.Errorf("gml:%d: anchors.centerIn wants an item id", at.line)
			}
			a.CenterIn = id
		case "anchors.left", "anchors.right", "anchors.top", "anchors.bottom",
			"anchors.horizontalCenter", "anchors.verticalCenter":
			l, err := anchorEdge(at.expr, env)
			if err != nil {
				return a, fmt.Errorf("gml:%d: %v", at.line, err)
			}
			if l == nil {
				continue // `undefined`: this edge is not anchored
			}
			switch at.name {
			case "anchors.left":
				a.Left = l
			case "anchors.right":
				a.Right = l
			case "anchors.top":
				a.Top = l
			case "anchors.bottom":
				a.Bottom = l
			case "anchors.horizontalCenter":
				a.HCenter = l
			case "anchors.verticalCenter":
				a.VCenter = l
			}
		case "anchors.horizontalCenterOffset", "anchors.verticalCenterOffset":
			// A nudge off the centre an item is anchored to; the corpus
			// lifts the portal 40px above its box that way.
			f, err := attrFloat(at, env)
			if err != nil {
				return a, err
			}
			if at.name == "anchors.horizontalCenterOffset" {
				a.HCenterOffset = f
			} else {
				a.VCenterOffset = f
			}
		case "anchors.margins", "anchors.leftMargin", "anchors.rightMargin",
			"anchors.topMargin", "anchors.bottomMargin":
			f, err := attrFloat(at, env)
			if err != nil {
				return a, err
			}
			switch at.name {
			case "anchors.margins":
				a.Margins = f
			case "anchors.leftMargin":
				a.LeftMargin = f
			case "anchors.rightMargin":
				a.RightMargin = f
			case "anchors.topMargin":
				a.TopMargin = f
			case "anchors.bottomMargin":
				a.BottomMargin = f
			}
		case "x":
			f, err := attrFloat(at, env)
			if err != nil {
				return a, err
			}
			a.X, a.HasX = f, true
		case "y":
			f, err := attrFloat(at, env)
			if err != nil {
				return a, err
			}
			a.Y, a.HasY = f, true
		}
	}
	return a, nil
}

// dropXY leaves out the x/y attributes when they belong to a file
// component's root rather than to the use site — those are resolved in the
// component's own scope instead (see buildAnchorChildren).
func dropXY(attrs []attr, keep bool) []attr {
	if keep {
		return attrs
	}
	out := make([]attr, 0, len(attrs))
	for _, a := range attrs {
		if a.name == "x" || a.name == "y" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// buildAnchorChildren builds an anchor container's children, wrapping each in
// a ui.AnchorEl carrying its id and anchor spec.
func buildAnchorChildren(parent *node, env *Env, doc *docState) ([]ui.Element, error) {
	var out []ui.Element
	for _, c := range parent.children {
		switch {
		case isBareMouseArea(c) || isPointerHandler(c):
			// Hoisted onto the parent box by buildBox — not a visual child.
			continue
		case isNonVisual(c):
			doc.refreshNonVisual(c, env)
			continue
		case c.typeName == "Repeater":
			// Every row carries the delegate's OWN x/y and anchors, which
			// are per-row expressions (`x: posX * field.width - width/2`
			// off a model role) — so they are parsed inside the row scope,
			// exactly as a plain child's are. Placing the rows at the
			// origin instead piled the aircraft blueprint's markers in one
			// spot in the corner.
			reps, err := buildRepeaterRows(c, env, doc, func(d *node) (ui.Element, error) {
				return buildAnchoredChild(d, env, doc)
			})
			if err != nil {
				return nil, err
			}
			out = append(out, reps...)
		case isOverlayType(c.typeName):
			// A floating panel positions itself in window coordinates from
			// its own x/y — anchoring it inside the parent would both strip
			// those and give it a flow slot it must not have.
			el, err := doc.slot(env, func() (ui.Element, error) { return build(c, env, doc) })
			if err != nil {
				return nil, err
			}
			out = append(out, el)
		default:
			el, err := doc.slot(env, func() (ui.Element, error) { return buildAnchoredChild(c, env, doc) })
			if err != nil {
				return nil, err
			}
			out = append(out, el)
		}
	}
	return out, nil
}

// buildAnchoredChild builds one child of a non-positioner container and
// wraps it in the anchor slot its own attrs describe (anchors, x/y). It is
// shared by plain children and Repeater rows, so a row's per-row position
// is honoured the way Qt honours it.
func buildAnchoredChild(c *node, env *Env, doc *docState) (ui.Element, error) {
	// A bare `width`/`height` in the child's own x/y binding is the
	// CHILD's (QML item scope): `x: parent.width/2 - width/2`
	// centres the child, not the parent.
	// Overrides first: a child's POSITION is read here, from the
	// child's own attrs, so an x assigned through its id — or
	// animated, a frame at a time — has to be visible to this pass
	// too. Without it the element built with the new x but stayed
	// anchored at the old one, and animating `x` moved nothing.
	c = doc.withAttrOverrides(c)
	attrs, ownXY := anchorAttrsOfSrc(c, doc)
	restoreGeom := shadowSelfGeometry(c, env, doc)
	a, err := parseAnchors(&node{attrs: dropXY(attrs, ownXY), line: c.line}, env)
	restoreGeom()
	if err != nil {
		return nil, err
	}
	// The anchors were just parsed OUT of the child, so record
	// what they impose on it: an item anchored to both sides (or
	// filling) has a width, which is what decides whether a
	// wrapping Text breaks its lines (see textWidthImposed).
	inner := stripAnchorAttrs(c)
	if a.Fill != "" || (a.Left != nil && a.Right != nil) {
		inner.imposedWidth = true
	}
	el, err := build(inner, env, doc)
	if err != nil {
		return nil, err
	}
	// A file component's own x/y were resolved in ITS scope while
	// it built (noteInstanceXY): they are written in terms of the
	// component's declared properties, which do not exist out here.
	if !ownXY {
		if p, ok := doc.instXY[doc.key(c)]; ok {
			a.X, a.HasX = p.x, p.hasX
			a.Y, a.HasY = p.y, p.hasY
		}
	}
	return ui.Anchored(el, nodeID(c), a), nil
}

// dragHandlerAttrs renames a DragHandler's attributes to the drag.* ones
// wireDragTarget reads.
var dragHandlerAttrs = map[string]string{
	"target":               "drag.target",
	"dragThreshold":        "drag.threshold",
	"xAxis.minimum":        "drag.minimumX",
	"xAxis.maximum":        "drag.maximumX",
	"yAxis.minimum":        "drag.minimumY",
	"yAxis.maximum":        "drag.maximumY",
	"xAxis.enabled":        "drag.xEnabled",
	"yAxis.enabled":        "drag.yEnabled",
	"onTranslationChanged": "drag.onTranslationChanged",
	"onActiveChanged":      "drag.onActiveChanged",
}
