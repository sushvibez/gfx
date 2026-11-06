package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// A property change deep in the tree rebuilds the subtree that reads it
// and PATCHES it into its kept ancestors: the root element stays the same
// object, the ancestors are not rebuilt, and the new text shows.
func TestSubtreePatchKeepsAncestors(t *testing.T) {
	src := `
Item {
    width: 300; height: 200
    property int count: 0
    Rectangle {
        anchors.fill: parent
        color: "#202020"
        Column {
            anchors.centerIn: parent
            Text { text: "static"; color: "#ffffff" }
            Text { id: dyn; text: "count: " + count; color: "#ffffff" }
        }
    }
}`
	prev := noPatch
	noPatch = false // the feature under test, whatever the default
	defer func() { noPatch = prev }()
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	for i := 0; i < 4; i++ {
		u.Layout(300, 200)
	}
	root := tree
	says := func(want string) bool {
		found := false
		walkTree(tree, func(e ui.Element) {
			if s, ok := ui.TextOf(e); ok && s == want {
				found = true
			}
		})
		return found
	}
	if !says("count: 0") {
		t.Fatal("initial text missing")
	}
	d.doc.freshBuilds, d.doc.patched = 0, 0
	if err := env.vars["count"].(Ref).set(7); err != nil {
		t.Fatal(err)
	}
	u.Layout(300, 200)
	if !says("count: 7") {
		t.Fatalf("the text did not follow the property (fresh=%d patched=%d)", d.doc.freshBuilds, d.doc.patched)
	}
	if tree != root {
		t.Errorf("the root element was rebuilt; the change should have been patched into it")
	}
	if d.doc.patched == 0 {
		t.Errorf("nothing was patched (fresh=%d)", d.doc.freshBuilds)
	}
	t.Logf("fresh=%d patched=%d", d.doc.freshBuilds, d.doc.patched)
	if d.doc.freshBuilds > 2 {
		t.Errorf("%d subtrees rebuilt for one changed Text; want the Text (and at most its Column)", d.doc.freshBuilds)
	}
}

// The same, through a component USE SITE — which is where most of the
// work in a real page is, and where it was all being repeated.
//
// A use site's build is not cheap: it resolves the component, merges the
// use-site attributes into a derived node, declares the instance's
// properties and binds its scope. On the corpus' buttons page that was
// 58 % of a frame, run twice over (the card, and the styled button
// inside it) every frame the pulse animation ticked — to arrive back at
// the elements it already had.
//
// It is skippable because the instance's own subtree patches in place
// and comes back as the SAME element, which the use site already
// finished (layout hints, wrappers, registration). So the use site
// keeps its element and only the leaf that changed rebuilds.
func TestSubtreePatchThroughUseSite(t *testing.T) {
	src := `
Item {
    width: 300; height: 200
    property int count: 0
    component Card: Rectangle {
        color: "#202020"
        Column {
            Text { text: "static"; color: "#ffffff" }
            Text { text: "count: " + count; color: "#ffffff" }
        }
    }
    Card { anchors.fill: parent }
}`
	prev := noPatch
	noPatch = false
	defer func() { noPatch = prev }()
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	for i := 0; i < 4; i++ {
		u.Layout(300, 200)
	}
	says := func(want string) bool {
		found := false
		walkTree(tree, func(e ui.Element) {
			if s, ok := ui.TextOf(e); ok && s == want {
				found = true
			}
		})
		return found
	}
	if !says("count: 0") {
		t.Fatal("initial text missing")
	}
	// The instance's own element, under the anchored-child wrapper the
	// use site's `anchors.fill` puts around it (that wrapper IS allowed
	// to be a new object — patch swaps it into the kept parent).
	rootEl := tree
	card := ui.Children(ui.Children(tree)[0])[0]
	d.doc.freshBuilds, d.doc.patched = 0, 0
	if err := env.vars["count"].(Ref).set(7); err != nil {
		t.Fatal(err)
	}
	u.Layout(300, 200)
	if !says("count: 7") {
		t.Fatalf("the text did not follow the property (fresh=%d patched=%d)", d.doc.freshBuilds, d.doc.patched)
	}
	if tree != rootEl {
		t.Errorf("the root element was rebuilt")
	}
	if got := ui.Children(ui.Children(tree)[0])[0]; got != card {
		t.Errorf("the component instance was rebuilt (fresh=%d patched=%d); "+
			"the change should have been patched into it", d.doc.freshBuilds, d.doc.patched)
	}
	t.Logf("fresh=%d patched=%d", d.doc.freshBuilds, d.doc.patched)
	// ONE: the Text that changed. Without the use site as a slot this
	// was two — the Text, and the whole use site above it.
	if d.doc.freshBuilds > 1 {
		t.Errorf("%d subtrees rebuilt for one changed Text inside a component; "+
			"the use site should have been patched, not rebuilt", d.doc.freshBuilds)
	}
}
