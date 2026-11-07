package ui

import "testing"

func TestDefaultPalette(t *testing.T) {
	a, d := DefaultPalette(GroupActive), DefaultPalette(GroupDisabled)
	if a.Window != RGB(0xf0f0f0) || a.Highlight != RGB(0x308cc6) {
		t.Errorf("active window %v highlight %v", a.Window, a.Highlight)
	}
	if d.Text != RGB(0xbebebe) || d.Window != a.Window {
		t.Errorf("disabled text %v window %v: text greys, window stays", d.Text, d.Window)
	}
	if c, ok := a.Role("highlightedText"); !ok || c != a.HighlightedText {
		t.Errorf("Role(highlightedText) = %v, %v", c, ok)
	}
	if _, ok := a.Role("nope"); ok {
		t.Error("Role of a non-role reported ok")
	}
	if len(PaletteRoles()) != 21 {
		t.Errorf("%d roles, want 21", len(PaletteRoles()))
	}
}
