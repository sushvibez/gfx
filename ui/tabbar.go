package ui

import "github.com/sushvibez/gfx/app"

// TabBar (QML's TabBar and TabButton) in Qt's Basic look: a light 40 px
// bar, the current tab near-white with black text, the others mid-grey with
// white text, 1 px apart. The tabs share the bar's width, except one that
// keeps its own (Fixed). Left/Right move between tabs while the bar holds
// focus.

// Tab is one tab of a TabBar.
type Tab struct {
	Label string
	// Fixed keeps the tab its natural width instead of a share of the bar.
	Fixed bool
	// SizePx is the label's type size; 0 is the bar's 12 px.
	SizePx float64
}

// Qt Basic's tab palette (the default theme's) and metrics, sampled from
// the reference renders.
const (
	TabCurrentColor  = 0xefefef // the current tab, and the bar between tabs
	TabInactiveColor = 0x9f9f9f // the other tabs
	TabHoverColor    = 0xb4b4b4 // an inactive tab under the pointer
	TabBarHeight     = 40
	tabPad           = 6
	TabGap           = 1
	tabFontPx        = 12
)

// TabBar is a row of tabs with current selected; onSelect runs with the tab
// clicked (or reached with Left/Right) — set the current index there.
func TabBar(current int, tabs []Tab, onSelect func(int)) *DivEl {
	tm := CurrentTheme()
	bar := Div().Row().Gap(TabGap).Bg(tm.Band).Focusable()
	bar.Role(RoleTabList, "")
	for i, t := range tabs {
		active := i == current
		textCol, bg := tm.TabInactiveText, tm.TabInactive
		if active {
			textCol, bg = tm.TabText, tm.Band
		}
		size := t.SizePx
		if size <= 0 {
			size = tabFontPx
		}
		tab := Div().Row().ItemsCenter().JustifyCenter().H(TabBarHeight).P(tabPad).
			Bg(bg).Children(Text(t.Label, textCol).Truncate().SizePx(size))
		if !active {
			// The current tab is already the light colour and does not react.
			tab.HoverBg(tm.TabHover)
		}
		if !t.Fixed {
			tab.Grow().Shrink().Clip()
		}
		if onSelect != nil {
			i := i
			tab.OnClick(func() { onSelect(i) })
		}
		tab.Access(Access{Role: RoleTab, Name: t.Label, Selected: active, Checked: -1, Expanded: -1})
		bar.Children(tab)
	}
	if onSelect != nil && len(tabs) > 0 {
		bar.OnKeyAccept(func(ev app.KeyEvent) bool {
			if !ev.Down {
				return false
			}
			switch ev.Key {
			case app.KeyLeft:
				if current > 0 {
					onSelect(current - 1)
				}
			case app.KeyRight:
				if current < len(tabs)-1 {
					onSelect(current + 1)
				}
			default:
				return false
			}
			return true
		})
	}
	return bar
}
