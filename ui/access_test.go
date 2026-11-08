package ui

import (
	"strings"
	"testing"

	"github.com/sushvibez/gfx/core"
)

func TestAccessTree(t *testing.T) {
	var on, wrap core.Property[bool]
	var vol core.Property[float32]
	var tabs int
	var mb MenuBarState
	var tree TreeState
	var table TableState
	name := NewInput("gopher")
	pw := NewInput("secret")
	vol.Set(0.25)
	wrap.Set(true)
	clicks := 0
	tree.SetExpanded("0", true)
	table.Select(1)
	u := New(nil, nil, func() Element {
		return Div().Col().Children(
			MenuBar(&mb, []MenuBarMenu{{Title: "File", Items: []MenuItem{{Text: "Open"}}}}),
			Button("Save", ButtonOpts{OnClick: func() { clicks++ }}),
			Button("Off", ButtonOpts{Disabled: true}),
			Checkbox(&wrap, "Word wrap"),
			Switch(&on, "Sync"),
			Slider(&vol, 100),
			Input(name, 100).Placeholder("Name", RGB(0x888888)),
			Input(pw, 100).Echo('•'),
			TabBar(tabs, []Tab{{Label: "A"}, {Label: "B"}}, func(i int) { tabs = i }),
			ProgressBar(0.6, ProgressOpts{}),
			TableView(&table, TableOpts{Rows: 2, Cols: 1, H: 80, Selectable: true,
				Text: func(r, c int) string { return []string{"x", "y"}[r] }}),
			TreeView(&tree, []TreeNode{{Label: "src", Children: []TreeNode{{Label: "a.go"}}}}, TreeOpts{H: 80}),
			Text("plain words", RGB(0)),
		)
	})
	u.Layout(600, 900)
	u.Layout(600, 900)
	root := u.AccessTree()
	got := root.String()
	for _, want := range []string{
		"window",
		"menubar",
		`menuitem "File" collapsed`,
		`button "Save"`,
		`button "Off" disabled`,
		`checkbox "Word wrap" checked`,
		`switch "Sync" unchecked`,
		"slider value=0.25",
		`textbox "Name" value=gopher`,
		"tablist",
		`tab "A" selected`,
		`tab "B"`,
		"progressbar",
		"value=60%",
		"table",
		`row "y" selected`,
		"tree",
		`treeitem "src" expanded`,
		`treeitem "a.go"`,
		`text "plain words"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tree lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "secret") {
		t.Error("a password field's value was exposed")
	}
	// Activating a node does what clicking it does.
	if !u.AccessActivate(root.Find(RoleButton, "Save")) || clicks != 1 {
		t.Errorf("activating Save: clicks %d, want 1", clicks)
	}
	if !u.AccessActivate(root.Find(RoleCheckBox, "Word wrap")) || wrap.Get() {
		t.Error("activating the check box did not toggle it")
	}
	if RoleByName("PushButton") != RoleButton || RoleByName("checkbox") != RoleCheckBox || RoleByName("nonsense") != RoleNone {
		t.Error("RoleByName")
	}
}
