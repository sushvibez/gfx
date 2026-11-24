//go:build !js

package shell

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestContentMin: the window's floor is the app's MinSize when it gives
// one, else the laid-out tree's min-content, never under the small fixed
// floor, in physical px at the window's scale.
func TestContentMin(t *testing.T) {
	u := ui.New(nil, nil, func() ui.Element {
		return ui.Div().Row().Gap(10).Children(ui.Div().W(500).H(40), ui.Div().Grow(), ui.Div().W(300).H(200))
	})
	u.Layout(1200, 800)
	if w, h := contentMin(Hooks{}, nil, u, 1); w != 820 || h != 200 {
		t.Errorf("from the tree: %d×%d, want 820×200", w, h)
	}
	if w, h := contentMin(Hooks{}, nil, u, 1.5); w != 1230 || h != 300 {
		t.Errorf("from the tree at 1.5: %d×%d, want 1230×300", w, h)
	}
	app := Hooks{MinSize: func(*App) (int, int) { return 900, 640 }}
	if w, h := contentMin(app, nil, u, 1); w != 900 || h != 640 {
		t.Errorf("from Hooks.MinSize: %d×%d, want 900×640", w, h)
	}
	tiny := ui.New(nil, nil, func() ui.Element { return ui.Div().W(20).H(10) })
	tiny.Layout(400, 300)
	if w, h := contentMin(Hooks{}, nil, tiny, 1); w != minFloorW || h != minFloorH {
		t.Errorf("a tiny tree: %d×%d, want the %d×%d floor", w, h, minFloorW, minFloorH)
	}
	if w, h := contentMin(Hooks{}, nil, nil, 1); w != minFloorW || h != minFloorH {
		t.Errorf("no tree yet: %d×%d, want the floor", w, h)
	}
}
