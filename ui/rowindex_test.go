package ui

import (
	"math/rand"
	"slices"
	"testing"
)

// linearRows is the walk the row index replaced, kept as the reference:
// every question is answered by summing rows from the first one. Sizes in
// these tests are quarter pixels, which a float32 sum carries exactly, so
// the reference and the index must agree to the bit.
type linearRows struct {
	known    []float32 // unknownRow where a row takes the estimate
	est, gap float32
}

func (l *linearRows) size(i int) float32 {
	if v := l.known[i]; rowKnown(v) {
		return v
	}
	return l.est
}

func (l *linearRows) top(k int) float32 {
	var y float32
	for i := 0; i < k; i++ {
		y += l.size(i) + l.gap
	}
	return y
}

func (l *linearRows) content() float32 {
	if len(l.known) == 0 {
		return 0
	}
	return max(l.top(len(l.known))-l.gap, 0)
}

// visible is the range ListEl.layout used to find by walking.
func (l *linearRows) visible(rowsOff, view float32) (first, last int) {
	count := len(l.known)
	first, last = 0, count
	var y float32
	for i := 0; i < count; i++ {
		h := l.size(i)
		if y+h < rowsOff {
			first = i + 1
		}
		if y > rowsOff+view {
			last = i
			break
		}
		y += h + l.gap
	}
	first = max(first-1, 0)
	last = min(last+1, count)
	return min(first, max(last, 0)), last
}

func quarter(rng *rand.Rand) float32 { return float32(rng.Intn(240)) / 4 }

// The index answers what the linear walk answers, through random sets,
// forgets, bulk rewrites, growth, truncation and edits in the middle.
func TestRowIndexMatchesLinear(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	var ix rowIndex
	ref := &linearRows{}
	check := func(step int, op string) {
		t.Helper()
		ref.est, ref.gap = quarter(rng), float32(rng.Intn(5))
		n := len(ref.known)
		if ix.len() != n {
			t.Fatalf("step %d (%s): %d rows, want %d", step, op, ix.len(), n)
		}
		// tops[k] is the linear walk's start of row k.
		tops := make([]float32, n+1)
		for i := range n {
			tops[i+1] = tops[i] + ref.size(i) + ref.gap
		}
		for range 6 {
			k := rng.Intn(n + 1)
			if got, want := ix.top(k, ref.est, ref.gap), float64(tops[k]); got != want {
				t.Fatalf("step %d (%s): top(%d) = %v, want %v (%d rows)", step, op, k, got, want, n)
			}
			if k < n && ix.size(k, ref.est) != ref.size(k) {
				t.Fatalf("step %d (%s): size(%d) = %v, want %v", step, op, k, ix.size(k, ref.est), ref.size(k))
			}
			// The two searches, against a scan.
			at := float64(tops[k]) + float64(rng.Intn(3)-1)*0.25
			below, through := 0, 0
			for j := 1; j <= n; j++ {
				y := float64(tops[j])
				if y < at {
					below = j
				}
				if y <= at {
					through = j
				}
			}
			if got := ix.find(at, false, ref.est, ref.gap); got != below {
				t.Fatalf("step %d (%s): find(<%v) = %d, want %d", step, op, at, got, below)
			}
			if got := ix.find(at, true, ref.est, ref.gap); got != through {
				t.Fatalf("step %d (%s): find(<=%v) = %d, want %d", step, op, at, got, through)
			}
		}
	}
	for step := range 4000 {
		n := len(ref.known)
		var op string
		switch r := rng.Intn(100); {
		case r < 45 && n > 0:
			op = "set"
			i, v := rng.Intn(n), quarter(rng)
			ix.set(i, v)
			ref.known[i] = v
		case r < 55 && n > 0:
			op = "forget"
			i := rng.Intn(n)
			j := min(i+rng.Intn(40), n)
			ix.forget(i, j)
			for k := i; k < j; k++ {
				ref.known[k] = unknownRow
			}
		case r < 65 && n > 0:
			op = "setPrefix"
			h := make([]float32, rng.Intn(n+1))
			copy(h, ref.known)
			for range rng.Intn(len(h)/4 + 2) {
				if len(h) > 0 {
					h[rng.Intn(len(h))] = quarter(rng)
				}
			}
			ix.setPrefix(h)
			copy(ref.known, h)
		case r < 80:
			op = "grow"
			add := rng.Intn(5)
			if rng.Intn(8) == 0 {
				add = rng.Intn(300)
			}
			ix.resize(n + add)
			for range add {
				ref.known = append(ref.known, unknownRow)
			}
		case r < 86 && n > 0:
			op = "truncate"
			m := n - rng.Intn(min(n, 50)+1)
			ix.resize(m)
			ref.known = ref.known[:m]
		case r < 93 && n > 0:
			op = "insert"
			at, k := rng.Intn(n), 1+rng.Intn(20)
			ix.insert(at, k)
			ref.known = slices.Insert(ref.known, at, slices.Repeat([]float32{unknownRow}, k)...)
		case n > 0:
			op = "remove"
			at := rng.Intn(n)
			k := 1 + rng.Intn(min(n-at, 20))
			ix.remove(at, k)
			ref.known = slices.Delete(ref.known, at, at+k)
		default:
			continue
		}
		check(step, op)
	}
	if len(ref.known) < 200 {
		t.Fatalf("the walk ended with only %d rows: it did not exercise much", len(ref.known))
	}
}

// A whole list laid out through the index puts the same rows in the same
// places as the linear walk did, with heights given (Heights and
// HeightsRev) and with heights measured, while rows change size, the model
// grows and shrinks, and rows are inserted and removed in the middle.
func TestListVariableLayoutMatchesLinearWalk(t *testing.T) {
	for _, mode := range []string{"heights", "heightsRev", "measured"} {
		t.Run(mode, func(t *testing.T) {
			rng := rand.New(rand.NewSource(7))
			const view = 300
			st := &ListState{}
			ref := &linearRows{est: 24, gap: 3}
			// sizes is the model: what each row is given or measures as.
			sizes := make([]float32, 1500)
			for i := range sizes {
				sizes[i] = 8 + quarter(rng)
			}
			var rev uint64
			rows := map[int]*DivEl{}
			mk := func() *ListEl {
				clear(rows)
				l := List(st, ref.est, len(sizes), func(i int) Element {
					d := Div().Size(200, sizes[i])
					rows[i] = d
					return d
				}).Spacing(ref.gap)
				switch mode {
				case "heights":
					return l.Heights(sizes)
				case "heightsRev":
					return l.HeightsRev(sizes, rev)
				}
				return l.Measured()
			}
			for step := range 600 {
				// One edit of the model, mirrored in the reference.
				n := len(sizes)
				switch r := rng.Intn(10); {
				case r < 3 && n > 0: // a row changes size
					i := rng.Intn(n)
					sizes[i] = 8 + quarter(rng)
					if mode == "heightsRev" && step > 0 && rng.Intn(2) == 0 {
						// Told directly: the rev stands.
						st.SetRowHeight(i, sizes[i])
						rev--
					}
					if mode == "measured" && rng.Intn(2) == 0 {
						st.InvalidateRow(i)
						if i < len(ref.known) {
							ref.known[i] = unknownRow
						}
					}
				case r < 4: // rows appended
					for range 1 + rng.Intn(30) {
						sizes = append(sizes, 8+quarter(rng))
					}
				case r < 5 && n > 50: // rows dropped from the end
					sizes = sizes[:n-rng.Intn(30)]
				case r < 6 && n > 0: // rows inserted in the middle
					at, k := rng.Intn(n), 1+rng.Intn(10)
					add := make([]float32, k)
					for i := range add {
						add[i] = 8 + quarter(rng)
					}
					sizes = slices.Insert(sizes, at, add...)
					st.InsertRows(at, k)
					if at < len(ref.known) {
						ref.known = slices.Insert(ref.known, at, slices.Repeat([]float32{unknownRow}, k)...)
					}
				case r < 7 && n > 50: // rows removed from the middle
					at := rng.Intn(n - 20)
					k := 1 + rng.Intn(10)
					sizes = slices.Delete(sizes, at, at+k)
					st.RemoveRows(at, k)
					if at < len(ref.known) {
						ref.known = slices.Delete(ref.known, at, min(at+k, len(ref.known)))
					}
				}
				rev++
				// The reference follows the row count, and for given heights
				// knows them all.
				for len(ref.known) < len(sizes) {
					ref.known = append(ref.known, unknownRow)
				}
				ref.known = ref.known[:len(sizes)]
				if mode != "measured" {
					copy(ref.known, sizes)
				}
				if rng.Intn(3) == 0 {
					st.SetOffset(float32(rng.Intn(int(st.MaxY())+200)) + float32(rng.Intn(4))/4)
				}
				if rng.Intn(40) == 0 {
					st.SetOffset(st.MaxY())
				}

				// What the linear walk makes of it.
				off := min(max(st.Offset(), 0), max(ref.content()-view, 0))
				first, last := ref.visible(off, view)
				if mode == "measured" {
					for i := first; i < last; i++ {
						ref.known[i] = sizes[i]
					}
					off = min(max(off, 0), max(ref.content()-view, 0))
				}

				el := mk()
				el.layout(&Context{}, Rect{0, 0, 200, view})
				gotFirst, gotN := el.VisibleRange()
				if gotFirst != first || gotFirst+gotN != last {
					t.Fatalf("step %d: rows [%d, %d), want [%d, %d)", step, gotFirst, gotFirst+gotN, first, last)
				}
				if got, want := st.MaxY(), max(ref.content()-view, 0); got != want {
					t.Fatalf("step %d: scroll range %v, want %v", step, got, want)
				}
				if st.Offset() != off {
					t.Fatalf("step %d: offset %v, want %v", step, st.Offset(), off)
				}
				top := ref.top(first)
				for i := first; i < last; i++ {
					got := rows[i].Rect()
					if got.Y != top-off || got.H != ref.size(i) {
						t.Fatalf("step %d: row %d at y %v h %v, want y %v h %v", step, i, got.Y, got.H, top-off, ref.size(i))
					}
					if rt, rh, ok := st.RowTop(i); !ok || rt != top || rh != ref.size(i) {
						t.Fatalf("step %d: RowTop(%d) = %v, %v, %v, want %v, %v", step, i, rt, rh, ok, top, ref.size(i))
					}
					top += ref.size(i) + ref.gap
				}
			}
		})
	}
}

// HeightsRev skips the comparison only while the slice and the rev are the
// ones it last saw: a new rev, a new slice and a different row count are
// all picked up.
func TestListHeightsRev(t *testing.T) {
	st := &ListState{}
	h := []float32{10, 20, 30, 40}
	lay := func(heights []float32, rev uint64, count int) {
		List(st, 24, count, func(int) Element { return Div() }).HeightsRev(heights, rev).layout(&Context{}, Rect{0, 0, 100, 50})
	}
	content := func() float32 { return st.scroll.contentH }
	lay(h, 1, 4)
	if content() != 100 {
		t.Fatalf("content %v, want 100", content())
	}
	h[1] = 50
	lay(h, 2, 4)
	if content() != 130 {
		t.Errorf("after a new rev: content %v, want 130", content())
	}
	other := []float32{1, 2, 3, 4}
	lay(other, 2, 4)
	if content() != 10 {
		t.Errorf("after a new slice with the old rev: content %v, want 10", content())
	}
	lay(other, 2, 3)
	if content() != 6 {
		t.Errorf("after the count shrank: content %v, want 6", content())
	}
	// Rows past the slice take the estimate.
	lay(other, 2, 6)
	if content() != 10+2*24 {
		t.Errorf("after the count grew past the slice: content %v, want 58", content())
	}
	// Plain Heights after HeightsRev compares again, whatever the rev was.
	other[0] = 11
	List(st, 24, 4, func(int) Element { return Div() }).Heights(other).layout(&Context{}, Rect{0, 0, 100, 50})
	if content() != 20 {
		t.Errorf("Heights after HeightsRev: content %v, want 20", content())
	}
}

// ScrollToRow moves the least distance that shows the row, and a list that
// stops measuring forgets what it measured.
func TestListScrollToRowAndModeChange(t *testing.T) {
	st := &ListState{}
	size := func(i int) float32 { return float32(20 + i%4*10) } // 20 30 40 50, 140 a cycle
	mk := func() *ListEl {
		return List(st, 24, 400, func(i int) Element { return Div().Size(100, size(i)) })
	}
	heights := make([]float32, 400)
	for i := range heights {
		heights[i] = size(i)
	}
	lay := func() { mk().Heights(heights).layout(&Context{}, Rect{0, 0, 100, 200}) }
	lay()
	st.ScrollToRow(100) // below the view: its end meets the view's end
	lay()
	if top, h, _ := st.RowTop(100); st.Offset() != top+h-200 || top != 25*140 {
		t.Errorf("row 100 (top %v): offset %v, want %v", top, st.Offset(), top+h-200)
	}
	st.ScrollToRow(101) // already in view? no: one row further
	if top, h, _ := st.RowTop(101); st.Offset() != top+h-200 {
		t.Errorf("row 101: offset %v, want %v", st.Offset(), top+h-200)
	}
	st.ScrollToRow(100) // in view: nothing moves
	if top, h, _ := st.RowTop(101); st.Offset() != top+h-200 {
		t.Errorf("a visible row moved the list to %v", st.Offset())
	}
	st.ScrollToRow(7) // above the view: its start meets the view's start
	if st.Offset() != 140+20+30+40 {
		t.Errorf("row 7: offset %v, want 230", st.Offset())
	}
	st.ScrollToRow(399)
	if st.Offset() != st.MaxY() {
		t.Errorf("last row: offset %v, want the end %v", st.Offset(), st.MaxY())
	}
	if _, _, ok := st.RowTop(400); ok {
		t.Error("RowTop answered for a row past the end")
	}

	// Measured rows are forgotten when the list no longer measures.
	st = &ListState{}
	mk().Measured().layout(&Context{}, Rect{0, 0, 100, 200})
	if _, h, _ := st.RowTop(1); h != 30 {
		t.Fatalf("measured row 1 is %v long, want 30", h)
	}
	mk().Heights([]float32{}).layout(&Context{}, Rect{0, 0, 100, 200})
	if _, h, _ := st.RowTop(1); h != 24 {
		t.Errorf("row 1 is %v long after measuring stopped, want the estimate 24", h)
	}
}

// The index's per-frame work allocates nothing once it has its rows.
func TestRowIndexDoesNotAllocate(t *testing.T) {
	var ix rowIndex
	ix.resize(10000)
	h := make([]float32, 10000)
	for i := range h {
		h[i] = float32(20 + i%3)
	}
	ix.setPrefix(h)
	i := 0
	if n := testing.AllocsPerRun(200, func() {
		i++
		ix.set(i%10000, float32(i%7))
		ix.setPrefix(h)
		_ = ix.top(9000, 24, 2)
		_ = ix.find(150000, true, 24, 2)
		ix.resize(10000 - i%2)
		ix.resize(10000)
	}); n != 0 {
		t.Errorf("%v allocations per frame, want 0", n)
	}
}
