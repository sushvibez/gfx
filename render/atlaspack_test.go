//go:build !js

package render

import (
	"math/rand"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/gpu"
)

// glyphSizes is the shape of a real character set: mostly x-height,
// some ascenders and caps, a few descenders, and punctuation that is
// tiny. The mix is the point — a packer that only sees one height has
// nothing to sort.
func glyphSizes(n int, size int, rnd *rand.Rand) [][2]int {
	out := make([][2]int, n)
	for i := range out {
		var hf float64
		switch r := rnd.Float64(); {
		case r < 0.12: // punctuation
			hf = 0.18
		case r < 0.55: // x-height
			hf = 0.52
		case r < 0.85: // caps and ascenders
			hf = 0.74
		default: // descenders
			hf = 0.95
		}
		h := max(2, int(float64(size)*hf))
		w := max(2, int(float64(h)*(0.5+rnd.Float64()*0.6)))
		out[i] = [2]int{w, h}
	}
	return out
}

// What a packer is for is holding glyphs, so the measurement is how
// many of them a sheet takes before it overflows — not how tightly it
// packs a set that was never going to fill it. A hundred glyphs of one
// font occupy three per cent of a sheet however they are arranged.
func TestPackerFitsMoreThanTheOpenShelf(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	for _, size := range []int{13, 20, 34, 48} {
		sizes := glyphSizes(60000, size, rnd)
		var p packer
		p.reset(atlasSize, atlasSize)
		bucketed := 0
		for _, s := range sizes {
			if _, _, ok := p.alloc(s[0], s[1]); !ok {
				break
			}
			bucketed++
		}
		open := shelfCapacity(atlasSize, atlasSize, sizes)
		if bucketed <= open {
			t.Errorf("%dpx: the bucketed packer fitted %d glyphs, the open shelf %d",
				size, bucketed, open)
		}
		t.Logf("%dpx: %d glyphs a sheet against the open shelf's %d (%+.0f%%)",
			size, bucketed, open, 100*(float64(bucketed)/float64(open)-1))
	}
}

// shelfCapacity is how many of the same glyphs the old packer fitted.
func shelfCapacity(w, h int, sizes [][2]int) int {
	penX, penY, rowH, n := 0, 0, 0, 0
	for _, s := range sizes {
		if penX+s[0]+atlasPad > w {
			penX = 0
			penY += rowH + atlasPad
			rowH = 0
		}
		if penY+s[1]+atlasPad > h {
			return n
		}
		penX += s[0] + atlasPad
		rowH = max(rowH, s[1])
		n++
	}
	return n
}

// Nothing may overlap, and nothing may land outside the sheet.
func TestPackerRectsDoNotOverlap(t *testing.T) {
	rnd := rand.New(rand.NewSource(11))
	var p packer
	p.reset(256, 256)
	type rect struct{ x0, y0, x1, y1 int }
	var placed []rect
	for _, s := range glyphSizes(600, 18, rnd) {
		x, y, ok := p.alloc(s[0], s[1])
		if !ok {
			break
		}
		if x < 0 || y < 0 || x+s[0] > 256 || y+s[1] > 256 {
			t.Fatalf("%d,%d %dx%d falls outside the sheet", x, y, s[0], s[1])
		}
		r := rect{x, y, x + s[0], y + s[1]}
		for _, o := range placed {
			if r.x0 < o.x1 && o.x0 < r.x1 && r.y0 < o.y1 && o.y0 < r.y1 {
				t.Fatalf("%v overlaps %v", r, o)
			}
		}
		placed = append(placed, r)
	}
	if len(placed) < 100 {
		t.Fatalf("only %d glyphs fitted a 256-square sheet", len(placed))
	}
}

// A sheet that is full says so rather than handing back a position on
// top of something else.
func TestPackerReportsFull(t *testing.T) {
	var p packer
	p.reset(64, 64)
	n := 0
	for {
		if _, _, ok := p.alloc(20, 20); !ok {
			break
		}
		n++
		if n > 100 {
			t.Fatal("a 64-square sheet never filled")
		}
	}
	if n == 0 {
		t.Fatal("nothing fitted at all")
	}
	if _, _, ok := p.alloc(20, 20); ok {
		t.Error("a full sheet allocated anyway")
	}
	// Too wide for the sheet at any point: refused, not wrapped.
	if _, _, ok := p.alloc(999, 4); ok {
		t.Error("a glyph wider than the sheet was placed")
	}
}

// A font whose glyphs outgrow a sheet opens another one, instead of
// dropping the glyph and leaving a permanent hole in the text — which
// is what happened before, and what the comment at that spot said would
// be fixed "with the real batcher".
//
// Sixteen sub-pixel phases of a large face is the case that reaches it:
// sixteen rasterisations of every glyph is sixteen times the sheet.
func TestSubpixelSheetsPageRatherThanDrop(t *testing.T) {
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	if !r.SubpixelTextEnabled() {
		t.Skip("no subpixel pipeline on this device")
	}
	f, err := r.NewFont(goregular.TTF, 60)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	var ascii []rune
	for c := rune(33); c < 127; c++ {
		ascii = append(ascii, c)
	}
	missing := 0
	for phase := 0; phase < subpixelPhases; phase++ {
		for _, c := range ascii {
			if _, ok := f.ensureSubpixel(c, phase); !ok {
				missing++
			}
		}
	}
	if missing != 0 {
		t.Errorf("%d of %d glyph rasterisations were dropped", missing, len(ascii)*subpixelPhases)
	}
	if len(f.spages) < 2 {
		t.Fatalf("%d glyphs at 60px over %d phases fitted %d sheet(s) — the test is not reaching the pager",
			len(ascii), subpixelPhases, len(f.spages))
	}
	// Every glyph names a sheet that exists, and sits inside it.
	for key, g := range f.sglyphs {
		if g.page < 0 || g.page >= len(f.spages) {
			t.Fatalf("glyph %d is on sheet %d of %d", key, g.page, len(f.spages))
		}
		if g.u0 < 0 || g.v0 < 0 || g.u1 > 1 || g.v1 > 1 {
			t.Fatalf("glyph %d has uv %v..%v", key, [2]float32{g.u0, g.v0}, [2]float32{g.u1, g.v1})
		}
	}
	t.Logf("%d glyphs over %d phases at 60px: %d sheets, %d rasterisations",
		len(ascii), subpixelPhases, len(f.spages), len(f.sglyphs))
}

// A space has an advance and no bitmap, and it has to be cached like
// anything else.
//
// It was not, briefly: the pager found no room for a zero-sized rect on
// any sheet, opened a new one, could not place it there either, and
// answered "no glyph" — so every space on every line did it again. A
// page of text came to five hundred megabytes of empty sheets a frame,
// and nothing in the suite noticed, because the text still looked
// right.
func TestBlankGlyphsDoNotOpenSheets(t *testing.T) {
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	r.SetSubpixelText(false)
	f, err := r.NewFont(goregular.TTF, 15)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	white := RGBA{R: 1, G: 1, B: 1, A: 1}
	r.Begin(800, 600)
	r.DrawText(f, 10, 10, "a b c d e f g h i j k l m n o p", white)
	first := len(f.pages)
	if first == 0 {
		t.Fatal("nothing was rasterised at all")
	}
	for i := 0; i < 200; i++ {
		r.Begin(800, 600)
		r.DrawText(f, 10, 10, "a b c d e f g h i j k l m n o p", white)
	}
	if len(f.pages) != first {
		t.Errorf("200 redraws of the same line went from %d sheet(s) to %d", first, len(f.pages))
	}
	// And the space is in the glyph cache, with an advance and no box.
	g, ok := f.glyphs[' ']
	if !ok {
		t.Fatal("the space was never cached")
	}
	if g.w != 0 || g.h != 0 {
		t.Errorf("the space has a %gx%g box", g.w, g.h)
	}
	if g.advance <= 0 {
		t.Errorf("the space advances %v", g.advance)
	}
}
