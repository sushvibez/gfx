package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
)

// TestViewport3DPhysicalPixels: on a scaled display the offscreen 3D target
// is sized in physical pixels (box × DPI scale) so it is not rendered small
// and stretched, while View3D.Size and the element's rect stay logical —
// the units of mouse coordinates, which picking (render.ScreenRay) and
// overlays mix them with.
func TestViewport3DPhysicalPixels(t *testing.T) {
	for _, tc := range []struct {
		scale  float32
		pw, ph int
	}{{1, 300, 200}, {1.5, 450, 300}, {2, 600, 400}, {1.25, 375, 250}} {
		r := &render.Renderer2D{} // no GPU: layout only reads the scale
		r.SetScale(tc.scale)
		v := render.NewView3D(nil, nil, gpu.TextureFormat(gpu.FormatRGBA8Unorm))
		var vp *Viewport3DEl
		u := New(r, nil, func() Element {
			vp = Viewport3D(v).Size(300, 200)
			return Div().Children(vp)
		})
		u.Layout(800, 600)
		if w, h := v.Size(); w != 300 || h != 200 {
			t.Errorf("scale %v: Size() = %dx%d, want logical 300x200", tc.scale, w, h)
		}
		if w, h := v.PixelSize(); w != tc.pw || h != tc.ph {
			t.Errorf("scale %v: PixelSize() = %dx%d, want %dx%d", tc.scale, w, h, tc.pw, tc.ph)
		}
		if rc := vp.Rect(); rc.W != 300 || rc.H != 200 {
			t.Errorf("scale %v: Rect() = %v, want a logical 300x200 box", tc.scale, rc)
		}
	}
}

func TestScaledPixels(t *testing.T) {
	for _, tc := range []struct {
		w, h   int
		s      float32
		pw, ph int
	}{{101, 51, 1.5, 152, 77}, {1, 1, 0, 1, 1}, {0, 0, 2, 1, 1}, {640, 480, -1, 640, 480}} {
		if pw, ph := render.ScaledPixels(tc.w, tc.h, tc.s); pw != tc.pw || ph != tc.ph {
			t.Errorf("ScaledPixels(%d,%d,%v) = %d,%d want %d,%d", tc.w, tc.h, tc.s, pw, ph, tc.pw, tc.ph)
		}
	}
}

// TestViewport3DDragIsNotTap: a release within TapSlop of the press taps
// the view; one further off ended a drag (an orbit) and does not.
func TestViewport3DDragIsNotTap(t *testing.T) {
	v := render.NewView3D(nil, nil, gpu.TextureFormat(gpu.FormatRGBA8Unorm))
	taps := 0
	u := New(&render.Renderer2D{}, nil, func() Element {
		return Div().Children(Viewport3D(v).Size(300, 200).OnTap(func(x, y float32) { taps++ }))
	})
	press := func(x0, y0, x1, y1 float32) {
		u.Layout(800, 600)
		u.Mouse([]app.MouseEvent{
			{Kind: app.MouseMove, X: x0, Y: y0},
			{Kind: app.MouseDown, X: x0, Y: y0, Button: 1},
			{Kind: app.MouseMove, X: x1, Y: y1},
			{Kind: app.MouseUp, X: x1, Y: y1, Button: 1},
		})
	}
	press(50, 50, 52, 51)
	if taps != 1 {
		t.Fatalf("a still click gave %d taps, want 1", taps)
	}
	press(50, 50, 120, 90)
	if taps != 1 {
		t.Fatalf("a drag tapped (%d taps)", taps)
	}
}
