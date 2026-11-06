//go:build linux || windows

package shell

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// The step language is the contract between the two engines: the same file
// drives our headless run and the generated QtTest case, so a step that
// silently parses to something else would compare two different gestures.
func TestParseScript(t *testing.T) {
	steps, err := ParseScript(`
# a comment, then a blank line

click 10,20
press 1,2
drag 4,5 40,50
wheel 100,200 -120
key ctrl+shift+a
type hi
wait
`)
	if err != nil {
		t.Fatalf("parseScript: %v", err)
	}
	if len(steps) != 7 {
		t.Fatalf("parsed %d steps, want 7 (comments and blanks skipped)", len(steps))
	}
	// A click is a press AND a release, delivered through the real event
	// path: that is what sets hover and pressed state, and what a control's
	// click-on-release semantics are written against.
	clicks := 0
	box := ui.Div().Size(50, 50)
	box.OnClick(func() { clicks++ })
	u := ui.New(nil, nil, func() ui.Element { return box })
	u.Layout(200, 200)
	steps[0].run(u) // click 10,20 — inside the box
	if clicks != 1 {
		t.Fatalf("click step fired %d handlers, want 1 (press and release through the event path)", clicks)
	}
	// A press alone must NOT complete a click: a script that presses and
	// captures is looking at the pressed state, not the result.
	steps[1].run(u)
	if clicks != 1 {
		t.Fatalf("press step completed a click (%d)", clicks)
	}
	for _, bad := range []string{"click", "click 1", "click 1,2,3", "wheel 1,2", "key", "key ctrl+", "type", "jump 1,2"} {
		if _, err := ParseScript(bad); err == nil {
			t.Errorf("ParseScript(%q) accepted a bad step", bad)
		}
	}
}

// The name a step's PNG gets, so a directory of captures reads back as the
// script that produced it.
func TestStepName(t *testing.T) {
	for line, want := range map[string]string{
		"click 240,195":     "click-240-195",
		"drag 4,5 40,50":    "drag-4-5-40-50",
		"key ctrl+shift+a":  "key-ctrl-shift-a",
		"wheel 100,200 -60": "wheel-100-200-60",
		"wait":              "wait",
	} {
		if got := stepName(step{line: line}); got != want {
			t.Errorf("stepName(%q) = %q, want %q", line, got, want)
		}
	}
}
