package gml

// Qt Quick Controls' MenuBar and Menu.
//
// A MenuBar is a row of titles, one per Menu child; clicking a title opens
// that menu below it. A Menu is a popup list of Action / MenuItem entries
// (with MenuSeparator lines and checkable entries), which also opens on its
// own — from a button's `menu.popup()` or a right-click handler.
//
// Which menu is open is engine state keyed to the bar, so a click opens it
// and a second click (or picking an entry) closes it.

import "github.com/sushvibez/gfx/ui"

// buildMenuBar renders a MenuBar: the titles, plus the open menu's popup.
func buildMenuBar(n *node, env *Env, doc *docState) (ui.Element, error) {
	var w, h float32
	grow := false
	for _, a := range n.attrs {
		switch a.name {
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
		default:
			// id, anchors, chrome and the Controls contract: accepted.
		}
	}
	openRef := doc.intProp(doc.key(n)+"|openMenu", -1)
	openIdx := refInt(openRef)

	// Qt's Basic style again (see the tab palette in stack.go): a menu bar
	// is the same near-white as a current tab, with dark titles. Its
	// metrics are Basic's own, measured from Qt: the bar is 40 tall with
	// no padding of its own, and each title is inset 12 left and 16 right
	// at a 12px font.
	bar := ui.Div().Row().ItemsCenter().H(basicMenuBarHeight).Bg(ui.RGB(basicTabCurrent))
	if w > 0 {
		bar.W(w)
	}
	if h > 0 {
		bar.H(h)
	}
	if grow {
		bar.Grow()
	}
	var menus []*node
	for _, c := range n.children {
		if c.typeName != "Menu" {
			if isNonVisual(c) {
				doc.refreshNonVisual(c, env)
			}
			continue
		}
		menus = append(menus, c)
	}
	// Each title's own element, so the open menu can be anchored UNDER it:
	// an overlay's x/y are window coordinates, so a menu positioned from
	// the title's offset within the bar dropped at the window's corner
	// instead of under the title that opened it.
	items := make([]ui.Element, len(menus))
	for i, m := range menus {
		title := menuTitle(m, env)
		idx := i
		item := ui.Div().Row().ItemsCenter().H(basicMenuBarHeight).
			PadXY(14, 6). // Qt insets 12 left / 16 right; the total is what shows

			HoverBg(ui.RGBA{R: 0.85, G: 0.86, B: 0.88, A: 1}).
			Children(ui.Text(title, ui.RGB(0x1a1a1a)).SizePx(basicMenuFontPx))
		if i == openIdx {
			item.Bg(ui.RGBA{R: 0.80, G: 0.82, B: 0.86, A: 1})
		}
		ref := openRef
		item.OnClick(func() {
			if refInt(ref) == idx {
				_ = ref.set(-1)
			} else {
				_ = ref.set(idx)
			}
		})
		bar.Children(item)
		items[i] = item
	}
	// The open menu floats under its title.
	if openIdx >= 0 && openIdx < len(menus) {
		list, err := buildMenuList(menus[openIdx], env, doc, func() { _ = openRef.set(-1) })
		if err != nil {
			return nil, err
		}
		// Anchored to the title, resolved at layout time (so it follows the
		// bar through resizes and reflows) — the overlay is added after the
		// items, which is what AnchorBelow needs.
		// A click anywhere else closes it, and so does Escape (the overlay's
		// dismiss scrim is what both go through) — Qt's menus do both, and
		// ours could only be closed by picking something.
		// No snapping to the window: a menu that does not fit below its
		// title is CLIPPED by the window, as Qt's is here — pulling it up
		// to fit would cover the bar that opened it.
		bar.Children(ui.Overlay(list, 0, 0).AnchorBelow(items[openIdx], 0).
			OnClickOutside(func() { _ = openRef.set(-1) }))
	}
	return bar, nil
}

const (
	// Qt's Basic MenuBar, measured: the bar and its items are 40 tall, the
	// items carry 12/16 horizontal and 6 vertical padding, and their
	// labels are 12px in the default family (Basic does not take the
	// application font).
	basicMenuBarHeight = 40
	basicMenuFontPx    = 12
)

// menuTitle reads a Menu's title.
func menuTitle(n *node, env *Env) string {
	for _, a := range n.attrs {
		if a.name == "title" {
			if v, err := a.expr.eval(env); err == nil {
				return str(v)
			}
		}
	}
	return ""
}

// buildMenuList renders a Menu's entries as a floating list. onPick runs
// after an entry's own handler, to close the menu.
func buildMenuList(n *node, env *Env, doc *docState, onPick func()) (ui.Element, error) {
	list := ui.Div().Col().P(4).Rounded(6).
		Bg(ui.RGBA{R: 0.16, G: 0.17, B: 0.20, A: 1}).
		Border(1, ui.RGBA{R: 0.30, G: 0.31, B: 0.35, A: 1}).
		MinW(160)
	for _, c := range n.children {
		switch c.typeName {
		case "MenuSeparator":
			list.Children(ui.Div().H(1).Bg(ui.RGBA{R: 0.3, G: 0.31, B: 0.35, A: 1}))
			continue
		case "Action", "MenuItem":
		case "Menu":
			// A submenu: shown as its title, not expandable here.
			list.Children(ui.Div().Row().ItemsCenter().PadXY(10, 6).
				Children(ui.Text(menuTitle(c, env)+"  >", ui.RGB(0xdddddd))))
			continue
		default:
			if isNonVisual(c) {
				doc.refreshNonVisual(c, env)
			}
			continue
		}
		text, checkable, checked, enabled, handler := menuEntry(c, env, doc)
		label := text
		if checkable {
			mark := "  "
			if checked {
				mark = "✓ "
			}
			label = mark + text
		}
		color := ui.RGB(0xeeeeee)
		if !enabled {
			color = ui.RGB(0x777777)
		}
		row := ui.Div().Row().ItemsCenter().PadXY(10, 6).Rounded(4).
			Children(ui.Text(label, color))
		if enabled {
			row.HoverBg(ui.RGBA{R: 0.24, G: 0.25, B: 0.30, A: 1})
			run, close := handler, onPick
			row.OnClick(func() {
				if run != nil {
					run()
				}
				if close != nil {
					close()
				}
			})
		}
		list.Children(row)
	}
	return list, nil
}

// menuEntry reads one Action / MenuItem: its text, checkable state and
// trigger handler.
func menuEntry(n *node, env *Env, doc *docState) (text string, checkable, checked, enabled bool, run func()) {
	enabled = true
	var stmts []stmt
	for _, a := range n.attrs {
		switch a.name {
		case "text":
			if v, err := a.expr.eval(env); err == nil {
				text = str(v)
			}
		case "checkable":
			if b, err := attrBool(a, env); err == nil {
				checkable = b
			}
		case "checked":
			if b, err := attrBool(a, env); err == nil {
				checked = b
			}
		case "enabled":
			if b, err := attrBool(a, env); err == nil {
				enabled = b
			}
		case "onTriggered", "onClicked", "onToggled":
			stmts = append(stmts, a.stmt...)
		}
	}
	// A checkable entry owns its state, like the control it stands for.
	if checkable {
		ref := doc.boolProp(doc.key(n)+".checked", checked)
		checked = ref.get() == true
		if len(stmts) > 0 || true {
			runner := doc.handlerRunner(env)
			body := stmts
			run = func() {
				_ = ref.set(!(ref.get() == true))
				if len(body) > 0 {
					runner(body, "onTriggered")
				}
			}
		}
		return text, checkable, checked, enabled, run
	}
	if len(stmts) > 0 {
		runner := doc.handlerRunner(env)
		body := stmts
		run = func() { runner(body, "onTriggered") }
	}
	return text, checkable, checked, enabled, run
}

// buildMenu renders a standalone Menu (not inside a MenuBar): a popup that
// shows while its `visible`/`opened` state is set, which popup() sets.
func buildMenu(n *node, env *Env, doc *docState) (ui.Element, error) {
	openRef := doc.boolProp(doc.key(n)+"|open", false)
	// popup()/open()/close() on the menu's id, so a button can show it.
	doc.exposeIDMethods(n, map[string]any{
		"popup": jsFunc(func([]any) (any, error) { return nil, openRef.set(true) }),
		"open":  jsFunc(func([]any) (any, error) { return nil, openRef.set(true) }),
		"close": jsFunc(func([]any) (any, error) { return nil, openRef.set(false) }),
	})
	doc.exposeIDProp(n, "opened", openRef.get() == true)
	doc.exposeIDProp(n, "visible", openRef.get() == true)
	// A declared `visible:`/`open:` binding still wins.
	for _, a := range n.attrs {
		switch a.name {
		case "visible", "open", "opened":
			if b, err := attrBool(a, env); err == nil && b {
				_ = openRef.set(true)
			}
		}
	}
	if openRef.get() != true {
		return ui.Div(), nil
	}
	list, err := buildMenuList(n, env, doc, func() { _ = openRef.set(false) })
	if err != nil {
		return nil, err
	}
	x, y := menuPos(n, env)
	return ui.Overlay(list, x, y).OnClickOutside(func() { _ = openRef.set(false) }), nil
}

// menuPos reads a Menu's x/y (where popup() placed it) — in its parent
// item's coordinates, as QML states them — and returns the window position.
func menuPos(n *node, env *Env) (float32, float32) {
	x, y := popupOrigin(n, env)
	for _, a := range n.attrs {
		switch a.name {
		case "x":
			if f, err := attrFloat(a, env); err == nil {
				x += f
			}
		case "y":
			if f, err := attrFloat(a, env); err == nil {
				y += f
			}
		}
	}
	return x, y
}
