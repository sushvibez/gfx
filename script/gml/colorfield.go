package gml

import (
	"fmt"

	"github.com/sushvibez/gfx/ui"
)

// ColorField (gfx extension): a colour well that opens a picker — a
// saturation × value square, a hue bar, a hex field and swatches
// (ui.ColorField).
//
//	ColorField { color: lightColour; width: 160
//	             onColorEdited: setColour(color)     // live, while dragging
//	             onColorPicked: commitColour(color) } // a pick finished
//
// `color` is any colour string; the handlers see `color` as "#rrggbb".
// swatches: an array of colour strings replaces the presets ([] hides them).
func buildColorField(n *node, env *Env, doc *docState) (ui.Element, error) {
	var o ui.ColorOpts
	c := ui.RGBA{A: 1}
	enabled := true
	var onEdited, onPicked []stmt
	for _, a := range n.attrs {
		switch a.name {
		case "id", "visible", "opacity":
		case "color":
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			if pc, err := ui.ParseColor(str(v)); err == nil {
				c = pc
			}
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			o.Width = f
		case "enabled":
			if b, err := attrBool(a, env); err == nil {
				enabled = b
			}
		case "swatches":
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			arr, _ := v.([]any)
			o.Swatches = []ui.RGBA{}
			for _, s := range arr {
				if pc, err := ui.ParseColor(str(s)); err == nil {
					o.Swatches = append(o.Swatches, pc)
				}
			}
		case "onColorEdited":
			onEdited = a.stmt
		case "onColorPicked":
			onPicked = a.stmt
		default:
			return nil, fmt.Errorf("gml:%d: ColorField has no attribute %q", a.line, a.name)
		}
	}
	o.Disabled = !enabled
	key := doc.key(n)
	if doc.colors == nil {
		doc.colors = map[string]*ui.ColorState{}
	}
	st := doc.colors[key]
	if st == nil {
		st = ui.NewColorState()
		doc.colors[key] = st
	}
	doc.exposeIDProp(n, "color", ui.ColorHex(c))
	run := doc.handlerRunner(env)
	with := func(stmts []stmt, what string) func(ui.RGBA) {
		if stmts == nil {
			return nil
		}
		return func(nc ui.RGBA) {
			undo := env.bindEvent("color", ui.ColorHex(nc))
			run(stmts, what)
			undo()
		}
	}
	o.OnChange = with(onEdited, "onColorEdited")
	o.OnDone = with(onPicked, "onColorPicked")
	return ui.ColorField(c, st, o), nil
}
