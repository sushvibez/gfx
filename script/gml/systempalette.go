package gml

// SystemPalette: the colour roles of the platform's palette, which a page
// reads to match the controls around it (`color: palette.window`). There
// is no platform theme to ask here, so it holds the light palette gml's
// own controls are drawn in (the Fusion family, the style the control
// templates follow: #308cc6 highlight, #f0f0f0 window), so a page that
// paints with it sits right among them. colorGroup picks Active, Inactive
// or Disabled; it is re-read every build, so a binding on it follows.

// QML's SystemPalette.ColorGroup values are QPalette's: Active 0,
// Disabled 1, Inactive 2.
const (
	paletteActive   = 0
	paletteDisabled = 1
	paletteInactive = 2
)

// systemPaletteActive is the Active group, role by role.
var systemPaletteActive = map[string]string{
	"window": "#f0f0f0", "windowText": "#1a1a1a",
	"base": "#ffffff", "alternateBase": "#f7f7f7",
	"text": "#1a1a1a", "placeholderText": "#801a1a1a",
	"button": "#e0e0e0", "buttonText": "#1a1a1a", "brightText": "#ffffff",
	"light": "#ffffff", "midlight": "#cacaca", "mid": "#b8b8b8",
	"dark": "#9f9f9f", "shadow": "#767676",
	"highlight": "#308cc6", "highlightedText": "#ffffff", "accent": "#308cc6",
	"link": "#0000ff", "linkVisited": "#ff00ff",
	"toolTipBase": "#ffffdc", "toolTipText": "#1a1a1a",
}

// systemPaletteDisabled are the roles the Disabled group changes: text
// greyed, the highlight muted.
var systemPaletteDisabled = map[string]string{
	"windowText": "#bebebe", "text": "#bebebe", "buttonText": "#bebebe",
	"placeholderText": "#80bebebe", "highlight": "#919191",
	"highlightedText": "#ffffff", "base": "#f0f0f0",
}

// systemPaletteInactive: an unfocused window keeps its colours; Fusion
// only dims the selection a little.
var systemPaletteInactive = map[string]string{
	"highlight": "#4e9fd3",
}

// systemPaletteColor is one role of one group, as a colour value.
func systemPaletteColor(group int, role string) any {
	var over map[string]string
	switch group {
	case paletteDisabled:
		over = systemPaletteDisabled
	case paletteInactive:
		over = systemPaletteInactive
	}
	if c, ok := over[role]; ok {
		return colorVal(c)
	}
	return colorVal(systemPaletteActive[role])
}

// seedSystemPalette writes the palette's roles, for its colorGroup, into
// a SystemPalette's id object.
func seedSystemPalette(n *node, env *Env, obj *elemObject) {
	if n.typeName != "SystemPalette" || obj == nil {
		return
	}
	group := paletteActive
	for _, a := range n.attrs {
		if a.name == "colorGroup" && a.expr != nil {
			if v, err := a.expr.eval(env); err == nil {
				if g, err := toInt(v); err == nil {
					group = g
				}
			}
		}
	}
	obj.setProp("colorGroup", float64(group))
	for role := range systemPaletteActive {
		obj.setProp(role, systemPaletteColor(group, role))
	}
}
