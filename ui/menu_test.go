package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestMenuBarAndKeys(t *testing.T) {
	var bar MenuBarState
	var wrap core.Property[bool]
	opened, exported := 0, ""
	menus := []MenuBarMenu{
		{Title: "File", Items: []MenuItem{
			{Text: "Open", OnTrigger: func() { opened++ }},
			{Separator: true},
			{Text: "Export", Sub: []MenuItem{
				{Text: "PNG", OnTrigger: func() { exported = "png" }},
				{Text: "SVG", OnTrigger: func() { exported = "svg" }},
			}},
		}},
		{Title: "View", Items: []MenuItem{{Text: "Wrap", Checkable: true, Checked: &wrap}}},
	}
	var tree Element
	u := New(nil, nil, func() Element { tree = Div().Col().Children(MenuBar(&bar, menus)); return tree })
	frame := func() { u.Layout(400, 300) }
	frame()
	clickText := func(s string) {
		frame()
		var r Rect
		// Headless text has no width (no font): click the row holding it.
		var walk func(Element)
		walk = func(e Element) {
			if e == nil || r.W > 0 {
				return
			}
			for _, c := range Children(e) {
				if txt, ok := TextOf(c); ok && (txt == s || txt == "  "+s || txt == "✓ "+s) {
					r = Bounds(e)
					return
				}
			}
			for _, c := range Children(e) {
				walk(c)
			}
		}
		walk(tree)
		for _, o := range u.ctx.overlays {
			walk(o)
		}
		if r.W == 0 {
			t.Fatalf("no %q on screen", s)
		}
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: r.X + 2, Y: r.Y + 2}, {Kind: app.MouseDown, X: r.X + 2, Y: r.Y + 2, Button: 1}, {Kind: app.MouseUp, X: r.X + 2, Y: r.Y + 2, Button: 1}})
		frame()
	}
	clickText("File")
	if bar.OpenMenu() != 0 {
		t.Fatalf("File did not open: %d", bar.OpenMenu())
	}
	clickText("Open")
	if opened != 1 || bar.OpenMenu() != -1 {
		t.Errorf("picked Open: opened %d, menu %d; want 1, closed", opened, bar.OpenMenu())
	}
	clickText("View")
	clickText("Wrap")
	if !wrap.Get() {
		t.Error("the checkable Wrap did not check")
	}
	// Keyboard: open File, Down to Open, Down past the separator to Export,
	// Right into the submenu, Down to SVG, Enter.
	clickText("File")
	key := func(k app.Key) { u.Key(app.KeyEvent{Down: true, Key: k}); frame() }
	key(app.KeyDown)
	key(app.KeyDown)
	key(app.KeyRight)
	key(app.KeyDown)
	key(app.KeyEnter)
	if exported != "svg" || bar.OpenMenu() != -1 {
		t.Errorf("keyboard Export > SVG: exported %q, menu %d", exported, bar.OpenMenu())
	}
	clickText("File")
	key(app.KeyEscape)
	if bar.OpenMenu() != -1 {
		t.Errorf("Escape left the menu open")
	}
}

func TestMenuActionGroup(t *testing.T) {
	g := NewButtonGroup()
	var a, b core.Property[bool]
	a.Set(true)
	items := []MenuItem{
		{Text: "Left", Checkable: true, Checked: &a, Group: g},
		{Text: "Right", Checkable: true, Checked: &b, Group: g},
	}
	MenuPanel(&MenuState{}, items, nil) // the entries join their group as the menu is built
	items[1].Activate()
	if a.Get() || !b.Get() {
		t.Errorf("picking Right: left %v right %v; want only right", a.Get(), b.Get())
	}
	items[1].Activate() // a checked member of an exclusive group stays checked
	if !b.Get() {
		t.Error("picking the checked entry again unchecked it")
	}
	g.Exclusive = false
	items[1].Activate()
	if b.Get() {
		t.Error("a non-exclusive group's entry did not toggle")
	}
}

// Hovering an entry highlights it and opens its submenu; hovering another
// moves the highlight and closes the submenu.
func TestMenuHoverOpensSubmenu(t *testing.T) {
	var st MenuState
	st.Open()
	items := []MenuItem{
		{Text: "Shapes", Sub: []MenuItem{{Text: "Box"}}},
		{Text: "Undo"},
	}
	var panel *DivEl
	u := New(nil, nil, func() Element { panel = MenuPanel(&st, items, nil); return Div().Children(panel) })
	u.Layout(400, 300)
	var rows []Rect
	var walk func(Element)
	walk = func(e Element) {
		for _, c := range Children(e) {
			if txt, ok := TextOf(c); ok && (txt == "Shapes" || txt == "Undo") {
				rows = append(rows, Bounds(e))
			}
			walk(c)
		}
	}
	walk(panel)
	if len(rows) != 2 {
		t.Fatalf("rows %v", rows)
	}
	hover := func(r Rect) {
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: r.X + 3, Y: r.Y + r.H/2}})
		u.Layout(400, 300)
		u.Layout(400, 300)
	}
	hover(rows[0])
	if st.sub.Get() != 1 || st.hl.Get() != 1 {
		t.Fatalf("hover on Shapes: sub %d hl %d", st.sub.Get(), st.hl.Get())
	}
	hover(rows[1])
	if st.sub.Get() != 0 || st.hl.Get() != 2 {
		t.Fatalf("hover on Undo: sub %d hl %d", st.sub.Get(), st.hl.Get())
	}
}
