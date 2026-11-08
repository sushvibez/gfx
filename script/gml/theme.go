package gml

// The controls' theme from the markup, over ui.ThemeScope: an item that
// states Material.theme / Material.accent (or Universal's), or a palette
// role, draws the controls inside it in that theme — QML's attached style
// properties, which every child inherits.
//
//	ApplicationWindow {
//	    Material.theme: dark.checked ? Material.Dark : Material.Light
//	    Material.accent: Material.Teal
//	    palette.window: "#202020"
//	}
//
// The theme is the controls kit's (ui.LightTheme / ui.DarkTheme), not a
// Material Design rendition: Material.theme picks light or dark, the
// accent recolours what the accent colours, background and foreground set
// the page and its text, and the palette roles map onto the theme's.

import (
	"strings"

	"github.com/sushvibez/gfx/ui"
)

// Qt's Material.Theme and Universal.Theme values.
const (
	themeLight  = 0
	themeDark   = 1
	themeSystem = 2
)

// materialColors are Material.Color's swatches (the 500 shades), in Qt's
// enum order (Material.Red is 0).
var materialColors = []uint32{
	0xf44336, 0xe91e63, 0x9c27b0, 0x673ab7, 0x3f51b5, 0x2196f3, 0x03a9f4,
	0x00bcd4, 0x009688, 0x4caf50, 0x8bc34a, 0xcddc39, 0xffeb3b, 0xffc107,
	0xff9800, 0xff5722, 0x795548, 0x9e9e9e, 0x607d8b,
}

var materialColorNames = []string{
	"Red", "Pink", "Purple", "DeepPurple", "Indigo", "Blue", "LightBlue",
	"Cyan", "Teal", "Green", "LightGreen", "Lime", "Yellow", "Amber",
	"Orange", "DeepOrange", "Brown", "Grey", "BlueGrey",
}

// universalColors are Universal.Color's, in Qt's enum order.
var universalColors = []uint32{
	0xa4c400, 0x60a917, 0x008a00, 0x00aba9, 0x1ba1e2, 0x0050ef, 0x6a00ff,
	0xaa00ff, 0xf472d0, 0xd80073, 0xa20025, 0xe51400, 0xfa6800, 0xf0a30a,
	0xe3c800, 0x825a2c, 0x6d8764, 0x647687, 0x76608a, 0x87794e,
}

var universalColorNames = []string{
	"Lime", "Green", "Emerald", "Teal", "Cyan", "Cobalt", "Indigo", "Violet",
	"Pink", "Magenta", "Crimson", "Red", "Orange", "Amber", "Yellow", "Brown",
	"Olive", "Steel", "Mauve", "Taupe",
}

// registerThemeEnums adds the theme and colour names to the Material and
// Universal namespaces (Material.Dark, Material.Teal, Material.color()).
func registerThemeEnums(vars map[string]any) {
	style := func(ns map[string]any, names []string, colors []uint32) map[string]any {
		if ns == nil {
			ns = map[string]any{}
		}
		ns["Light"], ns["Dark"], ns["System"] = float64(themeLight), float64(themeDark), float64(themeSystem)
		for i, name := range names {
			ns[name] = float64(i)
		}
		// Material.color(Material.Teal): the swatch as a colour.
		ns["color"] = jsFunc(func(args []any) (any, error) {
			if len(args) > 0 {
				if f, err := toFloat64(args[0]); err == nil && int(f) >= 0 && int(f) < len(colors) {
					return hexColor(colors[int(f)]), nil
				}
			}
			return "#000000", nil
		})
		return ns
	}
	m, _ := vars["Material"].(map[string]any)
	vars["Material"] = style(m, materialColorNames, materialColors)
	vars["Universal"] = style(nil, universalColorNames, universalColors)
}

func hexColor(c uint32) string {
	const digits = "0123456789abcdef"
	b := []byte("#000000")
	for i := 0; i < 6; i++ {
		b[6-i] = digits[(c>>(4*i))&0xf]
	}
	return string(b)
}

// hasThemeAttrs reports whether a node states anything themeOf reads.
func hasThemeAttrs(n *node) bool {
	for _, a := range n.attrs {
		if strings.HasPrefix(a.name, "Material.") || strings.HasPrefix(a.name, "Universal.") {
			return true
		}
		if themePaletteRole(a.name) {
			return true
		}
	}
	return false
}

// themePaletteRole reports a palette role that sets the theme (rather than
// one control's own colour, which the control reads itself).
func themePaletteRole(name string) bool {
	switch name {
	case "palette.window", "palette.windowText", "palette.base", "palette.text",
		"palette.highlight", "palette.accent", "palette.toolTipBase", "palette.toolTipText":
		return true
	}
	return false
}

// themeOf is the theme a node states, on top of the theme it is built in.
func themeOf(n *node, env *Env) ui.Theme {
	t := ui.CurrentTheme()
	// The light/dark choice first: the colours below override it.
	for _, a := range n.attrs {
		if a.name != "Material.theme" && a.name != "Universal.theme" {
			continue
		}
		if f, err := attrFloat(a, env); err == nil {
			accent := t.Accent
			switch int(f) {
			case themeDark:
				t = ui.DarkTheme()
			default: // Light, and System (there is no platform theme behind it)
				t = ui.LightTheme()
			}
			t = t.WithAccent(accent) // the theme switch keeps an accent set above
		}
	}
	color := func(a attr, swatches []uint32) (ui.RGBA, bool) {
		if v, err := a.expr.eval(env); err == nil {
			if f, ok := v.(float64); ok {
				if int(f) >= 0 && int(f) < len(swatches) {
					return ui.RGB(swatches[int(f)]), true
				}
				return ui.RGBA{}, false
			}
		}
		c, err := attrColor(a, env)
		return c, err == nil
	}
	for _, a := range n.attrs {
		switch a.name {
		case "Material.accent", "Material.primary":
			if c, ok := color(a, materialColors); ok {
				t = t.WithAccent(c)
			}
		case "Universal.accent":
			if c, ok := color(a, universalColors); ok {
				t = t.WithAccent(c)
			}
		case "Material.background", "Universal.background", "palette.window":
			if c, ok := color(a, materialColors); ok {
				t.Window = c
			}
		case "Material.foreground", "Universal.foreground", "palette.windowText":
			if c, ok := color(a, materialColors); ok {
				t.Text, t.Label = c, c
			}
		case "palette.base":
			if c, err := attrColor(a, env); err == nil {
				t.Field, t.Input = c, c
			}
		case "palette.text":
			if c, err := attrColor(a, env); err == nil {
				t.Text, t.InputText = c, c
			}
		case "palette.highlight", "palette.accent":
			if c, err := attrColor(a, env); err == nil {
				t = t.WithAccent(c)
			}
		case "palette.toolTipBase":
			if c, err := attrColor(a, env); err == nil {
				t.ToolTip = c
			}
		case "palette.toolTipText":
			if c, err := attrColor(a, env); err == nil {
				t.ToolTipText = c
			}
		}
	}
	return t
}

// themeScope is the node's own ui.ThemeScope, kept across frames.
func (doc *docState) themeScope(n *node) *ui.ThemeScope {
	if doc.themeScopes == nil {
		doc.themeScopes = map[string]*ui.ThemeScope{}
	}
	key := doc.key(n)
	s := doc.themeScopes[key]
	if s == nil {
		s = &ui.ThemeScope{}
		doc.themeScopes[key] = s
	}
	return s
}
