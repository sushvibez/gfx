package gml

import (
	"github.com/sushvibez/gfx/ui"
)

// A styled POPUP: a Dialog, Popup, Menu or ToolTip whose looks come from the
// application's own style file rather than from us.
//
// The style file states the panel — `background:`, and for a Dialog the
// `header:` and `footer:` bands — and the page states the content (a
// Dialog's children go in its contentItem, a Menu's children ARE its
// items). This composes those into the floating panel and hands it to the
// popup's own state (popupState), which owns whether it is open, the
// signals it emits and where it sits.
//
// Without this, the styles were dead files: every dialog in the corpus drew
// our own chrome — a light card and a Basic button box over a dark page —
// while Qt drew the app's dark card with its teal title.

// isPopupKind reports the T.* kinds that FLOAT rather than sit in the flow.
func isPopupKind(kind string) bool {
	switch kind {
	case "Popup", "Dialog", "Menu", "ToolTip", "Drawer":
		return true
	}
	return false
}

// templatePopupParts are the delegates a style file gave the popup.
type templatePopupParts struct {
	background, contentItem *node
	header, footer          *node
	overlayModal            *node // Overlay.modal: the scrim behind a modal
	delegate                *node // a Menu's item delegate
	w, h                    float32
	padL, padR, padT, padB  float32
}

// buildTemplatePopup composes a styled popup and floats it.
func buildTemplatePopup(kind string, n *node, env *Env, doc *docState,
	scope *elemObject, parts templatePopupParts) (ui.Element, error) {
	st := popupStateOf(n, env, doc)
	// The style reads its own state while its delegates build
	// (`root.title`, `root.opened`, a Menu's `root.currentIndex`).
	if scope != nil {
		if scope.props == nil {
			scope.props = map[string]any{}
		}
		// A ComboBox's contract has a `popup` sub-object, which every
		// control's scope is seeded with — on a POPUP that name is the
		// method that opens it, and the prop shadowed it (props are
		// consulted first), so `viewMenu.popup()` called a map.
		delete(scope.props, "popup")
		scope.setProp("opened", st.shown)
		scope.setProp("visible", st.shown)
		if st.title != "" {
			scope.setProp("title", st.title)
		}
		// A ToolTip's style reads `root.text` for its label.
		if txt := popupText(n, env); txt != "" {
			scope.setProp("text", txt)
		}
	}
	if !st.shown {
		return ui.Div(), nil // closed: no flow space, nothing floating
	}
	// The panel's own size: what the style states (implicitWidth/Height),
	// which for the corpus' Dialog is 400 wide and content-tall.
	panel := ui.Div().Col()
	if parts.w > 0 {
		panel.W(parts.w)
	}
	if parts.h > 0 && kind != "Dialog" && kind != "Menu" {
		// A Dialog's implicitHeight is its content plus its bands, and a
		// Menu's is its items' (through a ListView whose contentHeight the
		// style's expression cannot see) — both are measured by laying the
		// content out rather than by evaluating the style's own expression.
		// Pinned to the expression, a menu was as tall as its padding and
		// its rows spilled out below the card.
		panel.H(parts.h)
	}

	// Background: the style's own card, behind everything.
	var bg ui.Element
	if parts.background != nil {
		el, err := build(stripAnchorAttrs(parts.background), env, doc)
		if err != nil {
			logf("gml: popup background: %v", err)
		} else {
			bg = el
		}
	}

	// Header, content and footer stack down the panel.
	band := func(delegate *node, what string) ui.Element {
		if delegate == nil {
			return nil
		}
		el, err := build(stripAnchorAttrs(delegate), env, doc)
		if err != nil {
			logf("gml: popup %s: %v", what, err)
			return nil
		}
		// A band states its own height as an IMPLICIT one (`implicitHeight:
		// Style.resize(48)` on a Dialog's header): the popup gives it
		// exactly that, since nothing else in the stack would.
		if d, ok := el.(*ui.DivEl); ok {
			if hh := attrFloatOr(delegate, "implicitHeight", env, 0); hh > 0 {
				d.H(hh)
			}
		}
		return el
	}
	if hel := band(parts.header, "header"); hel != nil {
		if d, ok := hel.(*ui.DivEl); ok {
			d.CrossFill()
		}
		panel.Children(hel)
	}
	content, err := popupContent(kind, n, env, doc, parts)
	if err != nil {
		return nil, err
	}
	if content != nil {
		// The content sits inside the style's padding, and FILLS what is
		// left of the panel: a popup's own ColumnLayout pushes its button
		// to the bottom with `Item { Layout.fillHeight: true }`, which only
		// works if the layout itself has the height to give away.
		if d, ok := content.(*ui.DivEl); ok {
			d.Grow().CrossFill()
		}
		pad := ui.Div().Col().Padding(parts.padT, parts.padR, parts.padB, parts.padL).
			CrossFill().Grow().Children(content)
		panel.Children(pad)
	}
	if fel := popupFooter(kind, n, env, doc, parts, st); fel != nil {
		if d, ok := fel.(*ui.DivEl); ok {
			d.CrossFill()
		}
		panel.Children(fel)
	}

	// The background goes UNDER the bands, sized to the whole panel: the
	// stack takes its size from the panel (AnchorFit) and the background
	// fills whatever that turns out to be.
	var out ui.Element = panel
	if bg != nil {
		stack := ui.Div().AnchorMode().AnchorFit()
		if parts.w > 0 {
			stack.W(parts.w)
		}
		stack.Children(
			ui.Anchored(bg, "", ui.Anchors{Fill: "parent"}),
			ui.Anchored(panel, "", ui.Anchors{Fill: "parent"}),
		)
		out = stack
	}
	// The scrim: the style's `Overlay.modal` rectangle states its colour,
	// which is how a dark app dims its page (Qt.rgba(0,0,0,0.4)).
	dim := float32(0.45)
	if parts.overlayModal != nil {
		if c, ok := overlayScrimAlpha(parts.overlayModal, env); ok {
			dim = c
		}
	}
	if kind == "ToolTip" { // a tooltip never dims and takes no clicks
		return ui.PositionedModal(out, st.px, st.py, ui.RGBA{}, nil), nil
	}
	return st.float(out, dim), nil
}

// popupContent is what goes between a styled popup's bands: the use site's
// children (a Dialog's content, a Menu's items), or the style's own
// contentItem when it has one (a ToolTip's label).
func popupContent(kind string, n *node, env *Env, doc *docState,
	parts templatePopupParts) (ui.Element, error) {
	if kind == "Menu" {
		return styledMenuList(n, env, doc, parts)
	}
	var kids []ui.Element
	for _, c := range n.children {
		if isNonVisual(c) {
			doc.refreshNonVisual(c, env)
			continue
		}
		el, err := build(c, env, doc)
		if err != nil {
			if doc.lenient {
				logf("gml: popup content: %v (skipped)", err)
				continue
			}
			return nil, err
		}
		kids = append(kids, el)
	}
	if len(kids) == 0 {
		// No children of its own: the style's contentItem is the content (a
		// ToolTip's text, a Popup whose style fills itself).
		if parts.contentItem != nil {
			el, err := build(stripAnchorAttrs(parts.contentItem), env, doc)
			if err != nil {
				logf("gml: popup contentItem: %v", err)
				return nil, nil
			}
			return el, nil
		}
		return nil, nil
	}
	if len(kids) == 1 {
		return kids[0], nil
	}
	return ui.Div().Col().Children(kids...), nil
}

// popupFooter is a Dialog's button box. The corpus' style states
// `footer: DialogButtonBox { visible: count > 0 }` — a type with no style
// file of its own, so Qt draws Basic's, and so do we; the buttons come from
// the dialog's `standardButtons`.
func popupFooter(kind string, n *node, env *Env, doc *docState,
	parts templatePopupParts, st *popupState) ui.Element {
	if kind != "Dialog" {
		return nil
	}
	if parts.footer != nil && parts.footer.typeName != "DialogButtonBox" {
		if el, err := build(stripAnchorAttrs(parts.footer), env, doc); err == nil {
			return el
		}
	}
	if st.buttons == 0 {
		return nil // `visible: count > 0`
	}
	o := st.dialogOpts(false)
	close := st.closeFn()
	return ui.DialogButtonBox(o.Buttons, func(b ui.StandardButton) {
		close()
		if b.Accepts() {
			o.OnAccepted()
		} else {
			o.OnRejected()
		}
		o.OnClosed()
	})
}

// overlayScrimAlpha reads the alpha of an `Overlay.modal:` rectangle's
// colour — how much the style dims the page behind a modal.
func overlayScrimAlpha(n *node, env *Env) (float32, bool) {
	for _, a := range n.attrs {
		if a.name != "color" {
			continue
		}
		if c, err := attrColor(a, env); err == nil {
			return c.A, true
		}
	}
	return 0, false
}

// styledMenuList builds a Menu's items through the app's own MenuItem
// style. A Menu's children ARE its items (Qt puts them in its contentModel,
// which the style's ListView walks); an `Action` among them is the same
// thing written shorter, so it becomes a MenuItem node here.
//
// Whatever an item's own handler does, choosing it also closes the menu —
// that is the Menu's doing, not the item's.
func styledMenuList(n *node, env *Env, doc *docState, parts templatePopupParts) (ui.Element, error) {
	st := popupStateOf(n, env, doc)
	list := ui.Div().Col()
	for _, c := range n.children {
		if isNonVisual(c) {
			doc.refreshNonVisual(c, env)
			continue
		}
		item := c
		switch c.typeName {
		case "MenuSeparator", "ToolSeparator":
			// A hairline the width of the menu.
			list.Children(ui.Div().H(9).CrossFill().Children(
				ui.Div().H(1).CrossFill().Bg(ui.RGBA{R: 1, G: 1, B: 1, A: 0.12})))
			continue
		case "Action":
			item = actionAsMenuItem(c)
		case "Menu":
			// A submenu: its title is the row, and opening it is not
			// modelled here — draw the row so the menu keeps its shape.
			item = &node{typeName: "MenuItem", line: c.line,
				attrs: []attr{{name: "text", expr: &litExpr{v: menuTitle(c, env)}, line: c.line}}}
		}
		el, err := build(item, env, doc)
		if err != nil {
			if doc.lenient {
				logf("gml: menu item: %v (skipped)", err)
				continue
			}
			return nil, err
		}
		if d, ok := el.(*ui.DivEl); ok {
			d.CrossFill().AlsoOnClick(st.dismiss)
		}
		list.Children(el)
	}
	return list, nil
}

// actionAsMenuItem rewrites an `Action { text; onTriggered }` as the
// MenuItem it stands for, so it draws through the same style.
func actionAsMenuItem(a *node) *node {
	cp := *a
	cp.typeName = "MenuItem"
	return &cp
}
