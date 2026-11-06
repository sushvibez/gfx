//go:build !js

package ui

import (
	"testing"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/render"
	"golang.org/x/image/font/gofont/goregular"
)

// A row inside a scrolling column: a long truncating label that grows,
// followed by a sibling. The sibling must stay inside the view — FFDC's
// glTF inspector lost the tag after every long node name, because the
// label's full width made the column's content wider than the view and
// the sibling was laid out past its right edge.
func TestGrowingTextInScrollColumnLeavesRoomForSibling(t *testing.T) {
	inst, err := gpu.NewInstance(wgpuLib(t))
	if err != nil {
		t.Skip(err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Skip(err)
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Skip(err)
	}
	r, err := render.New2D(device, device.Queue(), gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	font, err := r.NewFont(goregular.TTF, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer font.Release()

	sib := Div().Size(30, 20)
	label := Text("a very long node name that does not fit in two hundred pixels", RGBA{R: 1, G: 1, B: 1, A: 1}).Truncate().Grow()
	row := Div().Row().Gap(8).Children(label, sib)
	page := Scroll(&ScrollState{}).Children(Div().Col().Children(row, Div().Size(100, 20)))
	u := New(r, font, func() Element { return page })
	u.Layout(200, 300)
	rb, lb, sb := Bounds(row), Bounds(label), Bounds(sib)
	t.Logf("row %+v label %+v sibling %+v", rb, lb, sb)
	if rb.W > 200.5 {
		t.Errorf("the row is wider than the 200 px view: %+v", rb)
	}
	if sb.X+sb.W > 200.5 || sb.W < 30 {
		t.Errorf("the sibling is not inside the view: %+v", sb)
	}
}
