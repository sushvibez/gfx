package render

import (
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// CapHeight is the height of the face's 'H' at the font's size: Go
// Regular's H stands 1480 of its 2048 units over the baseline, linear in
// the size, and well under the line's ascent.
func TestCapHeight(t *testing.T) {
	var r *Renderer2D // NewFont needs no device for metrics
	for _, px := range []float64{10, 20, 48} {
		f, err := r.NewFont(goregular.TTF, px)
		if err != nil {
			t.Fatal(err)
		}
		want := float32(px) * 1480 / 2048
		if got := f.CapHeight(); got < want-0.1 || got > want+0.1 {
			t.Errorf("%v px: cap height %.2f, want %.2f", px, got, want)
		}
		if f.CapHeight() >= f.Ascent() {
			t.Errorf("%v px: cap height %.2f not under the ascent %.2f", px, f.CapHeight(), f.Ascent())
		}
	}
}
