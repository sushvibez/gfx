package ui

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// A flick keeps the content moving after the pointer lets go, slowing at a
// fixed rate, and stops — v²/2a further on, and not past the end.
func TestScrollFlingDecelerates(t *testing.T) {
	st := &ScrollState{contentH: 5000, viewH: 500}
	e := Scroll(st).Flickable()
	f := scrollFlick{e}
	f.fling(0, -600) // flick up: the content scrolls down at 600 px/s

	now := time.Unix(0, 0)
	var moved float32
	for i := 0; i < 200 && st.flingY != 0; i++ {
		before := st.Y
		now = now.Add(10 * time.Millisecond)
		st.advanceFling(now)
		if st.Y < before {
			t.Fatalf("the content moved backwards at step %d", i)
		}
		moved += st.Y - before
	}
	if st.flingY != 0 {
		t.Fatal("the flick never stopped")
	}
	// v²/2a = 600²/3000 = 120px, give or take a frame's worth.
	if want := float32(120); moved < want*0.8 || moved > want*1.2 {
		t.Errorf("flick travelled %.0fpx, want about %.0f", moved, want)
	}
}

// It stops dead at the end of the content rather than running past it.
func TestScrollFlingStopsAtTheEnd(t *testing.T) {
	st := &ScrollState{contentH: 600, viewH: 500} // 100px of travel
	scrollFlick{Scroll(st).Flickable()}.fling(0, -2000)
	now := time.Unix(0, 0)
	for i := 0; i < 200 && st.flingY != 0; i++ {
		now = now.Add(10 * time.Millisecond)
		st.advanceFling(now)
	}
	if st.Y != st.maxY() {
		t.Errorf("stopped at %.0f, want the end (%.0f)", st.Y, st.maxY())
	}
	if st.flingY != 0 {
		t.Error("the flick is still running at the end of the content")
	}
}

// Dragging moves the content with the pointer, and a press catches one that
// is still flying.
func TestScrollDragFollowsPointer(t *testing.T) {
	st := &ScrollState{contentH: 2000, viewH: 500}
	f := scrollFlick{Scroll(st).Flickable()}
	f.fling(0, -900)
	f.drag(0, 0, DragStart)
	if st.flingY != 0 {
		t.Error("the press did not catch the moving content")
	}
	f.drag(0, -80, DragMove) // pointer up 80px: content scrolls down 80px
	if st.Y != 80 {
		t.Errorf("offset %.0f after an 80px drag, want 80", st.Y)
	}
	f.drag(0, 40, DragMove) // back past the start: clamped at the top
	if st.Y != 0 {
		t.Errorf("offset %.0f, want 0 (clamped at the top)", st.Y)
	}
}

// The wheel belongs to the region under the pointer, innermost first: a
// list inside a page's scroll view takes it, and only a delta the list
// cannot use bubbles out to the page. Regions were registered after their
// content laid out, which put the OUTERMOST last — and the loop reads the
// list backwards, so the page swallowed every wheel and turning it over a
// list scrolled the whole page instead.
func TestWheelReachesTheInnermostRegion(t *testing.T) {
	inner := &ScrollState{}
	outer := &ScrollState{}
	list := Scroll(inner).Children(Div().Size(200, 2000))
	page := Scroll(outer).Children(
		Div().Col().Children(Div().Size(200, 300), list, Div().Size(200, 2000)),
	)
	u := New(nil, nil, func() Element { return page })
	u.Layout(200, 400)
	// Over the inner list (which sits 300px down, 400 tall in a 400 view —
	// laid out inside the page's content).
	b := Bounds(list)
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: b.X + 10, Y: b.Y + 10, ScrollY: 48}})
	if inner.Y == 0 {
		t.Fatalf("the inner list did not take the wheel (inner %v, page %v)", inner.Y, outer.Y)
	}
	if outer.Y != 0 {
		t.Errorf("the page scrolled too (%v): the list had room for the whole delta", outer.Y)
	}
	// At the end of the inner list, the rest bubbles out to the page.
	inner.Y = inner.maxY()
	u.Layout(200, 400)
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: b.X + 10, Y: b.Y + 10, ScrollY: 48}})
	if outer.Y == 0 {
		t.Error("a delta the list could not use never reached the page")
	}
}

// A QML view has no scrollbar of its own — one is attached with
// `ScrollBar.vertical`, and Qt's own bar is visible only while the view is
// moving. Painted permanently, ours drew a line down the side of every page
// the reference has not got.
func TestAutoHidingBarFadesWhenIdle(t *testing.T) {
	st := &ScrollState{}
	// Nothing has moved: nothing to show.
	if a := st.barAlpha(); a != 0 {
		t.Errorf("a view that never moved has bar alpha %v, want 0", a)
	}
	st.contentH, st.viewH = 1000, 200
	if !st.scrollBy(60) {
		t.Fatal("the view did not scroll")
	}
	if a := st.barAlpha(); a != 1 {
		t.Errorf("bar alpha %v just after scrolling, want 1", a)
	}
	// It holds, then fades, then it is gone.
	core.TickAnimations(300 * time.Millisecond)
	if a := st.barAlpha(); a != 1 {
		t.Errorf("bar alpha %v 300ms after scrolling, want it still held", a)
	}
	core.TickAnimations(250 * time.Millisecond)
	if a := st.barAlpha(); a <= 0 || a >= 1 {
		t.Errorf("bar alpha %v mid-fade, want between 0 and 1", a)
	}
	core.TickAnimations(500 * time.Millisecond)
	if a := st.barAlpha(); a != 0 {
		t.Errorf("bar alpha %v long after scrolling, want 0", a)
	}
	// Moving again brings it back.
	st.scrollBy(-30)
	if a := st.barAlpha(); a != 1 {
		t.Errorf("bar alpha %v after scrolling again, want 1", a)
	}
}

// One wheel notch scrolls what Qt scrolls. Measured with a QtTest case
// sending an angle delta of 120 to a ListView and a Flickable: each moves
// 72 pixels, and notches add up linearly (three of them, 216). Our
// platform backends deliver that many pixels per notch, so a page scrolls
// the same distance under the same gesture.
func TestWheelNotchMatchesQt(t *testing.T) {
	st := &ScrollState{contentH: 5000, viewH: 500}
	e := Scroll(st).Children(Div().Size(100, 5000))
	u := New(nil, nil, func() Element { return e })
	u.Layout(100, 500)
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 50, Y: 50, ScrollY: 72}})
	if st.Y != 72 {
		t.Errorf("one notch scrolled %v, want 72 (Qt's measured step)", st.Y)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseScroll, X: 50, Y: 50, ScrollY: 144}})
	if st.Y != 216 {
		t.Errorf("three notches scrolled %v in total, want 216 (Qt adds them linearly)", st.Y)
	}
}

// Bg and Rounded are part of a scroll view's paint signature, so changing
// the background repaints it under damage tracking.
func TestScrollBgInPaintSig(t *testing.T) {
	st := &ScrollState{}
	plain := Scroll(st).paintSig()
	bg := Scroll(st).Bg(RGBA{R: 1, A: 1}).paintSig()
	round := Scroll(st).Bg(RGBA{R: 1, A: 1}).Rounded(6).paintSig()
	if plain == bg || bg == round {
		t.Errorf("paint signatures %x %x %x: background and radius must change it", plain, bg, round)
	}
}
