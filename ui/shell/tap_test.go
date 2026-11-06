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
