package gml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// corpusPage loads one page of the QML examples corpus (skipped when the
// checkout is not there), for benchmarks that need a REAL document: styled
// controls, file components, a running animation.
func corpusPage(b *testing.B, name string) *Document {
	root := os.Getenv("GFX_QML_CORPUS")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, "workspace", "qml-examples")
	}
	path := filepath.Join(root, "examples", name, "Main.qml")
	if _, err := os.Stat(path); err != nil {
		b.Skipf("corpus page not found: %s", path)
	}
	d, err := Load(path, NewEnv())
	if err != nil {
		b.Fatalf("Load: %v", err)
	}
	return d
}

// BenchmarkCorpusButtonsFrame is one frame of the buttons page — build +
// layout at 900x700 — with its pulse animation running, so the subtree
// chain from the animated ring to the root rebuilds every frame the way it
// does live. Deterministic where a live window is not: no present, no
// vsync sleep, no clock ramp between frames.
func BenchmarkCorpusButtonsFrame(b *testing.B) {
	d := corpusPage(b, "buttons")
	view := d.View()
	u := ui.New(nil, nil, func() ui.Element { return view() })
	for i := 0; i < 30; i++ { // settle: first frames declare, lay out, complete
		u.Layout(900, 700)
	}
	d.doc.freshBuilds = 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.Layout(900, 700)
	}
	b.StopTimer()
	b.ReportMetric(float64(d.doc.freshBuilds)/float64(b.N), "rebuilds/frame")
}

// One frame of a page built out of DELEGATE ROWS — the case the subtree
// cache used to be switched off for entirely. navdisplay is four cards
// sharing state from the root, one of them a ListView of waypoints;
// chatui is two lists of chat bubbles.
func BenchmarkCorpusNavDisplayFrame(b *testing.B) { benchCorpusFrame(b, "navdisplay") }
func BenchmarkCorpusChatUIFrame(b *testing.B)     { benchCorpusFrame(b, "chatui") }
func BenchmarkCorpusIndicatorsFrame(b *testing.B) { benchCorpusFrame(b, "indicators") }

func benchCorpusFrame(b *testing.B, page string) {
	d := corpusPage(b, page)
	view := d.View()
	u := ui.New(nil, nil, func() ui.Element { return view() })
	for i := 0; i < 30; i++ {
		u.Layout(900, 700)
	}
	d.doc.freshBuilds = 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.Layout(900, 700)
	}
	b.StopTimer()
	b.ReportMetric(float64(d.doc.freshBuilds)/float64(b.N), "rebuilds/frame")
}
