//go:build linux || windows || js

// Controls is the controls kit on one page, written against the Go API
// alone (no gml): buttons, toggles, values, navigation, a split view, and
// the popups — menus, a dialog, a drawer, a tool tip — in the theme the
// switch in the tool bar picks.
//
//	go run ./controls
//	go run ./controls -theme dark
//	go run ./controls -theme light -screenshot out.png   # headless
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

const W, H = 1460, 760

type state struct {
	dark core.Property[bool]

	bold, wrap, notify, sw core.Property[bool]
	tri                    core.Property[int]
	small, medium, large   core.Property[bool]
	sizes                  *ui.ButtonGroup

	volume, lo, hi, knob core.Property[float32]
	count, combo         core.Property[int]
	comboOpen            core.Property[bool]
	name                 *ui.InputState
	busy, sweep          ui.Activity
	hold                 ui.DelayState
	held                 core.Property[int]

	tab, page, hour core.Property[int]
	wheel           ui.TumblerState
	split           ui.SplitState

	table ui.TableState
	tree  ui.TreeState

	bar     ui.MenuBarState
	ctx     ui.MenuState
	align   *ui.ButtonGroup
	left    core.Property[bool]
	right   core.Property[bool]
	dialog  core.Property[bool]
	drawer  ui.DrawerState
	tip     ui.ToolTipState
	lastAct core.Property[string]
}

func newState(dark bool) *state {
	s := &state{name: ui.NewInput("gopher"), sizes: ui.NewButtonGroup(), align: ui.NewButtonGroup()}
	s.dark.Set(dark)
	s.wrap.Set(true)
	s.sw.Set(true)
	s.tri.Set(ui.PartiallyChecked)
	s.medium.Set(true)
	s.volume.Set(0.6)
	s.lo.Set(0.2)
	s.hi.Set(0.7)
	s.knob.Set(0.35)
	s.count.Set(3)
	s.combo.Set(1)
	s.page.Set(1)
	s.hour.Set(9)
	s.left.Set(true)
	s.lastAct.Set("nothing yet")
	s.tree.SetExpanded("0", true)
	s.table.Select(1)
	s.table.SortBy(2, false) // by age
	return s
}

func (s *state) theme() ui.Theme {
	if s.dark.Get() {
		return ui.DarkTheme()
	}
	return ui.LightTheme()
}

func (s *state) view() ui.Element {
	ui.SetTheme(s.theme())
	tm := ui.CurrentTheme()
	did := func(what string) func() { return func() { s.lastAct.Set(what) } }

	menus := ui.MenuBar(&s.bar, []ui.MenuBarMenu{
		{Title: "File", Items: []ui.MenuItem{
			{Text: "New", Shortcut: "Ctrl+N", OnTrigger: did("File > New")},
			{Text: "Open…", Shortcut: "Ctrl+O", OnTrigger: did("File > Open")},
			{Separator: true},
			{Text: "Export", Sub: []ui.MenuItem{
				{Text: "PNG", OnTrigger: did("Export PNG")},
				{Text: "PDF", OnTrigger: did("Export PDF")},
			}},
			{Separator: true},
			{Text: "Quit", Disabled: true},
		}},
		{Title: "View", Items: []ui.MenuItem{
			{Text: "Word wrap", Checkable: true, Checked: &s.wrap},
			{Separator: true},
			{Text: "Align left", Checkable: true, Checked: &s.left, Group: s.align},
			{Text: "Align right", Checkable: true, Checked: &s.right, Group: s.align},
		}},
	}).Grow()

	themeSwitch := ui.Switch(&s.dark, "Dark")
	toolbar := ui.ToolBar(
		ui.ToolButton("Bold", ui.ButtonOpts{Checked: &s.bold}),
		ui.ToolButton("Notify", ui.ButtonOpts{Checked: &s.notify}),
		ui.Separator(true),
		ui.ToolButton("Drawer", ui.ButtonOpts{OnClick: s.drawer.Toggle}),
		ui.ToolButton("Dialog", ui.ButtonOpts{OnClick: func() { s.dialog.Set(true) }}),
		ui.Div().Grow(),
		ui.Text("Last: "+s.lastAct.Get(), tm.Text),
		ui.Div().W(16),
		themeSwitch,
	)

	label := func(t string) ui.Element { return ui.Text(t, tm.Text) }

	buttons := ui.GroupBox("Buttons",
		ui.Div().Row().Gap(8).ItemsCenter().Children(
			ui.Button("Button", ui.ButtonOpts{OnClick: did("Button")}),
			ui.Button("Highlighted", ui.ButtonOpts{Highlighted: true, Width: 110}),
			ui.Button("Flat", ui.ButtonOpts{Flat: true}),
			ui.Button("Disabled", ui.ButtonOpts{Disabled: true}),
		),
		ui.Div().Row().Gap(8).ItemsCenter().Children(
			ui.RoundButton("+", ui.ButtonOpts{OnClick: func() { s.count.Set(s.count.Get() + 1) }}),
			s.tip.Attach(ui.Button("Hover me", ui.ButtonOpts{Width: 110}), "A tool tip, after half a second",
				ui.ToolTipOpts{Delay: 500 * time.Millisecond}),
			ui.DelayButton("Hold", &s.hold, ui.DelayOpts{Delay: 800 * time.Millisecond,
				OnActivated: func() { s.held.Set(s.held.Get() + 1) }}),
			label(fmt.Sprintf("held %d×", s.held.Get())),
		),
	)

	toggles := ui.GroupBox("Toggles",
		ui.Switch(&s.sw, "Switch"),
		ui.Checkbox(&s.wrap, "Word wrap"),
		ui.TriCheckbox(&s.tri, "Partly"),
		ui.Div().Row().Gap(14).Children(
			ui.Radio(&s.small, "Small", s.sizes),
			ui.Radio(&s.medium, "Medium", s.sizes),
			ui.Radio(&s.large, "Large", s.sizes),
		),
	)

	values := ui.GroupBox("Values",
		ui.Div().Row().Gap(12).ItemsCenter().Children(label("Slider"), ui.Slider(&s.volume, 200)),
		ui.Div().Row().Gap(12).ItemsCenter().Children(label("Range"), ui.RangeSlider(&s.lo, &s.hi, 200)),
		ui.Div().Row().Gap(16).ItemsCenter().Children(
			ui.DialControl(&s.knob, 0, 1, ui.DialOpts{Size: 64}),
			ui.SpinBox(&s.count, ui.SpinOpts{From: 0, To: 20}),
			ui.ComboBox(&s.combo, &s.comboOpen, ui.ComboOpts{Items: []string{"Red", "Green", "Blue"}}),
		),
		ui.Input(s.name, 260),
		ui.Div().Row().Gap(12).ItemsCenter().Children(
			ui.ProgressBar(s.volume.Get(), ui.ProgressOpts{Width: 160}),
			ui.ProgressBar(0, ui.ProgressOpts{Width: 120, Indeterminate: &s.sweep}),
			ui.BusyIndicator(&s.busy, 28),
		),
	)
	s.busy.Run(true)

	pages := []string{"Overview", "Details", "History"}
	nav := ui.GroupBox("Navigation",
		ui.TabBar(s.tab.Get(), []ui.Tab{{Label: pages[0]}, {Label: pages[1]}, {Label: pages[2]}}, s.tab.Set).W(320),
		ui.Div().H(60).P(10).Children(label(pages[s.tab.Get()]+" page")),
		ui.Div().Row().Gap(24).ItemsCenter().Children(
			ui.PageIndicator(5, s.page.Get(), s.page.Set, ui.PageIndicatorOpts{}),
			ui.Tumbler(&s.wheel, s.hour.Get(), s.hour.Set, ui.TumblerOpts{Count: 24, Visible: 3, Wrap: true, W: 56, H: 96},
				func(i int, d float32) ui.Element {
					c := tm.Text
					if d != 0 {
						c.A = 0.4
					}
					return ui.Text(fmt.Sprintf("%02d", i), c)
				}),
			label("o'clock"),
		),
	)

	pane := func(t string) *ui.DivEl {
		return ui.Div().P(10).Bg(tm.Field).Children(label(t))
	}
	split := ui.GroupBox("Split view",
		ui.SplitView(&s.split, []ui.SplitPane{
			{El: pane("Files"), Preferred: 140, Minimum: 80},
			{El: pane("Editor (fills)"), Fill: true, Minimum: 120},
			{El: pane("Outline"), Preferred: 120, Minimum: 60},
		}, ui.SplitOpts{}).Size(480, 110),
	)

	people := [][]string{{"Ada", "Engineer", "36"}, {"Grace", "Admiral", "41"}, {"Linus", "Maintainer", "29"},
		{"Margaret", "Director", "33"}, {"Ken", "Researcher", "52"}}
	widths := []float32{110, 120, 60}
	data := ui.GroupBox("Data",
		// Click a title to sort, drag its edge to resize, drag it to move the
		// column; arrows move the selection while the table has focus.
		ui.TableView(&s.table, ui.TableOpts{Rows: len(people), Cols: 3, ColWidths: widths, H: 200, Selectable: true,
			Sortable: true, Resizable: true, Reorderable: true,
			Title: func(c int) string { return []string{"Name", "Role", "Age"}[c] },
			Text:  func(r, c int) string { return people[r][c] }}),
		ui.Div().W(290).Children(ui.TreeView(&s.tree, []ui.TreeNode{
			{Label: "src", Children: []ui.TreeNode{
				{Label: "main.go"},
				{Label: "ui", Children: []ui.TreeNode{{Label: "div.go"}, {Label: "theme.go"}}},
			}},
			{Label: "docs", Children: []ui.TreeNode{{Label: "ui.md"}}},
			{Label: "README.md"},
		}, ui.TreeOpts{H: 170})),
	)

	page := ui.Page(
		ui.Div().Col().Children(menus, toolbar),
		ui.Div().Row().P(16).Gap(16).Children(
			ui.Div().Col().Gap(16).Children(buttons, toggles, split),
			ui.Div().Col().Gap(16).Children(values, nav),
			ui.Div().Col().Gap(16).Children(data),
		),
		nil,
	).Bg(tm.Window).Grow()

	dialog := ui.Dialog(&s.dialog, ui.Div().P(20).Children(label("Save the changes to “notes.txt”?")),
		ui.DialogOpts{Title: "Save", Buttons: ui.ButtonSave | ui.ButtonDiscard | ui.ButtonCancel,
			OnAccepted: did("Dialog: saved"), OnRejected: did("Dialog: not saved")})
	drawer := ui.Drawer(&s.drawer, ui.Div().Col().P(16).Gap(10).Children(
		ui.Text("Drawer", tm.Text).Bold(),
		ui.Button("Close", ui.ButtonOpts{OnClick: s.drawer.Close}),
	), ui.DrawerOpts{Size: 260, DragMargin: 12})
	return ui.Div().Col().Children(page, dialog, drawer)
}

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	theme := flag.String("theme", "light", "light or dark")
	open := flag.String("open", "", "headless: open a popup first (menu, dialog, drawer)")
	flag.Parse()

	s := newState(*theme == "dark")
	switch *open {
	case "menu":
		s.bar.Open(0)
	case "dialog":
		s.dialog.Set(true)
	case "drawer":
		s.drawer.SetPosition(1)
	}
	opts := shell.Options{Title: "graphics: controls", Width: W, Height: H, MaxFrames: *frames}
	var err error
	if *shot != "" {
		err = screenshot(opts, s, *shot)
	} else {
		err = shell.RunUI(opts, s.view)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "controls:", err)
		os.Exit(1)
	}
}
