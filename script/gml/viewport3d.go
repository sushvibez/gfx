package gml

// Viewport3D embeds a live 3D view in markup. The Go host owns the scene: it
// registers a *render.View3D under a name in the document Env, and the
// markup places it like any element:
//
//	// Go: env.Set("mainView", view3d)
//	Viewport3D { source: mainView; width: 480; height: 320
//	             onTapped: console.log(tapX, tapY) }
//
// onTapped runs with tapX/tapY bound to the viewport-local click position —
// the host (or script) forwards them to picking.

import (
	"fmt"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

func buildViewport3D(n *node, env *Env, doc *docState) (ui.Element, error) {
	var view *render.View3D
	var w, h float32
	var grow bool
	var overlay ui.ViewportOverlay
	var onTapped []stmt
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "source":
			v, err := resolveAny(a.expr, env)
			if err != nil {
				return nil, err
			}
			if vv, ok := v.(*render.View3D); ok {
				view = vv
			}
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			h = f
		case "grow": // extension: fill the parent box
			b, err := attrBool(a, env)
			if err != nil {
				return nil, err
			}
			grow = b
		case "overlay":
			// A host-registered ui.ViewportOverlay painted over the frame.
			v, err := resolveAny(a.expr, env)
			if err != nil {
				return nil, err
			}
			if fn, ok := v.(ui.ViewportOverlay); ok {
				overlay = fn
			}
		case "onTapped":
			onTapped = a.stmt
		default:
			return nil, fmt.Errorf("gml:%d: Viewport3D has no attribute %q", a.line, a.name)
		}
	}
	if view == nil {
		return nil, fmt.Errorf("gml:%d: Viewport3D needs source: a registered render.View3D", n.line)
	}
	el := ui.Viewport3D(view)
	if w > 0 || h > 0 {
		el.Size(w, h)
	}
	if grow {
		el.Grow()
	}
	if overlay != nil {
		el.Overlay(overlay)
	}
	if onTapped != nil {
		el.OnTap(func(x, y float32) {
			scope := env.child()
			scope.vars["tapX"] = float64(x)
			scope.vars["tapY"] = float64(y)
			if err := runStmts(onTapped, scope); err != nil {
				logf("gml: Viewport3D onTapped: %v", err)
			}
		})
	}
	return el, nil
}
