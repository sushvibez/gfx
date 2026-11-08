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
