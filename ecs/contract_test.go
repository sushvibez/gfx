package ecs

import (
	"testing"

	"github.com/sushvibez/gfx/gpu"
)

// fakeColumn is a GPUColumn whose device calls are counted instead of
// made, so the capacity and dirty-range bookkeeping run without a GPU (and
// on js).
type fakeColumn struct {
	*GPUColumn[particleRow]
	buffers  int // buffers made
	bufBytes int // the current buffer's size
	uploaded int // bytes written in all
	gpuRows  []particleRow
}

type particleRow struct{ X, Y, DX, DY float32 }

func newFakeColumn(comp *Component[particleRow], maxRows int) *fakeColumn {
	f := &fakeColumn{}
	f.GPUColumn = &GPUColumn[particleRow]{comp: comp, maxRows: maxRows, dirty: rowSpan{lo: -1}}
	f.alloc = func(bytes int) {
		f.buffers++
		f.bufBytes = bytes
		f.gpuRows = make([]particleRow, bytes/16) // a new buffer is zeroed
	}
	f.write = func(_ *gpu.Queue, off int, data []byte) {
		if off%16 != 0 || len(data)%16 != 0 || off+len(data) > f.bufBytes {
			panic("write outside the buffer or off a row boundary")
		}
		f.uploaded += len(data)
		// What the GPU holds follows the column rows the bytes came from.
		copy(f.gpuRows[off/16:], comp.Column()[off/16:(off+len(data))/16])
	}
	return f
}

// same reports whether the GPU's live rows equal the column.
func (f *fakeColumn) same(t *testing.T, what string) {
	t.Helper()
	col := f.comp.Column()
	if f.Rows() != len(col) {
		t.Fatalf("%s: mirror holds %d rows, column has %d", what, f.Rows(), len(col))
	}
	for i, v := range col {
		if f.gpuRows[i] != v {
			t.Fatalf("%s: GPU row %d is %v, column has %v", what, i, f.gpuRows[i], v)
		}
	}
}

// A column that grows a row between every sync used to make a buffer and
// upload the whole column each time (1000 rows: 1000 buffers, 8,008,000
// bytes). With geometric capacity it makes a logarithmic number of buffers
// and uploads a small multiple of the data.
func TestGPUColumnGrowsGeometrically(t *testing.T) {
	w := NewWorld()
	parts := NewComponent[particleRow](w)
	col := newFakeColumn(parts, 1<<30)
	const n = 1000
	gens := map[uint32]bool{}
	for i := range n {
		parts.Add(w.Spawn(), particleRow{X: float32(i)})
		if !col.Sync(nil) {
			t.Fatalf("row %d: nothing uploaded", i)
		}
		gens[col.Generation()] = true
		if col.Capacity() < col.Rows() || col.bufBytes != col.Capacity()*16 {
			t.Fatalf("row %d: capacity %d rows, buffer %d bytes, %d rows live", i, col.Capacity(), col.bufBytes, col.Rows())
		}
	}
	col.same(t, "after growth")
	// 1, 2, 4 ... 1024: eleven buffers.
	if col.buffers != 11 || len(gens) != col.buffers {
		t.Errorf("%d buffers made (%d generations), want 11", col.buffers, len(gens))
	}
	// Each row once, plus the full upload after each of the ten regrowths.
	t.Logf("%d rows one at a time: %d buffers, %d bytes uploaded", n, col.buffers, col.uploaded)
	if limit := 3 * n * 16; col.uploaded > limit {
		t.Errorf("%d bytes uploaded for %d bytes of rows, want at most %d", col.uploaded, n*16, limit)
	}
	// A static column is exactly its size, as it always was.
	w2 := NewWorld()
	p2 := NewComponent[particleRow](w2)
	for range 300 {
		p2.Add(w2.Spawn(), particleRow{})
	}
	c2 := newFakeColumn(p2, 1<<30)
	c2.Sync(nil)
	if c2.Capacity() != 300 || c2.bufBytes != 300*16 {
		t.Errorf("first buffer holds %d rows (%d bytes), want exactly 300", c2.Capacity(), c2.bufBytes)
	}
	// An empty column still gets a buffer of one row.
	c3 := newFakeColumn(NewComponent[particleRow](NewWorld()), 1<<30)
	if c3.Sync(nil) || c3.bufBytes != 16 {
		t.Errorf("empty column: buffer of %d bytes, want 16 and no upload", c3.bufBytes)
	}
}

func TestGrowCapacity(t *testing.T) {
	for _, c := range []struct{ have, need, limit, want int }{
		{0, 0, 1000, 1},     // never zero-sized
		{0, 300, 1000, 300}, // first buffer: exact
		{300, 300, 1000, 300},
		{300, 100, 1000, 300}, // shrinking is Sync's decision, not this one
		{300, 301, 1000, 600},
		{300, 900, 1000, 900},
		{600, 601, 1000, 1000}, // doubling stops at the limit
		{1000, 1001, 1000, 1001},
		{0, 2000, 1000, 2000}, // past the limit: what was asked, the device reports it
	} {
		if got := growCapacity(c.have, c.need, c.limit); got != c.want {
			t.Errorf("growCapacity(%d, %d, %d) = %d, want %d", c.have, c.need, c.limit, got, c.want)
		}
	}
}

// A buffer left mostly empty is replaced by a smaller one, but only after
// GPUColumnKeepSyncs syncs, so a column that empties and refills keeps its
// buffer.
func TestGPUColumnShrinksAfterStayingEmpty(t *testing.T) {
	w := NewWorld()
	parts := NewComponent[particleRow](w)
	col := newFakeColumn(parts, 1<<30)
	var es []Entity
	fill := func(n int) {
		for len(es) < n {
			e := w.Spawn()
			parts.Add(e, particleRow{X: float32(len(es))})
			es = append(es, e)
		}
		for len(es) > n {
			w.Despawn(es[len(es)-1])
			es = es[:len(es)-1]
		}
	}
	fill(10000)
	col.Sync(nil)
	fill(10001)
	col.Sync(nil) // 20000 rows of room
	if col.Capacity() != 20000 || col.buffers != 2 {
		t.Fatalf("capacity %d after %d buffers, want 20000 after 2", col.Capacity(), col.buffers)
	}
	// Emptying and refilling every other sync never shrinks.
	for i := range 3 * GPUColumnKeepSyncs {
		fill(100 + i%2*9900)
		col.Sync(nil)
	}
	if col.buffers != 2 {
		t.Fatalf("a column that keeps refilling made %d buffers, want 2", col.buffers)
	}
	// Staying small does, once, to twice the column.
	fill(100)
	col.Sync(nil)
	for range GPUColumnKeepSyncs - 2 {
		col.Sync(nil)
	}
	if col.buffers != 2 {
		t.Fatalf("shrunk after fewer than %d syncs", GPUColumnKeepSyncs)
	}
	gen := col.Generation()
	col.Sync(nil)
	if col.buffers != 3 || col.Capacity() != 200 || col.Generation() == gen {
		t.Fatalf("after %d small syncs: %d buffers, capacity %d, want 3 and 200", GPUColumnKeepSyncs, col.buffers, col.Capacity())
	}
	col.same(t, "after shrinking")
	for range 2 * GPUColumnKeepSyncs {
		col.Sync(nil)
	}
	if col.buffers != 3 {
		t.Errorf("a small buffer was replaced again (%d buffers)", col.buffers)
	}
}

// Two mirrors of one component each upload every change: the dirty range
// used to belong to the component, and the first mirror to sync cleared it
// for the other.
func TestGPUColumnTwoMirrorsEachSeeEveryChange(t *testing.T) {
	w := NewWorld()
	parts := NewComponent[particleRow](w)
	var es []Entity
	for i := range 8 {
		e := w.Spawn()
		parts.Add(e, particleRow{X: float32(i)})
		es = append(es, e)
	}
	a, b := newFakeColumn(parts, 1<<30), newFakeColumn(parts, 1<<30)
	a.Sync(nil)
	b.Sync(nil)

	parts.Column()[3].X = 99
	parts.MarkDirtyRow(3)
	if !a.Sync(nil) {
		t.Fatal("first mirror did not upload")
	}
	if !b.Sync(nil) {
		t.Fatal("second mirror did not upload the row the first one consumed")
	}
	if off, n := b.LastUpload(); off != 3*16 || n != 16 {
		t.Errorf("second mirror uploaded (%d, %d), want (48, 16)", off, n)
	}
	a.same(t, "first mirror")
	b.same(t, "second mirror")

	// Mirrors that sync at different rates each keep their own range.
	parts.Get(es[1]).X = 5
	parts.MarkDirtyRow(1)
	a.Sync(nil)
	parts.Get(es[6]).X = 6
	parts.MarkDirtyRow(6)
	parts.Remove(es[7]) // clamps both ranges
	parts.Add(es[2], particleRow{X: 7})
	a.Sync(nil)
	if off, n := a.LastUpload(); off != 2*16 || n != 5*16 {
		t.Errorf("first mirror uploaded (%d, %d), want rows 2 to 6", off, n)
	}
	b.Sync(nil)
	if off, n := b.LastUpload(); off != 1*16 || n != 6*16 {
		t.Errorf("second mirror uploaded (%d, %d), want rows 1 to 6", off, n)
	}
	a.same(t, "first mirror, later")
	b.same(t, "second mirror, later")
	if a.Sync(nil) || b.Sync(nil) {
		t.Error("a clean column uploaded again")
	}

	// A released mirror stops being tracked, may be released again, and
	// starts over when it is synced again.
	b.Release()
	b.Release()
	if len(parts.mirrors) != 1 || b.Buffer() != nil || b.Capacity() != 0 {
		t.Fatalf("after Release: %d mirrors tracked, capacity %d", len(parts.mirrors), b.Capacity())
	}
	parts.MarkDirtyRow(0)
	gen := b.Generation()
	if !b.Sync(nil) || b.Generation() == gen {
		t.Fatal("a released mirror did not start over on Sync")
	}
	if off, n := b.LastUpload(); off != 0 || n != 7*16 {
		t.Errorf("a revived mirror uploaded (%d, %d), want the whole column", off, n)
	}
	b.same(t, "revived mirror")
	// Release on a column that never synced is harmless.
	newFakeColumn(parts, 1<<30).Release()
	if len(parts.mirrors) != 2 {
		t.Errorf("%d mirrors tracked, want 2", len(parts.mirrors))
	}
}

// Marking rows dirty and syncing costs no allocation a frame, with two
// mirrors attached.
func TestMarkDirtyDoesNotAllocate(t *testing.T) {
	w := NewWorld()
	parts := NewComponent[particleRow](w)
	for range 64 {
		parts.Add(w.Spawn(), particleRow{})
	}
	a, b := newFakeColumn(parts, 1<<30), newFakeColumn(parts, 1<<30)
	a.Sync(nil)
	b.Sync(nil)
	i := 0
	if n := testing.AllocsPerRun(100, func() {
		i++
		parts.Column()[i%64].X++
		parts.MarkDirtyRow(i % 64)
		a.Sync(nil)
		b.Sync(nil)
	}); n != 0 {
		t.Errorf("%v allocations per marked row and sync, want 0", n)
	}
}

// The iteration rules in Component.Each: removing rows inside Each must
// not run off the shrunken column, rows added are not visited, and what is
// skipped is what the comment says.
func TestEachWhileMutating(t *testing.T) {
	w := NewWorld()
	c := NewComponent[int](w)
	for i := range 6 {
		c.Add(w.Spawn(), i)
	}
	// Despawning the entity being visited: the last row takes its place
	// and is not visited in this pass, so half are left.
	visits := 0
	c.Each(func(e Entity, _ *int) {
		visits++
		w.Despawn(e)
	})
	if visits != 3 || c.Len() != 3 {
		t.Errorf("despawn in Each: %d visits, %d rows left, want 3 and 3", visits, c.Len())
	}
	// Removing a row not yet reached: every remaining row is still
	// visited once.
	var seen []int
	c.Each(func(e Entity, v *int) {
		seen = append(seen, *v)
		if len(seen) == 1 {
			c.Remove(c.EntityAt(1))
		}
	})
	if len(seen) != 2 || c.Len() != 2 {
		t.Errorf("remove ahead in Each: visited %v, %d rows left, want 2 and 2", seen, c.Len())
	}
	// Adding inside Each: the new rows wait for the next pass.
	visits = 0
	c.Each(func(Entity, *int) {
		visits++
		c.Add(w.Spawn(), 100)
	})
	if visits != 2 || c.Len() != 4 {
		t.Errorf("add in Each: %d visits, %d rows, want 2 and 4", visits, c.Len())
	}
	// Each2 walks the smaller column; removing from it mid-walk is the
	// same rule.
	d := NewComponent[int](w)
	c.Each(func(e Entity, _ *int) { d.Add(e, 1) })
	c.Add(w.Spawn(), 0)
	visits = 0
	Each2(c, d, func(e Entity, _ *int, _ *int) {
		visits++
		d.Remove(e)
	})
	if visits != 2 || d.Len() != 2 {
		t.Errorf("remove in Each2: %d visits, %d rows left, want 2 and 2", visits, d.Len())
	}
}
