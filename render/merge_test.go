package render

import "testing"

func boxed(kind batchKind, start, count int, x0, y0, x1, y1 float32) batch {
	return batch{kind: kind, start: start, count: count, bounded: true, bx0: x0, by0: y0, bx1: x1, by1: y1}
}

// A HUD draws panel, label, panel, label: the quads and the glyphs each
// collapse into one batch when every label is disjoint from the panels
// after it.
func TestMergeBatchesJoinsDisjointRuns(t *testing.T) {
	in := []batch{
		boxed(kindQuad, 0, 1, 0, 0, 10, 10),  // panel A
		boxed(kindGlyph, 0, 3, 1, 1, 9, 9),   // label on A
		boxed(kindQuad, 1, 1, 20, 0, 30, 10), // panel B
		boxed(kindGlyph, 3, 3, 21, 1, 29, 9), // label on B
		boxed(kindQuad, 2, 1, 40, 0, 50, 10), // panel C
		boxed(kindGlyph, 6, 2, 41, 1, 49, 9), // label on C
	}
	out := mergeBatches(in, 0.5)
	if len(out) != 2 || out[0].kind != kindQuad || out[0].count != 3 || out[1].kind != kindGlyph || out[1].count != 8 {
		t.Fatalf("got %d batches: %+v", len(out), out)
	}
	if out[0].bx1 != 50 || out[1].bx1 != 49 {
		t.Errorf("merged bounds not grown: %+v", out)
	}
}

// A label that overlaps the NEXT panel pins the draw order: the panel
// after it cannot move in front of it.
func TestMergeBatchesKeepsOverlappingOrder(t *testing.T) {
	in := []batch{
		boxed(kindQuad, 0, 1, 0, 0, 10, 10),
		boxed(kindGlyph, 0, 3, 5, 5, 25, 8), // runs into panel B
		boxed(kindQuad, 1, 1, 20, 0, 30, 10),
	}
	if out := mergeBatches(in, 0.5); len(out) != 3 {
		t.Fatalf("overlapping batches were reordered: %+v", out)
	}
	// Within the AA margin counts as touching.
	in = []batch{
		boxed(kindQuad, 0, 1, 0, 0, 10, 10),
		boxed(kindGlyph, 0, 3, 0, 10.2, 10, 12),
		boxed(kindQuad, 1, 1, 0, 10, 10, 20),
	}
	if out := mergeBatches(in, 0.5); len(out) != 3 {
		t.Fatalf("a batch within the margin was moved across: %+v", out)
	}
}

// An unbounded batch (arbitrary vertices) blocks every move across it,
// and only buffer-adjacent batches of one kind join.
func TestMergeBatchesStopsAtUnboundedAndNonAdjacent(t *testing.T) {
	in := []batch{
		boxed(kindQuad, 0, 1, 0, 0, 10, 10),
		{kind: kindSprite, start: 0, count: 6},
		boxed(kindQuad, 1, 1, 20, 0, 30, 10),
	}
	if out := mergeBatches(in, 0.5); len(out) != 3 {
		t.Fatalf("moved across an unbounded batch: %+v", out)
	}
	clipped := boxed(kindQuad, 1, 1, 20, 0, 30, 10)
	clipped.clipSlot = 1
	in = []batch{
		boxed(kindQuad, 0, 1, 0, 0, 10, 10),
		boxed(kindGlyph, 0, 3, 50, 50, 60, 60),
		clipped,
		boxed(kindQuad, 2, 1, 40, 0, 50, 10),
	}
	out := mergeBatches(in, 0.5)
	if len(out) != 4 {
		t.Fatalf("a differently clipped batch merged, or a non-adjacent one: %+v", out)
	}
}
