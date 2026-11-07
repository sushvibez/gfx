package ui

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/app"
)

func TestPinch(t *testing.T) {
	type call struct {
		f     float32
		phase DragPhase
	}
	var calls []call
	scale := float32(1)
	var turned, movedX float32
	u := New(nil, nil, func() Element {
		return Div().Row().Children(
			Div().Size(200, 200).OnPinch(func(e PinchEvent) {
				calls = append(calls, call{e.Scale, e.Phase})
				scale *= e.Scale
				turned += e.Rotation
				movedX += e.DX
			}),
			Div().Size(200, 200), // no pinch region: Ctrl+wheel here is a wheel
		)
	})
	u.Layout(400, 200)
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

	// Touchpad: Ctrl+wheel, one notch away from the user = one 15 % step in.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 100, Y: 100, Ctrl: true, ScrollY: -app.WheelNotchPx}})
	if len(calls) != 1 || calls[0].phase != DragMove || !near(scale, PinchStepPerNotch) {
		t.Fatalf("ctrl+wheel: calls %v scale %v", calls, scale)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 300, Y: 100, Ctrl: true, ScrollY: -app.WheelNotchPx}})
	if len(calls) != 1 {
		t.Errorf("Ctrl+wheel outside the region pinched it")
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 100, Y: 100, ScrollY: -app.WheelNotchPx}})
	if len(calls) != 1 {
		t.Errorf("a plain wheel pinched")
	}

	// Touchscreen: two fingers 40 apart spreading to 80 doubles the scale.
	calls, scale = nil, 1
	u.Touch([]app.TouchEvent{{ID: 1, Phase: app.TouchBegin, X: 80, Y: 100}, {ID: 2, Phase: app.TouchBegin, X: 120, Y: 100}})
	u.Touch([]app.TouchEvent{{ID: 1, Phase: app.TouchMove, X: 60, Y: 100}, {ID: 2, Phase: app.TouchMove, X: 140, Y: 100}})
	u.Touch([]app.TouchEvent{{ID: 1, Phase: app.TouchEnd, X: 60, Y: 100}, {ID: 2, Phase: app.TouchEnd, X: 140, Y: 100}})
	if len(calls) < 3 || calls[0].phase != DragStart || calls[len(calls)-1].phase != DragEnd {
		t.Fatalf("touch pinch calls = %v, want start … end", calls)
	}
	if !near(scale, 2) {
		t.Errorf("touch pinch scale = %v, want 2", scale)
	}

	// Turning a quarter clockwise while the pair moves 30 right.
	turned, movedX = 0, 0
	u.Touch([]app.TouchEvent{{ID: 1, Phase: app.TouchBegin, X: 80, Y: 100}, {ID: 2, Phase: app.TouchBegin, X: 120, Y: 100}})
	u.Touch([]app.TouchEvent{{ID: 1, Phase: app.TouchMove, X: 130, Y: 80}, {ID: 2, Phase: app.TouchMove, X: 130, Y: 120}})
	u.Touch([]app.TouchEvent{{ID: 1, Phase: app.TouchEnd, X: 130, Y: 80}, {ID: 2, Phase: app.TouchEnd, X: 130, Y: 120}})
	if !near(turned, 90) || !near(movedX, 30) {
		t.Errorf("turned %v moved %v, want 90, 30", turned, movedX)
	}
}
