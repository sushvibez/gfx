package gml

// Qt Quick Controls' MenuBar and Menu, over ui.MenuBar / ui.MenuPanel (the
// Go API for the same).
//
// A MenuBar is a row of titles, one per Menu child; clicking a title opens
// that menu below it. A Menu is a popup list of Action / MenuItem entries
// (with MenuSeparator lines, checkable entries and nested Menus as
// submenus), which also opens on its own — from a button's `menu.popup()`
// or a right-click handler. An Action's `shortcut` shows beside it, and
// inside a MenuBar it triggers the entry from anywhere in the window.

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
	var menus []ui.MenuBarMenu
	for _, c := range n.children {
		if c.typeName != "Menu" {
			if isNonVisual(c) {
				doc.refreshNonVisual(c, env)
			}
			continue
		}
		menus = append(menus, ui.MenuBarMenu{Title: menuTitle(c, env), Items: doc.menuItems(c, env)})
	}
	if doc.menuBars == nil {
		doc.menuBars = map[string]*ui.MenuBarState{}
	}
	key := doc.key(n)
	st := doc.menuBars[key]
	if st == nil {
		st = &ui.MenuBarState{}
		doc.menuBars[key] = st
	}
	bar := ui.MenuBar(st, menus)
	if w > 0 {
		bar.W(w)
	}
	if h > 0 {
		bar.H(h)
	}
	if grow {
		bar.Grow()
	}
	// The entries' shortcuts work from anywhere in the window.
	for _, m := range menus {
		wireShortcuts(bar, m.Items)
	}
	return bar, nil
}

// wireShortcuts registers each entry's shortcut on el, which sits in the
// tree whether the menu is open or not: the shortcut triggers the entry
// from anywhere in the window, as an Action's does in Qt.
func wireShortcuts(el *ui.DivEl, items []ui.MenuItem) {
	for _, it := range items {
		if len(it.Sub) > 0 {
			wireShortcuts(el, it.Sub)
			continue
		}
		if it.Shortcut == "" || it.Disabled || (it.OnTrigger == nil && it.Checked == nil) {
			continue
		}
		el.Shortcut(it.Activate, it.Shortcut)
	}
}

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

// menuItems reads a Menu's entries: Actions and MenuItems, separators,
// and nested Menus as submenus.
func (doc *docState) menuItems(n *node, env *Env) []ui.MenuItem {
	var out []ui.MenuItem
	for _, c := range n.children {
		switch c.typeName {
		case "MenuSeparator":
			out = append(out, ui.MenuItem{Separator: true})
		case "Menu":
			out = append(out, ui.MenuItem{Text: menuTitle(c, env), Sub: doc.menuItems(c, env)})
		case "Action", "MenuItem":
			out = append(out, doc.menuEntry(c, env))
		default:
			if isNonVisual(c) {
				doc.refreshNonVisual(c, env)
			}
		}
	}
	return out
}

// menuEntry reads one Action / MenuItem: its text, shortcut, enabled,
// checkable state (its own, toggled by the menu) and trigger handler.
func (doc *docState) menuEntry(n *node, env *Env) ui.MenuItem {
	var it ui.MenuItem
	var stmts []stmt
	checked := false
	group := ""
	for _, a := range n.attrs {
		switch a.name {
		case "text":
			if v, err := a.expr.eval(env); err == nil {
				it.Text = str(v)
			}
		case "shortcut":
			if v, err := a.expr.eval(env); err == nil {
				if s, ok := v.(string); ok {
					it.Shortcut = s
				} else if cs := keyCombosOf(v); len(cs) > 0 {
					it.Shortcut = shortcutText(cs[0])
				}
			}
		case "checkable":
			if b, err := attrBool(a, env); err == nil {
				it.Checkable = b
			}
		case "checked":
			if b, err := attrBool(a, env); err == nil {
				checked = b
			}
		case "enabled":
			if b, err := attrBool(a, env); err == nil {
				it.Disabled = !b
			}
		case "ActionGroup.group":
			if id, ok := a.expr.(*identExpr); ok {
				group = id.name
			}
		case "onTriggered", "onClicked", "onToggled":
			stmts = append(stmts, a.stmt...)
		}
	}
	// A grouped action is checkable: Qt checks it when it is triggered.
	if group != "" {
		it.Checkable = true
	}
	// A checkable entry owns its state, like the control it stands for.
	if it.Checkable {
		ref := doc.boolProp(doc.key(n)+".checked", checked)
		doc.exposeIDProp(n, "checked", ref)
		if pr, _, ok := propOf[bool](ref); ok {
			it.Checked = pr.p
		}
	}
	if len(stmts) > 0 {
		runner := doc.handlerRunner(env)
		body := stmts
		it.OnTrigger = func() { runner(body, "onTriggered") }
	}
	if group != "" && it.Checked != nil {
		bg := doc.actionGroup(group)
		it.Group = bg.g
		doc.join(bg, n, it.Checked)
		// The group's own onTriggered(action) runs after the action's.
		p, own := it.Checked, it.OnTrigger
		it.OnTrigger = func() {
			if own != nil {
				own()
			}
			doc.groupTriggered(bg, p)
		}
	}
	return it
}

// shortcutText writes a key combination the way a menu shows it.
func shortcutText(c ui.KeyCombo) string {
	s := ""
	if c.Ctrl {
		s += "Ctrl+"
	}
	if c.Alt {
		s += "Alt+"
	}
	if c.Shift {
		s += "Shift+"
	}
	if c.Rune != 0 {
		return s + string(c.Rune)
	}
	for name, k := range map[string]string{"Esc": "Key_Escape", "Del": "Key_Delete", "Ins": "Key_Insert"} {
		if qtCodeKeys[qtKeyCodes[k]] == c.Key {
			return s + name
		}
	}
	for i := 1; i <= 12; i++ {
		if qtCodeKeys[qtKeyCodes["Key_F"+itoa(i)]] == c.Key {
			return s + "F" + itoa(i)
		}
	}
	return s
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return "1" + string(rune('0'+i-10))
}

// buildMenu renders a standalone Menu (not inside a MenuBar): a popup that
// shows while its `visible`/`opened` state is set, which popup() sets.
func buildMenu(n *node, env *Env, doc *docState) (ui.Element, error) {
	openRef := doc.boolProp(doc.key(n)+"|open", false)
	if doc.menus == nil {
		doc.menus = map[string]*ui.MenuState{}
	}
	key := doc.key(n)
	st := doc.menus[key]
	if st == nil {
		st = &ui.MenuState{}
		doc.menus[key] = st
	}
	open := func() (any, error) {
		st.Open() // a fresh highlight and no submenu open
		return nil, openRef.set(true)
	}
	// popup()/open()/close() on the menu's id, so a button can show it.
	doc.exposeIDMethods(n, map[string]any{
		"popup": jsFunc(func([]any) (any, error) { return open() }),
		"open":  jsFunc(func([]any) (any, error) { return open() }),
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
	// The entries are read open or closed: their shortcuts work either way.
	items := doc.menuItems(n, env)
	host := ui.Div()
	wireShortcuts(host, items)
	if openRef.get() != true {
		return host, nil
	}
	closeMenu := func() { _ = openRef.set(false) }
	list := ui.MenuPanel(st, items, closeMenu)
	x, y := menuPos(n, env)
	return host.Children(ui.Overlay(list, x, y).OnClickOutside(closeMenu)), nil
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

// basicMenuBarHeight is Qt's Basic MenuBar height, which ui.MenuBar draws.
const basicMenuBarHeight = 40
