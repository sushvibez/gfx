package ui

import (
	"math"
	"testing"
	"time"

	"github.com/sushvibez/gfx/core"
)

func TestPopupState(t *testing.T) {
	st := &PopupState{Enter: FadeIn(200 * time.Millisecond),
		Exit: append(FadeOut(100*time.Millisecond), PopupAnim{Prop: PopupScale, To: 0.8, Dur: 100 * time.Millisecond})}
	if st.Visible() {
		t.Fatal("visible before opening")
	}
	st.Open()
	if !st.Visible() || st.opacity.Get() != 0 {
		t.Errorf("just opened: visible %v opacity %v", st.Visible(), st.opacity.Get())
	}
	core.TickAnimations(100 * time.Millisecond)
	if o := st.opacity.Get(); math.Abs(float64(o)-0.5) > 0.01 {
		t.Errorf("half way in: opacity %v", o)
	}
	core.TickAnimations(150 * time.Millisecond)
	st.Close()
	if !st.Visible() || st.IsOpen() {
		t.Errorf("closing: visible %v open %v, want drawn but not open", st.Visible(), st.IsOpen())
	}
	core.TickAnimations(150 * time.Millisecond)
	if st.Visible() {
		t.Errorf("still visible after the exit (opacity %v scale %v)", st.opacity.Get(), st.scale.Get())
	}
	st.Open() // opens from full scale, wherever the exit left it
	if st.scale.Get() != 1 {
		t.Errorf("reopened at scale %v, want 1", st.scale.Get())
	}
	d := st.Apply(Div())
	if !d.hasOpacity {
		t.Error("Apply did not set the faded-out opacity")
	}
	core.TickAnimations(time.Second)
}
