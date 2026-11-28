//go:build !js

package render

import (
	"math"
	"testing"
)

// A small icon's curve is anti-aliased by true area coverage: a 2-unit
// circle stroke drawn at 14 px through the 4× MSAA pass, which on its own
// gives five levels of coverage, shows many partial levels along its edge,
// and a second draw of the same size reuses the cached raster. (The
// circle sits off the grid's symmetry, which on its own would repeat the
// same few coverage values all the way round.)
func TestSVGRasterSmoothCurves(t *testing.T) {
	const W, H = 20, 20
	s, err := ParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="2"><circle cx="11.7" cy="12.2" r="9"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	var cached int
	pix := drawMSAA(t, W, H, func(r *Renderer2D) {
		r.DrawSVG(s, 2.4, 2.6, 14.0/24, White)
		r.DrawSVG(s, 2.4, 2.6, 14.0/24, White)
		cached = len(r.svgRasters)
	})
	if cached != 1 {
		t.Errorf("%d rasters cached for one size, want 1", cached)
	}
	levels := map[byte]bool{}
	for i := 0; i < len(pix); i += 4 {
		if v := pix[i]; v > 8 && v < 247 {
			levels[v/8] = true
		}
	}
	if len(levels) < 8 {
		t.Errorf("%d distinct partial coverage bands on a 14 px circle, want at least 8 (smooth, not stepped)", len(levels))
	}
}

// A small icon's stroke is its exact width, as a browser draws it: a
// 2-unit stroke at the editor rail's 18 px is 1.5 px of coverage (here
// three quarters of each of the two rows it straddles), hinted or not —
// not rounded to one solid pixel, which drew 14–18 px glyphs visibly
// thinner than Chrome and 20 px ones (rounded to two) heavier.
func TestSVGRasterExactWidth(t *testing.T) {
	const W, H = 24, 24
	s, err := ParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="2"><path d="M4 12H20"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	s.Hinted = true
	pix := drawMSAA(t, W, H, func(r *Renderer2D) { r.DrawSVG(s, 3, 3, 18.0/24, White) })
	var col []int
	sum := 0
	for y := 0; y < H; y++ {
		col = append(col, int(pix[(y*W+12)*4]))
		sum += col[y]
	}
	if col[11] < 185 || col[11] > 197 || col[12] < 185 || col[12] > 197 || sum > 2*197 {
		t.Errorf("column through the line %v: want two rows at 3/4 coverage (~191)", col)
	}
}

// A stroke's coverage is the union of its pieces: where a round joint's
// disc overlaps its segments, or two segments overlap, an edge pixel is
// not counted twice. The total coverage of an L with round joins and caps
// is its exact area — two 12×2 bodies, a whole disc for the two caps, a
// quarter disc outside the corner, less the 2×2 square both bodies cover.
func TestSVGRasterStrokeUnion(t *testing.T) {
	s, err := ParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6.3 6.3V18.3H18.3"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	img := svgRasterize(s, 1, 1)
	var sum float64
	for i := 3; i < len(img.Pix); i += 4 {
		sum += float64(img.Pix[i]) / 255
	}
	want := 2*12*2 + math.Pi + math.Pi/4 - 1
	if math.Abs(sum-want) > 0.3 {
		t.Errorf("L stroke covers %.2f px², want %.2f (its union, each overlap counted once)", sum, want)
	}
	// A rounded rect's corners are rounded: rx="2" at 24 px leaves the
	// corner pixel outside the stroke mostly empty.
	s, err = ParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="2"><rect width="18" height="18" x="3" y="3" rx="2"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	img = svgRasterize(s, 1, 1)
	if a := img.Pix[(2+svgRasterPad)*img.Stride+(2+svgRasterPad)*4+3]; a > 64 {
		t.Errorf("corner pixel of a rx=2 rect has coverage %d, want a rounded corner (< 64)", a)
	}
}
