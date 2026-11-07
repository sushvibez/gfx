//go:build !js

package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
)

// pngOf encodes a w×h image coloured by at.
func pngOf(t *testing.T, w, h int, at func(x, y int) color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, at(x, y))
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The 9-patch keeps its corners at their source size while the edges and
// middle stretch; Tile repeats the source at its own size; Pad draws it
// as it is, centred.
func TestImageNinePatchTileAndPad(t *testing.T) {
	const W, H = 260, 100
	inst, err := gpu.NewInstance(wgpuLib(t))
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Fatal(err)
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Fatal(err)
	}
	queue := device.Queue()
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := render.New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()

	red, green, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}, color.RGBA{0, 0, 255, 255}
	// 30×30: 10-pixel corners red, edges green, middle blue.
	patch := &ImageCell{Data: pngOf(t, 30, 30, func(x, y int) color.RGBA {
		ex, ey := x < 10 || x >= 20, y < 10 || y >= 20
		switch {
		case ex && ey:
			return red
		case ex || ey:
			return green
		}
		return blue
	})}
	// 10×10: left half red, right half blue.
	halves := &ImageCell{Data: pngOf(t, 10, 10, func(x, y int) color.RGBA {
		if x < 5 {
			return red
		}
		return blue
	})}
	solid := &ImageCell{Data: pngOf(t, 10, 10, func(int, int) color.RGBA { return red })}

	u := New(r, nil, func() Element {
		return Div().Row().Children(
			LazyImage(patch, 120, 90).Natural(30, 30).NinePatch(10, 10, 10, 10, render.PatchStretch, render.PatchStretch),
			LazyImage(halves, 40, 20).Natural(10, 10).Fill(FillTile),
			LazyImage(solid, 40, 40).Natural(10, 10).Fill(FillPad),
		)
	})
	r.Begin(W, H)
	u.Frame(W, H)
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatal(err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) color.RGBA {
		i := (y*W + x) * 4
		return color.RGBA{pix[i], pix[i+1], pix[i+2], pix[i+3]}
	}
	is := func(c, want color.RGBA) bool {
		d := func(a, b uint8) bool { return int(a) > int(b)+40 || int(b) > int(a)+40 }
		return !d(c.R, want.R) && !d(c.G, want.G) && !d(c.B, want.B)
	}
	black := color.RGBA{0, 0, 0, 255}
	for _, tc := range []struct {
		name string
		x, y int
		want color.RGBA
	}{
		{"9-patch top-left corner", 4, 4, red},
		{"9-patch top edge, past where a stretched corner would end", 15, 4, green},
		{"9-patch top-right corner", 115, 4, red},
		{"9-patch left edge", 4, 45, green},
		{"9-patch middle", 60, 45, blue},
		{"9-patch bottom-right corner", 115, 85, red},
		{"tile 1, left half", 122, 5, red},
		{"tile 1, right half", 127, 5, blue},
		{"tile 2, left half", 132, 5, red},
		{"tile 4, right half", 157, 5, blue},
		{"pad centre", 180, 20, red},
		{"pad outside the source", 163, 3, black},
	} {
		if c := at(tc.x, tc.y); !is(c, tc.want) {
			t.Errorf("%s (%d,%d) = %v, want %v", tc.name, tc.x, tc.y, c, tc.want)
		}
	}
}
