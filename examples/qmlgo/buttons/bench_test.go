package main

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/qmlgo/corpus"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

// BenchmarkButtonsFrame is one frame of the Go port — view rebuild + layout
// at 900x700 with the pulse animation advancing — the counterpart of
// script/gml's BenchmarkCorpusButtonsFrame, so the two front ends are
// compared on the same deterministic footing (no present, no vsync sleep,
// no clock ramp). Neither loads a font or icons, so text measures trivially
// on both sides alike.
func BenchmarkButtonsFrame(b *testing.B) {
	s := &state{glowLayer: ui.NewLayerState(), icons: corpus.NewIcons(&shell.App{})}
	s.status.Set("Interact with the buttons above")
	u := ui.New(nil, nil, s.view)
	for i := 0; i < 30; i++ {
		core.TickAnimations(time.Second / 60)
		u.Layout(900, 700)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		core.TickAnimations(time.Second / 60)
		u.Layout(900, 700)
	}
}
