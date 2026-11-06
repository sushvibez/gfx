package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestItemTransforms: `rotation`, `scale` and a `transform:` list reach the
// element in QML's order, and layout is untouched — a rotated item keeps its
// unrotated box, which is why rotating a card does not disturb its siblings.
func TestItemTransforms(t *testing.T) {
	src := `
Item {
    width: 200; height: 200
    Rectangle { id: turned; width: 40; height: 20; rotation: 90 }
    Rectangle { id: big; width: 40; height: 20; scale: 2 }
    Rectangle {
        id: dot
        width: 10; height: 10
        transform: [
            Translate { y: -50 },
            Rotation { angle: 90; origin.x: 5; origin.y: 5 }
        ]
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)

	div := func(id string) *ui.DivEl {
		el, ok := d.doc.elems.get(d.doc.intern(id)).(*ui.DivEl)
		if !ok {
			t.Fatalf("%s did not build as a Div", id)
		}
		return el
	}
	// Layout is untouched by a transform, as in QML.
	if b := ui.Bounds(div("turned")); b.W != 40 || b.H != 20 {
		t.Errorf("a rotated item's layout box is %vx%v, want 40x20", b.W, b.H)
	}
	if ts := div("turned").Transforms(); len(ts) != 1 || ts[0].Angle != 90 {
		t.Errorf("rotation: %v", ts)
	}
	if ts := div("big").Transforms(); len(ts) != 1 || ts[0].SX != 2 || ts[0].SY != 2 {
		t.Errorf("scale: %v", ts)
	}
	ts := div("dot").Transforms()
	if len(ts) != 2 {
		t.Fatalf("transform list has %d entries, want 2", len(ts))
	}
	if ts[0].Kind != ui.XformTranslate || ts[0].Y != -50 {
		t.Errorf("first entry = %+v, want the Translate", ts[0])
	}
	if ts[1].Kind != ui.XformRotate || ts[1].Angle != 90 || ts[1].OX != 5 {
		t.Errorf("second entry = %+v, want the Rotation about (5,5)", ts[1])
	}
}
