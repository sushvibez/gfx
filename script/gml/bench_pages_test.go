package gml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// How much of each corpus page does a steady frame rebuild?
//
// The per-page number is what says whether the subtree cache is working
// on real markup, and it is the tool that found the delegate-row work:
// every view used to switch the cache off for its whole interior.
//
//	GFX_PAGE_STATS=1 go test ./gml -run PageRebuild -v
//
// PAGE=indicators narrows it to one page, and adding GFX_BUILD_STATS=1
// prints the per-frame miss breakdown (absent / env / untracked /
// stamped) that the totals here do not carry — those counters are only
// kept when that knob is on, and View() clears them as it prints.
//
// Measured 2026-09-25, steady frame at 900x700, before -> after the row
// token (rowtoken.go):
//
//	navdisplay    91 ->   0 fresh
//	chatui       184 ->  38
//	indicators   228 ->  34
//	shapes        19 ->  19
//	sliders       12 ->  12
//	buttons        1 ->   1
//
// animations, toasts, hud, pfd, states, transforms, cards2d5, lenstabs,
// multiplane, switches and layouts rebuild NOTHING in a steady frame.
//
// What is left on the four that still rebuild is ordinary untracked
// reads — an `Animation` or `Easing` namespace map, a plain `property
// real` an ancestor bound by value — not the row scope.
func TestPageRebuildCounts(t *testing.T) {
	if os.Getenv("GFX_PAGE_STATS") == "" {
		t.Skip("set GFX_PAGE_STATS=1: reports per-page rebuild counts")
	}
	only := os.Getenv("PAGE")
	home, _ := os.UserHomeDir()
	for _, name := range []string{"buttons", "animations", "indicators", "toasts",
		"hud", "pfd", "navdisplay", "states", "transforms", "cards2d5", "chatui",
		"lenstabs", "multiplane", "shapes", "sliders", "switches", "layouts"} {
		if only != "" && name != only {
			continue
		}
		path := filepath.Join(home, "workspace", "qml-examples", "examples", name, "Main.qml")
		if _, err := os.Stat(path); err != nil {
			t.Skip("corpus not found")
		}
		d, err := Load(path, NewEnv())
		if err != nil {
			t.Errorf("%-14s load failed: %v", name, err)
			continue
		}
		view := d.View()
		u := ui.New(nil, nil, func() ui.Element { return view() })
		for i := 0; i < 30; i++ { // settle
			u.Layout(900, 700)
		}
		d.doc.freshBuilds, d.doc.cacheHits, d.doc.patched = 0, 0, 0
		const n = 60
		for i := 0; i < n; i++ {
			u.Layout(900, 700)
		}
		t.Logf("%-14s fresh %6.1f  hits %6.1f  patched %5.1f",
			name, float64(d.doc.freshBuilds)/n, float64(d.doc.cacheHits)/n, float64(d.doc.patched)/n)
	}
}
