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

// An upright small icon is drawn as a browser draws it, Hinted or not:
// the coverage raster takes the exact geometry, so a 2-unit stroke at
// 14 px is 1.17 px of coverage across the rows it straddles. (Hinted once
// rounded it to one opaque pixel here; side by side with Chrome that made
// every glyph thinner at 14–18 px and heavier from 20, and squared small
// curves — see TestSVGRasterExactWidth.)
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
	// The plus's arms: profiles across each, away from the middle.
	profile := func(pix []byte, horiz bool) []int {
		var p []int
		for i := 2; i < 18; i++ {
			if horiz {
				p = append(p, int(pix[(i*W+6)*4])) // column 6
			} else {
				p = append(p, int(pix[(6*W+i)*4])) // row 6
			}
		}
		return p
	}
	hinted, plain := shoot(true), shoot(false)
	for _, horiz := range []bool{true, false} {
		hp, pp := profile(hinted, horiz), profile(plain, horiz)
		sum := 0
		for i := range hp {
			if hp[i] != pp[i] {
				t.Errorf("horizontal %v: hinted %v, unhinted %v: want the same", horiz, hp, pp)
				break
			}
			sum += hp[i]
		}
		if want := 14.0 / 12 * 255; float64(sum) < want-8 || float64(sum) > want+8 {
			t.Errorf("horizontal %v: profile %v sums to %d, want %.0f (a 1.17 px stroke)", horiz, hp, sum, want)
		}
	}
}
