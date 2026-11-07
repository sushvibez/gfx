package shell

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

// A touchpad tap (press and release in one frame) reads as a click with
// the button down that frame, and the release the frame after — as a
// slower click does. A slower click is unchanged.
func TestTapSpansTwoFrames(t *testing.T) {
	in := NewInput()
	in.DrainEvents(nil, []app.MouseEvent{
		{Kind: app.MouseDown, Button: 1, X: 10, Y: 20},
		{Kind: app.MouseUp, Button: 1, X: 10, Y: 20},
	})
	if !in.MouseClicked() || !in.MouseDown() || in.MouseReleased() {
		t.Fatalf("tap frame 1: clicked %v down %v released %v; want true true false",
			in.MouseClicked(), in.MouseDown(), in.MouseReleased())
	}
	if !in.Pending() {
		t.Error("the held-back release is not reported as pending")
	}
	in.DrainEvents(nil, nil)
	if in.MouseClicked() || in.MouseDown() || !in.MouseReleased() {
		t.Fatalf("tap frame 2: clicked %v down %v released %v; want false false true",
			in.MouseClicked(), in.MouseDown(), in.MouseReleased())
	}
	if in.Pending() {
		t.Error("still pending after the release was delivered")
	}

	// A right-button tap too.
	in.DrainEvents(nil, []app.MouseEvent{{Kind: app.MouseDown, Button: 3}, {Kind: app.MouseUp, Button: 3}})
	if !in.RightClicked() || !in.RightDown() {
		t.Error("a right tap did not read as pressed in its first frame")
	}
	in.DrainEvents(nil, nil)
	if !in.RightReleased() {
		t.Error("a right tap's release never came")
	}

	// A click whose release comes in a later frame is untouched.
	in.DrainEvents(nil, []app.MouseEvent{{Kind: app.MouseDown, Button: 1}})
	in.DrainEvents(nil, []app.MouseEvent{{Kind: app.MouseUp, Button: 1}})
	if !in.MouseReleased() || in.Pending() {
		t.Error("an ordinary click's release was held back")
	}
}

// PointerScale moves positions into the units a screen draws in: a 200 %
// window drawing its layout at 1.6 sees a pointer at logical 210 as 262.5.
func TestPointerScale(t *testing.T) {
	in := NewInput()
	in.PointerScale = 2.0 / 1.6
	in.DrainEvents(nil, []app.MouseEvent{{Kind: app.MouseDown, Button: 1, X: 210, Y: 400}})
	if in.MouseX != 262.5 || in.MouseY != 500 {
		t.Errorf("pointer at %v,%v, want 262.5,500", in.MouseX, in.MouseY)
	}
	in.DrainEvents(nil, nil) // nothing new: the position is not scaled again
	if in.MouseX != 262.5 {
		t.Errorf("an empty frame moved the pointer to %v", in.MouseX)
	}
}
