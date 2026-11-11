package ui

import (
	"testing"

	"github.com/sushvibez/gfx/render"
)

// paintCounter is a leaf that counts its paints and draws nothing.
type paintCounter struct {
	h     float32
	rect  Rect
	count *int
}

func (p *paintCounter) measure(*Context) (float32, float32) { return 100, p.h }
func (p *paintCounter) layout(_ *Context, r Rect)           { p.rect = r }
func (p *paintCounter) paint(*Context)                      { *p.count++ }
func (p *paintCounter) hit(x, y float32) Element {
	if p.rect.contains(x, y) {
		return p
	}
	return nil
}
func (p *paintCounter) onClick() func() { return nil }
func (p *paintCounter) bounds() Rect    { return p.rect }

// scrollRows builds a 200px-tall scroll of n 20px rows, each a Div
// wrapping a counting leaf.
func scrollRows(st *ScrollState, n int, painted *int, leaves *[]*paintCounter) func() Element {
	return func() Element {
		*leaves = (*leaves)[:0]
		rows := make([]Element, n)
		for i := range rows {
			leaf := &paintCounter{h: 20, count: painted}
			*leaves = append(*leaves, leaf)
			rows[i] = Div().Children(leaf)
		}
		return Div().Col().Children(Scroll(st).H(200).W(300).Children(rows...))
	}
}

// TestScrollCullsOffscreenRows: a scroll over 2000 rows records only the
// rows that touch its viewport, at the top and after scrolling, while
// hit testing still reaches a row anywhere it is visible.
func TestScrollCullsOffscreenRows(t *testing.T) {
	r := &render.Renderer2D{}
	st := &ScrollState{}
	painted := 0
	var leaves []*paintCounter
	u := New(r, nil, scrollRows(st, 2000, &painted, &leaves))
	paintOnce := func() int {
		painted, ScrollCulled = 0, 0
		u.Layout(400, 300)
		u.root.paint(&u.ctx)
		return painted
	}
	// 200px of 20px rows: 10 rows, plus up to two the cull margin touches.
	if got := paintOnce(); got < 10 || got > 12 {
		t.Fatalf("at the top: painted %d rows of 2000, want about 10", got)
	}
	if ScrollCulled < 1980 {
		t.Errorf("ScrollCulled = %d, want the ~1990 off-screen rows", ScrollCulled)
	}
	st.Y = 20 * 1000
	if got := paintOnce(); got < 10 || got > 12 {
		t.Fatalf("scrolled to row 1000: painted %d rows, want about 10", got)
	}
	// Hit testing is untouched by culling: row 1003 sits at y = 60.
	if el := u.root.hit(50, 70); el != leaves[1003] {
		t.Errorf("hit(50,70) = %v, want row 1003's leaf", el)
	}
}

// TestScrollCullKeepsOverflowingSubtree: a row whose own box is scrolled
// out of view still paints when a descendant overflows into the viewport.
func TestScrollCullKeepsOverflowingSubtree(t *testing.T) {
	r := &render.Renderer2D{}
	st := &ScrollState{}
	painted := 0
	leaf := &paintCounter{h: 600, count: &painted}
	u := New(r, nil, func() Element {
		// A 20px row holding a 600px child, then filler to scroll over.
		return Scroll(st).H(200).W(300).Children(
			Div().Size(100, 20).Children(leaf),
			Div().Size(100, 1000),
		)
	})
	u.Layout(400, 300)
	st.Y = 300 // the row is now 300px above the view, its child reaches in
	u.Layout(400, 300)
	row := leaf.rect
	if row.Y+row.H <= 0 || row.Y >= 0 {
		t.Fatalf("fixture: leaf at %v should straddle the viewport top", row)
	}
	u.root.paint(&u.ctx)
	if painted != 1 {
		t.Errorf("overflowing descendant painted %d times, want 1", painted)
	}
}

// BenchmarkScrollPaint2000Rows: painting a 2000-row scroll (row = a
// filled Div with a filled child) with culling, versus the same rows
// laid out in a plain column (no viewport, so nothing can be culled).
func BenchmarkScrollPaint2000Rows(b *testing.B) {
	rows := func() []Element {
		els := make([]Element, 2000)
		for i := range els {
			els[i] = Div().H(20).Bg(RGBA{R: 0.2, A: 1}).Children(Div().Size(80, 16).Bg(RGBA{G: 0.5, A: 1}))
		}
		return els
	}
	for _, mode := range []string{"scroll", "column"} {
		b.Run(mode, func(b *testing.B) {
			r := &render.Renderer2D{}
			st := &ScrollState{}
			u := New(r, nil, func() Element {
				if mode == "scroll" {
					return Scroll(st).H(200).W(300).Children(rows()...)
				}
				return Div().Col().W(300).Children(rows()...)
			})
			u.Layout(400, 300)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r.Begin(400, 300)
				u.root.paint(&u.ctx)
			}
		})
	}
}
