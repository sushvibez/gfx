package ui

import (
	"testing"

	"github.com/sushvibez/gfx/app"
)

func TestColorHSVRoundTrip(t *testing.T) {
	for _, hex := range []uint32{0xff0000, 0x00ff00, 0x0000ff, 0xffcc88, 0x336699, 0x808080, 0x000000, 0xffffff} {
		c := RGB(hex)
		h, s, v := rgbToHSV(c)
		if got := ColorHex(hsvToRGB(h, s, v)); got != ColorHex(c) {
			t.Errorf("%06x: back as %s", hex, got)
		}
	}
}

// A ColorField opens on a click; the square sets saturation and value
// (live, and done on release), the hue bar the hue, a swatch and the hex
// field the colour outright; a click outside closes it.
func TestColorField(t *testing.T) {
	c := RGB(0x808080)
	st := NewColorState()
	var edits int
	var done []string
	u := New(nil, nil, func() Element {
		return Div().Children(ColorField(c, st, ColorOpts{
			OnChange: func(nc RGBA) { c = nc; edits++ },
			OnDone:   func(nc RGBA) { done = append(done, ColorHex(nc)) },
		}))
	})
	frame := func() { u.Layout(400, 400) }
	press := func(kind app.MouseKind, x, y float32) {
		frame()
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}, {Kind: kind, X: x, Y: y, Button: 1}})
		frame()
	}
	click := func(x, y float32) { press(app.MouseDown, x, y); press(app.MouseUp, x, y) }
	click(40, 14)
	if !st.Open.Get() {
		t.Fatal("a click did not open the picker")
	}
	// The square: field 28 + gap 2 + panel padding 8 → y 38…188, x 8…208.
	press(app.MouseDown, 120, 120)
	frame()
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 207, Y: 39, Button: 1}})
	frame()
	if h, s, v := rgbToHSV(c); h > 0.01 || s < 0.95 || v < 0.95 {
		t.Errorf("dragged to the square's top right from grey: %s, want red (hue 0)", ColorHex(c))
	}
	if len(done) != 0 {
		t.Errorf("done before the release: %v", done)
	}
	press(app.MouseUp, 207, 39)
	if len(done) != 1 || edits < 2 {
		t.Errorf("after the release: %d edits, done %v", edits, done)
	}
	// The hue bar, half way: cyan.
	click(108, 205)
	if h, _, _ := rgbToHSV(c); h < 0.48 || h > 0.52 {
		t.Errorf("hue bar half way: %s (hue %.2f)", ColorHex(c), h)
	}
	// Hex field.
	st.Hex.SetText("#336699")
	st.Hex.Key(app.KeyEvent{Down: true, Key: app.KeyEnter})
	frame()
	if ColorHex(c) != "#336699" || done[len(done)-1] != "#336699" {
		t.Errorf("hex Enter: %s, done %v", ColorHex(c), done)
	}
	click(390, 390)
	if st.Open.Get() {
		t.Error("an outside click did not close it")
	}
}

// A drop-down near the window's bottom opens above its trigger rather than
// being snapped up over it.
func TestAnchorBelowFlipsUp(t *testing.T) {
	var trigger *DivEl
	var ov *OverlayEl
	u := New(nil, nil, func() Element {
		trigger = Div().Size(100, 28)
		ov = Overlay(Div().Size(120, 200), 0, 0).AnchorBelow(trigger, 2).SnapToWindow(4)
		return Div().Col().Children(Div().H(300), trigger, ov)
	})
	u.Layout(400, 400)
	if r := ov.rect; r.Y+r.H > trigger.rect.Y {
		t.Errorf("panel %v overlaps its trigger at y %v: want it above", r, trigger.rect.Y)
	}
	u.Layout(400, 800)
	if r := ov.rect; r.Y < trigger.rect.Y+trigger.rect.H {
		t.Errorf("room below: panel %v, want it under the trigger (%v)", r, trigger.rect)
	}
}

// A page that hands the colour back late (an event behind) does not undo
// the picker: its own colours coming back are not changes; a colour from
// elsewhere still re-seeds it.
func TestColorFieldLaggingPage(t *testing.T) {
	page := RGB(0x808080)
	var latest RGBA
	st := NewColorState()
	u := New(nil, nil, func() Element {
		return Div().Children(ColorField(page, st, ColorOpts{OnChange: func(nc RGBA) { latest = nc }}))
	})
	frame := func() { u.Layout(400, 400) }
	click := func(x, y float32) {
		frame()
		u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: x, Y: y}, {Kind: app.MouseDown, X: x, Y: y, Button: 1}, {Kind: app.MouseUp, X: x, Y: y, Button: 1}})
		frame()
	}
	click(40, 14) // open
	click(108, 205)
	first := latest // hue half way
	click(60, 205)
	page = first // the page catches up with the FIRST pick only now
	frame()
	if h, _, _ := rgbToHSV(st.synced); h > 0.3 {
		t.Errorf("a late echo of the first pick re-seeded the picker: hue %.2f, want the second pick's (≈0.26)", h)
	}
	page = RGB(0x00ff00) // a change from elsewhere
	frame()
	if ColorHex(st.synced) != "#00ff00" {
		t.Errorf("an outside change: picker at %s", ColorHex(st.synced))
	}
}
