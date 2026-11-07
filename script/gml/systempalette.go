package gml

import (
	"fmt"

	"github.com/sushvibez/gfx/ui"
)

// SystemPalette: the colour roles of ui.DefaultPalette (the light palette
// gml's own controls are drawn in; there is no platform theme to ask), so
// a page that paints with it sits right among the controls. colorGroup
// picks Active, Inactive or Disabled; it is re-read every build, so a
// binding on it follows.

// QML's SystemPalette.ColorGroup values are QPalette's, as ui.ColorGroup's.
const (
	paletteActive   = int(ui.GroupActive)
	paletteDisabled = int(ui.GroupDisabled)
	paletteInactive = int(ui.GroupInactive)
)

// systemPaletteColor is one role of one group, as a colour value.
func systemPaletteColor(group int, role string) any {
	c, _ := ui.DefaultPalette(ui.ColorGroup(group)).Role(role)
	return colorVal(paletteHex(c))
}

// paletteHex writes c as #rrggbb, or Qt's #aarrggbb when it is not opaque.
func paletteHex(c ui.RGBA) string {
	if c.A >= 1 {
		cl := func(v float32) int { return min(max(int(float64(v)*255+0.5), 0), 255) }
		return fmt.Sprintf("#%02x%02x%02x", cl(c.R), cl(c.G), cl(c.B))
	}
	return colorHex(c)
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
	p := ui.DefaultPalette(ui.ColorGroup(group))
	for _, role := range ui.PaletteRoles() {
		c, _ := p.Role(role)
		obj.setProp(role, colorVal(paletteHex(c)))
	}
}
