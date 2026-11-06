package gml

import (
	"fmt"
	"github.com/sushvibez/gfx/ui"
	"strings"
	"testing"
)

// TestSubtreeDirtyTracking proves a property change re-evaluates only the
// subtrees that read it: the whole-document gate opens (revision changed),
// but unrelated cached subtrees are reused — same element identity, no
// fresh build.
func TestSubtreeDirtyTracking(t *testing.T) {
	src := `
Column {
    property int a: 1
    property int b: 2
    Text { id: ta; text: "a is " + a; color: "#ffffff" }
    Text { id: tb; text: "b is " + b; color: "#ffffff" }
    Rectangle { id: rs; width: 40; height: 40; color: "#3b5bdb" }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	view := d.View()
	view() // first build populates the caches

	for _, id := range []string{"ta", "tb", "rs"} {
		if d.doc.subtrees.get(d.doc.intern(id)) == nil {
			t.Fatalf("subtree %q not cached after first build", id)
		}
	}
	taEl := d.doc.subtrees.get(d.doc.intern("ta")).el
	tbEl := d.doc.subtrees.get(d.doc.intern("tb")).el
	rsEl := d.doc.subtrees.get(d.doc.intern("rs")).el

	// Idle frame: the whole-document gate short-circuits — no fresh builds.
	d.doc.freshBuilds = 0
	view()
	if d.doc.freshBuilds != 0 {
		t.Fatalf("idle frame rebuilt %d subtrees, want 0", d.doc.freshBuilds)
	}

	// Change a: ta (reads a) and the root Column rebuild; tb and rs reuse.
	if err := d.doc.props["a"].set(5); err != nil {
		t.Fatalf("set a: %v", err)
	}
	d.doc.freshBuilds = 0
	if el := view(); el == nil {
		t.Fatal("nil view after set")
	}
	if d.doc.freshBuilds == 0 {
		t.Fatal("changing a rebuilt nothing — change was not observed")
	}
	if d.doc.freshBuilds > 2 {
		t.Fatalf("changing a rebuilt %d subtrees, want <= 2 (root + ta)", d.doc.freshBuilds)
	}
	if d.doc.subtrees.get(d.doc.intern("ta")).el == taEl {
		t.Fatal("ta reads a but was reused after a changed")
	}
	if d.doc.subtrees.get(d.doc.intern("tb")).el != tbEl {
		t.Fatal("tb does not read a but was rebuilt")
	}
	if d.doc.subtrees.get(d.doc.intern("rs")).el != rsEl {
		t.Fatal("rs reads nothing reactive but was rebuilt")
	}
}

// TestSubtreeGeometryTracked: a subtree whose expression reads layout
// geometry (id.width) depends on THAT item's geometry token — provably
// fresh while the item stays put, rebuilt the frame after it moves.
func TestSubtreeGeometryTracked(t *testing.T) {
	src := `
Column {
    property int a: 1
    Rectangle { id: anchorbox; width: 100; height: 30; color: "#333333" }
    Rectangle { id: follower; width: anchorbox.width; height: 10; color: "#666666" }
    Text { id: plain; text: "static"; color: "#ffffff" }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	for i := 0; i < 3; i++ {
		u.Layout(300, 300) // geometry reads settle a frame behind
	}
	e := d.doc.subtrees.get(d.doc.intern("follower"))
	if e == nil {
		t.Fatal("follower subtree not cached")
	}
	if e.rs.Stale() {
		t.Fatal("nothing moved, yet the geometry-reading subtree is stale")
	}
	if p := d.doc.subtrees.get(d.doc.intern("plain")); p == nil || p.rs.Stale() {
		t.Fatal("static text subtree should have a clean, provable read-set")
	}
	// Move the item it follows: the next frame's beginFrame stamps the
	// token, the follower goes stale and rebuilds at the new width.
	d.doc.setAttrOverride(d.doc.intern("anchorbox"), "width", float64(200))
	u.Layout(300, 300)
	u.Layout(300, 300)
	if w := ui.Bounds(d.doc.subtrees.get(d.doc.intern("follower")).el).W; w != 200 {
		t.Fatalf("follower width = %v after the anchor grew, want 200", w)
	}
}

// BenchmarkDirtyRebuild measures the frame cost of ONE property change in a
// wide document (200 bound Texts): per-subtree dirty-tracking should rebuild
// ~2 subtrees (root + the hot one), not all 200.
func BenchmarkDirtyRebuild(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("Column {\n    property int hot: 0\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "    property int p%d: %d\n", i, i)
	}
	sb.WriteString(`    Text { id: thot; text: "hot " + hot; color: "#ffffff" }` + "\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "    Text { id: t%d; text: \"v \" + p%d; color: \"#ffffff\" }\n", i, i)
	}
	sb.WriteString("}\n")
	d, err := LoadString(sb.String(), NewEnv())
	if err != nil {
		b.Fatalf("LoadString: %v", err)
	}
	view := d.View()
	view()
	hot := d.doc.props["hot"]
	d.doc.freshBuilds = 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hot.set(i + 1)
		view()
	}
	b.StopTimer()
	b.ReportMetric(float64(d.doc.freshBuilds)/float64(b.N), "rebuilds/frame")
}
