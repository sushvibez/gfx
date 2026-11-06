package gml

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// QML hangs a tooltip off a control's own hover state, and Qt shows it only
// once the pointer has RESTED for the control's `ToolTip.delay` — then takes
// it away again after `timeout`. Accepted and ignored, a page of tooltips
// asking for 0ms, 500ms and 1500ms showed none of them.
func TestAttachedToolTipRespectsDelayAndTimeout(t *testing.T) {
	src := `
import QtQuick
import QtQuick.Controls
Rectangle {
    width: 300; height: 220; color: "#1a1d23"
    Button {
        id: quick
        x: 20; y: 20; width: 120; height: 40
        text: "Instant"
        ToolTip.visible: hovered
        ToolTip.text: "right away"
        ToolTip.delay: 0
        ToolTip.timeout: 1000
    }
    Button {
        id: slow
        x: 20; y: 120; width: 120; height: 40
        text: "Patient"
        ToolTip.visible: hovered
        ToolTip.text: "after a while"
        ToolTip.delay: 1500
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(300, 220)
	shows := func(want string) bool {
		var found bool
		var walk func(ui.Element)
		walk = func(e ui.Element) {
			if e == nil {
				return
			}
			if s, ok := ui.TextOf(e); ok && s == want {
				found = true
			}
			for _, ch := range ui.Children(e) {
				walk(ch)
			}
		}
		walk(tree)
		return found
	}
	settle := func(d time.Duration) {
		core.TickAnimations(d)
		u.Layout(300, 220)
		u.Layout(300, 220)
	}
	// Hover the instant one: it appears.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 80, Y: 40}})
	settle(50 * time.Millisecond)
	if !shows("right away") {
		t.Error("a tooltip with no delay did not appear on hover")
	}
	// ... and goes away once its timeout has passed.
	settle(1200 * time.Millisecond)
	if shows("right away") {
		t.Error("the tooltip outstayed its timeout")
	}
	// The patient one waits for its delay.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 80, Y: 140}})
	settle(500 * time.Millisecond)
	if shows("after a while") {
		t.Error("a 1500ms tooltip appeared after 500ms")
	}
	settle(1500 * time.Millisecond)
	if !shows("after a while") {
		t.Error("the 1500ms tooltip never appeared")
	}
	// The pointer leaves: it goes, and the rest starts over.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 280, Y: 200}})
	settle(50 * time.Millisecond)
	if shows("after a while") || shows("right away") {
		t.Error("a tooltip stayed after the pointer left")
	}
}
