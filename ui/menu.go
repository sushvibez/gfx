package ui

import (
	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// Menus (QML's Menu, MenuItem, MenuSeparator, MenuBar): a MenuPanel is the
// list of a menu's entries, with submenus that open to its right; a MenuBar
// is a row of titles that drops each menu under its title. The keyboard
// drives an open menu: Up/Down move the highlight, Enter triggers it,
// Right opens a submenu and Left closes it, Escape closes the menu. State
// lives in properties, so opening a menu redraws whatever caches the tree.

// MenuItem is one entry of a menu.
type MenuItem struct {
	Text      string
	OnTrigger func()
	// Checkable entries show a check mark and toggle Checked when picked.
	Checkable bool
	Checked   *core.Property[bool]
	Disabled  bool
	Separator bool       // a dividing line; the other fields are unused
	Sub       []MenuItem // a submenu, opened to the right
	Shortcut  string     // shown on the right ("Ctrl+S"); see DivEl.Shortcut to make it work
	// Global marks a Shortcut meant to work from anywhere in the window (an
	// Action's, in QML): a host wires such entries with DivEl.Shortcut, and
	// leaves the rest as text, which their own key handling backs.
	Global bool
	// Group makes checkable entries exclusive (QML's ActionGroup): picking
	// one checks it and unchecks the others, and a checked one stays
	// checked. The entry joins the group as the menu is built.
	Group *ButtonGroup
}

// MenuState is a menu's retained state: open, the highlighted entry, the
// open submenu and their own states. Keep one per menu across frames.
// Activate does what picking the entry does: toggles a checkable entry (or
// checks it through its group) and runs OnTrigger. A host that wires the
// entry's Shortcut calls it.
func (it MenuItem) Activate() {
	if it.Disabled || it.Separator || len(it.Sub) > 0 {
		return
	}
	if it.Checkable && it.Checked != nil {
		if it.Group != nil && it.Group.Exclusive {
			it.Group.Add(it.Checked)
			it.Group.Check(it.Checked)
		} else {
			it.Checked.Set(!it.Checked.Get())
		}
	}
	if it.OnTrigger != nil {
		it.OnTrigger()
	}
}

type MenuState struct {
	open core.Property[bool]
	hl   core.Property[int] // highlighted entry + 1 (0: none)
	sub  core.Property[int] // open submenu entry + 1 (0: none)
	subs map[int]*MenuState
}

// Open, Close and IsOpen drive a standalone menu (a context menu, a menu a
// button pops up). A MenuBar opens its own.
func (m *MenuState) Open()        { m.open.Set(true); m.hl.Set(0); m.sub.Set(0) }
func (m *MenuState) Close()       { m.open.Set(false); m.sub.Set(0) }
func (m *MenuState) IsOpen() bool { return m.open.Get() }

func (m *MenuState) child(i int) *MenuState {
	if m.subs == nil {
		m.subs = map[int]*MenuState{}
	}
	s := m.subs[i]
	if s == nil {
		s = &MenuState{}
		m.subs[i] = s
	}
	return s
}

// MenuPanel is the list of a menu's entries. onClose runs when an entry is
// picked or the menu is dismissed from the keyboard (close the menu there).
func MenuPanel(st *MenuState, items []MenuItem, onClose func()) *DivEl {
	tm := CurrentTheme()
	// An open menu holds the keyboard; its own border stands for the focus
	// (no ring round a menu).
	list := Div().Col().P(4).Rounded(6).Bg(tm.Menu).Border(1, tm.MenuBorder).MinW(160).
		FocusBorder(1, tm.MenuBorder)
	list.Role(RoleMenu, "")
	hl, sub := st.hl.Get()-1, st.sub.Get()-1
	trigger := func(i int) {
		it := items[i]
		if it.Disabled || it.Separator {
			return
		}
		if len(it.Sub) > 0 {
			st.sub.Set(i + 1)
			st.child(i).hl.Set(1)
			return
		}
		it.Activate()
		if onClose != nil {
			onClose()
		}
	}
	rows := make([]*DivEl, len(items))
	for i, it := range items {
		if it.Separator {
			list.Children(Div().H(1).Bg(tm.MenuBorder))
			continue
		}
		c := tm.MenuText
		if it.Disabled {
			c = tm.MenuDim
		}
		label := it.Text
		if it.Checkable {
			mark := "  "
			if it.Group != nil && it.Checked != nil {
				it.Group.Add(it.Checked)
			}
			if it.Checked != nil && it.Checked.Get() {
				mark = "✓ "
				if it.Group != nil && it.Group.Exclusive {
					mark = "• "
				}
			}
			label = mark + label
		}
		row := Div().Row().ItemsCenter().PadXY(10, 6).Rounded(4).Gap(16).
			Children(Text(label, c))
		switch {
		case len(it.Sub) > 0:
			row.Children(Div().Grow(), Text(">", c))
		case it.Shortcut != "":
			row.Children(Div().Grow(), Text(it.Shortcut, tm.MenuDim))
		}
		if i == hl || i == sub {
			row.Bg(tm.MenuHover)
		}
		if !it.Disabled {
			i := i
			row.HoverBg(tm.MenuHover).OnClick(func() {
				if len(items[i].Sub) > 0 && st.sub.Get() == i+1 {
					st.sub.Set(0) // a second click folds the submenu
					return
				}
				trigger(i)
			})
		}
		chk, exp := int8(-1), int8(-1)
		if it.Checkable {
			chk = checkState(it.Checked != nil && it.Checked.Get())
		}
		if len(it.Sub) > 0 {
			exp = checkState(i == sub)
		}
		row.Access(Access{Role: RoleMenuItem, Name: it.Text, Checked: chk, Expanded: exp, Disabled: it.Disabled})
		rows[i] = row
		list.Children(row)
	}
	// The open submenu floats to the right of its entry.
	if sub >= 0 && sub < len(items) && rows[sub] != nil {
		child := st.child(sub)
		panel := MenuPanel(child, items[sub].Sub, onClose)
		list.Children(Overlay(panel, 0, 0).AnchorRight(rows[sub], 6, -4).SnapToWindow(4))
	}
	step := func(dir int) {
		n := len(items)
		if n == 0 {
			return
		}
		i := hl
		for k := 0; k < n; k++ {
			i = (i + dir + n) % n
			if !items[i].Separator && !items[i].Disabled {
				st.hl.Set(i + 1)
				return
			}
		}
	}
	list.OnKeyAccept(func(ev app.KeyEvent) bool {
		if !ev.Down {
			return false
		}
		switch ev.Key {
		case app.KeyDown:
			step(1)
		case app.KeyUp:
			step(-1)
		case app.KeyEnter:
			if hl >= 0 && hl < len(items) {
				trigger(hl)
			}
		case app.KeyRight:
			if hl >= 0 && hl < len(items) && len(items[hl].Sub) > 0 {
				trigger(hl)
			}
		case app.KeyLeft:
			st.sub.Set(0)
		case app.KeyEscape:
			if st.sub.Get() != 0 {
				st.sub.Set(0)
			} else if onClose != nil {
				onClose()
			}
		default:
			return false
		}
		return true
	})
	// An open submenu takes the keyboard; otherwise this menu does.
	if sub < 0 {
		list.RequestFocus()
	}
	return list
}

// MenuBarMenu is one menu of a MenuBar.
type MenuBarMenu struct {
	Title string
	Items []MenuItem
}

// MenuBarState is a menu bar's retained state: which menu is open and each
// menu's own state.
type MenuBarState struct {
	open  core.Property[int] // open menu + 1 (0: none)
	menus map[int]*MenuState
}

// OpenMenu is the index of the open menu, or -1.
func (b *MenuBarState) OpenMenu() int { return b.open.Get() - 1 }

// Open opens menu i (as a click on its title does) and Close closes it.
func (b *MenuBarState) Open(i int) { b.open.Set(i + 1); b.menu(i).Open() }
func (b *MenuBarState) Close()     { b.open.Set(0) }

func (b *MenuBarState) menu(i int) *MenuState {
	if b.menus == nil {
		b.menus = map[int]*MenuState{}
	}
	m := b.menus[i]
	if m == nil {
		m = &MenuState{}
		b.menus[i] = m
	}
	return m
}

// Menu bar look (Qt's Basic style): near-white, dark titles, 40 tall,
// 12 px labels.
const (
	menuBarHeight = 40
	menuBarFontPx = 12
)

// MenuBar is a row of menu titles; clicking one drops its menu under it,
// clicking it again or anywhere else closes it.
func MenuBar(st *MenuBarState, menus []MenuBarMenu) *DivEl {
	tm := CurrentTheme()
	bar := Div().Row().ItemsCenter().H(menuBarHeight).Bg(tm.Band)
	bar.Role(RoleMenuBar, "")
	open := st.OpenMenu()
	titles := make([]*DivEl, len(menus))
	for i, m := range menus {
		i := i
		t := Div().Row().ItemsCenter().H(menuBarHeight).PadXY(14, 6).
			HoverBg(tm.BandHover).
			Children(Text(m.Title, tm.BandText).SizePx(menuBarFontPx))
		if i == open {
			t.Bg(tm.BandOpen)
		}
		t.OnClick(func() {
			if st.OpenMenu() == i {
				st.open.Set(0)
				return
			}
			st.open.Set(i + 1)
			st.menu(i).Open()
		})
		t.Access(Access{Role: RoleMenuItem, Name: m.Title, Checked: -1, Expanded: checkState(i == open)})
		titles[i] = t
		bar.Children(t)
	}
	if open >= 0 && open < len(menus) {
		closeAll := func() { st.open.Set(0) }
		panel := MenuPanel(st.menu(open), menus[open].Items, closeAll)
		bar.Children(Overlay(panel, 0, 0).AnchorBelow(titles[open], 0).OnClickOutside(closeAll))
	}
	return bar
}
