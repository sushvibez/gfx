package gml

import "testing"

// TestShapeRadialGradientKeepsItsCirclesAndStops: a ShapePath's
// RadialGradient paints by DISTANCE from its focal circle, with every stop
// — the canvas page's lit sphere is white → amber → orange with the focal
// point offset from the centre, and painting the two end colours along the
// item diagonal made it a flat wash of pale peach.
func TestShapeRadialGradientKeepsItsCirclesAndStops(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
import QtQuick.Shapes
Rectangle {
    width: 70; height: 70
    Shape {
        anchors.fill: parent
        ShapePath {
            strokeWidth: 0
            startX: 35; startY: 0
            PathArc { x: 35; y: 70; radiusX: 35; radiusY: 35; useLargeArc: true }
            PathArc { x: 35; y: 0; radiusX: 35; radiusY: 35; useLargeArc: true }
            fillGradient: RadialGradient {
                centerX: 35; centerY: 35
                centerRadius: 35
                focalX: 25; focalY: 25
                focalRadius: 0
                GradientStop { position: 0; color: "#FFFFFF" }
                GradientStop { position: 0.5; color: "#FEA601" }
                GradientStop { position: 1; color: "#FF5900" }
            }
        }
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	// `fillGradient: RadialGradient { ... }` is an element-valued
	// ATTRIBUTE, which findNode does not walk into.
	var find func(*node) *node
	find = func(n *node) *node {
		if n.typeName == "RadialGradient" {
			return n
		}
		for _, c := range n.children {
			if got := find(c); got != nil {
				return got
			}
		}
		for _, a := range n.attrs {
			if a.elem != nil {
				if got := find(a.elem); got != nil {
					return got
				}
			}
		}
		return nil
	}
	n := find(d.root)
	if n == nil {
		t.Fatal("no RadialGradient in the document")
	}
	g := shapeGradientOf(n, d.env, 70, 70)
	if g == nil || !g.ok {
		t.Fatal("the gradient did not resolve")
	}
	if !g.radial {
		t.Error("the gradient is not radial: it would paint along the item diagonal")
	}
	if g.fx != 25 || g.fy != 25 || g.r1 != 35 {
		t.Errorf("focal (%v,%v) r1 %v, want (25,25) and 35", g.fx, g.fy, g.r1)
	}
	if len(g.stops) != 3 {
		t.Fatalf("kept %d stops, want all three", len(g.stops))
	}
	if g.stops[1].Pos != 0.5 || g.stops[1].Col.G < 0.5 {
		t.Errorf("middle stop = %+v, want the amber at 0.5", g.stops[1])
	}
}

// TestShapeRadialGradientPadsBeyondItsCircle: beyond the outer circle a
// radial gradient PADS with its last stop (Qt and SVG both). The corpus'
// lit sphere is a circle of radius 35 whose gradient is centred on a focal
// point 14px off-centre, so the far crescent lies outside the gradient's
// own circle — and had nothing painted in it at all, which made the
// rendered dome 60px wide where Qt's is 70.
func TestShapeRadialGradientPadsBeyondItsCircle(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
import QtQuick.Shapes
Rectangle {
    width: 70; height: 70
    Shape {
        anchors.fill: parent
        ShapePath {
            strokeWidth: 0
            startX: 35; startY: 0
            PathArc { x: 35; y: 70; radiusX: 35; radiusY: 35; useLargeArc: true }
            PathArc { x: 35; y: 0; radiusX: 35; radiusY: 35; useLargeArc: true }
            fillGradient: RadialGradient {
                centerX: 35; centerY: 35
                centerRadius: 35
                focalX: 25; focalY: 25
                GradientStop { position: 0; color: "#FFFFFF" }
                GradientStop { position: 1; color: "#FF5900" }
            }
        }
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var find func(*node) *node
	find = func(n *node) *node {
		if n.typeName == "RadialGradient" {
			return n
		}
		for _, c := range n.children {
			if got := find(c); got != nil {
				return got
			}
		}
		for _, a := range n.attrs {
			if a.elem != nil {
				if got := find(a.elem); got != nil {
					return got
				}
			}
		}
		return nil
	}
	n := find(d.root)
	if n == nil {
		t.Fatal("no RadialGradient in the document")
	}
	g := shapeGradientOf(n, d.env, 70, 70)
	if g == nil || !g.ok || !g.radial {
		t.Fatal("the radial gradient did not resolve")
	}
	// The far corner of the shape is 1.4x the gradient's radius from the
	// focal point, so what is painted there is the LAST stop.
	last := g.stops[len(g.stops)-1].Col
	if last.R < 0.9 || last.G > 0.5 || last.B > 0.1 {
		t.Errorf("last stop = %+v, want the outer orange that pads the rest", last)
	}
	dx, dy := 70-g.fx, 70-g.fy
	if d2 := dx*dx + dy*dy; d2 <= g.r1*g.r1 {
		t.Errorf("the shape's far corner is inside the gradient circle (%v vs %v): "+
			"the test no longer exercises padding", d2, g.r1*g.r1)
	}
}
