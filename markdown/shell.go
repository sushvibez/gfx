//go:build linux || windows

package main

// The Obsidian chrome, faithful to screenshots/obsidian.png (light theme):
// icon ribbon, file explorer with the vault tree, tab strip, and the
// Canvas view — dot grid, idea cards, floating tool clusters.

import (
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

var (
	cText    = ui.RGB(0x222226)
	cMuted   = ui.RGB(0x6a6d74)
	cFaint   = ui.RGB(0x9b9ea6)
	cRibbon  = ui.RGB(0xf2f2f4)
	cSidebar = ui.RGB(0xfbfbfc)
	cBorder  = ui.RGB(0xe3e4e8)
	cRowSel  = ui.RGB(0xe9eaee)
	cRowHov  = ui.RGB(0xf0f1f4)
	cWhite   = ui.RGB(0xffffff)
	cDot     = ui.RGB(0xe4e5e9)
	cCardBrd = ui.RGB(0xcfd2d8)
	cBadge   = ui.RGB(0x8a8d94)
)

// treeRow is one file-explorer entry.
type treeRow struct {
	name   string
	indent int
	chev   string // "d" open, "r" closed, "" leaf
	badge  string // "BASE", "CANVAS"
	sel    bool
	active bool // ATX header-style gray pill
}

var vaultTree = []treeRow{
	{name: "ATX", chev: "d", active: true},
	{name: "Hiring", indent: 1},
	{name: "Products", indent: 1},
	{name: "Imagine.BO", chev: "r"},
	{name: "Personal", chev: "d"},
	{name: "Finance", indent: 1},
	{name: "Synergy", chev: "d"},
	{name: "Ideas", indent: 1, chev: "d"},
	{name: "Digital Twin", indent: 2},
	{name: "Quick Commercce", indent: 2},
	{name: "Warehouse", indent: 2},
	{name: "Leads", indent: 1, chev: "r"},
	{name: "zExperiments", chev: "d"},
	{name: "GPUI", indent: 1},
	{name: "Marketing OS", indent: 1},
	{name: "Startup OS", indent: 1},
	{name: "Untitled", indent: 1, badge: "BASE"},
	{name: "Untitled", badge: "CANVAS", sel: true},
}

// card is one canvas card (window coordinates, calibrated to the shot).
type card struct {
	x, y, w, h float32
	text       string
	wrap       bool
}

var canvasCards = []card{
	{351, 250, 223, 51, "Game Like", false},
	{837, 250, 109, 51, "Qt/QML", false},
	{998, 250, 110, 51, "High Topo", false},
	{351, 322, 223, 51, "Minecraft", false},
	{603, 322, 223, 51, "Notes", false},
	{837, 322, 109, 51, "Excavator", false},
	{998, 322, 110, 51, "HVAC", false},
	{351, 394, 223, 51, "3D Map of city", false},
	{603, 394, 223, 51, "", false},
	{998, 394, 110, 51, "Natural Gas", true},
	{351, 465, 223, 51, "Drive in a Real City", false},
	{351, 537, 223, 51, "F Zero style Racer", false},
	{351, 609, 223, 51, "747", false},
}

type shell struct {
	winW, winH float32
	selected   core.Property[int]

	// Window controls (wired by main; nil in headless).
	onMin, onMax, onClose func()
}

// titleDragRect is the empty tab-strip region that drags the window.
func (s *shell) titleDragRect() ui.Rect {
	x := float32(ribbonW + sidebarW + 260)
	return ui.Rect{X: x, Y: 0, W: s.winW - 180 - x, H: topH}
}

func newShell() *shell {
	s := &shell{}
	s.selected.Set(-1)
	return s
}

func icon(name string, size float32, tint ui.RGBA) ui.Element {
	return ui.SVG(icons[name], size, size).Tint(tint)
}

func iconBtn(name string, size float32, tint ui.RGBA) *ui.DivEl {
	return ui.Div().P(5).Rounded(6).HoverBg(cRowHov).Children(icon(name, size, tint))
}

const (
	ribbonW  = 44
	sidebarW = 298
	topH     = 44
	navH     = 31
)

func (s *shell) view() ui.Element {
	root := ui.Div()
	mainX := float32(ribbonW + sidebarW)

	root.Children(
		ui.Overlay(s.canvas(mainX), 0, 0),
		ui.Overlay(s.ribbon(), 0, 0),
		ui.Overlay(s.explorer(), ribbonW, 0),
		ui.Overlay(s.tabBar(mainX), mainX, 0),
		ui.Overlay(s.navRow(mainX), mainX, topH),
	)

	// Floating clusters over the canvas.
	root.Children(ui.Overlay(s.cardToolbar(), mainX+(s.winW-mainX)/2-66, s.winH-58))
	root.Children(ui.Overlay(s.rightButtons(), s.winW-42, topH+52))
	root.Children(ui.Overlay(
		ui.Div().PadXY(9, 5).Bg(cWhite).Rounded(6).Border(1, cBorder).
			Children(ui.Text("0 backlinks", cMuted).SizePx(11)),
		s.winW-90, s.winH-30))
	return root
}

// ribbon is the far-left vertical icon strip.
func (s *shell) ribbon() ui.Element {
	r := ui.Div().Col().W(ribbonW).H(s.winH).Bg(cRibbon).BorderRight(1, cBorder).
		ItemsCenter().PadXY(0, 8).Gap(4)
	r.Children(iconBtn("panelL", 16, cMuted), ui.Div().H(18))
	for _, n := range []string{"filesrch", "graph", "grid", "terminal", "binary", "listtree"} {
		r.Children(iconBtn(n, 16, cMuted))
	}
	return r
}

// explorer is the file tree panel.
func (s *shell) explorer() ui.Element {
	p := ui.Div().Col().W(sidebarW).H(s.winH).Bg(cSidebar).BorderRight(1, cBorder)

	p.Children(ui.Div().Row().ItemsCenter().Gap(14).PadXY(16, 10).Children(
		iconBtn("folder", 16, cText),
		iconBtn("search", 16, cFaint),
		iconBtn("bookmark", 16, cFaint),
	))
	actions := ui.Div().Row().ItemsCenter().Gap(12).PadXY(24, 6)
	for _, n := range []string{"pencil", "folderP", "sort", "expand", "collapse"} {
		actions.Children(iconBtn(n, 15, cMuted))
	}
	p.Children(actions, ui.Div().H(6))

	tree := ui.Div().Col().Grow().PadXY(10, 0)
	for i, row := range vaultTree {
		i, row := i, row
		r := ui.Div().Row().ItemsCenter().Gap(4).PadXY(6, 5).Rounded(6).HoverBg(cRowHov)
		if row.sel || s.selected.Get() == i {
			r.Bg(cRowSel)
		} else if row.active {
			r.Bg(cRowHov)
		}
		r.Children(ui.Div().W(float32(row.indent) * 17))
		switch row.chev {
		case "d":
			r.Children(icon("chevD", 11, cMuted))
		case "r":
			r.Children(icon("chevR", 11, cMuted))
		default:
			r.Children(ui.Div().W(11))
		}
		r.Children(ui.Text(row.name, cText).SizePx(12))
		if row.badge != "" {
			r.Children(ui.Div().Grow(),
				ui.Text(row.badge, cBadge).SizePx(8))
		}
		r.OnClick(func() { s.selected.Set(i) })
		tree.Children(r)
	}
	p.Children(tree)

	p.Children(ui.Div().Row().ItemsCenter().Gap(6).PadXY(14, 10).Children(
		icon("chevUD", 13, cMuted),
		ui.Text("Obsidian Vault", cText).SizePx(12),
		ui.Div().Grow(),
		iconBtn("help", 15, cMuted),
		iconBtn("gear", 15, cMuted),
	))
	return p
}

// tabBar is the top strip with the active Canvas tab and window buttons.
func (s *shell) tabBar(mainX float32) ui.Element {
	w := s.winW - mainX
	bar := ui.Div().Row().ItemsCenter().W(w).H(topH).Bg(cSidebar).BorderBottom(1, cBorder).PadXY(8, 0)
	tab := ui.Div().Row().ItemsCenter().Gap(8).PadXY(12, 7).Bg(cWhite).Rounded(8).
		Border(1, cBorder).Children(
		icon("canvasT", 13, cMuted),
		ui.Text("Untitled", cText).SizePx(12),
		ui.Div().W(60),
		icon("close", 12, cMuted),
	)
	bar.Children(tab, ui.Div().W(10), iconBtn("plus", 15, cMuted))
	minBtn := iconBtn("minus", 15, cText)
	maxBtn := iconBtn("maxim", 14, cText)
	closeBtn := iconBtn("close", 15, cText)
	if s.onMin != nil {
		minBtn.OnClick(s.onMin)
		maxBtn.OnClick(s.onMax)
		closeBtn.OnClick(s.onClose)
	}
	bar.Children(ui.Div().Grow(),
		iconBtn("chevD", 14, cMuted), ui.Div().W(4),
		iconBtn("panelR", 15, cMuted), ui.Div().W(10),
		minBtn, ui.Div().W(8),
		maxBtn, ui.Div().W(8),
		closeBtn,
	)
	return bar
}

// navRow holds back/forward, the note title and the more menu.
func (s *shell) navRow(mainX float32) ui.Element {
	w := s.winW - mainX
	return ui.Div().Row().ItemsCenter().W(w).H(navH).Bg(cWhite).PadXY(10, 0).Children(
		iconBtn("arrowL", 14, cFaint),
		ui.Div().W(4),
		iconBtn("arrowR", 14, cFaint),
		ui.Div().Grow(),
		ui.Text("Untitled", cText).SizePx(12),
		ui.Div().Grow(),
		iconBtn("moreV", 14, cMuted),
	)
}

// canvas paints the dot grid and the idea cards (absolute coordinates —
// ui.Overlay positions in window space).
func (s *shell) canvas(mainX float32) ui.Element {
	w := s.winW - mainX
	h := s.winH - topH - navH
	grid := ui.NewCanvas(w, h, func(r *render.Renderer2D, rect ui.Rect) {
		const step = 19.5
		for y := rect.Y; y < rect.Y+rect.H; y += step {
			for x := rect.X; x < rect.X+rect.W; x += step {
				r.FillRect(x, y, 2, 2, render.RGBA(cDot))
			}
		}
	})
	c := ui.Div()
	c.Children(ui.Overlay(ui.Div().Size(w, h).Bg(cWhite), mainX, topH+navH))
	c.Children(ui.Overlay(grid, mainX, topH+navH))
	for _, cd := range canvasCards {
		cd := cd
		el := ui.Div().Size(cd.w, cd.h).Bg(cWhite).Rounded(9).Border(1.5, cCardBrd).
			PadXY(12, 6)
		if cd.text != "" {
			if cd.wrap {
				el.Children(ui.Wrapped(cd.text, cText, cd.w-24).SizePx(12))
			} else {
				el.Col().JustifyCenter().Children(ui.Text(cd.text, cText).SizePx(12))
			}
		}
		c.Children(ui.Overlay(el, cd.x, cd.y))
	}
	return c
}

// cardToolbar is the bottom-center card-type cluster.
func (s *shell) cardToolbar() ui.Element {
	t := ui.Div().Row().ItemsCenter().Gap(10).PadXY(14, 9).Bg(cWhite).Rounded(10).
		Border(1, cBorder).Shadow(8, 0, 2, ui.RGBA{R: 0.2, G: 0.22, B: 0.26, A: 0.10})
	for _, n := range []string{"fileCard", "noteCard", "imgCard"} {
		t.Children(iconBtn(n, 17, cMuted))
	}
	return t
}

// rightButtons is the right-edge floating button stack.
func (s *shell) rightButtons() ui.Element {
	col := ui.Div().Col().Gap(10).ItemsCenter()
	box := func(names ...string) ui.Element {
		b := ui.Div().Col().ItemsCenter().Bg(cWhite).Rounded(8).Border(1, cBorder).P(2)
		for _, n := range names {
			b.Children(iconBtn(n, 15, cMuted))
		}
		return b
	}
	col.Children(
		box("gear"),
		box("plus", "refresh", "fullscr", "minus"),
		box("undo", "redo"),
		box("help"),
	)
	return col
}
