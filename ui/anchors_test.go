package ui

import "testing"

func rectEq(a, b Rect) bool {
	return a.X == b.X && a.Y == b.Y && a.W == b.W && a.H == b.H
}

func TestAnchorResolve(t *testing.T) {
	parent := Rect{0, 0, 400, 300}
	resolved := map[string]Rect{"parent": parent}

	cases := []struct {
		name string
		a    Anchors
		want Rect
	}{
		{"fill", Anchors{Fill: "parent"}, Rect{0, 0, 400, 300}},
		{"fill+margins", Anchors{Fill: "parent", Margins: 10}, Rect{10, 10, 380, 280}},
		{"centerIn", Anchors{CenterIn: "parent"}, Rect{180, 140, 40, 20}},
		{"topLeft+margins",
			Anchors{Left: &AnchorLink{"parent", EdgeLeft}, Top: &AnchorLink{"parent", EdgeTop}, Margins: 12},
			Rect{12, 12, 40, 20}},
		{"left+right stretch",
			Anchors{Left: &AnchorLink{"parent", EdgeLeft}, Right: &AnchorLink{"parent", EdgeRight}},
			Rect{0, 0, 400, 20}},
		{"hcenter", Anchors{HCenter: &AnchorLink{"parent", EdgeHCenter}}, Rect{180, 0, 40, 20}},
		{"manual xy", Anchors{X: 30, Y: 40, HasX: true, HasY: true}, Rect{30, 40, 40, 20}},
	}
	for _, c := range cases {
		got := c.a.resolve(40, 20, resolved, parent)
		if !rectEq(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestAnchorResolveSibling(t *testing.T) {
	parent := Rect{0, 0, 400, 300}
	// box occupies the top-left; item anchors its left edge to box's right.
	resolved := map[string]Rect{"parent": parent, "box": {10, 10, 60, 30}}
	a := Anchors{Left: &AnchorLink{"box", EdgeRight}, Top: &AnchorLink{"box", EdgeTop}, LeftMargin: 5}
	got := a.resolve(50, 30, resolved, parent)
	if !rectEq(got, Rect{75, 10, 50, 30}) { // 70 + 5 margin
		t.Fatalf("sibling anchor = %+v", got)
	}
}

func TestAnchorLayoutFixpoint(t *testing.T) {
	// b anchors to a, which anchors to parent — resolution must order them.
	a := Anchored(Div().Size(40, 40), "a",
		Anchors{Left: &AnchorLink{"parent", EdgeLeft}, Top: &AnchorLink{"parent", EdgeTop}, Margins: 10})
	b := Anchored(Div().Size(20, 20), "b",
		Anchors{Left: &AnchorLink{"a", EdgeRight}, Top: &AnchorLink{"a", EdgeTop}})
	// Declare b BEFORE a so the fixpoint loop (not declaration order) decides.
	root := Div().AnchorMode().Children(b, a)

	ctx := &Context{}
	root.layout(ctx, Rect{0, 0, 200, 200})

	if !rectEq(a.bounds(), Rect{10, 10, 40, 40}) {
		t.Fatalf("a = %+v, want {10 10 40 40}", a.bounds())
	}
	if !rectEq(b.bounds(), Rect{50, 10, 20, 20}) { // a.right=50, a.top=10
		t.Fatalf("b = %+v, want {50 10 20 20}", b.bounds())
	}
}

// QML's anchors.horizontalCenterOffset / verticalCenterOffset nudge an item
// off the centre it is anchored to. Ignoring them left the portal page's
// swirl 40px below where Qt draws it — the page centres it and then lifts
// it.
func TestAnchorCentreOffsets(t *testing.T) {
	parent := Rect{0, 0, 200, 100}
	a := Anchors{
		HCenter:       &AnchorLink{Target: "parent", Edge: EdgeHCenter},
		VCenter:       &AnchorLink{Target: "parent", Edge: EdgeVCenter},
		HCenterOffset: 10,
		VCenterOffset: -40,
	}
	resolved := map[string]Rect{"parent": parent}
	got := a.resolve(40, 20, resolved, parent)
	want := Rect{80 + 10, 40 - 40, 40, 20}
	if got != want {
		t.Errorf("centred with offsets = %v, want %v", got, want)
	}
	// centerIn takes them too.
	ci := Anchors{CenterIn: "parent", HCenterOffset: 5, VCenterOffset: 5}
	got = ci.resolve(40, 20, resolved, parent)
	if want := (Rect{85, 45, 40, 20}); got != want {
		t.Errorf("centerIn with offsets = %v, want %v", got, want)
	}
}
