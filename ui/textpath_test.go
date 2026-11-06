package ui

import "testing"

// TextEl.DistanceField is QML's Text.renderType per run: it overrides
// the UI-wide path (UI.SetMSDFText) for one piece of text. It has to
// reach the damage signature, or a frame that only flipped the path
// would be skipped as unchanged and the text would not redraw.
func TestTextDistanceFieldIsPainted(t *testing.T) {
	c := RGB(0xffffff)
	base := Text("Hamburgefonstiv", c).paintSig()
	field := Text("Hamburgefonstiv", c).DistanceField(true).paintSig()
	raster := Text("Hamburgefonstiv", c).DistanceField(false).paintSig()
	if field == base {
		t.Error("DistanceField(true) does not change the paint signature")
	}
	if raster == base {
		t.Error("DistanceField(false) does not change the paint signature")
	}
	if field == raster {
		t.Error("the two paths share a paint signature")
	}
}
