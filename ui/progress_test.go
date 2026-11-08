package ui

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

func TestProgressBarAndBusy(t *testing.T) {
	var act, spin Activity
	var bar, ind, busy *DivEl
	u := New(nil, nil, func() Element {
		bar = ProgressBar(30, ProgressOpts{From: 0, To: 120, Width: 200})
		ind = ProgressBar(0, ProgressOpts{Width: 300, Indeterminate: &act})
		busy = BusyIndicator(&spin, 40)
		return Div().Col().Children(bar, ind, busy)
	})
	u.Layout(400, 300)
	if r := Bounds(Children(bar)[0]); r.W != 50 || r.H != 6 {
		t.Errorf("30 of 0..120 fills %vx%v, want 50x6", r.W, r.H)
	}
	if !act.Running() {
		t.Fatal("an indeterminate bar did not start its loop")
	}
	core.TickAnimations(600 * time.Millisecond) // half a round
	u.Layout(400, 300)
	seg := Bounds(Children(ind)[1])
	if seg.W != 100 || seg.X-Bounds(ind).X != 100 {
		t.Errorf("half way round the segment is at %v, %v wide; want 100, 100", seg.X-Bounds(ind).X, seg.W)
	}
	if spin.Running() {
		t.Error("a spinner started without Run")
	}
	spin.Run(true)
	core.TickAnimations(300 * time.Millisecond)
	if p := spin.Phase(); p < 0.24 || p > 0.26 {
		t.Errorf("a quarter round in, phase %v", p)
	}
	act.Run(false)
	spin.Run(false)
}

func TestToolTipDelay(t *testing.T) {
	var st ToolTipState
	var btn *DivEl
	u := New(nil, nil, func() Element {
		btn = st.Attach(Div().Size(80, 30).OnClick(func() {}), "Save the file", ToolTipOpts{Delay: 500 * time.Millisecond})
		return Div().P(50).Children(btn)
	})
	frame := func() { u.Layout(400, 300) }
	frame()
	tipShown := func() bool { return len(Children(btn)) > 0 }
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 60, Y: 60}})
	frame()
	if !st.hover.Get() || tipShown() {
		t.Fatalf("just hovered: hover %v, tip shown %v; want hovered, no tip yet", st.hover.Get(), tipShown())
	}
	core.TickAnimations(600 * time.Millisecond)
	frame()
	if !tipShown() {
		t.Fatal("no tip after the delay")
	}
	tip := Bounds(Children(Children(btn)[0])[0])
	if tip.Y+tip.H > Bounds(btn).Y {
		t.Errorf("tip at %+v is not above the button %+v", tip, Bounds(btn))
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 300, Y: 250}})
	frame()
	frame() // hover is reported by layout, so the next build sees it
	if tipShown() {
		t.Error("the tip stayed after the pointer left")
	}
}
