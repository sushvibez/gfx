//go:build !js

package render

import (
	"testing"

	"github.com/sushvibez/gfx/gpu"
)

// drawMSAA draws into a w×h target through a 4× multisampled pass, as a
// window and a screenshot do (path and SVG edges are anti-aliased only so).
func drawMSAA(t *testing.T, w, h int, draw func(r *Renderer2D)) []byte {
	t.Helper()
	device, queue := headlessDevice(t)
	target := device.CreateRenderTexture(w, h, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	ms := device.CreateMSAATexture(w, h, gpu.FormatRGBA8Unorm, 4)
	defer ms.Release()
	msView := ms.CreateView()
	defer msView.Release()
	r, err := New2DMSAA(device, queue, gpu.FormatRGBA8Unorm, 4)
	if err != nil {
		t.Fatalf("New2DMSAA: %v", err)
	}
	defer r.Release()
	r.Begin(w, h)
	draw(r)
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPassResolve(msView, view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, w, h)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	return pix
}

// A hinted icon's 2-unit strokes on a 24-unit grid, drawn at 14 px from a
// fractional position, come out one crisp device pixel wide: the
// horizontal stroke fills one row (nearly opaque) with almost nothing in
// the rows beside it, the vertical one a column. Unhinted, the 1.17 px
// stroke straddles two rows at partial coverage.
func TestSVGHintedStrokesCrisp(t *testing.T) {
	const W, H = 24, 24
	doc := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round"><path d="M5 12H19"/><path d="M12 5V19"/></svg>`)
	shoot := func(hinted bool) []byte {
		s, err := ParseSVG(doc)
		if err != nil {
			t.Fatal(err)
		}
		s.Hinted = hinted
		return drawMSAA(t, W, H, func(r *Renderer2D) {
			r.DrawSVGScaled(s, 3.3, 2.7, 14.0/24, 14.0/24, 0, White)
		})
	}
	// The plus's arms: the horizontal at y ≈ 2.7 + 7 = 9.7, the vertical
	// at x ≈ 3.3 + 7 = 10.3. Profiles across each, away from the middle.
	profile := func(pix []byte, horiz bool) []int {
		var p []int
		for i := 5; i < 15; i++ {
			if horiz {
				p = append(p, int(pix[(i*W+6)*4])) // column 6, rows 5..14
			} else {
				p = append(p, int(pix[(6*W+i)*4])) // row 6, columns 5..14
			}
		}
		return p
	}
	crisp := func(p []int) (peak, side int) {
		at := 0
		for i, v := range p {
			if v > p[at] {
				at = i
			}
		}
		for i, v := range p {
			if i != at {
				side = max(side, v)
			}
		}
		return p[at], side
	}
	hinted, plain := shoot(true), shoot(false)
	for _, horiz := range []bool{true, false} {
		hp, hs := crisp(profile(hinted, horiz))
		pp, ps := crisp(profile(plain, horiz))
		t.Logf("horizontal %v: hinted peak %d side %d (%v); unhinted peak %d side %d (%v)",
			horiz, hp, hs, profile(hinted, horiz), pp, ps, profile(plain, horiz))
		if hp < 230 || hs > 40 {
			t.Errorf("horizontal %v: hinted stroke peak %d, brightest neighbour %d: want one opaque pixel", horiz, hp, hs)
		}
		if ps < 60 {
			t.Errorf("horizontal %v: the unhinted stroke is already crisp (side %d): the test shows nothing", horiz, ps)
		}
	}
}
