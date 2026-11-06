package ui

import "testing"

// countingLeaf paints nothing but counts how often it was asked to.
type countingLeaf struct {
	n    *int
	rect Rect
	sig  uint64
}

func (c *countingLeaf) measure(*Context) (float32, float32) { return 20, 20 }
func (c *countingLeaf) layout(_ *Context, r Rect)           { c.rect = r }
func (c *countingLeaf) paint(ctx *Context) {
	if ctx.culled(c) {
		return
	}
	*c.n++
}
func (c *countingLeaf) hit(float32, float32) Element { return nil }
func (c *countingLeaf) onClick() func()              { return nil }
func (c *countingLeaf) bounds() Rect                 { return c.rect }
func (c *countingLeaf) paintSig() uint64             { return c.sig }

// TestDamageCullsCleanSubtrees: a tree rebuilt every frame (as a Go view
// is) whose only change is one leaf's signature repaints that leaf's
// column and nothing else, and reports the damage as that leaf's box.
func TestDamageCullsCleanSubtrees(t *testing.T) {
	var left, right int
	sigR := uint64(1)
	u := New(nil, nil, func() Element {
		return Div().Row().Children(
			Div().W(100).H(100).Children(&countingLeaf{n: &left, sig: 1}),
			Div().W(100).H(100).Children(&countingLeaf{n: &right, sig: sigR}),
		)
	})
	u.EnableDamage(true)
	frame := func() { u.Layout(300, 200); u.Frame(300, 200) }
	frame() // first frame: everything paints
	if left != 1 || right != 1 {
		t.Fatalf("first frame painted left=%d right=%d, want 1/1", left, right)
	}
	frame() // nothing changed: nothing paints
	if left != 1 || right != 1 {
		t.Fatalf("an unchanged frame painted left=%d right=%d, want 1/1", left, right)
	}
	sigR = 2 // the right leaf changes
	frame()
	if left != 1 || right != 2 {
		t.Fatalf("a right-only change painted left=%d right=%d, want 1/2", left, right)
	}
	// Inspect the damage of one more right-only change before painting it.
	sigR = 3
	u.Layout(300, 200)
	u.PrepareDamage()
	r, full := u.Damage()
	if full {
		t.Fatal("a one-leaf change reported a full frame")
	}
	if r.X < 99 || r.X > 101 || r.W > 24 {
		t.Fatalf("damage = %v, want the right leaf's box around x=100..120", r)
	}
}

// TestDamageFirstAndForcedFramesAreFull: the first frame, and any frame
// after ForceFullRedraw (input, resize, expose), repaint everything.
func TestDamageFirstAndForcedFramesAreFull(t *testing.T) {
	u := New(nil, nil, func() Element { return Div().W(100).H(100) })
	u.EnableDamage(true)
	u.Layout(300, 200)
	u.PrepareDamage()
	if _, full := u.Damage(); !full {
		t.Fatal("the first frame must be full")
	}
	u.Frame(300, 200)
	u.Layout(300, 200)
	u.PrepareDamage()
	if r, full := u.Damage(); full || r.W != 0 {
		t.Fatalf("an unchanged frame reported damage %v full=%v, want empty", r, full)
	}
	u.Frame(300, 200)
	u.ForceFullRedraw()
	u.Layout(300, 200)
	u.PrepareDamage()
	if _, full := u.Damage(); !full {
		t.Fatal("ForceFullRedraw did not make the next frame full")
	}
}
