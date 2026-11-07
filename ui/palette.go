package ui

// Palette is a set of colour roles (Qt's QPalette roles): what a page
// paints with to match the controls around it. DefaultPalette is the
// light palette the built-in widgets and gml's controls are drawn in
// (the Fusion family: #308cc6 highlight, #f0f0f0 window). There is no
// platform theme behind it.
type Palette struct {
	Window, WindowText         RGBA
	Base, AlternateBase        RGBA
	Text, PlaceholderText      RGBA
	Button, ButtonText         RGBA
	BrightText                 RGBA
	Light, Midlight, Mid       RGBA
	Dark, Shadow               RGBA
	Highlight, HighlightedText RGBA
	Accent                     RGBA
	Link, LinkVisited          RGBA
	ToolTipBase, ToolTipText   RGBA
}

// ColorGroup is the state a palette is for. The values are QPalette's
// (QML's SystemPalette.colorGroup uses the same).
type ColorGroup int

const (
	GroupActive   ColorGroup = 0 // a focused window
	GroupDisabled ColorGroup = 1 // a disabled control: text greyed
	GroupInactive ColorGroup = 2 // an unfocused window
)

// withAlpha is c at alpha a.
func withAlpha(c RGBA, a float32) RGBA { c.A = a; return c }

// DefaultPalette is the built-in palette for a colour group.
func DefaultPalette(g ColorGroup) Palette {
	p := Palette{
		Window: RGB(0xf0f0f0), WindowText: RGB(0x1a1a1a),
		Base: RGB(0xffffff), AlternateBase: RGB(0xf7f7f7),
		Text: RGB(0x1a1a1a), PlaceholderText: withAlpha(RGB(0x1a1a1a), 128.0/255),
		Button: RGB(0xe0e0e0), ButtonText: RGB(0x1a1a1a), BrightText: RGB(0xffffff),
		Light: RGB(0xffffff), Midlight: RGB(0xcacaca), Mid: RGB(0xb8b8b8),
		Dark: RGB(0x9f9f9f), Shadow: RGB(0x767676),
		Highlight: RGB(0x308cc6), HighlightedText: RGB(0xffffff), Accent: RGB(0x308cc6),
		Link: RGB(0x0000ff), LinkVisited: RGB(0xff00ff),
		ToolTipBase: RGB(0xffffdc), ToolTipText: RGB(0x1a1a1a),
	}
	switch g {
	case GroupDisabled:
		grey := RGB(0xbebebe)
		p.WindowText, p.Text, p.ButtonText = grey, grey, grey
		p.PlaceholderText = withAlpha(grey, 128.0/255)
		p.Highlight, p.HighlightedText, p.Base = RGB(0x919191), RGB(0xffffff), RGB(0xf0f0f0)
	case GroupInactive:
		p.Highlight = RGB(0x4e9fd3) // Fusion dims an unfocused selection a little
	}
	return p
}

// paletteRoles names each role as QML does (palette.window,
// SystemPalette.windowText).
var paletteRoles = map[string]func(*Palette) *RGBA{
	"window":          func(p *Palette) *RGBA { return &p.Window },
	"windowText":      func(p *Palette) *RGBA { return &p.WindowText },
	"base":            func(p *Palette) *RGBA { return &p.Base },
	"alternateBase":   func(p *Palette) *RGBA { return &p.AlternateBase },
	"text":            func(p *Palette) *RGBA { return &p.Text },
	"placeholderText": func(p *Palette) *RGBA { return &p.PlaceholderText },
	"button":          func(p *Palette) *RGBA { return &p.Button },
	"buttonText":      func(p *Palette) *RGBA { return &p.ButtonText },
	"brightText":      func(p *Palette) *RGBA { return &p.BrightText },
	"light":           func(p *Palette) *RGBA { return &p.Light },
	"midlight":        func(p *Palette) *RGBA { return &p.Midlight },
	"mid":             func(p *Palette) *RGBA { return &p.Mid },
	"dark":            func(p *Palette) *RGBA { return &p.Dark },
	"shadow":          func(p *Palette) *RGBA { return &p.Shadow },
	"highlight":       func(p *Palette) *RGBA { return &p.Highlight },
	"highlightedText": func(p *Palette) *RGBA { return &p.HighlightedText },
	"accent":          func(p *Palette) *RGBA { return &p.Accent },
	"link":            func(p *Palette) *RGBA { return &p.Link },
	"linkVisited":     func(p *Palette) *RGBA { return &p.LinkVisited },
	"toolTipBase":     func(p *Palette) *RGBA { return &p.ToolTipBase },
	"toolTipText":     func(p *Palette) *RGBA { return &p.ToolTipText },
}

// Role is the colour of the role named as QML names it ("window",
// "highlightedText"); ok is false for a name that is not a role.
func (p Palette) Role(name string) (c RGBA, ok bool) {
	if f := paletteRoles[name]; f != nil {
		return *f(&p), true
	}
	return RGBA{}, false
}

// PaletteRoles are the role names Role knows.
func PaletteRoles() []string {
	out := make([]string, 0, len(paletteRoles))
	for k := range paletteRoles {
		out = append(out, k)
	}
	return out
}
