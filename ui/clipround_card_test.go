package ui

import (
	"testing"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
)

// A painted object's rounded clip holds inside a card that has a shadow and
// a hover colour, in a scroll view, round a cropped image (cqb's menu: a
// scenario's photo in a rounded card showed square corners over the card's
// border — DrawImageCropped's own square clip of the same box replaced the
// rounded one).
func TestPushClipRoundInsideCard(t *testing.T) {
	const W, H = 200, 160
	inst, err := gpu.NewInstance(wgpuLib(t))
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	adapter, _ := inst.RequestAdapter(nil)
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Fatalf("RequestDevice: %v", err)
	}
	queue := device.Queue()
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := render.New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	img := r.NewImage(1, 1, []byte{0, 255, 0, 255})
	var st ScrollState
	for _, c := range []struct {
		name string
		card func(*DivEl) *DivEl
	}{
		{"plain", func(d *DivEl) *DivEl { return d }},
		{"shadow", func(d *DivEl) *DivEl { return d.Shadow(12, 0, 4, RGBA{A: 0.5}) }},
		{"hover", func(d *DivEl) *DivEl { return d.HoverBg(RGBA{R: 0.3, G: 0.3, B: 0.3, A: 1}) }},
		{"both", func(d *DivEl) *DivEl {
			return d.Shadow(12, 0, 4, RGBA{A: 0.5}).HoverBg(RGBA{R: 0.3, G: 0.3, B: 0.3, A: 1})
		}},
	} {
		u := New(r, nil, func() Element {
			thumb := NewCanvas(120, 80, func(r *render.Renderer2D, rc Rect) {
				r.PushClipRound(rc.X, rc.Y, rc.W, rc.H, 11)
				// DrawImageCropped pushes its own square clip of the same box.
				r.DrawImageCropped(img, rc.X, rc.Y, rc.W, rc.H, render.White)
				r.PopClip()
			})
			card := c.card(Div().Rounded(12).Bg(RGBA{R: 0.2, G: 0.2, B: 0.2, A: 1}).Children(thumb))
			return Scroll(&st).Children(Div().P(20).Children(card))
		})
		pix := frame(t, device, queue, target, view, r, u, W, H)
		green := func(x, y int) int { return int(pix[(y*W+x)*4+1]) }
		// The thumb is at (20, 20) 120×80: its corner pixel is outside
		// the 11 px rounding, its middle inside.
		if green(80, 60) < 200 {
			t.Errorf("%s: the thumb's middle is not painted (g %d)", c.name, green(80, 60))
		}
		if g := green(20, 20); g > 120 {
			t.Errorf("%s: the thumb's corner is painted (g %d): the rounded clip was lost", c.name, g)
		}
	}
}
