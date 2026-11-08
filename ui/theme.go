package ui

import "github.com/sushvibez/gfx/core"

// Theme is the colours the controls are drawn in: the accent, text,
// fields, tracks, popups, bands (a menu bar, a dialog's button box, the
// current tab), menus and tool tips. Every control reads the current theme
// as it is built (SetTheme changes it for the whole program, and a
// control's options can carry one of its own).
//
// DefaultTheme is the look the controls have always had: light kit
// surfaces (buttons, fields, dialogs, tabs) with the original dark-ground
// form controls (check boxes, sliders, text inputs) and a dark menu card.
// LightTheme and DarkTheme are consistent all the way through; an app
// picks one with SetTheme.
type Theme struct {
	Dark bool // drawn for a dark page

	Window RGBA // the page behind the controls

	Accent, AccentHover, AccentPress RGBA // a button, a checked box, a slider's fill
	OnAccent                         RGBA // text and marks on the accent

	Text   RGBA // a control's own text (a switch's label, a field's value)
	Label  RGBA // a check box's or radio button's label
	Dim    RGBA // a hint glyph (a combo box's arrow)
	Border RGBA // a field's, a frame's, a progress bar's track

	Outline                   RGBA // an unchecked check box
	Track, Knob, KnobDisabled RGBA // a slider or dial
	SwitchOff                 RGBA // an unchecked switch's track

	Field, FieldButton    RGBA // a combo or spin box, and a spin box's buttons
	RowHover, RowSelected RGBA // a drop-down list's rows

	Input, InputBorder, InputText, Selection RGBA // a text input

	Surface RGBA // a popup's card (PopupSkin)
	Panel   RGBA // a drawer's panel

	Band, BandHover, BandOpen, BandText RGBA // a menu bar, a dialog's button box

	Menu, MenuBorder, MenuHover, MenuText, MenuDim RGBA

	TabInactive, TabHover, TabText, TabInactiveText RGBA // the current tab is Band

	ToolTip, ToolTipText RGBA

	Ring      RGBA // a dial's track
	Indicator RGBA // a page indicator's dots (the current one; the rest are faded)
}

// DefaultTheme is the controls' original look (see Theme).
func DefaultTheme() Theme {
	return Theme{
		Window: RGB(0xf0f0f0),
		Accent: RGB(0x3b5bdb), AccentHover: RGB(0x4c6ef5), AccentPress: RGB(0x2f44ad),
		OnAccent: RGB(0xffffff),
		Text:     RGB(0x111111), Label: RGB(0xd4d4e0), Dim: RGB(0x868e96), Border: RGB(0xced4da),
		Outline: RGB(0x4a4a5c), Track: RGB(0x32323f), Knob: RGB(0xe8e8f0), KnobDisabled: RGB(0x9a9a9a),
		SwitchOff: RGB(0xb0b4bf),
		Field:     RGB(0xffffff), FieldButton: RGB(0xe9ecef),
		RowHover: RGB(0xe7f0ff), RowSelected: RGB(0xd0e2ff),
		Input: RGB(0x101018), InputBorder: RGB(0x3b3b4f), InputText: RGB(0xe8e8f0),
		Selection: RGBA{R: 0.23, G: 0.36, B: 0.86, A: 0.5},
		Surface:   RGB(0xf4f5f7), Panel: RGB(0xffffff),
		Band: RGB(0xefefef), BandHover: RGBA{R: 0.85, G: 0.86, B: 0.88, A: 1},
		BandOpen: RGBA{R: 0.80, G: 0.82, B: 0.86, A: 1}, BandText: RGB(0x1a1a1a),
		Menu: RGBA{R: 0.16, G: 0.17, B: 0.20, A: 1}, MenuBorder: RGBA{R: 0.30, G: 0.31, B: 0.35, A: 1},
		MenuHover: RGBA{R: 0.24, G: 0.25, B: 0.30, A: 1}, MenuText: RGB(0xeeeeee), MenuDim: RGB(0x777777),
		TabInactive: RGB(0x9f9f9f), TabHover: RGB(0xb4b4b4), TabText: RGB(0x000000), TabInactiveText: RGB(0xffffff),
		ToolTip: RGB(0x333333), ToolTipText: RGB(0xffffff),
		Ring: RGB(0x5a5f6a), Indicator: RGBA{R: 0.6, G: 0.6, B: 0.6, A: 1},
	}
}

// LightTheme is light throughout: dark text on white fields and cards.
func LightTheme() Theme {
	t := DefaultTheme()
	t.Window = RGB(0xf8f9fa)
	t.Label = RGB(0x1a1a1a)
	t.Outline = RGB(0x868e96)
	t.Track, t.Knob, t.KnobDisabled = RGB(0xdee2e6), RGB(0xffffff), RGB(0xced4da)
	t.Input, t.InputBorder, t.InputText = RGB(0xffffff), RGB(0xced4da), RGB(0x111111)
	t.Menu, t.MenuBorder, t.MenuHover = RGB(0xffffff), RGB(0xced4da), RGB(0xe7f0ff)
	t.MenuText, t.MenuDim = RGB(0x111111), RGB(0xadb5bd)
	t.Ring = RGB(0xdee2e6)
	return t
}

// DarkTheme is dark throughout: light text on charcoal fields and cards.
func DarkTheme() Theme {
	t := DefaultTheme()
	t.Dark = true
	t.Window = RGB(0x17181d)
	t.Text, t.Label, t.Dim, t.Border = RGB(0xe8e8f0), RGB(0xd4d4e0), RGB(0x868e96), RGB(0x3a3d47)
	t.KnobDisabled, t.SwitchOff = RGB(0x5c5f6a), RGB(0x4a4d57)
	t.Field, t.FieldButton = RGB(0x1e2027), RGB(0x2c2f38)
	t.RowHover, t.RowSelected = RGB(0x2c3550), RGB(0x34437a)
	t.Surface, t.Panel = RGB(0x2a2d35), RGB(0x2a2d35)
	t.Band, t.BandHover, t.BandOpen, t.BandText = RGB(0x23252c), RGB(0x2f323b), RGB(0x3a3e49), RGB(0xe8e8f0)
	t.TabInactive, t.TabHover, t.TabText, t.TabInactiveText = RGB(0x17181d), RGB(0x2a2d35), RGB(0xe8e8f0), RGB(0x9a9cab)
	t.ToolTip, t.ToolTipText = RGB(0x3a3d47), RGB(0xe8e8f0)
	return t
}

// theme is the current theme. A property, so switching themes redraws
// whatever caches the tree.
var (
	theme       core.Property[Theme]
	themeInited bool
)

// SetTheme makes t the theme every control is built in from now on.
func SetTheme(t Theme) {
	themeInited = true
	theme.Set(t)
}

// CurrentTheme is the theme controls are built in now.
func CurrentTheme() Theme {
	if !themeInited {
		themeInited = true
		theme.Set(DefaultTheme())
	}
	return theme.Get()
}

// themeOr is t if a control's options carry one, else the current theme.
func themeOr(t *Theme) Theme {
	if t != nil {
		return *t
	}
	return CurrentTheme()
}
