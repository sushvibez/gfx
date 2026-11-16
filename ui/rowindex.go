package ui

import (
	"math"
	"math/bits"
	"slices"
)

// rowIndex holds what a variable-height list knows about its rows' sizes,
// arranged so the three questions a layout asks (how long is the content,
// which row is at this offset, where does row i start) cost O(log n)
// instead of a walk over every row, and so one row changing size costs
// O(log n) to record.
//
// A row is either known (given by Heights, or measured when it was built)
// or not; a row that is not known takes the list's estimate. The estimate
// and the spacing are not stored: the index keeps two Fenwick trees, the
// sum of the known sizes and the count of known rows, and the start of row
// k is
//
//	sum(k) + (k - known(k))*estimate + k*gap
//
// so a list whose estimate or spacing changes between frames needs no
// rebuild. Sums are float64: they are exact for pixel-sized float32 rows
// (the order of summation does not matter), where a float32 running sum
// over a hundred thousand rows is not.
//
// Appending rows and changing one are O(log n); inserting or removing in
// the middle moves the rows after it and rebuilds the trees, O(n), once
// per edit rather than once per frame.
type rowIndex struct {
	vals  []float32 // row i's size; NaN where it is not known
	sum   []float64 // Fenwick tree over the known sizes; element 0 is unused
	known []int32   // Fenwick tree over "row is known"; element 0 is unused
}

// unknownRow marks a row whose size has not been given or measured.
var unknownRow = float32(math.NaN())

func rowKnown(v float32) bool { return v == v }

func (x *rowIndex) len() int { return len(x.vals) }

// contrib is what a row adds to the two trees.
func contrib(v float32) (float64, int32) {
	if rowKnown(v) {
		return float64(v), 1
	}
	return 0, 0
}

// resize makes the index n rows long: rows past the end are dropped, new
// rows are added unknown.
func (x *rowIndex) resize(n int) {
	old := len(x.vals)
	switch {
	case n == old:
		return
	case n < old:
		// A Fenwick node only covers rows at or before it, so the trees of
		// a shorter list are a prefix of the trees of the longer one.
		x.vals = x.vals[:n]
		x.sum = x.sum[:n+1]
		x.known = x.known[:n+1]
		return
	}
	x.vals = slices.Grow(x.vals, n-old)[:n]
	for i := old; i < n; i++ {
		x.vals[i] = unknownRow
	}
	if len(x.sum) == 0 {
		x.sum = append(x.sum, 0)
		x.known = append(x.known, 0)
	}
	x.sum = slices.Grow(x.sum, n-old)[:n+1]
	x.known = slices.Grow(x.known, n-old)[:n+1]
	if add := n - old; old > 0 && add > 64 && add > n/16 {
		x.rebuild()
		return
	}
	// A new node is the sum of its span, and the rows before the new one
	// in that span are already summed by the nodes under it.
	for i := old + 1; i <= n; i++ {
		var s float64
		var k int32
		if old > 0 {
			for j, stop := i-1, i-(i&-i); j > stop; j -= j & -j {
				s += x.sum[j]
				k += x.known[j]
			}
		}
		x.sum[i], x.known[i] = s, k
	}
}

// rebuild recomputes both trees from vals, O(n).
func (x *rowIndex) rebuild() {
	n := len(x.vals)
	for i, v := range x.vals {
		x.sum[i+1], x.known[i+1] = contrib(v)
	}
	for i := 1; i <= n; i++ {
		if j := i + i&-i; j <= n {
			x.sum[j] += x.sum[i]
			x.known[j] += x.known[i]
		}
	}
}

// set records row i's size (unknownRow to forget it).
func (x *rowIndex) set(i int, v float32) {
	old := x.vals[i]
	if math.Float32bits(old) == math.Float32bits(v) {
		return
	}
	x.vals[i] = v
	s0, k0 := contrib(old)
	s1, k1 := contrib(v)
	ds, dk := s1-s0, k1-k0
	for j := i + 1; j <= len(x.vals); j += j & -j {
		x.sum[j] += ds
		x.known[j] += dk
	}
}

// setPrefix makes the leading rows equal to h, which must not be longer
// than the index. The comparison is a plain scan; only rows that differ
// are written, and a slice that differs in many places is taken whole.
func (x *rowIndex) setPrefix(h []float32) {
	limit := len(x.vals)/16 + 8
	changed := 0
	for i, v := range h {
		if math.Float32bits(x.vals[i]) == math.Float32bits(v) {
			continue
		}
		if changed < limit {
			x.set(i, v)
		} else {
			x.vals[i] = v
		}
		changed++
	}
	if changed > limit {
		x.rebuild()
	}
}

// forget makes rows [from, to) unknown.
func (x *rowIndex) forget(from, to int) {
	to = min(to, len(x.vals))
	if to-from > len(x.vals)/16+8 {
		for i := from; i < to; i++ {
			x.vals[i] = unknownRow
		}
		x.rebuild()
		return
	}
	for i := from; i < to; i++ {
		x.set(i, unknownRow)
	}
}

// insert opens n unknown rows at row at; the rows from there on move down.
func (x *rowIndex) insert(at, n int) {
	old := len(x.vals)
	x.resize(old + n)
	copy(x.vals[at+n:], x.vals[at:old])
	for i := at; i < at+n; i++ {
		x.vals[i] = unknownRow
	}
	x.rebuild()
}

// remove drops n rows at row at; the rows after them move up.
func (x *rowIndex) remove(at, n int) {
	old := len(x.vals)
	copy(x.vals[at:], x.vals[at+n:])
	x.vals = x.vals[:old-n]
	x.sum = x.sum[:old-n+1]
	x.known = x.known[:old-n+1]
	x.rebuild()
}

// size is row i's size, the estimate when it is not known.
func (x *rowIndex) size(i int, est float32) float32 {
	if v := x.vals[i]; rowKnown(v) {
		return v
	}
	return est
}

// top is where row k starts: the sizes of the k rows before it and a gap
// after each. top(len) is one gap past the end of the last row.
func (x *rowIndex) top(k int, est, gap float32) float64 {
	var s float64
	var n int32
	for j := k; j > 0; j -= j & -j {
		s += x.sum[j]
		n += x.known[j]
	}
	return s + float64(k-int(n))*float64(est) + float64(k)*float64(gap)
}

// find is the largest k in [0, len] with top(k) < at, or with top(k) <= at
// when through is set. Row starts must not decrease (no negative sizes),
// which is what makes the descent a binary search.
func (x *rowIndex) find(at float64, through bool, est, gap float32) int {
	n := len(x.vals)
	if n == 0 {
		return 0
	}
	e, g := float64(est), float64(gap)
	pos := 0
	var acc float64
	for step := 1 << (bits.Len(uint(n)) - 1); step > 0; step >>= 1 {
		next := pos + step
		if next > n {
			continue
		}
		// The node at next covers the step rows after pos.
		v := acc + x.sum[next] + float64(step-int(x.known[next]))*e + float64(step)*g
		if v < at || (through && v == at) {
			pos, acc = next, v
		}
	}
	return pos
}
