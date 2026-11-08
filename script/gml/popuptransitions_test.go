package gml

import (
	"math"
	"testing"
	"time"

	"github.com/sushvibez/gfx/core"
)

// A popup's enter transition fades it in; its exit transition keeps it
// drawn while it fades out, then it is gone.
func TestPopupEnterExit(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    width: 400; height: 300
    Popup { id: pop; x: 50; y: 50; width: 100; height: 80
        background: Rectangle { color: "#ff00ff" }
        enter: Transition { NumberAnimation { property: "opacity"; from: 0; to: 1; duration: 200 } }
        exit: Transition { ParallelAnimation {
            NumberAnimation { property: "opacity"; from: 1; to: 0; duration: 100 }
            NumberAnimation { property: "scale"; from: 1; to: 0.8; duration: 100 } } }
        Rectangle { anchors.fill: parent; color: "#ff00ff" }
    }
}`
	h := newStates(t, src)
	drawn := func() bool { return h.rect(0xff00ff).W > 0 }
	opacity := func() float64 {
		for _, ph := range h.doc.popupPhases {
			if p := ph.vals["opacity"]; p != nil {
				return float64(p.Get())
			}
		}
		return -1
	}
	if drawn() {
		t.Fatal("the popup is drawn before it opens")
	}
	h.set("pop.open()")
	if !drawn() || opacity() > 0.05 {
		t.Errorf("just opened: drawn %v opacity %v, want drawn and faded out", drawn(), opacity())
	}
	core.TickAnimations(100 * time.Millisecond)
	h.frame()
	if o := opacity(); math.Abs(o-0.5) > 0.05 {
		t.Errorf("half way in: opacity %v, want 0.5", o)
	}
	core.TickAnimations(150 * time.Millisecond)
	h.frame()
	h.set("pop.close()")
	if !drawn() {
		t.Error("the popup vanished at once: its exit transition should keep it")
	}
	core.TickAnimations(150 * time.Millisecond)
	h.frame()
	h.frame()
	if drawn() {
		t.Errorf("the popup is still drawn after its exit transition (opacity %v)", opacity())
	}
}
