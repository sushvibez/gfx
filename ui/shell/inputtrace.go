package shell

import (
	"fmt"
	"os"

	"github.com/sushvibez/gfx/app"
)

// inputTrace (GFX_INPUT_TRACE=1, with the app backend's raw-message
// trace) prints each frame's pointer events and the button state the
// Input made of them, so a click that goes missing can be followed from
// the window message to what the game read.
var inputTrace = os.Getenv("GFX_INPUT_TRACE") != ""

var traceFrameN int

func (in *Input) traceFrame(events []app.MouseEvent) {
	traceFrameN++
	if len(events) == 0 && len(in.carry) == 0 && !in.clicked && !in.released {
		return
	}
	kinds := [...]string{app.MouseMove: "move", app.MouseDown: "down", app.MouseUp: "up", app.MouseScroll: "wheel"}
	s := ""
	for _, e := range events {
		k := "?"
		if int(e.Kind) < len(kinds) {
			k = kinds[e.Kind]
		}
		if e.Kind == app.MouseMove {
			continue
		}
		s += fmt.Sprintf(" %s%d@%.0f,%.0f", k, e.Button, e.X, e.Y)
	}
	fmt.Fprintf(os.Stderr, "frame %d: events[%d]%s -> clicked=%v down=%v released=%v held=%d at %.0f,%.0f\n",
		traceFrameN, len(events), s, in.clicked, in.mouseDown, in.released, len(in.carry), in.MouseX, in.MouseY)
}
