package msdf

import (
	"image"
	"image/color"
	"math"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

const loadPPEM = fixed.Int26_6(256 << 6)

func shapeFor(t *testing.T, r rune) (Shape, *sfnt.Font) {
	t.Helper()
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	var b sfnt.Buffer
	s, ok := Load(f, &b, r, loadPPEM, DefaultCornerCos)
	if !ok {
		t.Fatalf("no outline for %q", r)
	}
	return s, f
}

// The field has to reconstruct the glyph. Thresholding the median at
// zero must agree with a high-resolution rasterisation of the same
// outline nearly everywhere — the disagreements being texels the edge
// passes through, where "inside" is a matter of a fraction of a texel.
//
// This is the test that says the whole thing works: a field that is
// subtly wrong still looks like a letter.
func TestFieldReconstructsTheGlyph(t *testing.T) {
	for _, r := range []rune{'A', 'o', 'M', 'e', 'g', '8', 'W'} {
		s, _ := shapeFor(t, r)
		const N, pad = 48, 6
		f := Generate(s, N, N, pad)

		// The truth: the same outline rasterised at the same grid.
		truth := rasterise(s, f, N)

		wrong, edgeish := 0, 0
		for y := 0; y < N; y++ {
			for x := 0; x < N; x++ {
				got := Median(f.At(x, y)) >= 128
				want := truth.GrayAt(x, y).Y >= 128
				if got == want {
					continue
				}
				// Only count it if the texel is not on the edge: the
				// coverage there is genuinely between the two answers.
				cov := truth.GrayAt(x, y).Y
				if cov > 40 && cov < 215 {
					edgeish++
					continue
				}
				wrong++
			}
		}
		if frac := float64(wrong) / float64(N*N); frac > 0.01 {
			t.Errorf("%q: %d of %d texels (%.1f%%) disagree with the rasteriser, %d on the edge",
				r, wrong, N*N, frac*100, edgeish)
		}
	}
}

// rasterise fills the shape over the field's own grid, so the two can be
// compared texel for texel.
func rasterise(s Shape, f *Field, n int) *image.Gray {
	ras := vector.NewRasterizer(n, n)
	sx := float32(n) / float32(f.MaxX-f.MinX)
	sy := float32(n) / float32(f.MaxY-f.MinY)
	to := func(p point) (float32, float32) {
		return (float32(p.X) - float32(f.MinX)) * sx, (float32(p.Y) - float32(f.MinY)) * sy
	}
	// The edges come back in contour order, so a new contour starts
	// wherever the previous edge's end is not this one's start.
	var last point
	started := false
	for i := range s.Edges {
		e := s.Edges[i]
		if !started || e.A.sub(last).len() > 1e-9 {
			if started {
				ras.ClosePath()
			}
			x, y := to(e.A)
			ras.MoveTo(x, y)
			started = true
		}
		x, y := to(e.B)
		ras.LineTo(x, y)
		last = e.B
	}
	if started {
		ras.ClosePath()
	}
	dst := image.NewGray(image.Rect(0, 0, n, n))
	ras.Draw(dst, dst.Bounds(), image.NewUniform(color.Gray{Y: 255}), image.Point{})
	return dst
}

func gray(v uint8) *image.Gray {
	return &image.Gray{Pix: []uint8{v}, Stride: 1, Rect: image.Rect(0, 0, 1, 1)}
}

// The whole reason for three channels: a corner has to stay a corner.
//
// A single-channel field cannot represent one. The distance to a point
// is a cone, a bilinear filter turns a cone into a dome, and the corner
// comes back rounded however much resolution you give it. Three channels
// carry the two sides of the corner separately and the median picks
// between them, which reconstructs the point exactly.
//
// Measured on a triangle rather than a letter, because a letter's corner
// is whatever the type designer drew — Go Regular's 'A', for one, has a
// flat apex and no corner to test.
func TestCornersStaySharp(t *testing.T) {
	// A tall thin triangle: apex at the top middle, base at the bottom.
	// In image convention, so Y runs down.
	apex := point{50, 5}
	tri := [][]point{{apex, {95, 95}, {5, 95}}}
	sharp := colourise(tri, DefaultCornerCos)
	flat := colourise(tri, -1) // one colour: an ordinary SDF

	const N, pad = 32, 4
	fs, ff := Generate(sharp, N, N, pad), Generate(flat, N, N, pad)

	// How far up the tip survives: the highest row at which anything is
	// still inside the reconstructed shape. A rounded apex loses its
	// point, so it stops lower down.
	tip := func(f *Field) float64 {
		for row := float64(pad); row < float64(N); row += 0.02 {
			for i := 0; i <= 200; i++ {
				x := float64(N)*0.35 + float64(i)/200*float64(N)*0.3
				if sampleMedian(f, x, row) >= 0.5 {
					return row
				}
			}
		}
		return float64(N)
	}
	sharpTip, flatTip := tip(fs), tip(ff)
	t.Logf("the tip reaches row %.2f with three channels, %.2f with one (the apex is at %d)",
		sharpTip, flatTip, pad)
	if sharpTip >= flatTip {
		t.Errorf("three channels reached row %.2f and one reached %.2f — no sharper",
			sharpTip, flatTip)
	}
	// And it is still a triangle, widening downwards.
	width := func(f *Field, row float64) float64 {
		const samples = 800
		in := 0
		for i := 0; i < samples; i++ {
			u := float64(i) / float64(samples-1)
			if sampleMedian(f, u*float64(N), row) >= 0.5 {
				in++
			}
		}
		return float64(in) / float64(samples)
	}
	if a, b := width(fs, 8), width(fs, 16); a >= b {
		t.Errorf("not a triangle: %.4f wide at row 8, %.4f at row 16", a, b)
	}
}

// sampleMedian reads the field with bilinear filtering, the way a GPU
// sampler does, and returns the median in [0,1] with 0.5 the edge.
func sampleMedian(f *Field, x, y float64) float64 {
	x, y = x-0.5, y-0.5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	var ch [3]float64
	for c := 0; c < 3; c++ {
		var v float64
		for _, o := range [4][3]int{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}} {
			px, py := clampI(x0+o[0], f.W-1), clampI(y0+o[1], f.H-1)
			w := (1 - fx) * (1 - fy)
			if o[0] == 1 {
				w = fx * (1 - fy)
			}
			if o[1] == 1 {
				w = (1 - fx) * fy
			}
			if o[0] == 1 && o[1] == 1 {
				w = fx * fy
			}
			v += w * float64(f.Pix[(py*f.W+px)*3+c])
		}
		ch[c] = v
	}
	m := math.Max(math.Min(ch[0], ch[1]), math.Min(math.Max(ch[0], ch[1]), ch[2]))
	return m / 255
}

func clampI(v, hi int) int {
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}

// One field, every size. That is the point of the exercise: the atlas
// this replaces keeps a separate rasterised sheet per pixel size, which
// is why a font at eleven, thirteen and thirty-four costs three atlases
// and why text cannot be scaled or rotated smoothly between them.
//
// Reconstructed at four sizes from the same 32-texel field, the glyph's
// coverage must stay within a few per cent — not identical, since the
// sampling grid changes, but not drifting either.
func TestOneFieldDrawsEverySize(t *testing.T) {
	s, _ := shapeFor(t, 'e')
	f := Generate(s, 32, 32, 4)

	cover := func(n int) float64 {
		in := 0
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				// Sample the field at this output resolution.
				u := (float64(x) + 0.5) / float64(n) * float64(f.W)
				v := (float64(y) + 0.5) / float64(n) * float64(f.H)
				if sampleMedian(f, u, v) >= 0.5 {
					in++
				}
			}
		}
		return float64(in) / float64(n*n)
	}
	base := cover(256)
	for _, n := range []int{16, 32, 64, 128, 512} {
		got := cover(n)
		if math.Abs(got-base) > 0.03 {
			t.Errorf("at %d px the glyph covers %.3f of the tile, at 256 px %.3f", n, got, base)
		}
	}
	t.Logf("coverage from one 32-texel field: 16px %.3f  64px %.3f  512px %.3f  (256px %.3f)",
		cover(16), cover(64), cover(512), base)
}
