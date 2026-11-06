package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// A snapping slider only ever holds a step boundary: QML's
// `stepSize: 10` with `snapMode: Slider.SnapAlways` reports 20, never 19,
// however carefully the handle is dragged.
func TestSliderSnapsToStep(t *testing.T) {
	var v core.Property[float32]
	v.Set(50)
	sl := Slider(&v, 200).Range(0, 100).Step(10, true)
	u := New(nil, nil, func() Element { return sl })
	u.Layout(200, 40)
	// Press two thirds along the track, then drag a little further.
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 130, Y: 20, Button: 1},
		{Kind: app.MouseMove, X: 137, Y: 20},
		{Kind: app.MouseUp, X: 137, Y: 20, Button: 1},
	})
	got := v.Get()
	if got != snapTo(got, 0, 10) {
		t.Fatalf("snapping slider reported %v, which is not a multiple of 10", got)
	}
	// The same drag on a free slider lands between steps.
	v.Set(50)
	free := Slider(&v, 200).Range(0, 100).Step(10, false)
	u = New(nil, nil, func() Element { return free })
	u.Layout(200, 40)
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 137, Y: 20, Button: 1},
		{Kind: app.MouseUp, X: 137, Y: 20, Button: 1},
	})
	if v.Get() == snapTo(v.Get(), 0, 10) && v.Get() != 70 {
		t.Errorf("NoSnap slider snapped anyway (%v)", v.Get())
	}
}

// A vertical slider's zero is at the BOTTOM (QML orientation: Qt.Vertical),
// and it reads the pointer's Y. Read across X instead, dragging it up did
// nothing and a press anywhere jumped the value to the middle.
func TestVerticalSliderReadsY(t *testing.T) {
	var v core.Property[float32]
	sl := Slider(&v, 30).Range(0, 100).Vertical()
	u := New(nil, nil, func() Element { return sl })
	u.Layout(30, 200)
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 15, Y: 180, Button: 1}})
	low := v.Get()
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseMove, X: 15, Y: 20},
		{Kind: app.MouseUp, X: 15, Y: 20, Button: 1},
	})
	high := v.Get()
	if !(low < 25) {
		t.Errorf("a press near the BOTTOM gave %v, want a low value", low)
	}
	if !(high > 75) {
		t.Errorf("dragging to the top gave %v, want a high value", high)
	}
}

// A control takes the press for itself: in QML a Slider accepts it, so the
// Flickable it sits in never sees it. Without that, dragging a slider on a
// scrollable page moved the slider AND panned the page.
func TestSliderPressDoesNotStartTheEnclosingDrag(t *testing.T) {
	var v core.Property[float32]
	v.Set(50)
	sl := Slider(&v, 200).Range(0, 100)
	var phases []DragPhase
	page := Div().Size(200, 200).
		OnDrag(func(dx, dy float32, p DragPhase) { phases = append(phases, p) }).
		Children(sl)
	u := New(nil, nil, func() Element { return page })
	u.Layout(200, 200)
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 100, Y: 10, Button: 1},
		{Kind: app.MouseMove, X: 140, Y: 10},
		{Kind: app.MouseUp, X: 140, Y: 10, Button: 1},
	})
	if len(phases) > 0 {
		t.Errorf("the page's drag region took a slider press: %v", phases)
	}
	if v.Get() <= 50 {
		t.Errorf("the slider did not move (%v)", v.Get())
	}
	// A press on plain content DOES reach the drag region: a list of
	// buttons still flicks.
	phases = nil
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 100, Y: 150, Button: 1},
		{Kind: app.MouseMove, X: 100, Y: 120},
		{Kind: app.MouseUp, X: 100, Y: 120, Button: 1},
	})
	if len(phases) == 0 {
		t.Error("a press on plain content never reached the drag region")
	}
}

// A dial turns to the pointer's ANGLE about its centre, sweeping Qt's
// -140°..+140° through 12 o'clock — and it turns on the press, not only on
// a drag, which is what Qt's Dial does. Ours took no input at all: clicking
// a dial in the corpus moved Qt's and left ours where it was.
func TestDialTurnsToThePointerAngle(t *testing.T) {
	var v core.Property[float32]
	dial := Dial(&v, 0, 100).Size(100)
	u := New(nil, nil, func() Element { return dial })
	u.Layout(100, 100)
	press := func(x, y float32) float32 {
		u.Mouse([]app.MouseEvent{
			{Kind: app.MouseDown, X: x, Y: y, Button: 1},
			{Kind: app.MouseUp, X: x, Y: y, Button: 1},
		})
		return v.Get()
	}
	// Straight up from the centre is the middle of the sweep.
	if got := press(50, 5); got < 45 || got > 55 {
		t.Errorf("12 o'clock gave %v, want ~50", got)
	}
	// Hard left and hard right are the ends.
	if got := press(5, 60); got > 15 {
		t.Errorf("the left end gave %v, want ~0", got)
	}
	if got := press(95, 60); got < 85 {
		t.Errorf("the right end gave %v, want ~100", got)
	}
	// It keeps following a held press.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseDown, X: 50, Y: 5, Button: 1}})
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 95, Y: 60}})
	if got := v.Get(); got < 85 {
		t.Errorf("dragging round to the right gave %v, want ~100", got)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: 95, Y: 60, Button: 1}})
	// A snapping dial only holds step boundaries.
	stepped := Dial(&v, 0, 100).Size(100).Step(10, true)
	u = New(nil, nil, func() Element { return stepped })
	u.Layout(100, 100)
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseDown, X: 72, Y: 12, Button: 1},
		{Kind: app.MouseUp, X: 72, Y: 12, Button: 1},
	})
	if got := v.Get(); got != snapTo(got, 0, 10) {
		t.Errorf("a snapping dial reported %v, which is not a multiple of 10", got)
	}
}
