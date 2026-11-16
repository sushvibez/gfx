package ui

import "testing"

// benchListEnd lays out a variable-height list scrolled to its end: the
// case where finding and placing the visible rows walks the most rows
// ahead of them.
func benchListEnd(b *testing.B, count int, measured bool) {
	st := &ListState{}
	var heights []float32
	if !measured {
		heights = make([]float32, count)
		for i := range heights {
			heights[i] = float32(20 + i%3*8)
		}
	}
	row := Div().Size(200, 28)
	mk := func() *ListEl {
		l := List(st, 24, count, func(int) Element { return row })
		if measured {
			return l.Measured()
		}
		return l.Heights(heights)
	}
	ctx := &Context{}
	r := Rect{0, 0, 200, 600}
	mk().layout(ctx, r)
	st.SetOffset(st.MaxY())
	b.ReportAllocs()
	for b.Loop() {
		ctx.scrolls = ctx.scrolls[:0]
		mk().layout(ctx, r)
	}
}

func BenchmarkListHeightsEnd10k(b *testing.B)   { benchListEnd(b, 10000, false) }
func BenchmarkListHeightsEnd100k(b *testing.B)  { benchListEnd(b, 100000, false) }
func BenchmarkListMeasuredEnd10k(b *testing.B)  { benchListEnd(b, 10000, true) }
func BenchmarkListMeasuredEnd100k(b *testing.B) { benchListEnd(b, 100000, true) }

// Rows of a variable-height list land where summing the heights before
// them puts them, at any scroll position.
func TestListVariableRowsLandOnTheirPrefixSums(t *testing.T) {
	const count = 500
	heights := make([]float32, count)
	tops := make([]float32, count)
	var y float32
	for i := range heights {
		heights[i] = float32(20 + i%3*8)
		tops[i] = y
		y += heights[i] + 3
	}
	st := &ListState{}
	rows := map[int]*DivEl{}
	mk := func() *ListEl {
		clear(rows)
		return List(st, 24, count, func(i int) Element {
			d := Div()
			rows[i] = d
			return d
		}).Heights(heights).Spacing(3)
	}
	r := Rect{0, 0, 200, 300}
	mk().layout(&Context{}, r)
	for _, off := range []float32{0, 137, 4000, st.MaxY()} {
		st.SetOffset(off)
		el := mk()
		el.layout(&Context{}, r)
		first, n := el.VisibleRange()
		if n == 0 {
			t.Fatalf("offset %v: no rows built", off)
		}
		for i := first; i < first+n; i++ {
			got := rows[i].Rect()
			if got.Y != tops[i]-st.Offset() || got.H != heights[i] {
				t.Errorf("offset %v row %d at y %v h %v, want y %v h %v", off, i, got.Y, got.H, tops[i]-st.Offset(), heights[i])
			}
		}
	}
}

// benchListRev is benchListEnd with HeightsRev: the slice is vouched for,
// so nothing in the layout is proportional to the row count. With edit, one
// row changes height every frame and the state is told (SetRowHeight).
func benchListRev(b *testing.B, count int, edit bool) {
	st := &ListState{}
	heights := make([]float32, count)
	for i := range heights {
		heights[i] = float32(20 + i%3*8)
	}
	row := Div().Size(200, 28)
	var rev uint64
	mk := func() *ListEl {
		return List(st, 24, count, func(int) Element { return row }).HeightsRev(heights, rev)
	}
	ctx := &Context{}
	r := Rect{0, 0, 200, 600}
	mk().layout(ctx, r)
	st.SetOffset(st.MaxY())
	b.ReportAllocs()
	n := 0
	for b.Loop() {
		ctx.scrolls = ctx.scrolls[:0]
		if edit {
			n++
			i := n * 7919 % count
			heights[i] = float32(20 + n%5*4)
			st.SetRowHeight(i, heights[i])
		}
		mk().layout(ctx, r)
	}
}

func BenchmarkListHeightsRevEnd10k(b *testing.B)      { benchListRev(b, 10000, false) }
func BenchmarkListHeightsRevEnd100k(b *testing.B)     { benchListRev(b, 100000, false) }
func BenchmarkListHeightsRevEditEnd100k(b *testing.B) { benchListRev(b, 100000, true) }
