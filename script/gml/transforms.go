package gml

// QML item transforms: `rotation`, `scale` and the `transform:` list of
// Translate / Rotation / Scale objects. They affect PAINTING only — the
// item keeps its unrotated layout box, as in QML — and the ui layer applies
// them to the whole subtree (see ui/transform.go).

import "github.com/sushvibez/gfx/ui"

// readTransformList reads a `transform:` attribute: one element or a list.
// doc may be nil; with one, each transform element is read THROUGH its
// attribute overrides, so an animation driving `translate.x` (a
// `NumberAnimation { target: someTranslate; property: "x" }`) moves it.
func readTransformList(a attr, env *Env, doc *docState) []ui.Transform {
	var nodes []*node
	if a.elem != nil {
		nodes = append(nodes, a.elem)
	} else if a.expr != nil {
		if v, err := a.expr.eval(env); err == nil {
			if arr, ok := v.([]any); ok {
				for _, it := range arr {
					if n, ok := it.(*node); ok {
						nodes = append(nodes, n)
					}
				}
			}
		}
	}
	out := make([]ui.Transform, 0, len(nodes))
	for _, n := range nodes {
		if doc != nil {
			n = doc.withAttrOverrides(n)
		}
		t, ok := readTransform(n, env)
		if ok {
			out = append(out, t)
		}
	}
	return out
}

// readTransform reads one Translate / Rotation / Scale element.
func readTransform(n *node, env *Env) (ui.Transform, bool) {
	num := func(name string) (float32, bool) {
		for _, a := range n.attrs {
			if a.name == name {
				if f, err := attrFloat(a, env); err == nil {
					return f, true
				}
			}
		}
		return 0, false
	}
	switch n.typeName {
	case "Translate":
		x, _ := num("x")
		y, _ := num("y")
		return ui.Translate(x, y), true
	case "Rotation":
		ang, _ := num("angle")
		ox, hasOX := num("origin.x")
		oy, hasOY := num("origin.y")
		if !hasOX && !hasOY {
			// No origin: QML rotates about the item's own origin (0,0).
			ox, oy = 0, 0
		}
		// `axis { x: 0; y: 1; z: 0 }` turns the item about that axis, which
		// tilts it away from the viewer: the card-flip and 3D-rotation
		// idioms. Without an axis QML turns about Z, in the plane.
		ax, hasAX := num("axis.x")
		ay, hasAY := num("axis.y")
		az, hasAZ := num("axis.z")
		if hasAX || hasAY || hasAZ {
			return ui.Rotation3D(ang, ox, oy, ax, ay, az), true
		}
		return ui.Rotation(ang, ox, oy), true
	case "Scale":
		sx, hasX := num("xScale")
		sy, hasY := num("yScale")
		if !hasX {
			sx = 1
		}
		if !hasY {
			sy = 1
		}
		ox, _ := num("origin.x")
		oy, _ := num("origin.y")
		return ui.ScaleXY(sx, sy, ox, oy), true
	}
	return ui.Transform{}, false
}
